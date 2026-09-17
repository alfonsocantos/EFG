package presence_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"efg/api"
	"efg/presence"

	"github.com/fulldump/biff"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestFriendsPresenceRequiresIdentity(t *testing.T) {
	for _, id := range []string{"", "  "} {
		request := httptest.NewRequest(http.MethodGet, "/me/friends/presence", nil)
		request.Header.Set("X-Player-ID", id)
		response := httptest.NewRecorder()
		api.New(nil).ServeHTTP(response, request)
		biff.AssertEqual(response.Code, http.StatusUnauthorized)
	}
}

func readFriends(t *testing.T, server *httptest.Server, playerID string) []map[string]any {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, server.URL+"/me/friends/presence", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Player-ID", playerID)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	var friends []map[string]any
	if err := json.NewDecoder(response.Body).Decode(&friends); err != nil {
		t.Fatal(err)
	}
	return friends
}

func TestFriendsPresenceMongo(t *testing.T) {
	server, players, playerID := fixture(t)
	if got := readFriends(t, server, playerID); got == nil || len(got) != 0 {
		t.Fatalf("unknown player should return [], got %v", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := players.InsertOne(ctx, bson.M{"_id": playerID}); err != nil {
		t.Fatal(err)
	}
	if got := readFriends(t, server, playerID); got == nil || len(got) != 0 {
		t.Fatalf("player without friends should return [], got %v", got)
	}

	ids := make([]string, 50)
	want := make([]map[string]any, 50)
	batch := presence.EventsRequest{ServerID: "server"}
	for i := range ids {
		ids[i] = fmt.Sprintf("%s-friend-%02d", playerID, 49-i)
		state := "ingame"
		if i == 1 {
			state = "offline"
		}
		if i == 2 {
			state = "spectating" // States remain client-defined.
		}
		want[i] = map[string]any{"player_id": ids[i], "state": state, "game": "chess"}
		if i == 0 || i == 1 || i >= 48 {
			want[i]["state"] = "offline"
			delete(want[i], "game")
		}
		if i < 48 {
			batch.Events = append(batch.Events, presence.Event{
				PlayerID: ids[i], State: state, Game: "chess", OccurredAtMS: 100,
			})
		}
	}
	// One friend exists without events; the last friend does not exist yet.
	if _, err := players.InsertOne(ctx, bson.M{"_id": ids[48]}); err != nil {
		t.Fatal(err)
	}
	if _, err := players.UpdateOne(ctx, bson.M{"_id": playerID}, bson.M{"$set": bson.M{"friends": ids}}); err != nil {
		t.Fatal(err)
	}
	// The caller's own events must preserve friends and not appear in the list.
	batch.Events = append(batch.Events, presence.Event{
		PlayerID: playerID, State: "online", Game: "other-game", OccurredAtMS: 100,
	})
	if err := send(server, batch); err != nil {
		t.Fatal(err)
	}
	response := statusRequest(t, server, http.MethodPut, ids[0], `{"offline_mode":true}`)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", response.StatusCode)
	}
	if got := readFriends(t, server, playerID); !reflect.DeepEqual(got, want) {
		t.Fatalf("friends = %v, want %v", got, want)
	}
	// Disabling the preference reveals the real state and game again.
	response = statusRequest(t, server, http.MethodPut, ids[0], `{"offline_mode":false}`)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", response.StatusCode)
	}
	want[0]["state"], want[0]["game"] = "ingame", "chess"
	if got := readFriends(t, server, playerID); !reflect.DeepEqual(got, want) {
		t.Fatalf("friends = %v, want %v", got, want)
	}
}
