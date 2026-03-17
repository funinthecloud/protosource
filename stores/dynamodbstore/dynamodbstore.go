package dynamodbstore

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	historyv1 "github.com/funinthecloud/protosource/history/v1"
	recordv1 "github.com/funinthecloud/protosource/record/v1"
)

// Dynamoer is a minimal interface covering the DynamoDB operations needed by
// the store. It is satisfied by *dynamodb.Client.
type Dynamoer interface {
	Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	TransactWriteItems(ctx context.Context, params *dynamodb.TransactWriteItemsInput, optFns ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error)
	PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
}

// DynamoDB attribute names are kept to single characters to minimize read/write
// costs. DynamoDB charges per byte for both reads and writes, and attribute
// names are included in every item's stored and transferred size. At scale
// (millions of events), the savings from "a" vs "aggregate_id" are material.
const (
	attrPartitionKey = "a" // partition key (aggregate ID)
	attrSortKey      = "v" // sort key (version number)
	attrData         = "d" // event/aggregate payload
	attrTTL          = "t" // TTL epoch seconds (optional)

	DefaultEventsTable     = "events"
	DefaultAggregatesTable = "aggregates"
	maxTransactItems       = 100
)

// DynamoDBStore implements the protosource Store, AggregateStore, and
// SnapshotTailStore interfaces backed by DynamoDB.
type DynamoDBStore struct {
	client          Dynamoer
	eventsTable     string
	aggregatesTable string
	tenantPrefix    string
	ttl             time.Duration // when non-zero, sets TTL attribute on event writes
}

// New creates a new DynamoDBStore. The client must not be nil.
func New(client Dynamoer, opts ...Option) (*DynamoDBStore, error) {
	s := &DynamoDBStore{
		client:          client,
		eventsTable:     DefaultEventsTable,
		aggregatesTable: DefaultAggregatesTable,
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.client == nil {
		return nil, fmt.Errorf("dynamodbstore.New: client must not be nil")
	}
	return s, nil
}

// Option configures a DynamoDBStore.
type Option func(*DynamoDBStore)

// WithEventsTable sets the DynamoDB table name used for event storage.
func WithEventsTable(name string) Option {
	return func(s *DynamoDBStore) { s.eventsTable = name }
}

// WithAggregatesTable sets the DynamoDB table name used for aggregate state.
func WithAggregatesTable(name string) Option {
	return func(s *DynamoDBStore) { s.aggregatesTable = name }
}

// WithTenantPrefix prepends "prefix#" to all aggregate IDs for multi-tenant
// table sharing.
func WithTenantPrefix(prefix string) Option {
	return func(s *DynamoDBStore) { s.tenantPrefix = prefix }
}

// WithTTL sets a time-to-live duration for event records. When set, each saved
// event includes a TTL attribute ("t") containing the Unix epoch second at
// which the record should expire. The DynamoDB table must have TTL enabled on
// the "t" attribute for automatic deletion to occur.
//
// A zero or negative duration disables TTL (the default).
func WithTTL(ttl time.Duration) Option {
	return func(s *DynamoDBStore) { s.ttl = ttl }
}

// Save stores records for the given aggregate ID using TransactWriteItems for
// atomicity. If there are more than 100 records, they are written in batches
// of 100 (DynamoDB's per-transaction limit). Each record uses a condition
// expression to prevent duplicate versions.
//
// Saving zero records is a no-op.
func (s *DynamoDBStore) Save(ctx context.Context, aggregateID string, records ...*recordv1.Record) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("dynamodbstore.Save: %w", err)
	}
	if len(records) == 0 {
		return nil
	}

	key := s.makeKey(aggregateID)

	// Batch into groups of maxTransactItems.
	for i := 0; i < len(records); i += maxTransactItems {
		end := i + maxTransactItems
		if end > len(records) {
			end = len(records)
		}
		batch := records[i:end]

		items := make([]types.TransactWriteItem, len(batch))
		for j, rec := range batch {
			item := map[string]types.AttributeValue{
				attrPartitionKey: &types.AttributeValueMemberS{Value: key},
				attrSortKey:      &types.AttributeValueMemberN{Value: strconv.FormatInt(rec.GetVersion(), 10)},
				attrData:         &types.AttributeValueMemberB{Value: rec.GetData()},
			}
			if s.ttl > 0 {
				expiry := time.Now().Add(s.ttl).Unix()
				item[attrTTL] = &types.AttributeValueMemberN{Value: strconv.FormatInt(expiry, 10)}
			}
			items[j] = types.TransactWriteItem{
				Put: &types.Put{
					TableName:           &s.eventsTable,
					Item:                item,
					ConditionExpression: aws.String("attribute_not_exists(a) AND attribute_not_exists(v)"),
				},
			}
		}

		if _, err := s.client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
			TransactItems: items,
		}); err != nil {
			return fmt.Errorf("dynamodbstore.Save: %w", err)
		}
	}

	return nil
}

