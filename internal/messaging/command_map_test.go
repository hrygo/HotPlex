package messaging

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

func TestCommandMap_Lookup(t *testing.T) {
	t.Parallel()

	cm := NewCommandMap(
		map[string]string{
			"/gc":    "gc",
			"/reset": "reset",
		},
		map[string]string{
			"$gc":    "gc",
			"$休眠":    "gc",
			"$reset": "reset",
		},
	)

	tests := []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		{"slash exact match", "/gc", "gc", true},
		{"slash exact match reset", "/reset", "reset", true},
		{"natural exact match", "$gc", "gc", true},
		{"natural cjk match", "$休眠", "gc", true},
		{"natural reset", "$reset", "reset", true},
		{"not found", "hello", "", false},
		{"empty string", "", "", false},
		{"slash not in natural", "/gc", "gc", true}, // slash takes priority
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := cm.Lookup(tt.input)
			require.Equal(t, tt.ok, ok)
			if tt.ok {
				require.Equal(t, tt.want, got)
			}
		})
	}
}

func TestCommandMap_LookupSlash(t *testing.T) {
	t.Parallel()

	cm := NewCommandMap(
		map[string]string{"/gc": "gc"},
		map[string]string{"$gc": "gc"},
	)

	tests := []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		{"found", "/gc", "gc", true},
		{"not in slash map", "$gc", "", false},
		{"not found", "/reset", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := cm.LookupSlash(tt.input)
			require.Equal(t, tt.ok, ok)
			if tt.ok {
				require.Equal(t, tt.want, got)
			}
		})
	}
}

func TestCommandMap_LookupNatural(t *testing.T) {
	t.Parallel()

	cm := NewCommandMap(
		map[string]string{"/gc": "gc"},
		map[string]string{"$gc": "gc", "$休眠": "gc"},
	)

	tests := []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		{"found", "$gc", "gc", true},
		{"cjk found", "$休眠", "gc", true},
		{"not in natural map", "/gc", "", false},
		{"not found", "$reset", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := cm.LookupNatural(tt.input)
			require.Equal(t, tt.ok, ok)
			if tt.ok {
				require.Equal(t, tt.want, got)
			}
		})
	}
}

func TestCommandMap_Empty(t *testing.T) {
	t.Parallel()

	cm := NewCommandMap[string](nil, nil)

	_, ok := cm.Lookup("anything")
	require.False(t, ok)

	_, ok = cm.LookupSlash("anything")
	require.False(t, ok)

	_, ok = cm.LookupNatural("anything")
	require.False(t, ok)
}

func TestCommandMap_SlashPriority(t *testing.T) {
	t.Parallel()

	// When same key exists in both maps, Lookup returns slash first.
	cm := NewCommandMap(
		map[string]string{"/x": "slash"},
		map[string]string{"/x": "natural"},
	)

	got, ok := cm.Lookup("/x")
	require.True(t, ok)
	require.Equal(t, "slash", got)

	got, ok = cm.LookupNatural("/x")
	require.True(t, ok)
	require.Equal(t, "natural", got)
}

func TestControlCommands_Integration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  events.ControlAction
		ok    bool
	}{
		{"/gc", events.ControlActionGC, true},
		{"/reset", events.ControlActionReset, true},
		{"/park", events.ControlActionGC, true},
		{"/new", events.ControlActionReset, true},
		{"hello", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			result := ParseControlCommand(tt.input)
			if !tt.ok {
				require.Nil(t, result)
				return
			}
			require.NotNil(t, result)
			require.Equal(t, tt.want, result.Action)
		})
	}
}
