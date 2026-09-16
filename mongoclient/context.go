package mongoclient

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/mongo"
)

type mongoClientKey struct{}

func SetMongoClient(ctx context.Context, client *mongo.Client) context.Context {
	return context.WithValue(ctx, mongoClientKey{}, client)
}

func GetMongoClient(ctx context.Context) *mongo.Client {
	return ctx.Value(mongoClientKey{}).(*mongo.Client)
}
