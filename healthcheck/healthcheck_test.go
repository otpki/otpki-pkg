package healthcheck

import (
	"context"
	"testing"

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
