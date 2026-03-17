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

### Field Names

**All Cosmos DB document field names use single characters** to minimize per-document byte costs. Cosmos DB charges based on Request Units (RU), which are directly proportional to document size. Field names are stored in every document's JSON representation, so shorter names reduce both storage and RU consumption at scale.

| Field | Purpose | Type | Description |
|-------|---------|------|-------------|
| `id` | Document ID | string | Required by Cosmos DB; `<aggregateId>_<version>` for events, `<aggregateId>` for aggregates |
| `a` | Partition key | string | Aggregate ID (with optional tenant prefix) |
| `v` | — | number | Version number |
| `d` | — | string | Event/aggregate payload (base64-encoded) |
| `y` | — | string | Document type discriminator (`"e"` for event, `"a"` for aggregate) |
| `t` | — | number | TTL epoch seconds (optional, events only; Cosmos DB native TTL) |

### Package and Naming

- Package: `cosmosdbstore`
- Main type: `CosmosDBStore`
- Constructor: `New(container *azcosmos.ContainerClient, opts ...Option) *CosmosDBStore`
- Accept an already-created `*azcosmos.ContainerClient` — the caller owns the lifecycle
- Use separate containers for events and aggregates (or a single container with a `y` discriminator field)

### Container and Document Structure

**Events container** (default: `events`):
- Partition key: `/a`
- Documents:

```json
{
    "id": "<aggregateId>_<version>",
    "a": "<aggregateId>",
    "v": 42,
    "d": "<base64-encoded bytes>",
    "y": "e",
    "t": 1742169600
}
```

The `id` field must be unique within the partition. Combining aggregate ID and version ensures uniqueness. Store `d` as base64-encoded string since Cosmos DB JSON doesn't have a native binary type. The `t` field is only present when TTL is configured.

**Aggregates container** (default: `aggregates`, or same container with `y: "a"`):
- Partition key: `/a`
- Documents:

```json
{
    "id": "<aggregateId>",
    "a": "<aggregateId>",
    "v": 42,
    "d": "<base64-encoded bytes>",
    "y": "a"
}
```

### Single-Container Alternative

If using one container for both events and aggregates:
- Partition key: `/a`
- Discriminate by `y` field (`"e"` vs `"a"`)
- Queries filter by `y` in the WHERE clause
- This reduces container count but slightly complicates queries

Either approach works. Document the choice in the code.

### Save Implementation

- Use Cosmos DB transactional batch (`ContainerClient.NewTransactionalBatch`) for atomicity
- All items in a batch must share the same partition key — this works since all records share the same aggregate ID
- Transactional batches are limited to 100 operations — batch accordingly
- Atomicity is per-batch only; when len(records) > 100, earlier batches persist even if a later batch fails
- For each record: `batch.CreateItem` with a condition to prevent overwrite
- When TTL is configured, include the `t` field with the expiry epoch
- Check `ctx.Err()` before starting
- Save with no records should succeed (no-op)
- Encode `d` as base64 for JSON storage

### Load Implementation

- Query: `SELECT * FROM c WHERE c.a = @id AND c.y = 'e' ORDER BY c.v ASC`
- Use `ContainerClient.NewQueryItemsPager` with partition key
- Iterate all pages, build `[]*recordv1.Record`
- Decode `d` from base64
- Return empty `&historyv1.History{}` if no documents found

### LoadTail Implementation

- Query: `SELECT TOP @n * FROM c WHERE c.a = @id AND c.y = 'e' ORDER BY c.v DESC`
- Reverse the results to return ascending version order
- Or use a subquery: `SELECT * FROM (SELECT TOP @n ... ORDER BY c.v DESC) ORDER BY c.v ASC` (Cosmos DB supports this in some API versions)
- If n <= 0, return empty History immediately
- Paginate across pages until n records are collected

### SaveAggregate / LoadAggregate

- `SaveAggregate`: `UpsertItem` with partition key = aggregateId
- `LoadAggregate`: `ReadItem` by id = aggregateId; return `nil, 0, nil` if `StatusCode == 404`

### Functional Options

```go
type Option func(*CosmosDBStore)

func WithEventsContainer(name string) Option     // default: "events"
func WithAggregatesContainer(name string) Option // default: "aggregates"
func WithTenantPrefix(prefix string) Option      // prepends "prefix#" to aggregate IDs for multi-tenant containers
func WithTTL(ttl time.Duration) Option           // sets TTL on event documents; container must have TTL enabled
```

### TTL (Time To Live)

Use `WithTTL(duration)` to set an expiration on event documents. When configured, each saved event includes a `t` field containing the Unix epoch second at which the document should expire. Cosmos DB's native TTL feature handles automatic deletion.

**Requirements:**
- The events container must have a default TTL configured (e.g., `-1` for per-item TTL) — see [Cosmos DB TTL docs](https://learn.microsoft.com/en-us/azure/cosmos-db/nosql/time-to-live)
- Cosmos DB TTL uses a `ttl` property (seconds relative to `_ts`) or you can use an absolute epoch in a custom field and a Cosmos DB TTL policy
- TTL is only applied to events, not aggregates (aggregate state should persist)
- A zero or negative duration disables TTL (the default)

**Use cases:**
- Event expiration after snapshots: once a snapshot captures aggregate state, older events can age off
- Temporary/ephemeral aggregates with bounded lifetimes
- Cost control for high-volume event streams

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
- LoadTail with n <= 0 returns empty history

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

**TTL:**
- WithTTL sets TTL field with correct expiry epoch
- Without TTL, no TTL field is present

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

Use `stores/dynamodbstore/` as the primary reference — it shares the same document-oriented, short-field-name, TTL-enabled design. Also reference `stores/boltdbstore/` for interface implementation patterns and `stores/memorystore/` for test patterns.

## Build & Test

```bash
go test ./stores/cosmosdbstore/ -v -count=1
go vet ./stores/cosmosdbstore/
```

Ensure `go vet` is clean and there are no data races (`go test -race`).
