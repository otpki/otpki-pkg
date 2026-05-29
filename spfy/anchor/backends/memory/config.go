package memory

import "github.com/otpki/otpki-pkg/spfy/anchor/internal"

// Config contains the small set of tunables for the in-memory store.
type Config struct {
	WatchBuffer int `mapstructure:"watch_buffer"`
}

func DefaultConfig() Config {
	return Config{WatchBuffer: internal.DefaultWatchBuffer}
}