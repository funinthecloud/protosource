package protosource_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/funinthecloud/protosource"
	samplenov1 "github.com/funinthecloud/protosource/example/app/samplenosnapshot/v1"
	testv1 "github.com/funinthecloud/protosource/example/app/test/v1"
	historyv1 "github.com/funinthecloud/protosource/history/v1"
	recordv1 "github.com/funinthecloud/protosource/record/v1"
	"github.com/funinthecloud/protosource/serializers/protobinaryserializer"
	"github.com/funinthecloud/protosource/stores/memorystore"
)

// newTestRepo creates a Repository wired to the test domain with memorystore and protobinary serializer.
func newTestRepo(opts ...memorystore.Option) *protosource.Repository {
	store := memorystore.New(opts...)
	return protosource.New(
		&testv1.Test{},
		protosource.WithStore(store),
		protosource.WithSerializer(protobinaryserializer.NewSerializer()),
	)
}

// --- Apply tests ---

func TestApply_NilCommand(t *testing.T) {
	repo := newTestRepo()
	_, err := repo.Apply(context.Background(), nil)
	if !errors.Is(err, protosource.ErrNilCommand) {
		t.Fatalf("expected ErrNilCommand, got: %v", err)
	}
}

func TestApply_EmptyAggregateId(t *testing.T) {
	repo := newTestRepo()
	cmd := &testv1.Create{Id: "", Actor: "actor", Body: "body"}
	_, err := repo.Apply(context.Background(), cmd)
	if !errors.Is(err, protosource.ErrEmptyAggregateId) {
		t.Fatalf("expected ErrEmptyAggregateId, got: %v", err)
	}
}

func TestApply_CancelledContext(t *testing.T) {
	repo := newTestRepo()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := &testv1.Create{Id: "id-1", Actor: "actor", Body: "body"}
	_, err := repo.Apply(ctx, cmd)
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

func TestApply_Create(t *testing.T) {
	repo := newTestRepo()
	// Create emits Created + Unlocked (2 events), so version is 2
	cmd := &testv1.Create{Id: "id-1", Actor: "actor", Body: "hello"}
	version, err := repo.Apply(context.Background(), cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != 2 {
		t.Fatalf("expected version 2, got %d", version)
	}
}

func TestApply_CreateThenUpdate(t *testing.T) {
	repo := newTestRepo()
	ctx := context.Background()

	_, err := repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "hello"})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Create=v1,v2; Updated(v3) hits interval=3 → Snapshot(v4)
	version, err := repo.Apply(ctx, &testv1.Update{Id: "id-1", Actor: "actor", Body: "updated"})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if version != 4 {
		t.Fatalf("expected version 4, got %d", version)
	}
}

func TestApply_AlreadyCreated(t *testing.T) {
	repo := newTestRepo()
	ctx := context.Background()

	_, err := repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "hello"})
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}

	_, err = repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "hello again"})
	if !errors.Is(err, protosource.ErrAlreadyCreated) {
		t.Fatalf("expected ErrAlreadyCreated, got: %v", err)
	}
}

func TestApply_NotCreatedYet(t *testing.T) {
	repo := newTestRepo()
	_, err := repo.Apply(context.Background(), &testv1.Update{Id: "id-1", Actor: "actor", Body: "nope"})
	if !errors.Is(err, protosource.ErrNotCreatedYet) {
		t.Fatalf("expected ErrNotCreatedYet, got: %v", err)
	}
}

// --- Authorization tests ---

func TestApply_UpdateRejectedWhenLocked(t *testing.T) {
	repo := newTestRepo()
	ctx := context.Background()

	_, _ = repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "hello"})
	_, _ = repo.Apply(ctx, &testv1.Lock{Id: "id-1", Actor: "actor"})

	_, err := repo.Apply(ctx, &testv1.Update{Id: "id-1", Actor: "actor", Body: "nope"})
	if !errors.Is(err, protosource.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got: %v", err)
	}
}

