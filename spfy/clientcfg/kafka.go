package clientcfg

import (
	"context"
	"encoding"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	pulsekafka "github.com/otpki/otpki-pkg/spfy/pulse/backends/kafka"
	"github.com/samber/oops"
	segmentio "github.com/segmentio/kafka-go"
	kafkasasl "github.com/segmentio/kafka-go/sasl"
	"github.com/segmentio/kafka-go/sasl/plain"
)

var _ encoding.TextUnmarshaler = (*KafkaVersion)(nil)
var _ encoding.TextUnmarshaler = (*KafkaSASLMechanism)(nil)

type KafkaVersion string

func (v *KafkaVersion) UnmarshalText(text []byte) error {
	s := strings.TrimSpace(string(text))
	*v = KafkaVersion(s)
	return nil
}

type KafkaSASLMechanism string

const (
	KafkaSASLMechanismPlain KafkaSASLMechanism = "PLAIN"
)

func (m *KafkaSASLMechanism) UnmarshalText(text []byte) error {
	switch strings.ToUpper(strings.TrimSpace(string(text))) {
	case "", "PLAIN":
		*m = KafkaSASLMechanismPlain
		return nil
	default:
		return oops.Errorf("unsupported kafka sasl mechanism %q", string(text))
	}
}

type KafkaSASLConfig struct {
	Enable    bool               `mapstructure:"enable"`
	Mechanism KafkaSASLMechanism `mapstructure:"mechanism"`
	Version   int16              `mapstructure:"version"`
	Handshake bool               `mapstructure:"handshake"`
	User      string             `mapstructure:"user"`
	Password  string             `mapstructure:"password"`
	AuthzID   string             `mapstructure:"authz_id"`
}

type KafkaClientConfig struct {
	Brokers                   []string         `mapstructure:"brokers"`
	Version                   KafkaVersion     `mapstructure:"version"`
	ClientID                  string           `mapstructure:"client_id"`
	RackID                    string           `mapstructure:"rack_id"`
	ChannelBufferSize         int              `mapstructure:"channel_buffer_size"`
	MaxOpenRequests           int              `mapstructure:"max_open_requests"`
	DialTimeout               time.Duration    `mapstructure:"dial_timeout"`
	ReadTimeout               time.Duration    `mapstructure:"read_timeout"`
	WriteTimeout              time.Duration    `mapstructure:"write_timeout"`
	KeepAlive                 time.Duration    `mapstructure:"keep_alive"`
	TLS                       *TLSConfig       `mapstructure:"tls"`
	SASL                      *KafkaSASLConfig `mapstructure:"sasl"`
	MetadataRetryMax          int              `mapstructure:"metadata_retry_max"`
	MetadataRetryBackoff      time.Duration    `mapstructure:"metadata_retry_backoff"`
	MetadataRefreshFrequency  time.Duration    `mapstructure:"metadata_refresh_frequency"`
	MetadataFull              *bool            `mapstructure:"metadata_full"`
	MetadataTimeout           time.Duration    `mapstructure:"metadata_timeout"`
	AllowAutoTopicCreation    *bool            `mapstructure:"allow_auto_topic_creation"`
	ProducerRequiredAcks      int16            `mapstructure:"producer_required_acks"`
	ProducerTimeout           time.Duration    `mapstructure:"producer_timeout"`
	ProducerRetryMax          int              `mapstructure:"producer_retry_max"`
	ProducerRetryBackoff      time.Duration    `mapstructure:"producer_retry_backoff"`
	ProducerIdempotent        bool             `mapstructure:"producer_idempotent"`
	ConsumerSessionTimeout    time.Duration    `mapstructure:"consumer_session_timeout"`
	ConsumerHeartbeatInterval time.Duration    `mapstructure:"consumer_heartbeat_interval"`
	ConsumerRebalanceTimeout  time.Duration    `mapstructure:"consumer_rebalance_timeout"`
	ConsumerRetryBackoff      time.Duration    `mapstructure:"consumer_retry_backoff"`
	ConsumerOffsetsInitial    int64            `mapstructure:"consumer_offsets_initial"`
}

