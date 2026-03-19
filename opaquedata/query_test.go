package opaquedata

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock Querier
// ---------------------------------------------------------------------------

type mockQuerier struct {
	calls   []*dynamodb.QueryInput
	results []*dynamodb.QueryOutput
	err     error
}

func (m *mockQuerier) Query(_ context.Context, input *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	m.calls = append(m.calls, input)
	if m.err != nil {
		return nil, m.err
	}
	if len(m.results) == 0 {
		return &dynamodb.QueryOutput{}, nil
	}
	result := m.results[0]
	m.results = m.results[1:]
	return result, nil
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
// SortOperator expression tests
// ---------------------------------------------------------------------------

func TestQueryPKSK_Equal(t *testing.T) {
	mock := &mockQuerier{
		results: []*dynamodb.QueryOutput{{
			Items: []map[string]types.AttributeValue{makeOpaqueItem("pk1", "sk1", []byte("data"))},
		}},
	}
	results, err := QueryPKSK(context.Background(), mock, "table", "pk", "pk1", "sk", &SortCondition{Operator: Equal, Value: "sk1"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "pk1", results[0].GetPk())
	assert.Contains(t, *mock.calls[0].KeyConditionExpression, "#pk = :pk")
	assert.Contains(t, *mock.calls[0].KeyConditionExpression, "#sk = :sk")
}

func TestQueryPKSK_Lt(t *testing.T) {
	mock := &mockQuerier{
		results: []*dynamodb.QueryOutput{{
			Items: []map[string]types.AttributeValue{makeOpaqueItem("pk1", "sk1", []byte("d"))},
		}},
	}
	_, err := QueryPKSK(context.Background(), mock, "table", "pk", "pk1", "sk", &SortCondition{Operator: Lt, Value: "z"})
	require.NoError(t, err)
	assert.Contains(t, *mock.calls[0].KeyConditionExpression, "#sk < :sk")
}

func TestQueryPKSK_Le(t *testing.T) {
	mock := &mockQuerier{
		results: []*dynamodb.QueryOutput{{
			Items: []map[string]types.AttributeValue{makeOpaqueItem("pk1", "sk1", nil)},
		}},
	}
	_, err := QueryPKSK(context.Background(), mock, "table", "pk", "pk1", "sk", &SortCondition{Operator: Le, Value: "z"})
	require.NoError(t, err)
	assert.Contains(t, *mock.calls[0].KeyConditionExpression, "#sk <= :sk")
}

func TestQueryPKSK_Gt(t *testing.T) {
	mock := &mockQuerier{
		results: []*dynamodb.QueryOutput{{
			Items: []map[string]types.AttributeValue{makeOpaqueItem("pk1", "sk1", nil)},
		}},
	}
	_, err := QueryPKSK(context.Background(), mock, "table", "pk", "pk1", "sk", &SortCondition{Operator: Gt, Value: "a"})
	require.NoError(t, err)
	assert.Contains(t, *mock.calls[0].KeyConditionExpression, "#sk > :sk")
}

func TestQueryPKSK_Ge(t *testing.T) {
	mock := &mockQuerier{
		results: []*dynamodb.QueryOutput{{
			Items: []map[string]types.AttributeValue{makeOpaqueItem("pk1", "sk1", nil)},
		}},
	}
	_, err := QueryPKSK(context.Background(), mock, "table", "pk", "pk1", "sk", &SortCondition{Operator: Ge, Value: "a"})
	require.NoError(t, err)
	assert.Contains(t, *mock.calls[0].KeyConditionExpression, "#sk >= :sk")
}

func TestQueryPKSK_Between(t *testing.T) {
	mock := &mockQuerier{
		results: []*dynamodb.QueryOutput{{
			Items: []map[string]types.AttributeValue{makeOpaqueItem("pk1", "sk1", nil)},
		}},
	}
	_, err := QueryPKSK(context.Background(), mock, "table", "pk", "pk1", "sk", &SortCondition{Operator: Between, Value: "a", Value2: "z"})
	require.NoError(t, err)
	assert.Contains(t, *mock.calls[0].KeyConditionExpression, "#sk BETWEEN :sk AND :sk2")
}

func TestQueryPKSK_BeginsWith(t *testing.T) {
	mock := &mockQuerier{
		results: []*dynamodb.QueryOutput{{
			Items: []map[string]types.AttributeValue{makeOpaqueItem("pk1", "sk1", nil)},
		}},
	}
	_, err := QueryPKSK(context.Background(), mock, "table", "pk", "pk1", "sk", &SortCondition{Operator: BeginsWith, Value: "PREFIX"})
	require.NoError(t, err)
	assert.Contains(t, *mock.calls[0].KeyConditionExpression, "begins_with(#sk, :sk)")
}

func TestQueryPKSK_NoSort(t *testing.T) {
	mock := &mockQuerier{
		results: []*dynamodb.QueryOutput{{
			Items: []map[string]types.AttributeValue{makeOpaqueItem("pk1", "sk1", nil)},
		}},
	}
	_, err := QueryPKSK(context.Background(), mock, "table", "pk", "pk1", "sk", nil)
	require.NoError(t, err)
	assert.Equal(t, "#pk = :pk", *mock.calls[0].KeyConditionExpression)
}

// ---------------------------------------------------------------------------
// GSI index naming
// ---------------------------------------------------------------------------

func TestGSIIndex_NamingConvention(t *testing.T) {
	mock := &mockQuerier{
		results: []*dynamodb.QueryOutput{{
			Items: []map[string]types.AttributeValue{makeOpaqueItem("pk1", "sk1", nil)},
		}},
	}
	_, err := QueryPKSK(context.Background(), mock, "table", "gsi1pk", "val", "gsi1sk", nil, WithGSIIndex(1))
	require.NoError(t, err)
	assert.Equal(t, "gsi1pk-gsi1sk-index", *mock.calls[0].IndexName)
}

func TestGSIIndex_10(t *testing.T) {
	mock := &mockQuerier{
		results: []*dynamodb.QueryOutput{{
			Items: []map[string]types.AttributeValue{makeOpaqueItem("pk1", "sk1", nil)},
		}},
	}
	_, err := QueryPKSK(context.Background(), mock, "table", "gsi10pk", "val", "gsi10sk", nil, WithGSIIndex(10))
	require.NoError(t, err)
	assert.Equal(t, "gsi10pk-gsi10sk-index", *mock.calls[0].IndexName)
}

func TestGSIIndex_20(t *testing.T) {
	mock := &mockQuerier{
		results: []*dynamodb.QueryOutput{{
			Items: []map[string]types.AttributeValue{makeOpaqueItem("pk1", "sk1", nil)},
		}},
	}
	_, err := QueryPKSK(context.Background(), mock, "table", "gsi20pk", "val", "gsi20sk", nil, WithGSIIndex(20))
	require.NoError(t, err)
	assert.Equal(t, "gsi20pk-gsi20sk-index", *mock.calls[0].IndexName)
}

func TestGSIIndex_NoGSI(t *testing.T) {
	mock := &mockQuerier{
		results: []*dynamodb.QueryOutput{{
			Items: []map[string]types.AttributeValue{makeOpaqueItem("pk1", "sk1", nil)},
		}},
	}
	_, err := QueryPKSK(context.Background(), mock, "table", "pk", "val", "sk", nil)
	require.NoError(t, err)
	assert.Nil(t, mock.calls[0].IndexName)
}

// ---------------------------------------------------------------------------
// Pagination
// ---------------------------------------------------------------------------

func TestQueryPKSK_Pagination(t *testing.T) {
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
	mock := &mockQuerier{results: []*dynamodb.QueryOutput{page1, page2}}

	results, err := QueryPKSK(context.Background(), mock, "table", "pk", "pk1", "sk", nil)
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, "sk1", results[0].GetSk())
	assert.Equal(t, "sk2", results[1].GetSk())
	assert.Len(t, mock.calls, 2)
}

// ---------------------------------------------------------------------------
// TTL filter
// ---------------------------------------------------------------------------

func TestQueryPKSK_TTLFilter(t *testing.T) {
	mock := &mockQuerier{
		results: []*dynamodb.QueryOutput{{
			Items: []map[string]types.AttributeValue{makeOpaqueItem("pk1", "sk1", nil)},
		}},
	}
	_, err := QueryPKSK(context.Background(), mock, "table", "pk", "pk1", "sk", nil)
	require.NoError(t, err)
	assert.Contains(t, *mock.calls[0].FilterExpression, "attribute_not_exists(#ttl)")
	assert.Contains(t, *mock.calls[0].FilterExpression, "#ttl > :now")
}

// ---------------------------------------------------------------------------
// Empty results → ErrNotFound
// ---------------------------------------------------------------------------

func TestQueryPKSK_EmptyResults(t *testing.T) {
	mock := &mockQuerier{
		results: []*dynamodb.QueryOutput{{Items: nil}},
	}
	_, err := QueryPKSK(context.Background(), mock, "table", "pk", "pk1", "sk", nil)
	assert.ErrorIs(t, err, ErrNotFound)
}

// ---------------------------------------------------------------------------
// GSIIndexName helper
// ---------------------------------------------------------------------------

func TestGSIIndexName(t *testing.T) {
	assert.Equal(t, "gsi1pk-gsi1sk-index", GSIIndexName(1))
	assert.Equal(t, "gsi10pk-gsi10sk-index", GSIIndexName(10))
	assert.Equal(t, "gsi20pk-gsi20sk-index", GSIIndexName(20))
}

// ---------------------------------------------------------------------------
// itemToOpaqueData
// ---------------------------------------------------------------------------

func TestItemToOpaqueData_FullItem(t *testing.T) {
	item := map[string]types.AttributeValue{
		"pk":      &types.AttributeValueMemberS{Value: "PK"},
		"sk":      &types.AttributeValueMemberS{Value: "SK"},
		"body":    &types.AttributeValueMemberB{Value: []byte("body")},
		"ttl":     &types.AttributeValueMemberN{Value: "999"},
		"version": &types.AttributeValueMemberN{Value: "0"},
		"gsi1pk":  &types.AttributeValueMemberS{Value: "G1PK"},
		"gsi20sk": &types.AttributeValueMemberS{Value: "G20SK"},
	}
	od, err := itemToOpaqueData(item)
	require.NoError(t, err)
	assert.Equal(t, "PK", od.GetPk())
	assert.Equal(t, "SK", od.GetSk())
	assert.Equal(t, []byte("body"), od.GetBody())
	assert.Equal(t, int64(999), od.GetTtl())
	assert.Equal(t, int64(0), od.GetVersion())
	assert.Equal(t, "G1PK", od.GetGsi1Pk())
	assert.Equal(t, "G20SK", od.GetGsi20Sk())
}
