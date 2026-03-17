package boltdbstore

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	recordv1 "github.com/funinthecloud/protosource/record/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func record(version int64, data string) *recordv1.Record {
	return &recordv1.Record{Version: version, Data: []byte(data)}
}

func newTestStore(t *testing.T, opts ...Option) *BoltDBStore {
	t.Helper()
	dir := t.TempDir()
	store, err := New(dir, "test", opts...)
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

// --- Save ---

func TestSave_SingleRecord(t *testing.T) {
	s := newTestStore(t)
	err := s.Save(context.Background(), "agg-1", record(1, "hello"))
	require.NoError(t, err)
}

func TestSave_MultipleRecordsAtOnce(t *testing.T) {
	s := newTestStore(t)
	err := s.Save(context.Background(), "agg-1",
		record(1, "a"),
		record(2, "b"),
		record(3, "c"),
	)
	require.NoError(t, err)

	h, err := s.Load(context.Background(), "agg-1")
	require.NoError(t, err)
	assert.Len(t, h.GetRecords(), 3)
}

func TestSave_AppendsToPreviousRecords(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.Save(ctx, "agg-1", record(1, "first")))
	require.NoError(t, s.Save(ctx, "agg-1", record(2, "second")))

	h, err := s.Load(ctx, "agg-1")
	require.NoError(t, err)
	records := h.GetRecords()
	require.Len(t, records, 2)
	assert.Equal(t, int64(1), records[0].GetVersion())
	assert.Equal(t, int64(2), records[1].GetVersion())
}

func TestSave_NoRecords(t *testing.T) {
	s := newTestStore(t)
	err := s.Save(context.Background(), "agg-1")
	require.NoError(t, err)
}

func TestSave_CancelledContext(t *testing.T) {
	s := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := s.Save(ctx, "agg-1", record(1, "data"))
	assert.Error(t, err)
}

func TestSave_DeadlineExceededContext(t *testing.T) {
	s := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()

	err := s.Save(ctx, "agg-1", record(1, "data"))
	assert.Error(t, err)
}

// --- Load ---

func TestLoad_NonExistentAggregate(t *testing.T) {
	s := newTestStore(t)
	h, err := s.Load(context.Background(), "does-not-exist")
	require.NoError(t, err)
	assert.Empty(t, h.GetRecords())
}

func TestLoad_AfterSave(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.Save(ctx, "agg-1",
		record(1, "event-a"),
		record(2, "event-b"),
	))

	h, err := s.Load(ctx, "agg-1")
	require.NoError(t, err)

	records := h.GetRecords()
	require.Len(t, records, 2)
	assert.Equal(t, "event-a", string(records[0].GetData()))
	assert.Equal(t, "event-b", string(records[1].GetData()))
}

func TestLoad_VersionOrder(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Save in non-sequential order within a single call
	require.NoError(t, s.Save(ctx, "agg-1",
		record(3, "c"),
		record(1, "a"),
		record(2, "b"),
	))

	h, err := s.Load(ctx, "agg-1")
	require.NoError(t, err)

	records := h.GetRecords()
	require.Len(t, records, 3)
	// BoltDB sorts by key (big-endian version), so records come back sorted.
	assert.Equal(t, int64(1), records[0].GetVersion())
	assert.Equal(t, int64(2), records[1].GetVersion())
	assert.Equal(t, int64(3), records[2].GetVersion())
}

func TestLoad_CancelledContext(t *testing.T) {
	s := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.Load(ctx, "agg-1")
	assert.Error(t, err)
}

// --- Aggregate isolation ---

func TestSaveLoad_IndependentAggregates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.Save(ctx, "agg-1", record(1, "alpha")))
	require.NoError(t, s.Save(ctx, "agg-2", record(1, "beta")))
	require.NoError(t, s.Save(ctx, "agg-1", record(2, "gamma")))

	h1, _ := s.Load(ctx, "agg-1")
	h2, _ := s.Load(ctx, "agg-2")

	assert.Len(t, h1.GetRecords(), 2)
	assert.Len(t, h2.GetRecords(), 1)
}

// --- Record data integrity ---

func TestLoad_PreservesRecordFields(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	original := &recordv1.Record{Version: 42, Data: []byte("payload")}
	require.NoError(t, s.Save(ctx, "agg-1", original))

	h, _ := s.Load(ctx, "agg-1")
	got := h.GetRecords()[0]

	assert.Equal(t, int64(42), got.GetVersion())
	assert.Equal(t, "payload", string(got.GetData()))
}

// --- AggregateStore ---

