package presence_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"efg/presence"

	"github.com/fulldump/biff"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestInvalidStatus(t *testing.T) {
	for _, tc := range []struct {
		name, method, playerID, body string
		status                       int
	}{
		{"missing identity on read", http.MethodGet, "", "", http.StatusUnauthorized},
		{"missing identity on write", http.MethodPut, "", `{"offline_mode":true}`, http.StatusUnauthorized},
		{"blank identity", http.MethodPut, "  ", `{"offline_mode":true}`, http.StatusUnauthorized},
		{"missing preference", http.MethodPut, "player", `{}`, http.StatusBadRequest},
		{"null preference", http.MethodPut, "player", `{"offline_mode":null}`, http.StatusBadRequest},
		{"wrong type", http.MethodPut, "player", `{"offline_mode":"true"}`, http.StatusBadRequest},
		{"malformed JSON", http.MethodPut, "player", `{oops}`, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, "/me/status", strings.NewReader(tc.body))
			request.Header.Set("X-Player-ID", tc.playerID)
			response := httptest.NewRecorder()
			newTestAPI(t, nil).ServeHTTP(response, request)
			biff.AssertEqual(response.Code, tc.status)
		})
	}
}

func statusRequest(t *testing.T, server *httptest.Server, method, playerID, body string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, server.URL+"/me/status", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Player-ID", playerID)
	request.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { response.Body.Close() })
	return response
}

func TestOfflineModeMongo(t *testing.T) {
	server, players, playerID := fixture(t)
	setMode := func(body string) {
		t.Helper()
		response := statusRequest(t, server, http.MethodPut, playerID, body)
		if response.StatusCode != http.StatusNoContent {
			data, _ := io.ReadAll(response.Body)
			t.Fatalf("status = %d, want 204: %s", response.StatusCode, data)
		}
	}
	checkStatus := func(id, state string, offline bool) {
		t.Helper()
		response := statusRequest(t, server, http.MethodGet, id, "")
		if response.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", response.StatusCode)
		}
		var got presence.StatusResponse
		if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got.State != state || got.OfflineMode != offline {
			t.Fatalf("status = %+v, want state=%s offline_mode=%v", got, state, offline)
		}
	}

	checkStatus(playerID, "offline", false)
	setMode(`{"offline_mode":true}`) // Preference can be set before any event.
	setMode(`{"offline_mode":true}`) // Repeating the request is harmless.
	checkStatus(playerID, "offline", true)

	batch := presence.EventsRequest{ServerID: "server", Events: []presence.Event{
		{PlayerID: playerID, State: "ingame", OccurredAtMS: 100},
		{PlayerID: playerID + "-other", State: "online", OccurredAtMS: 100},
	}}
	if err := send(server, batch); err != nil {
		t.Fatal(err)
	}
	checkStatus(playerID, "offline", true)
	checkStatus(playerID+"-other", "online", false)
	assertPlayerFields(t, players, playerID, bson.M{"state": "ingame", "offline_mode": true, "last_event_ms": int64(100)})

	setMode(`{"offline_mode":false}`)
	checkStatus(playerID, "ingame", false)
	setMode(`{"offline_mode":true}`) // Also works after matchmaking events exist.
	batch.Events = []presence.Event{{PlayerID: playerID, State: "spectating", OccurredAtMS: 200}}
	if err := send(server, batch); err != nil {
		t.Fatal(err)
	}
	checkStatus(playerID, "offline", true)
	setMode(`{"offline_mode":false}`)
	checkStatus(playerID, "spectating", false)
}
