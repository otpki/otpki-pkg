package pulse

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/apache/pulsar-client-go/pulsar"
	"github.com/google/uuid"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/otpki/otpki-pkg/log"
	"github.com/samber/oops"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/proto"
)

type ServiceClient struct {
	*clientBase
	logger        log.Logger
	producerCache *lru.Cache[string, pulsar.Producer]
	producerMu    sync.Mutex
	consumer      pulsar.Consumer
	responseMap   sync.Map // map[string]*Future[*Envelope]
	propagator    propagation.TextMapPropagator
	tracer        trace.Tracer
	wg            sync.WaitGroup
	closeOnce     sync.Once           // ensures Close() is only called once
	propagators   []ContextPropagator // context propagators for this instance
}

func NewServiceClient(ctx context.Context, config *RPCClientConfig, logger log.Logger) (*ServiceClient, error) {
	base, err := newClientBase(ctx, config)
	if err != nil {
		return nil, err
	}

	// create a consumer
	consOpts, err := base.createConsumerOptions(config.ResponseTopic, config.ClientID)
	if err != nil {
		base.cancelContext()
		return nil, err
	}

	consumer, err := base.config.PulsarClient.Subscribe(consOpts)
	if err != nil {
		base.cancelContext()
		return nil, err
	}

	cacheSize := 100
	if config.MaxProducerCacheSize > 0 {
		cacheSize = config.MaxProducerCacheSize
	}

	cache, err := lru.NewWithEvict(cacheSize, func(topic string, producer pulsar.Producer) {
		if producer != nil {
			producer.Close()
		}
	})
	if err != nil {
		base.cancelContext()
		return nil, err
	}

	propagator := otel.GetTextMapPropagator()
	tracer := otel.GetTracerProvider().Tracer("rpc-client")

	// Determine which propagators to use
	propagators := config.ContextPropagators
	if len(propagators) == 0 {
		propagators = globalRegistry.getPropagators()
	}

	return &ServiceClient{
		clientBase:    base,
		logger:        logger,
		producerCache: cache, // cached producer
		consumer:      consumer,
		propagator:    propagator,
		tracer:        tracer,
		propagators:   propagators,
	}, nil
}

func (c *ServiceClient) Start() {
	if c.IsStarted() {
		c.logger.Warn().Msgf("Pulsar Requester on channel: %s already running", c.config.RequestTopic)
		return
	}
	c.markStarted()
	c.wg.Go(c.processResponses)
}

func (c *ServiceClient) Shutdown() {
	c.closeOnce.Do(func() {
		c.markStopped()

		drained := c.drainChannel(c.config.Timeout, func() bool {
			select {
			case msg, ok := <-c.consumer.Chan():
				if !ok {
					// channel closed
					return false
				}

				// process the message
				corrID := msg.Properties()[CorrelationIDKey]
				clientID := msg.Properties()[ClientIDKey]

				if corrID == "" || clientID != c.config.ClientID {
					c.consumer.Nack(msg)
					return true // Continue draining
				}

				// if we can process it
				var envelope Envelope
				if err := json.Unmarshal(msg.Payload(), &envelope); err != nil {
					c.consumer.Nack(msg)
					return true // continue draining
				}

				// try to deliver to waiting goroutine
				if val, exists := c.responseMap.Load(corrID); exists {
					respCh := val.(chan *Envelope)
					select {
					case respCh <- &envelope:
						_ = c.consumer.Ack(msg)
					default:
						// Channel full or closed
						c.consumer.Nack(msg)
					}

					return true // message processed (discarded)
				} else {
					c.consumer.Nack(msg)
					return true // there may be more messages, but we just got a bad one
				}
			default:
				// no more messages in the buffer
				return false
			}
		})
		c.logger.Trace().Msgf("Pulsar Requester on channel: %s drained: %v", c.config.RequestTopic, drained)
		c.cancelContext()

		// wait for goroutines to finish
		c.wg.Wait()

		// close all resources
		c.producerCache.Purge()
		c.consumer.Close()
	})
}

