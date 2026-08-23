package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/skills/reconcile"
)

type fakeSkillsRunner struct {
	statusReport reconcile.Report
	syncReport   reconcile.Report
	removeReport reconcile.Report
	statusErr    error
	syncErr      error
	removeErr    error
	statusCalls  int
	syncCalls    int
	removeCalls  int
	statusOpts   reconcile.Options
	syncOpts     reconcile.Options
	removeOpts   reconcile.Options
}

func (f *fakeSkillsRunner) Status(_ context.Context, options reconcile.Options) (reconcile.Report, error) {
	f.statusCalls++
	f.statusOpts = options
	return f.statusReport, f.statusErr
}

func (f *fakeSkillsRunner) Sync(_ context.Context, options reconcile.Options) (reconcile.Report, error) {
	f.syncCalls++
	f.syncOpts = options
	return f.syncReport, f.syncErr
}

func (f *fakeSkillsRunner) Remove(_ context.Context, options reconcile.Options) (reconcile.Report, error) {
	f.removeCalls++
	f.removeOpts = options
	return f.removeReport, f.removeErr
}

func newTestSkillsDeps(t *testing.T, runner reconcile.Runner, cfg *config.Config) (skillsCommandDeps, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	userHome, hotplexHome := t.TempDir(), t.TempDir()
	stdout, stderr := new(bytes.Buffer), new(bytes.Buffer)
	deps := skillsCommandDeps{
		LoadConfig:     func(string) (*config.Config, error) { return cfg, nil },
		UserHomeDir:    func() (string, error) { return userHome, nil },
		HotplexHomeDir: func() string { return hotplexHome },
		NewRunner: func(gotUserHome, gotHotplexHome string) (reconcile.Runner, error) {
			require.Equal(t, userHome, gotUserHome)
			require.Equal(t, hotplexHome, gotHotplexHome)
			return runner, nil
		},
		Output:    stdout,
		ErrOutput: stderr,
	}
	return deps, stdout, stderr
}

func executeSkills(t *testing.T, deps skillsCommandDeps, args ...string) (stdout, stderr *bytes.Buffer, err error) {
	t.Helper()
	cmd := newSkillsCmdWithDeps(deps)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return deps.Output.(*bytes.Buffer), deps.ErrOutput.(*bytes.Buffer), err
}

func TestSkillsCommandsExposeClosedSubcommandsAndFlags(t *testing.T) {
	deps, _, _ := newTestSkillsDeps(t, &fakeSkillsRunner{}, &config.Config{})
	cmd := newSkillsCmdWithDeps(deps)
	require.Equal(t, "skills", cmd.Use)
	require.ElementsMatch(t, []string{"status", "sync", "remove"}, commandUses(cmd.Commands()))

	status, _, err := cmd.Find([]string{"status"})
	require.NoError(t, err)
	require.NotNil(t, status.Flag("profile"))
	require.NotNil(t, status.Flag("worker"))
	require.NotNil(t, status.Flag("json"))
	require.NotNil(t, status.Flag("config"))

	sync, _, err := cmd.Find([]string{"sync"})
	require.NoError(t, err)
	require.NotNil(t, sync.Flag("dry-run"))
	require.NotNil(t, sync.Flag("json"))

	remove, _, err := cmd.Find([]string{"remove"})
	require.NoError(t, err)
	require.NotNil(t, remove.Flag("dry-run"))
	require.NotNil(t, remove.Flag("json"))

	root := newRootCmd()
	registered, _, err := root.Find([]string{"skills"})
	require.NoError(t, err)
	require.Equal(t, "skills", registered.Name())
}

func TestSkillsSyncUsesResolvedConfigTargetsWhenWorkerFlagIsAbsent(t *testing.T) {
	runner := &fakeSkillsRunner{}
	cfg := &config.Config{Messaging: config.MessagingConfig{
		Slack: config.SlackConfig{MessagingPlatformConfig: config.MessagingPlatformConfig{Enabled: true, WorkerType: "codex_cli"}},
	}}
	deps, _, _ := newTestSkillsDeps(t, runner, cfg)
	_, _, err := executeSkills(t, deps, "sync")
	require.NoError(t, err)
	require.Equal(t, 1, runner.syncCalls)
	require.Equal(t, []reconcile.WorkerType{reconcile.WorkerCodex}, runner.syncOpts.WorkerTypes)
	require.Equal(t, "runtime", string(runner.syncOpts.Profile))
}

func TestSkillsSyncWithExplicitWorkerSkipsConfigLoad(t *testing.T) {
	runner := &fakeSkillsRunner{}
	loadCalled := false
	deps, stdout, _ := newTestSkillsDeps(t, runner, nil)
	deps.LoadConfig = func(string) (*config.Config, error) {
		loadCalled = true
		return nil, errors.New("config must not be loaded")
	}
	cmd := newSkillsCmdWithDeps(deps)
	cmd.SetArgs([]string{"sync", "--worker", "claude_code", "--worker", "acp"})
	err := cmd.Execute()
	require.NoError(t, err)
	require.False(t, loadCalled)
	require.Equal(t, 1, runner.syncCalls)
	require.Equal(t, []reconcile.WorkerType{reconcile.WorkerClaude, reconcile.WorkerACP}, runner.syncOpts.WorkerTypes)
	require.Empty(t, stdout.String())
}

