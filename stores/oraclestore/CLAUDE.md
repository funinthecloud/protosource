# oraclestore

> Part of the [protosource](../../CLAUDE.md) event sourcing framework.

Build an Oracle Database-backed implementation of the `protosource.Store`, `protosource.AggregateStore`, and `protosource.SnapshotTailStore` interfaces. Use `database/sql` with the `github.com/sijms/go-ora/v2` driver (pure Go, no Oracle client required).

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

- Package: `oraclestore`
- Main type: `OracleStore`
- Constructor: `New(db *sql.DB, opts ...Option) *OracleStore`
- Accept an already-opened `*sql.DB` — the caller owns the lifecycle

### Table Schema

**Events table** (default name: `EVENTS`):

```sql
CREATE TABLE events (
    aggregate_id VARCHAR2(255)  NOT NULL,
    version      NUMBER(19)     NOT NULL,
    data         BLOB           NOT NULL,
    CONSTRAINT events_pk PRIMARY KEY (aggregate_id, version)
);
```

**Aggregates table** (default name: `AGGREGATES`):

```sql
CREATE TABLE aggregates (
    aggregate_id VARCHAR2(255)  NOT NULL,
    version      NUMBER(19)     NOT NULL,
    data         BLOB           NOT NULL,
    CONSTRAINT aggregates_pk PRIMARY KEY (aggregate_id)
);
```

### Save Implementation

- Use a transaction (`BeginTx`) for atomicity
- Insert records: `INSERT INTO events (aggregate_id, version, data) VALUES (:1, :2, :3)`
- Oracle uses numbered bind variables (`:1`, `:2`, `:3`)
- The composite primary key prevents duplicate versions
- Check `ctx.Err()` before starting
- Save with no records should succeed (no-op)

### Load Implementation

- `SELECT version, data FROM events WHERE aggregate_id = :1 ORDER BY version ASC`
- Return empty `&historyv1.History{}` if no rows found

### LoadTail Implementation

- Use Oracle's `FETCH FIRST N ROWS ONLY` (12c+):
  `SELECT version, data FROM (SELECT version, data FROM events WHERE aggregate_id = :1 ORDER BY version DESC FETCH FIRST :2 ROWS ONLY) sub ORDER BY version ASC`
- Or use `ROWNUM` for pre-12c compatibility:
  `SELECT * FROM (SELECT version, data FROM events WHERE aggregate_id = :1 ORDER BY version DESC) WHERE ROWNUM <= :2`
  then reverse in Go
- Prefer the `FETCH FIRST` syntax if targeting 12c+

### SaveAggregate / LoadAggregate

- `SaveAggregate`: Use Oracle's `MERGE`:
  ```sql
  MERGE INTO aggregates a
  USING (SELECT :1 AS aggregate_id FROM dual) s
  ON (a.aggregate_id = s.aggregate_id)
  WHEN MATCHED THEN UPDATE SET version = :2, data = :3
  WHEN NOT MATCHED THEN INSERT (aggregate_id, version, data) VALUES (:1, :2, :3)
  ```
- `LoadAggregate`: `SELECT version, data FROM aggregates WHERE aggregate_id = :1`; return `nil, 0, nil` if `sql.ErrNoRows`

### Functional Options

```go
type Option func(*OracleStore)

func WithEventsTable(name string) Option     // default: "events"
func WithAggregatesTable(name string) Option // default: "aggregates"
```

## Testing Strategy

Use `github.com/DATA-DOG/go-sqlmock` for unit tests, and optionally `testcontainers-go` with `gvenzl/oracle-free` for integration tests.

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
- SaveAggregate overwrites previous state (MERGE)
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

**Oracle-specific:**
- Duplicate version (primary key violation) returns error
- BLOB handles binary data correctly

### Test Helper

```go
func newTestStore(t *testing.T, opts ...Option) (*OracleStore, sqlmock.Sqlmock) {
    t.Helper()
    db, mock, err := sqlmock.New()
    require.NoError(t, err)
    t.Cleanup(func() { db.Close() })
    return New(db, opts...), mock
}
```

## DDL

Include a `ddl/` subdirectory with the SQL scripts for creating the tables.

## Reference Implementation

Use `stores/boltdbstore/` as the structural reference for interface implementation, and `stores/memorystore/` for the test patterns. The boltdbstore tests demonstrate the expected coverage areas.

## Build & Test

```bash
go test ./stores/oraclestore/ -v -count=1
go vet ./stores/oraclestore/
```

Ensure `go vet` is clean and there are no data races (`go test -race`).
