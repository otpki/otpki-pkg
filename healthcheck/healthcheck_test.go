package healthcheck

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"

	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
)

func TestNewHealthServerServesAggregateStatus(t *testing.T) {
	t.Parallel()

	server := NewHealthServer("")
	resp, err := server.Check(context.Background(), &healthv1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("Check() returned error: %v", err)
	}
	if resp.GetStatus() != healthv1.HealthCheckResponse_SERVING {
		t.Fatalf("expected SERVING status, got %s", resp.GetStatus().String())
	}
}

func TestRegisterServicesMarksSpecificServicesServing(t *testing.T) {
	t.Parallel()

	server := NewHealthServer("")
	server.RegisterServices("otpki.public.issuance.v1.IssuanceService")

	resp, err := server.Check(context.Background(), &healthv1.HealthCheckRequest{
		Service: "otpki.public.issuance.v1.IssuanceService",
	})
	if err != nil {
		t.Fatalf("Check() returned error: %v", err)
	}
	if resp.GetStatus() != healthv1.HealthCheckResponse_SERVING {
		t.Fatalf("expected SERVING status, got %s", resp.GetStatus().String())
	}
}

func TestInProcessHealth(t *testing.T) {
	health := NewHealthServer("", "test.Service")
	server := connect.NewServer()
	health.Register(server)
	client := connect.NewClient(connectinprocess.New(server))
	spec := func(method string, streamType connect.StreamType) connect.Spec {
		return connect.Spec{Procedure: "/" + HealthV1ServiceName + "/" + method, StreamType: streamType}
	}

	var check healthv1.HealthCheckResponse
	if err := client.CallUnary(t.Context(), spec("Check", connect.StreamTypeUnary), &healthv1.HealthCheckRequest{Service: "test.Service"}, &check); err != nil {
		t.Fatal(err)
	}
	if check.GetStatus() != healthv1.HealthCheckResponse_SERVING {
		t.Fatal(check.GetStatus())
	}
	err := client.CallUnary(t.Context(), spec("Check", connect.StreamTypeUnary), &healthv1.HealthCheckRequest{Service: "missing"}, &check)
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("unknown service: %v", err)
	}

	var list healthv1.HealthListResponse
	if err := client.CallUnary(t.Context(), spec("List", connect.StreamTypeUnary), &healthv1.HealthListRequest{}, &list); err != nil {
		t.Fatal(err)
	}
	if list.GetStatuses()["test.Service"].GetStatus() != healthv1.HealthCheckResponse_SERVING {
		t.Fatal(&list)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	watch, err := client.CallServerStream(ctx, spec("Watch", connect.StreamTypeServer), &healthv1.HealthCheckRequest{Service: "test.Service"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := watch.Close(); err != nil {
			t.Errorf("closing watch: %v", err)
		}
	}()
	var update healthv1.HealthCheckResponse
	if err := watch.Receive(&update); err != nil {
		t.Fatal(err)
	}
	if update.GetStatus() != healthv1.HealthCheckResponse_SERVING {
		t.Fatal(update.GetStatus())
	}
	health.SetStatus("test.Service", healthv1.HealthCheckResponse_NOT_SERVING)
	if err := watch.Receive(&update); err != nil {
		t.Fatal(err)
	}
	if update.GetStatus() != healthv1.HealthCheckResponse_NOT_SERVING {
		t.Fatal(update.GetStatus())
	}
	cancel()
	if err := watch.Receive(&update); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}
