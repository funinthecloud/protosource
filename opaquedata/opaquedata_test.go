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

func (t *testItem) PK() string      { return t.pk }
func (t *testItem) SK() string      { return t.sk }
func (t *testItem) GSI1PK() string  { return t.gsi1pk }
func (t *testItem) GSI1SK() string  { return t.gsi1sk }
func (t *testItem) GSI2PK() string  { return "" }
func (t *testItem) GSI2SK() string  { return "" }
func (t *testItem) GSI3PK() string  { return "" }
func (t *testItem) GSI3SK() string  { return "" }
func (t *testItem) GSI4PK() string  { return "" }
func (t *testItem) GSI4SK() string  { return "" }
func (t *testItem) GSI5PK() string  { return "" }
func (t *testItem) GSI5SK() string  { return "" }
func (t *testItem) GSI6PK() string  { return "" }
func (t *testItem) GSI6SK() string  { return "" }
func (t *testItem) GSI7PK() string  { return "" }
func (t *testItem) GSI7SK() string  { return "" }
func (t *testItem) GSI8PK() string  { return "" }
func (t *testItem) GSI8SK() string  { return "" }
func (t *testItem) GSI9PK() string  { return "" }
func (t *testItem) GSI9SK() string  { return "" }
func (t *testItem) GSI10PK() string { return t.gsi10pk }
func (t *testItem) GSI10SK() string { return "" }
func (t *testItem) GSI11PK() string { return "" }
func (t *testItem) GSI11SK() string { return "" }
func (t *testItem) GSI12PK() string { return "" }
func (t *testItem) GSI12SK() string { return "" }
func (t *testItem) GSI13PK() string { return "" }
func (t *testItem) GSI13SK() string { return "" }
func (t *testItem) GSI14PK() string { return "" }
func (t *testItem) GSI14SK() string { return "" }
func (t *testItem) GSI15PK() string { return "" }
func (t *testItem) GSI15SK() string { return "" }
func (t *testItem) GSI16PK() string { return "" }
func (t *testItem) GSI16SK() string { return "" }
func (t *testItem) GSI17PK() string { return "" }
func (t *testItem) GSI17SK() string { return "" }
func (t *testItem) GSI18PK() string { return "" }
func (t *testItem) GSI18SK() string { return "" }
func (t *testItem) GSI19PK() string { return "" }
func (t *testItem) GSI19SK() string { return "" }
func (t *testItem) GSI20PK() string { return t.gsi20pk }
func (t *testItem) GSI20SK() string { return "" }

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
