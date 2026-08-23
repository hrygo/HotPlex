package gateway

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/skills"
	"github.com/hrygo/hotplex/internal/skills/builtin"
)

const testSkillFM = "---\nname: my-skill\ndescription: a useful skill\n---\n# Body\n"

// newSkillHandlersEnv 构造隔离的 SkillHandlers 测试环境：workspace 与 home 均落在
// t.TempDir()，避免触碰真实文件系统。owner=u-admin（admin 登录）。
func newSkillHandlersEnv(t *testing.T) (*SkillHandlers, string, string, string) {
	t.Helper()
	env := newTestAuthEnv(t)
	cookie := env.loginAs(t, "admin", "adminpass", http.StatusOK)

	workDir := t.TempDir()
	ws := &session.Workspace{ID: "ws-skill", OwnerUserID: "u-admin", Name: "test-ws", WorkDir: workDir, Status: "active"}
	require.NoError(t, env.store.CreateWorkspace(context.Background(), ws, 1700000000))

	home := t.TempDir()
	locator := skills.NewLocator(slog.Default(), time.Minute)
	t.Cleanup(locator.Close)

	sh := NewSkillHandlers(locator, env.store, env.auth, slog.Default())
	sh.homeFn = func() string { return home }
	return sh, workDir, home, cookie
}

