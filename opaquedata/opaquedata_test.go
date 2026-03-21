package opaquedata

import (
	"bytes"
	"testing"
	"time"

	"github.com/funinthecloud/protosource"
	opaquedatav1 "github.com/funinthecloud/protosource/opaquedata/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// ---------------------------------------------------------------------------
// Test helper: wraps *opaquedatav1.OpaqueData to implement AutoPKSK + Hydrater
// ---------------------------------------------------------------------------

type testItem struct {
	*opaquedatav1.OpaqueData
	pk, sk         string
	gsi1pk, gsi1sk string
	gsi10pk        string
	gsi20pk        string
}

func (t *testItem) DynamoPK() string      { return t.pk }
func (t *testItem) DynamoSK() string      { return t.sk }
func (t *testItem) DynamoGSI1PK() string  { return t.gsi1pk }
func (t *testItem) DynamoGSI1SK() string  { return t.gsi1sk }
func (t *testItem) DynamoGSI2PK() string  { return "" }
func (t *testItem) DynamoGSI2SK() string  { return "" }
func (t *testItem) DynamoGSI3PK() string  { return "" }
func (t *testItem) DynamoGSI3SK() string  { return "" }
func (t *testItem) DynamoGSI4PK() string  { return "" }
func (t *testItem) DynamoGSI4SK() string  { return "" }
func (t *testItem) DynamoGSI5PK() string  { return "" }
func (t *testItem) DynamoGSI5SK() string  { return "" }
func (t *testItem) DynamoGSI6PK() string  { return "" }
func (t *testItem) DynamoGSI6SK() string  { return "" }
func (t *testItem) DynamoGSI7PK() string  { return "" }
func (t *testItem) DynamoGSI7SK() string  { return "" }
func (t *testItem) DynamoGSI8PK() string  { return "" }
func (t *testItem) DynamoGSI8SK() string  { return "" }
func (t *testItem) DynamoGSI9PK() string  { return "" }
func (t *testItem) DynamoGSI9SK() string  { return "" }
func (t *testItem) DynamoGSI10PK() string { return t.gsi10pk }
func (t *testItem) DynamoGSI10SK() string { return "" }
func (t *testItem) DynamoGSI11PK() string { return "" }
func (t *testItem) DynamoGSI11SK() string { return "" }
func (t *testItem) DynamoGSI12PK() string { return "" }
func (t *testItem) DynamoGSI12SK() string { return "" }
func (t *testItem) DynamoGSI13PK() string { return "" }
func (t *testItem) DynamoGSI13SK() string { return "" }
func (t *testItem) DynamoGSI14PK() string { return "" }
func (t *testItem) DynamoGSI14SK() string { return "" }
func (t *testItem) DynamoGSI15PK() string { return "" }
func (t *testItem) DynamoGSI15SK() string { return "" }
func (t *testItem) DynamoGSI16PK() string { return "" }
func (t *testItem) DynamoGSI16SK() string { return "" }
func (t *testItem) DynamoGSI17PK() string { return "" }
func (t *testItem) DynamoGSI17SK() string { return "" }
func (t *testItem) DynamoGSI18PK() string { return "" }
func (t *testItem) DynamoGSI18SK() string { return "" }
func (t *testItem) DynamoGSI19PK() string { return "" }
func (t *testItem) DynamoGSI19SK() string { return "" }
func (t *testItem) DynamoGSI20PK() string { return t.gsi20pk }
func (t *testItem) DynamoGSI20SK() string { return "" }

func (t *testItem) Hydrate(body []byte) error {
	return proto.Unmarshal(body, t.OpaqueData)
}

// ---------------------------------------------------------------------------
// NewOpaqueDataFromProto / ReHydrate round-trip
// ---------------------------------------------------------------------------

func TestRoundTrip_SmallBody(t *testing.T) {
	inner := &opaquedatav1.OpaqueData{Pk: "inner-pk", Sk: "inner-sk"}
	msg := &testItem{OpaqueData: inner, pk: "USER#1", sk: "PROFILE#1"}

	od, err := NewOpaqueDataFromProto(msg)
	require.NoError(t, err)
	assert.Equal(t, "USER#1", od.GetPk())
	assert.Equal(t, "PROFILE#1", od.GetSk())
	assert.NotEmpty(t, od.GetBody())
	// Small body should not be gzipped (below default 300 threshold).
	assert.False(t, protosource.IsGzipped(od.GetBody()))

	target := &testItem{OpaqueData: &opaquedatav1.OpaqueData{}}
	require.NoError(t, ReHydrate(od, target))
	assert.Equal(t, "inner-pk", target.OpaqueData.GetPk())
	assert.Equal(t, "inner-sk", target.OpaqueData.GetSk())
}

func TestRoundTrip_LargeBodyCompressed(t *testing.T) {
	largeBody := bytes.Repeat([]byte("data"), 200) // 800 bytes
	inner := &opaquedatav1.OpaqueData{Pk: "big", Body: largeBody}
	msg := &testItem{OpaqueData: inner, pk: "PK", sk: "SK"}

	od, err := NewOpaqueDataFromProto(msg)
	require.NoError(t, err)
	assert.True(t, protosource.IsGzipped(od.GetBody()), "large body should be gzipped")

	target := &testItem{OpaqueData: &opaquedatav1.OpaqueData{}}
	require.NoError(t, ReHydrate(od, target))
	assert.Equal(t, "big", target.OpaqueData.GetPk())
	assert.Equal(t, largeBody, target.OpaqueData.GetBody())
}

func TestRoundTrip_WithCompressThresholdOverride(t *testing.T) {
	inner := &opaquedatav1.OpaqueData{Pk: "test"}
	msg := &testItem{OpaqueData: inner, pk: "PK", sk: "SK"}

	// Threshold 1 = always compress (0 disables)
	od, err := NewOpaqueDataFromProto(msg, WithCompressThreshold(1))
	require.NoError(t, err)
	assert.True(t, protosource.IsGzipped(od.GetBody()))

	target := &testItem{OpaqueData: &opaquedatav1.OpaqueData{}}
	require.NoError(t, ReHydrate(od, target))
	assert.Equal(t, "test", target.OpaqueData.GetPk())
}

func TestNewOpaqueKeyFromProto_KeysOnly(t *testing.T) {
	msg := &testItem{
		OpaqueData: &opaquedatav1.OpaqueData{Pk: "should-not-appear"},
		pk:         "USER#1",
		sk:         "PROFILE#1",
	}

	od := NewOpaqueKeyFromProto(msg)
	assert.Equal(t, "USER#1", od.GetPk())
	assert.Equal(t, "PROFILE#1", od.GetSk())
	assert.Nil(t, od.GetBody())
}

func TestGSIKeysPopulated(t *testing.T) {
	msg := &testItem{
		OpaqueData: &opaquedatav1.OpaqueData{},
		pk:         "PK",
		sk:         "SK",
		gsi1pk:     "GSI1-PK",
		gsi1sk:     "GSI1-SK",
		gsi10pk:    "GSI10-PK",
		gsi20pk:    "GSI20-PK",
	}

	od, err := NewOpaqueDataFromProto(msg)
	require.NoError(t, err)
	assert.Equal(t, "GSI1-PK", od.GetGsi1Pk())
	assert.Equal(t, "GSI1-SK", od.GetGsi1Sk())
	assert.Equal(t, "GSI10-PK", od.GetGsi10Pk())
	assert.Equal(t, "GSI20-PK", od.GetGsi20Pk())
	// Unused GSIs should be empty
	assert.Empty(t, od.GetGsi2Pk())
	assert.Empty(t, od.GetGsi5Sk())
}

func TestTTL_SetsCorrectEpoch(t *testing.T) {
	msg := &testItem{OpaqueData: &opaquedatav1.OpaqueData{}, pk: "PK", sk: "SK"}

	before := time.Now().Add(1 * time.Hour).Unix()
	od, err := NewOpaqueDataFromProto(msg, WithTTL(1*time.Hour))
	require.NoError(t, err)
	after := time.Now().Add(1 * time.Hour).Unix()

	assert.GreaterOrEqual(t, od.GetTtl(), before)
	assert.LessOrEqual(t, od.GetTtl(), after)
}

func TestTTL_NoTTL(t *testing.T) {
	msg := &testItem{OpaqueData: &opaquedatav1.OpaqueData{}, pk: "PK", sk: "SK"}

	od, err := NewOpaqueDataFromProto(msg)
	require.NoError(t, err)
	assert.Equal(t, int64(0), od.GetTtl())
}
