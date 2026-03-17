package dynamodbstore

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	recordv1 "github.com/funinthecloud/protosource/record/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock Dynamoer
// ---------------------------------------------------------------------------

// mockDynamoer simulates DynamoDB behavior in memory for unit testing.
type mockDynamoer struct {
	mu     sync.Mutex
	tables map[string]map[string]map[string]types.AttributeValue // table -> compositeKey -> item
}

func newMockDynamoer() *mockDynamoer {
	return &mockDynamoer{
		tables: make(map[string]map[string]map[string]types.AttributeValue),
	}
}

func (m *mockDynamoer) ensureTable(name string) map[string]map[string]types.AttributeValue {
	if m.tables[name] == nil {
		m.tables[name] = make(map[string]map[string]types.AttributeValue)
	}
	return m.tables[name]
}

func compositeKey(item map[string]types.AttributeValue) string {
	pk := item["aggregate_id"].(*types.AttributeValueMemberS).Value
	sk := ""
	if v, ok := item["version"]; ok {
		sk = v.(*types.AttributeValueMemberN).Value
	}
	return pk + "|" + sk
}

func (m *mockDynamoer) TransactWriteItems(ctx context.Context, input *dynamodb.TransactWriteItemsInput, _ ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	// First pass: check all conditions.
	for _, item := range input.TransactItems {
		if item.Put == nil {
			continue
		}
		table := m.ensureTable(*item.Put.TableName)
		ck := compositeKey(item.Put.Item)
		if item.Put.ConditionExpression != nil {
			if _, exists := table[ck]; exists {
				return nil, &types.TransactionCanceledException{
					CancellationReasons: []types.CancellationReason{
						{Code: strPtr("ConditionalCheckFailed")},
					},
				}
			}
		}
	}

	// Second pass: write all items.
	for _, item := range input.TransactItems {
		if item.Put == nil {
			continue
		}
		table := m.ensureTable(*item.Put.TableName)
		ck := compositeKey(item.Put.Item)
		// Deep copy the item to prevent aliasing.
		copied := make(map[string]types.AttributeValue, len(item.Put.Item))
		for k, v := range item.Put.Item {
			copied[k] = v
		}
		table[ck] = copied
	}

	return &dynamodb.TransactWriteItemsOutput{}, nil
}

func (m *mockDynamoer) Query(ctx context.Context, input *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	table := m.ensureTable(*input.TableName)

	// Extract the partition key value from expression attribute values.
	pkVal := input.ExpressionAttributeValues[":id"].(*types.AttributeValueMemberS).Value

	// Collect matching items.
	var items []map[string]types.AttributeValue
	for _, item := range table {
		if item["aggregate_id"].(*types.AttributeValueMemberS).Value == pkVal {
			items = append(items, item)
		}
	}

	// Sort by version.
	ascending := input.ScanIndexForward == nil || *input.ScanIndexForward
	sort.Slice(items, func(i, j int) bool {
		vi, _ := strconv.ParseInt(items[i]["version"].(*types.AttributeValueMemberN).Value, 10, 64)
		vj, _ := strconv.ParseInt(items[j]["version"].(*types.AttributeValueMemberN).Value, 10, 64)
		if ascending {
			return vi < vj
		}
		return vi > vj
	})

	// Apply limit.
	if input.Limit != nil && int(*input.Limit) < len(items) {
		items = items[:*input.Limit]
	}

	return &dynamodb.QueryOutput{Items: items}, nil
}

func (m *mockDynamoer) PutItem(ctx context.Context, input *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	table := m.ensureTable(*input.TableName)
	pk := input.Item["aggregate_id"].(*types.AttributeValueMemberS).Value
	// For the aggregates table there is no sort key, so use pk alone.
	table[pk] = input.Item
	return &dynamodb.PutItemOutput{}, nil
}

func (m *mockDynamoer) GetItem(ctx context.Context, input *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	table := m.ensureTable(*input.TableName)
	pk := input.Key["aggregate_id"].(*types.AttributeValueMemberS).Value
	item, ok := table[pk]
	if !ok {
		return &dynamodb.GetItemOutput{}, nil
	}
	return &dynamodb.GetItemOutput{Item: item}, nil
}