func TestSaveAggregate_Basic(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Must save events first to assign a shard.
	require.NoError(t, s.Save(ctx, "agg-1", record(1, "event")))

	require.NoError(t, s.SaveAggregate(ctx, "agg-1", []byte("serialized-aggregate"), 5))

	data, version, err := s.LoadAggregate(ctx, "agg-1")
	require.NoError(t, err)
	assert.Equal(t, int64(5), version)
	assert.Equal(t, "serialized-aggregate", string(data))
}

func TestLoadAggregate_NonExistent(t *testing.T) {
	s := newTestStore(t)
	data, version, err := s.LoadAggregate(context.Background(), "does-not-exist")
	require.NoError(t, err)
	assert.Nil(t, data)
	assert.Equal(t, int64(0), version)
}

func TestSaveAggregate_OverwritesPrevious(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.Save(ctx, "agg-1", record(1, "e")))
	require.NoError(t, s.SaveAggregate(ctx, "agg-1", []byte("v1-state"), 1))
	require.NoError(t, s.SaveAggregate(ctx, "agg-1", []byte("v3-state"), 3))

	data, version, _ := s.LoadAggregate(ctx, "agg-1")
	assert.Equal(t, int64(3), version)
	assert.Equal(t, "v3-state", string(data))
}

func TestSaveAggregate_IndependentAggregates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.Save(ctx, "agg-1", record(1, "e")))
	require.NoError(t, s.Save(ctx, "agg-2", record(1, "e")))

	require.NoError(t, s.SaveAggregate(ctx, "agg-1", []byte("first"), 1))
	require.NoError(t, s.SaveAggregate(ctx, "agg-2", []byte("second"), 2))

	d1, v1, _ := s.LoadAggregate(ctx, "agg-1")
	d2, v2, _ := s.LoadAggregate(ctx, "agg-2")

	assert.Equal(t, "first", string(d1))
	assert.Equal(t, int64(1), v1)
	assert.Equal(t, "second", string(d2))
	assert.Equal(t, int64(2), v2)
}

func TestSaveAggregate_CancelledContext(t *testing.T) {
	s := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := s.SaveAggregate(ctx, "agg-1", []byte("data"), 1)
	assert.Error(t, err)
}

func TestLoadAggregate_CancelledContext(t *testing.T) {
	s := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := s.LoadAggregate(ctx, "agg-1")
	assert.Error(t, err)
}

func TestSaveAggregate_DoesNotAffectEventHistory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.Save(ctx, "agg-1", record(1, "event-data")))
	require.NoError(t, s.SaveAggregate(ctx, "agg-1", []byte("aggregate-state"), 1))

	h, _ := s.Load(ctx, "agg-1")
	require.Len(t, h.GetRecords(), 1)
	assert.Equal(t, "event-data", string(h.GetRecords()[0].GetData()))
}

// --- Sharding ---

func TestSharding_MultipleShards(t *testing.T) {
	s := newTestStore(t, WithMaxPerShard(3))
	ctx := context.Background()

	// Save 10 aggregates — should create ceil(10/3) = 4 shards.
	for i := 0; i < 10; i++ {
		require.NoError(t, s.Save(ctx, fmt.Sprintf("agg-%d", i), record(1, "data")))
	}

	// Verify all 10 aggregates are loadable.
	for i := 0; i < 10; i++ {
		h, err := s.Load(ctx, fmt.Sprintf("agg-%d", i))
		require.NoError(t, err)
		assert.Len(t, h.GetRecords(), 1, "agg-%d", i)
	}

	// Verify multiple shard files exist.
	dir := filepath.Join(s.basePath, s.pkg)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	shardCount := 0
	for _, e := range entries {
		if matched, _ := filepath.Match("shard-*.db", e.Name()); matched {
			shardCount++
		}
	}
	assert.GreaterOrEqual(t, shardCount, 3, "expected at least 3 shard files for 10 aggregates with max 3 per shard")
}

func TestSharding_SurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// First session: save 5 aggregates across shards.
	s1, err := New(dir, "test", WithMaxPerShard(2))
	require.NoError(t, err)
	for i := 0; i < 5; i++ {
		require.NoError(t, s1.Save(ctx, fmt.Sprintf("agg-%d", i), record(1, fmt.Sprintf("data-%d", i))))
	}
	require.NoError(t, s1.Close())

	// Second session: reopen and verify data.
	s2, err := New(dir, "test", WithMaxPerShard(2))
	require.NoError(t, err)
	defer s2.Close()

	for i := 0; i < 5; i++ {
		h, err := s2.Load(ctx, fmt.Sprintf("agg-%d", i))
		require.NoError(t, err)
		require.Len(t, h.GetRecords(), 1, "agg-%d", i)
		assert.Equal(t, fmt.Sprintf("data-%d", i), string(h.GetRecords()[0].GetData()))
	}

	// New aggregates should go to the right shard.
	require.NoError(t, s2.Save(ctx, "agg-new", record(1, "new-data")))
	h, err := s2.Load(ctx, "agg-new")
	require.NoError(t, err)
	assert.Len(t, h.GetRecords(), 1)
}

