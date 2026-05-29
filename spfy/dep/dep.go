package dep

import (
	"context"
	"log/slog"

	"github.com/apache/pulsar-client-go/pulsar"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/otpki/otpki-pkg/config"
	"github.com/otpki/otpki-pkg/log"
	"github.com/otpki/otpki-pkg/spfy/clientcfg"
	"github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
	"github.com/samber/do/v2"
)

var Package = do.Package(
	do.Lazy(newAWSConfig),
	do.Lazy(newSNSClient),
	do.Lazy(newSQSClient),
	do.Lazy(newKafkaWrapperClient),
	do.Lazy(newKafkaClient),
	do.Lazy(newNatsWrapperClient),
	do.Lazy(newNatsClient),
	do.Lazy(newPostgresWrapperClient),
	do.Lazy(newPostgresClient),
	do.Lazy(newPulsarWrapperClient),
	do.Lazy(newPulsarClient),
	do.Lazy(newRabbitWrapperClient),
	do.Lazy(newRabbitClient),
	do.Lazy(newRedisWrapperClient),
	do.Lazy(newRedisClient),
	do.Lazy(newPubSub),
	do.Lazy(newStateStore),
)

type kafkaClient struct{ *clientcfg.KafkaBackendClient }
type natsClient struct{ *nats.Conn }
type postgresClient struct{ *pgxpool.Pool }
type pulsarClient struct{ pulsar.Client }
type rabbitClient struct{ *amqp091.Connection }
type redisClient struct{ redis.UniversalClient }

func (c *kafkaClient) Shutdown() error  { return c.Close() }
func (c *natsClient) Shutdown()         { c.Close() }
func (c *postgresClient) Shutdown()     { c.Close() }
func (c *pulsarClient) Shutdown()       { c.Close() }
func (c *rabbitClient) Shutdown() error { return c.Close() }
func (c *redisClient) Shutdown() error  { return c.Close() }

func newAWSConfig(i do.Injector) (aws.Config, error) {
	ctx := do.MustInvoke[context.Context](i)
	conf := do.MustInvoke[config.Config](i)
	logger := do.MustInvoke[log.Logger](i)
	slogger := log.NewSlogAdapter(logger, "aws")
	return clientcfg.LoadAWSConfig(ctx, conf.Backends.AWS, slogger)
}

func newSNSClient(i do.Injector) (*sns.Client, error) {
	awsConf := do.MustInvoke[aws.Config](i)
	conf := do.MustInvoke[config.Config](i)
	logger := do.MustInvoke[log.Logger](i)
	slogger := log.NewSlogAdapter(logger, "sns")
	return clientcfg.NewSNSClient(awsConf, conf.Backends.AWS, slogger), nil
}

func newSQSClient(i do.Injector) (*sqs.Client, error) {
	awsConf := do.MustInvoke[aws.Config](i)
	conf := do.MustInvoke[config.Config](i)
	logger := do.MustInvoke[log.Logger](i)
	slogger := log.NewSlogAdapter(logger, "sqs")
	return clientcfg.NewSQSClient(awsConf, conf.Backends.AWS, slogger), nil
}

func newKafkaWrapperClient(i do.Injector) (kafkaClient, error) {
	conf := do.MustInvoke[config.Config](i)
	logger := do.MustInvoke[log.Logger](i)
	slogger := log.NewSlogAdapter(logger, "kafka")
	kc, err := clientcfg.NewKafkaClient(conf.Backends.Kafka, slogger)
	return kafkaClient{kc}, err
}

func newKafkaClient(i do.Injector) (*clientcfg.KafkaBackendClient, error) {
	return do.MustInvoke[kafkaClient](i).KafkaBackendClient, nil
}

func newNatsWrapperClient(i do.Injector) (natsClient, error) {
	conf := do.MustInvoke[config.Config](i)
	logger := do.MustInvoke[log.Logger](i)
	slogger := log.NewSlogAdapter(logger, "nats")
	nc, err := clientcfg.NewNATSClient(conf.Backends.NATS, slogger)
	return natsClient{nc}, err
}

func newNatsClient(i do.Injector) (*nats.Conn, error) {
	return do.MustInvoke[natsClient](i).Conn, nil
}

func newPostgresWrapperClient(i do.Injector) (postgresClient, error) {
	ctx := do.MustInvoke[context.Context](i)
	conf := do.MustInvoke[config.Config](i)
	logger := do.MustInvoke[log.Logger](i)
	slogger := log.NewSlogAdapter(logger, "postgres")
	pc, err := clientcfg.NewPostgresPool(ctx, conf.Backends.Postgres, slogger)
	return postgresClient{pc}, err
}

func newPostgresClient(i do.Injector) (*pgxpool.Pool, error) {
	return do.MustInvoke[postgresClient](i).Pool, nil
}

func newPulsarWrapperClient(i do.Injector) (pulsarClient, error) {
	conf := do.MustInvoke[config.Config](i)
	pc, err := clientcfg.NewPulsarClient(conf.Backends.Pulsar, slog.New(slog.DiscardHandler))
	return pulsarClient{pc}, err
}

func newPulsarClient(i do.Injector) (pulsar.Client, error) {
	return do.MustInvoke[pulsarClient](i).Client, nil
}

func newRabbitWrapperClient(i do.Injector) (rabbitClient, error) {
	conf := do.MustInvoke[config.Config](i)
	rc, err := clientcfg.NewRabbitMQConnection(conf.Backends.RabbitMQ)
	if err != nil {
		return rabbitClient{}, err
	}
	return rabbitClient{rc}, nil
}

func newRabbitClient(i do.Injector) (*amqp091.Connection, error) {
	return do.MustInvoke[rabbitClient](i).Connection, nil
}

func newRedisWrapperClient(i do.Injector) (redisClient, error) {
	conf := do.MustInvoke[config.Config](i)
	logger := do.MustInvoke[log.Logger](i)
	slogger := log.NewSlogAdapter(logger, "redis")
	rc, err := clientcfg.NewRedisClient(conf.Backends.Redis, slogger)
	return redisClient{rc}, err
}

func newRedisClient(i do.Injector) (redis.UniversalClient, error) {
	return do.MustInvoke[redisClient](i).UniversalClient, nil
}
