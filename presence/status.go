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

var ErrMissingPlayerID = errors.New("X-Player-ID header is required")
var ErrInvalidStatus = errors.New("offline_mode must be a boolean")

type StatusRequest struct {
	OfflineMode *bool `json:"offline_mode"`
}

type StatusResponse struct {
	State       string `json:"state" bson:"state"`
	OfflineMode bool   `json:"offline_mode" bson:"offline_mode"`
}

// SetStatus changes the visibility preference without changing matchmaking state.
func SetStatus(ctx context.Context, r *http.Request, request StatusRequest) error {
	playerID := strings.TrimSpace(r.Header.Get("X-Player-ID"))
	if playerID == "" {
		return ErrMissingPlayerID
	}
	if request.OfflineMode == nil {
		return ErrInvalidStatus
	}
	players := mongoclient.GetMongoClient(ctx).Database("efg").Collection("players")
	_, err := players.UpdateOne(ctx, bson.M{"_id": playerID}, bson.M{
		"$set":         bson.M{"offline_mode": *request.OfflineMode},
		"$setOnInsert": bson.M{"player_id": playerID},
	}, options.UpdateOne().SetUpsert(true))
	return err
}

// GetStatus returns the visible state, honoring the player's offline preference.
func GetStatus(ctx context.Context, r *http.Request) (StatusResponse, error) {
	playerID := strings.TrimSpace(r.Header.Get("X-Player-ID"))
	if playerID == "" {
		return StatusResponse{}, ErrMissingPlayerID
	}
	status := StatusResponse{State: "offline"}
	players := mongoclient.GetMongoClient(ctx).Database("efg").Collection("players")
	err := players.FindOne(ctx, bson.M{"_id": playerID}).Decode(&status)
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return StatusResponse{}, err
	}
	status.State = visibleState(status.State, status.OfflineMode)
	return status, nil
}

func visibleState(state string, offlineMode bool) string {
	if offlineMode || state == "" {
		return "offline"
	}
	return state
}
