package ristretto

import "github.com/otpki/otpki-pkg/spfy/anchor/internal"

// Config is used by NewWithConfig when the store should
// own the underlying cache. Use New when you want to provide an
// already configured cache yourself.
type Config struct {
	NumCounters int64 `mapstructure:"num_counters"`
	MaxCost     int64 `mapstructure:"max_cost"`
	BufferItems int64 `mapstructure:"buffer_items"`
	WatchBuffer int   `mapstructure:"watch_buffer"`
}

func DefaultConfig() Config {
	return Config{
		NumCounters: 1_000_000,
		MaxCost:     64 << 20,
		BufferItems: 64,
		WatchBuffer: internal.DefaultWatchBuffer,
	}
}
