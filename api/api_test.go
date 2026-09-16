package api

import (
	"context"
	"efg/mongoclient"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fulldump/box"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func TestHealth(t *testing.T) {
	response := httptest.NewRecorder()
	New(nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" {
		t.Fatalf("body = %v, want status ok", body)
	}
}

func TestRoutingErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		method string
		path   string
		status int
	}{
		{"missing route", http.MethodGet, "/missing", http.StatusNotFound},
		{"unsupported method", http.MethodPost, "/health", http.StatusMethodNotAllowed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			New(nil).ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, nil))
			if response.Code != tc.status {
				t.Fatalf("status = %d, want %d", response.Code, tc.status)
			}
		})
	}
}

func TestMongoClientAvailableToInterceptors(t *testing.T) {
	client := &mongo.Client{}
	b := New(client)
	called := false
	b.Use(func(next box.H) box.H {
		return func(ctx context.Context) {
			called = true
			if mongoclient.GetMongoClient(ctx) != client {
				t.Fatal("interceptor did not receive the configured MongoDB client")
			}
			next(ctx)
		}
	})
	b.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))
	if !called {
		t.Fatal("interceptor was not called")
	}
}
