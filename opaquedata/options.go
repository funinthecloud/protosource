package opaquedata

import (
	"time"

	"github.com/funinthecloud/protosource"
)

type OpaqueDataOptions struct {
	ttl               time.Duration
	compressThreshold int
}

type Option func(*OpaqueDataOptions)

func WithTTL(ttl time.Duration) Option {
	return func(o *OpaqueDataOptions) { o.ttl = ttl }
}

// WithCompressThreshold overrides the compression threshold (in bytes) for
// proto body data. Data at or above this size is gzip-compressed before storage.
// Pass 0 or a negative value to disable compression. The default is 300 bytes.
func WithCompressThreshold(threshold int) Option {
	return func(o *OpaqueDataOptions) { o.compressThreshold = threshold }
}

func buildOptions(opts []Option) OpaqueDataOptions {
	o := OpaqueDataOptions{compressThreshold: protosource.DefaultCompressThreshold}
	for _, fn := range opts {
		fn(&o)
	}
	return o
}

func GetTTL(ttl time.Duration) int64 {
	if ttl <= 0 {
		return 0
	}
	return time.Now().Add(ttl).Unix()
}
