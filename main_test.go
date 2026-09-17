package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"efg/presence"
)

func TestShutdown(t *testing.T) {
	for _, expire := range []bool{false, true} {
		name := "graceful"
		if expire {
			name = "timeout"
		}
		t.Run(name, func(t *testing.T) {
			started := make(chan struct{})
			release := make(chan struct{})
			queue := presence.NewQueue(t.Context(), 1, 1, func(ctx context.Context, _ presence.EventsRequest) error {
				close(started)
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := queue.Events(r.Context(), presence.EventsRequest{}); err != nil {
					http.Error(w, err.Error(), 503)
					return
				}
				w.WriteHeader(204)
			}))
			defer server.Close()
			response := make(chan int, 1)
			go func() {
				r, err := server.Client().Get(server.URL)
				if err != nil {
					response <- 0
					return
				}
				io.Copy(io.Discard, r.Body)
				r.Body.Close()
				response <- r.StatusCode
			}()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("request did not start")
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if expire {
				ctx, cancel = context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
				defer cancel()
			} else {
				close(release)
			}
			err := shutdown(ctx, server.Config, queue)
			if expire && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected deadline error, got %v", err)
			}
			if !expire && err != nil {
				t.Fatal(err)
			}
			select {
			case status := <-response:
				if !expire && status != 204 {
					t.Fatalf("persisted job was not acknowledged: %d", status)
				}
				if expire && status == 204 {
					t.Fatal("unfinished job was acknowledged")
				}
			case <-time.After(time.Second):
				t.Fatal("HTTP client remained blocked")
			}
		})
	}
}
