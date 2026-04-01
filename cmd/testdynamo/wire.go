//go:build wireinject

package main

import (
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/google/wire"

	"github.com/funinthecloud/protosource"
	orderv1 "github.com/funinthecloud/protosource/example/app/order/v1"
	samplev1 "github.com/funinthecloud/protosource/example/app/sample/v1"
	testv1 "github.com/funinthecloud/protosource/example/app/test/v1"
	testv1dynamodb "github.com/funinthecloud/protosource/example/app/test/v1/testv1dynamodb"
	opaquedynamo "github.com/funinthecloud/protosource/opaquedata/dynamo"
	"github.com/funinthecloud/protosource/serializers/protobinaryserializer"
	"github.com/funinthecloud/protosource/stores/dynamodbstore"
)

// provideOpaqueStore creates the shared opaque store for materialized aggregates.
func provideOpaqueStore(client *dynamodb.Client, table testv1dynamodb.AggregatesTableName) *opaquedynamo.Store {
	return opaquedynamo.New(client, string(table))
}

// provideStore creates the shared DynamoDB event store.
func provideStore(client *dynamodb.Client, opaqueStore *opaquedynamo.Store, table testv1dynamodb.EventsTableName) (*dynamodbstore.DynamoDBStore, error) {
	return dynamodbstore.New(client,
		dynamodbstore.WithEventsTable(string(table)),
		dynamodbstore.WithOpaqueStore(opaqueStore),
	)
}

// provideRouter creates a router with all aggregate handlers registered.
func provideRouter(
	testHandler *testv1.Handler,
	orderHandler *orderv1.Handler,
	sampleHandler *samplev1.Handler,
) *protosource.Router {
	router := protosource.NewRouter()
	testHandler.RegisterRoutes(router)
	orderHandler.RegisterRoutes(router)
	sampleHandler.RegisterRoutes(router)
	return router
}

// provideTestRepository creates the test aggregate repository.
func provideTestRepository(store *dynamodbstore.DynamoDBStore, serializer *protobinaryserializer.Serializer) testv1.Repo {
	return testv1.NewRepository(store, serializer)
}

// provideOrderRepository creates the order aggregate repository.
func provideOrderRepository(store *dynamodbstore.DynamoDBStore, serializer *protobinaryserializer.Serializer) orderv1.Repo {
	return orderv1.NewRepository(store, serializer)
}

// provideSampleRepository creates the sample aggregate repository.
func provideSampleRepository(store *dynamodbstore.DynamoDBStore, serializer *protobinaryserializer.Serializer) samplev1.Repo {
	return samplev1.NewRepository(store, serializer)
}

// InitializeRouter wires all dependencies and returns a configured router.
func InitializeRouter(
	client *dynamodb.Client,
	eventsTable testv1dynamodb.EventsTableName,
	aggregatesTable testv1dynamodb.AggregatesTableName,
) (*protosource.Router, error) {
	wire.Build(
		provideOpaqueStore,
		provideStore,
		protobinaryserializer.ProviderSet,
		provideTestRepository,
		provideOrderRepository,
		provideSampleRepository,
		testv1.NewHandler,
		orderv1.NewHandler,
		samplev1.NewHandler,
		provideRouter,
	)
	return nil, nil
}
