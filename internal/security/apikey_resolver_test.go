package security

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMapResolver(t *testing.T) {
	t.Parallel()

	t.Run("nil data returns false", func(t *testing.T) {
		t.Parallel()
		r := NewMapResolver(nil)
		_, ok := r.Resolve(context.Background(), "any-key")
		require.False(t, ok)
	})

	t.Run("empty data returns false", func(t *testing.T) {
		t.Parallel()
		r := NewMapResolver(map[string]string{})
		_, ok := r.Resolve(context.Background(), "any-key")
		require.False(t, ok)
	})

	t.Run("resolve known key", func(t *testing.T) {
		t.Parallel()
		r := NewMapResolver(map[string]string{
			"sk-alice": "alice",
			"sk-bob":   "bob",
		})
		uid, ok := r.Resolve(context.Background(), "sk-alice")
		require.True(t, ok)
		require.Equal(t, "alice", uid)

		uid, ok = r.Resolve(context.Background(), "sk-bob")
		require.True(t, ok)
		require.Equal(t, "bob", uid)
	})

	t.Run("resolve unknown key returns false", func(t *testing.T) {
		t.Parallel()
		r := NewMapResolver(map[string]string{"sk-alice": "alice"})
		_, ok := r.Resolve(context.Background(), "sk-unknown")
		require.False(t, ok)
	})

	t.Run("update replaces mapping", func(t *testing.T) {
		t.Parallel()
		r := NewMapResolver(map[string]string{"sk-old": "old-user"})
		r.Update(map[string]string{"sk-new": "new-user"})

		_, ok := r.Resolve(context.Background(), "sk-old")
		require.False(t, ok)

		uid, ok := r.Resolve(context.Background(), "sk-new")
		require.True(t, ok)
		require.Equal(t, "new-user", uid)
	})

	t.Run("update with nil clears mapping", func(t *testing.T) {
		t.Parallel()
		r := NewMapResolver(map[string]string{"sk-alice": "alice"})
		r.Update(nil)
		_, ok := r.Resolve(context.Background(), "sk-alice")
		require.False(t, ok)
	})
}

func TestChainResolver(t *testing.T) {
	t.Parallel()

	t.Run("empty chain returns false", func(t *testing.T) {
		t.Parallel()
		r := NewChainResolver()
		_, ok := r.Resolve(context.Background(), "any-key")
		require.False(t, ok)
	})

	t.Run("nil resolvers are filtered", func(t *testing.T) {
		t.Parallel()
		r := NewChainResolver(nil, NewMapResolver(map[string]string{"k": "v"}), nil)
		uid, ok := r.Resolve(context.Background(), "k")
		require.True(t, ok)
		require.Equal(t, "v", uid)
	})

	t.Run("first match wins", func(t *testing.T) {
		t.Parallel()
		first := NewMapResolver(map[string]string{"k": "from-first"})
		second := NewMapResolver(map[string]string{"k": "from-second"})
		r := NewChainResolver(first, second)

		uid, ok := r.Resolve(context.Background(), "k")
		require.True(t, ok)
		require.Equal(t, "from-first", uid)
	})

	t.Run("fallback to second resolver", func(t *testing.T) {
		t.Parallel()
		first := NewMapResolver(map[string]string{"a": "alice"})
		second := NewMapResolver(map[string]string{"b": "bob"})
		r := NewChainResolver(first, second)

		uid, ok := r.Resolve(context.Background(), "b")
		require.True(t, ok)
		require.Equal(t, "bob", uid)
	})

	t.Run("no match in chain", func(t *testing.T) {
		t.Parallel()
		r := NewChainResolver(
			NewMapResolver(map[string]string{"a": "alice"}),
			NewMapResolver(map[string]string{"b": "bob"}),
		)
		_, ok := r.Resolve(context.Background(), "unknown")
		require.False(t, ok)
	})
}
