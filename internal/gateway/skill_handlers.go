package gateway

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/hrygo/hotplex/internal/audit"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/skills"
)

// maxSkillUploadSize 限制 zip 上传 body（spec §3.4 multipart 上传 20MB）。
// zip 解压阶段的容量阈值见 internal/skills/zip.go。
const maxSkillUploadSize = 20 << 20

// SkillHandlers 服务 /api/skills 与 /api/workspaces/{wid}/skills（spec §3.4）。
//
// 用户端鉴权复用 Authenticator（api-key 优先 + cookie 回落，与 workspace REST
// 同一租户锚点）。workspace 写操作走 owner 校验；全局 skill 写操作不在此暴露
// （仅 admin 端 /admin/api/skills）。写操作审计经 auditCollector → user_activity
// （tamper-evident），与 GatewayAPI session.delete 一致。
type SkillHandlers struct {
	locator        *skills.Locator
	wsStore        session.UserWorkspaceStore
	auth           *security.Authenticator
	log            *slog.Logger
	auditCollector *audit.Collector // optional; nil = audit disabled
	homeFn         func() string    // resolves global skill base dir; defaults to $HOME (injectable for tests)
}

// defaultHomeDir wraps os.UserHomeDir into a no-error signature for homeFn.
func defaultHomeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}

// NewSkillHandlers 构造用户端 skill handlers。locator 不可为 nil。
func NewSkillHandlers(locator *skills.Locator, wsStore session.UserWorkspaceStore, auth *security.Authenticator, log *slog.Logger) *SkillHandlers {
	if locator == nil {
		panic("gateway: NewSkillHandlers requires non-nil locator")
	}
	return &SkillHandlers{locator: locator, wsStore: wsStore, auth: auth, log: log.With("component", "skill_api"), homeFn: defaultHomeDir}
}

// SetAuditCollector 注入审计收集器（nil 关闭审计，no-op）。
func (h *SkillHandlers) SetAuditCollector(c *audit.Collector) { h.auditCollector = c }

func (h *SkillHandlers) requireAuth(w http.ResponseWriter, r *http.Request) (string, bool) {
	uid, _, err := h.auth.AuthenticateRequest(r)
	if err != nil {
		writeAppError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return "", false
	}
	return uid, true
}

func (h *SkillHandlers) isAdmin(r *http.Request, uid string) bool {
	idp := h.auth.IdentityProvider()
	if idp == nil {
		return false
	}
	u, err := idp.Lookup(r.Context(), uid)
	return err == nil && u.Role == "admin" && u.Status == "active"
}

// homeDir 解析进程 home（全局 skill 基址）。失败时返回空串，全局 skill 不可用。
func (h *SkillHandlers) homeDir() string {
	home := h.homeFn()
	if home == "" {
		h.log.Warn("skill_api: resolve $HOME failed; global skills unavailable")
		return ""
	}
	return home
}

// ownerWorkDirs 收集用户所有 workspace 的 WorkDir（用于合并列表）。
func (h *SkillHandlers) ownerWorkDirs(ctx context.Context, uid string) []string {
	if h.wsStore == nil {
		return nil
	}
	wss, err := h.wsStore.ListWorkspacesByOwner(ctx, uid, 1000, 0)
	if err != nil {
		h.log.Warn("skill_api: list owner workspaces", "err", err)
		return nil
	}
	dirs := make([]string, 0, len(wss))
	for _, ws := range wss {
		if ws.WorkDir != "" {
			dirs = append(dirs, ws.WorkDir)
		}
	}
	return dirs
}

// ListMerged: GET /api/skills — 合并 global + 用户所有 workspace + 外部只读（spec §5）。
func (h *SkillHandlers) ListMerged(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.requireAuth(w, r)
	if !ok {
		return
	}
	merged, err := h.locator.ListMerged(r.Context(), h.homeDir(), h.ownerWorkDirs(r.Context(), uid))
	if err != nil {
		h.log.Error("skill_api: list merged", "err", err)
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "list skills failed")
		return
	}
	respondJSON(w, map[string]any{"skills": merged, "total": len(merged)})
}

// GetMerged: GET /api/skills/{name} — 合并列表中该 skill 的元信息（覆盖胜出 scope）。
// 完整 body 由 scoped 详情端点（workspace / admin）提供。
func (h *SkillHandlers) GetMerged(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.requireAuth(w, r)
	if !ok {
		return
	}
	name := r.PathValue("name")
	merged, err := h.locator.ListMerged(r.Context(), h.homeDir(), h.ownerWorkDirs(r.Context(), uid))
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "list skills failed")
		return
	}
	for _, s := range merged {
		if s.Name == name {
			respondJSON(w, s)
			return
		}
	}
	writeAppError(w, http.StatusNotFound, "SKILL_NOT_FOUND", "skill not found")
}

// authorizeWorkspace 校验鉴权 + workspace 归属（spec §3.4 owner 校验闸）。
func (h *SkillHandlers) authorizeWorkspace(w http.ResponseWriter, r *http.Request) (*session.Workspace, string, bool) {
	uid, ok := h.requireAuth(w, r)
	if !ok {
		return nil, "", false
	}
	if h.wsStore == nil {
		writeAppError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "workspace store not configured")
		return nil, "", false
	}
	ws, err := h.wsStore.GetWorkspaceByID(r.Context(), r.PathValue("wid"))
	if err != nil {
		writeAppError(w, http.StatusNotFound, "WORKSPACE_NOT_FOUND", "workspace not found")
		return nil, "", false
	}
	if ws.OwnerUserID != uid && !h.isAdmin(r, uid) {
		writeAppError(w, http.StatusForbidden, "WORKSPACE_FORBIDDEN", "not your workspace")
		return nil, "", false
	}
	return ws, uid, true
}

