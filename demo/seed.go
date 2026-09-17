package demo

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var users = []struct {
	id, name, state, game string
	offlineMode           bool
}{
	{"demo-alex", "Alex Morgan", "online", "", false},
	{"demo-jamie", "Jamie Chen", "ingame", "CSGO", false},
	{"demo-sam", "Sam Rivera", "ingame", "Valorant", false},
	{"demo-jordan", "Jordan Lee", "offline", "", false},
	{"demo-riley", "Riley Brooks", "online", "", false},
	{"demo-casey", "Casey Park", "ingame", "CSGO", true},
	{"demo-morgan", "Morgan Taylor", "online", "", false},
	{"demo-taylor", "Taylor Reed", "offline", "", false},
}

// Seed inserts a small, repeatable demo without overwriting existing players.
func Seed(ctx context.Context, players *mongo.Collection) error {
	writes := make([]mongo.WriteModel, 0, len(users))
	for _, user := range users {
		friends := make([]string, 0, len(users)-1)
		for _, friend := range users {
			if friend.id != user.id {
				friends = append(friends, friend.id)
			}
		}
		document := bson.M{
			"player_id": user.id, "name": user.name, "friends": friends,
			"state": user.state, "game": user.game, "offline_mode": user.offlineMode,
			"server_id": "demo-server", "last_event_ms": time.Now().UnixMilli(),
		}
		writes = append(writes, mongo.NewUpdateOneModel().SetFilter(bson.M{"_id": user.id}).
			SetUpdate(bson.M{"$setOnInsert": document}).SetUpsert(true))
	}
	_, err := players.BulkWrite(ctx, writes)
	return err
}

// SeedRandomUsers inserts synthetic players in bounded batches. Stable IDs make
// repeated runs safe: existing states and preferences are never overwritten.
func SeedRandomUsers(ctx context.Context, players *mongo.Collection, count int) error {
	const batchSize = 1000
	for start := 0; start < count; start += batchSize {
		writes := make([]mongo.WriteModel, 0, batchSize)
		for i := start; i < min(start+batchSize, count); i++ {
			id := randomPlayerID(i)
			state := []string{"offline", "online", "ingame"}[rand.IntN(3)]
			friends := make([]string, 0, 10)
			seen := map[int]bool{i: true}
			for len(friends) < min(10, count-1) {
				friend := rand.IntN(count)
				if !seen[friend] {
					seen[friend] = true
					friends = append(friends, randomPlayerID(friend))
				}
			}
			document := bson.M{
				"player_id": id, "name": fmt.Sprintf("Player %06d", i), "friends": friends,
				"state": state, "game": randomGame(state), "offline_mode": false,
				"server_id": "demo-load", "last_event_ms": time.Now().UnixMilli(),
			}
			writes = append(writes, mongo.NewUpdateOneModel().SetFilter(bson.M{"_id": id}).
				SetUpdate(bson.M{"$setOnInsert": document}).SetUpsert(true))
		}
		if _, err := players.BulkWrite(ctx, writes, options.BulkWrite().SetOrdered(false)); err != nil {
			return err
		}
	}
	return nil
}

func randomPlayerID(i int) string { return fmt.Sprintf("load-%06d", i) }

func randomGame(state string) string {
	if state != "ingame" {
		return ""
	}
	games := []string{"CSGO", "Valorant", "League of Legends"}
	return games[rand.IntN(len(games))]
}
