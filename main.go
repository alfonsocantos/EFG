package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"efg/api"

	"github.com/fulldump/goconfig"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

type Config struct {
	Addr     string `json:"addr" usage:"HTTP server listen address"`
	MongoURI string `json:"mongo_uri" usage:"MongoDB connection URI"`
}

func main() {
	cfg := Config{
		Addr:     ":8080",
		MongoURI: "mongodb://localhost:27017/efg",
	}

	goconfig.Read(&cfg)

	if cfg.MongoURI == "" {
		panic("MongoDB connection URI is required")
	}
	client, err := mongo.Connect(options.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		panic(fmt.Errorf("connect to MongoDB: %w", err))
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := client.Disconnect(ctx); err != nil {
			log.Printf("Disconnect from MongoDB: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	err = client.Ping(ctx, readpref.Primary())
	cancel()
	if err != nil {
		panic(fmt.Errorf("ping MongoDB: %w", err))
	}

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.New(client),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("Listening on %s", cfg.Addr)
	if err := server.ListenAndServe(); err != nil {
		panic(err)
	}
}