// skillZipBody 构造 multipart/form-data body，file 字段为内联 zip。
func skillZipBody(t *testing.T, files map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	fw, err := mw.CreateFormFile("file", "skill.zip")
	require.NoError(t, err)
	zw := zip.NewWriter(fw)
	for name, content := range files {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	require.NoError(t, mw.Close())
	return body, mw.FormDataContentType()
}

func skillReq(method, target, cookie string, body io.Reader, contentType string) *http.Request {
	req := httptest.NewRequest(method, target, body)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req
}

// installWorkspace 调用 InstallWorkspace，返回 recorder。
func installWorkspace(t *testing.T, sh *SkillHandlers, cookie string, files map[string]string, replace bool) *httptest.ResponseRecorder {
	t.Helper()
	body, ct := skillZipBody(t, files)
	target := "/api/workspaces/ws-skill/skills"
	if replace {
		target += "?replace=true"
	}
	req := skillReq(http.MethodPost, target, cookie, body, ct)
	req.SetPathValue("wid", "ws-skill")
	w := httptest.NewRecorder()
	sh.InstallWorkspace(w, req)
	return w
}

func TestSkillHandlers_InstallAndGet(t *testing.T) {
	t.Parallel()
	sh, workDir, _, cookie := newSkillHandlersEnv(t)

	w := installWorkspace(t, sh, cookie, map[string]string{"SKILL.md": testSkillFM}, false)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.FileExists(t, filepath.Join(workDir, ".agents", "skills", "my-skill", "SKILL.md"))

	// Get 详情
	req := skillReq(http.MethodGet, "/api/workspaces/ws-skill/skills/my-skill", cookie, nil, "")
	req.SetPathValue("wid", "ws-skill")
	req.SetPathValue("name", "my-skill")
	gw := httptest.NewRecorder()
	sh.GetWorkspace(gw, req)
	require.Equal(t, http.StatusOK, gw.Code)
	require.Contains(t, gw.Body.String(), "my-skill")
	require.Contains(t, gw.Body.String(), "a useful skill")
}

func TestSkillHandlers_NotOwner(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	// 另一个普通用户
	env.createUser(t, "alice", "alicepass", "user")
	aliceCookie := env.loginAs(t, "alice", "alicepass", http.StatusOK)

	workDir := t.TempDir()
	ws := &session.Workspace{ID: "ws-skill", OwnerUserID: "u-admin", Name: "test-ws", WorkDir: workDir, Status: "active"}
	require.NoError(t, env.store.CreateWorkspace(context.Background(), ws, 1700000000))

	locator := skills.NewLocator(slog.Default(), time.Minute)
	t.Cleanup(locator.Close)
	sh := NewSkillHandlers(locator, env.store, env.auth, slog.Default())
	sh.homeFn = func() string { return t.TempDir() }

	body, ct := skillZipBody(t, map[string]string{"SKILL.md": testSkillFM})
	req := skillReq(http.MethodPost, "/api/workspaces/ws-skill/skills", aliceCookie, body, ct)
	req.SetPathValue("wid", "ws-skill")
	w := httptest.NewRecorder()
	sh.InstallWorkspace(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "WORKSPACE_FORBIDDEN")
}

func TestSkillHandlers_AdminMayManageOthersWorkspace(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	env.createUser(t, "bob", "bobpass", "user")
	// bob 拥有的 workspace（直接持久化）
	workDir := t.TempDir()
	ws := &session.Workspace{ID: "ws-bob", OwnerUserID: "u-bob", Name: "bob-ws", WorkDir: workDir, Status: "active"}
	require.NoError(t, env.store.CreateWorkspace(context.Background(), ws, 1700000000))

	locator := skills.NewLocator(slog.Default(), time.Minute)
	t.Cleanup(locator.Close)
	sh := NewSkillHandlers(locator, env.store, env.auth, slog.Default())
	sh.homeFn = func() string { return t.TempDir() }

	// admin 登录后管理 bob 的 workspace skill
	adminCookie := env.loginAs(t, "admin", "adminpass", http.StatusOK)
	body, ct := skillZipBody(t, map[string]string{"SKILL.md": testSkillFM})
	req := skillReq(http.MethodPost, "/api/workspaces/ws-bob/skills", adminCookie, body, ct)
	req.SetPathValue("wid", "ws-bob")
	w := httptest.NewRecorder()
	sh.InstallWorkspace(w, req)
	require.Equal(t, http.StatusOK, w.Code, "admin 应可管理任意 workspace skill")
}

func TestSkillHandlers_NoFileField(t *testing.T) {
	t.Parallel()
	sh, _, _, cookie := newSkillHandlersEnv(t)
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	require.NoError(t, mw.WriteField("notfile", "x"))
	require.NoError(t, mw.Close())
	req := skillReq(http.MethodPost, "/api/workspaces/ws-skill/skills", cookie, body, mw.FormDataContentType())
	req.SetPathValue("wid", "ws-skill")
	w := httptest.NewRecorder()
	sh.InstallWorkspace(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "SKILL_INVALID_ZIP")
}

func TestSkillHandlers_AlreadyExistsNoReplace(t *testing.T) {
	t.Parallel()
	sh, _, _, cookie := newSkillHandlersEnv(t)
	require.Equal(t, http.StatusOK, installWorkspace(t, sh, cookie, map[string]string{"SKILL.md": testSkillFM}, false).Code)
	w := installWorkspace(t, sh, cookie, map[string]string{"SKILL.md": testSkillFM}, false)
	require.Equal(t, http.StatusConflict, w.Code)
	require.Contains(t, w.Body.String(), "SKILL_ALREADY_EXISTS")
}

func TestSkillHandlers_ReplaceOverwrites(t *testing.T) {
	t.Parallel()
	sh, _, _, cookie := newSkillHandlersEnv(t)
	require.Equal(t, http.StatusOK, installWorkspace(t, sh, cookie, map[string]string{"SKILL.md": testSkillFM}, false).Code)
	w := installWorkspace(t, sh, cookie, map[string]string{"SKILL.md": "---\nname: my-skill\ndescription: v2\n---\n"}, true)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "v2")
}