func DefaultKafkaClientConfig() KafkaClientConfig {
	return KafkaClientConfig{
		Brokers:                   []string{"localhost:9092"},
		Version:                   "2.0.0",
		ClientID:                  "spfy",
		ChannelBufferSize:         256,
		MetadataRefreshFrequency:  9 * time.Minute,
		ConsumerSessionTimeout:    10 * time.Second,
		ConsumerHeartbeatInterval: 3 * time.Second,
	}
}

// KafkaBackendClient bundles the kafka-go pieces needed by the pulse Kafka backend.
// The backend owns topic naming and lifecycle; this only wires transport and
// connection settings.
type KafkaBackendClient struct {
	Writer        *segmentio.Writer
	ReaderFactory pulsekafka.ReaderFactory
	transport     *segmentio.Transport
}

func (c *KafkaBackendClient) Close() error {
	if c == nil {
		return nil
	}
	var err error
	if c.Writer != nil {
		err = c.Writer.Close()
	}
	if c.transport != nil {
		c.transport.CloseIdleConnections()
	}
	return err
}

// NewKafkaClient builds the kafka-go transport, writer, and reader factory used
// by the pulse Kafka backend in this module.
func NewKafkaClient(cfg KafkaClientConfig, logger *slog.Logger) (*KafkaBackendClient, error) {
	defaults := DefaultKafkaClientConfig()
	if len(cfg.Brokers) == 0 {
		cfg.Brokers = defaults.Brokers
	}
	if cfg.ClientID == "" {
		cfg.ClientID = defaults.ClientID
	}

	dialer, err := newKafkaDialer(cfg)
	if err != nil {
		return nil, err
	}
	transport := &segmentio.Transport{
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, address)
		},
		DialTimeout: dialer.Timeout,
		IdleTimeout: 30 * time.Second,
		ClientID:    cfg.ClientID,
		TLS:         dialer.TLS,
		SASL:        dialer.SASLMechanism,
	}
	if cfg.MetadataRefreshFrequency > 0 {
		transport.MetadataTTL = cfg.MetadataRefreshFrequency
	}
	writer := &segmentio.Writer{
		Addr:                   segmentio.TCP(cfg.Brokers...),
		Balancer:               &segmentio.Hash{},
		MaxAttempts:            kafkaWriterMaxAttempts(cfg),
		BatchTimeout:           kafkaWriterBatchTimeout(cfg),
		ReadTimeout:            kafkaWriterReadTimeout(cfg),
		WriteTimeout:           kafkaWriterWriteTimeout(cfg),
		RequiredAcks:           kafkaWriterRequiredAcks(cfg),
		Async:                  false,
		Transport:              transport,
		AllowAutoTopicCreation: kafkaWriterAllowAutoTopicCreation(cfg),
	}
	if logger != nil {
		adapter := NewKafkaGoLogger(logger)
		writer.Logger = adapter
		writer.ErrorLogger = adapter
	}

	factory := func(rc segmentio.ReaderConfig) *segmentio.Reader {
		rc.Brokers = append([]string(nil), cfg.Brokers...)
		rc.Dialer = dialer
		if rc.ReadBackoffMin == 0 && cfg.ConsumerRetryBackoff > 0 {
			rc.ReadBackoffMin = cfg.ConsumerRetryBackoff
		}
		if rc.ReadBackoffMax == 0 && cfg.ConsumerRetryBackoff > 0 {
			rc.ReadBackoffMax = cfg.ConsumerRetryBackoff
		}
		if rc.HeartbeatInterval == 0 && cfg.ConsumerHeartbeatInterval > 0 {
			rc.HeartbeatInterval = cfg.ConsumerHeartbeatInterval
		}
		if rc.RebalanceTimeout == 0 && cfg.ConsumerRebalanceTimeout > 0 {
			rc.RebalanceTimeout = cfg.ConsumerRebalanceTimeout
		}
		if rc.SessionTimeout == 0 && cfg.ConsumerSessionTimeout > 0 {
			rc.SessionTimeout = cfg.ConsumerSessionTimeout
		}
		if rc.StartOffset == 0 && cfg.ConsumerOffsetsInitial != 0 {
			rc.StartOffset = cfg.ConsumerOffsetsInitial
		}
		if logger != nil {
			adapter := NewKafkaGoLogger(logger)
			rc.Logger = adapter
			rc.ErrorLogger = adapter
		}
		return segmentio.NewReader(rc)
	}

	return &KafkaBackendClient{
		Writer:        writer,
		ReaderFactory: factory,
		transport:     transport,
	}, nil
}

