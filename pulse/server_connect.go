package pulse

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/apache/pulsar-client-go/pulsar"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/otpki/otpki-pkg/helpers"
	"github.com/otpki/otpki-pkg/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/proto"
)

type MethodHandler func(context.Context, proto.Message, map[string][]string) (proto.Message, map[string][]string, error)

type ServiceServer struct {
	*serverBase
	logger           log.Logger
	producerCache    *lru.Cache[string, pulsar.Producer]
	producerMu       sync.Mutex
	handlers         sync.Map        // map[string]MethodHandler
	sharedConsumer   pulsar.Consumer // used only in single-topic-per-service mode
	propagator       propagation.TextMapPropagator
	tracer           trace.Tracer
	sharedTopicSetup sync.Once           // ensures shared consumer is created only once
	closeOnce        sync.Once           // ensures Close() is only called once
	propagators      []ContextPropagator // context propagators for this instance
}

func NewServiceServer(ctx context.Context, config *RPCServerConfig, logger log.Logger) (*ServiceServer, error) {
	base, err := newServerBase(ctx, config)
	if err != nil {
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
		return nil, err
	}
	propagator := otel.GetTextMapPropagator()
	tracer := otel.GetTracerProvider().Tracer("rpc-server")

	// Determine which propagators to use
	propagators := config.ContextPropagators
	if len(propagators) == 0 {
		propagators = globalRegistry.getPropagators()
	}

	return &ServiceServer{
		serverBase:    base,
		logger:        logger,
		producerCache: cache,
		propagator:    propagator,
		tracer:        tracer,
		propagators:   propagators,
	}, nil
}

func (s *ServiceServer) AddHandler(methodName string, requestTopic string, handler MethodHandler, config *RPCServerConfig) error {
	// temporarily overwrite config
	oldConfig := s.config
	if config != nil {
		s.config = config
	}
	defer func() { s.config = oldConfig }()

	// store handler
	s.handlers.Store(methodName, handler)

	var err error
	s.sharedTopicSetup.Do(func() {
		fullyQualifiedTopic := BuildTopic(requestTopic, s.config.PulsarConfig, false)
		s.sharedConsumer, err = s.createConsumerWithPolicies(fullyQualifiedTopic)
	})
	return err
}

func (s *ServiceServer) Start() {
	s.markStarted()

	// single-topic mode: one goroutine dispatches to all handlers
	if s.sharedConsumer != nil {
		s.wg.Go(func() {
			s.processSharedTopic(s.sharedConsumer)
		})
	}
}

func (s *ServiceServer) Shutdown() {
	s.closeOnce.Do(func() {
		s.markStopped()

		// close shared consumer if in single-topic mode
		if s.sharedConsumer != nil {
			s.sharedConsumer.Close()
		}

		// close all cached producers
		if s.producerCache != nil {
			s.producerCache.Purge()
		}

		// close base resources
		s.shutdown()
	})
}

func (s *ServiceServer) processSharedTopic(consumer pulsar.Consumer) {
	for {
		// check if stopped
		if !s.IsStarted() {
			if !helpers.SleepWithContext(s.ctx, 100*time.Millisecond) {
				return // Sleep aborted by loop context cancellation
			}
			continue
		}

		select {
		case <-s.ctx.Done():
			return
		case msg := <-consumer.Chan():
			eCtx := s.propagator.Extract(s.ctx, propagation.MapCarrier(msg.Properties()))
			// extract method name from message properties
			methodName := msg.Properties()[MethodNameKey]
			if methodName == "" {
				s.logger.Error().Ctx(eCtx).Msg("Missing method name in single-topic mode")
				consumer.Nack(msg)
				continue
			}
			eCtx, span := s.tracer.Start(eCtx, "rpc-server:"+methodName)
			defer span.End()

			// look up handler
			handlerVal, ok := s.handlers.Load(methodName)
			if !ok {
				s.logger.Error().Ctx(eCtx).Msgf("No handler registered for method: %s", methodName)
				consumer.Nack(msg)
				continue
			}
			handler := handlerVal.(MethodHandler)

			go s.handleMessage(eCtx, msg, consumer, handler)

		}
	}
}

