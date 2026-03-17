# firestorestore

> Part of the [protosource](../../CLAUDE.md) event sourcing framework.

Build a Google Cloud Firestore-backed implementation of the `protosource.Store`, `protosource.AggregateStore`, and `protosource.SnapshotTailStore` interfaces. Use `cloud.google.com/go/firestore`.

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

- Package: `firestorestore`
- Main type: `FirestoreStore`
- Constructor: `New(client *firestore.Client, opts ...Option) *FirestoreStore`
- Accept an already-created `*firestore.Client` — the caller owns the lifecycle

### Collection Structure

Firestore is a document database with collections and subcollections:

```
<events_collection>/                     (default: "events")
  <aggregateID>/                         (document — can be empty or hold metadata)
    records/                             (subcollection)
      <version as string>/               (document ID — zero-padded for ordering, e.g., "0000000001")
        version: int64
        data: []byte

<aggregates_collection>/                 (default: "aggregates")
  <aggregateID>/                         (document)
    version: int64
    data: []byte
```

Using the aggregate ID as the document ID and events as a subcollection keeps queries scoped and efficient. Zero-pad version strings (e.g., `fmt.Sprintf("%019d", version)`) so document IDs sort lexicographically.

### Save Implementation

- Use a Firestore `WriteBatch` or transaction for atomicity
- Firestore batches support up to 500 operations — batch accordingly if needed
- For each record: `Set` a document in `events/<aggregateID>/records/<paddedVersion>`
- Use `Create` (not `Set`) if you want to enforce no-overwrite (returns `AlreadyExists` on duplicate)
- Check `ctx.Err()` before starting
- Save with no records should succeed (no-op)

### Load Implementation

- Query: `client.Collection("events").Doc(aggregateID).Collection("records").OrderBy("version", firestore.Asc).Documents(ctx)`
- Iterate all documents, build `[]*recordv1.Record`
- Return empty `&historyv1.History{}` if no documents found

### LoadTail Implementation

- Query: `client.Collection("events").Doc(aggregateID).Collection("records").OrderBy("version", firestore.Desc).Limit(n).Documents(ctx)`
- Reverse the results to return ascending version order
- This is a single indexed query — Firestore handles it natively

### SaveAggregate / LoadAggregate

- `SaveAggregate`: `client.Collection("aggregates").Doc(aggregateID).Set(ctx, map[string]interface{}{"version": version, "data": data})`
- `LoadAggregate`: `client.Collection("aggregates").Doc(aggregateID).Get(ctx)`; return `nil, 0, nil` if `status.Code(err) == codes.NotFound`

### Functional Options

```go
type Option func(*FirestoreStore)

func WithEventsCollection(name string) Option     // default: "events"
func WithAggregatesCollection(name string) Option // default: "aggregates"
```

## Firestore Indexes

Firestore requires composite indexes for queries with ordering. The subcollection query `OrderBy("version", ...)` on a single field should work with the automatic single-field index. No manual composite index should be needed for these queries, but verify in testing.

## Testing Strategy

Use the Firestore emulator for tests. The emulator runs locally and provides a real Firestore API without GCP credentials.

```bash
# Start the emulator (in a separate terminal or CI step)
gcloud emulators firestore start --host-port=localhost:8086

# Set the environment variable before running tests
export FIRESTORE_EMULATOR_HOST=localhost:8086
```

Alternatively, define a `Firestorer` interface wrapping the Firestore client methods you use, and mock it for pure unit tests.

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

**Firestore-specific:**
- Batch writes > 500 operations (if applicable)
- Document not found handling (codes.NotFound)

### Test Helper

```go
func newTestStore(t *testing.T, opts ...Option) *FirestoreStore {
    t.Helper()
    ctx := context.Background()
    client, err := firestore.NewClient(ctx, "test-project")
    require.NoError(t, err)
    t.Cleanup(func() { client.Close() })
    store := New(client, opts...)
    return store
}
```

## Reference Implementation

Use `stores/boltdbstore/` as the structural reference for interface implementation, and `stores/memorystore/` for the test patterns. The boltdbstore tests demonstrate the expected coverage areas.

## Build & Test

```bash
go test ./stores/firestorestore/ -v -count=1
go vet ./stores/firestorestore/
```

Ensure `go vet` is clean and there are no data races (`go test -race`).
