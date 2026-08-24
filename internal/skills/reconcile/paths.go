package reconcile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DefaultPaths returns the shared runtime layout used by all built-in Skill
// entry points. The .agents root is canonical; Claude receives per-package
// links through AliasRoots.
func DefaultPaths(userHome, hotplexHome string) Paths {
	return Paths{
		UserHome:     userHome,
		HotplexHome:  hotplexHome,
		InventoryDir: filepath.Join(hotplexHome, "skills", "builtin"),
		StateDir:     filepath.Join(hotplexHome, "state", "skills"),
		NativeRoots: map[WorkerType]string{
			WorkerClaude:   filepath.Join(userHome, ".agents", "skills"),
			WorkerCodex:    filepath.Join(userHome, ".agents", "skills"),
			WorkerOpenCode: filepath.Join(userHome, ".agents", "skills"),
		},
		AliasRoots: map[WorkerType]string{
			WorkerClaude: filepath.Join(userHome, ".claude", "skills"),
		},
	}
}

func ResolveTargets(userHome string, workerTypes []WorkerType) ([]Target, error) {
	if strings.TrimSpace(userHome) == "" {
		return nil, fmt.Errorf("%w: empty home", ErrRootOutsideHome)
	}
	if len(workerTypes) == 0 {
		return nil, ErrNoWorkerTargets
	}
	if !filepath.IsAbs(userHome) {
		return nil, fmt.Errorf("%w: UserHome must be absolute", ErrRootOutsideHome)
	}
	homeInput, err := filepath.Abs(filepath.Clean(userHome))
	if err != nil {
		return nil, fmt.Errorf("resolve user home: %w", err)
	}
	home, err := canonicalOSPath(homeInput)
	if err != nil {
		return nil, fmt.Errorf("resolve user home: %w", err)
	}
	if info, statErr := os.Stat(home); statErr != nil || !info.IsDir() {
		if statErr == nil {
			statErr = fmt.Errorf("not a directory")
		}
		return nil, fmt.Errorf("resolve user home: %w", statErr)
	}

	seen := make(map[WorkerType]struct{}, len(workerTypes))
	for _, workerType := range workerTypes {
		if _, err := ParseWorkerType(string(workerType)); err != nil {
			return nil, err
		}
		seen[workerType] = struct{}{}
	}

	var targets []Target
	appendTarget := func(target Target) {
		for i := range targets {
			if targets[i].CanonicalRoot != target.CanonicalRoot || targets[i].ReasonCode != target.ReasonCode {
				continue
			}
			targets[i].WorkerAliases = appendUniqueWorkers(targets[i].WorkerAliases, target.WorkerAliases...)
			targets[i].AliasRoots = appendUniqueStrings(targets[i].AliasRoots, target.AliasRoots...)
			return
		}
		targets = append(targets, target)
	}
	if _, ok := seen[WorkerClaude]; ok {
		root, err := resolveNativeOSRoot(homeInput, home, ".agents")
		if err != nil {
			return nil, err
		}
		aliasRoot, err := resolveNativeOSRoot(homeInput, home, ".claude")
		if err != nil {
			return nil, err
		}
		appendTarget(Target{CanonicalRoot: root, WorkerAliases: []WorkerType{WorkerClaude}, AliasRoots: []string{aliasRoot}})
	}
	if _, codex := seen[WorkerCodex]; codex {
		root, err := resolveNativeOSRoot(homeInput, home, ".agents")
		if err != nil {
			return nil, err
		}
		appendTarget(Target{CanonicalRoot: root, WorkerAliases: []WorkerType{WorkerCodex, WorkerOpenCode}})
	} else if _, openCode := seen[WorkerOpenCode]; openCode {
		root, err := resolveNativeOSRoot(homeInput, home, ".agents")
		if err != nil {
			return nil, err
		}
		appendTarget(Target{CanonicalRoot: root, WorkerAliases: []WorkerType{WorkerCodex, WorkerOpenCode}})
	}
	if _, ok := seen[WorkerACP]; ok {
		targets = append(targets, Target{WorkerAliases: []WorkerType{WorkerACP}, ReasonCode: ReasonUnsupportedWorker})
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].CanonicalRoot != targets[j].CanonicalRoot {
			return targets[i].CanonicalRoot < targets[j].CanonicalRoot
		}
		return targets[i].ReasonCode < targets[j].ReasonCode
	})
	return targets, nil
}

