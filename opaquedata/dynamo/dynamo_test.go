package dynamo

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/funinthecloud/protosource/opaquedata"
	opaquedatav1 "github.com/funinthecloud/protosource/opaquedata/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// GetKey / GetItem / GetValue / PrefixPKs tests
// ---------------------------------------------------------------------------

func TestGetKey(t *testing.T) {
	od := &opaquedatav1.OpaqueData{Pk: "USER#1", Sk: "PROFILE#1"}
	key := GetKey(od)

	assert.Equal(t, "USER#1", key["pk"].(*types.AttributeValueMemberS).Value)
	assert.Equal(t, "PROFILE#1", key["sk"].(*types.AttributeValueMemberS).Value)
	assert.Len(t, key, 2)
}

func TestGetItem_AllFields(t *testing.T) {
	od := &opaquedatav1.OpaqueData{
		Pk:     "PK",
		Sk:     "SK",
		Body:   []byte("body-data"),
		Ttl:    1234567890,
		Gsi1Pk: "G1PK",
		Gsi1Sk: "G1SK",
		Gsi5Pk: "G5PK",
	}

	item := GetItem(od)
	assert.Equal(t, "PK", item["pk"].(*types.AttributeValueMemberS).Value)
	assert.Equal(t, "SK", item["sk"].(*types.AttributeValueMemberS).Value)
	assert.Equal(t, []byte("body-data"), item["body"].(*types.AttributeValueMemberB).Value)
	assert.Equal(t, "1234567890", item["ttl"].(*types.AttributeValueMemberN).Value)
	assert.Equal(t, "G1PK", item["gsi1pk"].(*types.AttributeValueMemberS).Value)
	assert.Equal(t, "G1SK", item["gsi1sk"].(*types.AttributeValueMemberS).Value)
	assert.Equal(t, "G5PK", item["gsi5pk"].(*types.AttributeValueMemberS).Value)
}

func TestGetItem_OmitsEmptyGSIs(t *testing.T) {
	od := &opaquedatav1.OpaqueData{Pk: "PK", Sk: "SK", Body: []byte("x")}
	item := GetItem(od)

	_, hasGSI1 := item["gsi1pk"]
	assert.False(t, hasGSI1, "empty GSI should not appear")

	_, hasGSI20 := item["gsi20pk"]
	assert.False(t, hasGSI20, "empty GSI should not appear")
}

func TestGetItem_OmitsZeroTTL(t *testing.T) {
	od := &opaquedatav1.OpaqueData{Pk: "PK", Sk: "SK"}
	item := GetItem(od)
	_, hasTTL := item["ttl"]
	assert.False(t, hasTTL)
}

func TestGetItems_FiltersToNamed(t *testing.T) {
	od := &opaquedatav1.OpaqueData{Pk: "PK", Sk: "SK", Body: []byte("data"), Gsi1Pk: "G"}

	result, err := GetItems(od, "pk", "gsi1pk")
	require.NoError(t, err)
	assert.Len(t, result, 2)
	assert.Equal(t, "PK", result["pk"].(*types.AttributeValueMemberS).Value)
	assert.Equal(t, "G", result["gsi1pk"].(*types.AttributeValueMemberS).Value)
}

func TestGetItems_MissingField(t *testing.T) {
	od := &opaquedatav1.OpaqueData{Pk: "PK", Sk: "SK"}
	_, err := GetItems(od, "gsi1pk")
	assert.Error(t, err)
}

func TestGetExpressionValues_ColonPrefix(t *testing.T) {
	od := &opaquedatav1.OpaqueData{Pk: "PK", Sk: "SK", Gsi1Pk: "G"}

	result, err := GetExpressionValues(od, "pk", "gsi1pk")
	require.NoError(t, err)
	assert.Equal(t, "PK", result[":pk"].(*types.AttributeValueMemberS).Value)
	assert.Equal(t, "G", result[":gsi1pk"].(*types.AttributeValueMemberS).Value)
}

func TestGetValue_ExcludesKeys(t *testing.T) {
	od := &opaquedatav1.OpaqueData{Pk: "PK", Sk: "SK", Body: []byte("data"), Ttl: 123}

	val := GetValue(od)
	_, hasPK := val["pk"]
	assert.False(t, hasPK)
	_, hasSK := val["sk"]
	assert.False(t, hasSK)
	assert.Equal(t, []byte("data"), val["body"].(*types.AttributeValueMemberB).Value)
	assert.Equal(t, "123", val["ttl"].(*types.AttributeValueMemberN).Value)
}

