package main

import (
	"context"
	"log"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"efg/demo"

	"github.com/fulldump/goconfig"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Config struct {
	MongoURI string        `json:"mongo_uri" usage:"MongoDB connection URI"`
	Simulate bool          `json:"simulate" usage:"Run continuous simulation instead of seeding"`
	API      string        `json:"api" usage:"Presence API URL"`
	Users    int           `json:"users" usage:"Number of synthetic players, in addition to the eight demo players"`
	Clients  int           `json:"clients" usage:"Concurrent background simulation clients"`
	Interval time.Duration `json:"interval" usage:"Minimum interval between batches per client"`
}

func main() {

	c := &Config{
		MongoURI: "mongodb://localhost:27017/efg",
		API:      "http://localhost:8080",
		Users:    100_000,
		Clients:  10,
		Interval: 100 * time.Millisecond,

		// stress the service queue (30%) with 400 clients and lower interval... 36k events/s on Intel i7 9700k
		//Clients:  400,
		//Interval: 50 * time.Millisecond,
	}

	goconfig.Read(c)

	if c.Users < 0 || c.Clients <= 0 || c.Interval <= 0 {
		log.Fatal("Users must be non-negative; clients and interval must be positive")
	}
	if c.MongoURI == "" {
		log.Fatal("MongoDB connection URI is required")
	}
	upstream, err := url.Parse(c.API)
	if c.Simulate && (err != nil || upstream.Host == "" || (upstream.Scheme != "http" && upstream.Scheme != "https")) {
		log.Fatal("Invalid presence API URL")
	}

	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(runCtx, 5*time.Minute)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(c.MongoURI))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Disconnect(context.Background())
	players := client.Database("efg").Collection("players")
	if c.Simulate {
		log.Printf("Simulating %d synthetic players with %d clients (%s interval), plus the demo players", c.Users, c.Clients, c.Interval)
		demo.Simulate(runCtx, players, c.API, c.Users, c.Clients, c.Interval)
		return
	}
	if err := demo.Seed(ctx, players); err != nil {
		log.Fatal(err)
	}
	if err := demo.SeedRandomUsers(ctx, players, c.Users); err != nil {
		log.Fatal(err)
	}
	log.Printf("Eight demo players and %d synthetic players are ready; existing players were preserved", c.Users)
}
