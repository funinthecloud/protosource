# dynamodbstore

> Part of the [protosource](../../CLAUDE.md) event sourcing framework.

A DynamoDB-backed implementation of the `protosource.Store`, `protosource.AggregateStore`, and `protosource.SnapshotTailStore` interfaces. Uses the AWS SDK v2 (`github.com/aws/aws-sdk-go-v2/service/dynamodb`).

## Interfaces Implemented

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

## Design

### Attribute Names

**All DynamoDB attribute names use single characters** to minimize per-item byte costs. DynamoDB charges per byte for reads and writes, and attribute names are included in every item's stored and transferred size. At scale (millions of events), the savings are material.

| Attribute | Key | Type | Description |
|-----------|-----|------|-------------|
| `a` | Partition key | S | Aggregate ID (with optional tenant prefix) |
| `v` | Sort key | N | Version number |
| `d` | — | B | Event/aggregate data payload |
| `t` | — | N | TTL epoch seconds (optional, events table only) |

### Package and Naming

- Package: `dynamodbstore`
- Main type: `DynamoDBStore`
- Constructor: `New(client Dynamoer, opts ...Option) (*DynamoDBStore, error)`
- Accept a `Dynamoer` interface (not the concrete client) for testability

### Dynamoer Interface

Minimal interface covering the DynamoDB operations needed:

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
- Partition key: `a` (S) — aggregate ID
- Sort key: `v` (N) — version number
- Attribute: `d` (B) — `record.Data` bytes
- Attribute: `t` (N) — TTL epoch seconds (optional, only when `WithTTL` is set)

**Aggregates table** (default name: `aggregates`):
- Partition key: `a` (S) — aggregate ID
- Attributes: `d` (B) — aggregate state, `v` (N) — version

Using two separate tables keeps the schema clean.

### Save Implementation

- Use `TransactWriteItems` for atomicity
- Each record becomes a `Put` with a condition expression `attribute_not_exists(a) AND attribute_not_exists(v)` to prevent duplicate versions
- DynamoDB transactions are limited to 100 items — records are auto-batched into groups of 100
- When TTL is configured, each item includes a `t` attribute with the expiry epoch
- Check `ctx.Err()` before starting
- Save with no records is a no-op

### Load Implementation

- Use `Query` with `KeyConditionExpression: "a = :id"`
- `ScanIndexForward: true` for ascending version order
- `ConsistentRead: true`
- Paginate via `LastEvaluatedKey`
- Return empty `&historyv1.History{}` if no items found

### LoadTail Implementation

- Use `Query` with `ScanIndexForward: false` and `Limit: n`
- Reverse the results to return ascending version order
- This is the natural DynamoDB pattern for "last N by sort key"

### SaveAggregate / LoadAggregate

- `SaveAggregate`: `PutItem` to aggregates table with `a`, `v`, `d`
- `LoadAggregate`: `GetItem` by `a`; return `nil, 0, nil` if not found

### Functional Options

```go
type Option func(*DynamoDBStore)

func WithEventsTable(name string) Option     // default: "events"
func WithAggregatesTable(name string) Option // default: "aggregates"
func WithTenantPrefix(prefix string) Option  // prepends "prefix#" to aggregate IDs for multi-tenant tables
func WithTTL(ttl time.Duration) Option       // sets TTL on event records; table must have TTL enabled on "t"
```

### TTL (Time To Live)

Use `WithTTL(duration)` to set an expiration on event records. When configured, each saved event includes a `t` attribute containing the Unix epoch second at which the record should expire. DynamoDB's TTL feature handles automatic deletion asynchronously (typically within 48 hours of expiry).

**Requirements:**
- The events table must have TTL enabled on the `t` attribute (see `ddl/cloudformation.yaml`)
- TTL is only applied to events, not aggregates (aggregate state should persist)
- A zero or negative duration disables TTL (the default)

**Use cases:**
- Event expiration after snapshots: once a snapshot captures aggregate state, older events can age off
- Temporary/ephemeral aggregates with bounded lifetimes
- Cost control for high-volume event streams

## Testing Strategy

Use a mock implementation of the `Dynamoer` interface for unit tests. The mock simulates DynamoDB behavior: stores items in maps, enforces condition expressions, supports query ordering.

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
- Batching for > 100 records

**TTL:**
- WithTTL sets TTL attribute with correct expiry epoch
- Without TTL, no TTL attribute is present

### Test Helper

```go
func newTestStore(t *testing.T, opts ...Option) (*DynamoDBStore, *mockDynamoer) {
    t.Helper()
    mock := newMockDynamoer()
    store, err := New(mock, opts...)
    require.NoError(t, err)
    return store, mock
}
```

## DDL / Setup

The `ddl/` subdirectory contains CloudFormation for table creation with TTL enabled on the events table.

## Reference Implementation

Use `stores/boltdbstore/` as the structural reference and `stores/memorystore/` for the test patterns.

## Build & Test

```bash
go test ./stores/dynamodbstore/ -v -count=1
go vet ./stores/dynamodbstore/
```

Ensure `go vet` is clean and there are no data races (`go test -race`).
