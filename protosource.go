package protosource

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	historyv1 "github.com/funinthecloud/protosource/history/v1"
	recordv1 "github.com/funinthecloud/protosource/record/v1"
	"github.com/funinthecloud/protosource/stores/memorystore"
	"google.golang.org/protobuf/proto"
)

// Repo represents the interface for a repository that can handle both commands and queries.
// It provides methods to apply commands and load aggregates.
type Repo interface {
	// Apply processes a command and returns the current version of the aggregate
	Apply(ctx context.Context, command Commander) (int64, error)
	// Load retrieves an aggregate by its ID from storage
	Load(ctx context.Context, aggregateID string) (Aggregate, error)
	// History returns the full event history for an aggregate
	History(ctx context.Context, aggregateID string) (*historyv1.History, error)
}

// Store provides an abstraction for the Repository to save and load data.
// It defines methods for saving serialized records and loading event histories.
type Store interface {
	// Save the provided serialized records to the store
	Save(ctx context.Context, aggregateID string, records ...*recordv1.Record) error

	// Load the history of events for this aggregateId
	Load(ctx context.Context, aggregateId string) (*historyv1.History, error)
}

// AggregateStore is an optional interface that stores can implement to persist
// the materialized aggregate state after each successful Apply. This provides a
// read-optimized view of the current aggregate without needing to replay events.
//
// Repository checks for this interface via type assertion after persisting events.
// If the store implements it, the fully materialized aggregate is passed directly,
// letting the store decide how to serialize, compress, and index it. Stores backed
// by NoSQL databases (DynamoDB, Cosmos, Firestore) can type-assert the aggregate
// to AutoPKSK for GSI-indexed single-table storage via opaquedata.
type AggregateStore interface {
	// SaveAggregate persists the materialized aggregate state.
	// The store owns serialization and key computation.
	//
	// This is a write-only interface — the repository does not read materialized
	// aggregates back (it always rebuilds from events via Load). The persisted
	// state is intended for external consumers (dashboards, APIs, projections)
	// that query the store directly. A LoadAggregate read path may be added in
	// the future.
	SaveAggregate(ctx context.Context, aggregate proto.Message) error
}

// SnapshotTailStore is an optional interface that stores can implement to
// support efficient partial event loading. When the aggregate has a snapshot
// interval, the repository calls LoadTail to retrieve only the last N events
// instead of the entire history. This is a single-call operation that every
// common backend (SQL, DynamoDB, Firestore, Cosmos DB, BoltDB) can implement
// natively and efficiently.
type SnapshotTailStore interface {
	// LoadTail returns the last n events for the given aggregate, ordered by
	// version ascending. If the aggregate has fewer than n events, all events
	// are returned. If the aggregate has no events, an empty History is returned.
	LoadTail(ctx context.Context, aggregateID string, n int) (*historyv1.History, error)
}

// Serializer provides methods to convert between Events and Records.
// This includes marshaling events to records or data bytes,
// and unmarshaling records or data bytes back into events.
type Serializer interface {
	// MarshalEvent converts an Event to a Record
	MarshalEvent(event Event) (*recordv1.Record, error)
	MarshalEventAsData(event Event) ([]byte, error)

	// UnmarshalEvent converts a Record back into an Event
	UnmarshalEvent(record *recordv1.Record) (Event, error)
	UnmarshalEventFromData(data []byte) (Event, error)
}

// Aggregate represents a domain object in the bounded context.
// It encapsulates the state of the domain object at a specific point in time,
// typically described as the sum of all events that have occurred on this object.
//
// Aggregates process and apply events via the On method.
// Events should never be rejected based on business rules, as they represent past occurrences.
// Errors from the On method indicate systemic failures in applying event data to the aggregate.
type Aggregate interface {
	proto.Message
	// On will be called for each event; returns err if the event could not be
	// applied due to a systemic failure (not based on business rules)
	On(event Event) error
	// GetVersion returns the current version of the aggregate
	GetVersion() int64
}

// Commander represents a command that specifies a desired change to an aggregate.
// It includes not only the action but also all necessary data required for that change.
//
// Commands can be rejected based on business logic rules. For example,
// a "Create" command would be rejected if the aggregate already exists.
type Commander interface {
	proto.Message
	// GetId returns the ID of the aggregate to which this command should be applied
	GetId() string
	// GetActor returns the identifier of the actor responsible for this change
	GetActor() string
}

