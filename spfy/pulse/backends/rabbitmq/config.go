package rabbitmq

import "time"

type Config struct {
	ExchangePrefix    string        `mapstructure:"exchange_prefix"`
	QueuePrefix       string        `mapstructure:"queue_prefix"`
	ReplyQueuePrefix  string        `mapstructure:"reply_queue_prefix"`
	ConsumerTagPrefix string        `mapstructure:"consumer_tag_prefix"`
	Prefetch          int           `mapstructure:"prefetch"`
	CloseTimeout      time.Duration `mapstructure:"close_timeout"`
}

func DefaultConfig() Config {
	return Config{ReplyQueuePrefix: "pulse.reply.", ConsumerTagPrefix: "pulse.", Prefetch: 100, CloseTimeout: 5 * time.Second}
}
func normalizeConfig(cfg Config) Config {
	d := DefaultConfig()
	if cfg.ReplyQueuePrefix == "" {
		cfg.ReplyQueuePrefix = d.ReplyQueuePrefix
	}
	if cfg.ConsumerTagPrefix == "" {
		cfg.ConsumerTagPrefix = d.ConsumerTagPrefix
	}
	if cfg.Prefetch == 0 {
		cfg.Prefetch = d.Prefetch
	}
	if cfg.CloseTimeout == 0 {
		cfg.CloseTimeout = d.CloseTimeout
	}
	return cfg
}
