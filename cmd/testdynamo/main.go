package main

import (
	"context"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"

	"github.com/funinthecloud/protosource/adapters/awslambda"
	testv1dynamodb "github.com/funinthecloud/protosource/example/app/test/v1/testv1dynamodb"
)

func main() {
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		panic(err)
	}

	client := dynamodb.NewFromConfig(cfg)

	eventsTable := testv1dynamodb.EventsTableName(envOrDefault("EVENTS_TABLE", "events"))
	aggregatesTable := testv1dynamodb.AggregatesTableName(envOrDefault("AGGREGATES_TABLE", "aggregates"))

	router, err := InitializeRouter(client, eventsTable, aggregatesTable)
	if err != nil {
		panic(err)
	}

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
