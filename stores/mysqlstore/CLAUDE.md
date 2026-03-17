# mysqlstore

> Part of the [protosource](../../CLAUDE.md) event sourcing framework.

Build a MySQL-backed implementation of the `protosource.Store`, `protosource.AggregateStore`, and `protosource.SnapshotTailStore` interfaces. Use `database/sql` with the `github.com/go-sql-driver/mysql` driver.

There is an existing implementation in this directory that can be used as a reference for the SQL patterns, but it predates the current interfaces (`AggregateStore`, `SnapshotTailStore`) and has issues (errors on empty save, missing timestamp column in INSERT, no aggregate storage). Start fresh.

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

- Package: `mysqlstore`
- Main type: `MySQLStore`
- Constructor: `New(db *sql.DB, opts ...Option) *MySQLStore`
- Accept an already-opened `*sql.DB` — the caller owns the lifecycle

### Table Schema

**Events table** (default name: `events`):

```sql
CREATE TABLE events (
    aggregate_id VARCHAR(255) NOT NULL,
    version      BIGINT       NOT NULL,
    data         MEDIUMBLOB   NOT NULL,
    PRIMARY KEY (aggregate_id, version)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

**Aggregates table** (default name: `aggregates`):

```sql
CREATE TABLE aggregates (
    aggregate_id VARCHAR(255) NOT NULL,
    version      BIGINT       NOT NULL,
    data         MEDIUMBLOB   NOT NULL,
    PRIMARY KEY (aggregate_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

### Save Implementation

- Use a transaction (`BeginTx`) for atomicity
- Batch insert records: `INSERT INTO events (aggregate_id, version, data) VALUES (?, ?, ?)`
- The composite primary key `(aggregate_id, version)` prevents duplicate versions
- Check `ctx.Err()` before starting
- Save with no records should succeed (no-op)

### Load Implementation

- `SELECT version, data FROM events WHERE aggregate_id = ? ORDER BY version ASC`
- Return empty `&historyv1.History{}` if no rows found

### LoadTail Implementation

- `SELECT version, data FROM events WHERE aggregate_id = ? ORDER BY version DESC LIMIT ?`
- Reverse the results in Go to return ascending version order
- Or use a subquery: `SELECT * FROM (SELECT ... ORDER BY version DESC LIMIT ?) sub ORDER BY version ASC`

### SaveAggregate / LoadAggregate

- `SaveAggregate`: `INSERT INTO aggregates ... ON DUPLICATE KEY UPDATE version = VALUES(version), data = VALUES(data)`
- `LoadAggregate`: `SELECT version, data FROM aggregates WHERE aggregate_id = ?`; return `nil, 0, nil` if `sql.ErrNoRows`

### Functional Options

```go
type Option func(*MySQLStore)

func WithEventsTable(name string) Option     // default: "events"
func WithAggregatesTable(name string) Option // default: "aggregates"
```

## Testing Strategy

Use `github.com/DATA-DOG/go-sqlmock` for unit tests, and optionally `testcontainers-go` for integration tests with a real MySQL instance.

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
- SaveAggregate overwrites previous state (ON DUPLICATE KEY UPDATE)
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

**MySQL-specific:**
- Duplicate version (primary key violation) returns error
- Large blob data (MEDIUMBLOB supports up to 16MB)

### Test Helper

```go
func newTestStore(t *testing.T, opts ...Option) (*MySQLStore, sqlmock.Sqlmock) {
    t.Helper()
    db, mock, err := sqlmock.New()
    require.NoError(t, err)
    t.Cleanup(func() { db.Close() })
    return New(db, opts...), mock
}
```

## DDL

Include a `ddl/` subdirectory with the SQL migration scripts for creating the tables.

## Reference Implementation

Use `stores/boltdbstore/` as the structural reference for interface implementation, and `stores/memorystore/` for the test patterns. The boltdbstore tests demonstrate the expected coverage areas.

## Build & Test

```bash
go test ./stores/mysqlstore/ -v -count=1
go vet ./stores/mysqlstore/
```

Ensure `go vet` is clean and there are no data races (`go test -race`).
