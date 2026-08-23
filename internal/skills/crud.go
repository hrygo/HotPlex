package skills

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Scope 决定 skill 写入的物理位置（spec §3.2）。
type Scope string

const (
	ScopeGlobal    Scope = "global"    // 写 ~/.agents/skills
	ScopeWorkspace Scope = "workspace" // 写 <workspaceDir>/.agents/skills
)

// CRUD 语义错误。handler 层映射 HTTP 错误码（spec §5）：
//   - ErrSkillAlreadyExists → 409 SKILL_ALREADY_EXISTS
//   - ErrSkillNotFound      → 404 SKILL_NOT_FOUND
var (
	ErrSkillAlreadyExists   = errors.New("skill: already exists")
	ErrSkillNotFound        = errors.New("skill: not found")
	ErrSkillBuiltinReadonly = errors.New("skill: builtin is read-only")
)

// Detail 是单个 skill 的完整内容（spec §3.2）。
type Detail struct {
	Skill
	Body  string   `json:"body"`  // SKILL.md 全文
	Files []string `json:"files"` // 包内文件相对路径
}

// InstallResult 是 Install 的返回，含跨 scope 遮蔽 warning（spec §3.3 B6）。
type InstallResult struct {
	Detail
	Warning string `json:"warning,omitempty"` // 非空 = workspace 安装撞全局，UI 提示
}

// scopeSource 将 Scope 映射到 wire 语义的 Source 值（spec §3.2 / §9.1：保留 global/project）。
func scopeSource(s Scope) string {
	if s == ScopeGlobal {
		return SourceGlobal
	}
	return SourceProject
}

// managedDir 返回某 baseDir 下 managed skill 存储根目录（spec §3.1：.agents/skills）。
func managedDir(baseDir string) string {
	return filepath.Join(baseDir, ".agents", "skills")
}

// hasManagedSkill 报告 baseDir 的 managed 区是否已存在同名 skill。
func hasManagedSkill(baseDir, name string) bool {
	md, ok := findSkillMD(filepath.Join(managedDir(baseDir), name))
	return ok && filepath.Base(filepath.Dir(md)) == name
}

