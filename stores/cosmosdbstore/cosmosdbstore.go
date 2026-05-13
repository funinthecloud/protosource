// Package cosmosdbstore is the Azure Cosmos DB (NoSQL API) implementation of
// protosource.Store, protosource.AggregateStore, and
// protosource.SnapshotTailStore. It is the cross-cloud counterpart of
// stores/dynamodbstore — same event sourcing semantics, same single-character
// attribute names ("a", "v", "d", "t"), same opaquedata-backed aggregates
// container with 20 GSI slot pairs.
//
// Cosmos differs from DynamoDB in three ways that shape this code:
//
//  1. There is no per-row conditional write. The version-uniqueness guarantee
//     comes from Cosmos's rule that document `id` must be unique within a
//     partition; we set `id = strconv(version)` and use CreateItem semantics
//     (via TransactionalBatch) so a duplicate version returns HTTP 409.
//  2. TTL is relative (seconds remaining), not absolute (epoch). The on-wire
//     document stores both: `t` keeps the absolute epoch (so the app-level
//     TTL filter still works) and `ttl` carries Cosmos-native auto-purge.
//  3. Transactional batches are partition-local — same as Dynamo's
//     TransactWriteItems requiring a single table — and capped at 100 ops,
//     so the batching logic carries over directly.
package cosmosdbstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
	"github.com/funinthecloud/protosource"
	"github.com/funinthecloud/protosource/azure/cosmosclient"
	historyv1 "github.com/funinthecloud/protosource/history/v1"
	"github.com/funinthecloud/protosource/opaquedata"
	recordv1 "github.com/funinthecloud/protosource/record/v1"
	"google.golang.org/protobuf/proto"
)

const (
	attrPartitionKey = "a" // partition key path: "/a"
	attrSortKey      = "v" // numeric version
	attrData         = "d" // payload bytes (base64 in JSON)
	attrTTL          = "t" // absolute epoch seconds (app-level filter)

	DefaultEventsContainer     = "events"
	DefaultAggregatesContainer = "aggregates"

	// maxBatchItems is the Cosmos transactional batch ceiling. Matches the
	// DynamoDB TransactWriteItems cap so the batching logic carries 1:1.
	maxBatchItems = 100
)

// CosmosDBStore implements the protosource Store, AggregateStore, and
// SnapshotTailStore interfaces backed by Cosmos DB (NoSQL API).
type CosmosDBStore struct {
	events      cosmosclient.ContainerClient
	opaqueStore opaquedata.OpaqueStore // required for SaveAggregate
	ttl         time.Duration          // when non-zero, stamps TTL on event writes
}