func (s *ServiceServer) handleMessage(ctx context.Context, msg pulsar.Message, consumer pulsar.Consumer, handler MethodHandler) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error().Ctx(ctx).Msgf("Panic handling message: %v", r)
			consumer.Nack(msg)
		}
	}()

	// Decode envelope
	var envelope Envelope
	if err := json.Unmarshal(msg.Payload(), &envelope); err != nil {
		s.logger.Error().Ctx(ctx).Err(err).Msg("Failed to unmarshal envelope")
		consumer.Nack(msg)
		return
	}

	// Get request type from properties (must be set by client)
	reqTypeName := msg.Properties()[RequestTypeKey]
	if reqTypeName == "" {
		s.logger.Error().Ctx(ctx).Msg("Missing request type property")
		consumer.Nack(msg)
		return
	}

	// Create request message dynamically
	reqMsg, err := createMessageByName(reqTypeName)
	if err != nil {
		s.logger.Error().Ctx(ctx).Err(err).Msg("Failed to create request message")
		consumer.Nack(msg)
		return
	}

	if err := proto.Unmarshal(envelope.Payload, reqMsg); err != nil {
		s.logger.Error().Ctx(ctx).Err(err).Msg("Failed to unmarshal request")
		consumer.Nack(msg)
		return
	}
	handlerCtx, span := s.tracer.Start(ctx, reqTypeName)
	defer span.End()

	// apply custom context propagators to inject values from message properties
	handlerCtx = injectAll(handlerCtx, msg.Properties(), s.propagators)

	// Call handler
	respMsg, respMetadata, handlerErr := handler(handlerCtx, reqMsg, envelope.Metadata)

	// Prepare response envelope
	var respEnvelope Envelope
	respEnvelope.Metadata = respMetadata

	if handlerErr != nil {
		span.RecordError(handlerErr)
		if connectErr, ok := errors.AsType[*connect.Error](handlerErr); ok {
			respEnvelope.Error = &EnvelopeError{
				Code:    connectErr.Code().String(),
				Message: connectErr.Message(),
			}
		}
	}

	if respMsg != nil {
		respBytes, err := proto.Marshal(respMsg)
		if err != nil {
			s.logger.Error().Ctx(ctx).Err(err).Msg("Failed to marshal response")
			consumer.Nack(msg)
			span.RecordError(err)
			return
		}
		respEnvelope.Payload = respBytes
	}

	// Send response
	envelopeBytes, err := json.Marshal(respEnvelope)
	if err != nil {
		s.logger.Error().Ctx(ctx).Err(err).Msg("Failed to marshal response envelope")
		consumer.Nack(msg)
		return
	}

	corrID := msg.Properties()[CorrelationIDKey]
	clientID := msg.Properties()[ClientIDKey]

	// use a timeout for sending response to avoid blocking indefinitely
	sendCtx, cancel := context.WithTimeout(handlerCtx, s.config.ResponseTimeout)

	// get or create producer for this specific client
	clientProducer, err := s.getOrCreateProducer(clientID)
	if err != nil {
		s.logger.Error().Ctx(sendCtx).Err(err).Msg("Failed to get or create producer for client")
		consumer.Nack(msg)
		cancel()
		span.RecordError(err)
		return
	}

	_, err = clientProducer.Send(sendCtx, &pulsar.ProducerMessage{
		Payload: envelopeBytes,
		Properties: map[string]string{
			CorrelationIDKey: corrID,
			ClientIDKey:      clientID,
		},
	})
	cancel()

	if err != nil {
		s.logger.Error().Ctx(sendCtx).Err(err).Msg("Failed to send response")
		consumer.Nack(msg)
		span.RecordError(err)
		return
	}

	_ = consumer.Ack(msg)
}

func (s *ServiceServer) getOrCreateProducer(clientID string) (pulsar.Producer, error) {
	s.producerMu.Lock()
	defer s.producerMu.Unlock()

	// create client-specific response topic name
	clientResponseTopic := GetResponseTopicForClient(s.config.ResponseTopic, clientID)

	// check producer cache
	if producer, exists := s.producerCache.Get(clientResponseTopic); exists {
		return producer, nil
	}

	// determine producer name (use as a prefix for multiple producers)
	producerNamePrefix := s.config.ProducerName
	if producerNamePrefix == "" {
		// auto-generate prefix if not provided
		producerNamePrefix = "rpc-server-producer-" + s.config.ServerID
	}

	// create a unique producer name per client topic
	producerName := producerNamePrefix + "-" + clientID

	// Build fully qualified topic with namespace/tenant
	fullyQualifiedTopic := BuildTopic(clientResponseTopic, s.config.PulsarConfig, true)

	// create new producer
	prodOpts := pulsar.ProducerOptions{
		Topic: fullyQualifiedTopic,
		Name:  producerName,
	}

	if s.config != nil && s.config.TopicPolicies != nil {
		applyProducerPolicies(&prodOpts, s.config.TopicPolicies)
	}

	producer, err := s.config.PulsarClient.CreateProducer(prodOpts)
	if err != nil {
		return nil, err
	}

	// add to cache
	s.producerCache.Add(clientResponseTopic, producer)
	return producer, nil
}
