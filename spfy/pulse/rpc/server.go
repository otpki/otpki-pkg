package rpc

import (
	"context"
	"sync"

	"connectrpc.com/connect"
	"github.com/otpki/otpki-pkg/spfy/pulse"
	"github.com/samber/oops"
	"google.golang.org/protobuf/proto"
)

type Server struct {
	backend     pulse.Backend
	serviceName string
	reliability pulse.Reliability

	mu   sync.Mutex
	subs []pulse.Subscription
}

type ServerOption func(*Server)

func WithServerReliability(r pulse.Reliability) ServerOption {
	return func(s *Server) { s.reliability = r }
}

func NewServer(backend pulse.Backend, serviceName string, opts ...ServerOption) *Server {
	s := &Server{backend: backend, serviceName: serviceName}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Server) Handle(ctx context.Context, method string, handler pulse.RequestHandler) error {
	if s == nil || s.backend == nil {
		return pulse.ErrClosed
	}
	m := Method{Service: s.serviceName, Name: method}
	sub, err := s.backend.Handle(ctx, m.Subject(), handler, pulse.SubscribeOptions{
		Delivery:    pulse.WorkQueue,
		Group:       m.Group(),
		Reliability: s.reliability,
	})
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.subs = append(s.subs, sub)
	s.mu.Unlock()
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	subs := append([]pulse.Subscription(nil), s.subs...)
	s.subs = nil
	s.mu.Unlock()
	var errs []error
	for _, sub := range subs {
		if err := sub.Close(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return oops.Join(errs...)
}

func UnaryHandler[Req any, Resp any](
	newReq func() *Req,
	handler func(context.Context, *connect.Request[Req]) (*connect.Response[Resp], error),
) pulse.RequestHandler {
	return func(ctx context.Context, msg pulse.Message) (pulse.Message, error) {
		reqMsg := newReq()
		protoReq, ok := any(reqMsg).(proto.Message)
		if !ok {
			return pulse.Message{}, oops.New("pulse rpc: request type does not implement proto.Message")
		}
		if err := proto.Unmarshal(msg.Data, protoReq); err != nil {
			return pulse.Message{}, err
		}

		connectReq := connect.NewRequest(reqMsg)
		pulse.CopyHeaderToHTTP(msg.Header, connectReq.Header())

		connectResp, err := handler(ctx, connectReq)
		if err != nil {
			return ErrorMessage(err), nil
		}
		if connectResp == nil || connectResp.Msg == nil {
			return ErrorMessage(connect.NewError(connect.CodeInternal, oops.New("nil response"))), nil
		}
		protoResp, ok := any(connectResp.Msg).(proto.Message)
		if !ok {
			return pulse.Message{}, oops.New("pulse rpc: response type does not implement proto.Message")
		}
		data, err := proto.Marshal(protoResp)
		if err != nil {
			return pulse.Message{}, err
		}
		return pulse.Message{Header: pulse.HeaderFromHTTP(connectResp.Header()), Data: data}, nil
	}
}
