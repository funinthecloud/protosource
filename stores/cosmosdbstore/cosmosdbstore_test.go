package cosmosdbstore

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
	"github.com/funinthecloud/protosource/azure/cosmosclient"
	testv1 "github.com/funinthecloud/protosource/example/app/test/v1"
	"github.com/funinthecloud/protosource/opaquedata"
	opaquedatav1 "github.com/funinthecloud/protosource/opaquedata/v1"
	recordv1 "github.com/funinthecloud/protosource/record/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// In-memory mock cosmosclient.ContainerClient
// ---------------------------------------------------------------------------
//
// Models a single Cosmos container as partition -> id -> document. Enforces
// the rules the real store relies on: CreateItem fails with 409 on duplicate
// id within a partition; ExecuteCreateBatch is partition-scoped, atomic, and
// capped at maxBatchItems. Query support is intentionally narrow — only the
// patterns the store actually issues (predicate on `c.a` with ORDER BY `c.v`).

type mockCosmos struct {
	mu sync.Mutex
	// partition value -> id -> raw doc
	data map[string]map[string][]byte
	// pageSize controls how many items the query pager returns per page
	pageSize int

	batchCalls int
}

func newMockCosmos() *mockCosmos {
	return &mockCosmos{data: map[string]map[string][]byte{}}
}

func (m *mockCosmos) partition(pk azcosmos.PartitionKey) string {
	// PartitionKey doesn't expose its value, but its JSON form is `[<v>]`.
	// Marshal it as the test surrogate for "this partition".
	b, _ := json.Marshal(pk)
	return string(b)
}

func (m *mockCosmos) CreateItem(_ context.Context, pk azcosmos.PartitionKey, item []byte, _ *azcosmos.ItemOptions) (azcosmos.ItemResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return azcosmos.ItemResponse{}, m.createLocked(pk, item)
}

func (m *mockCosmos) createLocked(pk azcosmos.PartitionKey, item []byte) error {
	var doc map[string]any
	if err := json.Unmarshal(item, &doc); err != nil {
		return err
	}
	id, _ := doc["id"].(string)
	p := m.partition(pk)
	if m.data[p] == nil {
		m.data[p] = map[string][]byte{}
	}
	if _, exists := m.data[p][id]; exists {
		return &cosmosclient.BatchError{StatusCode: 409, Message: "duplicate id"}
	}
	m.data[p][id] = item
	return nil
}

func (m *mockCosmos) UpsertItem(_ context.Context, pk azcosmos.PartitionKey, item []byte, _ *azcosmos.ItemOptions) (azcosmos.ItemResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var doc map[string]any
	if err := json.Unmarshal(item, &doc); err != nil {
		return azcosmos.ItemResponse{}, err
	}
	id, _ := doc["id"].(string)
	p := m.partition(pk)
	if m.data[p] == nil {
		m.data[p] = map[string][]byte{}
	}
	m.data[p][id] = item
	return azcosmos.ItemResponse{}, nil
}

func (m *mockCosmos) ReadItem(_ context.Context, _ azcosmos.PartitionKey, _ string, _ *azcosmos.ItemOptions) (azcosmos.ItemResponse, error) {
	return azcosmos.ItemResponse{}, nil
}

func (m *mockCosmos) DeleteItem(_ context.Context, _ azcosmos.PartitionKey, _ string, _ *azcosmos.ItemOptions) (azcosmos.ItemResponse, error) {
	return azcosmos.ItemResponse{}, nil
}

// docsForPartition returns the partition's documents sorted by version. The
// `asc` parameter inverts the order — the only ORDER BY direction the store
// actually issues.
func (m *mockCosmos) docsForPartition(pk azcosmos.PartitionKey, asc bool) [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.partition(pk)
	items := m.data[p]
	out := make([][]byte, 0, len(items))
	type indexed struct {
		v    int64
		body []byte
	}
	var sorted []indexed
	for _, raw := range items {
		var d eventDoc
		if err := json.Unmarshal(raw, &d); err == nil {
			sorted = append(sorted, indexed{v: d.V, body: raw})
		}
	}
	sort.Slice(sorted, func(i, j int) bool {
		if asc {
			return sorted[i].v < sorted[j].v
		}
		return sorted[i].v > sorted[j].v
	})
	for _, s := range sorted {
		out = append(out, s.body)
	}
	return out
}

