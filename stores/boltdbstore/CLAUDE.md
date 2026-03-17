# boltdbstore

> Part of the [protosource](../../CLAUDE.md) event sourcing framework.

Build a BoltDB-backed implementation of the `protosource.Store` and `protosource.AggregateStore` interfaces. Use [bbolt](https://pkg.go.dev/go.etcd.io/bbolt) (`go.etcd.io/bbolt`), the maintained fork of the original BoltDB.

## Interfaces to Implement

From `protosource.go`:

```go
// Store — required
type Store interface {
    Save(ctx context.Context, aggregateID string, records ...*recordv1.Record) error
    Load(ctx context.Context, aggregateId string) (*historyv1.History, error)
}

// AggregateStore — optional but implement it
type AggregateStore interface {
    SaveAggregate(ctx context.Context, aggregateID string, data []byte, version int64) error
    LoadAggregate(ctx context.Context, aggregateID string) (data []byte, version int64, err error)
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

- Package: `boltdbstore`
- Main type: `BoltDBStore`
- Constructor: `New(db *bbolt.DB, opts ...Option) *BoltDBStore`
- Accept an already-opened `*bbolt.DB` — the caller owns the lifecycle (open/close). This matches how dynamodbstore accepts a client.

### Bucket Layout

Use two top-level buckets:

- **`events`** — event history per aggregate
  - Sub-bucket per aggregate ID (e.g., `events/order-123`)
  - Keys: version as 8-byte big-endian uint64 (for natural sort order via `binary.BigEndian.PutUint64`)
  - Values: `record.Data` bytes (the serialized event)
  - Also store version in the key so Load can reconstruct `recordv1.Record{Version: v, Data: data}`

- **`aggregates`** — materialized aggregate state
  - Keys: aggregate ID string
  - Values: proto-marshalled aggregate bytes (the `data` param from `SaveAggregate`)
  - Store version alongside the data. Simplest approach: prefix the value with 8 bytes of big-endian version, followed by the data bytes. Or use a sub-bucket with `data` and `version` keys.

### Save Implementation

- Use a single `db.Update` (read-write transaction) per Save call
- Create the aggregate's sub-bucket if it doesn't exist (`CreateBucketIfNotExists`)
- For each record, put `key=bigEndianVersion, value=record.Data`
- BoltDB transactions are serializable, so no explicit locking needed
- Check `ctx.Err()` before starting the transaction

### Load Implementation

- Use `db.View` (read-only transaction)
- Open the aggregate's sub-bucket; if it doesn't exist, return empty `&historyv1.History{}`
- Cursor over all keys in order, building `[]*recordv1.Record`
- Parse version from the 8-byte key, read data from value
- Check `ctx.Err()` before starting the transaction

### SaveAggregate / LoadAggregate

- Use the `aggregates` bucket
- `SaveAggregate`: `db.Update` → put key=aggregateID, value=version(8 bytes)+data
- `LoadAggregate`: `db.View` → get key=aggregateID, split version and data; return nil,0,nil if not found

### Functional Options

Follow the memorystore pattern:

```go
type Option func(*BoltDBStore)

func WithEventsBucket(name string) Option    // default: "events"
func WithAggregatesBucket(name string) Option // default: "aggregates"
```

Keep it simple — BoltDB doesn't need table names, TTLs, or tenant prefixes like DynamoDB. Add options only if they serve a purpose.

## Testing Strategy

Tests should be thorough. Use `t.TempDir()` for the DB file path so cleanup is automatic.

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

**Context handling:**
- Save with cancelled context returns error
- Load with cancelled context returns error
- SaveAggregate with cancelled context returns error
- LoadAggregate with cancelled context returns error

**Data integrity:**
- Record version and data survive round-trip
- Aggregate data and version survive round-trip
- Event history and aggregate state are independent (saving aggregate doesn't affect events)

**Concurrency:**
- Concurrent saves to different aggregates
- Concurrent reads and writes to same aggregate (BoltDB serializes writes, but reads can be concurrent with `View`)

**Edge cases:**
- Large number of records per aggregate (e.g., 1000)
- Empty data bytes
- Binary data with null bytes

### Test Helper

```go
func newTestStore(t *testing.T) *BoltDBStore {
    t.Helper()
    path := filepath.Join(t.TempDir(), "test.db")
    db, err := bbolt.Open(path, 0600, &bbolt.Options{Timeout: 1 * time.Second})
    if err != nil {
        t.Fatalf("open bolt db: %v", err)
    }
    t.Cleanup(func() { db.Close() })
    return New(db)
}
```

## Reference Implementation

Use `stores/memorystore/memorystore.go` and its test file as the structural reference. The memorystore tests demonstrate the expected patterns and coverage areas. The boltdbstore should pass equivalent tests with the same semantics.

## Build & Test

```bash
go test ./stores/boltdbstore/ -v -count=1
go vet ./stores/boltdbstore/
```

Ensure `go vet` is clean and there are no data races (`go test -race`).

## TODO

- [ ] Investigate shard rebalancing: when aggregates are deleted or event expiration thins out shards unevenly, consider a mechanism to consolidate or redistribute aggregates across shards to reclaim space and reduce file count
- [ ] Investigate distributing shards: explore placing shard files across multiple disks, network mounts, or machines to spread I/O load and storage capacity beyond a single filesystem
- [ ] Investigate Raft-based distributed cluster: use a Raft consensus library (e.g., hashicorp/raft) to replicate writes across nodes, partition shard ownership across the cluster, and expose a domain-specific API (Save, Load, LoadTail, SaveAggregate, LoadAggregate) rather than a generic KV interface — purpose-built distributed event store