func strPtr(s string) *string { return &s }

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func newTestStore(t *testing.T, opts ...Option) (*DynamoDBStore, *mockDynamoer) {
	t.Helper()
	mock := newMockDynamoer()
	store, err := New(mock, opts...)
	require.NoError(t, err)
	return store, mock
}

func makeRecord(version int64, data []byte) *recordv1.Record {
	return &recordv1.Record{Version: version, Data: data}
}

// ---------------------------------------------------------------------------
// Store basics
// ---------------------------------------------------------------------------

func TestNew_NilClient(t *testing.T) {
	_, err := New(nil)
	require.Error(t, err)
}

func TestSaveAndLoadSingleRecord(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	err := store.Save(ctx, "agg-1", makeRecord(1, []byte("event-1")))
	require.NoError(t, err)

	h, err := store.Load(ctx, "agg-1")
	require.NoError(t, err)
	require.Len(t, h.Records, 1)
	assert.Equal(t, int64(1), h.Records[0].Version)
	assert.Equal(t, []byte("event-1"), h.Records[0].Data)
}

func TestSaveMultipleRecordsAtOnce(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	err := store.Save(ctx, "agg-1",
		makeRecord(1, []byte("a")),
		makeRecord(2, []byte("b")),
		makeRecord(3, []byte("c")),
	)
	require.NoError(t, err)

	h, err := store.Load(ctx, "agg-1")
	require.NoError(t, err)
	require.Len(t, h.Records, 3)
	for i, rec := range h.Records {
		assert.Equal(t, int64(i+1), rec.Version)
	}
}

func TestSaveAppendsAcrossMultipleCalls(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, store.Save(ctx, "agg-1", makeRecord(1, []byte("a"))))
	require.NoError(t, store.Save(ctx, "agg-1", makeRecord(2, []byte("b"))))

	h, err := store.Load(ctx, "agg-1")
	require.NoError(t, err)
	require.Len(t, h.Records, 2)
	assert.Equal(t, int64(1), h.Records[0].Version)
	assert.Equal(t, int64(2), h.Records[1].Version)
}

func TestSaveNoRecords_IsNoOp(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	err := store.Save(ctx, "agg-1")
	require.NoError(t, err)

	h, err := store.Load(ctx, "agg-1")
	require.NoError(t, err)
	assert.Empty(t, h.Records)
}

func TestLoadNonExistent_ReturnsEmptyHistory(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	h, err := store.Load(ctx, "does-not-exist")
	require.NoError(t, err)
	assert.NotNil(t, h)
	assert.Empty(t, h.Records)
}

func TestRecordsReturnInVersionOrder(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// Save in non-sequential order across calls.
	require.NoError(t, store.Save(ctx, "agg-1", makeRecord(3, []byte("c"))))
	require.NoError(t, store.Save(ctx, "agg-1", makeRecord(1, []byte("a"))))
	require.NoError(t, store.Save(ctx, "agg-1", makeRecord(2, []byte("b"))))

	h, err := store.Load(ctx, "agg-1")
	require.NoError(t, err)
	require.Len(t, h.Records, 3)
	assert.Equal(t, int64(1), h.Records[0].Version)
	assert.Equal(t, int64(2), h.Records[1].Version)
	assert.Equal(t, int64(3), h.Records[2].Version)
}

// ---------------------------------------------------------------------------
// AggregateStore basics
// ---------------------------------------------------------------------------

func TestSaveAndLoadAggregate_RoundTrip(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	err := store.SaveAggregate(ctx, "agg-1", []byte("state-data"), 5)
	require.NoError(t, err)

	data, version, err := store.LoadAggregate(ctx, "agg-1")
	require.NoError(t, err)
	assert.Equal(t, int64(5), version)
	assert.Equal(t, []byte("state-data"), data)
}

func TestLoadAggregate_NonExistent(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	data, version, err := store.LoadAggregate(ctx, "nope")
	require.NoError(t, err)
	assert.Nil(t, data)
	assert.Equal(t, int64(0), version)
}