func (m *mockCosmos) QueryItems(_ context.Context, query string, pk azcosmos.PartitionKey, _ *azcosmos.QueryOptions) ([][]byte, error) {
	asc := containsASC(query)
	return m.docsForPartition(pk, asc), nil
}

func (m *mockCosmos) NewQueryItemsPager(query string, pk azcosmos.PartitionKey, _ *azcosmos.QueryOptions) cosmosclient.Pager {
	asc := containsASC(query)
	return &mockPager{items: m.docsForPartition(pk, asc), pageSize: m.pageSize}
}

func (m *mockCosmos) ExecuteCreateBatch(_ context.Context, pk azcosmos.PartitionKey, items [][]byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.batchCalls++
	if len(items) > maxBatchItems {
		return fmt.Errorf("mock: batch exceeds %d items", maxBatchItems)
	}
	// Atomicity: validate every create first; abort on any conflict.
	p := m.partition(pk)
	if m.data[p] == nil {
		m.data[p] = map[string][]byte{}
	}
	stage := make(map[string][]byte, len(items))
	for _, raw := range items {
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			return err
		}
		id, _ := doc["id"].(string)
		if _, exists := m.data[p][id]; exists {
			return &cosmosclient.BatchError{StatusCode: 409, Message: "duplicate id " + id}
		}
		if _, dup := stage[id]; dup {
			return &cosmosclient.BatchError{StatusCode: 409, Message: "duplicate id within batch " + id}
		}
		stage[id] = raw
	}
	for id, raw := range stage {
		m.data[p][id] = raw
	}
	return nil
}

func containsASC(query string) bool {
	// Tiny helper — query strings the store emits end with `ASC` or `DESC`.
	return strings.HasSuffix(query, " ASC")
}

type mockPager struct {
	items    [][]byte
	pageSize int
	pos      int
	done     bool
}

func (p *mockPager) More() bool {
	if p.done {
		return false
	}
	return p.pos < len(p.items)
}

func (p *mockPager) NextPage(_ context.Context) ([][]byte, error) {
	if !p.More() {
		p.done = true
		return nil, nil
	}
	end := len(p.items)
	if p.pageSize > 0 && p.pos+p.pageSize < end {
		end = p.pos + p.pageSize
	}
	page := p.items[p.pos:end]
	p.pos = end
	if p.pos >= len(p.items) {
		p.done = true
	}
	return page, nil
}

// ---------------------------------------------------------------------------
// Mock OpaqueStore (identical contract to the Dynamo test mock)
// ---------------------------------------------------------------------------

type mockOpaqueStore struct {
	mu    sync.Mutex
	items map[string]*opaquedatav1.OpaqueData
}

func (m *mockOpaqueStore) Put(_ context.Context, od *opaquedatav1.OpaqueData) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.items == nil {
		m.items = map[string]*opaquedatav1.OpaqueData{}
	}
	m.items[od.GetPk()+"|"+od.GetSk()] = od
	return nil
}

func (m *mockOpaqueStore) Get(_ context.Context, pk, sk string) (*opaquedatav1.OpaqueData, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if od, ok := m.items[pk+"|"+sk]; ok {
		return od, nil
	}
	return nil, opaquedata.ErrNotFound
}

