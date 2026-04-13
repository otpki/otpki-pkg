package config

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/samber/oops"
)

const ServiceName = "config"

// Manager is a generic, thread-safe configuration manager that supports
// multiple configuration sources, and validation.
type Manager[T any] struct {
	loadMu sync.Mutex // for serializing Load/Reload operations

	// current configuration (lock-free reads via atomic)
	config atomic.Pointer[T]

	// configuration metadata (lock-free reads via atomic)
	version  atomic.Uint64
	loadedAt atomic.Int64 // unix timestamp

	// configuration sources and options (immutable after creation)
	loaders   []Loader[T]
	defaults  *DefaultRegistry[T]
	validator Validator[T]
	hooks     []UnmarshalHook

	// key transformation options
	namespace    string // prefix to add to all loaded keys
	filterPrefix string // only load keys with this prefix
	stripPrefix  bool   // strip the filter prefix from keys

	// callbacks (immutable after creation)
	onReload []func(old, new *T)
}

type ManagerOption[T any] func(*Manager[T])

func NewManager[T any](opts ...ManagerOption[T]) *Manager[T] {
	m := &Manager[T]{
		loaders: make([]Loader[T], 0),
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// WithLoaders adds configuration loaders in priority order (first loader has highest priority).
func WithLoaders[T any](loaders ...Loader[T]) ManagerOption[T] {
	return func(m *Manager[T]) {
		m.loaders = append(m.loaders, loaders...)
	}
}

// WithDefaults sets the default registry.
func WithDefaults[T any](defaults *DefaultRegistry[T]) ManagerOption[T] {
	return func(m *Manager[T]) {
		m.defaults = defaults
	}
}

// WithValidator sets the configuration validator.
func WithValidator[T any](validator Validator[T]) ManagerOption[T] {
	return func(m *Manager[T]) {
		m.validator = validator
	}
}

// WithUnmarshalHooks adds custom unmarshal hooks.
func WithUnmarshalHooks[T any](hooks ...UnmarshalHook) ManagerOption[T] {
	return func(m *Manager[T]) {
		m.hooks = append(m.hooks, hooks...)
	}
}

// WithReloadCallback adds a callback invoked after a successful reload.
func WithReloadCallback[T any](fn func(old, new *T)) ManagerOption[T] {
	return func(m *Manager[T]) {
		m.onReload = append(m.onReload, fn)
	}
}

// WithNamespace adds a namespace prefix to all loaded configuration keys.
// Ex: WithNamespace("app1") transforms "host" → "app1.host"
func WithNamespace[T any](namespace string) ManagerOption[T] {
	return func(m *Manager[T]) {
		m.namespace = namespace
	}
}

// WithKeyFilter filters loaded keys to only include those with the given prefix.
// If stripPrefix is true, the prefix is removed from the keys.
// Ex: WithKeyFilter("app1.", true) transforms "app1.host" → "host"
func WithKeyFilter[T any](prefix string, stripPrefix bool) ManagerOption[T] {
	return func(m *Manager[T]) {
		m.filterPrefix = prefix
		m.stripPrefix = stripPrefix
	}
}

// Load loads the configuration from all configured sources and validates it.
func (m *Manager[T]) Load(ctx context.Context) error {
	m.loadMu.Lock()
	defer m.loadMu.Unlock()

	return m.load(ctx)
}

func (m *Manager[T]) load(ctx context.Context) error {
	// create merged config data
	data := make(map[string]any)

	// apply defaults first
	if m.defaults != nil {
		m.defaults.entries.Range(func(key, value any) bool {
			def := value.(DefaultEntry)
			setNestedValue(data, strings.ToLower(key.(string)), def.Value)
			return true
		})
	}

	// load from each source in reverse order (last has the lowest priority)
	for i := len(m.loaders) - 1; i >= 0; i-- {
		loader := m.loaders[i]
		sourceData, err := loader.Load(ctx)
		if err != nil {
			// check if this is a "source not found" error (acceptable)
			if oopsErr, ok := errors.AsType[oops.OopsError](err); ok && oopsErr.Code() == "source_not_found" {
				// source not found is acceptable, skip it
				continue
			}
			return oops.In(ServiceName).Wrapf(err, "loading configuration from source")
		}

		// deep merge data to preserve nested defaults
		deepMerge(data, sourceData)
	}

	// apply key transformations (filter and namespace)
	data = m.applyKeyTransformations(data)

	// unmarshal into config struct
	var cfg T
	if err := unmarshalMap(data, &cfg, m.hooks); err != nil {
		return oops.In(ServiceName).Wrapf(err, "unmarshalling configuration")
	}

	// validate if validator is set
	if m.validator != nil {
		if err := m.validator.Validate(&cfg); err != nil {
			return oops.In(ServiceName).Wrapf(err, "validating configuration")
		}
	}

	// validate defaults if the registry is set
	if m.defaults != nil {
		if err := m.defaults.ValidateRequired(data); err != nil {
			return oops.In(ServiceName).Wrapf(err, "validating required configuration")
		}
	}

	// update configuration
	m.config.Store(&cfg)
	m.version.Add(1)
	m.loadedAt.Store(time.Now().Unix())

	return nil
}

// Reload reloads the configuration from all sources.
func (m *Manager[T]) Reload(ctx context.Context) error {
	m.loadMu.Lock()
	defer m.loadMu.Unlock()

	old := m.config.Load()
	if err := m.load(ctx); err != nil {
		return err
	}

	// invoke reload callbacks
	newCfg := m.config.Load()
	for _, fn := range m.onReload {
		fn(old, newCfg)
	}

	return nil
}

// Get returns the current configuration.
// Returns nil if the configuration has not been loaded yet.
func (m *Manager[T]) Get() *T {
	return m.config.Load()
}

// MustGet returns the current configuration or panics if not loaded.
func (m *Manager[T]) MustGet() *T {
	cfg := m.config.Load()
	if cfg == nil {
		panic("configuration not loaded")
	}
	return cfg
}

// GetWithDefault returns the current configuration or the provided default if not loaded.
func (m *Manager[T]) GetWithDefault(def T) T {
	cfg := m.config.Load()
	if cfg == nil {
		return def
	}
	return *cfg
}

// Version returns the current configuration version.
func (m *Manager[T]) Version() uint64 {
	return m.version.Load()
}

// LoadedAt returns the timestamp when the configuration was last loaded.
func (m *Manager[T]) LoadedAt() time.Time {
	return time.Unix(m.loadedAt.Load(), 0)
}

// applyKeyTransformations applies namespace and filtering transformations to config data.
func (m *Manager[T]) applyKeyTransformations(data map[string]any) map[string]any {
	// apply filtering first
	if m.filterPrefix != "" {
		// extract nested value at the filter prefix
		prefixParts := strings.Split(strings.TrimSuffix(m.filterPrefix, "."), ".")

		current := any(data)
		for _, part := range prefixParts {
			if currentMap, ok := current.(map[string]any); ok {
				var exists bool
				current, exists = currentMap[part]
				if !exists {
					// prefix path doesn't exist, return empty
					return make(map[string]any)
				}
			} else {
				// not a map, can't navigate further
				return make(map[string]any)
			}
		}

		if m.stripPrefix {
			// if stripping prefix, return the nested value as the root
			if nestedMap, ok := current.(map[string]any); ok {
				data = nestedMap
			} else {
				// value at prefix is not a map
				return make(map[string]any)
			}
		} else {
			// if not stripping, rebuild with the prefix key
			filtered := make(map[string]any)
			setNestedValue(filtered, strings.TrimSuffix(m.filterPrefix, "."), current)
			data = filtered
		}
	}

	// apply namespace second
	if m.namespace != "" {
		// wrap entire data structure under the namespace key
		namespaced := make(map[string]any)
		setNestedValue(namespaced, m.namespace, data)
		data = namespaced
	}

	return data
}

// Snapshot returns a copy of the current configuration.
func (m *Manager[T]) Snapshot() T {
	cfg := m.config.Load()
	if cfg == nil {
		var zero T
		return zero
	}
	return *cfg
}

// IsLoaded returns true if the configuration has been loaded.
func (m *Manager[T]) IsLoaded() bool {
	return m.config.Load() != nil
}

func setNestedValue(data map[string]any, key string, value any) {
	parts := strings.Split(key, ".")

	// navigate to the deepest map, creating nested maps as needed
	current := data
	for i := 0; i < len(parts)-1; i++ {
		part := parts[i]

		// check if the key exists and is a map
		if existing, ok := current[part]; ok {
			if existingMap, ok := existing.(map[string]any); ok {
				current = existingMap
			} else {
				// key exists but is not a map, overwrite it
				newMap := make(map[string]any)
				current[part] = newMap
				current = newMap
			}
		} else {
			// key doesn't exist, create new map
			newMap := make(map[string]any)
			current[part] = newMap
			current = newMap
		}
	}

	// set the final value
	current[parts[len(parts)-1]] = value
}

func deepMerge(dst, src map[string]any) {
	for key, srcVal := range src {
		if dstVal, exists := dst[key]; exists {
			// both have this key, check if both are maps
			srcMap, srcIsMap := srcVal.(map[string]any)
			dstMap, dstIsMap := dstVal.(map[string]any)

			if srcIsMap && dstIsMap {
				// both are maps, recursively merge
				deepMerge(dstMap, srcMap)
			} else {
				// one or both are not maps, src overwrites dst
				dst[key] = srcVal
			}
		} else {
			// key doesn't exist in dst, add it
			dst[key] = srcVal
		}
	}
}
