package builtin_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
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
				"references/initialization.md",
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
				require.Contains(t, text, "description: \"Operate a HotPlex host or initialize one: first-time onboard, service install/start, binary updates, host configuration, audit inspection, or Admin mutations. Use only in an explicitly authorized operator context.\"")
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

func TestRepositoryBuiltinSkillsMatchEmbeddedCanonicalTrees(t *testing.T) {
	t.Parallel()

	registry, err := builtin.NewRegistry()
	require.NoError(t, err)

	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "../../.."))

	for _, packageName := range []string{"hotplex-cli", "hotplex-operator"} {
		packageName := packageName
		t.Run(packageName, func(t *testing.T) {
			t.Parallel()

			manifest, exists := registry.Package(packageName)
			require.True(t, exists)
			repositorySkillRoot := filepath.Join(repositoryRoot, ".agents", "skills", packageName)
			require.Equal(t, manifest.Paths(), repositoryFilePaths(t, repositorySkillRoot))

			for _, relativePath := range manifest.Paths() {
				embedded, err := registry.ReadFile(packageName, relativePath)
				require.NoError(t, err)
				repository, err := os.ReadFile(filepath.Join(repositorySkillRoot, filepath.FromSlash(relativePath)))
				require.NoError(t, err)
				require.Equal(t, embedded, repository, relativePath)
			}
		})
	}
}

func TestGoGenerateUsesRepositoryMirrorParent(t *testing.T) {
	t.Parallel()

	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "assets.go"))
	require.NoError(t, err)
	require.Contains(t, string(data), "--mirror ../../../.agents/skills")
	require.NotContains(t, string(data), "--mirror ../../../.agents/skills/hotplex-cli")
}

func repositoryFilePaths(t *testing.T, root string) []string {
	t.Helper()

	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relativePath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(relativePath))
		return nil
	})
	require.NoError(t, err)
	sort.Strings(paths)
	return paths
}
