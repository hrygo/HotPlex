package skills

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
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

// ─── installMu 并发序列化（spec review P1：check-then-rename 竞态）──────────

func TestLocator_Install_ConcurrentReplaceIsSerialized(t *testing.T) {
	t.Parallel()
	l := newTestLocator()
	defer l.Close()

	workDir := t.TempDir()
	// makeZip 用 t.Fatal 必须在主 goroutine 调用；zip.Reader 只读，N 个 goroutine
	// 共享同一 zr 并发读安全（Install 各自解压到独立 staging，不修改 zr）。
	zr := makeZip(t, map[string]string{"SKILL.md": validFM})

	const n = 32
	var wg sync.WaitGroup
	errs := make([]error, n)
	start := make(chan struct{})
	wg.Add(n)
	for i := range n {
		go func() {
			defer wg.Done()
			<-start // 同步起跑，最大化 check-then-rename 竞态窗口
			_, errs[i] = l.Install(context.Background(), ScopeWorkspace, workDir, "", zr, true)
		}()
	}
	close(start)
	wg.Wait()

	// replace=true 并发：installMu 串行化 check-then-rename，每次都允许成功
	// （先到先装，后到覆盖），无静默丢数据 / ENOTEMPTY 误 500 / race panic。
	for i, e := range errs {
		require.NoError(t, e, "goroutine %d", i)
	}

	// 最终恰好一个 skill 存活且内容完整（无半成品 / 损坏）。
	d, err := l.Read(context.Background(), ScopeWorkspace, workDir, "my-skill")
	require.NoError(t, err)
	require.Equal(t, "my-skill", d.Name)
	require.Contains(t, d.Body, "# My Skill")

	// 落盘目录唯一（无 staging 残壳、无重复碎片）。
	entries, err := os.ReadDir(filepath.Join(workDir, ".agents", "skills"))
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

// ─── ListWorkspaceInstalled（issue #918：workspace-only 列表）───────────────

// writeReadonlySkill 在 dir/.claude/skills/<name> 落一个只读（非 managed）skill。
func writeReadonlySkill(t *testing.T, dir, name string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".claude", "skills", name), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, ".claude", "skills", name, "SKILL.md"),
		[]byte("---\nname: "+name+"\ndescription: read only\n---\n"), 0o644))
}

func TestLocator_ListWorkspaceInstalled_OnlyWorkspaceManaged(t *testing.T) {
	t.Parallel()
	l := newTestLocator()
	defer l.Close()

	homeDir := t.TempDir()
	workDir := t.TempDir()

	// 全局 managed skill（home/.agents/skills）——必须被排除。
	_, err := l.Install(context.Background(), ScopeGlobal, homeDir, "",
		makeZip(t, map[string]string{"SKILL.md": "---\nname: glob\ndescription: g\n---\n"}), false)
	require.NoError(t, err)
	// workspace managed skill（workDir/.agents/skills）——唯一应返回者。
	_, err = l.Install(context.Background(), ScopeWorkspace, workDir, homeDir,
		makeZip(t, map[string]string{"SKILL.md": validFM}), false)
	require.NoError(t, err)
	// workspace 只读 skill（workDir/.claude/skills）——必须被排除。
	writeReadonlySkill(t, workDir, "ro-skill")

	got, err := l.ListWorkspaceInstalled(context.Background(), workDir)
	require.NoError(t, err)
	require.Len(t, got, 1, "仅返回 workspace 安装的受管 skill")
	require.Equal(t, "my-skill", got[0].Name)
	require.True(t, got[0].Managed)
	require.Equal(t, SourceProject, got[0].Source)
}

func TestLocator_ListWorkspaceInstalled_ExcludesOtherWorkspace(t *testing.T) {
	t.Parallel()
	l := newTestLocator()
	defer l.Close()

	wsA, wsB := t.TempDir(), t.TempDir()
	_, err := l.Install(context.Background(), ScopeWorkspace, wsA, "",
		makeZip(t, map[string]string{"SKILL.md": validFM}), false)
	require.NoError(t, err)

	got, err := l.ListWorkspaceInstalled(context.Background(), wsB)
	require.NoError(t, err)
	require.Empty(t, got, "其他 workspace 的 skill 不得出现")
}

func TestLocator_ListWorkspaceInstalled_EmptyOrMissingDir(t *testing.T) {
	t.Parallel()
	l := newTestLocator()
	defer l.Close()

	// 空 workDir（无 .agents/skills）→ 空切片、无错误（非 nil，JSON 序列化为 []）。
	got, err := l.ListWorkspaceInstalled(context.Background(), t.TempDir())
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Empty(t, got)

	// 空 workDir 字符串 → 空切片、无错误。
	got, err = l.ListWorkspaceInstalled(context.Background(), "")
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestLocator_ListWorkspaceInstalled_InvalidatedByWrite(t *testing.T) {
	t.Parallel()
	l := newTestLocator()
	defer l.Close()

	workDir := t.TempDir()
	// 预热 workspace-installed 缓存（空）。
	_, err := l.ListWorkspaceInstalled(context.Background(), workDir)
	require.NoError(t, err)
	require.Contains(t, l.cache, wsInstalledCacheKey(workDir))

	// workspace 写（Install）经 Invalidate 同步失效该键。
	_, err = l.Install(context.Background(), ScopeWorkspace, workDir, "",
		makeZip(t, map[string]string{"SKILL.md": validFM}), false)
	require.NoError(t, err)
	_, cached := l.cache[wsInstalledCacheKey(workDir)]
	require.False(t, cached, "Install 必须失效 workspace-installed 缓存键")

	// 再次列表反映最新（不返回陈旧空列表）。
	got, err := l.ListWorkspaceInstalled(context.Background(), workDir)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "my-skill", got[0].Name)

	// Delete 同样失效该键。
	require.NoError(t, l.Delete(context.Background(), ScopeWorkspace, workDir, "my-skill"))
	got, err = l.ListWorkspaceInstalled(context.Background(), workDir)
	require.NoError(t, err)
	require.Empty(t, got)
}
