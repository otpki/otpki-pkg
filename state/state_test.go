package state_test

import (
	"testing"
	"time"

	"github.com/otpki/otpki-pkg/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoreContract(t *testing.T) {
	t.Parallel()

	// run in memory state store tests
	factory := func() state.Store[string, string] {
		return state.NewMemoryStateStore[string](time.Millisecond*5, nil)
	}
	runStoreContractTests(t, factory)

	t.Run("PutWithKey accepts integer keys", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()

		s := state.NewMemoryStateStoreWithKey[int, string](time.Millisecond*5, func() int { return 1 }, nil)

		intKey := 12345
		require.NoError(t, s.PutWithKey(ctx, intKey, "with-int-key", 250*time.Millisecond, 0))

		got, err := s.Get(ctx, intKey)
		require.NoError(t, err)
		assert.Equal(t, "with-int-key", got)

		require.NoError(t, s.Delete(ctx, intKey))
		_, err = s.Get(ctx, intKey)
		require.Error(t, err)
	})

	// todo: use future valkey client test helper to test against valkey
}

func runStoreContractTests[V comparable](t *testing.T, newStore func() state.Store[string, V]) {
	t.Helper()
	ctx := t.Context()

	t.Run("Put/Get returns stored data", func(t *testing.T) {
		t.Parallel()
		s := newStore()

		want := anyToT[V](t, "hello")
		key, err := s.Put(ctx, want, 250*time.Millisecond, 0)
		require.NoError(t, err)
		require.NotEmpty(t, key)

		got, err := s.Get(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	})

	t.Run("Delete removes key", func(t *testing.T) {
		t.Parallel()
		s := newStore()

		key, err := s.Put(ctx, anyToT[V](t, "bye"), 250*time.Millisecond, 0)
		require.NoError(t, err)
		require.NotEmpty(t, key)

		require.NoError(t, s.Delete(ctx, key))

		_, err = s.Get(ctx, key)
		require.Error(t, err)
	})

	t.Run("PeekMultiple returns data for provided keys in order", func(t *testing.T) {
		t.Parallel()
		s := newStore()

		first, err := s.Put(ctx, anyToT[V](t, "first"), 250*time.Millisecond, 0)
		require.NoError(t, err)
		second, err := s.Put(ctx, anyToT[V](t, "second"), 250*time.Millisecond, 0)
		require.NoError(t, err)

		got, err := s.PeekMultiple(ctx, []string{second, first})
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, anyToT[V](t, "second"), got[0])
		assert.Equal(t, anyToT[V](t, "first"), got[1])
	})

	t.Run("Peek does not renew session", func(t *testing.T) {
		t.Parallel()
		s := newStore()

		ttl := 80 * time.Millisecond
		key, err := s.Put(ctx, anyToT[V](t, "peek"), ttl, 0)
		require.NoError(t, err)

		// pass >50% of TTL, but use Peek so no renewal should happen.
		time.Sleep(50 * time.Millisecond)
		_, err = s.Peek(ctx, key)
		require.NoError(t, err)

		// if Peek renewed, this would still be alive.
		time.Sleep(40 * time.Millisecond)
		_, err = s.Get(ctx, key)
		require.Error(t, err)
	})

	t.Run("TTL expiry causes Get to error", func(t *testing.T) {
		t.Parallel()
		s := newStore()

		key, err := s.Put(ctx, anyToT[V](t, "short"), 40*time.Millisecond, 0)
		require.NoError(t, err)
		require.NotEmpty(t, key)

		time.Sleep(60 * time.Millisecond)
		_, err = s.Get(ctx, key)
		require.Error(t, err)
	})

	t.Run("TTL=0 means no TTL expiry (no maxTTL)", func(t *testing.T) {
		t.Parallel()
		s := newStore()

		want := anyToT[V](t, "forever")
		key, err := s.Put(ctx, want, 0, 0)
		require.NoError(t, err)
		require.NotEmpty(t, key)

		// wait long enough that a broken implementation (ExpiresAt=now) would fail.
		time.Sleep(50 * time.Millisecond)

		got, err := s.Get(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	})

	t.Run("TTL=0 with maxTTL expires at maxTTL", func(t *testing.T) {
		t.Parallel()
		s := newStore()

		key, err := s.Put(ctx, anyToT[V](t, "maxed"), 0, 40*time.Millisecond)
		require.NoError(t, err)
		require.NotEmpty(t, key)

		time.Sleep(60 * time.Millisecond)
		_, err = s.Get(ctx, key)
		require.Error(t, err)
	})

	t.Run("sliding renewal extends session when >50% TTL elapsed", func(t *testing.T) {
		t.Parallel()
		s := newStore()

		ttl := 80 * time.Millisecond
		key, err := s.Put(ctx, anyToT[V](t, "slide"), ttl, 0)
		require.NoError(t, err)
		require.NotEmpty(t, key)

		// pass > 50% of TTL, trigger renewal on Get.
		time.Sleep(50 * time.Millisecond)

		_, err = s.Get(ctx, key)
		require.NoError(t, err)

		// if it didn't renew, the original TTL would expire around ~80ms after Put,
		// so sleeping a bit past that should fail. renewal should keep it alive.
		time.Sleep(40 * time.Millisecond) // total ~90ms since Put

		_, err = s.Get(ctx, key)
		require.NoError(t, err)
	})

	t.Run("sliding renewal does not extend beyond maxTTL", func(t *testing.T) {
		t.Parallel()
		s := newStore()

		ttl := 80 * time.Millisecond
		maxTTL := 120 * time.Millisecond
		key, err := s.Put(ctx, anyToT[V](t, "cap"), ttl, maxTTL)
		require.NoError(t, err)
		require.NotEmpty(t, key)

		// trigger a renewal attempt (>50% of TTL).
		time.Sleep(50 * time.Millisecond)
		_, err = s.Get(ctx, key)
		require.NoError(t, err)

		// wait past maxTTL; session must be gone regardless of renewals.
		time.Sleep(90 * time.Millisecond) // total ~140ms since Put

		_, err = s.Get(ctx, key)
		require.Error(t, err)
	})
}

func anyToT[T comparable](t *testing.T, v any) T {
	t.Helper()
	typed, ok := v.(T)
	require.Truef(t, ok, "test uses T=%T but tried to assign value of type %T", *new(T), v)
	return typed
}
