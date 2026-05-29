package dep

import (
	"context"

	"github.com/apache/pulsar-client-go/pulsar"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/otpki/otpki-pkg/config"
	"github.com/otpki/otpki-pkg/spfy/anchor"
	anchormemory "github.com/otpki/otpki-pkg/spfy/anchor/backends/memory"
	anchornatscore "github.com/otpki/otpki-pkg/spfy/anchor/backends/natscore"
	anchornatsjetstream "github.com/otpki/otpki-pkg/spfy/anchor/backends/natsjetstream"
	anchorpostgres "github.com/otpki/otpki-pkg/spfy/anchor/backends/postgres"
	anchorredis "github.com/otpki/otpki-pkg/spfy/anchor/backends/redis"
	anchorristretto "github.com/otpki/otpki-pkg/spfy/anchor/backends/ristretto"
	"github.com/otpki/otpki-pkg/spfy/clientcfg"
	"github.com/otpki/otpki-pkg/spfy/pulse"
	pulsekafka "github.com/otpki/otpki-pkg/spfy/pulse/backends/kafka"
	pulsememory "github.com/otpki/otpki-pkg/spfy/pulse/backends/memory"
	pulsenatscore "github.com/otpki/otpki-pkg/spfy/pulse/backends/natscore"
	pulsenatsjetstream "github.com/otpki/otpki-pkg/spfy/pulse/backends/natsjetstream"
	pulsepulsar "github.com/otpki/otpki-pkg/spfy/pulse/backends/pulsar"
	pulserabbitmq "github.com/otpki/otpki-pkg/spfy/pulse/backends/rabbitmq"
	pulseredis "github.com/otpki/otpki-pkg/spfy/pulse/backends/redis"
	pulsesnssqs "github.com/otpki/otpki-pkg/spfy/pulse/backends/snssqs"
	"github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
	"github.com/samber/do/v2"
)

type passthroughCodec struct{}

func (passthroughCodec) Marshal(v []byte) ([]byte, error) {
	return v, nil
}

func (passthroughCodec) Unmarshal(b []byte) ([]byte, error) {
	return b, nil
}

type stateStoreFactory[T any] func(
	i do.Injector,
	ssCfg config.StateStoreConfig,
	codec anchor.Codec[T],
) (anchor.Store[T], error)

type pubSubFactory func(
	i do.Injector,
	psCfg config.PubSubConfig,
) (pulse.Backend, error)

func newStateStore(i do.Injector) (anchor.Store[[]byte], error) {
	return NewStateStore[[]byte](i, passthroughCodec{})
}

func newPubSub(i do.Injector) (pulse.Backend, error) {
	return NewPubSub(i)
}

func NewStateStore[T any](i do.Injector, codec anchor.Codec[T]) (anchor.Store[T], error) {
	conf := do.MustInvoke[config.Config](i)
	ssCfg := conf.StateStore

	if !ssCfg.Enabled {
		return anchormemory.New(ssCfg.Memory, codec), nil
	}

	factory, ok := stateStoreFactories[T]()[ssCfg.Backend]
	if !ok {
		factory = stateStoreFactories[T]()[config.StateStoreBackendRedis]
	}

	return factory(i, ssCfg, codec)
}

func NewPubSub(i do.Injector) (pulse.Backend, error) {
	conf := do.MustInvoke[config.Config](i)
	psCfg := conf.PubSub

	if !psCfg.Enabled {
		return pulsememory.New(psCfg.Memory), nil
	}

	factory, ok := pubSubFactories[psCfg.Backend]
	if !ok {
		factory = pubSubFactories[config.PubSubBackendPulsar]
	}

	backend, err := factory(i, psCfg)
	if err != nil {
		return nil, err
	}
	return pulse.NewPropagatingBackend(backend), nil
}

