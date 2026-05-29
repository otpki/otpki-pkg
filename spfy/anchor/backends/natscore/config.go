package natscore

import (
	"time"

	"github.com/otpki/otpki-pkg/spfy/anchor/internal"
)

type Config struct {
	Subject          string        `mapstructure:"subject"`
	Origin           string        `mapstructure:"origin"`
	BootstrapTimeout time.Duration `mapstructure:"bootstrap_timeout"`
	OperationTimeout time.Duration `mapstructure:"operation_timeout"`
	WatchBuffer      int           `mapstructure:"watch_buffer"`
}

func DefaultConfig() Config {
	return Config{
		Subject:          "statestore",
		BootstrapTimeout: 500 * time.Millisecond,
		OperationTimeout: internal.DefaultNATSOperationTimeout,
		WatchBuffer:      internal.DefaultWatchBuffer,
	}
}