func TestSaveAggregate_Overwrites(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, store.SaveAggregate(ctx, "agg-1", []byte("v1"), 1))
	require.NoError(t, store.SaveAggregate(ctx, "agg-1", []byte("v2"), 2))

	data, version, err := store.LoadAggregate(ctx, "agg-1")
	require.NoError(t, err)
	assert.Equal(t, int64(2), version)
	assert.Equal(t, []byte("v2"), data)
}

func TestAggregates_Independent(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, store.SaveAggregate(ctx, "agg-1", []byte("one"), 1))
	require.NoError(t, store.SaveAggregate(ctx, "agg-2", []byte("two"), 2))

	d1, v1, err := store.LoadAggregate(ctx, "agg-1")
	require.NoError(t, err)
	assert.Equal(t, int64(1), v1)
	assert.Equal(t, []byte("one"), d1)

	d2, v2, err := store.LoadAggregate(ctx, "agg-2")
	require.NoError(t, err)
	assert.Equal(t, int64(2), v2)
	assert.Equal(t, []byte("two"), d2)
}

// ---------------------------------------------------------------------------
// SnapshotTailStore
// ---------------------------------------------------------------------------

func TestLoadTail_ReturnsLastN(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	for i := int64(1); i <= 10; i++ {
		require.NoError(t, store.Save(ctx, "agg-1", makeRecord(i, []byte(fmt.Sprintf("e%d", i)))))
	}

	h, err := store.LoadTail(ctx, "agg-1", 3)
	require.NoError(t, err)
	require.Len(t, h.Records, 3)
	// Should be ascending: 8, 9, 10
	assert.Equal(t, int64(8), h.Records[0].Version)
	assert.Equal(t, int64(9), h.Records[1].Version)
	assert.Equal(t, int64(10), h.Records[2].Version)
}

func TestLoadTail_FewerThanN(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, store.Save(ctx, "agg-1", makeRecord(1, []byte("a")), makeRecord(2, []byte("b"))))

	h, err := store.LoadTail(ctx, "agg-1", 10)
	require.NoError(t, err)
	require.Len(t, h.Records, 2)
	assert.Equal(t, int64(1), h.Records[0].Version)
	assert.Equal(t, int64(2), h.Records[1].Version)
}

func TestLoadTail_NonExistent(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	h, err := store.LoadTail(ctx, "nope", 5)
	require.NoError(t, err)
	assert.NotNil(t, h)
	assert.Empty(t, h.Records)
}

// ---------------------------------------------------------------------------
// Context handling
// ---------------------------------------------------------------------------

func TestCancelledContext(t *testing.T) {
	store, _ := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := store.Save(ctx, "agg-1", makeRecord(1, []byte("a")))
	assert.Error(t, err)

	_, err = store.Load(ctx, "agg-1")
	assert.Error(t, err)

	_, err = store.LoadTail(ctx, "agg-1", 5)
	assert.Error(t, err)

	err = store.SaveAggregate(ctx, "agg-1", []byte("x"), 1)
	assert.Error(t, err)

	_, _, err = store.LoadAggregate(ctx, "agg-1")
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// Data integrity
// ---------------------------------------------------------------------------

func TestRecordDataSurvivesRoundTrip(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	data := []byte{0x00, 0xFF, 0x01, 0xFE}
	require.NoError(t, store.Save(ctx, "agg-1", makeRecord(42, data)))

	h, err := store.Load(ctx, "agg-1")
	require.NoError(t, err)
	require.Len(t, h.Records, 1)
	assert.Equal(t, int64(42), h.Records[0].Version)
	assert.Equal(t, data, h.Records[0].Data)
}

func TestAggregateDataSurvivesRoundTrip(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	data := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	require.NoError(t, store.SaveAggregate(ctx, "agg-1", data, 99))

	got, version, err := store.LoadAggregate(ctx, "agg-1")
	require.NoError(t, err)
	assert.Equal(t, int64(99), version)
	assert.Equal(t, data, got)
}

func TestEventsAndAggregateAreIndependent(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, store.Save(ctx, "agg-1", makeRecord(1, []byte("event"))))
	require.NoError(t, store.SaveAggregate(ctx, "agg-1", []byte("aggregate"), 1))

	h, err := store.Load(ctx, "agg-1")
	require.NoError(t, err)
	require.Len(t, h.Records, 1)
	assert.Equal(t, []byte("event"), h.Records[0].Data)

	data, _, err := store.LoadAggregate(ctx, "agg-1")
	require.NoError(t, err)
	assert.Equal(t, []byte("aggregate"), data)
}

