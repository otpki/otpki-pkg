package redis

import "time"

type Config struct {
	KeyPrefix    string        `mapstructure:"key_prefix"`
	ReplyPrefix  string        `mapstructure:"reply_prefix"`
	StreamMaxLen int64         `mapstructure:"stream_max_len"`
	BlockTimeout time.Duration `mapstructure:"block_timeout"`
	ConsumerName string        `mapstructure:"consumer_name"`
}

func DefaultConfig() Config {
	return Config{ReplyPrefix: "_pulse_reply_", StreamMaxLen: 100000, BlockTimeout: 2 * time.Second, ConsumerName: "pulse"}
}
func normalizeConfig(cfg Config) Config {
	d := DefaultConfig()
	if cfg.ReplyPrefix == "" {
		cfg.ReplyPrefix = d.ReplyPrefix
	}
	if cfg.StreamMaxLen == 0 {
		cfg.StreamMaxLen = d.StreamMaxLen
	}
	if cfg.BlockTimeout == 0 {
		cfg.BlockTimeout = d.BlockTimeout
	}
	if cfg.ConsumerName == "" {
		cfg.ConsumerName = d.ConsumerName
	}
	return cfg
}
