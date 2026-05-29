package pulsar

import (
	"encoding"
	"strings"
	"time"

	"github.com/otpki/otpki-pkg/spfy/pulse"
)

var _ encoding.TextUnmarshaler = (*SubscriptionType)(nil)

type SubscriptionType string

const (
	SubscriptionTypeExclusive SubscriptionType = "exclusive"
	SubscriptionTypeFailover  SubscriptionType = "failover"
	SubscriptionTypeShared    SubscriptionType = "shared"
	SubscriptionTypeKeyShared SubscriptionType = "key_shared"
)

func (t SubscriptionType) String() string { return string(t) }
func (t *SubscriptionType) UnmarshalText(text []byte) error {
	switch strings.ToLower(strings.TrimSpace(string(text))) {
	case "":
		*t = ""
	case "exclusive":
		*t = SubscriptionTypeExclusive
	case "failover":
		*t = SubscriptionTypeFailover
	case "shared":
		*t = SubscriptionTypeShared
	case "key_shared", "keyshared":
		*t = SubscriptionTypeKeyShared
	default:
		return pulse.ErrInvalidName
	}
	return nil
}

type Config struct {
	Tenant      string `mapstructure:"tenant"`
	Namespace   string `mapstructure:"namespace"`
	TopicPrefix string `mapstructure:"topic_prefix"`

	ReplyTopicPrefix  string           `mapstructure:"reply_topic_prefix"`
	SubscriptionName  string           `mapstructure:"subscription_name"`
	SubscriptionType  SubscriptionType `mapstructure:"subscription_type"`
	ReceiverQueueSize int              `mapstructure:"receiver_queue_size"`

	DisableBatching         bool          `mapstructure:"disable_batching"`
	BatchingMaxPublishDelay time.Duration `mapstructure:"batching_max_publish_delay"`
	BatchingMaxMessages     uint          `mapstructure:"batching_max_messages"`
	BatchingMaxSize         uint          `mapstructure:"batching_max_size"`
}

func DefaultConfig() Config {
	return Config{Tenant: "public", Namespace: "default", ReplyTopicPrefix: "_pulse.reply.", ReceiverQueueSize: 1000, BatchingMaxMessages: 1000, BatchingMaxSize: 128 * 1024, BatchingMaxPublishDelay: 10 * time.Millisecond}
}

func normalizeConfig(cfg Config) Config {
	d := DefaultConfig()
	if cfg.Tenant == "" {
		cfg.Tenant = d.Tenant
	}
	if cfg.Namespace == "" {
		cfg.Namespace = d.Namespace
	}
	if cfg.ReplyTopicPrefix == "" {
		cfg.ReplyTopicPrefix = d.ReplyTopicPrefix
	}
	if cfg.ReceiverQueueSize == 0 {
		cfg.ReceiverQueueSize = d.ReceiverQueueSize
	}
	if !cfg.DisableBatching {
		if cfg.BatchingMaxPublishDelay == 0 {
			cfg.BatchingMaxPublishDelay = d.BatchingMaxPublishDelay
		}
		if cfg.BatchingMaxMessages == 0 {
			cfg.BatchingMaxMessages = d.BatchingMaxMessages
		}
		if cfg.BatchingMaxSize == 0 {
			cfg.BatchingMaxSize = d.BatchingMaxSize
		}
	}
	return cfg
}