// New creates a new CosmosDBStore. The events client targets the events
// container; the aggregates container is supplied via WithOpaqueStore as a
// separately wired OpaqueStore.
func New(events cosmosclient.ContainerClient, opts ...Option) (*CosmosDBStore, error) {
	if events == nil {
		return nil, fmt.Errorf("cosmosdbstore.New: events client must not be nil")
	}
	s := &CosmosDBStore{events: events}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// Option configures a CosmosDBStore.
type Option func(*CosmosDBStore)

// WithOpaqueStore sets the OpaqueStore used by SaveAggregate to persist
// materialized aggregates against the aggregates container. All aggregates
// must implement opaquedata.AutoPKSK to be materialized.
func WithOpaqueStore(store opaquedata.OpaqueStore) Option {
	return func(s *CosmosDBStore) { s.opaqueStore = store }
}

// WithTTL sets a time-to-live duration for event records. Each saved event
// includes both `t` (absolute epoch — feeds the app-level TTL query filter)
// and `ttl` (relative seconds — feeds Cosmos automatic purge). The container
// must be created with DefaultTimeToLive = -1 (see containers.go) for per-item
// `ttl` to be honored.
//
// A zero or negative duration disables TTL stamping (the default).
func WithTTL(ttl time.Duration) Option {
	return func(s *CosmosDBStore) { s.ttl = ttl }
}

// eventDoc is the on-wire shape of an event row.
//
// Three identity-related fields look redundant but each serves a distinct
// purpose dictated by Cosmos's data model:
//
//   - `a` — partition key value: the aggregate ID. Cosmos hashes this to
//     route the document to a physical partition. Every query for the
//     aggregate's events runs against this single partition (cheap).
//   - `v` — numeric version (int64). Used by SQL queries: `ORDER BY c.v`,
//     range filters, etc. Domain-meaningful.
//   - `id` — required Cosmos document id, a *string*. Cosmos guarantees
//     (id, partitionKey) is unique. We set `id = strconv(v)` so a second
//     write at the same version fails with HTTP 409 — this is the
//     Cosmos analog of Dynamo's `attribute_not_exists` conditional write
//     and gives us the version-uniqueness invariant the event store
//     depends on.
//
// `id` is a string because Cosmos requires it; `v` is a number because
// SQL ordering on a stringified number sorts lexicographically (10 < 2).
// They carry the same information in different shapes for different
// engine constraints.
type eventDoc struct {
	ID  string `json:"id"` // Cosmos doc id — strconv(v). Enforces per-partition version uniqueness.
	A   string `json:"a"`  // partition key — aggregate ID.
	V   int64  `json:"v"`  // numeric version — used for ORDER BY / range queries.
	D   []byte `json:"d,omitempty"`
	T   int64  `json:"t,omitempty"`
	TTL int64  `json:"ttl,omitempty"`
}

// Save stores records for the given aggregate ID. Each batch of up to 100
// records is written atomically using a Cosmos transactional batch keyed on
// the aggregate ID partition. Duplicate versions surface as HTTP 409
// conflicts (CreateItem rejects duplicate doc IDs within a partition).
//
// When len(records) exceeds 100, Save splits the work into multiple
// transactions. Atomicity holds within each batch, not across batches — same
// semantics as the DynamoDB store.
//
// Saving zero records is a no-op.
func (s *CosmosDBStore) Save(ctx context.Context, aggregateID string, records ...*recordv1.Record) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("cosmosdbstore.Save: %w", err)
	}
	if len(records) == 0 {
		return nil
	}

	pk := azcosmos.NewPartitionKeyString(aggregateID)

	for i := 0; i < len(records); i += maxBatchItems {
		end := i + maxBatchItems
		if end > len(records) {
			end = len(records)
		}
		batch := records[i:end]

		// Recapture per batch: large writes may span hundreds of ms between
		// transactions, and the relative `ttl` (seconds remaining) must
		// reflect the actual write time, not the wall clock at the start
		// of Save. Otherwise later batches would auto-purge later than
		// intended.
		now := time.Now()

		items := make([][]byte, len(batch))
		for j, rec := range batch {
			doc := eventDoc{
				ID: strconv.FormatInt(rec.GetVersion(), 10),
				A:  aggregateID,
				V:  rec.GetVersion(),
				D:  rec.GetData(),
			}
			// Per-record TTL: prefer the record's own ttl when set, else
			// fall back to the store-level TTL window. Both fields are
			// derived from the same absolute epoch so Cosmos's auto-purge
			// (`ttl`, relative seconds) never fires before the app-level
			// filter (`t`, absolute epoch) would have allowed reads.
			switch {
			case rec.GetTtl() > 0:
				doc.T = rec.GetTtl()
			case s.ttl > 0:
				doc.T = now.Add(s.ttl).Unix()
			}
			if doc.T > 0 {
				doc.TTL = doc.T - now.Unix()
				if doc.TTL < 1 {
					doc.TTL = 1
				}
			}
			body, err := json.Marshal(doc)
			if err != nil {
				return fmt.Errorf("cosmosdbstore.Save: marshal record %d: %w", rec.GetVersion(), err)
			}
			items[j] = body
		}

		if err := s.events.ExecuteCreateBatch(ctx, pk, items); err != nil {
			return fmt.Errorf("cosmosdbstore.Save: %w", err)
		}
	}

	return nil
}

// Load retrieves the full event history for the given aggregate ID in
// ascending version order. The query is partition-scoped (single partition),
// the cheapest read pattern Cosmos supports.
func (s *CosmosDBStore) Load(ctx context.Context, aggregateID string) (*historyv1.History, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cosmosdbstore.Load: %w", err)
	}

	const query = "SELECT * FROM c WHERE c.a = @id ORDER BY c.v ASC"
	pk := azcosmos.NewPartitionKeyString(aggregateID)
	opts := &azcosmos.QueryOptions{
		QueryParameters: []azcosmos.QueryParameter{{Name: "@id", Value: aggregateID}},
	}

	pages, err := s.events.QueryItems(ctx, query, pk, opts)
	if err != nil {
		return nil, fmt.Errorf("cosmosdbstore.Load: %w", err)
	}

	history := &historyv1.History{}
	for _, raw := range pages {
		rec, err := docToRecord(raw)
		if err != nil {
			return nil, fmt.Errorf("cosmosdbstore.Load: %w", err)
		}
		history.Records = append(history.Records, rec)
	}
	return history, nil
}

