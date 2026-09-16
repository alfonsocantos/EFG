package api

import (
	"context"
	"efg/mongoclient"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fulldump/biff"
	"github.com/fulldump/box"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func TestHealth(t *testing.T) {
	response := httptest.NewRecorder()
	New(nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))

	biff.AssertEqual(response.Code, http.StatusOK)
	biff.AssertEqual(response.Header().Get("Content-Type"), "application/json")
	var body map[string]string
	biff.AssertNil(json.Unmarshal(response.Body.Bytes(), &body))
	biff.AssertEqual(body, map[string]string{"status": "ok"})
}

func TestRoutingErrors(t *testing.T) {
	biff.Alternative("API routes", func(a *biff.A) {
		api := New(nil)
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
	b := New(client)
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
