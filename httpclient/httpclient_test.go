package httpclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	historyv1 "github.com/funinthecloud/protosource/history/v1"
	recordv1 "github.com/funinthecloud/protosource/record/v1"
	responsev1 "github.com/funinthecloud/protosource/response/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestApply_JSON(t *testing.T) {
	var gotContentType, gotAccept, gotAuth string
	var gotBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		gotAccept = r.Header.Get("Accept")
		gotAuth = r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"abc","version":3}`))
	}))
	defer server.Close()

	auth := NewBearerTokenAuth("tok123", "testuser")
	c := New(server.URL, auth, WithJSON())

	// Use a History proto as a stand-in command (has fields we can verify).
	cmd := &historyv1.History{}
	result, err := c.Apply(context.Background(), "sample/v1", cmd)

	require.NoError(t, err)
	assert.Equal(t, "abc", result.GetId())
	assert.Equal(t, int64(3), result.GetVersion())
	assert.Equal(t, "application/json", gotContentType)
	assert.Equal(t, "application/json", gotAccept)
	assert.Equal(t, "Bearer tok123", gotAuth)
	assert.NotEmpty(t, gotBody)
}

func TestApply_Protobuf(t *testing.T) {
	var gotContentType string

	resp := &responsev1.CommandResponse{Id: "xyz", Version: 1}
	respBytes, err := proto.Marshal(resp)
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/protobuf")
		w.Write(respBytes)
	}))
	defer server.Close()

	c := New(server.URL, NewNoAuth("actor"))
	result, err := c.Apply(context.Background(), "test/v1", &historyv1.History{})

	require.NoError(t, err)
	assert.Equal(t, "xyz", result.GetId())
	assert.Equal(t, int64(1), result.GetVersion())
	assert.Equal(t, "application/protobuf", gotContentType)
}

func TestApply_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"code":"CMD_UNMARSHAL","error":"bad request"}`))
	}))
	defer server.Close()

	c := New(server.URL, NewNoAuth("actor"))
	_, err := c.Apply(context.Background(), "test/v1", &historyv1.History{})

	require.Error(t, err)
	apiErr, ok := err.(*APIError)
	require.True(t, ok)
	assert.Equal(t, 400, apiErr.StatusCode)
	assert.Equal(t, "CMD_UNMARSHAL", apiErr.Code)
	assert.Equal(t, "bad request", apiErr.Message)
}

func TestLoad_JSON(t *testing.T) {
	history := &historyv1.History{
		Records: []*recordv1.Record{
			{Version: 1, Data: []byte("data")},
		},
	}
	jsonBytes, err := protojson.Marshal(history)
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/test/v1/id-123", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.Write(jsonBytes)
	}))
	defer server.Close()

	c := New(server.URL, NewNoAuth("actor"), WithJSON())
	target := &historyv1.History{}
	err = c.Load(context.Background(), "test/v1", "id-123", target)

	require.NoError(t, err)
	assert.Len(t, target.Records, 1)
	assert.Equal(t, int64(1), target.Records[0].Version)
}

func TestLoad_Protobuf(t *testing.T) {
	history := &historyv1.History{
		Records: []*recordv1.Record{
			{Version: 5, Data: []byte("event-data")},
		},
	}
	protoBytes, err := proto.Marshal(history)
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/protobuf")
		w.Write(protoBytes)
	}))
	defer server.Close()

	c := New(server.URL, NewNoAuth("actor"))
	target := &historyv1.History{}
	err = c.Load(context.Background(), "test/v1", "id-456", target)

	require.NoError(t, err)
	assert.Len(t, target.Records, 1)
	assert.Equal(t, int64(5), target.Records[0].Version)
}

