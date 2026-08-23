package builtin_test

import (
	"bytes"
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

func TestDocsDescribeInventoryProjectionAndExplicitSync(t *testing.T) {
	t.Parallel()

	paths := currentDocumentationPaths()
	var combined strings.Builder
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(repositoryRoot(t), filepath.FromSlash(path)))
		require.NoError(t, err, path)
		combined.Write(data)
		combined.WriteByte('\n')
	}
	docs := combined.String()
	for _, forbidden := range []string{
		"$(date",
		"date -d",
		"date -v",
		"date +",
		"cron-skill-manual.md",
		"~/.hotplex/skills/cron.md",
		"必须建立软链",
		"启动时自动同步",
		"source: builtin",
		"source=builtin",
	} {
		require.NotContains(t, docs, forbidden)
	}
	for _, required := range []string{
		"TOOLS.md",
		"SKILLS.md",
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
		"Phase C",
		"legacy manual migration",
	} {
		require.Contains(t, docs, required)
	}
	require.Contains(t, docs, "ACP")
	require.Regexp(t, regexp.MustCompile(`(?i)ACP[^\n]{0,160}(?:no|without|没有|无)[^\n]{0,160}(?:root|filesystem|文件系统)`), docs)
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../../.."))
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

func currentDocumentationPaths() []string {
	return []string{
		"docs/explanation/agent-config-system.md",
		"docs/explanation/cron-design.md",
		"docs/tutorials/agent-personality.md",
		"docs/tutorials/skills-setup.md",
		"docs/tutorials/cron-scheduled-tasks.md",
		"docs/guides/user/commands-cheatsheet.md",
		"docs/reference/cli.md",
		"docs/reference/configuration.md",
		"docs/reference/admin-api.md",
	}
}
