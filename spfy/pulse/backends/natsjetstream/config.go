package natsjetstream

import "time"

type Config struct {
	SubjectPrefix string        `mapstructure:"subject_prefix"`
	QueuePrefix   string        `mapstructure:"queue_prefix"`
	StreamName    string        `mapstructure:"stream_name"`
	AckWait       time.Duration `mapstructure:"ack_wait"`
	FlushTimeout  time.Duration `mapstructure:"flush_timeout"`
	DrainTimeout  time.Duration `mapstructure:"drain_timeout"`
}

func DefaultConfig() Config {
	return Config{StreamName: "PULSE", AckWait: 30 * time.Second, FlushTimeout: 2 * time.Second, DrainTimeout: 5 * time.Second}
}

func normalizeConfig(cfg Config) Config {
	d := DefaultConfig()
	if cfg.StreamName == "" {
		cfg.StreamName = d.StreamName
	}
	if cfg.AckWait == 0 {
		cfg.AckWait = d.AckWait
	}
	if cfg.FlushTimeout == 0 {
		cfg.FlushTimeout = d.FlushTimeout
	}
	if cfg.DrainTimeout == 0 {
		cfg.DrainTimeout = d.DrainTimeout
	}
	return cfg
}
