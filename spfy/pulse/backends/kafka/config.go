package kafka

import (
	"context"
	"time"

	segmentio "github.com/segmentio/kafka-go"
)

type ReaderFactory func(segmentio.ReaderConfig) *segmentio.Reader

// TopicEnsurer is an optional, caller-provided admin hook used by the backend
// to create broker topics before a reader or writer uses them. It keeps Kafka's
// connection/admin lifecycle caller-owned, while avoiding hidden topic-creation
// clients inside the backend.
type TopicEnsurer func(context.Context, string) error

type Config struct {
	TopicPrefix      string        `mapstructure:"topic_prefix"`
	GroupPrefix      string        `mapstructure:"group_prefix"`
	ReplyTopicPrefix string        `mapstructure:"reply_topic_prefix"`
	MinBytes         int           `mapstructure:"min_bytes"`
	MaxBytes         int           `mapstructure:"max_bytes"`
	CommitInterval   time.Duration `mapstructure:"commit_interval"`

	// EnsureTopic is intentionally not loaded from configuration. Pass it from
	// code when your Kafka cluster does not auto-create topics.
	EnsureTopic TopicEnsurer `mapstructure:"-"`
}

func DefaultConfig() Config {
	return Config{ReplyTopicPrefix: "_pulse_reply_", MinBytes: 1, MaxBytes: 10e6}
}

func normalizeConfig(cfg Config) Config {
	d := DefaultConfig()
	if cfg.ReplyTopicPrefix == "" {
		cfg.ReplyTopicPrefix = d.ReplyTopicPrefix
	}
	if cfg.MinBytes == 0 {
		cfg.MinBytes = d.MinBytes
	}
	if cfg.MaxBytes == 0 {
		cfg.MaxBytes = d.MaxBytes
	}
	return cfg
}
