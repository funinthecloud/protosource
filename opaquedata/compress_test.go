package opaquedata

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMaybeCompress_RoundTrip(t *testing.T) {
	original := bytes.Repeat([]byte("hello world "), 100)
	compressed, err := maybeCompress(original, 1)
	require.NoError(t, err)
	assert.True(t, isGzipped(compressed))

	decompressed, err := maybeDecompress(compressed)
	require.NoError(t, err)
	assert.Equal(t, original, decompressed)
}

func TestMaybeCompress_BelowThreshold(t *testing.T) {
	data := []byte("small")
	result, err := maybeCompress(data, 300)
	require.NoError(t, err)
	assert.Equal(t, data, result)
	assert.False(t, isGzipped(result))
}

func TestMaybeCompress_AboveThreshold(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 500)
	result, err := maybeCompress(data, 300)
	require.NoError(t, err)
	assert.True(t, isGzipped(result))
}

func TestMaybeDecompress_UncompressedPassthrough(t *testing.T) {
	data := []byte("not gzipped")
	result, err := maybeDecompress(data)
	require.NoError(t, err)
	assert.Equal(t, data, result)
}

func TestMaybeDecompress_EmptyData(t *testing.T) {
	result, err := maybeDecompress(nil)
	require.NoError(t, err)
	assert.Nil(t, result)

	result, err = maybeDecompress([]byte{})
	require.NoError(t, err)
	assert.Equal(t, []byte{}, result)
}

func TestIsGzipped_MagicBytes(t *testing.T) {
	assert.True(t, isGzipped([]byte{0x1f, 0x8b, 0x08}))
	assert.False(t, isGzipped([]byte{0x1f}))
	assert.False(t, isGzipped([]byte{0x00, 0x00}))
	assert.False(t, isGzipped(nil))
}

func TestMaybeCompress_NegativeThresholdNeverCompresses(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 1000)
	result, err := maybeCompress(data, -1)
	require.NoError(t, err)
	assert.Equal(t, data, result)
	assert.False(t, isGzipped(result))
}

func TestMaybeCompress_ZeroThresholdDisablesCompression(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 1000)
	result, err := maybeCompress(data, 0)
	require.NoError(t, err)
	assert.Equal(t, data, result)
	assert.False(t, isGzipped(result))
}
