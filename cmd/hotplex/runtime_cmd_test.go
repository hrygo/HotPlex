package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
)

func newFenceTestServer(t *testing.T, handler http.HandlerFunc) *fenceAdminClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &fenceAdminClient{baseURL: srv.URL, token: "test-token", http: srv.Client()}
}

func TestFenceAdminClient_ListFences(t *testing.T) {
	t.Parallel()
	client := newFenceTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/admin/executions/fences", r.URL.Path)
		require.Equal(t, "sess-1", r.URL.Query().Get("session_id"))
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"fences":[{"execution_id":"exec-1","session_id":"sess-1","runtime_status":"unknown","fence_reason":"GATEWAY_LEASE_EXPIRED","fence_version":3}],"limit":100,"offset":0}`))
	})

	out, err := client.listFences(context.Background(), "sess-1", 0)
	require.NoError(t, err)
	require.Len(t, out.Fences, 1)
	require.Equal(t, "exec-1", out.Fences[0].ExecutionID)
	require.Equal(t, int64(3), out.Fences[0].FenceVersion)
}

func TestFenceAdminClient_FenceActionResolve(t *testing.T) {
	t.Parallel()
	client := newFenceTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/admin/executions/exec-1/fence-action", r.URL.Path)
		var body struct {
			Decision             string `json:"decision"`
			ExpectedFenceVersion int64  `json:"expected_fence_version"`
			Reason               string `json:"reason"`
			EvidenceRef          string `json:"evidence_ref"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "resolve", body.Decision)
		require.Equal(t, int64(3), body.ExpectedFenceVersion)
		require.Equal(t, "operator checked", body.Reason)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"decision":"resolve","execution":{"execution_id":"exec-1","session_id":"sess-1","runtime_status":"unknown","fence_reason":"","fence_version":3}}`))
	})

	updated, err := client.fenceAction(context.Background(), "exec-1", "resolve", 3, "operator checked", "")
	require.NoError(t, err)
	require.Equal(t, "exec-1", updated.ExecutionID)
	require.Empty(t, updated.FenceReason)
}

func TestFenceAdminClient_ConflictDirectsReinspection(t *testing.T) {
	t.Parallel()
	client := newFenceTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":"FENCE_CONFLICT","message":"fence version conflict"}`))
	})

	_, err := client.fenceAction(context.Background(), "exec-1", "abandon", 2, "r", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "FENCE_CONFLICT")
	require.Contains(t, err.Error(), "re-inspect", "409 must direct re-inspection, not silent retry")
	require.Contains(t, err.Error(), "no automatic retry")
}