// ---------------------------------------------------------------------------
// DynamoDB-specific
// ---------------------------------------------------------------------------

func TestDuplicateVersionReturnsError(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, store.Save(ctx, "agg-1", makeRecord(1, []byte("first"))))
	err := store.Save(ctx, "agg-1", makeRecord(1, []byte("duplicate")))
	assert.Error(t, err)
}

func TestTenantPrefix_NamespacesAggregates(t *testing.T) {
	mock := newMockDynamoer()
	store1, err := New(mock, WithTenantPrefix("tenant-a"))
	require.NoError(t, err)
	store2, err := New(mock, WithTenantPrefix("tenant-b"))
	require.NoError(t, err)

	ctx := context.Background()

	require.NoError(t, store1.Save(ctx, "agg-1", makeRecord(1, []byte("from-a"))))
	require.NoError(t, store2.Save(ctx, "agg-1", makeRecord(1, []byte("from-b"))))

	h1, err := store1.Load(ctx, "agg-1")
	require.NoError(t, err)
	require.Len(t, h1.Records, 1)
	assert.Equal(t, []byte("from-a"), h1.Records[0].Data)

	h2, err := store2.Load(ctx, "agg-1")
	require.NoError(t, err)
	require.Len(t, h2.Records, 1)
	assert.Equal(t, []byte("from-b"), h2.Records[0].Data)
}

func TestTenantPrefix_AggregateStore(t *testing.T) {
	mock := newMockDynamoer()
	store1, err := New(mock, WithTenantPrefix("t1"))
	require.NoError(t, err)
	store2, err := New(mock, WithTenantPrefix("t2"))
	require.NoError(t, err)

	ctx := context.Background()

	require.NoError(t, store1.SaveAggregate(ctx, "agg-1", []byte("t1-data"), 1))
	require.NoError(t, store2.SaveAggregate(ctx, "agg-1", []byte("t2-data"), 2))

	d1, v1, err := store1.LoadAggregate(ctx, "agg-1")
	require.NoError(t, err)
	assert.Equal(t, int64(1), v1)
	assert.Equal(t, []byte("t1-data"), d1)

	d2, v2, err := store2.LoadAggregate(ctx, "agg-1")
	require.NoError(t, err)
	assert.Equal(t, int64(2), v2)
	assert.Equal(t, []byte("t2-data"), d2)
}

func TestSaveBatching_Over100Records(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	records := make([]*recordv1.Record, 150)
	for i := range records {
		records[i] = makeRecord(int64(i+1), []byte(fmt.Sprintf("e%d", i+1)))
	}

	err := store.Save(ctx, "agg-1", records...)
	require.NoError(t, err)

	h, err := store.Load(ctx, "agg-1")
	require.NoError(t, err)
	require.Len(t, h.Records, 150)
	assert.Equal(t, int64(1), h.Records[0].Version)
	assert.Equal(t, int64(150), h.Records[149].Version)
}

// ---------------------------------------------------------------------------
// Functional options
// ---------------------------------------------------------------------------

func TestWithEventsTable(t *testing.T) {
	store, _ := newTestStore(t, WithEventsTable("my-events"))
	assert.Equal(t, "my-events", store.eventsTable)
}

func TestWithAggregatesTable(t *testing.T) {
	store, _ := newTestStore(t, WithAggregatesTable("my-aggs"))
	assert.Equal(t, "my-aggs", store.aggregatesTable)
}
