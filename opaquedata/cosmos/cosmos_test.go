package cosmos

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
	"github.com/funinthecloud/protosource/azure/cosmosclient"
	"github.com/funinthecloud/protosource/opaquedata"
	opaquedatav1 "github.com/funinthecloud/protosource/opaquedata/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock ContainerClient
// ---------------------------------------------------------------------------

type queryCall struct {
	query string
	pk    azcosmos.PartitionKey
	opts  *azcosmos.QueryOptions
}

type mockCosmos struct {
	upsertCalls []azcosmos.PartitionKey
	upsertBody  [][]byte
	upsertErr   error

	createErr error

	readPK    []azcosmos.PartitionKey
	readID    []string
	readResp  azcosmos.ItemResponse
	readErr   error

	deletePK  []azcosmos.PartitionKey
	deleteID  []string
	deleteErr error

	queryCalls   []queryCall
	queryResults [][]byte
	queryErr     error
}

func (m *mockCosmos) CreateItem(_ context.Context, pk azcosmos.PartitionKey, item []byte, _ *azcosmos.ItemOptions) (azcosmos.ItemResponse, error) {
	m.upsertCalls = append(m.upsertCalls, pk)
	m.upsertBody = append(m.upsertBody, item)
	return azcosmos.ItemResponse{}, m.createErr
}

func (m *mockCosmos) UpsertItem(_ context.Context, pk azcosmos.PartitionKey, item []byte, _ *azcosmos.ItemOptions) (azcosmos.ItemResponse, error) {
	m.upsertCalls = append(m.upsertCalls, pk)
	m.upsertBody = append(m.upsertBody, item)
	return azcosmos.ItemResponse{}, m.upsertErr
}

func (m *mockCosmos) ReadItem(_ context.Context, pk azcosmos.PartitionKey, id string, _ *azcosmos.ItemOptions) (azcosmos.ItemResponse, error) {
	m.readPK = append(m.readPK, pk)
	m.readID = append(m.readID, id)
	return m.readResp, m.readErr
}

func (m *mockCosmos) DeleteItem(_ context.Context, pk azcosmos.PartitionKey, id string, _ *azcosmos.ItemOptions) (azcosmos.ItemResponse, error) {
	m.deletePK = append(m.deletePK, pk)
	m.deleteID = append(m.deleteID, id)
	return azcosmos.ItemResponse{}, m.deleteErr
}

func (m *mockCosmos) QueryItems(_ context.Context, query string, pk azcosmos.PartitionKey, opts *azcosmos.QueryOptions) ([][]byte, error) {
	m.queryCalls = append(m.queryCalls, queryCall{query: query, pk: pk, opts: opts})
	if m.queryErr != nil {
		return nil, m.queryErr
	}
	return m.queryResults, nil
}

func (m *mockCosmos) NewQueryItemsPager(_ string, _ azcosmos.PartitionKey, _ *azcosmos.QueryOptions) cosmosclient.Pager {
	return nil
}

func (m *mockCosmos) ExecuteCreateBatch(_ context.Context, _ azcosmos.PartitionKey, _ [][]byte) error {
	return nil
}

// notFoundErr fakes an azcore.ResponseError with a 404 status code so Get/Delete
// can exercise the ErrNotFound mapping path.
func notFoundErr() error {
	return &azcore.ResponseError{StatusCode: http.StatusNotFound}
}

func makeDoc(t *testing.T, doc document) []byte {
	t.Helper()
	b, err := json.Marshal(doc)
	require.NoError(t, err)
	return b
}

// ---------------------------------------------------------------------------
// marshalItem / unmarshalItem
// ---------------------------------------------------------------------------

func TestMarshalItem_AllFields(t *testing.T) {
	od := &opaquedatav1.OpaqueData{
		Pk:     "PK",
		Sk:     "SK",
		Body:   []byte("body-data"),
		Gsi1Pk: "G1PK",
		Gsi1Sk: "G1SK",
		Gsi5Pk: "G5PK",
		Gsi5Sk: "G5SK",
	}

	raw, err := marshalItem(od)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))

	assert.Equal(t, "SK", got["id"])
	assert.Equal(t, "PK", got["pk"])
	assert.Equal(t, "SK", got["sk"])
	assert.Equal(t, "G1PK", got["gsi1pk"])
	assert.Equal(t, "G1SK", got["gsi1sk"])
	assert.Equal(t, "G5PK", got["gsi5pk"])
	assert.Equal(t, "G5SK", got["gsi5sk"])
	// Empty GSIs must be omitted to save bytes.
	_, hasG2 := got["gsi2pk"]
	assert.False(t, hasG2)
}