func TestPrefixPKs(t *testing.T) {
	od := &opaquedatav1.OpaqueData{
		Pk:      "USER#1",
		Sk:      "PROFILE#1",
		Gsi1Pk:  "ORG#A",
		Gsi1Sk:  "ROLE#admin",
		Gsi3Pk:  "REGION#us",
		Gsi5Pk:  "NA",
		Gsi20Pk: "STATUS#active",
	}
	PrefixPKs(od, "tenant1")

	assert.Equal(t, "tenant1#USER#1", od.Pk)
	assert.Equal(t, "PROFILE#1", od.Sk, "SK should not be prefixed")
	assert.Equal(t, "tenant1#ORG#A", od.Gsi1Pk)
	assert.Equal(t, "ROLE#admin", od.Gsi1Sk, "GSI SK should not be prefixed")
	assert.Equal(t, "tenant1#REGION#us", od.Gsi3Pk)
	assert.Equal(t, "NA", od.Gsi5Pk, "NA sentinel should not be prefixed")
	assert.Equal(t, "tenant1#STATUS#active", od.Gsi20Pk)
	assert.Equal(t, "", od.Gsi2Pk, "empty GSI PK should remain empty")
}

// ---------------------------------------------------------------------------
// Mock Querier
// ---------------------------------------------------------------------------

type mockDynamo struct {
	queryCalls   []*dynamodb.QueryInput
	queryResults []*dynamodb.QueryOutput
	queryErr     error
	putCalls     []*dynamodb.PutItemInput
	putErr       error
	getCalls     []*dynamodb.GetItemInput
	getResult    *dynamodb.GetItemOutput
	getErr       error
	deleteCalls  []*dynamodb.DeleteItemInput
	deleteErr    error
}

func (m *mockDynamo) Query(_ context.Context, input *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	m.queryCalls = append(m.queryCalls, input)
	if m.queryErr != nil {
		return nil, m.queryErr
	}
	if len(m.queryResults) == 0 {
		return &dynamodb.QueryOutput{}, nil
	}
	result := m.queryResults[0]
	m.queryResults = m.queryResults[1:]
	return result, nil
}

func (m *mockDynamo) PutItem(_ context.Context, input *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	m.putCalls = append(m.putCalls, input)
	return &dynamodb.PutItemOutput{}, m.putErr
}

func (m *mockDynamo) GetItem(_ context.Context, input *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	m.getCalls = append(m.getCalls, input)
	if m.getErr != nil {
		return nil, m.getErr
	}
	if m.getResult != nil {
		return m.getResult, nil
	}
	return &dynamodb.GetItemOutput{}, nil
}

func (m *mockDynamo) DeleteItem(_ context.Context, input *dynamodb.DeleteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	m.deleteCalls = append(m.deleteCalls, input)
	return &dynamodb.DeleteItemOutput{}, m.deleteErr
}

func (m *mockDynamo) UpdateItem(_ context.Context, _ *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	return &dynamodb.UpdateItemOutput{}, nil
}

func makeOpaqueItem(pk, sk string, body []byte) map[string]types.AttributeValue {
	item := map[string]types.AttributeValue{
		"pk": &types.AttributeValueMemberS{Value: pk},
		"sk": &types.AttributeValueMemberS{Value: sk},
	}
	if len(body) > 0 {
		item["body"] = &types.AttributeValueMemberB{Value: body}
	}
	return item
}

// ---------------------------------------------------------------------------
// Store.Query tests (ported from query_test.go)
// ---------------------------------------------------------------------------

func newTestStore(mock *mockDynamo) *Store {
	return New(mock, "table")
}

