package protosource_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/funinthecloud/protosource"
	testv1 "github.com/funinthecloud/protosource/acme/app/test/v1"
	"github.com/funinthecloud/protosource/serializers/protobinaryserializer"
	"github.com/funinthecloud/protosource/stores/memorystore"
	"google.golang.org/protobuf/proto"
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

	// Create=2 events (v1,v2), Update=1 event (v3)
	version, err := repo.Apply(ctx, &testv1.Update{Id: "id-1", Actor: "actor", Body: "updated"})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if version != 3 {
		t.Fatalf("expected version 3, got %d", version)
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

	// Create=v1,v2; Update=v3
	version, err := repo.Apply(ctx, &testv1.Update{Id: "id-1", Actor: "actor", Body: "updated"})
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if version != 3 {
		t.Fatalf("expected version 3, got %d", version)
	}
}

func TestApply_LockThenUnlockThenUpdate(t *testing.T) {
	repo := newTestRepo()
	ctx := context.Background()

	_, _ = repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "hello"})   // v1,v2
	_, _ = repo.Apply(ctx, &testv1.Lock{Id: "id-1", Actor: "actor"})                     // v3
	_, _ = repo.Apply(ctx, &testv1.Unlock{Id: "id-1", Actor: "actor"})                   // v4

	// After unlock, update should succeed at v5
	version, err := repo.Apply(ctx, &testv1.Update{Id: "id-1", Actor: "actor", Body: "updated"})
	if err != nil {
		t.Fatalf("expected success after unlock, got: %v", err)
	}
	if version != 5 {
		t.Fatalf("expected version 5, got %d", version)
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
	_, _ = repo.Apply(ctx, &testv1.Update{Id: "id-1", Actor: "actor", Body: "v2"})  // v3
	_, _ = repo.Apply(ctx, &testv1.Update{Id: "id-1", Actor: "actor", Body: "v3"})  // v4

	agg, err := repo.Load(ctx, "id-1")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	test := agg.(*testv1.Test)
	if test.GetVersion() != 4 {
		t.Errorf("expected version 4, got %d", test.GetVersion())
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
	// Snapshot every 3 events so we can test it without 50 iterations
	repo := newTestRepo(memorystore.WithSnapshotInterval(3))
	ctx := context.Background()

	// Create emits 2 events (v1 Created, v2 Unlocked)
	_, err := repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "v1"})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Update emits 1 event (v3) — should trigger snapshot at v3
	version, err := repo.Apply(ctx, &testv1.Update{Id: "id-1", Actor: "actor", Body: "v2"})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}

	// Version should account for the snapshot being inserted
	if version < 3 {
		t.Fatalf("expected version >= 3, got %d", version)
	}

	// Load should still reconstruct correctly
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
	store := memorystore.New()
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

	// The store should now have the materialized aggregate
	data, version, err := store.LoadAggregate(ctx, "id-1")
	if err != nil {
		t.Fatalf("load aggregate failed: %v", err)
	}
	if data == nil {
		t.Fatal("expected non-nil aggregate data")
	}
	if version != 2 {
		t.Errorf("expected version 2 (Created+Unlocked), got %d", version)
	}

	// Unmarshal and verify fields
	var agg testv1.Test
	if err := proto.Unmarshal(data, &agg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if agg.GetId() != "id-1" {
		t.Errorf("expected id 'id-1', got %q", agg.GetId())
	}
	if agg.GetBody() != "hello" {
		t.Errorf("expected body 'hello', got %q", agg.GetBody())
	}
	if agg.GetVersion() != 2 {
		t.Errorf("expected version 2, got %d", agg.GetVersion())
	}
	if agg.GetState() != testv1.State_STATE_UNLOCKED {
		t.Errorf("expected STATE_UNLOCKED, got %s", agg.GetState())
	}
}

func TestApply_MaterializedAggregateUpdatesOnMutation(t *testing.T) {
	store := memorystore.New()
	repo := protosource.New(
		&testv1.Test{},
		protosource.WithStore(store),
		protosource.WithSerializer(protobinaryserializer.NewSerializer()),
	)
	ctx := context.Background()

	_, _ = repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "v1"})
	_, _ = repo.Apply(ctx, &testv1.Update{Id: "id-1", Actor: "actor", Body: "v2"})

	data, version, _ := store.LoadAggregate(ctx, "id-1")
	if version != 3 {
		t.Errorf("expected version 3, got %d", version)
	}

	var agg testv1.Test
	_ = proto.Unmarshal(data, &agg)
	if agg.GetBody() != "v2" {
		t.Errorf("expected body 'v2', got %q", agg.GetBody())
	}
}

func TestApply_MaterializedAggregateReflectsStateTransitions(t *testing.T) {
	store := memorystore.New()
	repo := protosource.New(
		&testv1.Test{},
		protosource.WithStore(store),
		protosource.WithSerializer(protobinaryserializer.NewSerializer()),
	)
	ctx := context.Background()

	_, _ = repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "hello"})
	_, _ = repo.Apply(ctx, &testv1.Lock{Id: "id-1", Actor: "actor"})

	data, version, _ := store.LoadAggregate(ctx, "id-1")
	if version != 3 {
		t.Errorf("expected version 3, got %d", version)
	}

	var agg testv1.Test
	_ = proto.Unmarshal(data, &agg)
	if agg.GetState() != testv1.State_STATE_LOCKED {
		t.Errorf("expected STATE_LOCKED, got %s", agg.GetState())
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
	// Create=v1,v2; Update=v3
	if version != 3 {
		t.Fatalf("expected version 3, got %d", version)
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
	// Create=v1,v2; Update=v3
	if version != 3 {
		t.Fatalf("expected version 3, got %d", version)
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