func TestLoad_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"code":"GET_NOT_FOUND","error":"aggregate not found"}`))
	}))
	defer server.Close()

	c := New(server.URL, NewNoAuth("actor"))
	err := c.Load(context.Background(), "test/v1", "missing", &historyv1.History{})

	require.Error(t, err)
	apiErr, ok := err.(*APIError)
	require.True(t, ok)
	assert.Equal(t, 404, apiErr.StatusCode)
}

func TestHistory(t *testing.T) {
	history := &historyv1.History{
		Records: []*recordv1.Record{
			{Version: 1, Data: []byte("a")},
			{Version: 2, Data: []byte("b")},
		},
	}
	protoBytes, err := proto.Marshal(history)
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/test/v1/id-789/history", r.URL.Path)
		w.Header().Set("Content-Type", "application/protobuf")
		w.Write(protoBytes)
	}))
	defer server.Close()

	c := New(server.URL, NewNoAuth("actor"))
	result, err := c.History(context.Background(), "test/v1", "id-789")

	require.NoError(t, err)
	assert.Len(t, result.Records, 2)
}

func TestQuery_Protobuf(t *testing.T) {
	// Use History as a stand-in for {Aggregate}List -- both are proto messages.
	history := &historyv1.History{
		Records: []*recordv1.Record{{Version: 1, Data: []byte("x")}},
	}
	protoBytes, err := proto.Marshal(history)
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/test/v1/query/by-customer-id", r.URL.Path)
		assert.Equal(t, "123", r.URL.Query().Get("customer_id"))
		assert.Equal(t, "application/protobuf", r.Header.Get("Accept"))
		w.Header().Set("Content-Type", "application/protobuf")
		w.Write(protoBytes)
	}))
	defer server.Close()

	c := New(server.URL, NewNoAuth("actor"))
	target := &historyv1.History{}
	err = c.Query(context.Background(), "test/v1", "by-customer-id", map[string]string{
		"customer_id": "123",
	}, target)

	require.NoError(t, err)
	assert.Len(t, target.Records, 1)
}

func TestQuery_JSON(t *testing.T) {
	history := &historyv1.History{
		Records: []*recordv1.Record{{Version: 2, Data: []byte("y")}},
	}
	jsonBytes, err := protojson.Marshal(history)
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Accept"))
		w.Header().Set("Content-Type", "application/json")
		w.Write(jsonBytes)
	}))
	defer server.Close()

	c := New(server.URL, NewNoAuth("actor"), WithJSON())
	target := &historyv1.History{}
	err = c.Query(context.Background(), "test/v1", "by-foo", map[string]string{
		"foo": "bar",
	}, target)

	require.NoError(t, err)
	assert.Len(t, target.Records, 1)
	assert.Equal(t, int64(2), target.Records[0].Version)
}

func TestQuery_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"code":"QUERY_MISSING_PK","error":"missing parameter"}`))
	}))
	defer server.Close()

	c := New(server.URL, NewNoAuth("actor"))
	err := c.Query(context.Background(), "test/v1", "by-foo", nil, &historyv1.History{})

	require.Error(t, err)
	apiErr, ok := err.(*APIError)
	require.True(t, ok)
	assert.Equal(t, 400, apiErr.StatusCode)
	assert.Equal(t, "QUERY_MISSING_PK", apiErr.Code)
}

func TestSetActorField(t *testing.T) {
	// Use a Record which has version(1) and data(2) -- field 2 is bytes not string,
	// so setActorField should be a no-op (kind mismatch).
	rec := &recordv1.Record{Version: 1}
	setActorField(rec, "should-not-set")
	assert.Empty(t, rec.Data) // field 2 is bytes, not string -- unchanged
}

func TestNoAuth(t *testing.T) {
	auth := NewNoAuth("dev@local")
	assert.Equal(t, "dev@local", auth.Actor())

	req, _ := http.NewRequest("GET", "http://example.com", nil)
	err := auth.Authenticate(req)
	require.NoError(t, err)
	assert.Empty(t, req.Header.Get("Authorization"))
}

func TestBearerTokenAuth(t *testing.T) {
	auth := NewBearerTokenAuth("secret", "admin")
	assert.Equal(t, "admin", auth.Actor())

	req, _ := http.NewRequest("GET", "http://example.com", nil)
	err := auth.Authenticate(req)
	require.NoError(t, err)
	assert.Equal(t, "Bearer secret", req.Header.Get("Authorization"))
}