func TestApply_UpdateAllowedWhenUnlocked(t *testing.T) {
	repo := newTestRepo()
	ctx := context.Background()

	_, _ = repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "hello"})

	// Create=v1,v2; Updated(v3)→Snapshot(v4)
	version, err := repo.Apply(ctx, &testv1.Update{Id: "id-1", Actor: "actor", Body: "updated"})
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if version != 4 {
		t.Fatalf("expected version 4, got %d", version)
	}
}

func TestApply_LockThenUnlockThenUpdate(t *testing.T) {
	repo := newTestRepo()
	ctx := context.Background()

	_, _ = repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "hello"})   // v1,v2
	_, _ = repo.Apply(ctx, &testv1.Lock{Id: "id-1", Actor: "actor"})                     // Locked(v3)→Snap(v4)
	_, _ = repo.Apply(ctx, &testv1.Unlock{Id: "id-1", Actor: "actor"})                   // Unlocked(v5)

	// After unlock, update should succeed: Updated(v6)→Snap(v7)
	version, err := repo.Apply(ctx, &testv1.Update{Id: "id-1", Actor: "actor", Body: "updated"})
	if err != nil {
		t.Fatalf("expected success after unlock, got: %v", err)
	}
	if version != 7 {
		t.Fatalf("expected version 7, got %d", version)
	}
}

func TestApply_LockAlreadyLocked(t *testing.T) {
	repo := newTestRepo()
	ctx := context.Background()

	_, _ = repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "hello"})
	_, _ = repo.Apply(ctx, &testv1.Lock{Id: "id-1", Actor: "actor"})

	_, err := repo.Apply(ctx, &testv1.Lock{Id: "id-1", Actor: "actor"})
	if !errors.Is(err, protosource.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for double lock, got: %v", err)
	}
}

func TestApply_UnlockWhenNotLocked(t *testing.T) {
	repo := newTestRepo()
	ctx := context.Background()

	_, _ = repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "hello"})

	_, err := repo.Apply(ctx, &testv1.Unlock{Id: "id-1", Actor: "actor"})
	if !errors.Is(err, protosource.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for unlock when not locked, got: %v", err)
	}
}

// --- ProtoValidation tests ---

func TestApply_ProtoValidateRejectsEmptyActor(t *testing.T) {
	repo := newTestRepo()
	cmd := &testv1.Create{Id: "id-1", Actor: "", Body: "hello"}
	_, err := repo.Apply(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error for empty actor, got nil")
	}
	if !errors.Is(err, protosource.ErrValidationFailed) {
		t.Fatalf("expected ErrValidationFailed, got: %v", err)
	}
}

func TestApply_ProtoValidateRejectsEmptyBody(t *testing.T) {
	repo := newTestRepo()
	cmd := &testv1.Create{Id: "id-1", Actor: "actor", Body: ""}
	_, err := repo.Apply(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error for empty body, got nil")
	}
	if !errors.Is(err, protosource.ErrValidationFailed) {
		t.Fatalf("expected ErrValidationFailed, got: %v", err)
	}
}

// --- Load tests ---

func TestLoad_AggregateNotFound(t *testing.T) {
	repo := newTestRepo()
	_, err := repo.Load(context.Background(), "nonexistent")
	if !errors.Is(err, protosource.ErrAggregateNotFound) {
		t.Fatalf("expected ErrAggregateNotFound, got: %v", err)
	}
}

func TestLoad_AfterCreate(t *testing.T) {
	repo := newTestRepo()
	ctx := context.Background()

	_, err := repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "hello"})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	agg, err := repo.Load(ctx, "id-1")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	test, ok := agg.(*testv1.Test)
	if !ok {
		t.Fatalf("expected *testv1.Test, got %T", agg)
	}
	if test.GetId() != "id-1" {
		t.Errorf("expected id 'id-1', got %q", test.GetId())
	}
	// Create emits Created(v1) + Unlocked(v2)
	if test.GetVersion() != 2 {
		t.Errorf("expected version 2, got %d", test.GetVersion())
	}
	if test.GetBody() != "hello" {
		t.Errorf("expected body 'hello', got %q", test.GetBody())
	}
	if test.GetState() != testv1.State_STATE_UNLOCKED {
		t.Errorf("expected STATE_UNLOCKED, got %s", test.GetState())
	}
}

