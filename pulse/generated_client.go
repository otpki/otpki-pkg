package pulse

import (
	"context"

	"connectrpc.com/connect"
	"github.com/otpki/otpki-pkg/log"
	"github.com/samber/oops"
	"google.golang.org/protobuf/proto"
)

// GeneratedClient is used for code generators
type GeneratedClient struct {
	*ServiceClient
	serviceName string
}

func NewGeneratedClient(ctx context.Context, config *RPCClientConfig, logger log.Logger, serviceName string) (*GeneratedClient, error) {
	// auto-generate a response topic from the service name if not provided
	if config.ResponseTopic == "" {
		config.ResponseTopic = serviceName + "-responses"
	}

	baseClient, err := NewServiceClient(ctx, config, logger)
	if err != nil {
		return nil, err
	}

	return &GeneratedClient{
		ServiceClient: baseClient,
		serviceName:   serviceName,
	}, nil
}

func (c *GeneratedClient) CallMethod(ctx context.Context, methodName string, req proto.Message, resp proto.Message, reqMetadata map[string][]string) (map[string][]string, error) {
	// add method name to metadata for server-side dispatch
	if reqMetadata == nil {
		reqMetadata = make(map[string][]string)
	}
	reqMetadata[MethodNameKey] = []string{methodName}

	return c.Call(ctx, c.serviceName, req, resp, reqMetadata)
}

func (c *GeneratedClient) CallMethodAsync(ctx context.Context, methodName string, req proto.Message, resp proto.Message, reqMetadata map[string][]string) *Future[*Envelope] {
	// add method name to metadata for server-side dispatch
	if reqMetadata == nil {
		reqMetadata = make(map[string][]string)
	}
	reqMetadata[MethodNameKey] = []string{methodName}

	rawFuture := c.CallAsync(ctx, c.serviceName, req, reqMetadata)

	// wrap the future to handle unmarshalling in the background
	wrappedFuture := &Future[*Envelope]{
		ResultCh: make(chan *Envelope, 1),
		ErrCh:    make(chan error, 1),
		Ctx:      ctx,
		Done:     make(chan struct{}),
	}

	go func() {
		// resolve the envelope
		envelope, err := rawFuture.Resolve()
		if err != nil {
			wrappedFuture.ErrCh <- err
			close(wrappedFuture.ResultCh)
			close(wrappedFuture.ErrCh)
			return
		}

		// check for envelope error and convert to Connect error
		if envelope.Error != nil {
			wrappedFuture.ErrCh <- connect.NewError(
				parseConnectCode(envelope.Error.Code),
				oops.Errorf("%s", envelope.Error.Message),
			)
			close(wrappedFuture.ResultCh)
			close(wrappedFuture.ErrCh)
			return
		}

		// unmarshal response into provided message
		if err := proto.Unmarshal(envelope.Payload, resp); err != nil {
			wrappedFuture.ErrCh <- oops.In(ServiceName).Wrapf(err, "unmarshalling response")
			close(wrappedFuture.ResultCh)
			close(wrappedFuture.ErrCh)
			return
		}

		wrappedFuture.ResultCh <- envelope
		close(wrappedFuture.ResultCh)
		close(wrappedFuture.ErrCh)
	}()

	return wrappedFuture
}
