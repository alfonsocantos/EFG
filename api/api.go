package api

import (
	"context"
	"efg/mongoclient"
	"net/http"

	"github.com/fulldump/box"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// New builds the API routes.
func New(client *mongo.Client) *box.B {
	b := box.NewBox()
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

	return b
}