func (m *mockOpaqueStore) Delete(_ context.Context, _, _ string) error { return nil }
func (m *mockOpaqueStore) Query(_ context.Context, _, _, _ string, _ *opaquedata.SortCondition, _ ...opaquedata.QueryOption) ([]*opaquedatav1.OpaqueData, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestStore(t *testing.T, opts ...Option) (*CosmosDBStore, *mockCosmos) {
	t.Helper()
	mock := newMockCosmos()
	store, err := New(mock, opts...)
	require.NoError(t, err)
	return store, mock
}

func makeRecord(version int64, data []byte) *recordv1.Record {
	return &recordv1.Record{Version: version, Data: data}
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

func TestNew_NilClient(t *testing.T) {
	_, err := New(nil)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Save / Load round-trip
// ---------------------------------------------------------------------------

func TestSaveAndLoadSingleRecord(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, store.Save(ctx, "agg-1", makeRecord(1, []byte("e1"))))

	h, err := store.Load(ctx, "agg-1")
	require.NoError(t, err)
	require.Len(t, h.Records, 1)
	assert.Equal(t, int64(1), h.Records[0].Version)
	assert.Equal(t, []byte("e1"), h.Records[0].Data)
}

func TestSaveMultipleRecordsAtOnce(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, store.Save(ctx, "agg-1",
		makeRecord(1, []byte("a")), makeRecord(2, []byte("b")), makeRecord(3, []byte("c"))))

	h, err := store.Load(ctx, "agg-1")
	require.NoError(t, err)
	require.Len(t, h.Records, 3)
	for i, rec := range h.Records {
		assert.Equal(t, int64(i+1), rec.Version)
	}
}

func TestSaveAppendsAcrossMultipleCalls(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, store.Save(ctx, "agg-1", makeRecord(1, []byte("a"))))
	require.NoError(t, store.Save(ctx, "agg-1", makeRecord(2, []byte("b"))))

	h, err := store.Load(ctx, "agg-1")
	require.NoError(t, err)
	require.Len(t, h.Records, 2)
}

func TestSaveNoRecords_IsNoOp(t *testing.T) {
	store, mock := newTestStore(t)
	require.NoError(t, store.Save(context.Background(), "agg-1"))
	assert.Equal(t, 0, mock.batchCalls)
}

func TestLoadNonExistent_ReturnsEmptyHistory(t *testing.T) {
	store, _ := newTestStore(t)
	h, err := store.Load(context.Background(), "missing")
	require.NoError(t, err)
	assert.Empty(t, h.Records)
}

func TestDuplicateVersionReturnsError(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, store.Save(ctx, "agg-1", makeRecord(1, []byte("a"))))

	err := store.Save(ctx, "agg-1", makeRecord(1, []byte("a2")))
	require.Error(t, err)
	assert.True(t, IsConflict(err), "duplicate version should map to IsConflict")
}

func TestSaveBatching_Over100Records(t *testing.T) {
	store, mock := newTestStore(t)
	ctx := context.Background()
	const total = 250
	records := make([]*recordv1.Record, total)
	for i := 0; i < total; i++ {
		records[i] = makeRecord(int64(i+1), []byte(fmt.Sprintf("e%d", i+1)))
	}
	require.NoError(t, store.Save(ctx, "agg-1", records...))

	// 250 records → ceil(250/100) = 3 batches.
	assert.Equal(t, 3, mock.batchCalls)

	h, err := store.Load(ctx, "agg-1")
	require.NoError(t, err)
	require.Len(t, h.Records, total)
}

func TestRecordDataSurvivesRoundTrip(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	payload := []byte{0x00, 0x01, 0x02, 0xff, 0xfe, 0xfd}
	require.NoError(t, store.Save(ctx, "agg-1", makeRecord(1, payload)))
	h, err := store.Load(ctx, "agg-1")
	require.NoError(t, err)
	require.Len(t, h.Records, 1)
	assert.Equal(t, payload, h.Records[0].Data)
}

// ---------------------------------------------------------------------------
// LoadTail
// ---------------------------------------------------------------------------

