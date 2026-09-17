package presence

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"efg/mongoclient"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type FriendPresence struct {
	PlayerID string `json:"player_id"`
	State    string `json:"state"`
	Game     string `json:"game,omitempty"`
}

func FriendsPresence(ctx context.Context, r *http.Request) ([]FriendPresence, error) {

	playerID := strings.TrimSpace(r.Header.Get("X-Player-ID"))
	if playerID == "" {
		return nil, ErrMissingPlayerID
	}

	players := mongoclient.GetMongoClient(ctx).Database("efg").Collection("players")
	var player struct {
		Friends []string `bson:"friends"`
	}

	err := players.FindOne(ctx, bson.M{"_id": playerID},
		options.FindOne().SetProjection(bson.M{"friends": 1})).Decode(&player)
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, err
	}

	result := make([]FriendPresence, 0, len(player.Friends))
	if len(player.Friends) == 0 {
		return result, nil
	}

	// Fetch every friend's presence in one query instead of one query per friend.
	cursor, err := players.Find(ctx, bson.M{"_id": bson.M{"$in": player.Friends}},
		options.Find().SetProjection(bson.M{"state": 1, "game": 1, "offline_mode": 1}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var friends []struct {
		ID          string `bson:"_id"`
		State       string `bson:"state"`
		Game        string `bson:"game"`
		OfflineMode bool   `bson:"offline_mode"`
	}
	if err := cursor.All(ctx, &friends); err != nil {
		return nil, err
	}

	byID := make(map[string]FriendPresence, len(friends))
	for _, friend := range friends {
		state := visibleState(friend.State, friend.OfflineMode)
		presence := FriendPresence{PlayerID: friend.ID, State: state}
		if state != "offline" {
			presence.Game = friend.Game
		}
		byID[friend.ID] = presence
	}

	// Preserve the friend list order; friends without presence appear offline.
	for _, id := range player.Friends {
		friend, exists := byID[id]
		if !exists {
			friend = FriendPresence{PlayerID: id, State: "offline"}
		}
		result = append(result, friend)
	}
	return result, nil
}