func TestQuery_Equal(t *testing.T) {
	mock := &mockDynamo{
		queryResults: []*dynamodb.QueryOutput{{
			Items: []map[string]types.AttributeValue{makeOpaqueItem("pk1", "sk1", []byte("data"))},
		}},
	}
	store := newTestStore(mock)
	results, err := store.Query(context.Background(), "pk", "pk1", "sk", &opaquedata.SortCondition{Operator: opaquedata.Equal, Value: "sk1"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "pk1", results[0].GetPk())
	assert.Contains(t, *mock.queryCalls[0].KeyConditionExpression, "#pk = :pk")
	assert.Contains(t, *mock.queryCalls[0].KeyConditionExpression, "#sk = :sk")
}

func TestQuery_Lt(t *testing.T) {
	mock := &mockDynamo{
		queryResults: []*dynamodb.QueryOutput{{
			Items: []map[string]types.AttributeValue{makeOpaqueItem("pk1", "sk1", []byte("d"))},
		}},
	}
	store := newTestStore(mock)
	_, err := store.Query(context.Background(), "pk", "pk1", "sk", &opaquedata.SortCondition{Operator: opaquedata.Lt, Value: "z"})
	require.NoError(t, err)
	assert.Contains(t, *mock.queryCalls[0].KeyConditionExpression, "#sk < :sk")
}

func TestQuery_Le(t *testing.T) {
	mock := &mockDynamo{
		queryResults: []*dynamodb.QueryOutput{{
			Items: []map[string]types.AttributeValue{makeOpaqueItem("pk1", "sk1", nil)},
		}},
	}
	store := newTestStore(mock)
	_, err := store.Query(context.Background(), "pk", "pk1", "sk", &opaquedata.SortCondition{Operator: opaquedata.Le, Value: "z"})
	require.NoError(t, err)
	assert.Contains(t, *mock.queryCalls[0].KeyConditionExpression, "#sk <= :sk")
}

func TestQuery_Gt(t *testing.T) {
	mock := &mockDynamo{
		queryResults: []*dynamodb.QueryOutput{{
			Items: []map[string]types.AttributeValue{makeOpaqueItem("pk1", "sk1", nil)},
		}},
	}
	store := newTestStore(mock)
	_, err := store.Query(context.Background(), "pk", "pk1", "sk", &opaquedata.SortCondition{Operator: opaquedata.Gt, Value: "a"})
	require.NoError(t, err)
	assert.Contains(t, *mock.queryCalls[0].KeyConditionExpression, "#sk > :sk")
}

func TestQuery_Ge(t *testing.T) {
	mock := &mockDynamo{
		queryResults: []*dynamodb.QueryOutput{{
			Items: []map[string]types.AttributeValue{makeOpaqueItem("pk1", "sk1", nil)},
		}},
	}
	store := newTestStore(mock)
	_, err := store.Query(context.Background(), "pk", "pk1", "sk", &opaquedata.SortCondition{Operator: opaquedata.Ge, Value: "a"})
	require.NoError(t, err)
	assert.Contains(t, *mock.queryCalls[0].KeyConditionExpression, "#sk >= :sk")
}

func TestQuery_Between(t *testing.T) {
	mock := &mockDynamo{
		queryResults: []*dynamodb.QueryOutput{{
			Items: []map[string]types.AttributeValue{makeOpaqueItem("pk1", "sk1", nil)},
		}},
	}
	store := newTestStore(mock)
	_, err := store.Query(context.Background(), "pk", "pk1", "sk", &opaquedata.SortCondition{Operator: opaquedata.Between, Value: "a", Value2: "z"})
	require.NoError(t, err)
	assert.Contains(t, *mock.queryCalls[0].KeyConditionExpression, "#sk BETWEEN :sk AND :sk2")
}

func TestQuery_BeginsWith(t *testing.T) {
	mock := &mockDynamo{
		queryResults: []*dynamodb.QueryOutput{{
			Items: []map[string]types.AttributeValue{makeOpaqueItem("pk1", "sk1", nil)},
		}},
	}
	store := newTestStore(mock)
	_, err := store.Query(context.Background(), "pk", "pk1", "sk", &opaquedata.SortCondition{Operator: opaquedata.BeginsWith, Value: "PREFIX"})
	require.NoError(t, err)
	assert.Contains(t, *mock.queryCalls[0].KeyConditionExpression, "begins_with(#sk, :sk)")
}

func TestQuery_NoSort(t *testing.T) {
	mock := &mockDynamo{
		queryResults: []*dynamodb.QueryOutput{{
			Items: []map[string]types.AttributeValue{makeOpaqueItem("pk1", "sk1", nil)},
		}},
	}
	store := newTestStore(mock)
	_, err := store.Query(context.Background(), "pk", "pk1", "sk", nil)
	require.NoError(t, err)
	assert.Equal(t, "#pk = :pk", *mock.queryCalls[0].KeyConditionExpression)
}

// ---------------------------------------------------------------------------
// GSI index naming
// ---------------------------------------------------------------------------

func TestGSIIndex_NamingConvention(t *testing.T) {
	mock := &mockDynamo{
		queryResults: []*dynamodb.QueryOutput{{
			Items: []map[string]types.AttributeValue{makeOpaqueItem("pk1", "sk1", nil)},
		}},
	}
	store := newTestStore(mock)
	_, err := store.Query(context.Background(), "gsi1pk", "val", "gsi1sk", nil, opaquedata.WithGSIIndex(1))
	require.NoError(t, err)
	assert.Equal(t, "gsi1pk-gsi1sk-index", *mock.queryCalls[0].IndexName)
}

func TestGSIIndex_10(t *testing.T) {
	mock := &mockDynamo{
		queryResults: []*dynamodb.QueryOutput{{
			Items: []map[string]types.AttributeValue{makeOpaqueItem("pk1", "sk1", nil)},
		}},
	}
	store := newTestStore(mock)
	_, err := store.Query(context.Background(), "gsi10pk", "val", "gsi10sk", nil, opaquedata.WithGSIIndex(10))
	require.NoError(t, err)
	assert.Equal(t, "gsi10pk-gsi10sk-index", *mock.queryCalls[0].IndexName)
}

func TestGSIIndex_20(t *testing.T) {
	mock := &mockDynamo{
		queryResults: []*dynamodb.QueryOutput{{
			Items: []map[string]types.AttributeValue{makeOpaqueItem("pk1", "sk1", nil)},
		}},
	}
	store := newTestStore(mock)
	_, err := store.Query(context.Background(), "gsi20pk", "val", "gsi20sk", nil, opaquedata.WithGSIIndex(20))
	require.NoError(t, err)
	assert.Equal(t, "gsi20pk-gsi20sk-index", *mock.queryCalls[0].IndexName)
}

func TestGSIIndex_NoGSI(t *testing.T) {
	mock := &mockDynamo{
		queryResults: []*dynamodb.QueryOutput{{
			Items: []map[string]types.AttributeValue{makeOpaqueItem("pk1", "sk1", nil)},
		}},
	}
	store := newTestStore(mock)
	_, err := store.Query(context.Background(), "pk", "val", "sk", nil)
	require.NoError(t, err)
	assert.Nil(t, mock.queryCalls[0].IndexName)
}

// ---------------------------------------------------------------------------
// Pagination
// ---------------------------------------------------------------------------

func TestQuery_Pagination(t *testing.T) {
	page1 := &dynamodb.QueryOutput{
		Items: []map[string]types.AttributeValue{makeOpaqueItem("pk1", "sk1", []byte("a"))},
		LastEvaluatedKey: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: "pk1"},
			"sk": &types.AttributeValueMemberS{Value: "sk1"},
		},
	}
	page2 := &dynamodb.QueryOutput{
		Items: []map[string]types.AttributeValue{makeOpaqueItem("pk1", "sk2", []byte("b"))},
	}
	mock := &mockDynamo{queryResults: []*dynamodb.QueryOutput{page1, page2}}
	store := newTestStore(mock)

	results, err := store.Query(context.Background(), "pk", "pk1", "sk", nil)
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, "sk1", results[0].GetSk())
	assert.Equal(t, "sk2", results[1].GetSk())
	assert.Len(t, mock.queryCalls, 2)
}