func TestSkillsCommandRequiresExplicitWorkerWhenResolvedSetIsEmpty(t *testing.T) {
	runner := &fakeSkillsRunner{}
	deps, _, _ := newTestSkillsDeps(t, runner, &config.Config{})
	_, _, err := executeSkills(t, deps, "status")
	require.ErrorIs(t, err, reconcile.ErrNoWorkerTargets)
	require.Zero(t, runner.statusCalls)
}

func TestSkillsCommandRejectsUnknownProfileAndWorker(t *testing.T) {
	runner := &fakeSkillsRunner{}
	deps, _, _ := newTestSkillsDeps(t, runner, &config.Config{})
	_, _, err := executeSkills(t, deps, "status", "--profile", "all", "--worker", "claude_code")
	require.ErrorIs(t, err, reconcile.ErrUnknownProfile)
	_, _, err = executeSkills(t, deps, "status", "--worker", "unknown-worker")
	require.ErrorIs(t, err, reconcile.ErrUnknownWorker)
	require.Zero(t, runner.statusCalls)
}

func TestSkillsCommandACPProducesUnsupportedReportWithoutWrite(t *testing.T) {
	runner := &fakeSkillsRunner{statusReport: reconcile.Report{Items: []reconcile.Item{{
		Target: "acp", Action: reconcile.ActionNone, Outcome: reconcile.OutcomeFailed, ReasonCode: reconcile.ReasonUnsupportedWorker,
	}}}}
	deps, stdout, _ := newTestSkillsDeps(t, runner, nil)
	_, _, err := executeSkills(t, deps, "status", "--worker", "acp")
	require.ErrorIs(t, err, reconcile.ErrReportActionRequired)
	require.Equal(t, 1, runner.statusCalls)
	require.Equal(t, []reconcile.WorkerType{reconcile.WorkerACP}, runner.statusOpts.WorkerTypes)
	require.Contains(t, stdout.String(), reconcile.ReasonUnsupportedWorker)
}

func TestSkillsDryRunDoesNotChangeUserOrHotplexHome(t *testing.T) {
	deps, _, _ := newTestSkillsDeps(t, &fakeSkillsRunner{}, nil)
	userHome, err := deps.UserHomeDir()
	require.NoError(t, err)
	hotplexHome := deps.HotplexHomeDir()
	deps.NewRunner = newSkillsRunner
	userBefore, hotplexBefore := snapshotDir(t, userHome), snapshotDir(t, hotplexHome)
	_, _, err = executeSkills(t, deps, "sync", "--worker", "claude_code", "--dry-run")
	require.NoError(t, err)
	require.Equal(t, userBefore, snapshotDir(t, userHome))
	require.Equal(t, hotplexBefore, snapshotDir(t, hotplexHome))
}

func TestSkillsJSONOutputIsReportOnly(t *testing.T) {
	runner := &fakeSkillsRunner{statusReport: reconcile.Report{Profile: "runtime", Items: []reconcile.Item{{
		Target: "/tmp/skill", Action: reconcile.ActionUpdate, Outcome: reconcile.OutcomeChanged, ReasonCode: reconcile.ReasonChanged,
	}}}}
	deps, stdout, stderr := newTestSkillsDeps(t, runner, nil)
	_, _, err := executeSkills(t, deps, "status", "--worker", "claude_code", "--json")
	require.NoError(t, err)
	var report reconcile.Report
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &report))
	require.Equal(t, runner.statusReport, report)
	require.Empty(t, stderr.String())
	require.NotContains(t, stdout.String(), "action:")
}

func TestSkillsCommandReturnsBoundedErrorForConflictAndDrift(t *testing.T) {
	runner := &fakeSkillsRunner{removeReport: reconcile.Report{Items: []reconcile.Item{{
		Target: "/tmp/skill", Action: reconcile.ActionRemove, Outcome: reconcile.OutcomeDrift, ReasonCode: reconcile.ReasonDrift,
	}}}}
	deps, stdout, _ := newTestSkillsDeps(t, runner, nil)
	_, _, err := executeSkills(t, deps, "remove", "--worker", "claude_code")
	require.ErrorIs(t, err, reconcile.ErrReportActionRequired)
	require.Contains(t, stdout.String(), reconcile.ReasonDrift)
}

func commandUses(commands []*cobra.Command) []string {
	result := make([]string, 0, len(commands))
	for _, command := range commands {
		result = append(result, command.Name())
	}
	return result
}

func snapshotDir(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			result[rel] = "dir"
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result[rel] = string(data)
		return nil
	})
	require.NoError(t, err)
	return result
}