func TestLoad_AfterMultipleUpdates(t *testing.T) {
	repo := newTestRepo()
	ctx := context.Background()

	_, _ = repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "v1"})  // v1,v2
	_, _ = repo.Apply(ctx, &testv1.Update{Id: "id-1", Actor: "actor", Body: "v2"})  // Updated(v3)→Snap(v4)
	_, _ = repo.Apply(ctx, &testv1.Update{Id: "id-1", Actor: "actor", Body: "v3"})  // Updated(v5)

	agg, err := repo.Load(ctx, "id-1")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	test := agg.(*testv1.Test)
	if test.GetVersion() != 5 {
		t.Errorf("expected version 5, got %d", test.GetVersion())
	}
	if test.GetBody() != "v3" {
		t.Errorf("expected body 'v3', got %q", test.GetBody())
	}
}

func TestLoad_IndependentAggregates(t *testing.T) {
	repo := newTestRepo()
	ctx := context.Background()

	_, _ = repo.Apply(ctx, &testv1.Create{Id: "agg-1", Actor: "actor", Body: "first"})
	_, _ = repo.Apply(ctx, &testv1.Create{Id: "agg-2", Actor: "actor", Body: "second"})

	agg1, _ := repo.Load(ctx, "agg-1")
	agg2, _ := repo.Load(ctx, "agg-2")

	if agg1.(*testv1.Test).GetBody() != "first" {
		t.Errorf("agg-1 body mismatch")
	}
	if agg2.(*testv1.Test).GetBody() != "second" {
		t.Errorf("agg-2 body mismatch")
	}
}

// --- Snapshot tests ---

func TestApply_WithSnapshots(t *testing.T) {
	// Interval=3 from proto. Create emits Created(v1)+Unlocked(v2).
	// Update emits Updated(v3) → hits boundary → Snapshot(v4).
	repo := newTestRepo()
	ctx := context.Background()

	_, err := repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "v1"})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	version, err := repo.Apply(ctx, &testv1.Update{Id: "id-1", Actor: "actor", Body: "v2"})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}

	// Updated(v3) + Snapshot(v4) — version should be 4.
	if version != 4 {
		t.Fatalf("expected version 4 (Updated + Snapshot), got %d", version)
	}

	// Load should still reconstruct correctly.
	agg, err := repo.Load(ctx, "id-1")
	if err != nil {
		t.Fatalf("load after snapshot failed: %v", err)
	}

	test := agg.(*testv1.Test)
	if test.GetBody() != "v2" {
		t.Errorf("expected body 'v2', got %q", test.GetBody())
	}
}

// --- AggregateStore integration tests ---

func TestApply_MaterializesAggregate(t *testing.T) {
	repo := newTestRepo()
	ctx := context.Background()

	_, err := repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "hello"})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Verify materialization worked by loading via event replay — the aggregate
	// should reflect the created state. SaveAggregate is fire-and-forget; we
	// verify it didn't error by confirming Apply succeeded.
	agg, err := repo.Load(ctx, "id-1")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	test := agg.(*testv1.Test)
	if test.GetId() != "id-1" {
		t.Errorf("expected id 'id-1', got %q", test.GetId())
	}
	if test.GetBody() != "hello" {
		t.Errorf("expected body 'hello', got %q", test.GetBody())
	}
	if test.GetVersion() != 2 {
		t.Errorf("expected version 2, got %d", test.GetVersion())
	}
	if test.GetState() != testv1.State_STATE_UNLOCKED {
		t.Errorf("expected STATE_UNLOCKED, got %s", test.GetState())
	}
}

func TestApply_MaterializedAggregateUpdatesOnMutation(t *testing.T) {
	repo := newTestRepo()
	ctx := context.Background()

	_, _ = repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "v1"})
	_, _ = repo.Apply(ctx, &testv1.Update{Id: "id-1", Actor: "actor", Body: "v2"})

	agg, err := repo.Load(ctx, "id-1")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	test := agg.(*testv1.Test)
	if test.GetVersion() != 3 {
		t.Errorf("expected version 3, got %d", test.GetVersion())
	}
	if test.GetBody() != "v2" {
		t.Errorf("expected body 'v2', got %q", test.GetBody())
	}
}