func TestSkillHandlers_Delete(t *testing.T) {
	t.Parallel()
	sh, workDir, _, cookie := newSkillHandlersEnv(t)
	require.Equal(t, http.StatusOK, installWorkspace(t, sh, cookie, map[string]string{"SKILL.md": testSkillFM}, false).Code)

	req := skillReq(http.MethodDelete, "/api/workspaces/ws-skill/skills/my-skill", cookie, nil, "")
	req.SetPathValue("wid", "ws-skill")
	req.SetPathValue("name", "my-skill")
	w := httptest.NewRecorder()
	sh.DeleteWorkspace(w, req)
	require.Equal(t, http.StatusNoContent, w.Code)
	require.NoFileExists(t, filepath.Join(workDir, ".agents", "skills", "my-skill", "SKILL.md"))
}

func TestSkillHandlers_DeleteNotFound(t *testing.T) {
	t.Parallel()
	sh, _, _, cookie := newSkillHandlersEnv(t)
	req := skillReq(http.MethodDelete, "/api/workspaces/ws-skill/skills/ghost", cookie, nil, "")
	req.SetPathValue("wid", "ws-skill")
	req.SetPathValue("name", "ghost")
	w := httptest.NewRecorder()
	sh.DeleteWorkspace(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestSkillHandlers_ListMerged(t *testing.T) {
	t.Parallel()
	sh, _, home, cookie := newSkillHandlersEnv(t)
	// global skill（home managed 区）
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".agents", "skills", "glob"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".agents", "skills", "glob", "SKILL.md"),
		[]byte("---\nname: glob\ndescription: a global skill\n---\n"), 0o644))
	// workspace skill
	require.Equal(t, http.StatusOK, installWorkspace(t, sh, cookie, map[string]string{"SKILL.md": testSkillFM}, false).Code)

	req := skillReq(http.MethodGet, "/api/skills", cookie, nil, "")
	w := httptest.NewRecorder()
	sh.ListMerged(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "glob")
	require.Contains(t, w.Body.String(), "my-skill")
	// global skill 标注 managed
	require.Contains(t, w.Body.String(), `"managed":true`)
}

