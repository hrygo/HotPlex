package builtin_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/skills/builtin"
)

func TestCanonicalPackagesHavePortableFrontmatterAndClosedReferences(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		profile builtin.Profile
		files   []string
	}{
		{
			name:    "hotplex-cli",
			profile: builtin.ProfileRuntime,
			files: []string{
				"SKILL.md",
				"references/cron.md",
				"references/slack.md",
				"references/diagnostics.md",
				"references/cli-surface.generated.md",
			},
		},
		{
			name:    "hotplex-operator",
			profile: builtin.ProfileOperator,
			files: []string{
				"SKILL.md",
				"references/service-lifecycle.md",
				"references/install-update.md",
				"references/configuration.md",
				"references/admin-audit.md",
			},
		},
	}

	registry, err := builtin.NewRegistry()
	require.NoError(t, err)

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			manifest, ok := registry.Package(tc.name)
			require.True(t, ok)
			require.Equal(t, tc.profile, manifest.Profile)
			require.Regexp(t, "^v1-[0-9a-f]{64}$", manifest.Version)
			require.ElementsMatch(t, tc.files, manifest.Paths())

			body, err := registry.ReadFile(tc.name, "SKILL.md")
			require.NoError(t, err)
			text := string(body)
			require.Contains(t, text, "name: "+tc.name)
			require.Contains(t, text, "description:")
			require.Contains(t, text, "compatibility:")
			if tc.name == "hotplex-cli" {
				require.Contains(t, text, "description: Use HotPlex CLI for Cron jobs, explicitly requested Slack operations, and read-only status, doctor, security, or config diagnostics. Do not use for Feishu, releases, service installation, binary updates, or Admin mutations.")
				require.Contains(t, text, "compatibility: Requires the hotplex CLI and a runtime identity authorized for the requested operation.")
			} else {
				require.Contains(t, text, "description: Operate a HotPlex host: install or restart services, update binaries, change host configuration, inspect audit state, or perform Admin mutations. Use only in an explicitly authorized operator context.")
				require.Contains(t, text, "compatibility: Requires local host access, the hotplex CLI, and explicit operator or Admin authority.")
			}
		})
	}
}

func TestProfilePackageSetIsCumulativeAndStable(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"hotplex-cli"}, builtin.ProfilePackageSet(builtin.ProfileRuntime))
	require.Equal(t, []string{"hotplex-cli", "hotplex-operator"}, builtin.ProfilePackageSet(builtin.ProfileOperator))

	names := builtin.ProfilePackageSet(builtin.ProfileOperator)
	names[0] = "mutated"
	require.Equal(t, []string{"hotplex-cli", "hotplex-operator"}, builtin.ProfilePackageSet(builtin.ProfileOperator))
}

func TestRepositoryRuntimeSkillMatchesEmbeddedCanonicalTree(t *testing.T) {
	t.Parallel()

	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	manifest, ok := registry.Package("hotplex-cli")
	require.True(t, ok)

	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "../../.."))
	repositorySkillRoot := filepath.Join(repositoryRoot, ".agents", "skills", "hotplex-cli")

	for _, relativePath := range manifest.Paths() {
		embedded, err := registry.ReadFile("hotplex-cli", relativePath)
		require.NoError(t, err)
		repository, err := os.ReadFile(filepath.Join(repositorySkillRoot, filepath.FromSlash(relativePath)))
		require.NoError(t, err)
		require.Equal(t, embedded, repository, relativePath)
	}
}
