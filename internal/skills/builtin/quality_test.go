package builtin_test

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/hrygo/hotplex/internal/skills/builtin"
)

type canonicalFrontmatter struct {
	Name          string `yaml:"name"`
	Description   string `yaml:"description"`
	Compatibility string `yaml:"compatibility"`
}

func TestBuiltinSkillFrontmatterAndReferenceClosure(t *testing.T) {
	t.Parallel()

	registry, err := builtin.NewRegistry()
	require.NoError(t, err)

	for _, manifest := range registry.Packages() {
		manifest := manifest
		t.Run(manifest.Name, func(t *testing.T) {
			t.Parallel()

			body, err := registry.ReadFile(manifest.Name, "SKILL.md")
			require.NoError(t, err)
			frontmatter, markdown, ok := splitFrontmatter(body)
			require.True(t, ok, "SKILL.md must contain a YAML frontmatter block")

			metadata, err := decodeCanonicalFrontmatter(frontmatter)
			require.NoError(t, err)
			require.Equal(t, manifest.Name, strings.TrimSpace(metadata.Name))
			require.NotEmpty(t, strings.TrimSpace(metadata.Description))
			require.NotEmpty(t, strings.TrimSpace(metadata.Compatibility))
			require.NotContains(t, metadata.Description, "~/.hotplex")
			require.NotContains(t, metadata.Compatibility, "~/.hotplex")

			manifestPaths := make(map[string]struct{}, len(manifest.Assets))
			for _, asset := range manifest.Assets {
				manifestPaths[asset.Path] = struct{}{}
			}
			linkedReferences := make(map[string]struct{})
			links := regexp.MustCompile(`\]\(([^)]+)\)`).FindAllStringSubmatch(string(markdown), -1)
			for _, link := range links {
				target := strings.TrimSpace(strings.SplitN(link[1], "#", 2)[0])
				if target == "" || strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
					continue
				}
				require.True(t, strings.HasPrefix(target, "references/"), "link escapes package: %q", target)
				require.NotContains(t, strings.TrimPrefix(target, "references/"), "/", "reference links must be one level: %q", target)
				require.Equal(t, target, filepath.ToSlash(filepath.Clean(filepath.FromSlash(target))))
				_, exists := manifestPaths[target]
				require.True(t, exists, "linked reference is not in manifest: %q", target)
				linkedReferences[target] = struct{}{}
			}

			for _, path := range manifest.Paths() {
				_, err := registry.ReadFile(manifest.Name, path)
				require.NoError(t, err, path)
				if strings.HasPrefix(path, "references/") {
					_, linked := linkedReferences[path]
					require.True(t, linked, "reference asset is not reachable from SKILL.md: %q", path)
				}
			}
		})
	}
}

func TestBuiltinDescriptionsHavePositiveAndNegativeBoundaries(t *testing.T) {
	t.Parallel()

	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	for _, manifest := range registry.Packages() {
		body, err := registry.ReadFile(manifest.Name, "SKILL.md")
		require.NoError(t, err)
		frontmatter, _, ok := splitFrontmatter(body)
		require.True(t, ok)

		metadata, err := decodeCanonicalFrontmatter(frontmatter)
		require.NoError(t, err)
		description := strings.TrimSpace(metadata.Description)
		switch manifest.Name {
		case "hotplex-cli":
			require.Contains(t, description, "Use HotPlex CLI")
			require.Contains(t, description, "Do not use")
		case "hotplex-operator":
			require.Contains(t, description, "Operate a HotPlex host")
			require.Contains(t, description, "Use only in an explicitly authorized operator context")
		default:
			t.Fatalf("unclassified canonical package %q", manifest.Name)
		}
	}
}

func TestOperatorSkillConsolidatesSetupAndUpdateWorkflows(t *testing.T) {
	t.Parallel()

	root := repositoryRoot(t)
	t.Run("duplicate skills are removed", func(t *testing.T) {
		t.Parallel()

		require.NoDirExists(t, filepath.Join(root, ".agents", "skills", "hotplex-setup"))
		require.NoDirExists(t, filepath.Join(root, ".agents", "skills", "hotplex-update"))
	})

	t.Run("operator uses supported primitives", func(t *testing.T) {
		t.Parallel()

		operator := readDirectoryTree(t, filepath.Join(root, "internal", "skills", "builtin", "hotplex-operator"))
		for _, prohibited := range []string{
			"curl -fsSL",
			"| bash",
			"cp -f ./bin/hotplex",
			"sleep 2",
			"git checkout <previous",
		} {
			require.NotContains(t, operator, prohibited)
		}
		for _, requiredCommand := range []string{
			"hotplex onboard",
			"--non-interactive",
			"--install-service",
			"--sync-skills",
			"hotplex doctor",
			"hotplex doctor --json",
			"hotplex update",
			"hotplex service install",
			"hotplex service start",
			"hotplex service status",
			"hotplex service logs",
			"hotplex service restart",
			"hotplex skills status",
		} {
			require.Contains(t, operator, requiredCommand)
		}
	})
}

