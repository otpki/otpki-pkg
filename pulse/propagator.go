package pulse

import "context"

// ContextPropagator defines how context values are propagated across Pulsar messages.
// Implementations can extract values from context (client side) and inject values into
// context (server side). Multiple propagators can be registered and will all be applied.
type ContextPropagator interface {
	// Extract pulls values from the context and returns them as message properties.
	// Called on the client side before sending a message.
	Extract(ctx context.Context) map[string]string

	// Inject takes message properties and adds them to the context.
	// Called on the server side when receiving a message.
	Inject(ctx context.Context, properties map[string]string) context.Context
}

// propagatorRegistry holds all registered context propagators.
// Use RegisterContextPropagator to add propagators.
type propagatorRegistry struct {
	propagators []ContextPropagator
}

var globalRegistry = &propagatorRegistry{}

// RegisterContextPropagator registers a propagator globally.
// This affects all ServiceClient and ServiceServer instances that don't have
// instance-specific propagators configured.
func RegisterContextPropagator(p ContextPropagator) {
	globalRegistry.propagators = append(globalRegistry.propagators, p)
}

// SetContextPropagators replaces all globally registered propagators.
func SetContextPropagators(propagators ...ContextPropagator) {
	globalRegistry.propagators = propagators
}

func (r *propagatorRegistry) getPropagators() []ContextPropagator {
	result := make([]ContextPropagator, len(r.propagators))
	copy(result, r.propagators)
	return result
}

// extractAll runs all registered propagators and merges their extracted properties.
// later propagators can overwrite earlier ones if they use the same keys.
func extractAll(ctx context.Context, propagators []ContextPropagator) map[string]string {
	if len(propagators) == 0 {
		return nil
	}

	result := make(map[string]string)
	for _, p := range propagators {
		props := p.Extract(ctx)
		for k, v := range props {
			result[k] = v
		}
	}
	return result
}

// injectAll runs all registered propagators to build the context chain.
// each propagator gets the result of the previous one.
func injectAll(ctx context.Context, properties map[string]string, propagators []ContextPropagator) context.Context {
	for _, p := range propagators {
		ctx = p.Inject(ctx, properties)
	}
	return ctx
}
