package maps

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMapNilInterfaceValue(t *testing.T) {
	m := New[string, any]()
	m.Store("nil", nil)
	value, ok := m.Load("nil")
	require.True(t, ok)
	require.Nil(t, value)
	value, loaded := m.LoadOrStore("nil", "replacement")
	require.True(t, loaded)
	require.Nil(t, value)
	visited := false
	m.Range(func(key string, value any) bool {
		require.Equal(t, "nil", key)
		require.Nil(t, value)
		visited = true
		return true
	})
	require.True(t, visited)
	value, loaded = m.Swap("nil", nil)
	require.True(t, loaded)
	require.Nil(t, value)
	value, loaded = m.LoadAndDelete("nil")
	require.True(t, loaded)
	require.Nil(t, value)
	_, ok = m.Load("nil")
	require.False(t, ok)
}