func (c *ServiceClient) Call(ctx context.Context, requestTopic string, reqMsg proto.Message, respMsg proto.Message, reqMetadata map[string][]string) (map[string][]string, error) {
	// apply config timeout if context has no deadline
	ctx, cancel := c.applyTimeoutToContext(ctx)
	if cancel != nil {
		defer cancel()
	}
	sCtx, span := c.tracer.Start(ctx, requestTopic)
	defer span.End()

	// use CallAsync and wait to resolve
	future := c.CallAsync(sCtx, requestTopic, reqMsg, reqMetadata)
	envelope, err := future.Resolve()
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	if envelope.Error != nil {
		return nil, connect.NewError(
			parseConnectCode(envelope.Error.Code),
			oops.Errorf("%s", envelope.Error.Message),
		)
	}

	// unmarshal into the provided message
	if err := proto.Unmarshal(envelope.Payload, respMsg); err != nil {
		return nil, oops.In(ServiceName).Wrapf(err, "unmarshalling response")
	}

	return envelope.Metadata, nil
}

func (c *ServiceClient) CallAsync(ctx context.Context, requestTopic string, reqMsg proto.Message, reqMetadata map[string][]string) *Future[*Envelope] {
	// apply config timeout if context has no deadline
	ctx, cancel := c.applyTimeoutToContext(ctx)

	// acquire semaphore (for rate limiting)
	select {
	case c.semaphore <- struct{}{}:
		c.pendingCounter.Add(1)
	case <-ctx.Done():
		if cancel != nil {
			cancel()
		}
		// create an error channel and return if we cannot get the semaphore
		errCh := make(chan error, 1)
		errCh <- oops.In(ServiceName).Wrapf(ctx.Err(), "acquiring pending request slot")
		close(errCh)
		return &Future[*Envelope]{
			ResultCh: make(chan *Envelope),
			ErrCh:    errCh,
			Ctx:      ctx,
			Done:     make(chan struct{}),
		}
	}

	// get or create producer
	producer, err := c.getOrCreateProducer(requestTopic)
	if err != nil {
		if cancel != nil {
			cancel()
		}
		// release the semaphore and error out (cannot use defer since goroutine might release sema)
		<-c.semaphore
		c.pendingCounter.Add(-1)
		errCh := make(chan error, 1)
		errCh <- err
		close(errCh)
		return &Future[*Envelope]{
			ResultCh: make(chan *Envelope),
			ErrCh:    errCh,
			Ctx:      ctx,
			Done:     make(chan struct{}),
		}
	}

	// marshal request
	reqBytes, err := proto.Marshal(reqMsg)
	if err != nil {
		if cancel != nil {
			cancel()
		}
		// release the semaphore and error out
		<-c.semaphore
		c.pendingCounter.Add(-1)
		errCh := make(chan error, 1)
		errCh <- oops.In(ServiceName).Wrapf(err, "marshalling request")
		close(errCh)
		return &Future[*Envelope]{
			ResultCh: make(chan *Envelope),
			ErrCh:    errCh,
			Ctx:      ctx,
			Done:     make(chan struct{}),
		}
	}

	// create json response
	envelope := Envelope{
		Payload:  reqBytes,
		Metadata: reqMetadata,
	}

	envelopeBytes, err := json.Marshal(envelope)
	if err != nil {
		if cancel != nil {
			cancel()
		}
		// release the semaphore and error out
		<-c.semaphore
		c.pendingCounter.Add(-1)
		errCh := make(chan error, 1)
		errCh <- oops.In(ServiceName).Wrapf(err, "marshalling request envelope")
		close(errCh)
		return &Future[*Envelope]{
			ResultCh: make(chan *Envelope),
			ErrCh:    errCh,
			Ctx:      ctx,
			Done:     make(chan struct{}),
		}
	}

	// generate correlation ID
	corrID := uuid.NewString()

	// prepare message properties
	props := map[string]string{
		CorrelationIDKey: corrID,
		ClientIDKey:      c.config.ClientID,
		RequestTypeKey:   string(reqMsg.ProtoReflect().Descriptor().FullName()),
	}
	c.propagator.Inject(ctx, propagation.MapCarrier(props))

	// apply custom context propagators
	propagatedProps := extractAll(ctx, c.propagators)
	for k, v := range propagatedProps {
		props[k] = v
	}

	// add method name to properties if present in metadata (for single-topic mode)
	if reqMetadata != nil {
		if methodNames, ok := reqMetadata[MethodNameKey]; ok && len(methodNames) > 0 {
			props[MethodNameKey] = methodNames[0]
		}
	}

	// create future channels
	future := &Future[*Envelope]{
		ResultCh: make(chan *Envelope, 1),
		ErrCh:    make(chan error, 1),
		Ctx:      ctx,
		Done:     make(chan struct{}),
	} // will get cleaned up by GC on err when map no longer references
	c.responseMap.Store(corrID, future)

	// send request
	_, err = producer.Send(ctx, &pulsar.ProducerMessage{
		Payload:    envelopeBytes,
		Key:        corrID,
		Properties: props,
	})
	if err != nil {
		if cancel != nil {
			cancel()
		}
		// remove the future from the map
		c.responseMap.Delete(corrID)

		// release the semaphore and error out
		<-c.semaphore
		c.pendingCounter.Add(-1)
		errCh := make(chan error, 1)
		errCh <- oops.In(ServiceName).Wrapf(err, "sending request to topic %s", requestTopic)
		close(errCh)
		return &Future[*Envelope]{
			ResultCh: make(chan *Envelope),
			ErrCh:    errCh,
			Ctx:      ctx,
			Done:     make(chan struct{}),
		}
	}

	// clean up goroutine
	go func() {
		defer func() {
			if cancel != nil {
				cancel() // clean up timeout context when done
			}
			c.responseMap.Delete(corrID)
			close(future.ResultCh)
			close(future.ErrCh)
			<-c.semaphore
			c.pendingCounter.Add(-1)
		}()

		select {
		case <-future.Done:
			// result/error consumed by Resolve(), cleanup will happen in defer
		case <-ctx.Done():
			// context cancelled, send error
			select {
			case future.ErrCh <- ctx.Err():
			default:
				// channel already has a value
			}
			select {
			case <-future.Done:
			default:
				// channel already has a value
			}
		}
	}()

	return future
}

