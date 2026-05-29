package pulse

import (
	"time"

	"github.com/apache/pulsar-client-go/pulsar"
	"github.com/otpki/otpki-pkg/config"
)

type Runnable interface {
	Start()    // start the listener
	Shutdown() // close everything (should not restart!)
}

type TopicPolicies struct {
	// Producer-only policies (applied to pulsar.ProducerOptions)
	MaxPendingMessages int           // Max pending messages before blocking
	BatchMaxMessages   uint          // Max messages per batch
	BatchMaxBytes      uint          // Max batch size in bytes
	BatchMaxDelay      time.Duration // Max delay before sending batch

	// Consumer-only policies (applied to pulsar.ConsumerOptions)
	DLQMaxRedeliver       int                     // Dead letter queue max redelivery count (<0 disables DLQ)
	DLQTopic              string                  // Dead letter queue topic name
	SubscriptionType      pulsar.SubscriptionType // Subscription type: Exclusive, Shared, KeyShared, Failover
	InOrderConsumption    bool                    // Enable in-order consumption (sets Type to KeyShared)
	OutOfOrderConsumption bool                    // Enable out-of-order parallel consumption (sets Type to Shared)
}

type RPCClientConfig struct {
	PulsarConfig           config.PulsarConfig
	PulsarClient           pulsar.Client
	TopicPolicies          *TopicPolicies       // optional
	RequestTopic           string               // non-fully qualified request topic (we produce)
	ResponseTopic          string               // non-fully qualified response topic (we consume)
	ClientID               string               // optional, typically an auto-generated UUID
	ProducerName           string               // optional, to override 'rpc-producer-{ClientID}'
	Timeout                time.Duration        // optional, default 5s
	MaxRetries             int                  // optional, default 3
	MaxPendingRequests     int                  // optional, default 10000. different from TopicPolicies.MaxPendingMessages, this protects our app, not pulsar
	ConsumerMessageHandler func(pulsar.Message) // optional, overwrites default processing
	MaxProducerCacheSize   int                  // optional, default 100. used when multiple producers are used (currently only ServiceClient/connect)
	// ContextPropagators are applied to propagate context values across Pulsar messages.
	// If empty, uses globally registered propagators via RegisterContextPropagator.
	// These are applied in addition to OpenTelemetry propagation (which is always enabled).
	ContextPropagators []ContextPropagator
}

type RPCServerConfig struct {
	PulsarConfig         config.PulsarConfig
	PulsarClient         pulsar.Client
	TopicPolicies        *TopicPolicies // optional
	RequestTopic         string         // non-fully qualified request topic (we consume)
	ResponseTopic        string         // non-fully qualified response topic (we produce)
	ServerID             string         // optional, typically an auto-generate UUID
	ProducerName         string         // optional, to override 'rpc-producer-{ServerID}'
	ResponseTimeout      time.Duration  // optional, default 5s. timeout for sending responses
	MaxProducerCacheSize int            // optional, default 100. used for per-client producer caching
	// ContextPropagators are applied to propagate context values from incoming Pulsar messages.
	// If empty, uses globally registered propagators via RegisterContextPropagator.
	// These are applied in addition to OpenTelemetry propagation (which is always enabled).
	ContextPropagators []ContextPropagator
}
