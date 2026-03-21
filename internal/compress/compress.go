// Package compress provides gzip compression helpers shared by protosource
// and opaquedata. Data at or above a byte threshold is gzip-compressed;
// decompression is automatic via magic-byte detection.
package compress

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
)

// DefaultThreshold is the default compression threshold in bytes.
const DefaultThreshold = 300

// IsGzipped reports whether data starts with the gzip magic bytes.
func IsGzipped(data []byte) bool {
	return len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b
}

// MaybeCompress gzip-compresses data if its length meets or exceeds the
// threshold. A threshold <= 0 disables compression (returns data as-is).
func MaybeCompress(data []byte, threshold int) ([]byte, error) {
	if threshold <= 0 || len(data) < threshold {
		return data, nil
	}
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return nil, fmt.Errorf("compress: gzip write: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("compress: gzip close: %w", err)
	}
	return buf.Bytes(), nil
}

// MaybeDecompress decompresses gzip data (detected via magic bytes).
// Non-gzip data is returned unchanged.
func MaybeDecompress(data []byte) ([]byte, error) {
	if !IsGzipped(data) {
		return data, nil
	}
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("compress: gzip reader: %w", err)
	}
	defer r.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("compress: gzip read: %w", err)
	}
	return out, nil
}