func TestApply_MaterializedAggregateReflectsStateTransitions(t *testing.T) {
	repo := newTestRepo()
	ctx := context.Background()

	_, _ = repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "hello"})
	_, _ = repo.Apply(ctx, &testv1.Lock{Id: "id-1", Actor: "actor"})

	agg, err := repo.Load(ctx, "id-1")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	test := agg.(*testv1.Test)
	if test.GetVersion() != 3 {
		t.Errorf("expected version 3, got %d", test.GetVersion())
	}
	if test.GetState() != testv1.State_STATE_LOCKED {
		t.Errorf("expected STATE_LOCKED, got %s", test.GetState())
	}
}

// --- CommandEvaluator tests ---

// evaluatingUpdate wraps a testv1.Update and adds an Evaluate method.
// The evaluateFunc controls the behavior for each test case.
type evaluatingUpdate struct {
	*testv1.Update
	evaluateFunc func(protosource.Aggregate) error
}

func (e *evaluatingUpdate) Evaluate(aggregate protosource.Aggregate) error {
	return e.evaluateFunc(aggregate)
}

func TestApply_EvaluateReturnsNil_EventsPersisted(t *testing.T) {
	repo := newTestRepo()
	ctx := context.Background()

	_, err := repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "hello"})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Evaluate returns nil — events should be emitted and persisted normally.
	cmd := &evaluatingUpdate{
		Update:       &testv1.Update{Id: "id-1", Actor: "actor", Body: "updated"},
		evaluateFunc: func(protosource.Aggregate) error { return nil },
	}
	version, err := repo.Apply(ctx, cmd)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	// Create=v1,v2; Updated(v3)→Snapshot(v4)
	if version != 4 {
		t.Fatalf("expected version 4, got %d", version)
	}

	// Verify the aggregate was actually updated.
	agg, _ := repo.Load(ctx, "id-1")
	if agg.(*testv1.Test).GetBody() != "updated" {
		t.Errorf("expected body 'updated', got %q", agg.(*testv1.Test).GetBody())
	}
}

func TestApply_EvaluateReturnsErrSkip_NoEventsPersisted(t *testing.T) {
	repo := newTestRepo()
	ctx := context.Background()

	_, err := repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "hello"})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Evaluate returns ErrSkip — no events should be persisted, no error returned.
	cmd := &evaluatingUpdate{
		Update: &testv1.Update{Id: "id-1", Actor: "actor", Body: "duplicate"},
		evaluateFunc: func(protosource.Aggregate) error {
			return fmt.Errorf("already set: %w", protosource.ErrSkip)
		},
	}
	version, err := repo.Apply(ctx, cmd)
	if err != nil {
		t.Fatalf("expected no error for ErrSkip, got: %v", err)
	}
	// Version should remain at 2 (Create=v1,v2; no new events).
	if version != 2 {
		t.Fatalf("expected version 2 (unchanged), got %d", version)
	}

	// Verify the aggregate was NOT updated.
	agg, _ := repo.Load(ctx, "id-1")
	if agg.(*testv1.Test).GetBody() != "hello" {
		t.Errorf("expected body 'hello' (unchanged), got %q", agg.(*testv1.Test).GetBody())
	}
}

func TestApply_EvaluateSkipConditionNotMet_EventsPersisted(t *testing.T) {
	repo := newTestRepo()
	ctx := context.Background()

	_, err := repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "hello"})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Evaluate checks body equality — different body means proceed (no skip).
	cmd := &evaluatingUpdate{
		Update: &testv1.Update{Id: "id-1", Actor: "actor", Body: "different"},
		evaluateFunc: func(agg protosource.Aggregate) error {
			a := agg.(*testv1.Test)
			if a.GetBody() == "different" {
				return fmt.Errorf("body unchanged: %w", protosource.ErrSkip)
			}
			return nil
		},
	}
	version, err := repo.Apply(ctx, cmd)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	// Create=v1,v2; Updated(v3)→Snapshot(v4)
	if version != 4 {
		t.Fatalf("expected version 4, got %d", version)
	}

	// Verify the aggregate was updated.
	agg, _ := repo.Load(ctx, "id-1")
	if agg.(*testv1.Test).GetBody() != "different" {
		t.Errorf("expected body 'different', got %q", agg.(*testv1.Test).GetBody())
	}
}