func TestMarshalItem_OmitsEmptyGSIs(t *testing.T) {
	od := &opaquedatav1.OpaqueData{Pk: "PK", Sk: "SK"}
	raw, err := marshalItem(od)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))
	for i := 1; i <= 20; i++ {
		_, ok := got["gsi"+itoa(i)+"pk"]
		assert.False(t, ok, "gsi%dpk should be omitted", i)
	}
}

func TestMarshalItem_CoercesEmptySKToNA(t *testing.T) {
	od := &opaquedatav1.OpaqueData{Pk: "PK", Sk: "SK", Gsi3Pk: "G3PK"}
	raw, err := marshalItem(od)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, "G3PK", got["gsi3pk"])
	assert.Equal(t, "NA", got["gsi3sk"])
}

func TestMarshalItem_WritesTTL(t *testing.T) {
	future := time.Now().Unix() + 3600
	od := &opaquedatav1.OpaqueData{Pk: "PK", Sk: "SK", T: future}

	raw, err := marshalItem(od)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))

	// `t` keeps the absolute epoch for the in-app TTL filter.
	assert.InDelta(t, float64(future), got["t"], 1)
	// `ttl` is the Cosmos-native relative seconds; must be > 0.
	ttlVal, ok := got["ttl"].(float64)
	require.True(t, ok)
	assert.Greater(t, ttlVal, float64(0))
	assert.LessOrEqual(t, ttlVal, float64(3600))
}

func TestMarshalItem_OmitsTTLWhenZero(t *testing.T) {
	od := &opaquedatav1.OpaqueData{Pk: "PK", Sk: "SK"}
	raw, err := marshalItem(od)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))
	_, hasT := got["t"]
	assert.False(t, hasT)
	_, hasTTL := got["ttl"]
	assert.False(t, hasTTL)
}

func TestUnmarshalItem_RoundTrip(t *testing.T) {
	od := &opaquedatav1.OpaqueData{
		Pk:      "PK",
		Sk:      "SK",
		Body:    []byte("payload"),
		Version: 7,
		Gsi1Pk:  "G1PK", Gsi1Sk: "G1SK",
		Gsi20Pk: "G20PK", Gsi20Sk: "G20SK",
	}
	raw, err := marshalItem(od)
	require.NoError(t, err)
	got, err := unmarshalItem(raw)
	require.NoError(t, err)
	assert.Equal(t, "PK", got.GetPk())
	assert.Equal(t, "SK", got.GetSk())
	assert.Equal(t, []byte("payload"), got.GetBody())
	assert.Equal(t, int64(7), got.GetVersion())
	assert.Equal(t, "G1PK", got.GetGsi1Pk())
	assert.Equal(t, "G20SK", got.GetGsi20Sk())
}

// ---------------------------------------------------------------------------
// Store.Query — clause generation
// ---------------------------------------------------------------------------

func TestQuery_NoSort_MainPartition(t *testing.T) {
	mock := &mockCosmos{queryResults: [][]byte{makeDoc(t, document{ID: "sk1", Pk: "pk1", Sk: "sk1"})}}
	store := New(mock)

	results, err := store.Query(context.Background(), "pk", "pk1", "sk", nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "pk1", results[0].GetPk())

	call := mock.queryCalls[0]
	assert.Contains(t, call.query, "c.pk = @pk")
	// No sort condition → only the pk clause + TTL filter + ORDER BY are present.
	assert.NotContains(t, call.query, "c.sk =")
	assert.Contains(t, call.query, "ORDER BY c.sk ASC")
	assert.Contains(t, call.query, "IS_DEFINED(c.t)")
}

