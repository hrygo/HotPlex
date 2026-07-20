package admin

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"

	"github.com/hrygo/hotplex/internal/skills"
	"github.com/hrygo/hotplex/internal/web"
)

// maxAdminSkillUploadSize 限制全局 skill zip 上传 body（spec §3.4，20MB）。
const maxAdminSkillUploadSize = 20 << 20

// skillInstallResponse 描述 POST /admin/api/skills 成功响应，与 skills.InstallResult
// 字段对齐。swag 的 --dir 不含 internal/skills、未开 --parseDependency，无法解析跨包
// 类型，故在此定义等价形状供 swagger 注解引用。
type skillInstallResponse struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Source      string   `json:"source"`  // "global"
	Managed     bool     `json:"managed"` // .agents/skills 可写区
	Body        string   `json:"body"`
	Files       []string `json:"files"`
	Warning     string   `json:"warning,omitempty"` // workspace 同名遮蔽全局时非空
}

// toSkillInstallResponse 把 skills.InstallResult 映射为本包 swagger 响应类型
// （swag 无法解析跨包 skills.InstallResult，见 skillInstallResponse 注释）。
// JSON 形状与原 InstallResult 完全一致（FilePath 为 json:"-" 不下发）。
func toSkillInstallResponse(res *skills.InstallResult) skillInstallResponse {
	return skillInstallResponse{
		Name:        res.Name,
		Description: res.Description,
		Source:      res.Source,
		Managed:     res.Managed,
		Body:        res.Body,
		Files:       res.Files,
		Warning:     res.Warning,
	}
}

// HandleListSkills: GET /admin/api/skills — 全局 skill 列表（含外部只读，带 Managed 标注）。
// GET 不审计（isWriteMethod 为 false）；scope 由 Middleware 强制 admin:read。
//
// @Summary      List global skills (admin)
// @Description  Returns every skill under the global .agents/.claude/.hotplex dirs. Requires admin:read.
// @Tags         Admin API
// @Produce      json
// @Security     AdminBearerAuth
// @Success      200  {object}  map[string]any
// @Failure      403  {object}  ErrorResponse  "Insufficient scope"
// @Router       /admin/api/skills [get]
func (a *AdminAPI) HandleListSkills(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminRead) {
		return
	}
	if a.skillsLocator == nil {
		respondJSON(w, map[string]any{"skills": []any{}})
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		a.log.Error("admin: skills list resolve $HOME", "err", err)
		web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "resolve home failed")
		return
	}
	listed, err := a.skillsLocator.List(r.Context(), home, "")
	if err != nil {
		a.log.Error("admin: skills list", "err", err)
		web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "list skills failed")
		return
	}
	respondJSON(w, map[string]any{"skills": listed, "total": len(listed)})
}

// HandleInstallSkill: POST /admin/api/skills — zip 上传安装全局 skill（?replace=true 覆盖）。
// 写操作由 Middleware 的 audit defer 记录为 AuditSkillCreate（adminActionFor → skillAction）。
//
// @Summary      Install global skill (admin)
// @Description  Upload a skill zip into ~/.agents/skills. Use ?replace=true to overwrite. Requires admin:write.
// @Tags         Admin API
// @Accept       multipart/form-data
// @Produce      json
// @Security     AdminBearerAuth
// @Param        file     formData  file     true  "skill zip archive"
// @Param        replace  query     bool     false "overwrite existing same-name skill"
// @Success      200   {object}  skillInstallResponse
// @Failure      400   {object}  ErrorResponse  "SKILL_INVALID_ZIP / SKILL_INVALID_FORMAT / SKILL_FILE_TYPE_BLOCKED"
// @Failure      403   {object}  ErrorResponse  "Insufficient scope"
// @Failure      409   {object}  ErrorResponse  "SKILL_ALREADY_EXISTS"
// @Router       /admin/api/skills [post]
func (a *AdminAPI) HandleInstallSkill(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminWrite) {
		return
	}
	if a.skillsLocator == nil {
		web.WriteAppError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "skills not configured")
		return
	}
	zr, ok := parseAdminSkillUpload(w, r)
	if !ok {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "resolve home failed")
		return
	}
	replace := r.URL.Query().Has("replace")
	res, err := a.skillsLocator.Install(r.Context(), skills.ScopeGlobal, home, "", zr, replace)
	if err != nil {
		writeAdminSkillError(a, w, err, "install")
		return
	}
	respondJSON(w, toSkillInstallResponse(res))
}

