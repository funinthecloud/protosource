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
	historyv1 "github.com/funinthecloud/protosource/history/v1"
	recordv1 "github.com/funinthecloud/protosource/record/v1"
)

type Dynamoer interface {
	Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	TransactWriteItems(ctx context.Context, params *dynamodb.TransactWriteItemsInput, optFns ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error)
}

const (
	DefaultHashKey          = "a"
	DefaultSortKey          = "v"
	DefaultTTLKey           = "t"
	DefaultDataKey          = "d"
	DefaultTTL              = 0
	DefaultTableName        = "events"
	DefaultSnapshotInterval = 10
	MaxRecords              = 100
)

var (
	ErrNoRecords        = errors.New("no records to save")
	ErrDuplicateVersion = errors.New("version numbers must be unique")
	ErrTooManyRecords   = errors.New("too many records, maximum 100") // Refactor this to accept more than 100 records in groups.
)

// DynamoDBStore is an implementation of the Store interface that uses DynamoDB as the backend.
type DynamoDBStore struct {
	client                 Dynamoer // DynamoDB client
	tableName              string   // DynamoDB table name
	tenantPrefix           string   // For tenant segregation.
	hashKey                string   // The string to use for the hash key
	sortKey                string   // The string to use for the sort key
	ttlKey                 string   // the string to use for the ttl key
	dataKey                string   // the string to use for the data key
	ttl                    int64    // The default TTL to use.
	snapshotInterval       int32    // Snapshot interval
	keyConditionExpression string   // Only calculate and store this once
	hashKeyConditionString string   // Only calculate and store this once
}

// NewDynamoDBStore initializes and returns a new instance of DynamoDBStore.
// Requires a Dynamoer.
func NewDynamoDBStore(client Dynamoer, opts ...Option) (*DynamoDBStore, error) {
	dbs := &DynamoDBStore{
		client:           client,
		tableName:        DefaultTableName,
		tenantPrefix:     "",
		hashKey:          DefaultHashKey,
		sortKey:          DefaultSortKey,
		ttlKey:           DefaultTTLKey,
		dataKey:          DefaultDataKey,
		ttl:              DefaultTTL,
		snapshotInterval: DefaultSnapshotInterval,
	}
	for _, opt := range opts {
		opt(dbs)
	}

	dbs.keyConditionExpression = fmt.Sprintf("%s = :aggregate_id", dbs.hashKey)
	dbs.hashKeyConditionString = fmt.Sprintf("attribute_not_exists(%s) AND attribute_not_exists(%s)", dbs.hashKey, dbs.sortKey)

	if err := dbs.OK(); err != nil {
		return nil, fmt.Errorf("dynamodbstore.NewDynamoDBStore: %w", err)
	}
	return dbs, nil
}

func (s *DynamoDBStore) OK() error {
	if s.client == nil {
		return errors.New("dynamodbstore.OK: no client provided")
	}
	return nil
}

// Option represents a functional configuration of *DynamoDBStore.
type Option func(store *DynamoDBStore)

// WithTableName sets the table name to use within DynamoDB.
func WithTableName(tableName string) Option {
	return func(r *DynamoDBStore) {
		r.tableName = tableName
	}
}

// WithTenantPrefix allows the underlying store to prefix the AggregateId when putting them into DynamoDB.
//
// The purpose here is to be able to re-use the same DynamoDB table for MANY different tenants.
func WithTenantPrefix(tenantPrefix string) Option {
	return func(r *DynamoDBStore) {
		r.tenantPrefix = tenantPrefix
	}
}

// WithHashKey sets the hash key to be used with DynamoDB.
func WithHashKey(hashKey string) Option {
	return func(r *DynamoDBStore) {
		r.hashKey = hashKey
	}
}

// WithSortKey sets the sort key to be used with DynamoDB.
func WithSortKey(sortKey string) Option {
	return func(r *DynamoDBStore) {
		r.sortKey = sortKey
	}
}

// WithTTLKey sets the TTL (Time To Live) key to be used with DynamoDB.
func WithTTLKey(ttlKey string) Option {
	return func(r *DynamoDBStore) {
		r.ttlKey = ttlKey
	}
}

// WithDataKey sets the data key to be used with DynamoDB.
func WithDataKey(dataKey string) Option {
	return func(r *DynamoDBStore) {
		r.dataKey = dataKey
	}
}

