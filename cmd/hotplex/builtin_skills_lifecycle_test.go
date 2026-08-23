package main

import (
	"bytes"
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/cli/checkers"
	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/skills/reconcile"
)

func TestGatewayStartupStatusDoesNotWrite(t *testing.T) {
	t.Parallel()
	userHome := t.TempDir()
	hotplexHome := t.TempDir()
	runner := &lifecycleTestRunner{}
	cfg := &config.Config{Messaging: config.MessagingConfig{Slack: config.SlackConfig{
		MessagingPlatformConfig: config.MessagingPlatformConfig{Enabled: true, WorkerType: "claude_code"},
	}}}
	before := snapshotLifecycleTree(t, userHome, hotplexHome)

	err := runBuiltinSkillsStatus(context.Background(), cfg, userHome, hotplexHome,
		func(string, string) (reconcile.Runner, error) { return runner, nil },
		slog.New(slog.NewTextHandler(os.Stderr, nil)))
	require.NoError(t, err)
	require.Equal(t, 1, runner.statusCalls)
	require.Equal(t, before, snapshotLifecycleTree(t, userHome, hotplexHome))
	require.Zero(t, runner.syncCalls)
	require.Zero(t, runner.removeCalls)
}

func TestGatewayStartupStatusSkipsEmptyTargetsWithoutRunner(t *testing.T) {
	t.Parallel()
	called := false
	err := runBuiltinSkillsStatus(context.Background(), &config.Config{}, t.TempDir(), t.TempDir(),
		func(string, string) (reconcile.Runner, error) {
			called = true
			return &lifecycleTestRunner{}, nil
		}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	require.NoError(t, err)
	require.False(t, called)
}

func TestGatewayStartupStatusDoesNotLeakProjectionPaths(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	runner := &lifecycleTestRunner{statusReport: reconcile.Report{Items: []reconcile.Item{{
		Target:     "/private/native-root",
		BackupPath: "/private/backup",
		Outcome:    reconcile.OutcomeDrift,
		ReasonCode: reconcile.ReasonDrift,
	}}}}
	err := runBuiltinSkillsStatus(context.Background(), &config.Config{Messaging: config.MessagingConfig{Slack: config.SlackConfig{
		MessagingPlatformConfig: config.MessagingPlatformConfig{Enabled: true, WorkerType: "claude_code"},
	}}}, t.TempDir(), t.TempDir(),
		func(string, string) (reconcile.Runner, error) { return runner, nil },
		slog.New(slog.NewTextHandler(&logs, nil)))
	require.ErrorIs(t, err, reconcile.ErrReportActionRequired)
	require.Contains(t, logs.String(), reconcile.ReasonDrift)
	require.NotContains(t, logs.String(), "/private/native-root")
	require.NotContains(t, logs.String(), "/private/backup")
}

func TestDoctorSkillsStatusDoesNotWrite(t *testing.T) {
	t.Parallel()
	checker := checkers.NewBuiltinSkillsChecker(func(context.Context) (reconcile.Report, error) {
		return reconcile.Report{}, nil
	})
	diagnostic := checker.Check(context.Background())
	require.Nil(t, diagnostic.FixFunc)
}

type lifecycleTestRunner struct {
	statusCalls  int
	syncCalls    int
	removeCalls  int
	syncOptions  reconcile.Options
	statusReport reconcile.Report
	syncReport   reconcile.Report
}

func (r *lifecycleTestRunner) Status(_ context.Context, options reconcile.Options) (reconcile.Report, error) {
	r.statusCalls++
	return r.statusReport, nil
}

func (r *lifecycleTestRunner) Sync(_ context.Context, options reconcile.Options) (reconcile.Report, error) {
	r.syncCalls++
	r.syncOptions = options
	if r.syncReport.Profile != "" || len(r.syncReport.Items) > 0 {
		return r.syncReport, nil
	}
	return reconcile.Report{Profile: options.Profile}, nil
}

func (r *lifecycleTestRunner) Remove(_ context.Context, _ reconcile.Options) (reconcile.Report, error) {
	r.removeCalls++
	return reconcile.Report{}, nil
}

func snapshotLifecycleTree(t *testing.T, roots ...string) map[string]string {
	t.Helper()
	result := make(map[string]string)
	for index, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			key := filepath.Join(string(rune('0'+index)), rel)
			if entry.IsDir() {
				result[key] = "dir"
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			result[key] = string(data)
			return nil
		})
		require.NoError(t, err)
	}
	return result
}
