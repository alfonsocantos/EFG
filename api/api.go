package api

import (
	"context"
	"efg/mongoclient"
	"efg/presence"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/fulldump/box"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// New builds the API routes.
func New(client *mongo.Client) *box.B {
	b := box.NewBox()
	b.Use(handleErrors)
	b.Use(func(next box.H) box.H {
		return func(ctx context.Context) {
			next(mongoclient.SetMongoClient(ctx, client))
		}
	})
	b.HandleResourceNotFound = http.NotFound
	b.HandleMethodNotAllowed = func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}

	b.Handle(http.MethodGet, "/health", func() map[string]string {
		return map[string]string{"status": "ok"}
	})
	b.Handle(http.MethodPost, "/events", presence.Events)

	return b
}

// handleErrors turns Box deserialization and action errors into HTTP responses.
func handleErrors(next box.H) box.H {
	return func(ctx context.Context) {

		next(ctx)

		err := box.GetError(ctx)
		if err == nil {
			return
		}

		// todo handle different errors
		if errors.Is(err, presence.ErrInvalidEvents) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			http.Error(box.GetResponse(ctx), err.Error(), http.StatusBadRequest)
			return
		}
		log.Printf("API error: %v", err)
		http.Error(box.GetResponse(ctx), "Internal server error", http.StatusInternalServerError)
	}
}
