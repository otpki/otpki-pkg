package anchortest

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otpki/otpki-pkg/spfy/anchor"
	"github.com/samber/oops"
)

// StressOptions controls the reusable stress test. Zero values are replaced by
// conservative defaults suitable for real backend-backed integration tests.
type StressOptions struct {
	Workers           int
	OperationsPerWorker int
	Timeout           time.Duration
}

func DefaultStressOptions() StressOptions {
	return StressOptions{
		Workers:           16,
		OperationsPerWorker: 100,
		Timeout:           60 * time.Second,
	}
}

func normalizeStressOptions(o StressOptions) StressOptions {
	d := DefaultStressOptions()
	if o.Workers <= 0 {
		o.Workers = d.Workers
	}
	if o.OperationsPerWorker <= 0 {
		o.OperationsPerWorker = d.OperationsPerWorker
	}
	if o.Timeout <= 0 {
		o.Timeout = d.Timeout
	}
	return o
}

func stressOptionsFromEnv(base StressOptions) StressOptions {
	base = normalizeStressOptions(base)
	if v := os.Getenv("ANCHOR_STRESS_WORKERS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			base.Workers = n
		}
	}
	if v := os.Getenv("ANCHOR_STRESS_OPERATIONS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			base.OperationsPerWorker = n
		}
	}
	if v := os.Getenv("ANCHOR_STRESS_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			base.Timeout = d
		}
	}
	return base
}

func RunStressWithOptions(t *testing.T, f Factory, opts StressOptions) {
	if os.Getenv("ANCHOR_STRESS") != "1" {
		t.Skip("set ANCHOR_STRESS=1 to run stress tests")
	}
	opts = stressOptionsFromEnv(opts)
	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()
	store := f.New(t)
	
	var ok atomic.Int64
	var firstErr atomic.Value
	var wg sync.WaitGroup
	wg.Add(opts.Workers)
	for i := 0; i < opts.Workers; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opts.OperationsPerWorker; j++ {
				key := fmt.Sprintf("stress/%d/%d", id, j)
				value := StateValue{Name: "stress", Count: j}
				
				// Perform a Put operation
				entry, err := store.Put(ctx, key, value, anchor.WriteOptions{})
				if err != nil {
					if firstErr.Load() == nil {
						firstErr.Store(err)
					}
					return
				}
				
				// Verify the Put with a Get
				got, err := store.Get(ctx, key)
				if err != nil {
					if firstErr.Load() == nil {
						firstErr.Store(err)
					}
					return
				}
				if got.ETag != entry.ETag || got.Value.Name != value.Name || got.Value.Count != value.Count {
					if firstErr.Load() == nil {
						firstErr.Store(oops.Errorf("unexpected value %v for key %s", got, key))
					}
					return
				}
				
				// Perform a Delete
				if err := store.Delete(ctx, key); err != nil {
					if firstErr.Load() == nil {
						firstErr.Store(err)
					}
					return
				}
				
				ok.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if v := firstErr.Load(); v != nil {
		t.Fatalf("stress failed after %d successful operations: %v", ok.Load(), v)
	}
	if got, want := ok.Load(), int64(opts.Workers*opts.OperationsPerWorker); got != want {
		t.Fatalf("successful operations = %d, want %d", got, want)
	}
}