func TestApply_EvaluateReturnsError_Aborts(t *testing.T) {
	repo := newTestRepo()
	ctx := context.Background()

	_, err := repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "hello"})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Evaluate returns a non-skip error — Apply should abort.
	evalErr := errors.New("business rule violated")
	cmd := &evaluatingUpdate{
		Update:       &testv1.Update{Id: "id-1", Actor: "actor", Body: "bad"},
		evaluateFunc: func(protosource.Aggregate) error { return evalErr },
	}
	_, err = repo.Apply(ctx, cmd)
	if !errors.Is(err, evalErr) {
		t.Fatalf("expected evalErr, got: %v", err)
	}

	// Verify the aggregate was NOT updated.
	agg, _ := repo.Load(ctx, "id-1")
	if agg.(*testv1.Test).GetBody() != "hello" {
		t.Errorf("expected body 'hello' (unchanged), got %q", agg.(*testv1.Test).GetBody())
	}
}

// --- Utility function tests ---

func TestNowMicros_ReturnsReasonableValue(t *testing.T) {
	before := time.Now().UnixMicro()
	got := protosource.NowMicros()
	after := time.Now().UnixMicro()

	if got < before || got > after {
		t.Errorf("NowMicros() = %d, expected between %d and %d", got, before, after)
	}
}

func TestFromMicros_RoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	micros := now.UnixMicro()

	got := protosource.FromMicros(micros)
	if !got.Equal(now) {
		t.Errorf("FromMicros(%d) = %v, want %v", micros, got, now)
	}
}

func TestFromMicros_KnownValue(t *testing.T) {
	// 2024-01-01 00:00:00 UTC = 1704067200 seconds = 1704067200000000 microseconds
	micros := int64(1704067200000000)
	got := protosource.FromMicros(micros)
	expected := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	if !got.Equal(expected) {
		t.Errorf("FromMicros(%d) = %v, want %v", micros, got, expected)
	}
}

// --- loadHistory path coverage ---

// basicStore wraps a memorystore but only exposes the Store interface,
// hiding SnapshotTailStore and AggregateStore.
type basicStore struct {
	inner *memorystore.MemoryStore
}

func (s *basicStore) Save(ctx context.Context, aggregateID string, records ...*recordv1.Record) error {
	return s.inner.Save(ctx, aggregateID, records...)
}

func (s *basicStore) Load(ctx context.Context, aggregateID string) (*historyv1.History, error) {
	return s.inner.Load(ctx, aggregateID)
}

// snapshotTailStore wraps a memorystore and adds SnapshotTailStore support
// by returning the last N records from the full history.
type snapshotTailStore struct {
	inner *memorystore.MemoryStore
}

func (s *snapshotTailStore) Save(ctx context.Context, aggregateID string, records ...*recordv1.Record) error {
	return s.inner.Save(ctx, aggregateID, records...)
}

func (s *snapshotTailStore) Load(ctx context.Context, aggregateID string) (*historyv1.History, error) {
	return s.inner.Load(ctx, aggregateID)
}

func (s *snapshotTailStore) SaveAggregate(ctx context.Context, aggregateID string, data []byte, version int64) error {
	return s.inner.SaveAggregate(ctx, aggregateID, data, version)
}

func (s *snapshotTailStore) LoadAggregate(ctx context.Context, aggregateID string) ([]byte, int64, error) {
	return s.inner.LoadAggregate(ctx, aggregateID)
}

func (s *snapshotTailStore) LoadTail(ctx context.Context, aggregateID string, n int) (*historyv1.History, error) {
	h, err := s.inner.Load(ctx, aggregateID)
	if err != nil {
		return nil, err
	}
	records := h.GetRecords()
	if len(records) > n {
		records = records[len(records)-n:]
	}
	return &historyv1.History{Records: records}, nil
}

