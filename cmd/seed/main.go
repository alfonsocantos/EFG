package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"efg/demo"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func main() {
	uri := flag.String("mongo-uri", "mongodb://localhost:27017/efg", "MongoDB connection URI")
	simulate := flag.Bool("simulate", false, "Continuously send random demo presence events")
	api := flag.String("api", "http://localhost:8080", "Presence API URL")
	count := flag.Int("users", 100000, "Number of synthetic players, in addition to the eight demo players")
	clients := flag.Int("clients", 32, "Concurrent background simulation clients")
	interval := flag.Duration("interval", 20*time.Millisecond, "Minimum interval between batches per client")
	flag.Parse()
	if *count < 0 || *clients <= 0 || *interval <= 0 {
		log.Fatal("Users must be non-negative; clients and interval must be positive")
	}
	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(runCtx, 5*time.Minute)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(*uri))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Disconnect(context.Background())
	players := client.Database("efg").Collection("players")
	if *simulate {
		log.Printf("Simulating %d synthetic players with %d clients (%s interval), plus the demo players", *count, *clients, *interval)
		demo.Simulate(runCtx, players, *api, *count, *clients, *interval)
		return
	}
	if err := demo.Seed(ctx, players); err != nil {
		log.Fatal(err)
	}
	if err := demo.SeedRandomUsers(ctx, players, *count); err != nil {
		log.Fatal(err)
	}
	log.Printf("Eight demo players and %d synthetic players are ready; existing players were preserved", *count)
}