func TestGeneratedCLISurfaceHasNoHiddenOrSensitiveValues(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join(repositoryRoot(t), "internal/skills/builtin/hotplex-cli/references/cli-surface.generated.md"))
	require.NoError(t, err)
	text := string(data)
	require.Contains(t, text, "# Public HotPlex CLI surface")
	require.NotContains(t, text, "internal-generate-cli-surface")
	require.NotContains(t, text, "/Users/")
	require.NotContains(t, text, "/home/")
	require.NotContains(t, text, "~/.hotplex")
	require.NotContains(t, text, "GATEWAY_")
	require.NotContains(t, text, "example-secret")
	require.NotContains(t, text, "v1.41.0")
	require.NotRegexp(t, regexp.MustCompile(`(?i)(?:password|token|secret|api[_-]?key)\s*=`), text)
	require.NotRegexp(t, regexp.MustCompile(`(?m)^## .*\b(hidden|internal)\b`), text)
}

func TestCurrentDocsDescribeStrictAgentConfigAndSkillsArchitecture(t *testing.T) {
	t.Parallel()

	paths := strictCurrentDocumentationPaths()
	var combined strings.Builder
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(repositoryRoot(t), filepath.FromSlash(path)))
		require.NoError(t, err, path)
		combined.Write(data)
		combined.WriteByte('\n')
	}
	docs := combined.String()
	for _, forbidden := range []string{
		"SKILLS.md",
		"platform/default/",
		"ReleaseSkillManual",
		"cron-skill-manual.md",
		"legacy compatibility artifact",
		"legacy manual migration",
		"agent_configs.skills",
		"skills/cron.md",
		"skills/phrases.md",
		"skills/db-stats.md",
		"$(date",
		"date -d",
		"date -v",
		"date +",
		"必须建立软链",
		"启动时自动同步",
		"source: builtin",
		"source=builtin",
	} {
		require.NotContains(t, docs, forbidden)
	}
	for _, required := range []string{
		"SOUL.md",
		"AGENTS.md",
		"TOOLS.md",
		"不是 Agent Skill",
		"USER.md",
		"MEMORY.md",
		"Bot → 平台 → 全局",
		"present-empty",
		"internal/skills/builtin/hotplex-cli",
		"internal/skills/builtin/hotplex-operator",
		".agents/skills/hotplex-cli",
		".agents/skills/hotplex-operator",
		"hotplex-diagnostics",
		"hotplex-docs-patrol",
		"hotplex-release",
		"不会自动删除或改写",
		"Admin/WebChat",
		"public Skills catalog",
		"Session `/skills`",
		"hotplex skills status",
		"hotplex skills sync",
		"hotplex skills remove",
		"--sync-skills",
		"--worker",
		"--dry-run",
		"--json",
		"$HOTPLEX_HOME",
		"UserHome",
		"immutable inventory",
		"native projection",
		"discoverable",
		"callable",
		"SKILL_BUILTIN_READONLY",
		"builtin_package_version",
		"hotplex cron get <id|name> --json",
	} {
		require.Contains(t, docs, required)
	}
	require.Contains(t, docs, "ACP")
	require.Regexp(t, regexp.MustCompile(`(?i)ACP[^\n]{0,160}(?:no|without|没有|无)[^\n]{0,160}(?:root|filesystem|文件系统)`), docs)
}

func TestCurrentDocsNavigationAndCronSkillBoundary(t *testing.T) {
	t.Parallel()

	root := repositoryRoot(t)
	index := readCurrentDoc(t, root, "docs/index.md")
	require.Contains(t, index, "tutorials/skills-setup.md")
	require.NotContains(t, index, "全部 38 个 CLI")
	require.Contains(t, index, "public command/flag surface")

	skillsSetup := readCurrentDoc(t, root, "docs/tutorials/skills-setup.md")
	require.NotContains(t, skillsSetup, "../specs/Skill-Management-Spec.md")
	require.Contains(t, skillsSetup, "../reference/admin-api.md")

	boundaryDocs := strings.Join([]string{
		readCurrentDoc(t, root, "docs/explanation/agent-config-system.md"),
		readCurrentDoc(t, root, "docs/explanation/cron-design.md"),
		readCurrentDoc(t, root, "docs/guides/developer/cron-automation.md"),
		readCurrentDoc(t, root, "docs/guides/contributor/architecture.md"),
	}, "\n")
	for _, forbidden := range []string{
		"ReleaseSkillManual",
		"cron-skill-manual.md",
		"legacy compatibility artifact",
		"SKILLS.md",
	} {
		require.NotContains(t, boundaryDocs, forbidden)
	}
	require.Contains(t, boundaryDocs, "<name>/SKILL.md")
	require.Contains(t, boundaryDocs, "hotplex-cli")
	require.Contains(t, boundaryDocs, "TOOLS.md")

	cronGuide := readCurrentDoc(t, root, "docs/guides/developer/cron-automation.md")
	require.Contains(t, cronGuide, "hotplex-cli")
	require.Contains(t, cronGuide, "hotplex cron --help")
	architecture := readCurrentDoc(t, root, "docs/guides/contributor/architecture.md")
	require.Contains(t, architecture, "<name>/SKILL.md")
}