func (c *ServiceClient) processResponses() {
	for {
		// check if stopped every 100 ms
		if !c.IsStarted() {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		select {
		case <-c.ctx.Done():
			return // we were externally canceled
		case msg := <-c.consumer.Chan():
			// custom handler if provided (bypasses default processing)
			if c.config != nil && c.config.ConsumerMessageHandler != nil {
				c.config.ConsumerMessageHandler(msg)
				_ = c.consumer.Ack(msg)
				continue
			}

			// try to get information about message
			corrID := msg.Properties()[CorrelationIDKey]
			clientID := msg.Properties()[ClientIDKey]

			if corrID == "" || clientID != c.config.ClientID {
				c.consumer.Nack(msg) // could not extract proper information
				continue
			}

			// unmarshal
			var envelope Envelope
			if err := json.Unmarshal(msg.Payload(), &envelope); err != nil {
				c.consumer.Nack(msg) // could not process message
				continue
			}

			// see if an existing request was made, resolve
			val, exists := c.responseMap.Load(corrID)
			if exists {
				future := val.(*Future[*Envelope])
				future.ResultCh <- &envelope
				_ = c.consumer.Ack(msg)
			} else {
				// this request was not us, continue
				c.consumer.Nack(msg)
			}
		}
	}
}

func (c *ServiceClient) getOrCreateProducer(topic string) (pulsar.Producer, error) {
	c.producerMu.Lock()
	defer c.producerMu.Unlock()

	// check producer cache
	if producer, exists := c.producerCache.Get(topic); exists {
		return producer, nil
	}

	// determine producer name (use as a prefix for multiple producers)
	producerNamePrefix := c.config.ProducerName
	if producerNamePrefix == "" {
		// auto-generate prefix if not provided
		producerNamePrefix = "rpc-client-" + c.config.ClientID
	}

	// create a unique producer name per topic
	producerName := producerNamePrefix + "-" + topic

	// Build fully qualified topic with namespace/tenant
	fullyQualifiedTopic := BuildTopic(topic, c.config.PulsarConfig, false)

	// create new producer
	prodOpts := pulsar.ProducerOptions{
		Topic: fullyQualifiedTopic,
		Name:  producerName,
	}

	if c.config != nil && c.config.TopicPolicies != nil {
		applyProducerPolicies(&prodOpts, c.config.TopicPolicies)
	}

	producer, err := c.config.PulsarClient.CreateProducer(prodOpts)
	if err != nil {
		return nil, err
	}

	// add to cache
	c.producerCache.Add(topic, producer)
	return producer, nil
}