func TestLoad_NonSnapshotTailStore_UsesFullLoad(t *testing.T) {
	store := &basicStore{inner: memorystore.New()}
	repo := protosource.New(
		&testv1.Test{},
		protosource.WithStore(store),
		protosource.WithSerializer(protobinaryserializer.NewSerializer()),
	)
	ctx := context.Background()

	_, err := repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "hello"})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	agg, err := repo.Load(ctx, "id-1")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	test := agg.(*testv1.Test)
	if test.GetBody() != "hello" {
		t.Errorf("expected body 'hello', got %q", test.GetBody())
	}
}

func TestLoad_NonSnapshoterAggregate_UsesFullLoad(t *testing.T) {
	// samplenosnapshot doesn't implement Snapshoter, so loadHistory should
	// fall back to full Load even with a SnapshotTailStore.
	store := &snapshotTailStore{inner: memorystore.New()}
	repo := protosource.New(
		&samplenov1.Sample{},
		protosource.WithStore(store),
		protosource.WithSerializer(protobinaryserializer.NewSerializer()),
	)
	ctx := context.Background()

	_, err := repo.Apply(ctx, &samplenov1.Create{Id: "id-1", Actor: "actor", Body: "hello"})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	agg, err := repo.Load(ctx, "id-1")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	sample := agg.(*samplenov1.Sample)
	if sample.GetBody() != "hello" {
		t.Errorf("expected body 'hello', got %q", sample.GetBody())
	}
	if sample.GetVersion() != 1 {
		t.Errorf("expected version 1, got %d", sample.GetVersion())
	}
}

func TestLoad_SnapshotTailStore_UsesLoadTail(t *testing.T) {
	// testv1.Test implements Snapshoter and snapshotTailStore implements
	// SnapshotTailStore, so loadHistory should use LoadTail.
	store := &snapshotTailStore{inner: memorystore.New()}
	repo := protosource.New(
		&testv1.Test{},
		protosource.WithStore(store),
		protosource.WithSerializer(protobinaryserializer.NewSerializer()),
	)
	ctx := context.Background()

	_, err := repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "hello"})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	// Updated(v3) → Snapshot(v4)
	_, err = repo.Apply(ctx, &testv1.Update{Id: "id-1", Actor: "actor", Body: "updated"})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	// Updated(v5) — after snapshot
	_, err = repo.Apply(ctx, &testv1.Update{Id: "id-1", Actor: "actor", Body: "final"})
	if err != nil {
		t.Fatalf("update 2 failed: %v", err)
	}

	// Load uses LoadTail — should still reconstruct correctly from snapshot + trailing events.
	agg, err := repo.Load(ctx, "id-1")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	test := agg.(*testv1.Test)
	if test.GetBody() != "final" {
		t.Errorf("expected body 'final', got %q", test.GetBody())
	}
	if test.GetState() != testv1.State_STATE_UNLOCKED {
		t.Errorf("expected STATE_UNLOCKED, got %s", test.GetState())
	}
}

// --- Snapshot state correctness ---

func TestSnapshot_CapturesPostEventState(t *testing.T) {
	// Interval=3 (from proto). Create emits Created(v1)+Unlocked(v2), Update emits Updated(v3).
	// Snapshot should fire at v3 and capture the aggregate state WITH the Updated body.
	store := memorystore.New()
	ser := protobinaryserializer.NewSerializer()
	repo := protosource.New(
		&testv1.Test{},
		protosource.WithStore(store),
		protosource.WithSerializer(ser),
	)
	ctx := context.Background()

	_, err := repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "created"})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Updated(v3) hits the interval=3 boundary → snapshot at v4.
	_, err = repo.Apply(ctx, &testv1.Update{Id: "id-1", Actor: "actor", Body: "updated"})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}

	// Load the raw history and find the snapshot event.
	history, err := store.Load(ctx, "id-1")
	if err != nil {
		t.Fatalf("load history failed: %v", err)
	}

	var snapshot *testv1.Snapshot
	for _, record := range history.GetRecords() {
		event, err := ser.UnmarshalEvent(record)
		if err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}
		if s, ok := event.(*testv1.Snapshot); ok {
			snapshot = s
			break
		}
	}

	if snapshot == nil {
		t.Fatal("expected a snapshot event in the history, found none")
	}

	// The snapshot payload must reflect post-event state (after Updated applied).
	if snapshot.GetSnapshot().GetBody() != "updated" {
		t.Errorf("snapshot body = %q, want %q", snapshot.GetSnapshot().GetBody(), "updated")
	}
	if snapshot.GetSnapshot().GetState() != testv1.State_STATE_UNLOCKED {
		t.Errorf("snapshot state = %s, want STATE_UNLOCKED", snapshot.GetSnapshot().GetState())
	}
}

