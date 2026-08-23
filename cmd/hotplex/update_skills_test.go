package main

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/skills/builtin"
	"github.com/hrygo/hotplex/internal/skills/reconcile"
)

func TestUpdateDoesNotSyncWithoutExplicitFlag(t *testing.T) {
	t.Parallel()
	called := false
	deps := updateSkillsDeps{
		LoadConfig: func(string) (*config.Config, error) {
			called = true
			return &config.Config{}, nil
		},
		NewRunner: func(string, string) (reconcile.Runner, error) {
			called = true
			return nil, errors.New("runner must not be built")
		},
	}

	// The command-level gate must leave the reconciliation seam untouched when
	// the user did not explicitly request synchronization.
	report, err := maybeSyncSkillsAfterUpdate(context.Background(), false, false, "", builtin.ProfileRuntime, deps)
	require.NoError(t, err)
	require.Empty(t, report.Items)
	require.False(t, called)
}

func TestUpdateExplicitSyncUsesSelectedProfile(t *testing.T) {
	t.Parallel()
	runner := &lifecycleTestRunner{}
	deps := updateSkillsDeps{
		LoadConfig: func(string) (*config.Config, error) {
			return &config.Config{Messaging: config.MessagingConfig{Slack: config.SlackConfig{
				MessagingPlatformConfig: config.MessagingPlatformConfig{Enabled: true, WorkerType: "claude_code"},
			}}}, nil
		},
		UserHomeDir:    func() (string, error) { return t.TempDir(), nil },
		HotplexHomeDir: func() string { return t.TempDir() },
		NewRunner: func(string, string) (reconcile.Runner, error) {
			return runner, nil
		},
	}

	report, err := maybeSyncSkillsAfterUpdate(context.Background(), true, true, "", builtin.ProfileOperator, deps)
	require.NoError(t, err)
	require.Equal(t, 1, runner.syncCalls)
	require.Equal(t, builtin.ProfileOperator, runner.syncOptions.Profile)
	require.Equal(t, []reconcile.WorkerType{reconcile.WorkerClaude}, runner.syncOptions.WorkerTypes)
	require.Equal(t, builtin.ProfileOperator, report.Profile)
}

func TestUpdateExplicitProfileWithoutSyncIsUsageError(t *testing.T) {
	t.Parallel()
	cmd := newUpdateCmd()
	cmd.SetArgs([]string{"--skills-profile", "operator"})
	err := cmd.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "--sync-skills")
}

func TestUpdateSyncFailureReturnsBoundedReportError(t *testing.T) {
	t.Parallel()
	runner := &lifecycleTestRunner{statusReport: reconcile.Report{}}
	// Reuse the typed lifecycle runner's sync result to model a projection drift
	// after the binary has already been replaced.
	runner.syncReport = reconcile.Report{Profile: builtin.ProfileRuntime, Items: []reconcile.Item{{
		Target:     "/private/native-root",
		Outcome:    reconcile.OutcomeDrift,
		ReasonCode: reconcile.ReasonDrift,
	}}}
	deps := updateSkillsDeps{
		LoadConfig: func(string) (*config.Config, error) {
			return &config.Config{Messaging: config.MessagingConfig{Slack: config.SlackConfig{
				MessagingPlatformConfig: config.MessagingPlatformConfig{Enabled: true, WorkerType: "claude_code"},
			}}}, nil
		},
		UserHomeDir:    func() (string, error) { return t.TempDir(), nil },
		HotplexHomeDir: func() string { return t.TempDir() },
		NewRunner: func(string, string) (reconcile.Runner, error) {
			return runner, nil
		},
	}
	report, err := maybeSyncSkillsAfterUpdate(context.Background(), true, false, "", builtin.ProfileRuntime, deps)
	require.ErrorIs(t, err, reconcile.ErrReportActionRequired)
	require.Equal(t, runner.syncReport, report)
}