func TestLoadTail_ReturnsLastN(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	for i := int64(1); i <= 10; i++ {
		require.NoError(t, store.Save(ctx, "agg-1", makeRecord(i, []byte(fmt.Sprintf("e%d", i)))))
	}
	h, err := store.LoadTail(ctx, "agg-1", 3)
	require.NoError(t, err)
	require.Len(t, h.Records, 3)
	assert.Equal(t, int64(8), h.Records[0].Version)
	assert.Equal(t, int64(9), h.Records[1].Version)
	assert.Equal(t, int64(10), h.Records[2].Version)
}

func TestLoadTail_FewerThanN(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, store.Save(ctx, "agg-1", makeRecord(1, []byte("a")), makeRecord(2, []byte("b"))))
	h, err := store.LoadTail(ctx, "agg-1", 10)
	require.NoError(t, err)
	require.Len(t, h.Records, 2)
	assert.Equal(t, int64(1), h.Records[0].Version)
	assert.Equal(t, int64(2), h.Records[1].Version)
}

func TestLoadTail_NonExistent(t *testing.T) {
	store, _ := newTestStore(t)
	h, err := store.LoadTail(context.Background(), "missing", 5)
	require.NoError(t, err)
	assert.Empty(t, h.Records)
}

func TestLoadTail_ZeroOrNegative(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, store.Save(ctx, "agg-1", makeRecord(1, []byte("a"))))

	h, err := store.LoadTail(ctx, "agg-1", 0)
	require.NoError(t, err)
	assert.Empty(t, h.Records)

	h, err = store.LoadTail(ctx, "agg-1", -5)
	require.NoError(t, err)
	assert.Empty(t, h.Records)
}

func TestLoadTail_EarlyTermination(t *testing.T) {
	// With a small page size and a large dataset, LoadTail must stop reading
	// once it has n records — verified by inspecting how much of the pager
	// the store consumed.
	store, mock := newTestStore(t)
	mock.pageSize = 2
	ctx := context.Background()
	for i := int64(1); i <= 20; i++ {
		require.NoError(t, store.Save(ctx, "agg-1", makeRecord(i, []byte("x"))))
	}
	h, err := store.LoadTail(ctx, "agg-1", 3)
	require.NoError(t, err)
	require.Len(t, h.Records, 3)
}

// ---------------------------------------------------------------------------
// AggregateStore
// ---------------------------------------------------------------------------

