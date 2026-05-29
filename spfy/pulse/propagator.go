package pulse

import (
	"context"
	"sync"
	"sync/atomic"
)

// ContextPropagator defines how context values are propagated across Pulse messages.
//
// Extract runs before sending or publishing a message.
// Inject runs before invoking a subscriber or RPC handler.
//
// Implementations should be safe for concurrent use.
type ContextPropagator interface {
	Extract(ctx context.Context) map[string]string
	Inject(ctx context.Context, properties map[string]string) context.Context
}

// ContextPropagatorRegistry is a live, race-free, copy-on-write registry.
//
// Reads are lock-free. Writes take a mutex and publish a new immutable snapshot.
// A PropagatingBackend using this registry observes new registrations on the
// next send/receive without needing to be recreated.
type ContextPropagatorRegistry struct {
	mu    sync.Mutex
	value atomic.Value // stores []ContextPropagator
}

func NewContextPropagatorRegistry(propagators ...ContextPropagator) *ContextPropagatorRegistry {
	r := &ContextPropagatorRegistry{}
	r.value.Store(cleanContextPropagators(propagators))
	return r
}

// Register appends a propagator to the live registry.
func (r *ContextPropagatorRegistry) Register(p ContextPropagator) {
	if p == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	current := r.snapshot()
	next := make([]ContextPropagator, 0, len(current)+1)
	next = append(next, current...)
	next = append(next, p)

	r.value.Store(next)
}

// Set replaces all propagators in the live registry.
func (r *ContextPropagatorRegistry) Set(propagators ...ContextPropagator) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.value.Store(cleanContextPropagators(propagators))
}

// Clear removes all propagators from the live registry.
func (r *ContextPropagatorRegistry) Clear() {
	r.Set()
}

// Propagators returns a defensive copy of the current propagator list.
func (r *ContextPropagatorRegistry) Propagators() []ContextPropagator {
	current := r.snapshot()
	out := make([]ContextPropagator, len(current))
	copy(out, current)
	return out
}

// snapshot returns the current immutable registry slice.
// Callers must not mutate the returned slice.
func (r *ContextPropagatorRegistry) snapshot() []ContextPropagator {
	if r == nil {
		return nil
	}

	v := r.value.Load()
	if v == nil {
		return nil
	}

	return v.([]ContextPropagator)
}

func cleanContextPropagators(propagators []ContextPropagator) []ContextPropagator {
	if len(propagators) == 0 {
		return []ContextPropagator{}
	}

	out := make([]ContextPropagator, 0, len(propagators))
	for _, p := range propagators {
		if p != nil {
			out = append(out, p)
		}
	}
	return out
}

var globalContextPropagators = NewContextPropagatorRegistry()

// GlobalContextPropagatorRegistry returns the process-wide live propagator registry.
func GlobalContextPropagatorRegistry() *ContextPropagatorRegistry {
	return globalContextPropagators
}

// RegisterContextPropagator registers a propagator globally.
//
// Because the global registry is live, already-created propagating backends
// observe this new propagator on subsequent sends/receives.
func RegisterContextPropagator(p ContextPropagator) {
	globalContextPropagators.Register(p)
}

// SetContextPropagators replaces the global propagator registry.
//
// Because the global registry is live, already-created propagating backends
// observe the new list on subsequent sends/receives.
func SetContextPropagators(propagators ...ContextPropagator) {
	globalContextPropagators.Set(propagators...)
}

// ClearContextPropagators clears the global propagator registry.
func ClearContextPropagators() {
	globalContextPropagators.Clear()
}

// ContextPropagators returns a defensive copy of the current global propagators.
func ContextPropagators() []ContextPropagator {
	return globalContextPropagators.Propagators()
}

func extractContext(ctx context.Context, propagators []ContextPropagator) map[string]string {
	if len(propagators) == 0 {
		return nil
	}

	out := make(map[string]string)

	for _, p := range propagators {
		if p == nil {
			continue
		}

		props := p.Extract(ctx)
		for k, v := range props {
			if k == "" || v == "" {
				continue
			}

			// Later propagators win on key collision.
			// In normal use, keys should be namespaced enough that this is rare.
			out[k] = v
		}
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

func injectContext(ctx context.Context, header Header, propagators []ContextPropagator) context.Context {
	if len(propagators) == 0 || len(header) == 0 {
		return ctx
	}

	props := make(map[string]string, len(header))
	for k, values := range header {
		if k == "" {
			continue
		}

		for _, v := range values {
			if v != "" {
				props[k] = v
				break
			}
		}
	}

	if len(props) == 0 {
		return ctx
	}

	for _, p := range propagators {
		if p != nil {
			ctx = p.Inject(ctx, props)
		}
	}

	return ctx
}
