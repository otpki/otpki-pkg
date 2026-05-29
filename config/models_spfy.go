package config

import (
	anchormemory "github.com/otpki/otpki-pkg/spfy/anchor/backends/memory"
	anchornatscore "github.com/otpki/otpki-pkg/spfy/anchor/backends/natscore"
	anchornatsjetstream "github.com/otpki/otpki-pkg/spfy/anchor/backends/natsjetstream"
	anchorpostgres "github.com/otpki/otpki-pkg/spfy/anchor/backends/postgres"
	anchorredis "github.com/otpki/otpki-pkg/spfy/anchor/backends/redis"
	anchorristretto "github.com/otpki/otpki-pkg/spfy/anchor/backends/ristretto"
	"github.com/otpki/otpki-pkg/spfy/clientcfg"
	pulsekafka "github.com/otpki/otpki-pkg/spfy/pulse/backends/kafka"
	pulsememory "github.com/otpki/otpki-pkg/spfy/pulse/backends/memory"
	pulsenatscore "github.com/otpki/otpki-pkg/spfy/pulse/backends/natscore"
	pulsenatsjetstream "github.com/otpki/otpki-pkg/spfy/pulse/backends/natsjetstream"
	pulsepulsar "github.com/otpki/otpki-pkg/spfy/pulse/backends/pulsar"
	pulserabbitmq "github.com/otpki/otpki-pkg/spfy/pulse/backends/rabbitmq"
	pulseredis "github.com/otpki/otpki-pkg/spfy/pulse/backends/redis"
	pulsesnssqs "github.com/otpki/otpki-pkg/spfy/pulse/backends/snssqs"
	"github.com/samber/oops"
)

type StateStoreBackend string

const (
	StateStoreBackendMemory        StateStoreBackend = "memory"
	StateStoreBackendRistretto     StateStoreBackend = "ristretto"
	StateStoreBackendRedis         StateStoreBackend = "redis"
	StateStoreBackendPostgres      StateStoreBackend = "postgres"
	StateStoreBackendNATSCore      StateStoreBackend = "nats_core"
	StateStoreBackendNATSJetStream StateStoreBackend = "nats_jetstream"
)

func (b *StateStoreBackend) UnmarshalText(text []byte) error {
	value := StateStoreBackend(text)
	switch value {
	case StateStoreBackendMemory,
		StateStoreBackendRistretto,
		StateStoreBackendRedis,
		StateStoreBackendPostgres,
		StateStoreBackendNATSCore,
		StateStoreBackendNATSJetStream:
		*b = value
		return nil
	default:
		return oops.Errorf("invalid state store backend %q", string(text))
	}
}

type PubSubBackend string

const (
	PubSubBackendRedis         PubSubBackend = "redis"
	PubSubBackendNATSCore      PubSubBackend = "nats_core"
	PubSubBackendNATSJetStream PubSubBackend = "nats_jetstream"
	PubSubBackendRabbitMQ      PubSubBackend = "rabbitmq"
	PubSubBackendPulsar        PubSubBackend = "pulsar"
	PubSubBackendKafka         PubSubBackend = "kafka"
	PubSubBackendSNSSQS        PubSubBackend = "snssqs"
)

func (b *PubSubBackend) UnmarshalText(text []byte) error {
	value := PubSubBackend(text)
	switch value {
	case PubSubBackendRedis,
		PubSubBackendNATSCore,
		PubSubBackendNATSJetStream,
		PubSubBackendRabbitMQ,
		PubSubBackendPulsar,
		PubSubBackendKafka,
		PubSubBackendSNSSQS:
		*b = value
		return nil
	default:
		return oops.Errorf("invalid pubsub backend %q", string(text))
	}
}

type BackendsConfig struct {
	Postgres clientcfg.PostgresClientConfig `mapstructure:"postgres"`
	Redis    clientcfg.RedisClientConfig    `mapstructure:"redis"`
	NATS     clientcfg.NATSClientConfig     `mapstructure:"nats"`
	Pulsar   clientcfg.PulsarClientConfig   `mapstructure:"pulsar"`
	Kafka    clientcfg.KafkaClientConfig    `mapstructure:"kafka"`
	RabbitMQ clientcfg.RabbitMQClientConfig `mapstructure:"rabbitmq"`
	AWS      clientcfg.AWSClientConfig      `mapstructure:"aws"`
}

func DefaultBackendsConfig() BackendsConfig {
	return BackendsConfig{
		Postgres: clientcfg.DefaultPostgresClientConfig(),
		Redis:    clientcfg.DefaultRedisClientConfig(),
		NATS:     clientcfg.DefaultNATSClientConfig(),
		Pulsar:   clientcfg.DefaultPulsarClientConfig(),
		Kafka:    clientcfg.DefaultKafkaClientConfig(),
		RabbitMQ: clientcfg.DefaultRabbitMQClientConfig(),
		AWS:      clientcfg.DefaultAWSClientConfig(),
	}
}

type StateStoreConfig struct {
	Enabled       bool                       `mapstructure:"enabled"`
	Backend       StateStoreBackend          `mapstructure:"backend"`
	Memory        anchormemory.Config        `mapstructure:"memory"`
	Ristretto     anchorristretto.Config     `mapstructure:"ristretto"`
	Postgres      anchorpostgres.Config      `mapstructure:"postgres"`
	Redis         anchorredis.Config         `mapstructure:"redis"`
	NATSCore      anchornatscore.Config      `mapstructure:"nats_core"`
	NATSJetStream anchornatsjetstream.Config `mapstructure:"nats_jetstream"`
}

func DefaultStateStoreConfig() StateStoreConfig {
	return StateStoreConfig{
		Enabled:       true,
		Backend:       StateStoreBackendRedis,
		Memory:        anchormemory.DefaultConfig(),
		Ristretto:     anchorristretto.DefaultConfig(),
		Postgres:      anchorpostgres.DefaultConfig(),
		Redis:         anchorredis.DefaultConfig(),
		NATSCore:      anchornatscore.DefaultConfig(),
		NATSJetStream: anchornatsjetstream.DefaultConfig(),
	}
}

type PubSubConfig struct {
	Enabled bool          `mapstructure:"enabled"`
	Backend PubSubBackend `mapstructure:"backend"`

	Memory        pulsememory.Config        `mapstructure:"memory"`
	Redis         pulseredis.Config         `mapstructure:"redis"`
	NATSCore      pulsenatscore.Config      `mapstructure:"nats_core"`
	NATSJetStream pulsenatsjetstream.Config `mapstructure:"nats_jetstream"`
	Pulsar        pulsepulsar.Config        `mapstructure:"pulsar"`
	Kafka         pulsekafka.Config         `mapstructure:"kafka"`
	RabbitMQ      pulserabbitmq.Config      `mapstructure:"rabbitmq"`
	SNSSQS        pulsesnssqs.Config        `mapstructure:"snssqs"`
}

func DefaultPubSubConfig() PubSubConfig {
	return PubSubConfig{
		Enabled: true,
		Backend: PubSubBackendPulsar,

		Memory:        pulsememory.DefaultConfig(),
		Redis:         pulseredis.DefaultConfig(),
		NATSCore:      pulsenatscore.DefaultConfig(),
		NATSJetStream: pulsenatsjetstream.DefaultConfig(),
		Pulsar:        pulsepulsar.DefaultConfig(),
		Kafka:         pulsekafka.DefaultConfig(),
		RabbitMQ:      pulserabbitmq.DefaultConfig(),
		SNSSQS:        pulsesnssqs.DefaultConfig(),
	}
}
