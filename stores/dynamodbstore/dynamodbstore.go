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
	"github.com/funinthecloud/protosource/opaquedata"
	recordv1 "github.com/funinthecloud/protosource/record/v1"
	"google.golang.org/protobuf/proto"
)

// Dynamoer is the minimal DynamoDB interface required by DynamoDBStore.
// It is satisfied by *dynamodb.Client.
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

	DefaultEventsTable = "events"
	maxTransactItems   = 100
)

// DynamoDBStore implements the protosource Store, AggregateStore, and
// SnapshotTailStore interfaces backed by DynamoDB.
type DynamoDBStore struct {
	client      Dynamoer
	eventsTable string
	opaqueStore opaquedata.OpaqueStore // SaveAggregate requires this; aggregates are stored via opaquedata with GSI indexing
	ttl         time.Duration          // when non-zero, sets TTL attribute on event writes
}

// New creates a new DynamoDBStore. The client must not be nil.
func New(client Dynamoer, opts ...Option) (*DynamoDBStore, error) {
	s := &DynamoDBStore{
		client:      client,
		eventsTable: DefaultEventsTable,
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

// WithOpaqueStore sets the OpaqueStore used by SaveAggregate to persist
// materialized aggregates. The aggregates table uses pk/sk (String/String)
// keys with up to 20 GSIs for query access patterns. All aggregates must
// implement opaquedata.AutoPKSK to be materialized.
func WithOpaqueStore(store opaquedata.OpaqueStore) Option {
	return func(s *DynamoDBStore) { s.opaqueStore = store }
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

// Save stores records for the given aggregate ID. Each batch of up to 100
// records is written atomically using TransactWriteItems with condition
// expressions to prevent duplicate versions.
//
// When len(records) exceeds 100, Save splits the work into multiple
// transactions. Atomicity is guaranteed within each batch, but NOT across
// batches: if a later batch fails, earlier batches are already committed.
// Callers that require all-or-nothing semantics for large writes should
// pre-validate or limit batch size upstream.
//
// Saving zero records is a no-op.
func (s *DynamoDBStore) Save(ctx context.Context, aggregateID string, records ...*recordv1.Record) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("dynamodbstore.Save: %w", err)
	}
	if len(records) == 0 {
		return nil
	}

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
				attrPartitionKey: &types.AttributeValueMemberS{Value: aggregateID},
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

	history := &historyv1.History{}

	var exclusiveStartKey map[string]types.AttributeValue
	for {
		input := &dynamodb.QueryInput{
			TableName:              &s.eventsTable,
			ConsistentRead:         aws.Bool(true),
			ScanIndexForward:       aws.Bool(true),
			KeyConditionExpression: aws.String("a = :id"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":id": &types.AttributeValueMemberS{Value: aggregateID},
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
// version ascending. It queries DynamoDB in descending order with a per-page
// limit of n, paginating until n records are collected or no more pages remain,
// then reverses the results.
//
// If n <= 0, an empty History is returned immediately.
func (s *DynamoDBStore) LoadTail(ctx context.Context, aggregateID string, n int) (*historyv1.History, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("dynamodbstore.LoadTail: %w", err)
	}
	if n <= 0 {
		return &historyv1.History{}, nil
	}

	// Clamp to int32 range for the DynamoDB Limit parameter.
	limit := n
	const maxInt32 = 1<<31 - 1
	if limit > maxInt32 {
		limit = maxInt32
	}

	history := &historyv1.History{}
	remaining := n

	var exclusiveStartKey map[string]types.AttributeValue
	for remaining > 0 {
		pageLimit := remaining
		if pageLimit > maxInt32 {
			pageLimit = maxInt32
		}

		input := &dynamodb.QueryInput{
			TableName:              &s.eventsTable,
			ConsistentRead:         aws.Bool(true),
			ScanIndexForward:       aws.Bool(false),
			Limit:                  aws.Int32(int32(pageLimit)),
			KeyConditionExpression: aws.String("a = :id"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":id": &types.AttributeValueMemberS{Value: aggregateID},
			},
		}
		if exclusiveStartKey != nil {
			input.ExclusiveStartKey = exclusiveStartKey
		}

		resp, err := s.client.Query(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("dynamodbstore.LoadTail: %w", err)
		}

		for _, item := range resp.Items {
			if remaining <= 0 {
				break
			}
			rec, err := itemToRecord(item)
			if err != nil {
				return nil, fmt.Errorf("dynamodbstore.LoadTail: %w", err)
			}
			history.Records = append(history.Records, rec)
			remaining--
		}

		if resp.LastEvaluatedKey == nil || remaining <= 0 {
			break
		}
		exclusiveStartKey = resp.LastEvaluatedKey
	}

	// Reverse to ascending version order.
	for i, j := 0, len(history.Records)-1; i < j; i, j = i+1, j-1 {
		history.Records[i], history.Records[j] = history.Records[j], history.Records[i]
	}

	return history, nil
}

// SaveAggregate persists the materialized aggregate state via the OpaqueStore.
// The aggregate must implement opaquedata.AutoPKSK (generated from proto
// annotations) and an OpaqueStore must be configured via WithOpaqueStore.
// The aggregates table uses pk/sk keys with GSIs for query access patterns.
func (s *DynamoDBStore) SaveAggregate(ctx context.Context, aggregate proto.Message) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("dynamodbstore.SaveAggregate: %w", err)
	}
	if s.opaqueStore == nil {
		return fmt.Errorf("dynamodbstore.SaveAggregate: no OpaqueStore configured (use WithOpaqueStore)")
	}
	apk, ok := aggregate.(opaquedata.AutoPKSK)
	if !ok {
		return fmt.Errorf("dynamodbstore.SaveAggregate: aggregate %T does not implement opaquedata.AutoPKSK", aggregate)
	}
	od, err := opaquedata.NewOpaqueDataFromProto(apk)
	if err != nil {
		return fmt.Errorf("dynamodbstore.SaveAggregate: opaquedata: %w", err)
	}
	if err := s.opaqueStore.Put(ctx, od); err != nil {
		return fmt.Errorf("dynamodbstore.SaveAggregate: %w", err)
	}
	return nil
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