func stateStoreFactories[T any]() map[config.StateStoreBackend]stateStoreFactory[T] {
	return map[config.StateStoreBackend]stateStoreFactory[T]{
		config.StateStoreBackendNATSCore: func(
			i do.Injector,
			ssCfg config.StateStoreConfig,
			codec anchor.Codec[T],
		) (anchor.Store[T], error) {
			ctx := do.MustInvoke[context.Context](i)
			nc := do.MustInvoke[*nats.Conn](i)

			return anchornatscore.New(
				ctx,
				nc,
				ssCfg.NATSCore,
				codec,
			)
		},

		config.StateStoreBackendNATSJetStream: func(
			i do.Injector,
			ssCfg config.StateStoreConfig,
			codec anchor.Codec[T],
		) (anchor.Store[T], error) {
			ctx := do.MustInvoke[context.Context](i)
			nc := do.MustInvoke[*nats.Conn](i)

			return anchornatsjetstream.New(
				ctx,
				nc,
				ssCfg.NATSJetStream,
				codec,
			)
		},

		config.StateStoreBackendPostgres: func(
			i do.Injector,
			ssCfg config.StateStoreConfig,
			codec anchor.Codec[T],
		) (anchor.Store[T], error) {
			ctx := do.MustInvoke[context.Context](i)
			pool := do.MustInvoke[*pgxpool.Pool](i)

			return anchorpostgres.New(
				ctx,
				pool,
				ssCfg.Postgres,
				codec,
			)
		},

		config.StateStoreBackendRistretto: func(
			i do.Injector,
			ssCfg config.StateStoreConfig,
			codec anchor.Codec[T],
		) (anchor.Store[T], error) {
			return anchorristretto.New(
				ssCfg.Ristretto,
				codec,
			)
		},

		config.StateStoreBackendRedis: func(
			i do.Injector,
			ssCfg config.StateStoreConfig,
			codec anchor.Codec[T],
		) (anchor.Store[T], error) {
			return anchorredis.New(
				do.MustInvoke[redis.UniversalClient](i),
				ssCfg.Redis,
				codec,
			), nil
		},
	}
}

var pubSubFactories = map[config.PubSubBackend]pubSubFactory{
	config.PubSubBackendSNSSQS: func(
		i do.Injector,
		psCfg config.PubSubConfig,
	) (pulse.Backend, error) {
		return pulsesnssqs.New(
			do.MustInvoke[*sns.Client](i),
			do.MustInvoke[*sqs.Client](i),
			psCfg.SNSSQS,
		)
	},

	config.PubSubBackendKafka: func(
		i do.Injector,
		psCfg config.PubSubConfig,
	) (pulse.Backend, error) {
		client := do.MustInvoke[*clientcfg.KafkaBackendClient](i)
		return pulsekafka.New(
			client.Writer,
			client.ReaderFactory,
			psCfg.Kafka,
		)
	},

	config.PubSubBackendNATSCore: func(
		i do.Injector,
		psCfg config.PubSubConfig,
	) (pulse.Backend, error) {
		return pulsenatscore.New(
			do.MustInvoke[*nats.Conn](i),
			psCfg.NATSCore,
		)
	},

	config.PubSubBackendNATSJetStream: func(
		i do.Injector,
		psCfg config.PubSubConfig,
	) (pulse.Backend, error) {
		return pulsenatsjetstream.New(
			do.MustInvoke[*nats.Conn](i),
			psCfg.NATSJetStream,
		)
	},

	config.PubSubBackendRabbitMQ: func(
		i do.Injector,
		psCfg config.PubSubConfig,
	) (pulse.Backend, error) {
		ch, err := do.MustInvoke[*amqp091.Connection](i).Channel()
		if err != nil {
			return nil, err
		}
		return pulserabbitmq.New(
			ch,
			psCfg.RabbitMQ,
		)
	},

	config.PubSubBackendRedis: func(
		i do.Injector,
		psCfg config.PubSubConfig,
	) (pulse.Backend, error) {
		return pulseredis.New(
			do.MustInvoke[redis.UniversalClient](i),
			psCfg.Redis,
		)
	},

	config.PubSubBackendPulsar: func(
		i do.Injector,
		psCfg config.PubSubConfig,
	) (pulse.Backend, error) {
		return pulsepulsar.New(
			do.MustInvoke[pulsar.Client](i),
			psCfg.Pulsar,
		)
	},
}
