package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"efg/api"
	"efg/presence"

	"github.com/fulldump/goconfig"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

type Config struct {
	ShutdownTimeout time.Duration `json:"shutdown_timeout" usage:"Maximum time to drain requests and event jobs on shutdown"`
	QueueSize       int           `json:"queue_size" usage:"Maximum pending event batches"`
	Workers         int           `json:"workers" usage:"Number of event workers"`
	Addr            string        `json:"addr" usage:"HTTP server listen address"`
	MongoURI        string        `json:"mongo_uri" usage:"MongoDB connection URI"`
}

func main() {
	cfg := Config{
		ShutdownTimeout: 30 * time.Second,
		QueueSize:       100,
		Workers:         4,
		Addr:            ":8080",
		MongoURI:        "mongodb://localhost:27017/efg",
	}

	goconfig.Read(&cfg)

	if cfg.QueueSize <= 0 || cfg.Workers <= 0 {
		panic("Queue size and workers must be positive")
	}

	if cfg.ShutdownTimeout <= 0 {
		panic("Shutdown timeout must be positive")
	}

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

	queueCtx, stopQueue := context.WithCancel(context.Background())
	defer stopQueue()
	queue := presence.NewQueue(queueCtx, cfg.QueueSize, cfg.Workers, presence.Events)

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.New(client, queue),
		ReadHeaderTimeout: 5 * time.Second,
	}

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	serverResult := make(chan error, 1)
	go func() {
		log.Printf("Listening on %s", cfg.Addr)
		serverResult <- server.ListenAndServe()
	}()
	select {
	case <-signalCtx.Done():
		stopSignals() // A second signal can force the process to exit.
	case err := <-serverResult:
		if !errors.Is(err, http.ErrServerClosed) {
			panic(err)
		}
	}
	log.Printf("Shutting down; waiting up to %s for pending jobs", cfg.ShutdownTimeout)
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancelShutdown()
	if err := shutdown(shutdownCtx, server, queue); err != nil {
		log.Printf("Shutdown deadline reached or shutdown failed: %v", err)
	} else {
		log.Print("Shutdown complete; all event workers finished")
	}
}

// HTTP requests wait for their jobs, so drain both using one shared deadline.
func shutdown(ctx context.Context, server *http.Server, queue *presence.Queue) error {
	queueResult := make(chan error, 1)
	go func() { queueResult <- queue.Shutdown(ctx) }()
	serverErr := server.Shutdown(ctx)
	if serverErr != nil {
		// Shutdown leaves active connections open when its deadline expires.
		server.Close()
	}
	return errors.Join(serverErr, <-queueResult)
}