// LoadTail returns the last n events for the given aggregate, ordered by
// version ascending. It queries in descending order with PageSizeHint = n
// and terminates early once n records are collected — analogous to Dynamo's
// `ScanIndexForward: false` + `Limit: n` pattern.
//
// If n <= 0, an empty History is returned immediately.
func (s *CosmosDBStore) LoadTail(ctx context.Context, aggregateID string, n int) (*historyv1.History, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cosmosdbstore.LoadTail: %w", err)
	}
	if n <= 0 {
		return &historyv1.History{}, nil
	}

	const query = "SELECT * FROM c WHERE c.a = @id ORDER BY c.v DESC"
	pk := azcosmos.NewPartitionKeyString(aggregateID)
	pageHint := int32(n)
	if n > math.MaxInt32 {
		pageHint = math.MaxInt32
	}
	opts := &azcosmos.QueryOptions{
		PageSizeHint:    pageHint,
		QueryParameters: []azcosmos.QueryParameter{{Name: "@id", Value: aggregateID}},
	}

	pager := s.events.NewQueryItemsPager(query, pk, opts)
	history := &historyv1.History{}
	remaining := n
	for remaining > 0 && pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("cosmosdbstore.LoadTail: %w", err)
		}
		for _, raw := range page {
			if remaining <= 0 {
				break
			}
			rec, err := docToRecord(raw)
			if err != nil {
				return nil, fmt.Errorf("cosmosdbstore.LoadTail: %w", err)
			}
			history.Records = append(history.Records, rec)
			remaining--
		}
	}

	// Reverse to ascending version order to match Dynamo's LoadTail contract.
	for i, j := 0, len(history.Records)-1; i < j; i, j = i+1, j-1 {
		history.Records[i], history.Records[j] = history.Records[j], history.Records[i]
	}

	return history, nil
}

// SaveAggregate persists the materialized aggregate state via the OpaqueStore.
// The aggregate must implement opaquedata.AutoPKSK and an OpaqueStore must be
// configured via WithOpaqueStore. The aggregates container uses pk/sk keys
// with 20 GSI slot pairs for query access patterns.
func (s *CosmosDBStore) SaveAggregate(ctx context.Context, aggregate proto.Message) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("cosmosdbstore.SaveAggregate: %w", err)
	}
	if s.opaqueStore == nil {
		return fmt.Errorf("cosmosdbstore.SaveAggregate: no OpaqueStore configured (use WithOpaqueStore)")
	}
	apk, ok := aggregate.(opaquedata.AutoPKSK)
	if !ok {
		return fmt.Errorf("cosmosdbstore.SaveAggregate: aggregate %T does not implement opaquedata.AutoPKSK", aggregate)
	}
	var opts []opaquedata.Option
	if ttler, ok := aggregate.(protosource.EventTTLer); ok && ttler.EventTTLSeconds() > 0 {
		ttlSec := ttler.EventTTLSeconds()
		if ttlSec > math.MaxInt64/int64(time.Second) {
			return fmt.Errorf("cosmosdbstore.SaveAggregate: event_ttl_seconds %d overflows time.Duration", ttlSec)
		}
		opts = append(opts, opaquedata.WithTTL(time.Duration(ttlSec)*time.Second))
	}
	od, err := opaquedata.NewOpaqueDataFromProto(apk, opts...)
	if err != nil {
		return fmt.Errorf("cosmosdbstore.SaveAggregate: opaquedata: %w", err)
	}
	if err := s.opaqueStore.Put(ctx, od); err != nil {
		return fmt.Errorf("cosmosdbstore.SaveAggregate: %w", err)
	}
	return nil
}

// docToRecord parses an event document JSON blob into a recordv1.Record.
func docToRecord(raw []byte) (*recordv1.Record, error) {
	var doc eventDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("unmarshal event doc: %w", err)
	}
	return &recordv1.Record{
		Version: doc.V,
		Data:    doc.D,
	}, nil
}

// IsConflict reports whether err came from a duplicate-version write. Useful
// for callers that want to distinguish concurrent-writer collisions from
// transient infra failures. Mirrors errors.Is semantics; works on errors
// returned by Save.
func IsConflict(err error) bool {
	if cosmosclient.IsBatchConflict(err) {
		return true
	}
	var rerr *azcore.ResponseError
	if errors.As(err, &rerr) {
		return rerr.StatusCode == http.StatusConflict
	}
	return false
}
