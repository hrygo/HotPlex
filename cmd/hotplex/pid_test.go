package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/cli/pidutil"
	"github.com/hrygo/hotplex/internal/service"
)

type gatewayDiscoveryServiceManager struct {
	statuses map[service.Level]*service.Status
}

func (m gatewayDiscoveryServiceManager) Install(service.InstallOptions) error  { return nil }
func (m gatewayDiscoveryServiceManager) Uninstall(string, service.Level) error { return nil }
func (m gatewayDiscoveryServiceManager) Status(_ string, level service.Level) (*service.Status, error) {
	return m.statuses[level], nil
}
func (m gatewayDiscoveryServiceManager) Start(string, service.Level) error   { return nil }
func (m gatewayDiscoveryServiceManager) Stop(string, service.Level) error    { return nil }
func (m gatewayDiscoveryServiceManager) Restart(string, service.Level) error { return nil }
func (m gatewayDiscoveryServiceManager) Logs(string, service.Level, bool, int) error {
	return nil
}

func TestGatewayStateWriteReadRemove(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	err := writeGatewayState("/test/config.yaml", true)
	require.NoError(t, err)

	pidPath := filepath.Join(tmpDir, ".hotplex", ".pids", "gateway.pid")
	_, err = os.ReadFile(pidPath)
	require.NoError(t, err)

	state, _ := readGatewayState()
	requireConfigPath(t, state)

	removeGatewayState()
	_, err = os.Stat(pidPath)
	require.True(t, os.IsNotExist(err))

	t.Setenv("HOME", origHome)
}

func requireConfigPath(t *testing.T, state *pidutil.GatewayState) {
	t.Helper()
	require.NotNil(t, state)
	require.Equal(t, os.Getpid(), state.PID)
	require.Equal(t, "/test/config.yaml", state.ConfigPath)
	require.True(t, state.DevMode)
}

func TestReadGatewayState_NoFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	_, err := readGatewayState()
	require.Error(t, err)
	require.Contains(t, err.Error(), "no PID file")
}

func TestReadGatewayState_StalePID(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	pidPath := filepath.Join(tmpDir, ".hotplex", ".pids", "gateway.pid")
	err := os.MkdirAll(filepath.Dir(pidPath), 0o755)
	require.NoError(t, err)

	err = os.WriteFile(pidPath, []byte(`{"pid":99999999}`), 0o644)
	require.NoError(t, err)

	_, err = readGatewayState()
	require.Error(t, err)
	require.Contains(t, err.Error(), "stale")
}

func TestFindRunningGateway_ServiceTakesPrecedenceOverPIDState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, writeGatewayState("/test/config.yaml", false))

	inst, err := findRunningGatewayWith(gatewayDiscoveryServiceManager{statuses: map[service.Level]*service.Status{
		service.LevelUser: {Level: service.LevelUser, Running: true, PID: 0},
	}})
	require.NoError(t, err)
	require.Equal(t, sourceService, inst.Source)
	require.Equal(t, service.LevelUser, inst.Level)
	require.Zero(t, inst.PID)
}

func TestFindRunningGateway_FallsBackToPIDWhenNoServiceIsRunning(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, writeGatewayState("/test/config.yaml", false))

	inst, err := findRunningGatewayWith(gatewayDiscoveryServiceManager{statuses: map[service.Level]*service.Status{
		service.LevelUser: {Level: service.LevelUser, Running: false},
	}})
	require.NoError(t, err)
	require.Equal(t, sourcePID, inst.Source)
	require.Equal(t, os.Getpid(), inst.PID)
}

func TestGatewayAlreadyRunningMessage_ServiceWithoutPIDIsActionable(t *testing.T) {
	err := gatewayAlreadyRunningError(&gatewayInstance{
		Source: sourceService,
		Level:  service.LevelUser,
	})
	require.EqualError(t, err, "gateway is managed by the user service, but no active process PID is available; use 'hotplex service stop --level user' first")
}

func TestGatewayStoppedMessage_ServiceWithoutPIDExplainsUnavailablePID(t *testing.T) {
	message := gatewayStoppedMessage(&gatewayInstance{
		Source: sourceService,
		Level:  service.LevelUser,
	})
	require.Equal(t, "gateway service stopped (level=user, process PID unavailable)", message)
}