func TestQuery_Equal(t *testing.T) {
	mock := &mockCosmos{queryResults: [][]byte{makeDoc(t, document{ID: "sk1", Pk: "pk1", Sk: "sk1"})}}
	store := New(mock)
	_, err := store.Query(context.Background(), "pk", "pk1", "sk", &opaquedata.SortCondition{Operator: opaquedata.Equal, Value: "sk1"})
	require.NoError(t, err)
	assert.Contains(t, mock.queryCalls[0].query, "c.sk = @sk")
}

func TestQuery_Lt(t *testing.T) {
	mock := &mockCosmos{queryResults: [][]byte{makeDoc(t, document{ID: "sk1", Pk: "pk1", Sk: "sk1"})}}
	store := New(mock)
	_, err := store.Query(context.Background(), "pk", "pk1", "sk", &opaquedata.SortCondition{Operator: opaquedata.Lt, Value: "z"})
	require.NoError(t, err)
	assert.Contains(t, mock.queryCalls[0].query, "c.sk < @sk")
}

func TestQuery_Le(t *testing.T) {
	mock := &mockCosmos{queryResults: [][]byte{makeDoc(t, document{ID: "sk1", Pk: "pk1", Sk: "sk1"})}}
	store := New(mock)
	_, err := store.Query(context.Background(), "pk", "pk1", "sk", &opaquedata.SortCondition{Operator: opaquedata.Le, Value: "z"})
	require.NoError(t, err)
	assert.Contains(t, mock.queryCalls[0].query, "c.sk <= @sk")
}

func TestQuery_Gt(t *testing.T) {
	mock := &mockCosmos{queryResults: [][]byte{makeDoc(t, document{ID: "sk1", Pk: "pk1", Sk: "sk1"})}}
	store := New(mock)
	_, err := store.Query(context.Background(), "pk", "pk1", "sk", &opaquedata.SortCondition{Operator: opaquedata.Gt, Value: "a"})
	require.NoError(t, err)
	assert.Contains(t, mock.queryCalls[0].query, "c.sk > @sk")
}

func TestQuery_Ge(t *testing.T) {
	mock := &mockCosmos{queryResults: [][]byte{makeDoc(t, document{ID: "sk1", Pk: "pk1", Sk: "sk1"})}}
	store := New(mock)
	_, err := store.Query(context.Background(), "pk", "pk1", "sk", &opaquedata.SortCondition{Operator: opaquedata.Ge, Value: "a"})
	require.NoError(t, err)
	assert.Contains(t, mock.queryCalls[0].query, "c.sk >= @sk")
}

func TestQuery_Between(t *testing.T) {
	mock := &mockCosmos{queryResults: [][]byte{makeDoc(t, document{ID: "sk1", Pk: "pk1", Sk: "sk1"})}}
	store := New(mock)
	_, err := store.Query(context.Background(), "pk", "pk1", "sk", &opaquedata.SortCondition{Operator: opaquedata.Between, Value: "a", Value2: "z"})
	require.NoError(t, err)
	assert.Contains(t, mock.queryCalls[0].query, "c.sk BETWEEN @sk AND @sk2")
}

func TestQuery_BeginsWith(t *testing.T) {
	mock := &mockCosmos{queryResults: [][]byte{makeDoc(t, document{ID: "sk1", Pk: "pk1", Sk: "sk1"})}}
	store := New(mock)
	_, err := store.Query(context.Background(), "pk", "pk1", "sk", &opaquedata.SortCondition{Operator: opaquedata.BeginsWith, Value: "PRE"})
	require.NoError(t, err)
	assert.Contains(t, mock.queryCalls[0].query, "STARTSWITH(c.sk, @sk)")
}

