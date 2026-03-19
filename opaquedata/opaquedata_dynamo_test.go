package opaquedata

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	opaquedatav1 "github.com/funinthecloud/protosource/opaquedata/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