// ---------------------------------------------------------------------------
// TTL filter
// ---------------------------------------------------------------------------

func TestQuery_TTLFilter(t *testing.T) {
	mock := &mockDynamo{
		queryResults: []*dynamodb.QueryOutput{{
			Items: []map[string]types.AttributeValue{makeOpaqueItem("pk1", "sk1", nil)},
		}},
	}
	store := newTestStore(mock)
	_, err := store.Query(context.Background(), "pk", "pk1", "sk", nil)
	require.NoError(t, err)
	assert.Contains(t, *mock.queryCalls[0].FilterExpression, "attribute_not_exists(#ttl)")
	assert.Contains(t, *mock.queryCalls[0].FilterExpression, "#ttl > :now")
}

// ---------------------------------------------------------------------------
// Empty results → nil, nil (not ErrNotFound)
// ---------------------------------------------------------------------------

func TestQuery_EmptyResults(t *testing.T) {
	mock := &mockDynamo{
		queryResults: []*dynamodb.QueryOutput{{Items: nil}},
	}
	store := newTestStore(mock)
	results, err := store.Query(context.Background(), "pk", "pk1", "sk", nil)
	assert.NoError(t, err)
	assert.Nil(t, results)
}

// ---------------------------------------------------------------------------
// GSIIndexName helper
// ---------------------------------------------------------------------------

func TestGSIIndexName(t *testing.T) {
	assert.Equal(t, "gsi1pk-gsi1sk-index", GSIIndexName(1))
	assert.Equal(t, "gsi10pk-gsi10sk-index", GSIIndexName(10))
	assert.Equal(t, "gsi20pk-gsi20sk-index", GSIIndexName(20))
}

