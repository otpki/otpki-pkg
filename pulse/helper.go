package pulse

import (
	"connectrpc.com/connect"
	"github.com/apache/pulsar-client-go/pulsar"
	"github.com/otpki/otpki-pkg/config"
	"github.com/otpki/otpki-pkg/helpers"
	"github.com/samber/oops"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

const (
	ServiceName      = "pulse"
	CorrelationIDKey = "correlation_id"
	ClientIDKey      = "client_id"
	RequestTypeKey   = "request_type"
	MethodNameKey    = "method_name" // used in single-topic-per-service mode
	PrincipalKey     = "user_principal"
)

func BuildTopic(topic string, conf config.PulsarConfig, ephemeral bool) string {
	persist := "persistent"
	if ephemeral {
		//persist = "non-" + persist //TODO: unknown issue with ephemeral topics..
	}
	tenantName := helpers.DefaultIfEmpty(conf.Tenant, "public")
	ns := helpers.DefaultIfEmpty(conf.Namespace, "default")
	return persist + "://" + tenantName + "/" + ns + "/" + topic
}

func GetResponseTopicForClient(baseResponseTopic, clientID string) string {
	return baseResponseTopic + "-" + clientID
}

// applyConsumerPolicies applies topic policies to consumer options
func applyConsumerPolicies(opts *pulsar.ConsumerOptions, policies *TopicPolicies) {
	if policies.SubscriptionType != 0 {
		opts.Type = policies.SubscriptionType
	}
	if policies.DLQMaxRedeliver >= 0 {
		opts.DLQ = &pulsar.DLQPolicy{
			MaxDeliveries:   uint32(policies.DLQMaxRedeliver),
			DeadLetterTopic: policies.DLQTopic,
		}
	}
	if policies.InOrderConsumption {
		opts.Type = pulsar.KeyShared
	} else if policies.OutOfOrderConsumption {
		opts.Type = pulsar.Shared
	}
}

// applyProducerPolicies applies topic policies to producer options
func applyProducerPolicies(opts *pulsar.ProducerOptions, policies *TopicPolicies) {
	if policies.BatchMaxMessages > 0 {
		opts.BatchingMaxMessages = policies.BatchMaxMessages
	}
	if policies.BatchMaxBytes > 0 {
		opts.BatchingMaxSize = policies.BatchMaxBytes
	}
	if policies.BatchMaxDelay > 0 {
		opts.BatchingMaxPublishDelay = policies.BatchMaxDelay
	}
	if policies.MaxPendingMessages > 0 {
		opts.MaxPendingMessages = policies.MaxPendingMessages
	}
}

func createMessageByName(fullName string) (proto.Message, error) {
	msgType, err := protoregistry.GlobalTypes.FindMessageByName(protoreflect.FullName(fullName))
	if err != nil {
		return nil, oops.In(ServiceName).Wrapf(err, "looking up message type %s in registry", fullName)
	}

	// create a new instance of the message
	msg := msgType.New().Interface()
	return msg, nil
}

func parseConnectCode(code string) connect.Code {
	switch code {
	case "canceled":
		return connect.CodeCanceled
	case "unknown":
		return connect.CodeUnknown
	case "invalid_argument":
		return connect.CodeInvalidArgument
	case "deadline_exceeded":
		return connect.CodeDeadlineExceeded
	case "not_found":
		return connect.CodeNotFound
	case "already_exists":
		return connect.CodeAlreadyExists
	case "permission_denied":
		return connect.CodePermissionDenied
	case "resource_exhausted":
		return connect.CodeResourceExhausted
	case "failed_precondition":
		return connect.CodeFailedPrecondition
	case "aborted":
		return connect.CodeAborted
	case "out_of_range":
		return connect.CodeOutOfRange
	case "unimplemented":
		return connect.CodeUnimplemented
	case "internal":
		return connect.CodeInternal
	case "unavailable":
		return connect.CodeUnavailable
	case "data_loss":
		return connect.CodeDataLoss
	case "unauthenticated":
		return connect.CodeUnauthenticated
	default:
		return connect.CodeUnknown
	}
}
