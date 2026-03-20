package protosource

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
)

const defaultCompressThreshold = 300

func isGzipped(data []byte) bool {
	return len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b
}

func maybeCompress(data []byte, threshold int) ([]byte, error) {
	if threshold <= 0 || len(data) < threshold {
		return data, nil
	}
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return nil, fmt.Errorf("protosource: gzip write: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("protosource: gzip close: %w", err)
	}
	return buf.Bytes(), nil
}

func maybeDecompress(data []byte) ([]byte, error) {
	if !isGzipped(data) {
		return data, nil
	}
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("protosource: gzip reader: %w", err)
	}
	defer r.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("protosource: gzip read: %w", err)
	}
	return out, nil
}
