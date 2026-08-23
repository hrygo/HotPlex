package checkers

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/cli"
	"github.com/hrygo/hotplex/internal/skills/reconcile"
)

func TestBuiltinSkillsCheckerIsReadOnlyAndHasNoFix(t *testing.T) {
	t.Parallel()
	checker := NewBuiltinSkillsChecker(func(context.Context) (reconcile.Report, error) {
		return reconcile.Report{Items: []reconcile.Item{{
			Target:        "/private/native-root",
			BackupPath:    "/private/backup",
			Action:        reconcile.ActionUpdate,
			Outcome:       reconcile.OutcomeDrift,
			ReasonCode:    reconcile.ReasonDrift,
			WorkerAliases: []reconcile.WorkerType{reconcile.WorkerClaude},
		}}}, nil
	})

	diagnostic := checker.Check(context.Background())
	require.Equal(t, "skills.builtin", diagnostic.Name)
	require.Equal(t, "skills", diagnostic.Category)
	require.Equal(t, cli.StatusWarn, diagnostic.Status)
	require.Nil(t, diagnostic.FixFunc)
	require.Contains(t, diagnostic.Message, reconcile.ReasonDrift)
	require.NotContains(t, diagnostic.Message, "/private/native-root")
	require.NotContains(t, diagnostic.Detail, "/private/backup")
}

func TestBuiltinSkillsCheckerMapsUnsafeItemsToFailureWithoutLeakingPaths(t *testing.T) {
	t.Parallel()
	checker := NewBuiltinSkillsChecker(func(context.Context) (reconcile.Report, error) {
		return reconcile.Report{Items: []reconcile.Item{
			{Target: "/tmp/collision", Outcome: reconcile.OutcomeConflict, ReasonCode: reconcile.ReasonCollision},
			{Target: "/tmp/failed", Outcome: reconcile.OutcomeDrift, ReasonCode: reconcile.ReasonInvalidReceipt},
		}}, nil
	})

	diagnostic := checker.Check(context.Background())
	require.Equal(t, cli.StatusFail, diagnostic.Status)
	require.Contains(t, diagnostic.Message, reconcile.ReasonCollision)
	require.Contains(t, diagnostic.Message, reconcile.ReasonInvalidReceipt)
	require.NotContains(t, diagnostic.Message, "/tmp/collision")
	require.NotContains(t, diagnostic.Detail, "/tmp/failed")
}

func TestBuiltinSkillsCheckerMapsStatusErrorToStableFailure(t *testing.T) {
	t.Parallel()
	checker := NewBuiltinSkillsChecker(func(context.Context) (reconcile.Report, error) {
		return reconcile.Report{}, errors.New("sensitive path /Users/private/.hotplex/state")
	})

	diagnostic := checker.Check(context.Background())
	require.Equal(t, cli.StatusFail, diagnostic.Status)
	require.Contains(t, diagnostic.Message, "status_unavailable")
	require.NotContains(t, diagnostic.Message, "/Users/private")
	require.NotContains(t, diagnostic.Detail, "/Users/private")
}

func TestBuiltinSkillsCheckerIsRegisteredUnderSkills(t *testing.T) {
	checkers := cli.DefaultRegistry.ByCategory("skills")
	require.NotEmpty(t, checkers)
	found := false
	for _, checker := range checkers {
		if checker.Name() != "skills.builtin" {
			continue
		}
		found = true
		require.Equal(t, "skills", checker.Category())
	}
	require.True(t, found, "skills.builtin must be registered")
}
