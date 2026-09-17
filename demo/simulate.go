package demo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"

	"efg/presence"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// nextState models normal activity, with a 10% chance of disconnecting.
// Unknown states from older demo data also fall back to offline.
func nextState(state string, roll int) string {
	if roll < 10 {
		return "offline"
	}
	switch state {
	case "offline":
		return "online"
	case "online":
		if roll < 40 {
			return "offline"
		}
		return "ingame"
	case "ingame":
		return "online"
	default:
		return "offline"
	}
}

// Simulate keeps visible demo users active independently of background load.
// Each load client owns a disjoint set of players to preserve state transitions.
func Simulate(ctx context.Context, players *mongo.Collection, apiURL string, count, clients int, interval time.Duration) {
	// Keep a reusable connection for each background client and the demo loop.
	connections := min(clients, count) + 1
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = connections
	transport.MaxIdleConnsPerHost = connections
	transport.MaxConnsPerHost = connections
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	var workers sync.WaitGroup
	run := func(period time.Duration, ids func() []string, visible bool) {
		defer workers.Done()
		ticker := time.NewTicker(period)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				stepCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				err := simulateBatch(stepCtx, players, client, apiURL, ids(), visible)
				cancel()
				if err != nil && ctx.Err() == nil {
					log.Printf("Presence simulation failed: %v", err)
				}
			}
		}
	}
	workers.Add(1)
	go run(500*time.Millisecond, func() []string {
		return []string{users[rand.IntN(len(users))].id}
	}, true)
	for worker := 0; worker < min(clients, count); worker++ {
		workers.Add(1)
		go run(interval, func() []string { return loadPlayerIDs(worker, clients, count) }, false)
	}
	workers.Wait()
}

// A batch has up to ten distinct players, all belonging to this client.
func loadPlayerIDs(worker, clients, count int) []string {
	owned := (count-1-worker)/clients + 1
	ids := make([]string, 0, min(10, owned))
	seen := make(map[int]bool)
	for len(ids) < min(10, owned) {
		index := worker + rand.IntN(owned)*clients
		if !seen[index] {
			seen[index] = true
			ids = append(ids, randomPlayerID(index))
		}
	}
	return ids
}

func simulateBatch(ctx context.Context, players *mongo.Collection, client *http.Client, apiURL string, ids []string, visible bool) error {
	cursor, err := players.Find(ctx, bson.M{"_id": bson.M{"$in": ids}}, options.Find().SetProjection(bson.M{"state": 1}))
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	var stored []struct {
		ID    string `bson:"_id"`
		State string `bson:"state"`
	}
	if err := cursor.All(ctx, &stored); err != nil {
		return err
	}
	if len(stored) != len(ids) {
		return fmt.Errorf("missing simulation players; run the seed first")
	}
	batch := presence.EventsRequest{ServerID: "demo-server", Events: make([]presence.Event, 0, len(stored))}
	for _, player := range stored {
		state := nextState(player.State, rand.IntN(100))
		batch.Events = append(batch.Events, presence.Event{
			PlayerID: player.ID, State: state, Game: randomGame(state), OccurredAtMS: time.Now().UnixMilli(),
		})
	}
	body, err := json.Marshal(batch)
	if err != nil {
		return err
	}
	// Retry the same payload and timestamp when the server applies backpressure.
	for attempt := 0; attempt < 3; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(apiURL, "/")+"/events", bytes.NewReader(body))
		if err != nil {
			return err
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := client.Do(request)
		if err != nil {
			return err
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			break
		}
		if response.StatusCode != http.StatusServiceUnavailable || attempt == 2 {
			return fmt.Errorf("events API returned %s", response.Status)
		}
		// The demo API sends Retry-After: 1. Add jitter to stagger clients.
		timer := time.NewTimer(time.Second + time.Duration(rand.IntN(250))*time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	if visible {
		event := batch.Events[0]
		log.Printf("%s: %s -> %s %s", event.PlayerID, stored[0].State, event.State, event.Game)
	}
	return nil
}
