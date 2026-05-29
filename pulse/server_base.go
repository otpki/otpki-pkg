package pulse

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apache/pulsar-client-go/pulsar"
	"github.com/google/uuid"
	"github.com/samber/oops"
)

type serverBase struct {
	config    *RPCServerConfig
	isStarted atomic.Bool
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

func newServerBase(ctx context.Context, config *RPCServerConfig) (*serverBase, error) {
	if config.PulsarClient == nil {
		return nil, oops.In(ServiceName).New("missing pulsar connection")
	}
	if config.ResponseTopic == "" {
		return nil, oops.In(ServiceName).New("missing response topic")
	}

	// set defaults
	if config.ServerID == "" {
		config.ServerID = uuid.NewString()
	}
	if config.ProducerName == "" {
		config.ProducerName = "rpc-server-producer-" + config.ServerID
	}
	if config.ResponseTimeout == 0 {
		config.ResponseTimeout = 5 * time.Second
	}

	// for signaling to goroutines
	ctx, cancel := context.WithCancel(ctx)

	return &serverBase{
		config: config,
		ctx:    ctx,
		cancel: cancel,
	}, nil
}

func (b *serverBase) IsStarted() bool {
	return b.isStarted.Load()
}

func (b *serverBase) markStarted() {
	b.isStarted.Store(true)
}

func (b *serverBase) markStopped() {
	b.isStarted.Store(false)
}

func (b *serverBase) createConsumerWithPolicies(topic string) (pulsar.Consumer, error) {
	consOpts := pulsar.ConsumerOptions{
		Name:             "rpc-server-consumer-" + b.config.ServerID,
		Topic:            topic,
		SubscriptionName: "rpc-server-consumer-subscription", // when multiple servers use the same topic and same subscription name, this will create round-robin queuing
		Type:             pulsar.Shared,
	}

	if b.config != nil && b.config.TopicPolicies != nil {
		applyConsumerPolicies(&consOpts, b.config.TopicPolicies)
	}

	return b.config.PulsarClient.Subscribe(consOpts)
}

func (b *serverBase) shutdown() {
	b.markStopped()
	b.cancel()
	b.wg.Wait()
}
