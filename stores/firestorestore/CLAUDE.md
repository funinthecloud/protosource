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

### Field Names

**All Firestore document field names use single characters** to minimize per-document byte costs. Firestore charges per document read/write, and document size directly affects storage costs and read latency. Field names are stored in every document, so shorter names reduce both storage and bandwidth at scale.

| Field | Purpose | Type | Description |
|-------|---------|------|-------------|
| `a` | — | string | Aggregate ID (with optional tenant prefix) |
| `v` | — | number | Version number |
| `d` | — | bytes | Event/aggregate payload (Firestore has native `[]byte` support) |
| `t` | — | number | TTL epoch seconds (optional, events only) |

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
    r/                                   (subcollection, short for "records")
      <version as string>/               (document ID — zero-padded for ordering, e.g., "0000000001")
        v: int64                         (version number)
        d: []byte                        (event payload)
        t: int64                         (TTL epoch, optional)

<aggregates_collection>/                 (default: "aggregates")
  <aggregateID>/                         (document)
    v: int64                             (version number)
    d: []byte                            (aggregate state)
```

Using the aggregate ID as the document ID and events as a subcollection keeps queries scoped and efficient. Zero-pad version strings (e.g., `fmt.Sprintf("%019d", version)`) so document IDs sort lexicographically. The subcollection is named `r` (not `records`) to keep paths short — Firestore paths contribute to document reference size.

### Save Implementation

- Use a Firestore `WriteBatch` or transaction for atomicity
- Firestore batches support up to 500 operations — batch accordingly if needed
- Atomicity is per-batch only; when len(records) > 500, earlier batches persist even if a later batch fails
- For each record: `Set` a document in `events/<aggregateID>/r/<paddedVersion>`
- Use `Create` (not `Set`) if you want to enforce no-overwrite (returns `AlreadyExists` on duplicate)
- When TTL is configured, include the `t` field with the expiry epoch
- Check `ctx.Err()` before starting
- Save with no records should succeed (no-op)

### Load Implementation

- Query: `client.Collection("events").Doc(aggregateID).Collection("r").OrderBy("v", firestore.Asc).Documents(ctx)`
- Iterate all documents, build `[]*recordv1.Record`
- Paginate across all pages
- Return empty `&historyv1.History{}` if no documents found

### LoadTail Implementation

- Query: `client.Collection("events").Doc(aggregateID).Collection("r").OrderBy("v", firestore.Desc).Limit(n).Documents(ctx)`
- Reverse the results to return ascending version order
- This is a single indexed query — Firestore handles it natively
- If n <= 0, return empty History immediately
- Paginate across pages until n records are collected

### SaveAggregate / LoadAggregate

- `SaveAggregate`: `client.Collection("aggregates").Doc(aggregateID).Set(ctx, map[string]interface{}{"v": version, "d": data})`
- `LoadAggregate`: `client.Collection("aggregates").Doc(aggregateID).Get(ctx)`; return `nil, 0, nil` if `status.Code(err) == codes.NotFound`

### Functional Options

```go
type Option func(*FirestoreStore)

func WithEventsCollection(name string) Option     // default: "events"
func WithAggregatesCollection(name string) Option // default: "aggregates"
func WithTTL(ttl time.Duration) Option            // sets TTL field on event documents
```

### TTL (Time To Live)

Use `WithTTL(duration)` to set an expiration on event documents. When configured, each saved event includes a `t` field containing the Unix epoch second at which the document should expire.

**Requirements:**
- Firestore supports TTL natively via a [TTL policy](https://cloud.google.com/firestore/docs/ttl) configured on a timestamp field. Create a TTL policy on the `t` field for the events subcollection.
- Note: Firestore TTL expects a `Timestamp` type, so you may need to store `t` as a `time.Time` (which Firestore maps to a Timestamp) rather than a raw epoch integer. Adjust the field type accordingly.
- TTL is only applied to events, not aggregates (aggregate state should persist)
- A zero or negative duration disables TTL (the default)
- Firestore deletes expired documents asynchronously (typically within 24 hours of expiry)

**Use cases:**
- Event expiration after snapshots: once a snapshot captures aggregate state, older events can age off
- Temporary/ephemeral aggregates with bounded lifetimes
- Cost control for high-volume event streams

## Firestore Indexes

Firestore requires composite indexes for queries with ordering. The subcollection query `OrderBy("v", ...)` on a single field should work with the automatic single-field index. No manual composite index should be needed for these queries, but verify in testing.

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
- LoadTail with n <= 0 returns empty history

**Context handling:**
- All methods return error on cancelled context

**Data integrity:**
- Record version and data survive round-trip
- Aggregate data and version survive round-trip
- Event history and aggregate state are independent

**Firestore-specific:**
- Batch writes > 500 operations (if applicable)
- Document not found handling (codes.NotFound)

**TTL:**
- WithTTL sets TTL field with correct expiry
- Without TTL, no TTL field is present

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

Use `stores/dynamodbstore/` as the primary reference — it shares the same document-oriented, short-field-name, TTL-enabled design. Also reference `stores/boltdbstore/` for interface implementation patterns and `stores/memorystore/` for test patterns.

## Build & Test

```bash
go test ./stores/firestorestore/ -v -count=1
go vet ./stores/firestorestore/
```

Ensure `go vet` is clean and there are no data races (`go test -race`).
