package maps

import "sync"

type Map[K comparable, V any] struct {
	m sync.Map
}

// using new is optional
func New[K comparable, V any]() *Map[K, V] {
	return &Map[K, V]{}
}

func (m *Map[K, V]) Load(key K) (value V, ok bool) {
	v, ok := m.m.Load(key)
	if !ok {
		var zero V
		return zero, false
	}
	return mapValue[V](v), true
}

func (m *Map[K, V]) Store(key K, value V) {
	m.m.Store(key, value)
}

func (m *Map[K, V]) Delete(key K) {
	m.m.Delete(key)
}

func (m *Map[K, V]) LoadOrStore(key K, value V) (actual V, loaded bool) {
	v, loaded := m.m.LoadOrStore(key, value)
	return mapValue[V](v), loaded
}

func (m *Map[K, V]) LoadAndDelete(key K) (value V, loaded bool) {
	v, loaded := m.m.LoadAndDelete(key)
	if !loaded {
		var zero V
		return zero, false
	}
	return mapValue[V](v), true
}

func (m *Map[K, V]) Swap(key K, value V) (previous V, loaded bool) {
	v, loaded := m.m.Swap(key, value)
	if !loaded {
		var zero V
		return zero, false
	}
	return mapValue[V](v), true
}

func (m *Map[K, V]) CompareAndSwap(key K, old, replacement V) bool {
	return m.m.CompareAndSwap(key, old, replacement)
}

func (m *Map[K, V]) CompareAndDelete(key K, old V) bool {
	return m.m.CompareAndDelete(key, old)
}

func (m *Map[K, V]) Range(f func(key K, value V) bool) {
	m.m.Range(func(k, v any) bool {
		return f(mapValue[K](k), mapValue[V](v))
	})
}

func (m *Map[K, V]) Clear() {
	m.m.Clear()
}

func mapValue[T any](v any) T {
	if v == nil {
		var zero T
		return zero
	}
	value, ok := v.(T)
	if !ok {
		panic("maps: stored value violates Map type parameters")
	}
	return value
}
