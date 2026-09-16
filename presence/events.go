package presence

import (
	"context"
	"errors"
	"fmt"

	"efg/mongoclient"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type EventsRequest struct {
	ServerID      string  `json:"server_id"`
	ServerSession string  `json:"server_session"`
	Events        []Event `json:"events"`
}

type Event struct {
	PlayerID     string `json:"player_id" bson:"player_id"`
	AssignmentID string `json:"assignment_id" bson:"assignment_id"`
	State        string `json:"state" bson:"state"`
	OccurredAtMS int64  `json:"occurred_at_ms" bson:"occurred_at_ms"`
}

var ErrInvalidEvents = errors.New("invalid presence events")

// Events stores the newest presence for each player in the batch.
func Events(ctx context.Context, request EventsRequest) error {

	if request.ServerID == "" || request.ServerSession == "" || len(request.Events) == 0 {
		return fmt.Errorf("%w: requires server_id, server_session and events", ErrInvalidEvents)
	}

	for _, event := range request.Events {
		if event.PlayerID == "" || event.AssignmentID == "" || event.OccurredAtMS <= 0 {
			return fmt.Errorf("%w: requires player_id, assignment_id and a positive occurred_at_ms", ErrInvalidEvents)
		}
	}

	writes := make([]mongo.WriteModel, 0, len(request.Events))
	for _, event := range request.Events {
		writes = append(writes, playerUpsert(request.ServerID, request.ServerSession, event))
	}

	players := mongoclient.GetMongoClient(ctx).Database("efg").Collection("players")
	if _, err := players.BulkWrite(ctx, writes, options.BulkWrite().SetOrdered(false)); err != nil {
		return fmt.Errorf("persistence error: %w", err)
	}

	return nil
}