func TestSnapshot_MultiEventBoundaryCrossing(t *testing.T) {
	// Interval=3. Create emits Created(v1) + Unlocked(v2).
	// Then 4 updates: Updated(v3)→snap(v4), Updated(v5), Updated(v6)→snap(v7), Updated(v8).
	// The first snapshot fires mid-stream at v3 (the boundary). Without per-event
	// checking, a multi-event command crossing the boundary would miss it.
	store := memorystore.New()
	ser := protobinaryserializer.NewSerializer()
	repo := protosource.New(
		&testv1.Test{},
		protosource.WithStore(store),
		protosource.WithSerializer(ser),
	)
	ctx := context.Background()

	_, err := repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "hello"})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Apply updates to cross multiple snapshot boundaries.
	for i := 1; i <= 4; i++ {
		_, err = repo.Apply(ctx, &testv1.Update{Id: "id-1", Actor: "actor", Body: fmt.Sprintf("v%d", i)})
		if err != nil {
			t.Fatalf("update %d failed: %v", i, err)
		}
	}

	history, err := store.Load(ctx, "id-1")
	if err != nil {
		t.Fatalf("load history failed: %v", err)
	}

	var snapshots []*testv1.Snapshot
	for _, record := range history.GetRecords() {
		event, err := ser.UnmarshalEvent(record)
		if err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}
		if s, ok := event.(*testv1.Snapshot); ok {
			snapshots = append(snapshots, s)
		}
	}

	if len(snapshots) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(snapshots))
	}

	// First snapshot at v3 boundary: body="v1" (first update).
	if snapshots[0].GetSnapshot().GetBody() != "v1" {
		t.Errorf("snapshot[0] body = %q, want %q", snapshots[0].GetSnapshot().GetBody(), "v1")
	}

	// Second snapshot at v6 boundary: body="v3" (third update).
	if snapshots[1].GetSnapshot().GetBody() != "v3" {
		t.Errorf("snapshot[1] body = %q, want %q", snapshots[1].GetSnapshot().GetBody(), "v3")
	}
}

func TestSnapshot_LoadReconstructsFromSnapshot(t *testing.T) {
	// Verify that loading with LoadTail (snapshot-aware store) produces
	// the same aggregate state as full replay.
	store := &snapshotTailStore{inner: memorystore.New()}
	ser := protobinaryserializer.NewSerializer()
	repo := protosource.New(
		&testv1.Test{},
		protosource.WithStore(store),
		protosource.WithSerializer(ser),
	)
	ctx := context.Background()

	_, _ = repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "v1"})
	_, _ = repo.Apply(ctx, &testv1.Update{Id: "id-1", Actor: "actor", Body: "v2"}) // v3 → snapshot(v4)
	_, _ = repo.Apply(ctx, &testv1.Update{Id: "id-1", Actor: "actor", Body: "v3"}) // v5 (after snapshot)

	agg, err := repo.Load(ctx, "id-1")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	test := agg.(*testv1.Test)
	if test.GetBody() != "v3" {
		t.Errorf("expected body 'v3', got %q", test.GetBody())
	}
	if test.GetState() != testv1.State_STATE_UNLOCKED {
		t.Errorf("expected STATE_UNLOCKED, got %s", test.GetState())
	}
	if test.GetId() != "id-1" {
		t.Errorf("expected id 'id-1', got %q", test.GetId())
	}
}
