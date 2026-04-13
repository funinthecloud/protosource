package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/funinthecloud/protosource/stores/dynamodbstore"
)

const usage = `Usage: testdynamo-setup <command>

Commands:
  create              Create the DynamoDB tables (events + aggregates)
  fix                 Enable TTL and PITR on existing tables (idempotent)
  delete              Delete the DynamoDB tables (requires disable-protection first)
  disable-protection  Disable deletion protection on both tables
  status              Check table status, TTL, PITR, and deletion protection

Environment variables:
  EVENTS_TABLE       Events table name        (default: events)
  AGGREGATES_TABLE   Aggregates table name    (default: aggregates)
  AWS_ENDPOINT_URL   Custom endpoint          (e.g. http://localhost:8000 for DynamoDB Local)`

const gsiCount = dynamodbstore.NumGSIs

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
		if err := dynamodbstore.EnsureTables(ctx, client, eventsTable, aggregatesTable); err != nil {
			fatal("create: %v", err)
		}
		fmt.Printf("  tables created: %s, %s\n", eventsTable, aggregatesTable)
	case "fix":
		for _, table := range []string{eventsTable, aggregatesTable} {
			enablePITR(ctx, client, table)
			enableTTL(ctx, client, table, "t")
		}
	case "delete":
		for _, table := range []string{eventsTable, aggregatesTable} {
			deleteTable(ctx, client, table)
		}
	case "disable-protection":
		for _, table := range []string{eventsTable, aggregatesTable} {
			disableProtection(ctx, client, table)
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
		BillingMode:               types.BillingModePayPerRequest,
		DeletionProtectionEnabled: aws.Bool(true),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %s: error: %v\n", tableName, err)
		return
	}
	fmt.Printf("  %s: created (a/v, deletion protection enabled)\n", tableName)
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
		AttributeDefinitions:      attrDefs,
		BillingMode:               types.BillingModePayPerRequest,
		GlobalSecondaryIndexes:    gsis,
		DeletionProtectionEnabled: aws.Bool(true),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %s: error: %v\n", tableName, err)
		return
	}
	fmt.Printf("  %s: created (pk/sk + %d GSIs, deletion protection enabled)\n", tableName, gsiCount)
}

// waitForActive polls until all tables are ACTIVE.
func waitForActive(ctx context.Context, client *dynamodb.Client, tables ...string) {
	waiter := dynamodb.NewTableExistsWaiter(client)
	for _, table := range tables {
		fmt.Printf("  %s: waiting for ACTIVE...\n", table)
		err := waiter.Wait(ctx, &dynamodb.DescribeTableInput{
			TableName: &table,
		}, 2*time.Minute)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s: waiter error: %v\n", table, err)
		}
	}
}

// enablePITR enables point-in-time recovery on a table.
func enablePITR(ctx context.Context, client *dynamodb.Client, tableName string) {
	_, err := client.UpdateContinuousBackups(ctx, &dynamodb.UpdateContinuousBackupsInput{
		TableName: &tableName,
		PointInTimeRecoverySpecification: &types.PointInTimeRecoverySpecification{
			PointInTimeRecoveryEnabled: aws.Bool(true),
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %s: PITR error: %v\n", tableName, err)
		return
	}
	fmt.Printf("  %s: PITR enabled\n", tableName)
}

// enableTTL enables TTL on a table for the given attribute.
func enableTTL(ctx context.Context, client *dynamodb.Client, tableName, attr string) {
	_, err := client.UpdateTimeToLive(ctx, &dynamodb.UpdateTimeToLiveInput{
		TableName: &tableName,
		TimeToLiveSpecification: &types.TimeToLiveSpecification{
			Enabled:       aws.Bool(true),
			AttributeName: &attr,
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %s: TTL error: %v\n", tableName, err)
		return
	}
	fmt.Printf("  %s: TTL enabled (attribute: %s)\n", tableName, attr)
}

// disableProtection disables deletion protection on a table.
func disableProtection(ctx context.Context, client *dynamodb.Client, tableName string) {
	_, err := client.UpdateTable(ctx, &dynamodb.UpdateTableInput{
		TableName:                 &tableName,
		DeletionProtectionEnabled: aws.Bool(false),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %s: error: %v\n", tableName, err)
		return
	}
	fmt.Printf("  %s: deletion protection disabled\n", tableName)
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
	tbl := resp.Table
	gsiInfo := ""
	if len(tbl.GlobalSecondaryIndexes) > 0 {
		gsiInfo = fmt.Sprintf(" gsis=%d", len(tbl.GlobalSecondaryIndexes))
	}
	protection := "off"
	if tbl.DeletionProtectionEnabled != nil && *tbl.DeletionProtectionEnabled {
		protection = "on"
	}
	fmt.Printf("  %s: status=%s items=%d%s deletion_protection=%s\n",
		tableName, tbl.TableStatus, aws.ToInt64(tbl.ItemCount), gsiInfo, protection)

	if protection == "off" {
		fmt.Fprintf(os.Stderr, "  %s: WARNING: deletion protection is disabled\n", tableName)
	}

	// TTL
	ttlResp, err := client.DescribeTimeToLive(ctx, &dynamodb.DescribeTimeToLiveInput{
		TableName: &tableName,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %s: TTL status error: %v\n", tableName, err)
	} else {
		ttl := ttlResp.TimeToLiveDescription
		fmt.Printf("  %s: ttl=%s", tableName, ttl.TimeToLiveStatus)
		if ttl.AttributeName != nil {
			fmt.Printf(" (attribute: %s)", *ttl.AttributeName)
		}
		fmt.Println()
		if ttl.TimeToLiveStatus == types.TimeToLiveStatusDisabled || ttl.TimeToLiveStatus == types.TimeToLiveStatusDisabling {
			fmt.Fprintf(os.Stderr, "  %s: WARNING: TTL is not enabled (expected attribute \"t\"); run 'fix' to enable\n", tableName)
		}
	}

	// PITR
	backupResp, err := client.DescribeContinuousBackups(ctx, &dynamodb.DescribeContinuousBackupsInput{
		TableName: &tableName,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %s: PITR status error: %v\n", tableName, err)
	} else {
		pitr := backupResp.ContinuousBackupsDescription.PointInTimeRecoveryDescription
		if pitr != nil {
			fmt.Printf("  %s: pitr=%s", tableName, pitr.PointInTimeRecoveryStatus)
			if pitr.EarliestRestorableDateTime != nil {
				fmt.Printf(" (earliest: %s)", pitr.EarliestRestorableDateTime.Format(time.RFC3339))
			}
			if pitr.LatestRestorableDateTime != nil {
				fmt.Printf(" (latest: %s)", pitr.LatestRestorableDateTime.Format(time.RFC3339))
			}
			fmt.Println()
			if pitr.PointInTimeRecoveryStatus == types.PointInTimeRecoveryStatusDisabled {
				fmt.Fprintf(os.Stderr, "  %s: WARNING: PITR is not enabled; run 'fix' to enable\n", tableName)
			}
		}
	}
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
