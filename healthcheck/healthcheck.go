package healthcheck

import (
	"context"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	"google.golang.org/grpc/health"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
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

func (h *HealthServer) NewHandler() (string, http.Handler) {
	const serviceName = "/" + HealthV1ServiceName + "/"
	mux := http.NewServeMux()
	check := connect.NewUnaryHandler(
		serviceName+"Check",
		func(
			ctx context.Context,
			req *connect.Request[healthv1.HealthCheckRequest],
		) (*connect.Response[healthv1.HealthCheckResponse], error) {
			checkResponse, err := h.Check(ctx, req.Msg)
			if err != nil {
				return nil, err
			}
			return connect.NewResponse(checkResponse), nil
		},
	)
	mux.Handle(serviceName+"Check", check)
	list := connect.NewUnaryHandler(
		serviceName+"List",
		func(
			ctx context.Context,
			req *connect.Request[healthv1.HealthListRequest],
		) (*connect.Response[healthv1.HealthListResponse], error) {
			listResponse, err := h.List(ctx, req.Msg)
			if err != nil {
				return nil, err
			}
			return connect.NewResponse(listResponse), nil
		},
	)
	mux.Handle(serviceName+"List", list)
	watch := connect.NewServerStreamHandler(
		serviceName+"Watch",
		func(
			ctx context.Context,
			req *connect.Request[healthv1.HealthCheckRequest],
			stream *connect.ServerStream[healthv1.HealthCheckResponse],
		) error {
			err := h.Watch(req.Msg, IntercompatibleStream[healthv1.HealthCheckResponse]{connectStream: stream, ctx: ctx})
			if err != nil {
				return err
			}
			return nil
		},
	)
	mux.Handle(serviceName+"Watch", watch)
	return serviceName, mux
}

// IntercompatibleStream is a wrapper that should not exist and
// only exists because I don't want to write my own concurrent Watch()
type IntercompatibleStream[Res any] struct {
	connectStream *connect.ServerStream[Res]
	ctx           context.Context
}

func (i IntercompatibleStream[Res]) Send(m *Res) error {
	return i.connectStream.Send(m)
}

func (i IntercompatibleStream[Res]) SetHeader(md metadata.MD) error {
	for k, v := range md {
		// TODO: check the right way to do this
		i.connectStream.Conn().ResponseHeader().Set(k, strings.Join(v, ","))
	}
	return nil
}

func (i IntercompatibleStream[Res]) SendHeader(md metadata.MD) error {
	err := i.SetHeader(md)
	if err != nil {
		return err
	}
	return i.Send(nil)
}

func (i IntercompatibleStream[Res]) SetTrailer(md metadata.MD) {
	for k, v := range md {
		// TODO: check the right way to do this
		i.connectStream.Conn().ResponseTrailer().Set(k, strings.Join(v, ","))
	}
}

func (i IntercompatibleStream[Res]) Context() context.Context {
	return i.ctx
}

func (i IntercompatibleStream[Res]) SendMsg(m any) error {
	return i.connectStream.Conn().Send(m)
}

func (i IntercompatibleStream[Res]) RecvMsg(m any) error {
	return i.connectStream.Conn().Receive(m)
}