func TestFenceAdminClient_NotFound(t *testing.T) {
	t.Parallel()
	client := newFenceTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"FENCE_NOT_FOUND","message":"execution not found"}`))
	})

	_, err := client.fenceAction(context.Background(), "missing", "resolve", 1, "r", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "FENCE_NOT_FOUND")
}

func TestNewFenceAdminClient_UsesAdminAddress(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	writeFenceClientConfig(t, configPath, "localhost:8888", "localhost:9999", "test-token")

	client, err := newFenceAdminClient(configPath)
	require.NoError(t, err)
	require.Equal(t, "http://localhost:9999", client.baseURL)
}

func TestNewFenceAdminClient_UsesRunningConfigPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HOTPLEX_HOME", filepath.Join(t.TempDir(), "hotplex"))

	runningConfigPath := filepath.Join(t.TempDir(), "running.yaml")
	writeFenceClientConfig(t, runningConfigPath, "localhost:8888", "localhost:19999", "running-token")
	writeGatewayState(runningConfigPath, false)
	t.Cleanup(removeGatewayState)

	client, err := newFenceAdminClient("")
	require.NoError(t, err)
	require.Equal(t, "http://localhost:19999", client.baseURL)
	require.Equal(t, "running-token", client.token)
}

func TestNewFenceAdminClient_DefaultFallbackUnsetHome(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HOTPLEX_HOME", "")

	runningConfigPath := filepath.Join(t.TempDir(), "running.yaml")
	writeFenceClientConfig(t, runningConfigPath, "localhost:8888", "localhost:19997", "running-token")
	writeGatewayState(runningConfigPath, false)
	t.Cleanup(removeGatewayState)

	client, err := newFenceAdminClient("")
	require.NoError(t, err)
	require.Equal(t, "http://localhost:19997", client.baseURL)
	require.Equal(t, "running-token", client.token)
}

func TestNewFenceAdminClient_ExplicitDefaultPathNotReplaced(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HOTPLEX_HOME", filepath.Join(t.TempDir(), "hotplex"))

	runningConfigPath := filepath.Join(t.TempDir(), "running.yaml")
	explicitPath := config.DefaultConfigPath() // exactly the current default absolute path
	require.NoError(t, os.MkdirAll(filepath.Dir(explicitPath), 0o755))
	writeFenceClientConfig(t, runningConfigPath, "localhost:8888", "localhost:19996", "running-token")
	writeFenceClientConfig(t, explicitPath, "localhost:8888", "localhost:29996", "explicit-token")
	writeGatewayState(runningConfigPath, false)
	t.Cleanup(removeGatewayState)

	client, err := newFenceAdminClient(explicitPath)
	require.NoError(t, err)
	require.Equal(t, "http://localhost:29996", client.baseURL)
	require.Equal(t, "explicit-token", client.token)
}

func TestNewFenceAdminClient_ExplicitConfigPathWins(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HOTPLEX_HOME", filepath.Join(t.TempDir(), "hotplex"))

	runningConfigPath := filepath.Join(t.TempDir(), "running.yaml")
	explicitConfigPath := filepath.Join(t.TempDir(), "explicit.yaml")
	writeFenceClientConfig(t, runningConfigPath, "localhost:8888", "localhost:19999", "running-token")
	writeFenceClientConfig(t, explicitConfigPath, "localhost:8888", "localhost:29999", "explicit-token")
	writeGatewayState(runningConfigPath, false)
	t.Cleanup(removeGatewayState)

	client, err := newFenceAdminClient(explicitConfigPath)
	require.NoError(t, err)
	require.Equal(t, "http://localhost:29999", client.baseURL)
	require.Equal(t, "explicit-token", client.token)
}

func writeFenceClientConfig(t *testing.T, path, gatewayAddr, adminAddr, token string) {
	t.Helper()
	content := fmt.Sprintf("gateway:\n  addr: %q\nadmin:\n  addr: %q\n  tokens: [%q]\n", gatewayAddr, adminAddr, token)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func TestFenceActionCmd_RequiresConfirm(t *testing.T) {
	t.Parallel()
	cmd := newFencesActionCmd("resolve")
	cmd.SetArgs([]string{"exec-1", "--fence-version", "3", "--reason", "r"})

	err := cmd.Execute()

	require.Error(t, err)
	require.Contains(t, err.Error(), "--confirm", "irreversible decisions must require explicit confirmation")
}

func TestFenceActionCmd_RequiresReason(t *testing.T) {
	t.Parallel()
	cmd := newFencesActionCmd("abandon")
	cmd.SetArgs([]string{"exec-1", "--fence-version", "3", "--confirm"})

	err := cmd.Execute()

	require.Error(t, err)
	require.Contains(t, err.Error(), "--reason")
}

func TestFenceActionCmd_RequiresFenceVersion(t *testing.T) {
	t.Parallel()
	cmd := newFencesActionCmd("resolve")
	cmd.SetArgs([]string{"exec-1", "--reason", "r", "--confirm"})

	err := cmd.Execute()

	require.Error(t, err, "missing --fence-version (default -1) must fail before any network call")
	require.Contains(t, err.Error(), "--fence-version")
}
