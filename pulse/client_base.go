package pulse

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/apache/pulsar-client-go/pulsar"
	"github.com/google/uuid"
	"github.com/samber/oops"
)

// clientBase contains common functionality shared by RPCClient and ServiceClient
type clientBase struct {
	config         *RPCClientConfig
	isStarted      atomic.Bool   // whether the client has been started
	pendingCounter atomic.Int64  // current number of pending requests
	semaphore      chan struct{} // semaphore to rate limit concurrent requests
	ctx            context.Context
	cancel         context.CancelFunc
}

func newClientBase(ctx context.Context, config *RPCClientConfig) (*clientBase, error) {
	if config.PulsarConfig == nil {
		return nil, oops.In(ServiceName).New("missing pulsar config")
	}
	if config.PulsarClient == nil {
		return nil, oops.In(ServiceName).New("missing pulsar connection")
	}
	if config.ResponseTopic == "" {
		return nil, oops.In(ServiceName).New("missing response topic")
	}

	// set defaults
	if config.Timeout == 0 {
		config.Timeout = 5 * time.Second
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.MaxPendingRequests == 0 {
		config.MaxPendingRequests = 10000
	}
	if config.ClientID == "" {
		config.ClientID = uuid.NewString()
	}
	if config.ProducerName == "" {
		config.ProducerName = "rpc-client-producer-" + config.ClientID
	}

	// create context for managing goroutines
	ctx, cancel := context.WithCancel(ctx)

	return &clientBase{
		config:    config,
		semaphore: make(chan struct{}, config.MaxPendingRequests), // rate limiting
		ctx:       ctx,
		cancel:    cancel,
	}, nil
}

func (b *clientBase) IsStarted() bool {
	return b.isStarted.Load()
}

func (b *clientBase) markStarted() {
	b.isStarted.Store(true)
}

func (b *clientBase) markStopped() {
	b.isStarted.Store(false)
}

func (b *clientBase) cancelContext() {
	if b.cancel != nil {
		b.cancel()
	}
}

func (b *clientBase) drainChannel(timeout time.Duration, drain func() bool) int {
	timeoutCh := time.After(timeout)
	processed := 0

	for {
		select {
		case <-timeoutCh: // timeout reached
			return processed // how many requests we drained
		default:
			if drain() { // attempt to drain
				processed++
			} else { // nothing left to drain
				return processed
			}
		}
	}
}

func (b *clientBase) createConsumerOptions(responseTopic, clientID string) (pulsar.ConsumerOptions, error) {
	baseTopicName := responseTopic + "-" + clientID
	subscriptionName := "rpc-client-consumer-subscription" + clientID
	subscriptionType := pulsar.Exclusive

	// can be ephemeral since we don't care if the client goes away
	actualTopic := BuildTopic(baseTopicName, b.config.PulsarConfig, true)

	consOpts := pulsar.ConsumerOptions{
		Name:             "rpc-client-consumer-" + clientID,
		Topic:            actualTopic,
		SubscriptionName: subscriptionName,
		Type:             subscriptionType,
	}

	// apply topic policies
	if b.config.TopicPolicies != nil {
		applyConsumerPolicies(&consOpts, b.config.TopicPolicies)
	}

	return consOpts, nil
}

// applyTimeoutToContext wraps the context with config timeout if it has no deadline,
// returns the context and a cancel function (maybe nil if no timeout was applied)
// caller is responsible for calling cancel when done
func (b *clientBase) applyTimeoutToContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, hasDeadline := ctx.Deadline(); !hasDeadline && b.config.Timeout > 0 {
		return context.WithTimeout(ctx, b.config.Timeout)
	}
	return ctx, nil
}
