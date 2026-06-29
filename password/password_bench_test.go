package password

import "testing"

const benchPassword = "correct horse battery staple"

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
