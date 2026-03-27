package main

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const usage = `Usage: testdynamo-setup <command>

Commands:
  create    Create the DynamoDB tables (events + aggregates)
  delete    Delete the DynamoDB tables
  status    Check if the tables exist and show their status

Environment variables:
  EVENTS_TABLE       Events table name        (default: events)
  AGGREGATES_TABLE   Aggregates table name    (default: aggregates)
  AWS_ENDPOINT_URL   Custom endpoint          (e.g. http://localhost:8000 for DynamoDB Local)`

const gsiCount = 20

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(1)
	}

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		fatal("loading AWS config: %v", err)
	}

	var opts []func(*dynamodb.Options)
	if endpoint := os.Getenv("AWS_ENDPOINT_URL"); endpoint != "" {
		opts = append(opts, func(o *dynamodb.Options) {
			o.BaseEndpoint = &endpoint
		})
	}
	client := dynamodb.NewFromConfig(cfg, opts...)

	eventsTable := envOrDefault("EVENTS_TABLE", "events")
	aggregatesTable := envOrDefault("AGGREGATES_TABLE", "aggregates")

	switch os.Args[1] {
	case "create":
		createEventsTable(ctx, client, eventsTable)
		createAggregatesTable(ctx, client, aggregatesTable)
	case "delete":
		for _, table := range []string{eventsTable, aggregatesTable} {
			deleteTable(ctx, client, table)
		}
	case "status":
		for _, table := range []string{eventsTable, aggregatesTable} {
			describeTable(ctx, client, table)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n%s\n", os.Args[1], usage)
		os.Exit(1)
	}
}

// createEventsTable creates the events table with partition key "a" (S) and sort key "v" (N).
func createEventsTable(ctx context.Context, client *dynamodb.Client, tableName string) {
	_, err := client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName: &tableName,
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String("a"), KeyType: types.KeyTypeHash},
			{AttributeName: aws.String("v"), KeyType: types.KeyTypeRange},
		},
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String("a"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("v"), AttributeType: types.ScalarAttributeTypeN},
		},
		BillingMode: types.BillingModePayPerRequest,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %s: error: %v\n", tableName, err)
		return
	}
	fmt.Printf("  %s: created (a/v)\n", tableName)
}

// createAggregatesTable creates the aggregates table with pk/sk (S/S)
// and all 20 GSIs following the gsiNpk/gsiNsk naming convention.
func createAggregatesTable(ctx context.Context, client *dynamodb.Client, tableName string) {
	attrDefs := []types.AttributeDefinition{
		{AttributeName: aws.String("pk"), AttributeType: types.ScalarAttributeTypeS},
		{AttributeName: aws.String("sk"), AttributeType: types.ScalarAttributeTypeS},
	}

	var gsis []types.GlobalSecondaryIndex
	for i := 1; i <= gsiCount; i++ {
		pkAttr := fmt.Sprintf("gsi%dpk", i)
		skAttr := fmt.Sprintf("gsi%dsk", i)
		indexName := fmt.Sprintf("%s-%s-index", pkAttr, skAttr)

		attrDefs = append(attrDefs,
			types.AttributeDefinition{AttributeName: aws.String(pkAttr), AttributeType: types.ScalarAttributeTypeS},
			types.AttributeDefinition{AttributeName: aws.String(skAttr), AttributeType: types.ScalarAttributeTypeS},
		)

		gsis = append(gsis, types.GlobalSecondaryIndex{
			IndexName: aws.String(indexName),
			KeySchema: []types.KeySchemaElement{
				{AttributeName: aws.String(pkAttr), KeyType: types.KeyTypeHash},
				{AttributeName: aws.String(skAttr), KeyType: types.KeyTypeRange},
			},
			Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll},
		})
	}

	_, err := client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName: &tableName,
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String("pk"), KeyType: types.KeyTypeHash},
			{AttributeName: aws.String("sk"), KeyType: types.KeyTypeRange},
		},
		AttributeDefinitions:   attrDefs,
		BillingMode:            types.BillingModePayPerRequest,
		GlobalSecondaryIndexes: gsis,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %s: error: %v\n", tableName, err)
		return
	}
	fmt.Printf("  %s: created (pk/sk + %d GSIs)\n", tableName, gsiCount)
}

func deleteTable(ctx context.Context, client *dynamodb.Client, tableName string) {
	_, err := client.DeleteTable(ctx, &dynamodb.DeleteTableInput{
		TableName: &tableName,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %s: error: %v\n", tableName, err)
		return
	}
	fmt.Printf("  %s: deleted\n", tableName)
}

func describeTable(ctx context.Context, client *dynamodb.Client, tableName string) {
	resp, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: &tableName,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %s: not found or error: %v\n", tableName, err)
		return
	}
	t := resp.Table
	gsiInfo := ""
	if len(t.GlobalSecondaryIndexes) > 0 {
		gsiInfo = fmt.Sprintf(" gsis=%d", len(t.GlobalSecondaryIndexes))
	}
	fmt.Printf("  %s: status=%s items=%d%s\n", tableName, t.TableStatus, aws.ToInt64(t.ItemCount), gsiInfo)
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
