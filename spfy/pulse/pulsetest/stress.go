package pulsetest

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otpki/otpki-pkg/spfy/pulse"
	"github.com/samber/oops"
)

// StressOptions controls the reusable stress test. Zero values are replaced by
// conservative defaults suitable for real broker-backed integration tests.
type StressOptions struct {
	Workers           int
	RequestsPerWorker int
	Timeout           time.Duration
	RequestTimeout    time.Duration
}

func DefaultStressOptions() StressOptions {
	return StressOptions{
		Workers:           16,
		RequestsPerWorker: 100,
		Timeout:           60 * time.Second,
		RequestTimeout:    5 * time.Second,
	}
}

func normalizeStressOptions(o StressOptions) StressOptions {
	d := DefaultStressOptions()
	if o.Workers <= 0 {
		o.Workers = d.Workers
	}
	if o.RequestsPerWorker <= 0 {
		o.RequestsPerWorker = d.RequestsPerWorker
	}
	if o.Timeout <= 0 {
		o.Timeout = d.Timeout
	}
	if o.RequestTimeout <= 0 {
		o.RequestTimeout = d.RequestTimeout
	}
	return o
}

func stressOptionsFromEnv(base StressOptions) StressOptions {
	base = normalizeStressOptions(base)
	if v := os.Getenv("PULSE_STRESS_WORKERS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			base.Workers = n
		}
	}
	if v := os.Getenv("PULSE_STRESS_REQUESTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			base.RequestsPerWorker = n
		}
	}
	if v := os.Getenv("PULSE_STRESS_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			base.Timeout = d
		}
	}
	if v := os.Getenv("PULSE_STRESS_REQUEST_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			base.RequestTimeout = d
		}
	}
	return base
}

func RunStressWithOptions(t *testing.T, f Factory, opts StressOptions) {
	if os.Getenv("PULSE_STRESS") != "1" {
		t.Skip("set PULSE_STRESS=1 to run stress tests")
	}
	opts = stressOptionsFromEnv(opts)
	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()
	b := f.New(t)
	defer func(b pulse.Backend, ctx context.Context) {
		_ = b.Shutdown(ctx)
	}(b, context.Background())
	subj := subject(t, "stress")
	_, err := b.Handle(ctx, subj, func(ctx context.Context, msg pulse.Message) (pulse.Message, error) {
		return pulse.Message{Header: msg.Header.Clone(), Data: msg.Data}, nil
	}, pulse.SubscribeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	afterSubscribe(t, f)
	var ok atomic.Int64
	var firstErr atomic.Value
	var wg sync.WaitGroup
	wg.Add(opts.Workers)
	for i := 0; i < opts.Workers; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opts.RequestsPerWorker; j++ {
				payload := []byte(fmt.Sprintf("%d-%d", id, j))
				resp, err := b.Request(ctx, subj, pulse.Message{Data: payload}, pulse.RequestOptions{Timeout: opts.RequestTimeout})
				if err != nil {
					if firstErr.Load() == nil {
						firstErr.Store(err)
					}
					return
				}
				if string(resp.Data) != string(payload) {
					if firstErr.Load() == nil {
						firstErr.Store(oops.Errorf("unexpected response %q for payload %q", resp.Data, payload))
					}
					return
				}
				ok.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if v := firstErr.Load(); v != nil {
		t.Fatalf("stress failed after %d successful requests: %v", ok.Load(), v)
	}
	if got, want := ok.Load(), int64(opts.Workers*opts.RequestsPerWorker); got != want {
		t.Fatalf("successful requests = %d, want %d", got, want)
	}
}
