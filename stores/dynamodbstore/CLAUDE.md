# dynamodbstore

> Part of the [protosource](../../CLAUDE.md) event sourcing framework.

Build a DynamoDB-backed implementation of the `protosource.Store`, `protosource.AggregateStore`, and `protosource.SnapshotTailStore` interfaces. Use the AWS SDK v2 (`github.com/aws/aws-sdk-go-v2/service/dynamodb`).

There is an existing implementation in this directory that can be used as a reference for DynamoDB patterns, but it predates the current interfaces (`AggregateStore`, `SnapshotTailStore`) and has issues (hardcoded short attribute names, snapshot interval on the store instead of the aggregate, `ErrNoRecords` on empty save). Start fresh.

## Interfaces to Implement

From `protosource.go`:

```go
// Store — required
type Store interface {
    Save(ctx context.Context, aggregateID string, records ...*recordv1.Record) error
    Load(ctx context.Context, aggregateId string) (*historyv1.History, error)
}

// AggregateStore — required
type AggregateStore interface {
    SaveAggregate(ctx context.Context, aggregateID string, data []byte, version int64) error
    LoadAggregate(ctx context.Context, aggregateID string) (data []byte, version int64, err error)
}

// SnapshotTailStore — required
type SnapshotTailStore interface {
    LoadTail(ctx context.Context, aggregateID string, n int) (*historyv1.History, error)
}
```

Import paths for the proto types:

```go
historyv1 "github.com/funinthecloud/protosource/history/v1"
recordv1  "github.com/funinthecloud/protosource/record/v1"
```

## Record Proto

The `recordv1.Record` has two fields:

```go
type Record struct {
    Version int64   // sequential version number (sort key)
    Data    []byte  // serialized event payload
}
```

## Design Guidelines

### Package and Naming

- Package: `dynamodbstore`
- Main type: `DynamoDBStore`
- Constructor: `New(client Dynamoer, opts ...Option) (*DynamoDBStore, error)`
- Accept a `Dynamoer` interface (not the concrete client) for testability

### Dynamoer Interface

Define a minimal interface covering the DynamoDB operations needed:

```go
type Dynamoer interface {
    Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
    TransactWriteItems(ctx context.Context, params *dynamodb.TransactWriteItemsInput, optFns ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error)
    PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
    GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
}
```

### Table Schema

**Events table** (default name: `events`):
- Partition key: `aggregate_id` (S)
- Sort key: `version` (N)
- Attribute: `data` (B) — `record.Data` bytes

**Aggregates table** (default name: `aggregates`):
- Partition key: `aggregate_id` (S)
- Attributes: `data` (B), `version` (N)

Using two separate tables keeps the schema clean. Alternatively, a single table with a sort key prefix could work but adds complexity.

### Save Implementation

- Use `TransactWriteItems` for atomicity
- Each record becomes a `Put` with a condition expression `attribute_not_exists(aggregate_id) AND attribute_not_exists(version)` to prevent duplicate versions
- DynamoDB transactions are limited to 100 items — if `len(records) > 100`, batch into multiple transactions (document this trade-off)
- Check `ctx.Err()` before starting
- Save with no records should succeed (no-op)

### Load Implementation

- Use `Query` with `KeyConditionExpression: "aggregate_id = :id"`
- `ScanIndexForward: true` for ascending version order
- `ConsistentRead: true`
- Paginate if needed (check `LastEvaluatedKey`)
- Return empty `&historyv1.History{}` if no items found

### LoadTail Implementation

- Use `Query` with `ScanIndexForward: false` and `Limit: n`
- Reverse the results to return ascending version order
- This is the natural DynamoDB pattern for "last N by sort key"

### SaveAggregate / LoadAggregate

- `SaveAggregate`: `PutItem` to aggregates table with `aggregate_id`, `version`, `data`
- `LoadAggregate`: `GetItem` by `aggregate_id`; return `nil, 0, nil` if not found

### Functional Options

```go
type Option func(*DynamoDBStore)

func WithEventsTable(name string) Option     // default: "events"
func WithAggregatesTable(name string) Option // default: "aggregates"
func WithTenantPrefix(prefix string) Option  // prepends "prefix#" to aggregate IDs for multi-tenant tables
```

## Testing Strategy

Use a mock implementation of the `Dynamoer` interface for unit tests. The mock should simulate DynamoDB behavior: store items in maps, enforce condition expressions, support query ordering.

### Required Test Coverage

**Store basics:**
- Save single record, Load it back
- Save multiple records at once
- Save appends to previous records (multiple Save calls)
- Save with no records (should not error)
- Load non-existent aggregate returns empty history
- Records come back in version order

**AggregateStore basics:**
- SaveAggregate / LoadAggregate round-trip
- LoadAggregate for non-existent returns nil data, version 0, no error
- SaveAggregate overwrites previous state
- Independent aggregates don't interfere

**SnapshotTailStore:**
- LoadTail returns last N records in ascending order
- LoadTail with fewer records than N returns all records
- LoadTail for non-existent aggregate returns empty history

**Context handling:**
- All methods return error on cancelled context

**Data integrity:**
- Record version and data survive round-trip
- Aggregate data and version survive round-trip
- Event history and aggregate state are independent

**DynamoDB-specific:**
- Duplicate version (condition check failure) returns error
- Tenant prefix correctly namespaces aggregate IDs
- Batching for > 100 records (if implemented)

### Test Helper

```go
func newTestStore(t *testing.T, opts ...Option) *DynamoDBStore {
    t.Helper()
    mock := newMockDynamoer()
    store, err := New(mock, opts...)
    require.NoError(t, err)
    return store
}
```

## DDL / Setup

Include a `ddl/` subdirectory with CloudFormation or Terraform for table creation:

```yaml
EventsTable:
  Type: AWS::DynamoDB::Table
  Properties:
    TableName: events
    KeySchema:
      - AttributeName: aggregate_id
        KeyType: HASH
      - AttributeName: version
        KeyType: RANGE
    AttributeDefinitions:
      - AttributeName: aggregate_id
        AttributeType: S
      - AttributeName: version
        AttributeType: N
    BillingMode: PAY_PER_REQUEST
```

## Reference Implementation

Use `stores/boltdbstore/` as the structural reference for the sharded store pattern, and `stores/memorystore/` for the test patterns. The boltdbstore tests demonstrate the expected coverage areas.

## Build & Test

```bash
go test ./stores/dynamodbstore/ -v -count=1
go vet ./stores/dynamodbstore/
```

Ensure `go vet` is clean and there are no data races (`go test -race`).