// NewKafkaBackendClient is kept as a compatibility wrapper for older callers.
func NewKafkaBackendClient(cfg KafkaClientConfig, logger *slog.Logger) (*KafkaBackendClient, error) {
	return NewKafkaClient(cfg, logger)
}

func newKafkaDialer(cfg KafkaClientConfig) (*segmentio.Dialer, error) {
	defaults := DefaultKafkaClientConfig()
	if len(cfg.Brokers) == 0 {
		cfg.Brokers = defaults.Brokers
	}
	if cfg.ClientID == "" {
		cfg.ClientID = defaults.ClientID
	}

	tlsCfg, err := BuildTLSConfig(cfg.TLS)
	if err != nil {
		return nil, err
	}

	dialer := &segmentio.Dialer{
		ClientID:  cfg.ClientID,
		Timeout:   cfg.DialTimeout,
		KeepAlive: cfg.KeepAlive,
		TLS:       tlsCfg,
	}
	if cfg.SASL != nil && cfg.SASL.Enable {
		mech, err := kafkaSASLMechanism(cfg.SASL)
		if err != nil {
			return nil, err
		}
		dialer.SASLMechanism = mech
	}
	return dialer, nil
}

func kafkaSASLMechanism(cfg *KafkaSASLConfig) (kafkasasl.Mechanism, error) {
	if cfg == nil || !cfg.Enable {
		return nil, nil
	}
	if cfg.AuthzID != "" {
		return nil, oops.Errorf("kafka-go backend client does not support sasl authz_id")
	}
	switch cfg.Mechanism {
	case "", KafkaSASLMechanismPlain:
		return plain.Mechanism{Username: cfg.User, Password: cfg.Password}, nil
	default:
		return nil, oops.Errorf("unsupported kafka sasl mechanism %q for kafka-go backend client", string(cfg.Mechanism))
	}
}

func kafkaWriterMaxAttempts(cfg KafkaClientConfig) int {
	if cfg.ProducerRetryMax > 0 {
		return cfg.ProducerRetryMax
	}
	return 10
}

func kafkaWriterBatchTimeout(cfg KafkaClientConfig) time.Duration {
	if cfg.ProducerRetryBackoff > 0 {
		return cfg.ProducerRetryBackoff
	}
	return time.Second
}

func kafkaWriterRequiredAcks(cfg KafkaClientConfig) segmentio.RequiredAcks {
	if cfg.ProducerRequiredAcks != 0 {
		return segmentio.RequiredAcks(cfg.ProducerRequiredAcks)
	}
	return segmentio.RequireOne
}

func kafkaWriterReadTimeout(cfg KafkaClientConfig) time.Duration {
	if cfg.ReadTimeout > 0 {
		return cfg.ReadTimeout
	}
	return 10 * time.Second
}

func kafkaWriterWriteTimeout(cfg KafkaClientConfig) time.Duration {
	if cfg.WriteTimeout > 0 {
		return cfg.WriteTimeout
	}
	if cfg.ProducerTimeout > 0 {
		return cfg.ProducerTimeout
	}
	return 10 * time.Second
}

func kafkaWriterAllowAutoTopicCreation(cfg KafkaClientConfig) bool {
	if cfg.AllowAutoTopicCreation != nil {
		return *cfg.AllowAutoTopicCreation
	}
	return true
}

type kafkaGoSlogLogger struct{ logger *slog.Logger }

func (l *kafkaGoSlogLogger) Printf(format string, v ...interface{}) {
	l.logger.Info(fmt.Sprintf(format, v...))
}

func NewKafkaGoLogger(logger *slog.Logger) segmentio.Logger {
	if logger == nil {
		return nil
	}
	return &kafkaGoSlogLogger{logger: logger}
}
