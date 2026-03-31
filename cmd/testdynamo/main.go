package main

import (
	"context"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"

	"github.com/funinthecloud/protosource"
	"github.com/funinthecloud/protosource/adapters/awslambda"
	testv1 "github.com/funinthecloud/protosource/example/app/test/v1"
	opaquedynamo "github.com/funinthecloud/protosource/opaquedata/dynamo"
	"github.com/funinthecloud/protosource/serializers/protobinaryserializer"
	"github.com/funinthecloud/protosource/stores/dynamodbstore"
)

func main() {
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		panic(err)
	}

	client := dynamodb.NewFromConfig(cfg)

	eventsTable := envOrDefault("EVENTS_TABLE", "events")
	aggregatesTable := envOrDefault("AGGREGATES_TABLE", "aggregates")

	opaqueStore := opaquedynamo.New(client, aggregatesTable)

	store, err := dynamodbstore.New(client,
		dynamodbstore.WithEventsTable(eventsTable),
		dynamodbstore.WithOpaqueStore(opaqueStore),
	)
	if err != nil {
		panic(err)
	}

	serializer := protobinaryserializer.NewSerializer()
	repo := testv1.NewRepository(store, serializer)

	h := testv1.NewHandler(repo)
	router := protosource.NewRouter()
	h.RegisterRoutes(router)

	handler := awslambda.WrapRouter(router, extractActor)
	lambda.Start(handler)
}

func extractActor(_ events.APIGatewayProxyRequest) string {
	return "lambda"
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
