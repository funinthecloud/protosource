# cosmosdbstore

> Part of the [protosource](../../CLAUDE.md) event sourcing framework.

Azure Cosmos DB (NoSQL API) implementation of `protosource.Store`, `protosource.AggregateStore`, and `protosource.SnapshotTailStore`. Cross-cloud counterpart of `stores/dynamodbstore` — same event sourcing semantics, same single-character attribute names, same opaquedata-backed aggregates with 20 GSI slot pairs.

Uses `github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos`.

## Layout

Two containers:

| Container | Partition key | Doc id | Purpose |
|-----------|---------------|--------|---------|
| `events` | `/a` (aggregate ID) | `strconv(version)` | Append-only event log. CreateItem rejects duplicate ids within the partition, enforcing version uniqueness. |
| `aggregates` | `/pk` (opaquedata pk) | `sk` | Materialized aggregates + opaquedata projections with 20 GSI slot pairs. |

The aggregates container is the opaquedata single-table — there is no separate opaque container.

## Document fields (events)

Single-letter names matching `stores/dynamodbstore`:

| Field | Purpose |
|-------|---------|
| `id` | Cosmos doc id (string form of version) |
| `a`  | partition key — aggregate ID |
| `v`  | version (number) |
| `d`  | payload bytes (base64 in JSON) |
| `t`  | absolute epoch seconds — app-level TTL filter |
| `ttl`| Cosmos-native relative seconds — auto-purge |

Both `t` and `ttl` are written when TTL is configured. `t` is what our query filter and opaquedata adapter inspect; `ttl` is what Cosmos uses to auto-delete. The aggregates container's documents use the opaquedata schema (`pk`/`sk`/`gsiNpk`/`gsiNsk`/etc.) — see `opaquedata/cosmos`.

## API

```go
package cosmosdbstore

type CosmosDBStore struct { /* ... */ }

func New(events cosmosclient.ContainerClient, opts ...Option) (*CosmosDBStore, error)

// Required for SaveAggregate. The opaquedata.OpaqueStore must target the
// aggregates container. Use opaquedata/cosmos to construct one.
func WithOpaqueStore(store opaquedata.OpaqueStore) Option

// Stamps both `t` (absolute epoch) and `ttl` (relative seconds) on each event.
func WithTTL(ttl time.Duration) Option

// Conflict detection for callers that want to retry on version collisions.
func IsConflict(err error) bool

// Idempotent infra setup.
func EnsureDatabase(ctx context.Context, client *azcosmos.Client, databaseID string) (*azcosmos.DatabaseClient, error)
func EnsureContainers(ctx context.Context, db *azcosmos.DatabaseClient, eventsContainer, aggregatesContainer string) error
```

`EnsureContainers` creates each container with `DefaultTimeToLive = -1` so per-item `ttl` is honored without expiring untagged items — mirrors the Dynamo TTL-on-attribute model.

## Key design choices

- **Per-partition id uniqueness** replaces conditional writes. The Cosmos rule "doc id is unique within a partition" combined with `CreateItem` semantics gives the same version-uniqueness guarantee Dynamo gets from `attribute_not_exists` conditions.
- **Transactional batches** (≤100 ops per partition) are the Cosmos analog of Dynamo's `TransactWriteItems`. The store batches automatically; atomicity holds within each batch, not across batches. The `azure/cosmosclient` package exposes this via `ExecuteCreateBatch`.
- **Single-partition reads** for `Load` and `LoadTail` — the partition key IS the aggregate ID, so all reads stay in one logical partition.
- **`LoadTail` early termination** via the `cosmosclient.Pager` interface. The store passes `PageSizeHint = n`, drains pages until n records are collected, then reverses to ascending order.
- **GSI queries are cross-partition** but main-PK queries stay single-partition. See `opaquedata/cosmos` for the routing logic.

## Wire bindings

`providers.go` exposes typed aliases so the Wire graph stays unambiguous when two `cosmosclient.ContainerClient` instances are needed:

```go
type EventsContainerClient cosmosclient.ContainerClient
type AggregatesContainerClient cosmosclient.ContainerClient
```

Consumers supply both clients (one per container) and use `cosmosdbstore.ProviderSet`.

## Testing

Unit tests use an in-memory `mockCosmos` implementing `cosmosclient.ContainerClient` — emulates per-partition id uniqueness, batch atomicity, query ordering, and a configurable page size for pager tests. Race-clean.

Coverage (22 store tests):

- Save: single/multi/append/no-op/batching >100/duplicate-version→IsConflict
- Load: empty / ordering / round-trip / non-existent
- LoadTail: last-N / fewer-than-N / non-existent / zero-or-negative / early termination across pages
- SaveAggregate: no-OpaqueStore error / with-OpaqueStore Put / TTL propagation / TTL overflow / independence from events
- TTL on events: absolute+relative both written / record-TTL overrides store-TTL / neither when not configured
- Cancelled context: all four methods

Live emulator integration tests are deferred to `cmd/testcosmos` + `cmd/testcosmos-setup` (step 3 of the Azure rollout).

## Build & Test

```bash
go test ./stores/cosmosdbstore/ -v -count=1
go test -race ./stores/cosmosdbstore/
go vet ./stores/cosmosdbstore/
```

## Reference Implementation

Use `stores/dynamodbstore/` as the structural reference — every semantic in cosmosdbstore has a matching one there. `opaquedata/cosmos` mirrors `opaquedata/dynamo`.
