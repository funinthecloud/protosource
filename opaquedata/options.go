package opaquedata

import "time"

type OpaqueDataOptions struct {
	ttl               time.Duration
	compressThreshold int
}

type Option func(*OpaqueDataOptions)

func WithTTL(ttl time.Duration) Option {
	return func(o *OpaqueDataOptions) { o.ttl = ttl }
}

func WithCompressThreshold(threshold int) Option {
	return func(o *OpaqueDataOptions) { o.compressThreshold = threshold }
}

func buildOptions(opts []Option) OpaqueDataOptions {
	o := OpaqueDataOptions{compressThreshold: defaultCompressThreshold}
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
