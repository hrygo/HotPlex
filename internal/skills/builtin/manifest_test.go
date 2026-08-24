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
				require.Contains(t, text, "description: \"使用 HotPlex CLI 处理 Cron、明确请求的 Slack 操作，以及只读 status、doctor、security、config 诊断。不要用于飞书、发布、服务安装、二进制更新或 Admin 变更。\"")
				require.Contains(t, text, "compatibility: \"需要 hotplex CLI，以及已获授权执行目标操作的运行时身份。\"")
			} else {
				require.Contains(t, text, "description: \"运维或初始化 HotPlex 主机，覆盖首次 onboard、服务安装/启动、二进制更新、主机配置、审计检查和 Admin 变更。仅在明确授权的 operator 上下文中使用。\"")
				require.Contains(t, text, "compatibility: \"需要本机主机访问、hotplex CLI，以及明确的 operator 或 Admin 权限。\"")
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
