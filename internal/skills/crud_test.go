package skills

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newTestLocator() *Locator {
	return NewLocator(slog.Default(), time.Minute)
}

// ─── Install + Read（合法结构）──────────────────────────────────────────────

func TestLocator_Install_FlatThenRead(t *testing.T) {
	t.Parallel()
	l := newTestLocator()
	defer l.Close()

	homeDir := t.TempDir()
	workDir := t.TempDir()
	zr := makeZip(t, map[string]string{"SKILL.md": validFM})

	res, err := l.Install(context.Background(), ScopeWorkspace, workDir, homeDir, zr, false)
	require.NoError(t, err)
	require.Equal(t, "my-skill", res.Name)
	require.True(t, res.Managed)
	require.Equal(t, SourceProject, res.Source)
	require.Empty(t, res.Warning)
	require.Contains(t, res.Files, "SKILL.md")
	require.Contains(t, res.Body, "# My Skill")

	d, err := l.Read(context.Background(), ScopeWorkspace, workDir, "my-skill")
	require.NoError(t, err)
	require.Equal(t, "my-skill", d.Name)
	require.Equal(t, "a useful skill", d.Description)
	require.True(t, d.Managed)
}

func TestLocator_Install_SingleTopDir(t *testing.T) {
	t.Parallel()
	l := newTestLocator()
	defer l.Close()

	zr := makeZip(t, map[string]string{
		"my-skill/SKILL.md":     validFM,
		"my-skill/reference.md": "# ref\n",
	})
	res, err := l.Install(context.Background(), ScopeGlobal, t.TempDir(), "", zr, false)
	require.NoError(t, err)
	require.Equal(t, "my-skill", res.Name)
	require.Equal(t, SourceGlobal, res.Source)
	require.Len(t, res.Files, 2)
}

// ─── replace 语义（spec §3.3 B5）────────────────────────────────────────────

func TestLocator_Install_AlreadyExistsNoReplace(t *testing.T) {
	t.Parallel()
	l := newTestLocator()
	defer l.Close()

	workDir := t.TempDir()
	zr := makeZip(t, map[string]string{"SKILL.md": validFM})
	_, err := l.Install(context.Background(), ScopeWorkspace, workDir, "", zr, false)
	require.NoError(t, err)

	_, err = l.Install(context.Background(), ScopeWorkspace, workDir, "", zr, false)
	require.ErrorIs(t, err, ErrSkillAlreadyExists)
}

func TestLocator_Install_ReplaceOverwrites(t *testing.T) {
	t.Parallel()
	l := newTestLocator()
	defer l.Close()

	workDir := t.TempDir()
	_, err := l.Install(context.Background(), ScopeWorkspace, workDir, "",
		makeZip(t, map[string]string{"SKILL.md": "---\nname: my-skill\ndescription: v1\n---\n"}), false)
	require.NoError(t, err)

	_, err = l.Install(context.Background(), ScopeWorkspace, workDir, "",
		makeZip(t, map[string]string{"SKILL.md": "---\nname: my-skill\ndescription: v2\n---\n"}), true)
	require.NoError(t, err)

	d, err := l.Read(context.Background(), ScopeWorkspace, workDir, "my-skill")
	require.NoError(t, err)
	require.Equal(t, "v2", d.Description)
}

// ─── 跨 scope 遮蔽 warning（spec §3.3 B6）──────────────────────────────────

func TestLocator_Install_WorkspaceShadowsGlobal(t *testing.T) {
	t.Parallel()
	l := newTestLocator()
	defer l.Close()

	homeDir := t.TempDir()
	workDir := t.TempDir()
	fm := "---\nname: shared\ndescription: d\n---\n"
	// 全局先装 shared。
	_, err := l.Install(context.Background(), ScopeGlobal, homeDir, "", makeZip(t, map[string]string{"SKILL.md": fm}), false)
	require.NoError(t, err)
	// workspace 装同名 shared → 允许但 warning。
	res, err := l.Install(context.Background(), ScopeWorkspace, workDir, homeDir, makeZip(t, map[string]string{"SKILL.md": fm}), false)
	require.NoError(t, err)
	require.Contains(t, res.Warning, "shadows global skill 'shared'")
}

func TestLocator_Install_WorkspaceNoShadowWhenAbsent(t *testing.T) {
	t.Parallel()
	l := newTestLocator()
	defer l.Close()
	res, err := l.Install(context.Background(), ScopeWorkspace, t.TempDir(), t.TempDir(),
		makeZip(t, map[string]string{"SKILL.md": validFM}), false)
	require.NoError(t, err)
	require.Empty(t, res.Warning)
}