// HandleGetSkill: GET /admin/api/skills/{name} — 全局 skill 详情（含 SKILL.md 全文）。
func (a *AdminAPI) HandleGetSkill(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminRead) {
		return
	}
	if a.skillsLocator == nil {
		web.WriteAppError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "skills not configured")
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "resolve home failed")
		return
	}
	d, err := a.skillsLocator.Read(r.Context(), skills.ScopeGlobal, home, r.PathValue("name"))
	if err != nil {
		writeAdminSkillError(a, w, err, "read")
		return
	}
	respondJSON(w, d)
}

// HandleDeleteSkill: DELETE /admin/api/skills/{name} — 移除全局 skill。审计为 AuditSkillDelete。
func (a *AdminAPI) HandleDeleteSkill(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminWrite) {
		return
	}
	if a.skillsLocator == nil {
		web.WriteAppError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "skills not configured")
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "resolve home failed")
		return
	}
	if err := a.skillsLocator.Delete(r.Context(), skills.ScopeGlobal, home, r.PathValue("name")); err != nil {
		writeAdminSkillError(a, w, err, "delete")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// parseAdminSkillUpload 解析 multipart zip 上传（MaxBytesReader 兜底 20MB）。
func parseAdminSkillUpload(w http.ResponseWriter, r *http.Request) (*zip.Reader, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAdminSkillUploadSize)
	if err := r.ParseMultipartForm(maxAdminSkillUploadSize); err != nil {
		web.WriteAppError(w, http.StatusBadRequest, "SKILL_INVALID_ZIP", "multipart parse failed: "+err.Error())
		return nil, false
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		web.WriteAppError(w, http.StatusBadRequest, "SKILL_INVALID_ZIP", "missing 'file' field")
		return nil, false
	}
	defer func() { _ = f.Close() }()
	buf, err := io.ReadAll(f)
	if err != nil {
		web.WriteAppError(w, http.StatusBadRequest, "SKILL_INVALID_ZIP", "read upload failed")
		return nil, false
	}
	zr, err := zip.NewReader(bytes.NewReader(buf), int64(len(buf)))
	if err != nil {
		web.WriteAppError(w, http.StatusBadRequest, "SKILL_INVALID_ZIP", "invalid zip archive")
		return nil, false
	}
	return zr, true
}

// writeAdminSkillError 映射 skills 包语义错误到 HTTP 错误码（spec §5）。
func writeAdminSkillError(a *AdminAPI, w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, skills.ErrInvalidZip):
		web.WriteAppError(w, http.StatusBadRequest, "SKILL_INVALID_ZIP", err.Error())
	case errors.Is(err, skills.ErrFileTypeBlocked):
		web.WriteAppError(w, http.StatusBadRequest, "SKILL_FILE_TYPE_BLOCKED", err.Error())
	case errors.Is(err, skills.ErrInvalidFormat):
		web.WriteAppError(w, http.StatusBadRequest, "SKILL_INVALID_FORMAT", err.Error())
	case errors.Is(err, skills.ErrSkillAlreadyExists):
		web.WriteAppError(w, http.StatusConflict, "SKILL_ALREADY_EXISTS", err.Error())
	case errors.Is(err, skills.ErrSkillNotFound):
		web.WriteAppError(w, http.StatusNotFound, "SKILL_NOT_FOUND", err.Error())
	default:
		a.log.Error("admin: skill "+action, "err", err)
		web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", action+" failed")
	}
}