// Event represents a change that has occurred in the past.
// It should be described using past tense to indicate that it's an event that already happened.
//
// Events encapsulate the details of changes, including the aggregate ID,
// version number, timestamp, and responsible actor.
type Event interface {
	proto.Message
	// GetId returns the ID of the aggregate referenced by this event
	GetId() string

	// GetVersion returns the version number of this event
	GetVersion() int64

	// GetAt indicates when the event occurred in Unix microseconds (timestamp)
	GetAt() int64

	// GetActor returns the identifier of the actor responsible for this change
	GetActor() string
}

// Repository provides the primary abstraction for saving and loading events.
// It uses a store to persist data and a serializer to convert between events and records.
type Repository struct {
	prototype         reflect.Type // The type of aggregate prototype used to create new instances
	store             Store        // Underlying storage for events
	serializer        Serializer   // Converts between events and records
	compressThreshold int          // 0 = disabled; >0 = compress data at or above this byte size
}

// New creates a new Repository with the given prototype and options.
// By default, it uses protobinaryserializer and memorystore if no specific store or serializer is provided.
func New(prototype Aggregate, opts ...Option) *Repository {
	t := reflect.TypeOf(prototype)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	r := &Repository{
		prototype: t,
		store:     memorystore.New(),
		//serializer: protobinaryserializer.NewSerializer(),
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// Option provides functional configuration options for the Repository
type Option func(*Repository)

// WithStore specifies the underlying store to use; by default, an in-memory store is used for testing
func WithStore(store Store) Option {
	return func(r *Repository) {
		r.store = store
	}
}

// WithSerializer sets the serializer to be used for converting between events and records
func WithSerializer(serializer Serializer) Option {
	return func(r *Repository) {
		r.serializer = serializer
	}
}

// WithCompression enables gzip compression for event record data stored by the
// repository. Record data at or above the threshold (in bytes) is compressed
// before writing to the store. Data below the threshold is stored uncompressed.
// Decompression is automatic on read (detected via gzip magic bytes), so
// compressed and uncompressed data can coexist safely.
//
// Note: this only affects event records (Save/Load). Materialized aggregate
// state is passed directly to AggregateStore, which owns its own serialization.
//
// Use 300 as a sensible starting threshold. Pass 0 or any negative value to
// disable compression (the default).
func WithCompression(threshold int) Option {
	return func(r *Repository) {
		r.compressThreshold = threshold
	}
}

// Load retrieves the aggregate with the specified ID from storage.
// It loads the event history and applies each event to rebuild the aggregate state.
func (r *Repository) Load(ctx context.Context, aggregateId string) (Aggregate, error) {
	a, _, err := r.loadAggregateVersion(ctx, aggregateId)
	if err != nil {
		return nil, fmt.Errorf("load aggregate version: %w", err)
	}
	return a, nil
}

// History returns the full event history for an aggregate, bypassing any snapshot
// tail optimization. This is intended for query endpoints that need the complete stream.
// Record data is transparently decompressed when compression is enabled.
func (r *Repository) History(ctx context.Context, aggregateID string) (*historyv1.History, error) {
	if aggregateID == "" {
		return nil, ErrEmptyAggregateId
	}
	history, err := r.store.Load(ctx, aggregateID)
	if err != nil {
		return nil, err
	}
	for _, record := range history.GetRecords() {
		decompressed, err := MaybeDecompress(record.Data)
		if err != nil {
			return nil, fmt.Errorf("decompress event: %w", err)
		}
		record.Data = decompressed
	}
	return history, nil
}

// Apply processes the given command and returns the current version of the aggregate.
// It runs the command through the generated validation pipeline:
//
//  1. VersionValidator — lifecycle gate (create requires version==0, mutation requires version>0)
//  2. ProtoValidater — annotation-driven field and cross-field constraints via buf/protovalidate
//  3. CommandAuthorizer — validate command against current aggregate state (state-machine transitions)
//  4. EventEmitter check — verify command can emit events (fail fast before custom logic)
//  5. CommandEvaluator — optional custom business logic (duplicate detection, idempotency, conditional no-ops)
//  6. EventEmitter — emit events
//  7. Persist — save events to store
func (r *Repository) Apply(ctx context.Context, command Commander) (int64, error) {
	if command == nil {
		return 0, ErrNilCommand
	}
	aggregateID := command.GetId()
	if aggregateID == "" {
		return 0, ErrEmptyAggregateId
	}

	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("context error: %w", err)
	}

	// Load the aggregate (or create a fresh one if it doesn't exist yet).
	aggregate, version, err := r.loadAggregateVersion(ctx, aggregateID)
	if err != nil {
		aggregate = r.new()
	}

	// 1. Validate lifecycle constraints (create vs update).
	if v, ok := command.(VersionValidator); ok {
		if err := v.ValidateVersion(aggregate.GetVersion()); err != nil {
			return 0, err
		}
	}

	// 2. Run proto-annotation validations (field constraints + CEL via protovalidate).
	if pv, ok := command.(ProtoValidater); ok {
		if err := pv.ProtoValidate(); err != nil {
			return 0, err
		}
	}

	// 3. Authorize the command against the current aggregate state.
	if a, ok := command.(CommandAuthorizer); ok {
		if err := a.Authorize(aggregate); err != nil {
			return 0, err
		}
	}

	// 4. Verify the command can emit events before evaluating custom logic.
	e, ok := command.(EventEmitter)
	if !ok {
		return 0, fmt.Errorf("%T: %w", command, ErrUnhandledCommand)
	}

	// 5. Evaluate the command against the current aggregate state (optional).
	// Return ErrSkip to silently skip event emission (idempotent no-op).
	if ce, ok := command.(CommandEvaluator); ok {
		if err := ce.Evaluate(aggregate); err != nil {
			if errors.Is(err, ErrSkip) {
				return version, nil
			}
			return 0, err
		}
	}

	// 6. Emit events.
	events := e.EmitEvents(aggregate)

	// 7. Persist events.
	if err := r.Save(ctx, events...); err != nil {
		return 0, err
	}

	if v := len(events); v > 0 {
		version = events[v-1].GetVersion()
	}

	// 8. Materialize aggregate (optional).
	// If the store supports aggregate storage, apply the new events to the
	// in-memory aggregate and persist its serialized state. This is best-effort:
	// event persistence (step 7) is the source of truth.
	if as, ok := r.store.(AggregateStore); ok {
		for _, event := range events {
			if err := aggregate.On(event); err != nil {
				return version, nil // events saved; aggregate store is best-effort
			}
		}
		_ = as.SaveAggregate(ctx, aggregate)
	}

	return version, nil
}

// Save persists the given events into the underlying Store.
// It serializes each event and saves them under the aggregate ID.
// When compression is enabled, record data at or above the threshold is
// gzip-compressed before writing.
func (r *Repository) Save(ctx context.Context, events ...Event) error {
	if len(events) == 0 {
		return nil
	}
	aggregateID := events[0].GetId()

	h := &historyv1.History{}
	for _, event := range events {
		record, err := r.serializer.MarshalEvent(event)
		if err != nil {
			return err
		}

		if r.compressThreshold > 0 {
			compressed, err := MaybeCompress(record.Data, r.compressThreshold)
			if err != nil {
				return fmt.Errorf("compress event: %w", err)
			}
			record.Data = compressed
		}

		h.Records = append(h.Records, record)
	}

	return r.store.Save(ctx, aggregateID, h.GetRecords()...)
}

var (
	// Error variables for common repository errors
	ErrAggregateNotFound = errors.New("aggregate not found")
	ErrUnhandledCommand  = errors.New("unhandled command")
	ErrUnhandledEvent    = errors.New("unhandled event")
	ErrAlreadyCreated    = errors.New("aggregate already created")
	ErrNotCreatedYet     = errors.New("aggregate not created yet")
	ErrNilCommand        = errors.New("command is nil")
	ErrEmptyAggregateId  = errors.New("aggregate id is empty")
	ErrValidationFailed  = errors.New("command validation failed")
	ErrUnauthorized      = errors.New("command not authorized")
	ErrSkip              = errors.New("command skipped")
)

// loadAggregateVersion loads the specified aggregate from storage and reconstructs it
// by applying all relevant events in sequence. It returns the aggregate, its version,
// and any error encountered during loading.
//
// When the store implements SnapshotAwareStore and the aggregate has a snapshot
// interval, only events from the most recent snapshot boundary are loaded.
func (r *Repository) loadAggregateVersion(ctx context.Context, aggregateId string) (Aggregate, int64, error) {
	history, err := r.loadHistory(ctx, aggregateId)
	if err != nil {
		return nil, 0, err
	}

	entryCount := len(history.GetRecords())
	if entryCount == 0 {
		return nil, 0, fmt.Errorf("unable to load: %s: %w", aggregateId, ErrAggregateNotFound)
	}

	aggregate := r.new()

	var version int64
	for _, record := range history.GetRecords() {
		version = record.GetVersion()

		decompressed, err := MaybeDecompress(record.Data)
		if err != nil {
			return nil, 0, fmt.Errorf("decompress event: %w", err)
		}
		record.Data = decompressed

		event, err := r.serializer.UnmarshalEvent(record)
		if err != nil {
			return nil, 0, err
		}

		err = aggregate.On(event)
		if err != nil {
			return nil, 0, fmt.Errorf("aggregate was unable to handle event type %T: %w", event, ErrUnhandledEvent)
		}
	}

	return aggregate, version, nil
}

// loadHistory returns the event history for an aggregate. If the store supports
// SnapshotTailStore and the aggregate prototype has a snapshot interval, it
// loads only the last interval-worth of events instead of the entire history.
func (r *Repository) loadHistory(ctx context.Context, aggregateId string) (*historyv1.History, error) {
	sts, isSTS := r.store.(SnapshotTailStore)
	if !isSTS {
		return r.store.Load(ctx, aggregateId)
	}

	// Check if the aggregate prototype supports snapshots.
	p := r.new()
	snapshoter, hasSnapshots := p.(Snapshoter)
	if !hasSnapshots || snapshoter.SnapshotInterval() <= 0 {
		return r.store.Load(ctx, aggregateId)
	}

	return sts.LoadTail(ctx, aggregateId, int(snapshoter.SnapshotInterval()))
}

// EventEmitter is implemented by generated command types that can produce events.
// The aggregate is passed in so that EmitEvents can read the current version
// and, if the aggregate implements Snapshoter, automatically append snapshots.
type EventEmitter interface {
	EmitEvents(aggregate Aggregate) []Event
}

// VersionValidator is implemented by generated command types with lifecycle constraints.
// CREATION commands require version == 0; MUTATION commands require version > 0.
type VersionValidator interface {
	ValidateVersion(version int64) error
}

// ProtoValidater validates a command using buf/protovalidate annotations.
// Generated automatically on every command message; calls protovalidate.Validate
// to enforce field constraints declared in the proto schema.
type ProtoValidater interface {
	ProtoValidate() error
}

// CommandAuthorizer validates a command against the current aggregate state.
// Use this for state-machine transitions, ownership checks, or any business
// rule that depends on the aggregate's current state rather than the command
// fields alone.
type CommandAuthorizer interface {
	Authorize(aggregate Aggregate) error
}

// CommandEvaluator is an optional interface that command types can implement
// to inspect the current aggregate state before events are emitted. This is
// the extension point for custom business logic such as duplicate detection,
// idempotency checks, or conditional no-ops.
//
// Return nil to proceed with event emission, ErrSkip to silently skip (no
// events persisted, no error returned to caller), or any other error to
// abort the command.
type CommandEvaluator interface {
	Evaluate(aggregate Aggregate) error
}

// Snapshoter interface enables aggregates to create snapshots at specific versions.
// A snapshot represents the state of an aggregate at a given point in time.
// SnapshotInterval returns the frequency (in events) at which snapshots are taken.
// A return value of 0 means snapshots are disabled.
type Snapshoter interface {
	Snapshot(version int64) Event
	SnapshotInterval() int32
}

// new creates a new instance of the aggregate based on the prototype type.
// This method is used to instantiate aggregates when loading or applying commands.
func (r *Repository) new() Aggregate {
	return reflect.New(r.prototype).Interface().(Aggregate)
}

// Request is a provider-agnostic representation of an incoming HTTP request.
// Cloud-specific adapters convert their native request types into this struct
// before calling generated handlers. The Actor field is pre-populated by the
// adapter's ActorExtractor — generated handlers never parse auth context.
type Request struct {
	Body            string
	PathParameters  map[string]string
	QueryParameters map[string]string
	Headers         map[string]string
	Actor           string
}

// Response is a provider-agnostic representation of an HTTP response.
// Cloud-specific adapters convert this back to their native response type.
type Response struct {
	StatusCode int
	Body       string
	Headers    map[string]string
}

// HandlerFunc is the signature for provider-agnostic request handlers.
type HandlerFunc func(ctx context.Context, request Request) Response

// Utility functions for time conversion

// NowMicros returns the current time in Unix microseconds
func NowMicros() int64 {
	return time.Now().UnixNano() / 1e3
}

// FromMicros converts Unix microseconds back to a time.Time object
func FromMicros(us int64) time.Time {
	sec := us / 1e6
	nsec := (us % 1e6) * 1e3
	return time.Unix(sec, nsec).UTC()
}
