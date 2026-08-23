package builtin_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/skills/builtin"
)

func TestBuiltinPublicCatalogListsCanonicalWithoutProjection(t *testing.T) {
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	catalog := builtin.NewPublicCatalog(registry)

	listed, err := catalog.List(context.Background(), string(builtin.ProfileOperator))
	require.NoError(t, err)
	require.Len(t, listed, 2)
	require.Equal(t, []string{"hotplex-cli", "hotplex-operator"}, []string{listed[0].Name, listed[1].Name})
	for _, skill := range listed {
		require.Equal(t, "global", skill.Source)
		require.True(t, skill.Builtin)
		require.NotEmpty(t, skill.BuiltinPackageVersion)
		require.False(t, skill.Managed)
		require.Empty(t, skill.FilePath)
	}
}

func TestBuiltinPublicCatalogReadsEmbeddedBodyAndReferences(t *testing.T) {
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	catalog := builtin.NewPublicCatalog(registry)

	detail, err := catalog.Read(context.Background(), string(builtin.ProfileOperator), "hotplex-cli")
	require.NoError(t, err)
	require.Equal(t, "hotplex-cli", detail.Name)
	require.Contains(t, detail.Body, "name: hotplex-cli")
	require.Contains(t, detail.Files, "references/cron.md")
	require.Contains(t, detail.Files, "references/cli-surface.generated.md")
	require.True(t, detail.Builtin)
	require.False(t, detail.Managed)
	require.Empty(t, detail.FilePath)
}

func TestBuiltinPublicCatalogReadHonorsProfileAndName(t *testing.T) {
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	catalog := builtin.NewPublicCatalog(registry)

	_, err = catalog.Read(context.Background(), string(builtin.ProfileRuntime), "hotplex-operator")
	require.ErrorIs(t, err, builtin.ErrSkillNotFound)

	detail, err := catalog.Read(context.Background(), string(builtin.ProfileOperator), "hotplex-operator")
	require.NoError(t, err)
	require.Equal(t, "hotplex-operator", detail.Name)

	_, err = catalog.Read(context.Background(), "unknown", "hotplex-cli")
	require.Error(t, err)
	_, err = catalog.Read(context.Background(), string(builtin.ProfileRuntime), "unknown")
	require.ErrorIs(t, err, builtin.ErrSkillNotFound)
}
