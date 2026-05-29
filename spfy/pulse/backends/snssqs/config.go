package snssqs

import "time"

type Config struct {
	NamePrefix        string        `mapstructure:"name_prefix"`
	ReplyPrefix       string        `mapstructure:"reply_prefix"`
	MaxMessages       int32         `mapstructure:"max_messages"`
	WaitTimeSeconds   int32         `mapstructure:"wait_time_seconds"`
	VisibilityTimeout int32         `mapstructure:"visibility_timeout"`
	ReceiveBackoff    time.Duration `mapstructure:"receive_backoff"`
}

func DefaultConfig() Config {
	return Config{ReplyPrefix: "pulse-reply-", MaxMessages: 10, WaitTimeSeconds: 10, VisibilityTimeout: 30, ReceiveBackoff: 200 * time.Millisecond}
}
func normalizeConfig(cfg Config) Config {
	d := DefaultConfig()
	if cfg.ReplyPrefix == "" {
		cfg.ReplyPrefix = d.ReplyPrefix
	}
	if cfg.MaxMessages == 0 {
		cfg.MaxMessages = d.MaxMessages
	}
	if cfg.WaitTimeSeconds == 0 {
		cfg.WaitTimeSeconds = d.WaitTimeSeconds
	}
	if cfg.VisibilityTimeout == 0 {
		cfg.VisibilityTimeout = d.VisibilityTimeout
	}
	if cfg.ReceiveBackoff == 0 {
		cfg.ReceiveBackoff = d.ReceiveBackoff
	}
	return cfg
}
