package snssqs_test

import (
	"context"
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
)

func snssqsIntegrationFactory() pulsetest.Factory {
	return pulsetest.Factory{Name: "snssqs", New: func(tb testing.TB) pulse.Backend {
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
	}, AfterSubscribe: func(testing.TB) { time.Sleep(2 * time.Second) }}
}

func TestIntegrationStress(t *testing.T) {
	pulsetest.RunStressWithOptions(t, snssqsIntegrationFactory(), pulsetest.StressOptions{Workers: 4, RequestsPerWorker: 25, Timeout: 2 * time.Minute, RequestTimeout: 15 * time.Second})
}

func BenchmarkIntegrationRequestReply(b *testing.B) {
	pulsetest.BenchmarkRequestReply(b, snssqsIntegrationFactory())
}

func BenchmarkIntegrationRequestAsync(b *testing.B) {
	pulsetest.BenchmarkRequestAsync(b, snssqsIntegrationFactory())
}

func BenchmarkIntegrationFireAndForget(b *testing.B) {
	pulsetest.BenchmarkFireAndForget(b, snssqsIntegrationFactory())
}

func BenchmarkIntegrationPublishFanout(b *testing.B) {
	pulsetest.BenchmarkPublishFanout(b, snssqsIntegrationFactory())
}

func BenchmarkIntegrationPublishWorkQueue(b *testing.B) {
	pulsetest.BenchmarkPublishWorkQueue(b, snssqsIntegrationFactory())
}

func BenchmarkIntegrationRequestReplyParallel(b *testing.B) {
	pulsetest.BenchmarkRequestReplyParallel(b, snssqsIntegrationFactory())
}