func TestQuery_InvalidGSIIndex(t *testing.T) {
	mock := &mockDynamo{}
	store := newTestStore(mock)
	_, err := store.Query(context.Background(), "pk", "pk1", "sk", nil, opaquedata.WithGSIIndex(21))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GSI index 21 out of range [0,20]")

	_, err = store.Query(context.Background(), "pk", "pk1", "sk", nil, opaquedata.WithGSIIndex(-1))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GSI index -1 out of range [0,20]")
}

// ---------------------------------------------------------------------------
// UnmarshalMap round-trip (replaces former itemToOpaqueData test)
// ---------------------------------------------------------------------------

func TestUnmarshalMap_FullItem(t *testing.T) {
	item := map[string]types.AttributeValue{
		"pk":      &types.AttributeValueMemberS{Value: "PK"},
		"sk":      &types.AttributeValueMemberS{Value: "SK"},
		"body":    &types.AttributeValueMemberB{Value: []byte("body")},
		"ttl":     &types.AttributeValueMemberN{Value: "999"},
		"version": &types.AttributeValueMemberN{Value: "0"},
		"gsi1pk":  &types.AttributeValueMemberS{Value: "G1PK"},
		"gsi20sk": &types.AttributeValueMemberS{Value: "G20SK"},
	}
	var od opaquedatav1.OpaqueData
	require.NoError(t, attributevalue.UnmarshalMap(item, &od))
	assert.Equal(t, "PK", od.GetPk())
	assert.Equal(t, "SK", od.GetSk())
	assert.Equal(t, []byte("body"), od.GetBody())
	assert.Equal(t, int64(999), od.GetTtl())
	assert.Equal(t, int64(0), od.GetVersion())
	assert.Equal(t, "G1PK", od.GetGsi1Pk())
	assert.Equal(t, "G20SK", od.GetGsi20Sk())
}

// ---------------------------------------------------------------------------
// Store.Put / Store.Get / Store.Delete
// ---------------------------------------------------------------------------

func TestStore_Put(t *testing.T) {
	mock := &mockDynamo{}
	store := New(mock, "my-table")

	od := &opaquedatav1.OpaqueData{Pk: "PK", Sk: "SK", Body: []byte("data")}
	err := store.Put(context.Background(), od)
	require.NoError(t, err)
	require.Len(t, mock.putCalls, 1)
	assert.Equal(t, "my-table", *mock.putCalls[0].TableName)
}

func TestStore_Put_WithTenantPrefix(t *testing.T) {
	mock := &mockDynamo{}
	store := New(mock, "my-table", WithTenantPrefix("tenant1"))

	od := &opaquedatav1.OpaqueData{Pk: "USER#1", Sk: "SK", Gsi1Pk: "ORG#A"}
	err := store.Put(context.Background(), od)
	require.NoError(t, err)
	require.Len(t, mock.putCalls, 1)
	// PK and GSI PKs should be prefixed
	item := mock.putCalls[0].Item
	assert.Equal(t, "tenant1#USER#1", item["pk"].(*types.AttributeValueMemberS).Value)
	assert.Equal(t, "tenant1#ORG#A", item["gsi1pk"].(*types.AttributeValueMemberS).Value)
}

func TestStore_Get(t *testing.T) {
	item := map[string]types.AttributeValue{
		"pk":   &types.AttributeValueMemberS{Value: "PK"},
		"sk":   &types.AttributeValueMemberS{Value: "SK"},
		"body": &types.AttributeValueMemberB{Value: []byte("data")},
	}
	mock := &mockDynamo{getResult: &dynamodb.GetItemOutput{Item: item}}
	store := New(mock, "my-table")

	od, err := store.Get(context.Background(), "PK", "SK")
	require.NoError(t, err)
	assert.Equal(t, "PK", od.GetPk())
	assert.Equal(t, []byte("data"), od.GetBody())
}

func TestStore_Get_NotFound(t *testing.T) {
	mock := &mockDynamo{getResult: &dynamodb.GetItemOutput{}}
	store := New(mock, "my-table")

	_, err := store.Get(context.Background(), "PK", "SK")
	assert.ErrorIs(t, err, opaquedata.ErrNotFound)
}

func TestStore_Delete(t *testing.T) {
	mock := &mockDynamo{}
	store := New(mock, "my-table")

	err := store.Delete(context.Background(), "PK", "SK")
	require.NoError(t, err)
	require.Len(t, mock.deleteCalls, 1)
	assert.Equal(t, "my-table", *mock.deleteCalls[0].TableName)
}
