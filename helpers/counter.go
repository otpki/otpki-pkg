package helpers

import (
	"sync"
	"sync/atomic"
)

// Usage:
// This can be a Incrementer field on a struct's "singleton" instance, allowing
// thread safe ID or number incrementation. Especially useful when we need to track an object
// that exists only in memory for it's entire lifetime.
// Incrementer is a thread-safe auto-incrementing ID / counter.

type Incrementer struct {
	counter int64
}

var globalGenerator *Incrementer
var once sync.Once

// Returns global instance
func GetCurrent() *Incrementer {
	once.Do(func() {
		globalGenerator = &Incrementer{
			counter: 0, // First num will be 1
		}
	})
	return globalGenerator
}

func GetNext() int64 {
	return GetCurrent().GetNext()
}

// Creates a new "local" instance for use as a dependency/service.
// Use this when you need a separate sequence of IDs / counters
func NewCounter() *Incrementer {
	return &Incrementer{
		counter: 0,
	}
}

func (g *Incrementer) GetNext() int64 {
	return atomic.AddInt64(&g.counter, 1)
}
