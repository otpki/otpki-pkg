package redis_test

import (
	"testing"

	"github.com/otpki/otpki-pkg/spfy/pulse"
	pulsedis "github.com/otpki/otpki-pkg/spfy/pulse/backends/redis"
	"github.com/otpki/otpki-pkg/spfy/pulse/pulsetest"
	goredis "github.com/redis/go-redis/v9"
)

func redisIntegrationFactory() pulsetest.Factory {
	return pulsetest.Factory{Name: "redis", New: func(tb testing.TB) pulse.Backend {
		client := goredis.NewClient(&goredis.Options{Addr: redisAddr(tb)})
		waitRedis(tb, client)
		tb.Cleanup(func() { _ = client.Close() })
		b, err := pulsedis.New(client, pulsedis.Config{KeyPrefix: "test:" + pulse.SafeName(tb.Name())})
		if err != nil {
			tb.Fatal(err)
		}
		return b
	}}
}

func TestIntegrationStress(t *testing.T) {
	pulsetest.RunStressWithOptions(t, redisIntegrationFactory(), pulsetest.StressOptions{})
}

func BenchmarkIntegrationRequestReply(b *testing.B) {
	pulsetest.BenchmarkRequestReply(b, redisIntegrationFactory())
}

func BenchmarkIntegrationRequestAsync(b *testing.B) {
	pulsetest.BenchmarkRequestAsync(b, redisIntegrationFactory())
}

func BenchmarkIntegrationFireAndForget(b *testing.B) {
	pulsetest.BenchmarkFireAndForget(b, redisIntegrationFactory())
}

func BenchmarkIntegrationPublishFanout(b *testing.B) {
	pulsetest.BenchmarkPublishFanout(b, redisIntegrationFactory())
}

func BenchmarkIntegrationPublishWorkQueue(b *testing.B) {
	pulsetest.BenchmarkPublishWorkQueue(b, redisIntegrationFactory())
}

func BenchmarkIntegrationRequestReplyParallel(b *testing.B) {
	pulsetest.BenchmarkRequestReplyParallel(b, redisIntegrationFactory())
}