// Install 从 zip.Reader 安装 skill：解压+校验 → 原子落盘 → 缓存失效（spec §3.2/§3.3）。
//
// baseDir：global 传 homeDir，workspace 传 workspaceDir。homeDir 仅用于 workspace
// scope 的跨 scope 遮蔽 warning（global scope 可空）。replace=true 覆盖同名（先删后建）。
//
// 落盘语义：解压到与目标同文件系统的 staging 目录，校验通过后 os.Rename 原子替换，
// 任一步失败回滚 staging，不留半成品（spec §3.3 C）。
func (l *Locator) Install(_ context.Context, scope Scope, baseDir, homeDir string, zr *zip.Reader, replace bool) (*InstallResult, error) {
	if baseDir == "" {
		return nil, fmt.Errorf("skills: baseDir must not be empty")
	}

	destRoot := managedDir(baseDir)
	if err := os.MkdirAll(destRoot, 0o755); err != nil {
		return nil, fmt.Errorf("skills: mkdir skills root: %w", err)
	}

	// staging 与目标同文件系统（均在 baseDir 下），保证 Rename 原子。
	staging, err := os.MkdirTemp(baseDir, ".skill-install-*")
	if err != nil {
		return nil, fmt.Errorf("skills: create staging: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(staging) }

	es, extractErr := extractZip(zr, staging)
	if extractErr != nil {
		cleanup()
		return nil, extractErr
	}

	// check+rename 关键段持 installMu：消除并发 replace 的静默数据丢失（T2
	// RemoveAll 删 T1 刚 Rename 的目录）与并发新装的 TOCTOU（T2 Rename ENOTEMPTY
	// → 误 500）。extractZip 在锁外（IO 重）；skill 写低频，全局锁足够。
	l.installMu.Lock()
	defer l.installMu.Unlock()

	destDir := filepath.Join(destRoot, es.name)

	// 同 scope 同名校验（spec §3.3 B5）。
	if !replace && hasManagedSkill(baseDir, es.name) {
		cleanup()
		return nil, fmt.Errorf("%w: %s", ErrSkillAlreadyExists, es.name)
	}

	// 跨 scope 遮蔽 warning（spec §3.3 B6）：workspace 安装撞全局 → 允许但提示。
	var warning string
	if scope == ScopeWorkspace && homeDir != "" && hasManagedSkill(homeDir, es.name) {
		warning = fmt.Sprintf("shadows global skill '%s'", es.name)
	}

	// 确定 staging 内 skill 内容根：扁平=staging；单顶层=staging/<name>。
	contentRoot := staging
	if es.rootRel != "" {
		contentRoot = filepath.Join(staging, es.rootRel)
	}

	// 原子落盘（spec §3.3 C）。replace 时先删旧目录；destDir 父目录已存在。
	if replace {
		_ = os.RemoveAll(destDir)
	}
	if err := os.Rename(contentRoot, destDir); err != nil {
		// 持锁后 TOCTOU 已消除；ENOTEMPTY 仅在 destDir 残留时发生，重新分类为
		// AlreadyExists 给客户端契约化 409（而非通用 500）。
		if !replace && hasManagedSkill(baseDir, es.name) {
			cleanup()
			return nil, fmt.Errorf("%w: %s", ErrSkillAlreadyExists, es.name)
		}
		cleanup()
		return nil, fmt.Errorf("skills: install (rename): %w", err)
	}
	cleanup() // 清理 staging 残壳（单顶层时 staging 非空）

	l.invalidateScope(scope, baseDir)

	return &InstallResult{
		Detail: Detail{
			Skill: Skill{
				Name:        es.name,
				Description: es.description,
				Source:      scopeSource(scope),
				Managed:     true,
				FilePath:    filepath.Join(destDir, es.skillMDName),
			},
			Body:  es.body,
			Files: es.files,
		},
		Warning: warning,
	}, nil
}

// Read 返回指定 scope/name 的 skill 详情（含 SKILL.md 全文与包内文件列表）。
func (l *Locator) Read(_ context.Context, scope Scope, baseDir, name string) (*Detail, error) {
	if baseDir == "" {
		return nil, fmt.Errorf("%w: empty baseDir", ErrInvalidFormat)
	}
	if !skillNameRegexp.MatchString(name) {
		return nil, fmt.Errorf("%w: invalid name %q", ErrInvalidFormat, name)
	}
	dir := filepath.Join(managedDir(baseDir), name)
	md, ok := findSkillMD(dir)
	if !ok || filepath.Base(filepath.Dir(md)) != name {
		return nil, fmt.Errorf("%w: %s", ErrSkillNotFound, name)
	}
	data, err := os.ReadFile(md)
	if err != nil {
		return nil, fmt.Errorf("skills: read: %w", err)
	}
	fm := extractFrontmatter(data)
	if fm == nil || strings.TrimSpace(fm.Name) == "" {
		return nil, fmt.Errorf("%w: corrupt frontmatter", ErrInvalidFormat)
	}
	desc := CollapseSpaces(strings.ReplaceAll(strings.TrimSpace(fm.Description), "\n", " "))
	files, _ := collectFiles(dir)
	return &Detail{
		Skill: Skill{
			Name:        strings.TrimSpace(fm.Name),
			Description: desc,
			Source:      scopeSource(scope),
			Managed:     true,
			FilePath:    md,
		},
		Body:  string(data),
		Files: files,
	}, nil
}

// Delete 移除指定 scope/name 的 managed skill 目录并失效缓存。
// name 正则保证无路径分隔符/".."，删除严格落在 managedDir(baseDir)/<name> 内。
func (l *Locator) Delete(_ context.Context, scope Scope, baseDir, name string) error {
	if baseDir == "" {
		return fmt.Errorf("%w: empty baseDir", ErrInvalidFormat)
	}
	if !skillNameRegexp.MatchString(name) {
		return fmt.Errorf("%w: invalid name %q", ErrInvalidFormat, name)
	}
	// 持 installMu 与 Install 互斥：防并发 install+delete 同名的竞态。
	l.installMu.Lock()
	defer l.installMu.Unlock()

	destRoot := managedDir(baseDir)
	dir := filepath.Join(destRoot, name)
	// 双保险：解析后必须在 destRoot 内（name 已过正则，此处冗余防御）。
	if !strings.HasPrefix(dir, destRoot+string(filepath.Separator)) {
		return fmt.Errorf("%w: invalid name %q", ErrInvalidFormat, name)
	}
	if !hasManagedSkill(baseDir, name) {
		return fmt.Errorf("%w: %s", ErrSkillNotFound, name)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("skills: delete: %w", err)
	}
	l.invalidateScope(scope, baseDir)
	return nil
}

// Update 重新写入指定 managed skill 的 SKILL.md 内容并更新/校验 frontmatter。
func (l *Locator) Update(_ context.Context, scope Scope, baseDir, name, body string) (*Detail, error) {
	if baseDir == "" {
		return nil, fmt.Errorf("%w: empty baseDir", ErrInvalidFormat)
	}
	if !skillNameRegexp.MatchString(name) {
		return nil, fmt.Errorf("%w: invalid name %q", ErrInvalidFormat, name)
	}
	fm := extractFrontmatter([]byte(body))
	if fm == nil || strings.TrimSpace(fm.Name) == "" {
		return nil, fmt.Errorf("%w: missing or invalid frontmatter", ErrInvalidFormat)
	}

	l.installMu.Lock()
	defer l.installMu.Unlock()

	if !hasManagedSkill(baseDir, name) {
		return nil, fmt.Errorf("%w: %s", ErrSkillNotFound, name)
	}

	destRoot := managedDir(baseDir)
	dir := filepath.Join(destRoot, name)
	mdPath, ok := findSkillMD(dir)
	if !ok {
		mdPath = filepath.Join(dir, "SKILL.md")
	}

	if err := os.WriteFile(mdPath, []byte(body), 0o644); err != nil {
		return nil, fmt.Errorf("skills: update write: %w", err)
	}

	l.invalidateScope(scope, baseDir)

	files, _ := collectFiles(dir)
	desc := CollapseSpaces(strings.ReplaceAll(strings.TrimSpace(fm.Description), "\n", " "))
	return &Detail{
		Skill: Skill{
			Name:        strings.TrimSpace(fm.Name),
			Description: desc,
			Source:      scopeSource(scope),
			Managed:     true,
			FilePath:    mdPath,
		},
		Body:  body,
		Files: files,
	}, nil
}

// CreateText 根据文本内容（SKILL.md）创建新 managed skill。
func (l *Locator) CreateText(_ context.Context, scope Scope, baseDir, homeDir, name, body string, replace bool) (*InstallResult, error) {
	if baseDir == "" {
		return nil, fmt.Errorf("%w: empty baseDir", ErrInvalidFormat)
	}
	fm := extractFrontmatter([]byte(body))
	if fm == nil || strings.TrimSpace(fm.Name) == "" {
		return nil, fmt.Errorf("%w: missing or invalid frontmatter", ErrInvalidFormat)
	}
	if name == "" {
		name = strings.TrimSpace(fm.Name)
	}
	if !skillNameRegexp.MatchString(name) {
		return nil, fmt.Errorf("%w: invalid name %q", ErrInvalidFormat, name)
	}

	l.installMu.Lock()
	defer l.installMu.Unlock()

	destRoot := managedDir(baseDir)
	if err := os.MkdirAll(destRoot, 0o755); err != nil {
		return nil, fmt.Errorf("skills: mkdir skills root: %w", err)
	}

	if !replace && hasManagedSkill(baseDir, name) {
		return nil, fmt.Errorf("%w: %s", ErrSkillAlreadyExists, name)
	}

	var warning string
	if scope == ScopeWorkspace && homeDir != "" && hasManagedSkill(homeDir, name) {
		warning = fmt.Sprintf("shadows global skill '%s'", name)
	}

	destDir := filepath.Join(destRoot, name)
	if replace {
		_ = os.RemoveAll(destDir)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, fmt.Errorf("skills: mkdir skill dir: %w", err)
	}

	mdPath := filepath.Join(destDir, "SKILL.md")
	if err := os.WriteFile(mdPath, []byte(body), 0o644); err != nil {
		return nil, fmt.Errorf("skills: create write: %w", err)
	}

	l.invalidateScope(scope, baseDir)

	files, _ := collectFiles(destDir)
	desc := CollapseSpaces(strings.ReplaceAll(strings.TrimSpace(fm.Description), "\n", " "))
	return &InstallResult{
		Detail: Detail{
			Skill: Skill{
				Name:        strings.TrimSpace(fm.Name),
				Description: desc,
				Source:      scopeSource(scope),
				Managed:     true,
				FilePath:    mdPath,
			},
			Body:  body,
			Files: files,
		},
		Warning: warning,
	}, nil
}

// invalidateScope 按 scope 选择缓存失效策略（spec §3.2）：
// 全局写影响所有 workspace 的合并列表 → InvalidateAll；workspace 写仅清自身。
func (l *Locator) invalidateScope(scope Scope, baseDir string) {
	if scope == ScopeGlobal {
		l.InvalidateAll()
		return
	}
	l.Invalidate(baseDir)
}
