package protosource

import "github.com/funinthecloud/protosource/internal/compress"

const defaultCompressThreshold = compress.DefaultThreshold

func isGzipped(data []byte) bool                              { return compress.IsGzipped(data) }
func maybeCompress(data []byte, threshold int) ([]byte, error) { return compress.MaybeCompress(data, threshold) }
func maybeDecompress(data []byte) ([]byte, error)              { return compress.MaybeDecompress(data) }