func TestSkillHandlers_RequiresAuth(t *testing.T) {
	t.Parallel()
	sh, _, _, _ := newSkillHandlersEnv(t)
	req := httptest.NewRequest(http.MethodGet, "/api/skills", nil) // 无 cookie
	w := httptest.NewRecorder()
	sh.ListMerged(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestSkillHandlers_GetMerged_NotFound(t *testing.T) {
	t.Parallel()
	sh, _, _, cookie := newSkillHandlersEnv(t)
	req := skillReq(http.MethodGet, "/api/skills/missing", cookie, nil, "")
	req.SetPathValue("name", "missing")
	w := httptest.NewRecorder()
	sh.GetMerged(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
	require.Contains(t, w.Body.String(), "SKILL_NOT_FOUND")
}

func TestWebChatMergedListIncludesUniqueBuiltinAsReadOnly(t *testing.T) {
	t.Parallel()
	sh, _, _, cookie := newSkillHandlersEnv(t)
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	sh.SetBuiltinSkillsCatalog(builtin.NewPublicCatalog(registry))

	req := skillReq(http.MethodGet, "/api/skills", cookie, nil, "")
	w := httptest.NewRecorder()
	sh.ListMerged(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"name":"hotplex-cli"`)
	require.Contains(t, w.Body.String(), `"builtin":true`)
	require.Contains(t, w.Body.String(), `"managed":false`)
}

func TestWebChatMergedProjectSkillShadowsBuiltin(t *testing.T) {
	t.Parallel()
	sh, _, _, cookie := newSkillHandlersEnv(t)
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	sh.SetBuiltinSkillsCatalog(builtin.NewPublicCatalog(registry))
	require.Equal(t, http.StatusOK, installWorkspace(t, sh, cookie, map[string]string{
		"SKILL.md": "---\nname: hotplex-cli\ndescription: project override\n---\n# override\n",
	}, false).Code)

	listReq := skillReq(http.MethodGet, "/api/skills", cookie, nil, "")
	listRR := httptest.NewRecorder()
	sh.ListMerged(listRR, listReq)
	require.Equal(t, http.StatusOK, listRR.Code)
	var response struct {
		Skills []skills.Skill `json:"skills"`
	}
	require.NoError(t, json.Unmarshal(listRR.Body.Bytes(), &response))
	var matches []skills.Skill
	for _, skill := range response.Skills {
		if skill.Name == "hotplex-cli" {
			matches = append(matches, skill)
		}
	}
	require.Len(t, matches, 1)
	require.Equal(t, skills.SourceProject, matches[0].Source)
	require.Equal(t, "project override", matches[0].Description)
	require.False(t, matches[0].Builtin)
}

func TestWebChatGetMergedBuiltinAndProjectPrecedence(t *testing.T) {
	t.Parallel()
	sh, _, _, cookie := newSkillHandlersEnv(t)
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	sh.SetBuiltinSkillsCatalog(builtin.NewPublicCatalog(registry))

	builtinReq := skillReq(http.MethodGet, "/api/skills/hotplex-cli", cookie, nil, "")
	builtinReq.SetPathValue("name", "hotplex-cli")
	builtinRR := httptest.NewRecorder()
	sh.GetMerged(builtinRR, builtinReq)
	require.Equal(t, http.StatusOK, builtinRR.Code)
	var builtinSkill skills.Skill
	require.NoError(t, json.Unmarshal(builtinRR.Body.Bytes(), &builtinSkill))
	require.True(t, builtinSkill.Builtin)

	require.Equal(t, http.StatusOK, installWorkspace(t, sh, cookie, map[string]string{
		"SKILL.md": "---\nname: hotplex-cli\ndescription: project get override\n---\n# override\n",
	}, false).Code)
	projectReq := skillReq(http.MethodGet, "/api/skills/hotplex-cli", cookie, nil, "")
	projectReq.SetPathValue("name", "hotplex-cli")
	projectRR := httptest.NewRecorder()
	sh.GetMerged(projectRR, projectReq)
	require.Equal(t, http.StatusOK, projectRR.Code)
	var projectSkill skills.Skill
	require.NoError(t, json.Unmarshal(projectRR.Body.Bytes(), &projectSkill))
	require.Equal(t, skills.SourceProject, projectSkill.Source)
	require.Equal(t, "project get override", projectSkill.Description)
	require.False(t, projectSkill.Builtin)
}

func TestWorkspaceSkillCRUDNeverMutatesBuiltinInventory(t *testing.T) {
	t.Parallel()
	sh, workDir, _, cookie := newSkillHandlersEnv(t)
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	sh.SetBuiltinSkillsCatalog(builtin.NewPublicCatalog(registry))

	// Workspace management remains workspace-only, while a same-name user
	// override can be installed normally.
	body := map[string]string{"SKILL.md": "---\nname: hotplex-cli\ndescription: workspace override\n---\n# override\n"}
	require.Equal(t, http.StatusOK, installWorkspace(t, sh, cookie, body, false).Code)
	require.FileExists(t, filepath.Join(workDir, ".agents", "skills", "hotplex-cli", "SKILL.md"))

	w := httptest.NewRecorder()
	sh.ListWorkspace(w, listWorkspaceReq("ws-skill", cookie))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "hotplex-cli")
	require.NotContains(t, w.Body.String(), `"builtin":true`)
}

// ─── ListWorkspace（issue #918：workspace-only 列表端点）────────────────────

func listWorkspaceReq(wid, cookie string) *http.Request {
	req := skillReq(http.MethodGet, "/api/workspaces/"+wid+"/skills", cookie, nil, "")
	req.SetPathValue("wid", wid)
	return req
}

func TestSkillHandlers_ListWorkspace_OnlyWorkspaceSkills(t *testing.T) {
	t.Parallel()
	sh, workDir, home, cookie := newSkillHandlersEnv(t)

	// 全局 managed skill —— 不得出现在 workspace 列表。
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".agents", "skills", "glob"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".agents", "skills", "glob", "SKILL.md"),
		[]byte("---\nname: glob\ndescription: a global skill\n---\n"), 0o644))
	// workspace 只读 skill（.claude）—— 不得出现。
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, ".claude", "skills", "ro-skill"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, ".claude", "skills", "ro-skill", "SKILL.md"),
		[]byte("---\nname: ro-skill\ndescription: read only\n---\n"), 0o644))
	// workspace 安装的 managed skill —— 唯一应返回者。
	require.Equal(t, http.StatusOK, installWorkspace(t, sh, cookie, map[string]string{"SKILL.md": testSkillFM}, false).Code)

	w := httptest.NewRecorder()
	sh.ListWorkspace(w, listWorkspaceReq("ws-skill", cookie))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "my-skill")
	require.Contains(t, w.Body.String(), `"total":1`)
	require.NotContains(t, w.Body.String(), "glob", "全局 skill 不得混入 workspace 列表")
	require.NotContains(t, w.Body.String(), "ro-skill", ".claude 只读 skill 不得混入 workspace 列表")
}

func TestSkillHandlers_ListWorkspace_Empty(t *testing.T) {
	t.Parallel()
	sh, _, _, cookie := newSkillHandlersEnv(t)
	w := httptest.NewRecorder()
	sh.ListWorkspace(w, listWorkspaceReq("ws-skill", cookie))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"skills":[]`)
	require.Contains(t, w.Body.String(), `"total":0`)
}

func TestSkillHandlers_ListWorkspace_RequiresAuth(t *testing.T) {
	t.Parallel()
	sh, _, _, _ := newSkillHandlersEnv(t)
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/ws-skill/skills", nil) // 无 cookie
	req.SetPathValue("wid", "ws-skill")
	w := httptest.NewRecorder()
	sh.ListWorkspace(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestSkillHandlers_ListWorkspace_NotOwner(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	env.createUser(t, "alice", "alicepass", "user")
	aliceCookie := env.loginAs(t, "alice", "alicepass", http.StatusOK)

	ws := &session.Workspace{ID: "ws-skill", OwnerUserID: "u-admin", Name: "test-ws", WorkDir: t.TempDir(), Status: "active"}
	require.NoError(t, env.store.CreateWorkspace(context.Background(), ws, 1700000000))

	locator := skills.NewLocator(slog.Default(), time.Minute)
	t.Cleanup(locator.Close)
	sh := NewSkillHandlers(locator, env.store, env.auth, slog.Default())
	sh.homeFn = func() string { return t.TempDir() }

	w := httptest.NewRecorder()
	sh.ListWorkspace(w, listWorkspaceReq("ws-skill", aliceCookie))
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "WORKSPACE_FORBIDDEN")
}

func TestSkillHandlers_ListWorkspace_AdminMayListOthersWorkspace(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	env.createUser(t, "bob", "bobpass", "user")
	ws := &session.Workspace{ID: "ws-bob", OwnerUserID: "u-bob", Name: "bob-ws", WorkDir: t.TempDir(), Status: "active"}
	require.NoError(t, env.store.CreateWorkspace(context.Background(), ws, 1700000000))

	locator := skills.NewLocator(slog.Default(), time.Minute)
	t.Cleanup(locator.Close)
	sh := NewSkillHandlers(locator, env.store, env.auth, slog.Default())
	sh.homeFn = func() string { return t.TempDir() }

	adminCookie := env.loginAs(t, "admin", "adminpass", http.StatusOK)
	w := httptest.NewRecorder()
	sh.ListWorkspace(w, listWorkspaceReq("ws-bob", adminCookie))
	require.Equal(t, http.StatusOK, w.Code, "admin 应可列任意 workspace skill")
	require.Contains(t, w.Body.String(), `"total":0`)
}
