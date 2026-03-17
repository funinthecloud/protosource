package memorystore

import (
	"context"
	"fmt"
	"sync"
	"testing"

	recordv1 "github.com/funinthecloud/protosource/record/v1"
	"google.golang.org/protobuf/proto"
)

// helper to create a record with version and data
func record(version int64, data string) *recordv1.Record {
	return &recordv1.Record{Version: version, Data: []byte(data)}
}

// --- New / Options ---

func TestNew_DefaultSnapshotInterval(t *testing.T) {
	m := New()
	if got := m.SnapshotInterval(); got != 0 {
		t.Errorf("expected default snapshot interval 0, got %d", got)
	}
}

func TestNew_WithSnapshotInterval(t *testing.T) {
	m := New(WithSnapshotInterval(50))
	if got := m.SnapshotInterval(); got != 50 {
		t.Errorf("expected snapshot interval 50, got %d", got)
	}
}

func TestSetSnapshotInterval(t *testing.T) {
	m := New()
	m.SetSnapshotInterval(25)
	if got := m.SnapshotInterval(); got != 25 {
		t.Errorf("expected snapshot interval 25, got %d", got)
	}
}

// --- Save ---

func TestSave_SingleRecord(t *testing.T) {
	m := New()
	err := m.Save(context.Background(), "agg-1", record(1, "hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSave_MultipleRecordsAtOnce(t *testing.T) {
	m := New()
	err := m.Save(context.Background(), "agg-1",
		record(1, "a"),
		record(2, "b"),
		record(3, "c"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	h, _ := m.Load(context.Background(), "agg-1")
	if got := len(h.GetRecords()); got != 3 {
		t.Fatalf("expected 3 records, got %d", got)
	}
}

func TestSave_AppendsToPreviousRecords(t *testing.T) {
	m := New()
	ctx := context.Background()

	_ = m.Save(ctx, "agg-1", record(1, "first"))
	_ = m.Save(ctx, "agg-1", record(2, "second"))

	h, _ := m.Load(ctx, "agg-1")
	records := h.GetRecords()
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if records[0].GetVersion() != 1 {
		t.Errorf("first record: expected version 1, got %d", records[0].GetVersion())
	}
	if records[1].GetVersion() != 2 {
		t.Errorf("second record: expected version 2, got %d", records[1].GetVersion())
	}
}

func TestSave_NoRecords(t *testing.T) {
	m := New()
	err := m.Save(context.Background(), "agg-1")
	if err != nil {
		t.Fatalf("save with no records should succeed, got: %v", err)
	}

	// Aggregate should exist with empty history
	h, _ := m.Load(context.Background(), "agg-1")
	if got := len(h.GetRecords()); got != 0 {
		t.Errorf("expected 0 records, got %d", got)
	}
}

func TestSave_CancelledContext(t *testing.T) {
	m := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := m.Save(ctx, "agg-1", record(1, "data"))
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

func TestSave_DeadlineExceededContext(t *testing.T) {
	m := New()
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()

	err := m.Save(ctx, "agg-1", record(1, "data"))
	if err == nil {
		t.Fatal("expected error for expired context, got nil")
	}
}

// --- Load ---

func TestLoad_NonExistentAggregate(t *testing.T) {
	m := New()
	h, err := m.Load(context.Background(), "does-not-exist")
	if err != nil {
		t.Fatalf("load of non-existent aggregate should not error, got: %v", err)
	}
	if got := len(h.GetRecords()); got != 0 {
		t.Errorf("expected empty history (0 records), got %d", got)
	}
}

func TestLoad_AfterSave(t *testing.T) {
	m := New()
	ctx := context.Background()

	_ = m.Save(ctx, "agg-1",
		record(1, "event-a"),
		record(2, "event-b"),
	)

	h, err := m.Load(ctx, "agg-1")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	records := h.GetRecords()
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if string(records[0].GetData()) != "event-a" {
		t.Errorf("record[0] data: expected 'event-a', got %q", string(records[0].GetData()))
	}
	if string(records[1].GetData()) != "event-b" {
		t.Errorf("record[1] data: expected 'event-b', got %q", string(records[1].GetData()))
	}
}

func TestLoad_CancelledContext(t *testing.T) {
	m := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := m.Load(ctx, "agg-1")
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

// --- Aggregate isolation ---

func TestSaveLoad_IndependentAggregates(t *testing.T) {
	m := New()
	ctx := context.Background()

	_ = m.Save(ctx, "agg-1", record(1, "alpha"))
	_ = m.Save(ctx, "agg-2", record(1, "beta"))
	_ = m.Save(ctx, "agg-1", record(2, "gamma"))

	h1, _ := m.Load(ctx, "agg-1")
	h2, _ := m.Load(ctx, "agg-2")

	if got := len(h1.GetRecords()); got != 2 {
		t.Errorf("agg-1: expected 2 records, got %d", got)
	}
	if got := len(h2.GetRecords()); got != 1 {
		t.Errorf("agg-2: expected 1 record, got %d", got)
	}
}

// --- Record data integrity ---

func TestLoad_PreservesRecordFields(t *testing.T) {
	m := New()
	ctx := context.Background()

	original := &recordv1.Record{
		Version: 42,
		Data:    []byte("payload"),
	}
	_ = m.Save(ctx, "agg-1", original)

	h, _ := m.Load(ctx, "agg-1")
	got := h.GetRecords()[0]

	if got.GetVersion() != 42 {
		t.Errorf("version: expected 42, got %d", got.GetVersion())
	}
	if string(got.GetData()) != "payload" {
		t.Errorf("data: expected 'payload', got %q", string(got.GetData()))
	}
}

// --- Concurrency ---

func TestConcurrent_SaveAndLoad(t *testing.T) {
	m := New()
	ctx := context.Background()
	const numAggregates = 10
	const eventsPerAggregate = 50

	var wg sync.WaitGroup

	// Concurrent saves across multiple aggregates
	for i := 0; i < numAggregates; i++ {
		wg.Add(1)
		go func(aggID string) {
			defer wg.Done()
			for v := int64(1); v <= eventsPerAggregate; v++ {
				if err := m.Save(ctx, aggID, record(v, fmt.Sprintf("event-%d", v))); err != nil {
					t.Errorf("save failed for %s v%d: %v", aggID, v, err)
				}
			}
		}(fmt.Sprintf("agg-%d", i))
	}
	wg.Wait()

	// Verify each aggregate has the right number of records
	for i := 0; i < numAggregates; i++ {
		h, err := m.Load(ctx, fmt.Sprintf("agg-%d", i))
		if err != nil {
			t.Errorf("load failed for agg-%d: %v", i, err)
			continue
		}
		if got := len(h.GetRecords()); got != eventsPerAggregate {
			t.Errorf("agg-%d: expected %d records, got %d", i, eventsPerAggregate, got)
		}
	}
}

func TestConcurrent_SaveAndLoadSameAggregate(t *testing.T) {
	m := New()
	ctx := context.Background()
	const numGoroutines = 20

	// Pre-populate
	_ = m.Save(ctx, "shared", record(1, "seed"))

	var wg sync.WaitGroup

	// Mix of concurrent reads and writes on the same aggregate
	for i := 0; i < numGoroutines; i++ {
		wg.Add(2)
		go func(v int64) {
			defer wg.Done()
			_ = m.Save(ctx, "shared", record(v, fmt.Sprintf("event-%d", v)))
		}(int64(i + 2))
		go func() {
			defer wg.Done()
			_, _ = m.Load(ctx, "shared")
		}()
	}
	wg.Wait()

	// Just verify no panic and we can still load
	h, err := m.Load(ctx, "shared")
	if err != nil {
		t.Fatalf("load after concurrent ops failed: %v", err)
	}
	// 1 seed + numGoroutines writes
	if got := len(h.GetRecords()); got != numGoroutines+1 {
		t.Errorf("expected %d records, got %d", numGoroutines+1, got)
	}
}

// --- Proto equality (sanity check that proto records survive round-trip) ---

func TestLoad_RecordProtoEquality(t *testing.T) {
	m := New()
	ctx := context.Background()

	saved := &recordv1.Record{
		Version: 7,
		Data:    []byte{0x01, 0x02, 0x03},
	}
	_ = m.Save(ctx, "agg-1", saved)

	h, _ := m.Load(ctx, "agg-1")
	loaded := h.GetRecords()[0]

	if !proto.Equal(saved, loaded) {
		t.Errorf("saved and loaded records are not proto-equal\nsaved:  %v\nloaded: %v", saved, loaded)
	}
}

// --- AggregateStore ---

func TestSaveAggregate_Basic(t *testing.T) {
	m := New()
	ctx := context.Background()

	data := []byte("serialized-aggregate")
	err := m.SaveAggregate(ctx, "agg-1", data, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, version, err := m.LoadAggregate(ctx, "agg-1")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if version != 5 {
		t.Errorf("expected version 5, got %d", version)
	}
	if string(got) != "serialized-aggregate" {
		t.Errorf("expected data 'serialized-aggregate', got %q", string(got))
	}
}

func TestLoadAggregate_NonExistent(t *testing.T) {
	m := New()
	data, version, err := m.LoadAggregate(context.Background(), "does-not-exist")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data != nil {
		t.Errorf("expected nil data, got %v", data)
	}
	if version != 0 {
		t.Errorf("expected version 0, got %d", version)
	}
}

func TestSaveAggregate_OverwritesPrevious(t *testing.T) {
	m := New()
	ctx := context.Background()

	_ = m.SaveAggregate(ctx, "agg-1", []byte("v1-state"), 1)
	_ = m.SaveAggregate(ctx, "agg-1", []byte("v3-state"), 3)

	data, version, _ := m.LoadAggregate(ctx, "agg-1")
	if version != 3 {
		t.Errorf("expected version 3, got %d", version)
	}
	if string(data) != "v3-state" {
		t.Errorf("expected 'v3-state', got %q", string(data))
	}
}

func TestSaveAggregate_IndependentAggregates(t *testing.T) {
	m := New()
	ctx := context.Background()

	_ = m.SaveAggregate(ctx, "agg-1", []byte("first"), 1)
	_ = m.SaveAggregate(ctx, "agg-2", []byte("second"), 2)

	d1, v1, _ := m.LoadAggregate(ctx, "agg-1")
	d2, v2, _ := m.LoadAggregate(ctx, "agg-2")

	if string(d1) != "first" || v1 != 1 {
		t.Errorf("agg-1: got data=%q version=%d", string(d1), v1)
	}
	if string(d2) != "second" || v2 != 2 {
		t.Errorf("agg-2: got data=%q version=%d", string(d2), v2)
	}
}

func TestSaveAggregate_CancelledContext(t *testing.T) {
	m := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := m.SaveAggregate(ctx, "agg-1", []byte("data"), 1)
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

func TestLoadAggregate_CancelledContext(t *testing.T) {
	m := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := m.LoadAggregate(ctx, "agg-1")
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

func TestSaveAggregate_DoesNotAffectEventHistory(t *testing.T) {
	m := New()
	ctx := context.Background()

	// Save some events
	_ = m.Save(ctx, "agg-1", record(1, "event-data"))

	// Save aggregate state
	_ = m.SaveAggregate(ctx, "agg-1", []byte("aggregate-state"), 1)

	// Event history should be unaffected
	h, _ := m.Load(ctx, "agg-1")
	if got := len(h.GetRecords()); got != 1 {
		t.Errorf("expected 1 event record, got %d", got)
	}
	if string(h.GetRecords()[0].GetData()) != "event-data" {
		t.Errorf("event data should be unaffected")
	}
}

func TestConcurrent_SaveAndLoadAggregate(t *testing.T) {
	m := New()
	ctx := context.Background()
	const numGoroutines = 20

	var wg sync.WaitGroup
	for i := 0; i < numGoroutines; i++ {
		wg.Add(2)
		go func(v int) {
			defer wg.Done()
			_ = m.SaveAggregate(ctx, "shared", []byte(fmt.Sprintf("state-%d", v)), int64(v))
		}(i)
		go func() {
			defer wg.Done()
			_, _, _ = m.LoadAggregate(ctx, "shared")
		}()
	}
	wg.Wait()

	// Just verify no panic and we can still load
	data, version, err := m.LoadAggregate(ctx, "shared")
	if err != nil {
		t.Fatalf("load after concurrent ops failed: %v", err)
	}
	if data == nil {
		t.Fatal("expected non-nil data after concurrent saves")
	}
	if version < 0 || version >= int64(numGoroutines) {
		t.Errorf("version %d outside expected range", version)
	}
}
