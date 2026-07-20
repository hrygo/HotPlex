package gateway

import (
	"archive/zip"
	"bytes"
	"context"
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
