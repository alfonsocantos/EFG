package presence_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"efg/api"
	"efg/presence"

	"github.com/fulldump/box"

	"github.com/fulldump/biff"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestInvalidEvents(t *testing.T) {
	for _, body := range []string{
		`{`, `null`, `{}`, `{"server_id":"s","events":[]}`,
		`{"server_id":"s","events":[{"state":"online","occurred_at_ms":1}]}`,
		`{"server_id":"s","events":[{"player_id":"p","state":"online","occurred_at_ms":0}]}`,
		`{"server_id":"s","events":[{"player_id":"p","state":"online","occurred_at_ms":1},{"player_id":"q","state":"away","occurred_at_ms":0}]}`,
	} {
		t.Run(body, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			newTestAPI(t, nil).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(body)))
			biff.AssertEqual(recorder.Code, http.StatusBadRequest)
		})
	}
}

func TestEventsDatabaseError(t *testing.T) {
	client, err := mongo.Connect(options.Client().ApplyURI("mongodb://localhost:27017"))
	biff.AssertNil(err)

	err = client.Disconnect(context.Background())
	biff.AssertNil(err)

	recorder := httptest.NewRecorder()
	body := `{"server_id":"s","events":[{"player_id":"p","state":"online","occurred_at_ms":1}]}`
	newTestAPI(t, client).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(body)))
	biff.AssertEqual(recorder.Code, http.StatusInternalServerError)
}

// Integration tests use unique player IDs and remove only their own records.
func fixture(t testing.TB) (*httptest.Server, *mongo.Collection, string) {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("Set MONGO_TEST_URI to run MongoDB integration tests and benchmarks")
	}

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := client.Disconnect(ctx); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.Ping(ctx, nil); err != nil {
		t.Fatal(err)
	}
	players := client.Database("efg").Collection("players")
	prefix := "test-" + bson.NewObjectID().Hex()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := players.DeleteMany(ctx, bson.M{"_id": bson.M{"$regex": "^" + prefix}}); err != nil {
			t.Error(err)
		}
	})
	server := httptest.NewServer(newTestAPI(t, client))
	server.Client().Timeout = 15 * time.Second
	t.Cleanup(server.Close)
	return server, players, prefix
}

func send(server *httptest.Server, batch presence.EventsRequest) error {
	body, err := json.Marshal(batch)
	if err != nil {
		return err
	}
	response, err := server.Client().Post(server.URL+"/events", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("status = %d, want 204: %s", response.StatusCode, data)
	}
	return nil
}

func assertPlayerFields(t *testing.T, players *mongo.Collection, id string, want bson.M) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var player bson.M
	if err := players.FindOne(ctx, bson.M{"_id": id}).Decode(&player); err != nil {
		t.Fatal(err)
	}
	for field, expected := range want {
		if player[field] != expected {
			t.Errorf("player %q: %s = %v, want %v", id, field, player[field], expected)
		}
	}
}