// InstallWorkspace: POST /api/workspaces/{wid}/skills — zip 上传安装（?replace=true 覆盖）。
func (h *SkillHandlers) InstallWorkspace(w http.ResponseWriter, r *http.Request) {
	ws, uid, ok := h.authorizeWorkspace(w, r)
	if !ok {
		return
	}
	zr, ok := h.parseSkillUpload(w, r)
	if !ok {
		h.auditSkill(r, uid, "skill.install", audit.OutcomeFailure)
		return
	}
	replace := r.URL.Query().Has("replace")
	res, err := h.locator.Install(r.Context(), skills.ScopeWorkspace, ws.WorkDir, h.homeDir(), zr, replace)
	if err != nil {
		h.writeSkillError(w, err, "install")
		h.auditSkill(r, uid, "skill.install", audit.OutcomeFailure)
		return
	}
	h.auditSkill(r, uid, "skill.install", audit.OutcomeSuccess)
	respondJSON(w, res)
}

// GetWorkspace: GET /api/workspaces/{wid}/skills/{name} — workspace scope skill 详情。
func (h *SkillHandlers) GetWorkspace(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := h.authorizeWorkspace(w, r)
	if !ok {
		return
	}
	d, err := h.locator.Read(r.Context(), skills.ScopeWorkspace, ws.WorkDir, r.PathValue("name"))
	if err != nil {
		h.writeSkillError(w, err, "read")
		return
	}
	respondJSON(w, d)
}

// DeleteWorkspace: DELETE /api/workspaces/{wid}/skills/{name} — 移除 workspace skill。
func (h *SkillHandlers) DeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	ws, uid, ok := h.authorizeWorkspace(w, r)
	if !ok {
		return
	}
	if err := h.locator.Delete(r.Context(), skills.ScopeWorkspace, ws.WorkDir, r.PathValue("name")); err != nil {
		h.writeSkillError(w, err, "delete")
		h.auditSkill(r, uid, "skill.delete", audit.OutcomeFailure)
		return
	}
	h.auditSkill(r, uid, "skill.delete", audit.OutcomeSuccess)
	w.WriteHeader(http.StatusNoContent)
}

// parseSkillUpload 解析 multipart zip 上传（MaxBytesReader 兜底 20MB）。
func (h *SkillHandlers) parseSkillUpload(w http.ResponseWriter, r *http.Request) (*zip.Reader, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSkillUploadSize)
	if err := r.ParseMultipartForm(maxSkillUploadSize); err != nil {
		writeAppError(w, http.StatusBadRequest, "SKILL_INVALID_ZIP", "multipart parse failed: "+err.Error())
		return nil, false
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		writeAppError(w, http.StatusBadRequest, "SKILL_INVALID_ZIP", "missing 'file' field")
		return nil, false
	}
	defer func() { _ = f.Close() }()
	buf, err := io.ReadAll(f)
	if err != nil {
		writeAppError(w, http.StatusBadRequest, "SKILL_INVALID_ZIP", "read upload failed")
		return nil, false
	}
	zr, err := zip.NewReader(bytes.NewReader(buf), int64(len(buf)))
	if err != nil {
		writeAppError(w, http.StatusBadRequest, "SKILL_INVALID_ZIP", "invalid zip archive")
		return nil, false
	}
	return zr, true
}

// writeSkillError 映射 skills 包语义错误到 HTTP 错误码（spec §5）。
func (h *SkillHandlers) writeSkillError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, skills.ErrInvalidZip):
		writeAppError(w, http.StatusBadRequest, "SKILL_INVALID_ZIP", err.Error())
	case errors.Is(err, skills.ErrFileTypeBlocked):
		writeAppError(w, http.StatusBadRequest, "SKILL_FILE_TYPE_BLOCKED", err.Error())
	case errors.Is(err, skills.ErrInvalidFormat):
		writeAppError(w, http.StatusBadRequest, "SKILL_INVALID_FORMAT", err.Error())
	case errors.Is(err, skills.ErrSkillAlreadyExists):
		writeAppError(w, http.StatusConflict, "SKILL_ALREADY_EXISTS", err.Error())
	case errors.Is(err, skills.ErrSkillNotFound):
		writeAppError(w, http.StatusNotFound, "SKILL_NOT_FOUND", err.Error())
	default:
		h.log.Error("skill_api: "+action, "err", err)
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", action+" failed")
	}
}

// auditSkill 记录 workspace skill 写操作到 tamper-evident user_activity（spec §3.5）。
func (h *SkillHandlers) auditSkill(r *http.Request, uid, action, outcome string) {
	if h.auditCollector == nil {
		return
	}
	detail, _ := json.Marshal(map[string]any{"method": r.Method, "path": r.URL.Path})
	_ = h.auditCollector.Enqueue(context.Background(), &audit.UserActivity{
		Ts:           time.Now().UnixMilli(),
		UserID:       uid,
		UserIDType:   audit.UserIDTypeRegistered,
		Platform:     audit.PlatformWebChat,
		Action:       action,
		ResourceType: "skill",
		Outcome:      outcome,
		DetailJSON:   string(detail),
	})
}
