package main

import (
	"context"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"

	"github.com/funinthecloud/protosource/adapters/awslambda"
	"github.com/funinthecloud/protosource/stores/dynamodbstore"
)

func main() {
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		panic(err)
	}

	client := dynamodb.NewFromConfig(cfg)

	eventsTable := dynamodbstore.EventsTableName(envOrDefault("EVENTS_TABLE", "events"))
	aggregatesTable := dynamodbstore.AggregatesTableName(envOrDefault("AGGREGATES_TABLE", "aggregates"))

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
