package config

import (
	"strings"
	"sync"

	"github.com/samber/oops"
)

// DefaultEntry represents a default configuration value with metadata.
type DefaultEntry struct {
	Value     any                   // Value is the default value.
	Mutable   bool                  // Mutable indicates if this value can be changed after the initial load.
	Required  bool                  // Required indicates if this value must be provided (non-nil, non-empty).
	Validator func(value any) error // Validator is an optional validation function for this specific value. It is only run on required fields.
}

type DefaultRegistry[T any] struct {
	Prefix  string
	entries sync.Map // map[string]DefaultEntry
}

func NewDefaultRegistry[T any](prefix string) *DefaultRegistry[T] {
	prefix = strings.TrimSuffix(prefix, ".") + "."
	return &DefaultRegistry[T]{Prefix: prefix}
}

// Register registers a default value.
func (r *DefaultRegistry[T]) Register(key string, entry DefaultEntry) {
	r.entries.Store(r.Prefix+key, entry)
}

// RegisterSimple registers a simple default value with sensible defaults.
func (r *DefaultRegistry[T]) RegisterSimple(key string, value any) {
	r.Register(key, DefaultEntry{
		Value:   value,
		Mutable: true,
	})
}

// RegisterRequired registers a required default value.
func (r *DefaultRegistry[T]) RegisterRequired(key string, value any) {
	r.Register(key, DefaultEntry{
		Value:    value,
		Mutable:  true,
		Required: true,
	})
}

// Get retrieves a default entry.
func (r *DefaultRegistry[T]) Get(key string) (DefaultEntry, bool) {
	val, ok := r.entries.Load(key)
	if !ok {
		return DefaultEntry{}, false
	}
	return val.(DefaultEntry), true
}

// GetAll returns all default entries.
func (r *DefaultRegistry[T]) GetAll() map[string]DefaultEntry {
	result := make(map[string]DefaultEntry)
	r.entries.Range(func(key, value any) bool {
		result[key.(string)] = value.(DefaultEntry)
		return true
	})
	return result
}

// ValidateRequired validates that all required fields are present and non-empty.
func (r *DefaultRegistry[T]) ValidateRequired(data map[string]any) error {
	var errs []error

	r.entries.Range(func(key, value any) bool {
		entry := value.(DefaultEntry)
		if !entry.Required {
			return true
		}

		k := key.(string)
		val := getNestedValue(data, k)
		if val == nil || val == "" {
			errs = append(errs, oops.In(ServiceName).Errorf("required configuration key '%s' is missing or empty", k))
		}

		// run custom validator if present
		if entry.Validator != nil && val != nil {
			if err := entry.Validator(val); err != nil {
				errs = append(errs, oops.In(ServiceName).Wrapf(err, "validating configuration key '%s'", k))
			}
		}
		return true
	})

	if len(errs) == 0 {
		return nil
	}

	if len(errs) == 1 {
		return errs[0]
	}

	// multiple errors, combine them
	return oops.In(ServiceName).Wrapf(oops.Join(errs...), "validating required configuration keys")
}

// Keys returns all registered keys.
func (r *DefaultRegistry[T]) Keys() []string {
	keys := make([]string, 0)
	r.entries.Range(func(key, value any) bool {
		keys = append(keys, key.(string))
		return true
	})
	return keys
}

// Count returns the number of registered defaults.
func (r *DefaultRegistry[T]) Count() int {
	count := 0
	r.entries.Range(func(key, value any) bool {
		count++
		return true
	})
	return count
}

// Clear removes all registered defaults.
func (r *DefaultRegistry[T]) Clear() {
	r.entries.Range(func(key, value any) bool {
		r.entries.Delete(key)
		return true
	})
}

// Merge merges another registry into this one.
func (r *DefaultRegistry[T]) Merge(other *DefaultRegistry[T]) {
	other.entries.Range(func(key, value any) bool {
		r.entries.Store(key, value)
		return true
	})
}

func getNestedValue(data map[string]any, key string) any {
	parts := strings.Split(key, ".")

	current := any(data)
	for _, part := range parts {
		if currentMap, ok := current.(map[string]any); ok {
			var exists bool
			current, exists = currentMap[part]
			if !exists {
				return nil
			}
		} else {
			return nil
		}
	}

	return current
}
