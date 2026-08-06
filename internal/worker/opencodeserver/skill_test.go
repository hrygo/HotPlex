package opencodeserver

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker"
)

func TestConnSkillReplayRetainsNativeInvocation(t *testing.T) {
	t.Parallel()

	conn := &conn{}
	want := worker.NativeCommandInvocation{
		Name: "oracle-dba",
		Args: "10.102.78.1",
		Path: "/workspace/.agents/skills/oracle-dba/SKILL.md",
	}
	conn.setSkillReplay(want)

	got := conn.LastInputReplay()
	require.Equal(t, "/oracle-dba 10.102.78.1", got.Content)
	require.Equal(t, &want, got.Skill)
}

func TestServerCommanderListInvokableSkills(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		response any
		status   int
		want     []worker.SkillDescriptor
		wantErr  bool
	}{
		{
			name: "parses name-key entries with descriptions",
			response: []map[string]any{
				{"name": "oracle-dba", "description": "Inspect Oracle DB"},
				{"name": "k8s-debug"},
			},
			want: []worker.SkillDescriptor{
				{Name: "oracle-dba", Description: "Inspect Oracle DB"},
				{Name: "k8s-debug"},
			},
		},
		{
			name: "falls back to command-key entries",
			response: []map[string]any{
				{"command": "oracle-dba", "description": "Legacy command"},
				{"command": "k8s-debug"},
			},
			want: []worker.SkillDescriptor{
				{Name: "oracle-dba", Description: "Legacy command"},
				{Name: "k8s-debug"},
			},
		},
		{
			name:     "empty catalog",
			response: []map[string]any{},
			want:     []worker.SkillDescriptor{},
		},
		{
			name:     "HTTP error propagates, not empty catalog",
			status:   http.StatusUnauthorized,
			response: nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			requests := 0
			c, _ := newTestCommander(t, func(w http.ResponseWriter, r *http.Request) {
				requests++
				require.Equal(t, http.MethodGet, r.Method)
				require.Equal(t, "/command", r.URL.Path)
				if tt.status != 0 {
					w.WriteHeader(tt.status)
					w.Write([]byte("catalog denied"))
					return
				}
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(tt.response)
			})

			got, err := c.ListInvokableSkills(context.Background(), "/workspace")
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "opencode command catalog")
				require.Contains(t, err.Error(), "HTTP 401")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
			require.Equal(t, 1, requests, "GET /command must be called exactly once per query")
		})
	}
}

func TestServerCommanderListInvokableSkillsRejectsNonCommandFields(t *testing.T) {
	t.Parallel()

	c, _ := newTestCommander(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"name": "oracle-dba", "description": "Inspect Oracle DB", "path": "/tmp/skills/oracle-dba", "method": "POST"},
			{"command": "k8s-debug", "method": "POST"},
			{"path": "/tmp/skills/secret", "method": "POST", "permission": "bypass"},
			{"name": ""},
			{"description": "no name, no command"},
		})
	})

	got, err := c.ListInvokableSkills(context.Background(), "/workspace")
	require.NoError(t, err)
	require.Equal(t, []worker.SkillDescriptor{
		{Name: "oracle-dba", Description: "Inspect Oracle DB"},
		{Name: "k8s-debug"},
	}, got)
}

func TestWorkerListInvokableSkillsDelegates(t *testing.T) {
	t.Parallel()

	t.Run("delegates to commander", func(t *testing.T) {
		t.Parallel()
		requests := 0
		c, _ := newTestCommander(t, func(w http.ResponseWriter, r *http.Request) {
			requests++
			require.Equal(t, http.MethodGet, r.Method)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]map[string]any{{"name": "oracle-dba"}})
		})
		w := New()
		w.Mu.Lock()
		w.cmd = c
		w.Mu.Unlock()

		got, err := w.ListInvokableSkills(context.Background(), "/workspace")
		require.NoError(t, err)
		require.Equal(t, []worker.SkillDescriptor{{Name: "oracle-dba"}}, got)
		require.Equal(t, 1, requests)
	})

	t.Run("nil commander returns error", func(t *testing.T) {
		t.Parallel()
		w := New()
		_, err := w.ListInvokableSkills(context.Background(), "/workspace")
		require.Error(t, err)
		require.Contains(t, err.Error(), "worker not started")
	})
}

func TestServerCommanderListInvokableSkillsHonorsContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c, _ := newTestCommander(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called when context is cancelled")
		w.WriteHeader(http.StatusOK)
	})
	_, err := c.ListInvokableSkills(ctx, "/workspace")
	require.Error(t, err)
	require.Contains(t, err.Error(), "opencode command catalog")
}
