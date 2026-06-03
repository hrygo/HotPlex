package groupchat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRoundRobinSelector_Next(t *testing.T) {
	t.Parallel()

	s := &RoundRobinSelector{}

	t.Run("empty participants", func(t *testing.T) {
		require.Equal(t, "", s.Next(1, nil))
		require.Equal(t, "", s.Next(1, []string{}))
	})

	t.Run("single participant", func(t *testing.T) {
		p := []string{"bot-a"}
		require.Equal(t, "bot-a", s.Next(1, p))
		require.Equal(t, "bot-a", s.Next(2, p))
		require.Equal(t, "bot-a", s.Next(100, p))
	})

	t.Run("round-robin with two bots", func(t *testing.T) {
		p := []string{"alice", "bob"}
		require.Equal(t, "alice", s.Next(1, p))
		require.Equal(t, "bob", s.Next(2, p))
		require.Equal(t, "alice", s.Next(3, p))
		require.Equal(t, "bob", s.Next(4, p))
	})

	t.Run("round-robin with three bots", func(t *testing.T) {
		p := []string{"a", "b", "c"}
		require.Equal(t, "a", s.Next(1, p))
		require.Equal(t, "b", s.Next(2, p))
		require.Equal(t, "c", s.Next(3, p))
		require.Equal(t, "a", s.Next(4, p))
		require.Equal(t, "b", s.Next(5, p))
		require.Equal(t, "c", s.Next(6, p))
	})
}

func TestRemoveFromParticipants(t *testing.T) {
	t.Parallel()

	t.Run("remove existing", func(t *testing.T) {
		p := []string{"a", "b", "c"}
		result := RemoveFromParticipants(p, "b")
		require.Equal(t, []string{"a", "c"}, result)
	})

	t.Run("remove first", func(t *testing.T) {
		p := []string{"a", "b", "c"}
		result := RemoveFromParticipants(p, "a")
		require.Equal(t, []string{"b", "c"}, result)
	})

	t.Run("remove last", func(t *testing.T) {
		p := []string{"a", "b", "c"}
		result := RemoveFromParticipants(p, "c")
		require.Equal(t, []string{"a", "b"}, result)
	})

	t.Run("remove not found returns original", func(t *testing.T) {
		p := []string{"a", "b", "c"}
		result := RemoveFromParticipants(p, "z")
		require.Equal(t, []string{"a", "b", "c"}, result)
	})

	t.Run("remove from single element", func(t *testing.T) {
		p := []string{"only"}
		result := RemoveFromParticipants(p, "only")
		require.Equal(t, []string{}, result)
	})
}
