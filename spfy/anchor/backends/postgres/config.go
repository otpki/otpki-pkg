package postgres

import "github.com/otpki/otpki-pkg/spfy/anchor/internal"

type Config struct {
	Table       string `mapstructure:"table"`
	WatchBuffer int    `mapstructure:"watch_buffer"`
}

func DefaultConfig() Config {
	return Config{
		Table:       "statestore_entries",
		WatchBuffer: internal.DefaultWatchBuffer,
	}
}