func appendUniqueWorkers(values []WorkerType, additions ...WorkerType) []WorkerType {
	seen := make(map[WorkerType]struct{}, len(values)+len(additions))
	result := make([]WorkerType, 0, len(values)+len(additions))
	for _, value := range append(values, additions...) {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func appendUniqueStrings(values []string, additions ...string) []string {
	seen := make(map[string]struct{}, len(values)+len(additions))
	result := make([]string, 0, len(values)+len(additions))
	for _, value := range append(values, additions...) {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func resolveNativeOSRoot(homeInput, canonicalHome, baseName string) (string, error) {
	basePath := filepath.Join(homeInput, baseName)
	base, err := canonicalOSPath(basePath)
	if err != nil {
		return "", fmt.Errorf("resolve native base: %w", err)
	}
	if !isWithin(canonicalHome, base) {
		return "", fmt.Errorf("%w: %s", ErrRootOutsideHome, basePath)
	}
	rootInput := filepath.Join(basePath, "skills")
	root, err := canonicalOSPath(rootInput)
	if err != nil {
		return "", fmt.Errorf("resolve native root: %w", err)
	}
	if !isWithin(canonicalHome, root) || !isWithin(base, root) {
		return "", fmt.Errorf("%w: %s", ErrRootOutsideHome, root)
	}
	if info, statErr := os.Lstat(rootInput); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return root, nil
	}
	return rootInput, nil
}

func canonicalOSPath(name string) (string, error) {
	abs, err := filepath.Abs(filepath.Clean(name))
	if err != nil {
		return "", err
	}
	return canonicalPathWith(abs, os.Lstat, filepath.EvalSymlinks)
}

type lstatFunc func(string) (os.FileInfo, error)
type evalSymlinksFunc func(string) (string, error)

func canonicalPathWith(name string, lstat lstatFunc, eval evalSymlinksFunc) (string, error) {
	current := filepath.Clean(name)
	var suffix []string
	for {
		_, err := lstat(current)
		if err == nil {
			resolved, evalErr := eval(current)
			if evalErr != nil {
				return "", evalErr
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return current, nil
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func isWithin(base, candidate string) bool {
	rel, err := filepath.Rel(filepath.Clean(base), filepath.Clean(candidate))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func normalizePaths(fs FileSystem, input Paths) (Paths, error) {
	if fs == nil {
		return Paths{}, errors.New("skills: nil filesystem")
	}
	if !filepath.IsAbs(input.UserHome) || !filepath.IsAbs(input.HotplexHome) {
		return Paths{}, errors.New("skills: UserHome and HotplexHome must be absolute")
	}
	if (input.InventoryDir != "" && !filepath.IsAbs(input.InventoryDir)) ||
		(input.StateDir != "" && !filepath.IsAbs(input.StateDir)) {
		return Paths{}, errors.New("skills: inventory and state paths must be absolute")
	}
	userHome, err := canonicalFSPath(fs, input.UserHome)
	if err != nil {
		return Paths{}, fmt.Errorf("resolve UserHome: %w", err)
	}
	hotplexHome, err := canonicalFSPath(fs, input.HotplexHome)
	if err != nil {
		return Paths{}, fmt.Errorf("resolve HotplexHome: %w", err)
	}
	if !isExistingDir(fs, userHome) || !isExistingDir(fs, hotplexHome) {
		return Paths{}, errors.New("skills: UserHome and HotplexHome must be existing directories")
	}
	paths := Paths{
		UserHome:     userHome,
		HotplexHome:  hotplexHome,
		InventoryDir: input.InventoryDir,
		StateDir:     input.StateDir,
		NativeRoots:  make(map[WorkerType]string, len(input.NativeRoots)+3),
		AliasRoots:   make(map[WorkerType]string, len(input.AliasRoots)+1),
	}
	if paths.InventoryDir == "" {
		paths.InventoryDir = filepath.Join(hotplexHome, "skills", "builtin")
	}
	if paths.StateDir == "" {
		paths.StateDir = filepath.Join(hotplexHome, "state", "skills")
	}
	paths.InventoryDir, err = canonicalFSPath(fs, paths.InventoryDir)
	if err != nil {
		return Paths{}, fmt.Errorf("resolve inventory: %w", err)
	}
	paths.StateDir, err = canonicalFSPath(fs, paths.StateDir)
	if err != nil {
		return Paths{}, fmt.Errorf("resolve state: %w", err)
	}
	if !isWithin(hotplexHome, paths.InventoryDir) || !isWithin(hotplexHome, paths.StateDir) {
		return Paths{}, ErrInventoryOutsideHotplexHome
	}
	for workerType, root := range input.NativeRoots {
		if workerType == WorkerACP {
			continue
		}
		if !filepath.IsAbs(root) {
			return Paths{}, fmt.Errorf("%w: native root must be absolute", ErrRootOutsideHome)
		}
		paths.NativeRoots[workerType] = root
	}
	defaults := map[WorkerType]string{
		WorkerClaude:   filepath.Join(userHome, ".agents", "skills"),
		WorkerCodex:    filepath.Join(userHome, ".agents", "skills"),
		WorkerOpenCode: filepath.Join(userHome, ".agents", "skills"),
	}
	for workerType, root := range defaults {
		if _, exists := paths.NativeRoots[workerType]; !exists {
			paths.NativeRoots[workerType] = root
		}
	}
	for workerType, root := range paths.NativeRoots {
		canonicalRoot, rootErr := canonicalFSPath(fs, root)
		if rootErr != nil {
			return Paths{}, fmt.Errorf("resolve native root: %w", rootErr)
		}
		baseName := ".agents"
		base, baseErr := canonicalFSPath(fs, filepath.Join(userHome, baseName))
		if baseErr != nil {
			return Paths{}, fmt.Errorf("resolve native base: %w", baseErr)
		}
		if !isWithin(userHome, base) || !isWithin(base, canonicalRoot) {
			return Paths{}, fmt.Errorf("%w: %s", ErrRootOutsideHome, root)
		}
		paths.NativeRoots[workerType] = canonicalRoot
	}
	for workerType, root := range input.AliasRoots {
		if workerType != WorkerClaude {
			continue
		}
		if !filepath.IsAbs(root) {
			return Paths{}, fmt.Errorf("%w: alias root must be absolute", ErrRootOutsideHome)
		}
		aliasRoot := filepath.Clean(root)
		canonicalAlias, aliasErr := canonicalFSPath(fs, aliasRoot)
		if aliasErr != nil {
			return Paths{}, fmt.Errorf("resolve alias root: %w", aliasErr)
		}
		aliasBase, baseErr := canonicalFSPath(fs, filepath.Join(userHome, ".claude"))
		if baseErr != nil {
			return Paths{}, fmt.Errorf("resolve alias base: %w", baseErr)
		}
		centralRoot := paths.NativeRoots[WorkerCodex]
		if !isWithin(userHome, canonicalAlias) ||
			(!isWithin(aliasBase, canonicalAlias) && canonicalAlias != centralRoot) {
			return Paths{}, fmt.Errorf("%w: %s", ErrRootOutsideHome, aliasRoot)
		}
		paths.AliasRoots[workerType] = aliasRoot
	}
	return paths, nil
}

func canonicalFSPath(fs FileSystem, name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", errors.New("empty path")
	}
	abs, err := filepath.Abs(filepath.Clean(name))
	if err != nil {
		return "", err
	}
	return canonicalPathWith(abs, fs.Lstat, fs.EvalSymlinks)
}

func isExistingDir(fs FileSystem, name string) bool {
	info, err := fs.Lstat(name)
	return err == nil && info.IsDir()
}
