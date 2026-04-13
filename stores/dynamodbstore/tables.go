package dynamodbstore

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// NumGSIs is the number of GSI pairs on the aggregates table. The
// opaquedata single-table design projects every annotated field into
// one of these 20 slots.
const NumGSIs = 20

// EnsureTables idempotently creates the events and aggregates tables.
// If a table already exists it is left alone. Tables are created with
// PAY_PER_REQUEST billing, deletion protection enabled, TTL on
// attribute "t", and PITR enabled.
//
// PITR and TTL enablement are best-effort — DynamoDB Local does not
// always support them, so failures are silently ignored to keep local
// development and test runs working.
func EnsureTables(ctx context.Context, client *dynamodb.Client, eventsTable, aggregatesTable string) error {
	if err := ensureEventsTable(ctx, client, eventsTable); err != nil {
		return fmt.Errorf("ensure events table %q: %w", eventsTable, err)
	}
	if err := ensureAggregatesTable(ctx, client, aggregatesTable); err != nil {
		return fmt.Errorf("ensure aggregates table %q: %w", aggregatesTable, err)
	}
	return nil
}

func ensureEventsTable(ctx context.Context, client *dynamodb.Client, name string) error {
	exists, err := tableExists(ctx, client, name)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	_, err = client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName: aws.String(name),
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String("a"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("v"), AttributeType: types.ScalarAttributeTypeN},
		},
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String("a"), KeyType: types.KeyTypeHash},
			{AttributeName: aws.String("v"), KeyType: types.KeyTypeRange},
		},
		BillingMode:               types.BillingModePayPerRequest,
		DeletionProtectionEnabled: aws.Bool(true),
	})
	if err != nil {
		return fmt.Errorf("CreateTable: %w", err)
	}
	if err := waitActive(ctx, client, name); err != nil {
		return err
	}
	// Best-effort: DynamoDB Local may not support PITR/TTL.
	enablePITR(ctx, client, name)
	enableTTL(ctx, client, name)
	return nil
}

func ensureAggregatesTable(ctx context.Context, client *dynamodb.Client, name string) error {
	exists, err := tableExists(ctx, client, name)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	attrs := []types.AttributeDefinition{
		{AttributeName: aws.String("pk"), AttributeType: types.ScalarAttributeTypeS},
		{AttributeName: aws.String("sk"), AttributeType: types.ScalarAttributeTypeS},
	}
	var gsis []types.GlobalSecondaryIndex
	for i := 1; i <= NumGSIs; i++ {
		n := strconv.Itoa(i)
		pkAttr := "gsi" + n + "pk"
		skAttr := "gsi" + n + "sk"
		attrs = append(attrs,
			types.AttributeDefinition{AttributeName: aws.String(pkAttr), AttributeType: types.ScalarAttributeTypeS},
			types.AttributeDefinition{AttributeName: aws.String(skAttr), AttributeType: types.ScalarAttributeTypeS},
		)
		gsis = append(gsis, types.GlobalSecondaryIndex{
			IndexName: aws.String(pkAttr + "-" + skAttr + "-index"),
			KeySchema: []types.KeySchemaElement{
				{AttributeName: aws.String(pkAttr), KeyType: types.KeyTypeHash},
				{AttributeName: aws.String(skAttr), KeyType: types.KeyTypeRange},
			},
			Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll},
		})
	}

	_, err = client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName:              aws.String(name),
		AttributeDefinitions:   attrs,
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String("pk"), KeyType: types.KeyTypeHash},
			{AttributeName: aws.String("sk"), KeyType: types.KeyTypeRange},
		},
		GlobalSecondaryIndexes: gsis,
		BillingMode:            types.BillingModePayPerRequest,
		DeletionProtectionEnabled: aws.Bool(true),
	})
	if err != nil {
		return fmt.Errorf("CreateTable: %w", err)
	}
	if err := waitActive(ctx, client, name); err != nil {
		return err
	}
	enablePITR(ctx, client, name)
	enableTTL(ctx, client, name)
	return nil
}

func tableExists(ctx context.Context, client *dynamodb.Client, name string) (bool, error) {
	_, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(name)})
	if err == nil {
		return true, nil
	}
	var nf *types.ResourceNotFoundException
	if errors.As(err, &nf) {
		return false, nil
	}
	return false, err
}

func waitActive(ctx context.Context, client *dynamodb.Client, name string) error {
	waiter := dynamodb.NewTableExistsWaiter(client)
	return waiter.Wait(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(name)}, 2*time.Minute)
}

func enablePITR(ctx context.Context, client *dynamodb.Client, name string) {
	_, _ = client.UpdateContinuousBackups(ctx, &dynamodb.UpdateContinuousBackupsInput{
		TableName: aws.String(name),
		PointInTimeRecoverySpecification: &types.PointInTimeRecoverySpecification{
			PointInTimeRecoveryEnabled: aws.Bool(true),
		},
	})
}

func enableTTL(ctx context.Context, client *dynamodb.Client, name string) {
	_, _ = client.UpdateTimeToLive(ctx, &dynamodb.UpdateTimeToLiveInput{
		TableName: aws.String(name),
		TimeToLiveSpecification: &types.TimeToLiveSpecification{
			AttributeName: aws.String("t"),
			Enabled:       aws.Bool(true),
		},
	})
}