func TestEventsMongo(t *testing.T) {
	server, players, prefix := fixture(t)
	wantServer, wantGame := "", ""
	var newestTimestamp int64
	for _, step := range []struct {
		name, server, game, state string
		timestamp                 int64
		wantState                 string
		wantTimestamp             int64
	}{
		{"online", "server", "CSGO", "online", 100, "online", 100},
		{"ingame", "server", "CSGO", "ingame", 300, "ingame", 300},
		{"older", "server", "CSGO", "offline", 200, "ingame", 300},
		{"equal", "server", "CSGO", "offline", 300, "ingame", 300},
		{"new server", "other", "CSGO", "ingame", 400, "ingame", 400},
		{"new game", "other", "$new-game", "ingame", 600, "ingame", 600},
		{"stale metadata", "old-server", "old-game", "offline", 550, "ingame", 600},
		{"equal metadata", "old-server", "old-game", "offline", 600, "ingame", 600},
		{"offline", "other", "$new-game", "offline", 700, "offline", 700},
		{"custom state", "other", "$new-game", "away", 710, "away", 710},
		{"future state", "other", "$new-game", "$client-defined-state", 720, "$client-defined-state", 720},
		{"stale custom state", "other", "$new-game", "spectating", 715, "$client-defined-state", 720},
	} {
		t.Run(step.name, func(t *testing.T) {
			batch := presence.EventsRequest{ServerID: step.server, Events: []presence.Event{{
				PlayerID: prefix, State: step.state, Game: step.game, OccurredAtMS: step.timestamp,
			}}}
			if err := send(server, batch); err != nil {
				t.Fatal(err)
			}
			if step.timestamp > newestTimestamp {
				newestTimestamp = step.timestamp
				wantServer = step.server
				wantGame = step.game
			}
			assertPlayerFields(t, players, prefix, bson.M{
				"state": step.wantState, "last_event_ms": step.wantTimestamp,
				"server_id": wantServer,
				"game":      wantGame,
			})
		})
	}

	batch := presence.EventsRequest{ServerID: "server", Events: []presence.Event{
		{PlayerID: prefix, State: "ingame", OccurredAtMS: 800},
		{PlayerID: prefix, State: "online", OccurredAtMS: 750},
		{PlayerID: prefix + "-other", State: "online", OccurredAtMS: 100},
		{PlayerID: prefix + "-unknown", State: "online", OccurredAtMS: 100},
	}}
	if err := send(server, batch); err != nil {
		t.Fatal(err)
	}
	assertPlayerFields(t, players, prefix, bson.M{
		"state": "ingame", "last_event_ms": int64(800),
	})
	for _, id := range []string{prefix + "-other", prefix + "-unknown"} {
		assertPlayerFields(t, players, id, bson.M{
			"state": "online", "last_event_ms": int64(100),
		})
	}
}

func TestExistingPlayerFieldsMongo(t *testing.T) {
	server, players, prefix := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := players.InsertOne(ctx, bson.M{"_id": prefix, "nickname": "Alice"}); err != nil {
		t.Fatal(err)
	}
	batch := presence.EventsRequest{ServerID: "$server", Events: []presence.Event{{
		PlayerID: prefix, State: "online", OccurredAtMS: 100,
	}}}
	if err := send(server, batch); err != nil {
		t.Fatal(err)
	}
	assertPlayerFields(t, players, prefix, bson.M{
		"nickname": "Alice", "last_event_ms": int64(100),
		"server_id": "$server",
	})
}

func TestConcurrentEventsMongo(t *testing.T) {
	server, players, prefix := fixture(t)
	var group sync.WaitGroup
	for i := 1; i <= 50; i++ {
		group.Go(func() {
			batch := presence.EventsRequest{ServerID: fmt.Sprint(i), Events: []presence.Event{{
				PlayerID: prefix, State: "ingame", OccurredAtMS: int64(i),
			}}}
			if err := send(server, batch); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	assertPlayerFields(t, players, prefix, bson.M{
		"last_event_ms": int64(50), "server_id": "50",
	})
}

func BenchmarkEvents(b *testing.B) {
	server, players, prefix := fixture(b)
	documents := make([]any, 0, 1000)
	for match := 0; match < 100; match++ {
		for player := 0; player < 10; player++ {
			documents = append(documents, bson.M{
				"_id":       fmt.Sprintf("%s-%d-%d", prefix, match, player),
				"server_id": prefix,
				"state":     "offline", "last_event_ms": int64(0),
			})
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := players.InsertMany(ctx, documents); err != nil {
		b.Fatal(err)
	}
	var sequence atomic.Int64
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			n := sequence.Add(1)
			batch := presence.EventsRequest{ServerID: prefix, Events: make([]presence.Event, 10)}
			for i := range batch.Events {
				batch.Events[i] = presence.Event{
					PlayerID: fmt.Sprintf("%s-%d-%d", prefix, n%100, i),
					State:    "ingame", OccurredAtMS: n,
				}
			}
			if err := send(server, batch); err != nil {
				b.Error(err)
			}
		}
	})
	b.StopTimer()
	checkCtx, checkCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer checkCancel()
	count, err := players.CountDocuments(checkCtx, bson.M{"server_id": prefix, "last_event_ms": bson.M{"$gt": 0}})
	if err != nil || count != int64(min(b.N, 100)*10) {
		b.Fatalf("load test did not update expected players: count=%d, error=%v", count, err)
	}
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "batches/s")
	b.ReportMetric(float64(b.N*10)/b.Elapsed().Seconds(), "events/s")
}

func newTestAPI(t testing.TB, client *mongo.Client) *box.B {
	return api.New(client, presence.NewQueue(t.Context(), 100, 4, presence.Events))
}
