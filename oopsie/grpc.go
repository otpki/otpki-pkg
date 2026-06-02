package oopsie

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func ToGRPCStatus(err error) error {
	if err == nil {
		return nil
	}

	payload, marshalErr := Marshal(err)
	if marshalErr != nil {
		return status.Error(codes.Unknown, err.Error())
	}

	st := status.New(codes.Unknown, err.Error())
	withDetails, detailErr := st.WithDetails(&wrapperspb.BytesValue{Value: payload})
	if detailErr != nil {
		return st.Err()
	}

	return withDetails.Err()
}

func FromGRPCStatus(err error) error {
	if err == nil {
		return nil
	}

	st, ok := status.FromError(err)
	if !ok {
		return err
	}

	for _, detail := range st.Details() {
		bytesValue, ok := detail.(*wrapperspb.BytesValue)
		if !ok {
			continue
		}

		normalized, unmarshalErr := Unmarshal(bytesValue.Value)
		if unmarshalErr == nil && normalized != nil {
			return normalized
		}
	}

	return err
}

func GRPCCodeOf(err error) codes.Code {
	if err == nil {
		return codes.OK
	}
	return codes.Unknown
}

func GRPCErrorUnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		response, err := handler(ctx, req)
		if err != nil {
			return nil, ToGRPCStatus(err)
		}
		return response, nil
	}
}

func GRPCErrorUnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		err := invoker(ctx, method, req, reply, cc, opts...)
		return FromGRPCStatus(err)
	}
}

func WrapGRPCClientConn(cc grpc.ClientConnInterface, interceptor grpc.UnaryClientInterceptor) grpc.ClientConnInterface {
	if cc == nil || interceptor == nil {
		return cc
	}
	return unaryInterceptingClientConn{
		cc:          cc,
		interceptor: interceptor,
	}
}

type unaryInterceptingClientConn struct {
	cc          grpc.ClientConnInterface
	interceptor grpc.UnaryClientInterceptor
}

func (c unaryInterceptingClientConn) Invoke(ctx context.Context, method string, args any, reply any, opts ...grpc.CallOption) error {
	if conn, ok := c.cc.(*grpc.ClientConn); ok {
		return c.interceptor(ctx, method, args, reply, conn, func(
			ctx context.Context,
			method string,
			req, reply any,
			cc *grpc.ClientConn,
			opts ...grpc.CallOption,
		) error {
			return cc.Invoke(ctx, method, req, reply, opts...)
		}, opts...)
	}

	if err := c.cc.Invoke(ctx, method, args, reply, opts...); err != nil {
		return FromGRPCStatus(err)
	}
	return nil
}

func (c unaryInterceptingClientConn) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return c.cc.NewStream(ctx, desc, method, opts...)
}
