package api

import (
	"context"
	"efg/mongoclient"
	"efg/presence"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fulldump/biff"
	"github.com/fulldump/box"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func TestHealth(t *testing.T) {
	response := httptest.NewRecorder()
	newTestAPI(t, nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))

	biff.AssertEqual(response.Code, http.StatusOK)
	biff.AssertEqual(response.Header().Get("Content-Type"), "application/json")
	var body presence.Health
	biff.AssertNil(json.Unmarshal(response.Body.Bytes(), &body))
	biff.AssertEqual(body.Status, "ok")
	biff.AssertEqual(body.EventsPerSecond, uint64(0))
	biff.AssertEqual(body.Queue.Length, 0)
	biff.AssertEqual(body.Queue.Capacity, 100)
	biff.AssertEqual(body.Workers.Busy, int64(0))
	biff.AssertEqual(body.Workers.Total, 4)
}

func TestRoutingErrors(t *testing.T) {
	biff.Alternative("API routes", func(a *biff.A) {
		api := newTestAPI(t, nil)
		for _, tc := range []struct {
			name   string
			method string
			path   string
			status int
		}{
			{"missing route", http.MethodGet, "/missing", http.StatusNotFound},
			{"unsupported method", http.MethodPost, "/health", http.StatusMethodNotAllowed},
		} {
			a.Alternative(tc.name, func(a *biff.A) {
				response := httptest.NewRecorder()
				api.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, nil))
				biff.AssertEqual(response.Code, tc.status)
			})
		}
	})
}

func TestMongoClientAvailableToInterceptors(t *testing.T) {
	client := &mongo.Client{}
	b := newTestAPI(t, client)
	called := false
	b.Use(func(next box.H) box.H {
		return func(ctx context.Context) {
			called = true
			biff.AssertTrue(mongoclient.GetMongoClient(ctx) == client)
			next(ctx)
		}
	})
	b.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))
	biff.AssertTrue(called)
}

func newTestAPI(t *testing.T, client *mongo.Client) *box.B {
	return New(client, presence.NewQueue(t.Context(), 100, 4, presence.Events))
}

func TestEventsQueueBackpressure(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	persistErr := errors.New("persistence failed")
	queue := presence.NewQueue(t.Context(), 1, 1, func(ctx context.Context, _ presence.EventsRequest) error {
		started <- struct{}{}
		select {
		case <-release:
			return persistErr
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	handler := New(nil, queue)
	post := func() *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(`{"server_id":"demo","events":[{"player_id":"p","state":"online","occurred_at_ms":1}]}`)))
		return response
	}
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- post() }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	second := make(chan *httptest.ResponseRecorder, 1)
	go func() { second <- post() }()
	deadline := time.Now().Add(time.Second)
	for queue.Health().Queue.Length != 1 {
		if time.Now().After(deadline) {
			t.Fatal("second batch was not queued")
		}
		time.Sleep(time.Millisecond)
	}

	healthResponse := httptest.NewRecorder()
	handler.ServeHTTP(healthResponse, httptest.NewRequest(http.MethodGet, "/health", nil))
	var health presence.Health
	if err := json.Unmarshal(healthResponse.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if healthResponse.Code != 200 || health.Queue.Length != 1 || health.Workers.Busy != 1 {
		t.Fatalf("unexpected saturated health: %s", healthResponse.Body)
	}
	rejected := post()
	if rejected.Code != 503 || rejected.Header().Get("Retry-After") != "1" {
		t.Fatalf("expected 503 with Retry-After, got %d: %s", rejected.Code, rejected.Body)
	}
	select {
	case <-first:
		t.Fatal("request completed before persistence")
	default:
	}
	close(release)
	for _, result := range []chan *httptest.ResponseRecorder{first, second} {
		select {
		case response := <-result:
			if response.Code != 500 {
				t.Fatalf("persistence error was lost: %d", response.Code)
			}
		case <-time.After(time.Second):
			t.Fatal("request did not complete")
		}
	}
	if health := queue.Health(); health.Queue.Length != 0 || health.Workers.Busy != 0 {
		t.Fatalf("queue did not drain: %+v", health)
	}
}