// ─── Read / Delete ──────────────────────────────────────────────────────────

func TestLocator_Read_NotFound(t *testing.T) {
	t.Parallel()
	l := newTestLocator()
	defer l.Close()
	_, err := l.Read(context.Background(), ScopeWorkspace, t.TempDir(), "missing")
	require.ErrorIs(t, err, ErrSkillNotFound)
}

func TestLocator_Read_InvalidName(t *testing.T) {
	t.Parallel()
	l := newTestLocator()
	defer l.Close()
	_, err := l.Read(context.Background(), ScopeWorkspace, t.TempDir(), "../escape")
	require.ErrorIs(t, err, ErrInvalidFormat)
}

func TestLocator_Delete(t *testing.T) {
	t.Parallel()
	l := newTestLocator()
	defer l.Close()

	workDir := t.TempDir()
	_, err := l.Install(context.Background(), ScopeWorkspace, workDir, "", makeZip(t, map[string]string{"SKILL.md": validFM}), false)
	require.NoError(t, err)

	require.NoError(t, l.Delete(context.Background(), ScopeWorkspace, workDir, "my-skill"))
	_, err = l.Read(context.Background(), ScopeWorkspace, workDir, "my-skill")
	require.ErrorIs(t, err, ErrSkillNotFound)
}

func TestLocator_Delete_NotFound(t *testing.T) {
	t.Parallel()
	l := newTestLocator()
	defer l.Close()
	err := l.Delete(context.Background(), ScopeWorkspace, t.TempDir(), "ghost")
	require.ErrorIs(t, err, ErrSkillNotFound)
}

func TestLocator_Delete_InvalidName(t *testing.T) {
	t.Parallel()
	l := newTestLocator()
	defer l.Close()
	err := l.Delete(context.Background(), ScopeWorkspace, t.TempDir(), "../x")
	require.ErrorIs(t, err, ErrInvalidFormat)
}

// ─── 缓存失效策略（spec §3.2 b）────────────────────────────────────────────

func TestLocator_Install_WorkspaceInvalidatesOwn(t *testing.T) {
	t.Parallel()
	l := newTestLocator()
	defer l.Close()

	homeDir := t.TempDir()
	workDir := t.TempDir()
	// 预热 workDir 缓存。
	_, err := l.List(context.Background(), homeDir, workDir)
	require.NoError(t, err)
	require.Contains(t, l.cache, workDir)

	_, err = l.Install(context.Background(), ScopeWorkspace, workDir, homeDir, makeZip(t, map[string]string{"SKILL.md": validFM}), false)
	require.NoError(t, err)
	// workspace 写仅清自身。
	_, ok := l.cache[workDir]
	require.False(t, ok, "workspace Install 必须失效该 workDir 缓存")
}

func TestLocator_Install_GlobalInvalidatesAll(t *testing.T) {
	t.Parallel()
	l := newTestLocator()
	defer l.Close()

	homeDir := t.TempDir()
	wd1, wd2 := t.TempDir(), t.TempDir()
	// 预热两个 workDir 缓存。
	_, _ = l.List(context.Background(), homeDir, wd1)
	_, _ = l.List(context.Background(), homeDir, wd2)
	require.Len(t, l.cache, 2)

	_, err := l.Install(context.Background(), ScopeGlobal, homeDir, "", makeZip(t, map[string]string{"SKILL.md": validFM}), false)
	require.NoError(t, err)
	require.Empty(t, l.cache, "global Install 必须 InvalidateAll")
}

// ─── 错误不落盘（原子回滚，spec §3.3 C）────────────────────────────────────

func TestLocator_Install_FailureLeavesNoTrace(t *testing.T) {
	t.Parallel()
	l := newTestLocator()
	defer l.Close()

	workDir := t.TempDir()
	// 非法 zip（缺 SKILL.md）→ Install 必须失败且不创建目录。
	_, err := l.Install(context.Background(), ScopeWorkspace, workDir, "", makeZip(t, map[string]string{"notes.md": "# x\n"}), false)
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrSkillAlreadyExists))

	d, statErr := l.Read(context.Background(), ScopeWorkspace, workDir, "my-skill")
	require.ErrorIs(t, statErr, ErrSkillNotFound)
	_ = d
}