// WithTTL sets the TTL (Time To Live) for records being put into DynamoDB.
//
// The purpose here is to be able to re-use the same DynamoDB table for MANY different types of events/changes/aggregates.
func WithTTL(ttl int64) Option {
	return func(r *DynamoDBStore) {
		r.ttl = ttl
	}
}

// WithSnapshotInterval sets the snapshot interval for the store.
func WithSnapshotInterval(snapshotInterval int32) Option {
	return func(r *DynamoDBStore) {
		r.snapshotInterval = snapshotInterval
	}
}

// SnapshotInterval returns the configured snapshot interval.
func (s *DynamoDBStore) SnapshotInterval() int32 {
	return s.snapshotInterval
}

// Save stores a list of records for a given aggregate ID in the DynamoDB table.
// If any record conflicts (duplicate version for the same aggregate), an error is returned.
func (s *DynamoDBStore) Save(ctx context.Context, aggregateId string, records ...*recordv1.Record) error {
	if len(records) == 0 {
		return ErrNoRecords
	}
	if len(records) > MaxRecords {
		return ErrTooManyRecords
	}

	// Use a transaction to ensure atomicity across multiple writes
	var writeItems []types.TransactWriteItem
	for _, record := range records {
		writeItems = append(writeItems, types.TransactWriteItem{
			Put: &types.Put{
				TableName: &s.tableName,
				Item: map[string]types.AttributeValue{
					"a": &types.AttributeValueMemberS{Value: aggregateId},
					"v": &types.AttributeValueMemberN{Value: strconv.FormatInt(record.Version, 10)},
					"d": &types.AttributeValueMemberB{Value: record.Data},
					"l": &types.AttributeValueMemberN{Value: strconv.FormatInt(time.Now().Unix(), 10)},
				},
				ConditionExpression: aws.String("attribute_not_exists(a) AND attribute_not_exists(v)"),
			},
		})
	}

	// Execute the transaction
	_, err := s.client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: writeItems,
	})
	if err != nil {
		return fmt.Errorf("dynamodbstore.Save: failed to save records: %w", err)
	}

	return nil
}

// Load retrieves the history for a given aggregate ID from the DynamoDB table.
// If no history exists for the specified ID, an empty History object is returned.
func (s *DynamoDBStore) Load(ctx context.Context, aggregateId string) (*historyv1.History, error) {
	queryInput := &dynamodb.QueryInput{
		TableName:                 aws.String(s.tableName),
		ConsistentRead:            aws.Bool(true),
		ScanIndexForward:          aws.Bool(false),
		KeyConditionExpression:    aws.String(s.keyConditionExpression),
		ExpressionAttributeValues: s.makeExpressionAttributeValues(aggregateId),
	}
	if s.snapshotInterval > 0 {
		queryInput.Limit = aws.Int32(s.snapshotInterval)
	}

	resp, err := s.client.Query(ctx, queryInput)
	if err != nil {
		return nil, fmt.Errorf("dynamodbstore.Load: failed to query history: %w", err)
	}

	// Build the history object from the query result
	history := &historyv1.History{}
	for _, item := range resp.Items {
		version, err := strconv.ParseInt(item["v"].(*types.AttributeValueMemberN).Value, 10, 64) // Adjusted key to match the Save method
		if err != nil {
			return nil, fmt.Errorf("dynamodbstore.Load: failed to parse version: %w", err)
		}
		data, ok := item["d"].(*types.AttributeValueMemberB)
		if !ok {
			return nil, errors.New("dynamodbstore.Load: data field is not of type binary")
		}
		record := &recordv1.Record{
			Version: version,
			Data:    data.Value,
		}
		history.Records = append(history.Records, record)
	}

	return history, nil
}

func (s *DynamoDBStore) makeExpressionAttributeValues(aggregateId string) map[string]types.AttributeValue {
	result := make(map[string]types.AttributeValue)
	result[":aggregate_id"] = &types.AttributeValueMemberS{Value: makeDBKey(aggregateId, s.tenantPrefix)}
	return result
}
func makeDBKey(aggregateId string, tenantPrefix string) string {
	if tenantPrefix != "" {
		return tenantPrefix + "#" + aggregateId
	}
	return aggregateId

}
