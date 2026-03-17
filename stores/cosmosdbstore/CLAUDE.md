# cosmosdbstore

> Part of the [protosource](../../CLAUDE.md) event sourcing framework.

Build an Azure Cosmos DB-backed implementation of the `protosource.Store`, `protosource.AggregateStore`, and `protosource.SnapshotTailStore` interfaces. Use the `github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos` SDK.

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

- Package: `cosmosdbstore`
- Main type: `CosmosDBStore`
- Constructor: `New(container *azcosmos.ContainerClient, opts ...Option) *CosmosDBStore`
- Accept an already-created `*azcosmos.ContainerClient` — the caller owns the lifecycle
- Use separate containers for events and aggregates (or a single container with a `type` discriminator field)

### Container and Document Structure

**Events container** (default: `events`):
- Partition key: `/aggregateId`
- Documents:

```json
{
    "id": "<aggregateId>_<version>",
    "aggregateId": "<aggregateId>",
    "version": 42,
    "data": "<base64-encoded bytes>",
    "type": "event"
}
```

The `id` field must be unique within the partition. Combining aggregate ID and version ensures uniqueness. Store `data` as base64-encoded string since Cosmos DB JSON doesn't have a native binary type.

**Aggregates container** (default: `aggregates`, or same container with `type: "aggregate"`):
- Partition key: `/aggregateId`
- Documents:

```json
{
    "id": "<aggregateId>",
    "aggregateId": "<aggregateId>",
    "version": 42,
    "data": "<base64-encoded bytes>",
    "type": "aggregate"
}
```

### Single-Container Alternative

If using one container for both events and aggregates:
- Partition key: `/aggregateId`
- Discriminate by `type` field (`"event"` vs `"aggregate"`)
- Queries filter by `type` in the WHERE clause
- This reduces container count but slightly complicates queries

Either approach works. Document the choice in the code.

### Save Implementation

- Use Cosmos DB transactional batch (`ContainerClient.NewTransactionalBatch`) for atomicity
- All items in a batch must share the same partition key — this works since all records share the same aggregate ID
- Transactional batches are limited to 100 operations — batch accordingly
- For each record: `batch.CreateItem` with a condition to prevent overwrite
- Check `ctx.Err()` before starting
- Save with no records should succeed (no-op)
- Encode `data` as base64 for JSON storage

### Load Implementation

- Query: `SELECT * FROM c WHERE c.aggregateId = @id AND c.type = 'event' ORDER BY c.version ASC`
- Use `ContainerClient.NewQueryItemsPager` with partition key
- Iterate all pages, build `[]*recordv1.Record`
- Decode `data` from base64
- Return empty `&historyv1.History{}` if no documents found

### LoadTail Implementation

- Query: `SELECT TOP @n * FROM c WHERE c.aggregateId = @id AND c.type = 'event' ORDER BY c.version DESC`
- Reverse the results to return ascending version order
- Or use a subquery: `SELECT * FROM (SELECT TOP @n ... ORDER BY c.version DESC) ORDER BY c.version ASC` (Cosmos DB supports this in some API versions)

### SaveAggregate / LoadAggregate

- `SaveAggregate`: `UpsertItem` with partition key = aggregateId
- `LoadAggregate`: `ReadItem` by id = aggregateId; return `nil, 0, nil` if `StatusCode == 404`

### Functional Options

```go
type Option func(*CosmosDBStore)

func WithEventsContainer(name string) Option     // default: "events"
func WithAggregatesContainer(name string) Option // default: "aggregates"
```

## Testing Strategy

Use the Azure Cosmos DB Emulator for integration tests. The emulator runs locally and provides the full Cosmos DB API.

```bash
# Docker-based emulator
docker run -p 8081:8081 -p 10251-10254:10251-10254 \
    mcr.microsoft.com/cosmosdb/linux/azure-cosmos-emulator
```

Alternatively, define a wrapper interface for the Cosmos DB container client and mock it for pure unit tests.

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
- SaveAggregate overwrites previous state (upsert)
- Independent aggregates don't interfere

**SnapshotTailStore:**
- LoadTail returns last N records in ascending order
- LoadTail with fewer records than N returns all records
- LoadTail for non-existent aggregate returns empty history

**Context handling:**
- All methods return error on cancelled context

**Data integrity:**
- Record version and data survive round-trip (including base64 encode/decode)
- Aggregate data and version survive round-trip
- Event history and aggregate state are independent

**Cosmos DB-specific:**
- Duplicate document ID returns conflict error
- Transactional batch with > 100 items (batching)
- Base64 encoding preserves binary data with null bytes
- Partition key routing is correct

### Test Helper

```go
func newTestStore(t *testing.T, opts ...Option) *CosmosDBStore {
    t.Helper()
    // Connect to Cosmos DB emulator
    cred, err := azcosmos.NewKeyCredential("emulator-key")
    require.NoError(t, err)
    client, err := azcosmos.NewClientWithKey("https://localhost:8081", cred, nil)
    require.NoError(t, err)
    container, err := client.NewContainer("testdb", "events")
    require.NoError(t, err)
    return New(container, opts...)
}
```

## Reference Implementation

Use `stores/boltdbstore/` as the structural reference for interface implementation, and `stores/memorystore/` for the test patterns. The boltdbstore tests demonstrate the expected coverage areas.

## Build & Test

```bash
go test ./stores/cosmosdbstore/ -v -count=1
go vet ./stores/cosmosdbstore/
```

Ensure `go vet` is clean and there are no data races (`go test -race`).
