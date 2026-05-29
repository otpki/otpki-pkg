package natscore

import "time"

type Config struct {
	SubjectPrefix string        `mapstructure:"subject_prefix"`
	QueuePrefix   string        `mapstructure:"queue_prefix"`
	FlushTimeout  time.Duration `mapstructure:"flush_timeout"`
	DrainTimeout  time.Duration `mapstructure:"drain_timeout"`
}

func DefaultConfig() Config {
	return Config{FlushTimeout: 2 * time.Second, DrainTimeout: 5 * time.Second}
}

func normalizeConfig(cfg Config) Config {
	defaults := DefaultConfig()
	if cfg.FlushTimeout == 0 {
		cfg.FlushTimeout = defaults.FlushTimeout
	}
	if cfg.DrainTimeout == 0 {
		cfg.DrainTimeout = defaults.DrainTimeout
	}
	return cfg
}
