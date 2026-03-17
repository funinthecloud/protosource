# mssqlstore

> Part of the [protosource](../../CLAUDE.md) event sourcing framework.

Build a Microsoft SQL Server-backed implementation of the `protosource.Store`, `protosource.AggregateStore`, and `protosource.SnapshotTailStore` interfaces. Use `database/sql` with the `github.com/microsoft/go-mssqldb` driver.

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

- Package: `mssqlstore`
- Main type: `MSSQLStore`
- Constructor: `New(db *sql.DB, opts ...Option) *MSSQLStore`
- Accept an already-opened `*sql.DB` — the caller owns the lifecycle

### Table Schema

**Events table** (default name: `events`):

```sql
CREATE TABLE events (
    aggregate_id NVARCHAR(255) NOT NULL,
    version      BIGINT        NOT NULL,
    data         VARBINARY(MAX) NOT NULL,
    CONSTRAINT PK_events PRIMARY KEY (aggregate_id, version)
);
```

**Aggregates table** (default name: `aggregates`):

```sql
CREATE TABLE aggregates (
    aggregate_id NVARCHAR(255)  NOT NULL,
    version      BIGINT         NOT NULL,
    data         VARBINARY(MAX) NOT NULL,
    CONSTRAINT PK_aggregates PRIMARY KEY (aggregate_id)
);
```

### Save Implementation

- Use a transaction (`BeginTx`) for atomicity
- Insert records: `INSERT INTO events (aggregate_id, version, data) VALUES (@p1, @p2, @p3)`
- SQL Server uses `@p1`, `@p2`, etc. as parameter placeholders with `go-mssqldb`
- The composite primary key prevents duplicate versions
- Check `ctx.Err()` before starting
- Save with no records should succeed (no-op)

### Load Implementation

- `SELECT version, data FROM events WHERE aggregate_id = @p1 ORDER BY version ASC`
- Return empty `&historyv1.History{}` if no rows found

### LoadTail Implementation

- Use a subquery with `TOP`:
  `SELECT version, data FROM (SELECT TOP (@p2) version, data FROM events WHERE aggregate_id = @p1 ORDER BY version DESC) sub ORDER BY version ASC`
- Or query DESC + reverse in Go

### SaveAggregate / LoadAggregate

- `SaveAggregate`: Use SQL Server's `MERGE`:
  ```sql
  MERGE INTO aggregates WITH (HOLDLOCK) AS target
  USING (SELECT @p1 AS aggregate_id) AS source
  ON target.aggregate_id = source.aggregate_id
  WHEN MATCHED THEN UPDATE SET version = @p2, data = @p3
  WHEN NOT MATCHED THEN INSERT (aggregate_id, version, data) VALUES (@p1, @p2, @p3);
  ```
- `LoadAggregate`: `SELECT version, data FROM aggregates WHERE aggregate_id = @p1`; return `nil, 0, nil` if `sql.ErrNoRows`

### Functional Options

```go
type Option func(*MSSQLStore)

func WithEventsTable(name string) Option     // default: "events"
func WithAggregatesTable(name string) Option // default: "aggregates"
```

## Testing Strategy

Use `github.com/DATA-DOG/go-sqlmock` for unit tests, and optionally `testcontainers-go` with `mcr.microsoft.com/mssql/server` for integration tests.

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

**SQL Server-specific:**
- Duplicate version (primary key violation) returns error
- VARBINARY(MAX) handles binary data with null bytes correctly

### Test Helper

```go
func newTestStore(t *testing.T, opts ...Option) (*MSSQLStore, sqlmock.Sqlmock) {
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
go test ./stores/mssqlstore/ -v -count=1
go vet ./stores/mssqlstore/
```

Ensure `go vet` is clean and there are no data races (`go test -race`).