func TestQuery_UnknownOperator(t *testing.T) {
	mock := &mockCosmos{}
	store := New(mock)
	_, err := store.Query(context.Background(), "pk", "pk1", "sk", &opaquedata.SortCondition{Operator: opaquedata.SortOperator(99), Value: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown sort operator")
}

func TestQuery_TTLFilter(t *testing.T) {
	mock := &mockCosmos{queryResults: [][]byte{makeDoc(t, document{ID: "sk1", Pk: "pk1", Sk: "sk1"})}}
	store := New(mock)
	_, err := store.Query(context.Background(), "pk", "pk1", "sk", nil)
	require.NoError(t, err)
	q := mock.queryCalls[0].query
	assert.Contains(t, q, "NOT IS_DEFINED(c.t)")
	assert.Contains(t, q, "c.t > @now")
}

func TestQuery_InvalidGSIIndex(t *testing.T) {
	mock := &mockCosmos{}
	store := New(mock)
	_, err := store.Query(context.Background(), "pk", "pk1", "sk", nil, opaquedata.WithGSIIndex(21))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GSI index 21 out of range")

	_, err = store.Query(context.Background(), "pk", "pk1", "sk", nil, opaquedata.WithGSIIndex(-1))
	require.Error(t, err)
}

func TestQuery_EmptyResults(t *testing.T) {
	mock := &mockCosmos{queryResults: nil}
	store := New(mock)
	got, err := store.Query(context.Background(), "pk", "pk1", "sk", nil)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestQuery_ParametersBound(t *testing.T) {
	mock := &mockCosmos{queryResults: [][]byte{makeDoc(t, document{ID: "sk1", Pk: "pk1", Sk: "sk1"})}}
	store := New(mock)
	_, err := store.Query(context.Background(), "pk", "pk1", "sk", &opaquedata.SortCondition{Operator: opaquedata.Between, Value: "a", Value2: "z"})
	require.NoError(t, err)

	params := mock.queryCalls[0].opts.QueryParameters
	have := map[string]any{}
	for _, p := range params {
		have[p.Name] = p.Value
	}
	assert.Equal(t, "pk1", have["@pk"])
	assert.Equal(t, "a", have["@sk"])
	assert.Equal(t, "z", have["@sk2"])
	_, hasNow := have["@now"]
	assert.True(t, hasNow)
}

// ---------------------------------------------------------------------------
// Store.Put / Get / Delete
// ---------------------------------------------------------------------------

func TestStore_Put(t *testing.T) {
	mock := &mockCosmos{}
	store := New(mock)
	od := &opaquedatav1.OpaqueData{Pk: "PK", Sk: "SK", Body: []byte("data")}
	require.NoError(t, store.Put(context.Background(), od))
	require.Len(t, mock.upsertCalls, 1)

	// The body must round-trip through the document JSON envelope.
	got, err := unmarshalItem(mock.upsertBody[0])
	require.NoError(t, err)
	assert.Equal(t, "PK", got.GetPk())
	assert.Equal(t, []byte("data"), got.GetBody())
}

func TestStore_Get(t *testing.T) {
	doc := makeDoc(t, document{ID: "SK", Pk: "PK", Sk: "SK", Body: []byte("data")})
	mock := &mockCosmos{readResp: azcosmos.ItemResponse{Value: doc}}
	store := New(mock)

	od, err := store.Get(context.Background(), "PK", "SK")
	require.NoError(t, err)
	assert.Equal(t, "PK", od.GetPk())
	assert.Equal(t, []byte("data"), od.GetBody())
	assert.Equal(t, []string{"SK"}, mock.readID)
}

func TestStore_Get_NotFound(t *testing.T) {
	mock := &mockCosmos{readErr: notFoundErr()}
	store := New(mock)
	_, err := store.Get(context.Background(), "PK", "SK")
	assert.ErrorIs(t, err, opaquedata.ErrNotFound)
}

func TestStore_Get_OtherError(t *testing.T) {
	mock := &mockCosmos{readErr: errors.New("boom")}
	store := New(mock)
	_, err := store.Get(context.Background(), "PK", "SK")
	require.Error(t, err)
	assert.NotErrorIs(t, err, opaquedata.ErrNotFound)
}

func TestStore_Delete(t *testing.T) {
	mock := &mockCosmos{}
	store := New(mock)
	require.NoError(t, store.Delete(context.Background(), "PK", "SK"))
	require.Len(t, mock.deleteID, 1)
	assert.Equal(t, "SK", mock.deleteID[0])
}

func TestStore_Delete_NotFoundIsNoop(t *testing.T) {
	mock := &mockCosmos{deleteErr: notFoundErr()}
	store := New(mock)
	// Deleting a missing item is idempotent — the dynamo adapter has the same
	// behavior implicitly (DynamoDB returns success on missing keys).
	require.NoError(t, store.Delete(context.Background(), "PK", "SK"))
}

// itoa avoids pulling strconv just for a single int→ascii in test loops.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [3]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
