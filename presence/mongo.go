package presence

import (
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// playerUpsert inserts a new player or updates an existing one if the event is newer.
// MongoDB evaluates the comparison and the update atomically for each player.
func playerUpsert(serverID, serverSession string, event Event) *mongo.UpdateOneModel {
	fields := bson.M{
		"player_id":      event.PlayerID,
		"server_id":      serverID,
		"server_session": serverSession,
		"assignment_id":  event.AssignmentID,
		"state":          event.State,
		"last_event_ms":  event.OccurredAtMS,
	}

	// A new player has no timestamp. Valid event timestamps are always positive.
	lastTimestamp := bson.M{"$ifNull": bson.A{"$last_event_ms", 0}}
	isNewer := bson.M{"$lt": bson.A{lastTimestamp, event.OccurredAtMS}}

	// Preserve unrelated player fields. $literal keeps IDs starting with "$" as data.
	updatedPlayer := bson.M{"$mergeObjects": bson.A{"$$ROOT", bson.M{"$literal": fields}}}
	keepNewest := bson.M{"$cond": bson.A{isNewer, updatedPlayer, "$$ROOT"}}
	update := mongo.Pipeline{
		bson.D{{Key: "$replaceWith", Value: keepNewest}},
	}

	// Match only the unique player ID; timestamps must not be part of the upsert filter.
	return mongo.NewUpdateOneModel().
		SetFilter(bson.M{"_id": event.PlayerID}).
		SetUpdate(update).
		SetUpsert(true)
}
