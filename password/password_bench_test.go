package password

import (
	"crypto/rand"
	"fmt"
	"runtime"
	"sync"
	"testing"

	"golang.org/x/crypto/argon2"
)

const benchPassword = "correct horse battery staple"

type argon2Params struct {
	name    string
	memory  uint32
	time    uint32
	threads uint8
}

var argon2Benchmarks = []argon2Params{
	// Current implementation.
	{
		name:    "64MiB_t3_p4",
		memory:  64 * 1024,
		time:    3,
		threads: 4,
	},

	// Isolate the effect of Argon2 parallelism.
	{
		name:    "64MiB_t3_p1",
		memory:  64 * 1024,
		time:    3,
		threads: 1,
	},
	{
		name:    "64MiB_t3_p2",
		memory:  64 * 1024,
		time:    3,
		threads: 2,
	},

	// OWASP minimum.
	{
		name:    "OWASP_19MiB_t2_p1",
		memory:  19 * 1024,
		time:    2,
		threads: 1,
	},

	// Useful for seeing whether additional Argon2 parallelism helps
	// single-request latency at the OWASP memory/time settings.
	{
		name:    "19MiB_t2_p2",
		memory:  19 * 1024,
		time:    2,
		threads: 2,
	},
	{
		name:    "19MiB_t2_p4",
		memory:  19 * 1024,
		time:    2,
		threads: 4,
	},

	// Intermediate point.
	{
		name:    "32MiB_t2_p1",
		memory:  32 * 1024,
		time:    2,
		threads: 1,
	},
	{
		name:    "32MiB_t2_p2",
		memory:  32 * 1024,
		time:    2,
		threads: 2,
	},
}

func benchmarkArgon2(b *testing.B, params argon2Params) {
	b.Helper()

	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		b.Fatal(err)
	}

	password := []byte(benchPassword)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		hash := argon2.IDKey(
			password,
			salt,
			params.time,
			params.memory,
			params.threads,
			keyLength,
		)

		if len(hash) != keyLength {
			b.Fatal("unexpected hash length")
		}
	}

	elapsed := b.Elapsed().Seconds()

	b.ReportMetric(elapsed/float64(b.N), "s/hash")
	b.ReportMetric(float64(b.N)/elapsed, "hashes/s")
}

// BenchmarkArgon2Latency measures the cost of ONE enrollment.
//
// This is useful for request latency, but should NOT be used by itself
// to choose Argon2 parallelism for a concurrent enrollment service.
func BenchmarkArgon2Latency(b *testing.B) {
	for _, params := range argon2Benchmarks {
		b.Run(params.name, func(b *testing.B) {
			benchmarkArgon2(b, params)
		})
	}
}

// BenchmarkArgon2Throughput measures aggregate hashing throughput with
// many enrollment operations happening concurrently.
//
// This is generally the more important benchmark for a PKI enrollment
// service.
func BenchmarkArgon2Throughput(b *testing.B) {
	password := []byte(benchPassword)

	for _, params := range argon2Benchmarks {
		b.Run(params.name, func(b *testing.B) {
			b.ReportAllocs()

			b.ResetTimer()

			b.RunParallel(func(pb *testing.PB) {
				// Give each simulated request its own salt.
				//
				// Do this outside the benchmark iteration because we're
				// specifically measuring Argon2 throughput here rather
				// than crypto/rand throughput.
				salt := make([]byte, saltLength)
				if _, err := rand.Read(salt); err != nil {
					b.Fatal(err)
				}

				for pb.Next() {
					hash := argon2.IDKey(
						password,
						salt,
						params.time,
						params.memory,
						params.threads,
						keyLength,
					)

					if len(hash) != keyLength {
						b.Fatal("unexpected hash length")
					}
				}
			})

			elapsed := b.Elapsed().Seconds()

			b.ReportMetric(float64(b.N)/elapsed, "hashes/s")
		})
	}
}

// Prints some useful information alongside the benchmark so results
// are easier to compare between machines.
func BenchmarkSystemInfo(b *testing.B) {
	b.ReportMetric(float64(runtime.NumCPU()), "logical-cpus")
	b.ReportMetric(float64(runtime.GOMAXPROCS(0)), "gomaxprocs")
}

func BenchmarkArgon2ConcurrentThroughput(b *testing.B) {
	concurrencies := []int{
		1,
		2,
		4,
		8,
		16,
		32,
	}

	password := []byte(benchPassword)

	for _, params := range argon2Benchmarks {
		for _, concurrency := range concurrencies {
			name := params.name + "/concurrency=" + fmt.Sprint(concurrency)

			b.Run(name, func(b *testing.B) {
				b.ReportAllocs()

				sem := make(chan struct{}, concurrency)

				b.ResetTimer()

				var wg sync.WaitGroup

				for i := 0; i < b.N; i++ {
					sem <- struct{}{}

					wg.Go(func() {
						defer func() { <-sem }()

						salt := make([]byte, saltLength)
						if _, err := rand.Read(salt); err != nil {
							b.Error(err)
							return
						}

						hash := argon2.IDKey(
							password,
							salt,
							params.time,
							params.memory,
							params.threads,
							keyLength,
						)

						if len(hash) != keyLength {
							b.Error("unexpected hash length")
						}
					})
				}

				wg.Wait()

				elapsed := b.Elapsed().Seconds()
				b.ReportMetric(float64(b.N)/elapsed, "hashes/s")
			})
		}
	}
}

func BenchmarkHashPassword(b *testing.B) {
	benchmarks := []struct {
		name     string
		password string
	}{
		{"NonEmptyPassword", benchPassword},
		{"EmptyPassword", ""},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				hash, err := HashPassword(bm.password)
				if err != nil {
					b.Fatal(err)
				}
				if hash == "" {
					b.Fatal("empty hash")
				}
			}

			secondsPerOp := b.Elapsed().Seconds() / float64(b.N)
			opsPerSecond := float64(b.N) / b.Elapsed().Seconds()

			b.ReportMetric(secondsPerOp, "s/op")
			b.ReportMetric(opsPerSecond, "op/s")
		})
	}
}

func BenchmarkVerifyPassword(b *testing.B) {
	benchmarks := []struct {
		name     string
		password string
	}{
		{"NonEmptyPassword", benchPassword},
		{"EmptyPassword", ""},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			hash, err := HashPassword(bm.password)
			if err != nil {
				b.Fatal(err)
			}

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				ok, err := VerifyPassword(bm.password, hash)
				if err != nil {
					b.Fatal(err)
				}
				if !ok {
					b.Fatal("password did not verify")
				}
			}

			secondsPerOp := b.Elapsed().Seconds() / float64(b.N)
			opsPerSecond := float64(b.N) / b.Elapsed().Seconds()

			b.ReportMetric(secondsPerOp, "s/op")
			b.ReportMetric(opsPerSecond, "op/s")
		})
	}
}