func TestSharding_SameAggregateSameShard(t *testing.T) {
	s := newTestStore(t, WithMaxPerShard(2))
	ctx := context.Background()

	// Save to one aggregate multiple times — should always go to same shard.
	for v := int64(1); v <= 10; v++ {
		require.NoError(t, s.Save(ctx, "agg-1", record(v, fmt.Sprintf("event-%d", v))))
	}

	h, err := s.Load(ctx, "agg-1")
	require.NoError(t, err)
	assert.Len(t, h.GetRecords(), 10)
}

// --- Concurrency ---

func TestConcurrent_SaveDifferentAggregates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	const numAggregates = 10
	const eventsPerAggregate = 50

	var wg sync.WaitGroup
	for i := 0; i < numAggregates; i++ {
		wg.Add(1)
		go func(aggID string) {
			defer wg.Done()
			for v := int64(1); v <= eventsPerAggregate; v++ {
				if err := s.Save(ctx, aggID, record(v, fmt.Sprintf("event-%d", v))); err != nil {
					t.Errorf("save failed for %s v%d: %v", aggID, v, err)
				}
			}
		}(fmt.Sprintf("agg-%d", i))
	}
	wg.Wait()

	for i := 0; i < numAggregates; i++ {
		h, err := s.Load(ctx, fmt.Sprintf("agg-%d", i))
		require.NoError(t, err)
		assert.Len(t, h.GetRecords(), eventsPerAggregate, "agg-%d", i)
	}
}

func TestConcurrent_MixedReadsWrites(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	const numGoroutines = 20

	require.NoError(t, s.Save(ctx, "shared", record(1, "seed")))

	var wg sync.WaitGroup
	for i := 0; i < numGoroutines; i++ {
		wg.Add(2)
		go func(v int64) {
			defer wg.Done()
			_ = s.Save(ctx, "shared", record(v, fmt.Sprintf("event-%d", v)))
		}(int64(i + 2))
		go func() {
			defer wg.Done()
			_, _ = s.Load(ctx, "shared")
		}()
	}
	wg.Wait()

	h, err := s.Load(ctx, "shared")
	require.NoError(t, err)
	assert.Equal(t, numGoroutines+1, len(h.GetRecords()))
}

func TestConcurrent_SaveAndLoadAggregate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	const numGoroutines = 20

	require.NoError(t, s.Save(ctx, "shared", record(1, "seed")))

	var wg sync.WaitGroup
	for i := 0; i < numGoroutines; i++ {
		wg.Add(2)
		go func(v int) {
			defer wg.Done()
			_ = s.SaveAggregate(ctx, "shared", []byte(fmt.Sprintf("state-%d", v)), int64(v))
		}(i)
		go func() {
			defer wg.Done()
			_, _, _ = s.LoadAggregate(ctx, "shared")
		}()
	}
	wg.Wait()

	data, version, err := s.LoadAggregate(ctx, "shared")
	require.NoError(t, err)
	assert.NotNil(t, data)
	assert.GreaterOrEqual(t, version, int64(0))
}

// --- Edge cases ---

func TestEdge_ManyRecordsPerAggregate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for v := int64(1); v <= 1000; v++ {
		require.NoError(t, s.Save(ctx, "agg-big", record(v, fmt.Sprintf("data-%d", v))))
	}

	h, err := s.Load(ctx, "agg-big")
	require.NoError(t, err)
	assert.Len(t, h.GetRecords(), 1000)
	// Verify ordering.
	for i, r := range h.GetRecords() {
		assert.Equal(t, int64(i+1), r.GetVersion())
	}
}

func TestEdge_EmptyData(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.Save(ctx, "agg-1", &recordv1.Record{Version: 1, Data: []byte{}}))

	h, err := s.Load(ctx, "agg-1")
	require.NoError(t, err)
	require.Len(t, h.GetRecords(), 1)
	assert.Empty(t, h.GetRecords()[0].GetData())
}

func TestEdge_BinaryDataWithNullBytes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	binaryData := []byte{0x00, 0x01, 0x00, 0xFF, 0x00, 0xFE}
	require.NoError(t, s.Save(ctx, "agg-1", &recordv1.Record{Version: 1, Data: binaryData}))

	h, err := s.Load(ctx, "agg-1")
	require.NoError(t, err)
	require.Len(t, h.GetRecords(), 1)
	assert.True(t, bytes.Equal(binaryData, h.GetRecords()[0].GetData()))
}