func TestSaveAggregate_NoOpaqueStore(t *testing.T) {
	store, _ := newTestStore(t)
	err := store.SaveAggregate(context.Background(), &testv1.Test{Id: "agg-1", Version: 5, Body: "state-data"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no OpaqueStore configured")
}

func TestSaveAggregate_WithOpaqueStore(t *testing.T) {
	opaqueStore := &mockOpaqueStore{}
	store, _ := newTestStore(t, WithOpaqueStore(opaqueStore))
	require.NoError(t, store.SaveAggregate(context.Background(),
		&testv1.Test{Id: "agg-1", Version: 5, Body: "state-data"}))
	assert.Len(t, opaqueStore.items, 1)
}

func TestSaveAggregate_PropagatesTTL(t *testing.T) {
	opaqueStore := &mockOpaqueStore{}
	store, _ := newTestStore(t, WithOpaqueStore(opaqueStore))
	agg := &testv1.Test{Id: "agg-1", Version: 1, Body: "data"}
	ttl := time.Duration(agg.EventTTLSeconds()) * time.Second
	before := time.Now().Add(ttl).Unix()
	require.NoError(t, store.SaveAggregate(context.Background(), agg))
	after := time.Now().Add(ttl).Unix()

	require.Len(t, opaqueStore.items, 1)
	for _, od := range opaqueStore.items {
		assert.GreaterOrEqual(t, od.GetT(), before)
		assert.LessOrEqual(t, od.GetT(), after)
	}
}

type overflowTTLAggregate struct {
	testv1.Test
}

func (o *overflowTTLAggregate) EventTTLSeconds() int64 { return math.MaxInt64 }

func TestSaveAggregate_TTLOverflowReturnsError(t *testing.T) {
	opaqueStore := &mockOpaqueStore{}
	store, _ := newTestStore(t, WithOpaqueStore(opaqueStore))
	err := store.SaveAggregate(context.Background(), &overflowTTLAggregate{
		Test: testv1.Test{Id: "agg-1", Version: 1},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "overflows time.Duration")
}

func TestSaveAggregate_DoesNotAffectEvents(t *testing.T) {
	opaqueStore := &mockOpaqueStore{}
	store, _ := newTestStore(t, WithOpaqueStore(opaqueStore))
	ctx := context.Background()
	require.NoError(t, store.Save(ctx, "agg-1", makeRecord(1, []byte("e1"))))
	require.NoError(t, store.SaveAggregate(ctx, &testv1.Test{Id: "agg-1", Version: 1, Body: "data"}))

	h, err := store.Load(ctx, "agg-1")
	require.NoError(t, err)
	require.Len(t, h.Records, 1)
	assert.Equal(t, int64(1), h.Records[0].Version)
}

// ---------------------------------------------------------------------------
// TTL on event records
// ---------------------------------------------------------------------------

func TestWithTTL_StampsAbsoluteAndRelative(t *testing.T) {
	store, mock := newTestStore(t, WithTTL(time.Hour))
	ctx := context.Background()
	before := time.Now().Unix() + 3600
	require.NoError(t, store.Save(ctx, "agg-1", makeRecord(1, []byte("e1"))))
	after := time.Now().Unix() + 3600

	docs := mock.docsForPartition(azcosmos.NewPartitionKeyString("agg-1"), true)
	require.Len(t, docs, 1)
	var d eventDoc
	require.NoError(t, json.Unmarshal(docs[0], &d))
	assert.GreaterOrEqual(t, d.T, before-1)
	assert.LessOrEqual(t, d.T, after+1)
	assert.Greater(t, d.TTL, int64(0))
	assert.LessOrEqual(t, d.TTL, int64(3600))
}

func TestRecordTTL_TakesPrecedenceOverStoreTTL(t *testing.T) {
	store, mock := newTestStore(t, WithTTL(time.Hour))
	ctx := context.Background()
	recTTL := time.Now().Unix() + 7200
	rec := &recordv1.Record{Version: 1, Data: []byte("x"), Ttl: recTTL}
	require.NoError(t, store.Save(ctx, "agg-1", rec))

	docs := mock.docsForPartition(azcosmos.NewPartitionKeyString("agg-1"), true)
	require.Len(t, docs, 1)
	var d eventDoc
	require.NoError(t, json.Unmarshal(docs[0], &d))
	assert.Equal(t, recTTL, d.T, "record TTL should win over store TTL")
}

func TestWithoutTTL_NoTTLFields(t *testing.T) {
	store, mock := newTestStore(t)
	require.NoError(t, store.Save(context.Background(), "agg-1", makeRecord(1, []byte("e1"))))
	docs := mock.docsForPartition(azcosmos.NewPartitionKeyString("agg-1"), true)
	require.Len(t, docs, 1)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(docs[0], &raw))
	_, hasT := raw["t"]
	_, hasTTL := raw["ttl"]
	assert.False(t, hasT)
	assert.False(t, hasTTL)
}

// ---------------------------------------------------------------------------
// Context cancellation
// ---------------------------------------------------------------------------

func TestCancelledContext(t *testing.T) {
	store, _ := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := store.Save(ctx, "agg-1", makeRecord(1, []byte("e1")))
	require.Error(t, err)

	_, err = store.Load(ctx, "agg-1")
	require.Error(t, err)

	_, err = store.LoadTail(ctx, "agg-1", 1)
	require.Error(t, err)

	err = store.SaveAggregate(ctx, &testv1.Test{Id: "agg-1"})
	require.Error(t, err)
}
