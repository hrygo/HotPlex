package claudecode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/hrygo/hotplex/internal/skills"
	"github.com/hrygo/hotplex/internal/worker"
)

// nativeSkillRoot returns Claude Code's one supported global Skill root. The
// work directory is deliberately not involved: project/.claude and HotPlex's
// own inventory are discovery surfaces, not Claude's authoritative catalog.
func (w *Worker) nativeSkillRoot() (string, error) {
	resolve := w.userHomeDir
	if resolve == nil {
		resolve = os.UserHomeDir
	}
	home, err := resolve()
	if err != nil {
		return "", fmt.Errorf("claude: resolve user home: %w", err)
	}
	if home == "" {
		return "", errors.New("claude: user home is empty")
	}
	home, err = filepath.Abs(home)
	if err != nil {
		return "", fmt.Errorf("claude: canonicalize user home: %w", err)
	}
	return filepath.Join(home, ".claude", "skills"), nil
}

// ListInvokableSkills implements worker.SkillCatalogProvider using Claude
// Code's exact native root. A missing root is a valid empty catalog: Claude
// simply has no globally installed Skills yet. Other filesystem or metadata
// failures are returned so the Gateway can fail closed.
func (w *Worker) ListInvokableSkills(ctx context.Context, _ string) ([]worker.SkillDescriptor, error) {
	empty := make([]worker.SkillDescriptor, 0)
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}

	root, err := w.nativeSkillRoot()
	if err != nil {
		return empty, err
	}
	found, err := skills.ScanRootContext(ctx, root, skills.SourceGlobal, false)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return empty, nil
		}
		return empty, fmt.Errorf("claude: scan native skill root: %w", err)
	}

	descriptors := make([]worker.SkillDescriptor, 0, len(found))
	for _, skill := range found {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		path, err := filepath.Abs(skill.FilePath)
		if err != nil {
			return empty, fmt.Errorf("claude: resolve skill %q path: %w", skill.Name, err)
		}
		descriptors = append(descriptors, worker.SkillDescriptor{
			Name:        skill.Name,
			Description: skill.Description,
			Path:        path,
		})
	}
	sort.SliceStable(descriptors, func(i, j int) bool {
		if descriptors[i].Name != descriptors[j].Name {
			return descriptors[i].Name < descriptors[j].Name
		}
		return descriptors[i].Path < descriptors[j].Path
	})
	return descriptors, nil
}