// --- LoadTail ---

func TestLoadTail_AllEvents(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, s.Save(ctx, "agg-1",
		record(1, "a"), record(2, "b"), record(3, "c"),
	))

	h, err := s.LoadTail(ctx, "agg-1", 10)
	require.NoError(t, err)
	assert.Len(t, h.GetRecords(), 3)
	assert.Equal(t, int64(1), h.GetRecords()[0].GetVersion())
	assert.Equal(t, int64(3), h.GetRecords()[2].GetVersion())
}

func TestLoadTail_LastN(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, s.Save(ctx, "agg-1",
		record(1, "a"), record(2, "b"), record(3, "c"), record(4, "d"), record(5, "e"),
	))

	h, err := s.LoadTail(ctx, "agg-1", 3)
	require.NoError(t, err)
	require.Len(t, h.GetRecords(), 3)
	assert.Equal(t, int64(3), h.GetRecords()[0].GetVersion())
	assert.Equal(t, int64(4), h.GetRecords()[1].GetVersion())
	assert.Equal(t, int64(5), h.GetRecords()[2].GetVersion())
}

func TestLoadTail_ExactCount(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, s.Save(ctx, "agg-1",
		record(1, "a"), record(2, "b"), record(3, "c"),
	))

	h, err := s.LoadTail(ctx, "agg-1", 3)
	require.NoError(t, err)
	require.Len(t, h.GetRecords(), 3)
	assert.Equal(t, int64(1), h.GetRecords()[0].GetVersion())
}

func TestLoadTail_NonExistent(t *testing.T) {
	s := newTestStore(t)
	h, err := s.LoadTail(context.Background(), "nope", 10)
	require.NoError(t, err)
	assert.Empty(t, h.GetRecords())
}

func TestLoadTail_CancelledContext(t *testing.T) {
	s := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.LoadTail(ctx, "agg-1", 10)
	assert.Error(t, err)
}

func TestLoadTail_SnapshotScenario(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// 100 events, snapshot interval 50 — LoadTail(50) returns versions 51..100.
	for i := int64(1); i <= 100; i++ {
		require.NoError(t, s.Save(ctx, "agg-1", record(i, fmt.Sprintf("event-%d", i))))
	}

	h, err := s.LoadTail(ctx, "agg-1", 50)
	require.NoError(t, err)
	require.Len(t, h.GetRecords(), 50)
	assert.Equal(t, int64(51), h.GetRecords()[0].GetVersion())
	assert.Equal(t, int64(100), h.GetRecords()[49].GetVersion())
}

func TestLoadTail_FewerThanN(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, s.Save(ctx, "agg-1", record(1, "only-one")))

	h, err := s.LoadTail(ctx, "agg-1", 50)
	require.NoError(t, err)
	require.Len(t, h.GetRecords(), 1)
	assert.Equal(t, int64(1), h.GetRecords()[0].GetVersion())
}

func TestLoadTail_PreservesData(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, s.Save(ctx, "agg-1",
		record(1, "first"), record(2, "second"), record(3, "third"),
	))

	h, err := s.LoadTail(ctx, "agg-1", 2)
	require.NoError(t, err)
	require.Len(t, h.GetRecords(), 2)
	assert.Equal(t, "second", string(h.GetRecords()[0].GetData()))
	assert.Equal(t, "third", string(h.GetRecords()[1].GetData()))
}

// --- Lifecycle ---

func TestLifecycle_CloseAndReopen(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	s1, err := New(dir, "test")
	require.NoError(t, err)

	require.NoError(t, s1.Save(ctx, "agg-1", record(1, "persisted")))
	require.NoError(t, s1.SaveAggregate(ctx, "agg-1", []byte("agg-state"), 1))
	require.NoError(t, s1.Close())

	s2, err := New(dir, "test")
	require.NoError(t, err)
	defer s2.Close()

	h, err := s2.Load(ctx, "agg-1")
	require.NoError(t, err)
	require.Len(t, h.GetRecords(), 1)
	assert.Equal(t, "persisted", string(h.GetRecords()[0].GetData()))

	data, version, err := s2.LoadAggregate(ctx, "agg-1")
	require.NoError(t, err)
	assert.Equal(t, int64(1), version)
	assert.Equal(t, "agg-state", string(data))
}

func TestNew_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "deep", "nested")

	s, err := New(nested, "pkg")
	require.NoError(t, err)
	defer s.Close()

	info, err := os.Stat(filepath.Join(nested, "pkg"))
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}
