package healthcheck

import (
	"context"
	"net/http"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"google.golang.org/grpc/health"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// HealthV1ServiceName is the fully qualified name of the health service.
const HealthV1ServiceName = "grpc.health.v1.Health"

type HealthServer struct {
	healthv1.UnimplementedHealthServer
	grpcHS    *health.Server
	statusMap map[string]healthv1.HealthCheckResponse_ServingStatus
}

func NewHealthServer(services ...string) *HealthServer {
	grpcHS := health.NewServer()
	hs := &HealthServer{grpcHS: grpcHS, statusMap: make(map[string]healthv1.HealthCheckResponse_ServingStatus)}
	hs.RegisterServices(services...)
	return hs
}

// RegisterServices marks each known service as serving for gRPC health checks.
func (h *HealthServer) RegisterServices(services ...string) {
	for _, service := range services {
		h.SetStatus(service, healthv1.HealthCheckResponse_SERVING)
	}
}

func (h *HealthServer) SetStatus(service string, servingStatus healthv1.HealthCheckResponse_ServingStatus) {
	h.statusMap[service] = servingStatus
	h.grpcHS.SetServingStatus(service, servingStatus)
}

func (h *HealthServer) Check(ctx context.Context, req *healthv1.HealthCheckRequest) (*healthv1.HealthCheckResponse, error) {
	return h.grpcHS.Check(ctx, req)
}

func (h *HealthServer) List(_ context.Context, _ *healthv1.HealthListRequest) (*healthv1.HealthListResponse, error) {
	statuses := map[string]*healthv1.HealthCheckResponse{}
	for service, servingStatus := range h.statusMap {
		statuses[service] = &healthv1.HealthCheckResponse{Status: servingStatus}
	}
	return &healthv1.HealthListResponse{Statuses: statuses}, nil
}

func (h *HealthServer) Watch(in *healthv1.HealthCheckRequest, stream healthv1.Health_WatchServer) error {
	return h.grpcHS.Watch(in, stream)
}

// Register exposes Check, List, and Watch on a transport-independent server.
func (h *HealthServer) Register(server *connect.Server) {
	methods := healthv1.File_grpc_health_v1_health_proto.Services().ByName("Health").Methods()
	spec := func(name string, streamType connect.StreamType) connect.Spec {
		return connect.Spec{Procedure: "/" + HealthV1ServiceName + "/" + name, StreamType: streamType, Schema: methods.ByName(protoreflect.Name(name))}
	}
	server.Register(
		connect.Method{Spec: spec("Check", connect.StreamTypeUnary), Handler: func(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
			var req healthv1.HealthCheckRequest
			if err := stream.Receive(&req); err != nil {
				return err
			}
			res, err := h.Check(ctx, &req)
			if err != nil {
				return connectHealthError(err)
			}
			return stream.Send(res)
		}},
		connect.Method{Spec: spec("List", connect.StreamTypeUnary), Handler: func(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
			var req healthv1.HealthListRequest
			if err := stream.Receive(&req); err != nil {
				return err
			}
			res, err := h.List(ctx, &req)
			if err != nil {
				return connectHealthError(err)
			}
			return stream.Send(res)
		}},
		connect.Method{Spec: spec("Watch", connect.StreamTypeServer), Handler: func(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
			var req healthv1.HealthCheckRequest
			if err := stream.Receive(&req); err != nil {
				return err
			}
			return connectHealthError(h.Watch(&req, IntercompatibleStream[healthv1.HealthCheckResponse]{connectStream: stream, ctx: ctx}))
		}},
	)
}

func connectHealthError(err error) error {
	if err == nil {
		return nil
	}
	st := status.Convert(err)
	return connect.NewError(connect.Code(st.Code()), st.Message()).WithCause(err)
}

// NewHandler is a convenience for serving health checks over HTTP.
func (h *HealthServer) NewHandler() (string, http.Handler) {
	mux := http.NewServeMux()
	server := connect.NewServer()
	h.Register(server)
	connecthttp.Mount(mux, server)
	return "/" + HealthV1ServiceName + "/", mux
}

// IntercompatibleStream adapts the gRPC health watch implementation to Connect.
type IntercompatibleStream[Res any] struct {
	connectStream connect.ServerStream
	ctx           context.Context //nolint:containedctx // A gRPC stream must expose the context of its single RPC via Context().
}

func (i IntercompatibleStream[Res]) Send(m *Res) error { return i.connectStream.Send(m) }

func (i IntercompatibleStream[Res]) SetHeader(md metadata.MD) error {
	info, _ := connect.CallInfoForServerContext(i.ctx)
	for key, values := range md {
		info.ResponseHeader().SetValues(key, values)
	}
	return nil
}

func (i IntercompatibleStream[Res]) SendHeader(md metadata.MD) error {
	if err := i.SetHeader(md); err != nil {
		return err
	}
	return i.connectStream.SendHeaders()
}

func (i IntercompatibleStream[Res]) SetTrailer(md metadata.MD) {
	info, _ := connect.CallInfoForServerContext(i.ctx)
	for key, values := range md {
		info.ResponseTrailer().SetValues(key, values)
	}
}

func (i IntercompatibleStream[Res]) Context() context.Context { return i.ctx }
func (i IntercompatibleStream[Res]) SendMsg(m any) error      { return i.connectStream.Send(m) }
func (i IntercompatibleStream[Res]) RecvMsg(m any) error      { return i.connectStream.Receive(m) }
