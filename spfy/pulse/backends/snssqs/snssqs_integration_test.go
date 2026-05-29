package snssqs_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/otpki/otpki-pkg/spfy/pulse"
	"github.com/otpki/otpki-pkg/spfy/pulse/backends/snssqs"
	"github.com/otpki/otpki-pkg/spfy/pulse/pulsetest"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var localstackFixture struct {
	once      sync.Once
	endpoint  string
	container testcontainers.Container
	err       error
}

func ipv4Localhost(host string) string {
	if host == "localhost" || host == "::1" || host == "[::1]" {
		return "127.0.0.1"
	}
	return host
}

func waitLocalStack(ctx context.Context, endpoint string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(60 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/_localstack/health", nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 500 {
				return nil
			}
		}
		last = err
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return last
}

func localstackEndpoint(t testing.TB) string {
	t.Helper()
	localstackFixture.once.Do(func() {
		ctx := context.Background()
		ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image: "localstack/localstack:3.5",
				Env: map[string]string{
					"SERVICES":           "sns,sqs",
					"AWS_DEFAULT_REGION": "us-east-1",
				},
				ExposedPorts: []string{"4566/tcp"},
				WaitingFor: wait.ForAll(
					wait.ForListeningPort("4566/tcp"),
					wait.ForLog("Ready."),
				).WithStartupTimeout(90 * time.Second),
			},
			Started: true,
		})
		if err != nil {
			localstackFixture.err = err
			return
		}
		host, err := ctr.Host(ctx)
		if err != nil {
			_ = ctr.Terminate(context.Background())
			localstackFixture.err = err
			return
		}
		host = ipv4Localhost(host)
		port, err := ctr.MappedPort(ctx, "4566/tcp")
		if err != nil {
			_ = ctr.Terminate(context.Background())
			localstackFixture.err = err
			return
		}
		localstackFixture.container = ctr
		localstackFixture.endpoint = fmt.Sprintf("http://%s:%s", host, port.Port())
		if err := waitLocalStack(ctx, localstackFixture.endpoint); err != nil {
			_ = ctr.Terminate(context.Background())
			localstackFixture.err = err
			return
		}
	})
	if localstackFixture.err != nil {
		t.Fatal(localstackFixture.err)
	}
	return localstackFixture.endpoint
}

func TestMain(m *testing.M) {
	code := m.Run()
	if localstackFixture.container != nil {
		_ = localstackFixture.container.Terminate(context.Background())
	}
	os.Exit(code)
}

func TestIntegrationConformance(t *testing.T) {
	pulsetest.RunConformance(t, pulsetest.Factory{Name: "snssqs", New: func(tb testing.TB) pulse.Backend {
		endpoint := localstackEndpoint(tb)
		cfg, err := awsconfig.LoadDefaultConfig(context.Background(), awsconfig.WithRegion("us-east-1"), awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")))
		if err != nil {
			tb.Fatal(err)
		}
		snsClient := sns.NewFromConfig(cfg, func(o *sns.Options) { o.BaseEndpoint = aws.String(endpoint) })
		sqsClient := sqs.NewFromConfig(cfg, func(o *sqs.Options) { o.BaseEndpoint = aws.String(endpoint) })
		b, err := snssqs.New(snsClient, sqsClient, snssqs.Config{NamePrefix: "test-" + pulse.SafeName(tb.Name()) + "-"})
		if err != nil {
			tb.Fatal(err)
		}
		return b
	}, AfterSubscribe: func(testing.TB) { time.Sleep(2 * time.Second) }})
}
