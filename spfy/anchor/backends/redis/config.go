package redis

import "github.com/otpki/otpki-pkg/spfy/anchor/internal"

type Config struct {
	Prefix        string `mapstructure:"prefix"`
	MaxTxnRetries int    `mapstructure:"max_txn_retries"`
	WatchBuffer   int    `mapstructure:"watch_buffer"`
}

func DefaultConfig() Config {
	return Config{
		Prefix:        "statestore",
		MaxTxnRetries: 32,
		WatchBuffer:   internal.DefaultWatchBuffer,
	}
}