// Load retrieves the full event history for the given aggregate ID in ascending
// version order. Paginates automatically if DynamoDB returns partial results.
func (s *DynamoDBStore) Load(ctx context.Context, aggregateID string) (*historyv1.History, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("dynamodbstore.Load: %w", err)
	}

	key := s.makeKey(aggregateID)
	history := &historyv1.History{}

	var exclusiveStartKey map[string]types.AttributeValue
	for {
		input := &dynamodb.QueryInput{
			TableName:              &s.eventsTable,
			ConsistentRead:         aws.Bool(true),
			ScanIndexForward:       aws.Bool(true),
			KeyConditionExpression: aws.String("a = :id"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":id": &types.AttributeValueMemberS{Value: key},
			},
		}
		if exclusiveStartKey != nil {
			input.ExclusiveStartKey = exclusiveStartKey
		}

		resp, err := s.client.Query(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("dynamodbstore.Load: %w", err)
		}

		for _, item := range resp.Items {
			rec, err := itemToRecord(item)
			if err != nil {
				return nil, fmt.Errorf("dynamodbstore.Load: %w", err)
			}
			history.Records = append(history.Records, rec)
		}

		if resp.LastEvaluatedKey == nil {
			break
		}
		exclusiveStartKey = resp.LastEvaluatedKey
	}

	return history, nil
}

// LoadTail returns the last n events for the given aggregate, ordered by
// version ascending. It queries DynamoDB in descending order with a limit,
// then reverses the results.
func (s *DynamoDBStore) LoadTail(ctx context.Context, aggregateID string, n int) (*historyv1.History, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("dynamodbstore.LoadTail: %w", err)
	}

	key := s.makeKey(aggregateID)
	input := &dynamodb.QueryInput{
		TableName:              &s.eventsTable,
		ConsistentRead:         aws.Bool(true),
		ScanIndexForward:       aws.Bool(false),
		Limit:                  aws.Int32(int32(n)),
		KeyConditionExpression: aws.String("a = :id"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":id": &types.AttributeValueMemberS{Value: key},
		},
	}

	resp, err := s.client.Query(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("dynamodbstore.LoadTail: %w", err)
	}

	history := &historyv1.History{}
	for _, item := range resp.Items {
		rec, err := itemToRecord(item)
		if err != nil {
			return nil, fmt.Errorf("dynamodbstore.LoadTail: %w", err)
		}
		history.Records = append(history.Records, rec)
	}

	// Reverse to ascending version order.
	for i, j := 0, len(history.Records)-1; i < j; i, j = i+1, j-1 {
		history.Records[i], history.Records[j] = history.Records[j], history.Records[i]
	}

	return history, nil
}

// SaveAggregate persists the serialized aggregate state and its current version
// to the aggregates table.
func (s *DynamoDBStore) SaveAggregate(ctx context.Context, aggregateID string, data []byte, version int64) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("dynamodbstore.SaveAggregate: %w", err)
	}

	key := s.makeKey(aggregateID)
	_, err := s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &s.aggregatesTable,
		Item: map[string]types.AttributeValue{
			attrPartitionKey: &types.AttributeValueMemberS{Value: key},
			attrSortKey:      &types.AttributeValueMemberN{Value: strconv.FormatInt(version, 10)},
			attrData:         &types.AttributeValueMemberB{Value: data},
		},
	})
	if err != nil {
		return fmt.Errorf("dynamodbstore.SaveAggregate: %w", err)
	}
	return nil
}

// LoadAggregate retrieves the most recently saved aggregate state. Returns nil
// data with version 0 and no error if the aggregate has not been saved.
func (s *DynamoDBStore) LoadAggregate(ctx context.Context, aggregateID string) ([]byte, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, fmt.Errorf("dynamodbstore.LoadAggregate: %w", err)
	}

	key := s.makeKey(aggregateID)
	resp, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      &s.aggregatesTable,
		ConsistentRead: aws.Bool(true),
		Key: map[string]types.AttributeValue{
			attrPartitionKey: &types.AttributeValueMemberS{Value: key},
		},
	})
	if err != nil {
		return nil, 0, fmt.Errorf("dynamodbstore.LoadAggregate: %w", err)
	}

	if resp.Item == nil {
		return nil, 0, nil
	}

	versionStr, ok := resp.Item[attrSortKey].(*types.AttributeValueMemberN)
	if !ok {
		return nil, 0, fmt.Errorf("dynamodbstore.LoadAggregate: version is not a number")
	}
	version, err := strconv.ParseInt(versionStr.Value, 10, 64)
	if err != nil {
		return nil, 0, fmt.Errorf("dynamodbstore.LoadAggregate: failed to parse version: %w", err)
	}

	dataVal, ok := resp.Item[attrData].(*types.AttributeValueMemberB)
	if !ok {
		return nil, 0, fmt.Errorf("dynamodbstore.LoadAggregate: data is not binary")
	}

	return dataVal.Value, version, nil
}

// makeKey returns the DynamoDB partition key for the given aggregate ID,
// prepending the tenant prefix if configured.
func (s *DynamoDBStore) makeKey(aggregateID string) string {
	if s.tenantPrefix != "" {
		return s.tenantPrefix + "#" + aggregateID
	}
	return aggregateID
}

// itemToRecord converts a DynamoDB item into a recordv1.Record.
func itemToRecord(item map[string]types.AttributeValue) (*recordv1.Record, error) {
	versionVal, ok := item[attrSortKey].(*types.AttributeValueMemberN)
	if !ok {
		return nil, fmt.Errorf("version attribute is not a number")
	}
	version, err := strconv.ParseInt(versionVal.Value, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse version: %w", err)
	}
	dataVal, ok := item[attrData].(*types.AttributeValueMemberB)
	if !ok {
		return nil, fmt.Errorf("data attribute is not binary")
	}
	return &recordv1.Record{
		Version: version,
		Data:    dataVal.Value,
	}, nil
}
