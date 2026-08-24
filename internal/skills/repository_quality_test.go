package skills_test

import (
	"bytes"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type repositorySkillFrontmatter struct {
	Name          string         `yaml:"name"`
	Description   string         `yaml:"description"`
	Compatibility string         `yaml:"compatibility,omitempty"`
	License       string         `yaml:"license,omitempty"`
	AllowedTools  string         `yaml:"allowed-tools,omitempty"`
	Metadata      map[string]any `yaml:"metadata,omitempty"`
}

var markdownLinkPattern = regexp.MustCompile(`\[[^]]*\]\(([^)]+)\)`)

func TestRepositorySkillQuality(t *testing.T) {
	t.Parallel()

	root := filepath.Join(repositoryRoot(t), ".agents", "skills")
	entries, err := os.ReadDir(root)
	require.NoError(t, err)

	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	require.Equal(t, []string{
		"hotplex-cli",
		"hotplex-diagnostics",
		"hotplex-docs-patrol",
		"hotplex-operator",
		"hotplex-release",
		"hotplex-stt-tts",
	}, names)

	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			validateRepositorySkill(t, root, name)
		})
	}
}

func validateRepositorySkill(t *testing.T, skillsRoot, name string) {
	t.Helper()

	skillRoot := filepath.Join(skillsRoot, name)
	entrypoint := filepath.Join(skillRoot, "SKILL.md")
	data, err := os.ReadFile(entrypoint)
	require.NoError(t, err)

	frontmatter, body := splitRepositoryFrontmatter(t, data)
	var metadata repositorySkillFrontmatter
	decoder := yaml.NewDecoder(bytes.NewReader(frontmatter))
	decoder.KnownFields(true)
	require.NoError(t, decoder.Decode(&metadata))
	var extra any
	require.ErrorIs(t, decoder.Decode(&extra), io.EOF, "frontmatter must contain exactly one YAML document")
	require.Equal(t, name, strings.TrimSpace(metadata.Name))
	require.NotEmpty(t, strings.TrimSpace(metadata.Description))
	require.Contains(t, metadata.Description, "HotPlex")
	require.GreaterOrEqual(t, utf8.RuneCountInString(metadata.Description), 40, "description should discriminate the Skill")
	require.LessOrEqual(t, utf8.RuneCountInString(metadata.Description), 500, "description should remain selective")
	require.LessOrEqual(t, bytes.Count(data, []byte("\n"))+1, 120, "SKILL.md should remain a short router")

	tree := readSkillTree(t, skillRoot)
	for _, prohibited := range []string{
		"SKILLS.md",
		"TOK=$(grep",
		"| bash",
		"sleep 2",
		"git checkout <previous",
		"TODO",
		"FIXME",
		"Replace with",
	} {
		require.NotContains(t, tree, prohibited)
	}

	validateReferenceClosure(t, skillRoot, body)
	validateMutationBoundary(t, name, string(body), tree)
}

func splitRepositoryFrontmatter(t *testing.T, data []byte) ([]byte, []byte) {
	t.Helper()

	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	require.NotEmpty(t, lines)
	require.Equal(t, "---", lines[0], "SKILL.md must open with frontmatter")
	closing := -1
	for index := 1; index < len(lines); index++ {
		if lines[index] == "---" {
			closing = index
			break
		}
	}
	require.Greater(t, closing, 1, "SKILL.md must close its frontmatter exactly once before the body")

	return []byte(strings.Join(lines[1:closing], "\n")), []byte(strings.Join(lines[closing+1:], "\n"))
}

func validateReferenceClosure(t *testing.T, skillRoot string, entrypointBody []byte) {
	t.Helper()

	reachable := map[string]struct{}{"SKILL.md": {}}
	type queuedDocument struct {
		path string
		body []byte
	}
	queue := []queuedDocument{{path: "SKILL.md", body: entrypointBody}}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, match := range markdownLinkPattern.FindAllSubmatch(current.body, -1) {
			target := localMarkdownTarget(string(match[1]))
			if target == "" {
				continue
			}

			candidate := filepath.Clean(filepath.Join(filepath.Dir(filepath.Join(skillRoot, filepath.FromSlash(current.path))), filepath.FromSlash(target)))
			relative, err := filepath.Rel(skillRoot, candidate)
			require.NoError(t, err)
			require.NotEqual(t, "..", relative, "link escapes Skill directory: %q", target)
			require.False(t, strings.HasPrefix(relative, ".."+string(filepath.Separator)), "link escapes Skill directory: %q", target)
			relative = filepath.ToSlash(relative)
			require.FileExists(t, candidate, "missing local reference %q", relative)
			if _, seen := reachable[relative]; seen {
				continue
			}
			reachable[relative] = struct{}{}
			linked, err := os.ReadFile(candidate)
			require.NoError(t, err)
			queue = append(queue, queuedDocument{path: relative, body: linked})
		}
	}

	referencesRoot := filepath.Join(skillRoot, "references")
	if _, err := os.Stat(referencesRoot); os.IsNotExist(err) {
		return
	}
	require.NoError(t, filepath.WalkDir(referencesRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(skillRoot, path)
		if err != nil {
			return err
		}
		_, found := reachable[filepath.ToSlash(relative)]
		require.True(t, found, "reference is unreachable from SKILL.md: %s", relative)
		return nil
	}))
}

func localMarkdownTarget(raw string) string {
	target := strings.TrimSpace(strings.Trim(raw, "<>"))
	target = strings.SplitN(target, "#", 2)[0]
	target = strings.SplitN(target, "?", 2)[0]
	if target == "" || strings.HasPrefix(target, "/") {
		return ""
	}
	parsed, err := url.Parse(target)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" {
		return ""
	}
	if !strings.HasSuffix(strings.ToLower(parsed.Path), ".md") {
		return ""
	}
	return parsed.Path
}

func validateMutationBoundary(t *testing.T, name, entrypoint, tree string) {
	t.Helper()

	prefix := entrypoint
	if len(prefix) > 1200 {
		prefix = prefix[:1200]
	}
	switch name {
	case "hotplex-diagnostics":
		require.Contains(t, prefix, "只读")
		require.NotContains(t, tree, "gh issue create")
		require.NotContains(t, tree, "Authorization: Bearer")
	case "hotplex-operator":
		require.Regexp(t, regexp.MustCompile(`(?i)explicit[^\n]{0,80}authori`), prefix)
	case "hotplex-release":
		require.Contains(t, prefix, "明确授权")
		require.Contains(t, prefix, "tag")
		require.Contains(t, prefix, "push")
		require.Contains(t, prefix, "Release")
	case "hotplex-docs-patrol":
		require.Contains(t, prefix, "明确授权")
		for _, implicitDelivery := range []string{"git checkout -b", "gh issue create", "gh pr create", "git push fork"} {
			require.NotContains(t, tree, implicitDelivery)
		}
	case "hotplex-stt-tts":
		require.Contains(t, prefix, "explicitly authorizes")
		require.Contains(t, tree, "hotplex service restart")
	}
}

func readSkillTree(t *testing.T, root string) string {
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

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
}