func TestCurrentDocsBFSExcludesHistoricalSpecEntrypoints(t *testing.T) {
	t.Parallel()

	root := filepath.Join(repositoryRoot(t), "docs")
	reachable := map[string]struct{}{"index.md": {}}
	queue := []string{"index.md"}
	linkPattern := regexp.MustCompile(`\]\(([^)]+)\)`)
	for len(queue) > 0 {
		relative := queue[0]
		queue = queue[1:]
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		require.NoError(t, err, relative)
		for _, match := range linkPattern.FindAllStringSubmatch(string(data), -1) {
			destination := strings.SplitN(strings.SplitN(strings.TrimSpace(match[1]), "#", 2)[0], "?", 2)[0]
			if destination == "" || strings.HasPrefix(destination, "http://") || strings.HasPrefix(destination, "https://") || strings.HasPrefix(destination, "mailto:") || !strings.HasSuffix(destination, ".md") {
				continue
			}
			candidate := filepath.Clean(filepath.Join(filepath.Dir(filepath.Join(root, filepath.FromSlash(relative))), filepath.FromSlash(destination)))
			relativeCandidate, err := filepath.Rel(root, candidate)
			require.NoError(t, err)
			if strings.HasPrefix(relativeCandidate, ".."+string(filepath.Separator)) || relativeCandidate == ".." {
				continue
			}
			if _, err := os.Stat(candidate); err != nil {
				continue
			}
			canonical := filepath.ToSlash(relativeCandidate)
			if _, exists := reachable[canonical]; !exists {
				reachable[canonical] = struct{}{}
				queue = append(queue, canonical)
			}
		}
	}

	require.Len(t, reachable, 58)
	for _, historical := range []string{
		"specs/ACP-Worker-Spec.md",
		"specs/Admin-Workspace-PermissionMode-Management-Spec.md",
		"specs/Skill-Management-Spec.md",
		"specs/Workspace-Permission-Mode-Admin-Only-Revision-Spec.md",
		"specs/Workspace-Permission-Mode-Spec.md",
	} {
		_, found := reachable[historical]
		require.False(t, found, historical)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../../.."))
}

func readCurrentDoc(t *testing.T, root, relativePath string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relativePath)))
	require.NoError(t, err, relativePath)
	return string(data)
}

func readDirectoryTree(t *testing.T, root string) string {
	t.Helper()
	var combined strings.Builder
	require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		combined.Write(data)
		combined.WriteByte('\n')
		return nil
	}))
	return combined.String()
}

func splitFrontmatter(data []byte) ([]byte, []byte, bool) {
	if !strings.HasPrefix(string(data), "---\n") {
		return nil, nil, false
	}
	rest := data[len("---\n"):]
	end := strings.Index(string(rest), "\n---")
	if end < 0 {
		return nil, nil, false
	}
	return rest[:end], rest[end+len("\n---"):], true
}

func decodeCanonicalFrontmatter(data []byte) (canonicalFrontmatter, error) {
	var metadata canonicalFrontmatter
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	err := decoder.Decode(&metadata)
	return metadata, err
}

func strictCurrentDocumentationPaths() []string {
	return []string{
		"AGENTS.md",
		"internal/agentconfig/META-COGNITION.md",
		"docs/architecture/Agent-Config-Design.md",
		"docs/explanation/agent-config-system.md",
		"docs/explanation/cron-design.md",
		"docs/guides/contributor/architecture.md",
		"docs/guides/developer/cron-automation.md",
		"docs/guides/enterprise/multi-tenant.md",
		"docs/tutorials/agent-personality.md",
		"docs/tutorials/phrases-customization.md",
		"docs/tutorials/skills-setup.md",
		"docs/tutorials/cron-scheduled-tasks.md",
		"docs/guides/user/commands-cheatsheet.md",
		"docs/reference/cli.md",
		"docs/reference/configuration.md",
		"docs/reference/admin-api.md",
		"docs/reference/glossary.md",
		"docs/swagger/swagger.json",
	}
}
