package admin

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/hrygo/hotplex/internal/skills"
	"github.com/hrygo/hotplex/internal/web"
)

// maxAdminSkillUploadSize 限制全局 skill zip 上传 body（spec §3.4，20MB）。
const maxAdminSkillUploadSize = 20 << 20

// skillInstallResponse 描述 POST /admin/api/skills 成功响应，与 skills.InstallResult
// 字段对齐。swag 的 --dir 不含 internal/skills、未开 --parseDependency，无法解析跨包
// 类型，故在此定义等价形状供 swagger 注解引用。
type skillInstallResponse struct {
	Name                  string   `json:"name"`
	Description           string   `json:"description"`
	Source                string   `json:"source"`  // "global"
	Managed               bool     `json:"managed"` // .agents/skills 可写区
	Builtin               bool     `json:"builtin,omitempty"`
	BuiltinPackageVersion string   `json:"builtin_package_version,omitempty"`
	Body                  string   `json:"body"`
	Files                 []string `json:"files"`
	Warning               string   `json:"warning,omitempty"` // workspace 同名遮蔽全局时非空
}

// toSkillInstallResponse 把 skills.InstallResult 映射为本包 swagger 响应类型
// （swag 无法解析跨包 skills.InstallResult，见 skillInstallResponse 注释）。
// JSON 形状与原 InstallResult 完全一致（FilePath 为 json:"-" 不下发）。
func toSkillInstallResponse(res *skills.InstallResult) skillInstallResponse {
	return skillInstallResponse{
		Name:                  res.Name,
		Description:           res.Description,
		Source:                res.Source,
		Managed:               res.Managed,
		Builtin:               res.Builtin,
		BuiltinPackageVersion: res.BuiltinPackageVersion,
		Body:                  res.Body,
		Files:                 res.Files,
		Warning:               res.Warning,
	}
}

const publicBuiltinProfile = "operator"

// mergeBuiltinSkills appends embedded skills after real filesystem skills and
// suppresses any same-name builtin. Keeping real entries first preserves the
// existing source precedence while ensuring search and pagination operate on
// the final merged view.
func (a *AdminAPI) mergeBuiltinSkills(ctx context.Context, listed []skills.Skill) ([]skills.Skill, error) {
	if a.builtinSkills == nil {
		return listed, nil
	}
	builtins, err := a.builtinSkills.List(ctx, publicBuiltinProfile)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(listed)+len(builtins))
	merged := make([]skills.Skill, 0, len(listed)+len(builtins))
	for _, skill := range listed {
		if _, ok := seen[skill.Name]; ok {
			continue
		}
		seen[skill.Name] = struct{}{}
		merged = append(merged, skill)
	}
	for _, skill := range builtins {
		if _, ok := seen[skill.Name]; ok {
			continue
		}
		seen[skill.Name] = struct{}{}
		merged = append(merged, skill)
	}
	return merged, nil
}

// builtinOnly reports whether name resolves to an embedded skill and no real
// global skill exists. A real entry always wins and is allowed ordinary CRUD.
func (a *AdminAPI) builtinOnly(ctx context.Context, home, name string) (bool, error) {
	if a.builtinSkills == nil {
		return false, nil
	}
	if a.skillsLocator != nil && home != "" {
		listed, err := a.skillsLocator.List(ctx, home, "")
		if err != nil {
			return false, err
		}
		for _, skill := range listed {
			if skill.Name == name {
				return false, nil
			}
		}
	}
	_, err := a.builtinSkills.Read(ctx, name, "")
	if err == nil {
		return true, nil
	}
	if errors.Is(err, skills.ErrSkillNotFound) {
		return false, nil
	}
	return false, err
}

// HandleListSkills: GET /admin/api/skills — 全局 skill 列表（支持分页 page/page_size 与搜索 q）。
// GET 不审计（isWriteMethod 为 false）；scope 由 Middleware 强制 admin:read。
//
// @Summary      List global skills (admin)
// @Description  Returns skills under the global .agents/.claude/.hotplex dirs with search & pagination. Requires admin:read.
// @Tags         Admin API
// @Produce      json
// @Security     AdminBearerAuth
// @Param        page       query  int     false  "Page number (default: 1)"
// @Param        page_size  query  int     false  "Page size (default: 10, max: 100)"
// @Param        q          query  string  false  "Search query for skill name or description"
// @Success      200  {object}  map[string]any
// @Failure      403  {object}  ErrorResponse  "Insufficient scope"
// @Router       /admin/api/skills [get]
func (a *AdminAPI) HandleListSkills(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminRead) {
		return
	}
	if a.skillsLocator == nil && a.builtinSkills == nil {
		respondJSON(w, map[string]any{"skills": []any{}, "total": 0, "page": 1, "page_size": 10})
		return
	}
	var listed []skills.Skill
	if a.skillsLocator != nil {
		home, err := os.UserHomeDir()
		if err != nil {
			a.log.Error("admin: skills list resolve $HOME", "err", err)
			web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "resolve home failed")
			return
		}
		listed, err = a.skillsLocator.List(r.Context(), home, "")
		if err != nil {
			a.log.Error("admin: skills list", "err", err)
			web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "list skills failed")
			return
		}
	}
	listed, err := a.mergeBuiltinSkills(r.Context(), listed)
	if err != nil {
		a.log.Error("admin: builtin skills list", "err", err)
		web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "list skills failed")
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		q = strings.TrimSpace(r.URL.Query().Get("search"))
	}
	filtered := listed
	if q != "" {
		qLower := strings.ToLower(q)
		var matched []skills.Skill
		for _, s := range listed {
			if strings.Contains(strings.ToLower(s.Name), qLower) || strings.Contains(strings.ToLower(s.Description), qLower) {
				matched = append(matched, s)
			}
		}
		filtered = matched
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 {
		pageSize = 10
	} else if pageSize > 100 {
		pageSize = 100
	}

	total := len(filtered)
	start := (page - 1) * pageSize
	end := start + pageSize

	var paged []skills.Skill
	if start < total {
		if end > total {
			end = total
		}
		paged = filtered[start:end]
	} else {
		paged = []skills.Skill{}
	}

	respondJSON(w, map[string]any{
		"skills":    paged,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// HandleInstallSkill: POST /admin/api/skills — 安装全局 skill (支持 zip 上传或 JSON 文本创建, ?replace=true 覆盖)。
// 写操作由 Middleware 的 audit defer 记录为 AuditSkillCreate（adminActionFor → skillAction）。
//
// @Summary      Install global skill (admin)
// @Description  Upload a skill zip into ~/.agents/skills. Use ?replace=true to overwrite. Requires admin:write.
// @Tags         Admin API
// @Accept       multipart/form-data
// @Produce      json
// @Security     AdminBearerAuth
// @Param        file     formData  file  true   "skill zip archive"
// @Param        replace  query     bool  false  "overwrite existing same-name skill"
// @Success      200      {object}  skillInstallResponse
// @Failure      400      {object}  ErrorResponse  "SKILL_INVALID_ZIP / SKILL_INVALID_FORMAT / SKILL_FILE_TYPE_BLOCKED"
// @Failure      403      {object}  ErrorResponse  "Insufficient scope"
// @Failure      409      {object}  ErrorResponse  "SKILL_ALREADY_EXISTS"
// @Router       /admin/api/skills [post]
func (a *AdminAPI) HandleInstallSkill(w http.ResponseWriter, r *http.Request) {
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

	contentType := r.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/json") {
		var req struct {
			Name    string `json:"name"`
			Body    string `json:"body"`
			Replace bool   `json:"replace"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			web.WriteAppError(w, http.StatusBadRequest, "SKILL_INVALID_FORMAT", "invalid JSON payload: "+err.Error())
			return
		}
		replace := req.Replace || r.URL.Query().Has("replace")
		res, err := a.skillsLocator.CreateText(r.Context(), skills.ScopeGlobal, home, "", req.Name, req.Body, replace)
		if err != nil {
			writeAdminSkillError(a, w, err, "create")
			return
		}
		respondJSON(w, toSkillInstallResponse(res))
		return
	}

	zr, closer, ok := parseAdminSkillUpload(w, r)
	if !ok {
		return
	}
	defer closer() // f 供 zip.Reader 作 ReaderAt，Install 消费完方可 Close（spec review P2#1）
	replace := r.URL.Query().Has("replace")
	res, err := a.skillsLocator.Install(r.Context(), skills.ScopeGlobal, home, "", zr, replace)
	if err != nil {
		writeAdminSkillError(a, w, err, "install")
		return
	}
	respondJSON(w, toSkillInstallResponse(res))
}

// HandleUpdateSkill: PUT /admin/api/skills/{name} — 更新全局 managed skill 内容 (SKILL.md)。
// 审计自动记录为 AuditSkillUpdate。
//
// @Summary      Update global skill (admin)
// @Description  Update SKILL.md body of a global managed skill. Requires admin:write.
// @Tags         Admin API
// @Accept       json
// @Produce      json
// @Security     AdminBearerAuth
// @Param        name  path      string  true  "Skill name"
// @Success      200   {object}  map[string]any
// @Failure      403   {object}  ErrorResponse  "Insufficient scope"
// @Failure      404   {object}  ErrorResponse  "SKILL_NOT_FOUND"
// @Router       /admin/api/skills/{name} [put]
func (a *AdminAPI) HandleUpdateSkill(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminWrite) {
		return
	}
	name := r.PathValue("name")
	if name == "" {
		web.WriteAppError(w, http.StatusBadRequest, "SKILL_INVALID_FORMAT", "missing skill name in path")
		return
	}
	home := ""
	if a.skillsLocator != nil {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "resolve home failed")
			return
		}
	}
	builtinOnly, err := a.builtinOnly(r.Context(), home, name)
	if err != nil {
		writeAdminSkillError(a, w, err, "inspect")
		return
	}
	if builtinOnly {
		writeAdminSkillError(a, w, skills.ErrSkillBuiltinReadonly, "update")
		return
	}
	if a.skillsLocator == nil {
		web.WriteAppError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "skills not configured")
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		web.WriteAppError(w, http.StatusBadRequest, "SKILL_INVALID_FORMAT", "invalid JSON payload: "+err.Error())
		return
	}
	detail, err := a.skillsLocator.Update(r.Context(), skills.ScopeGlobal, home, name, req.Body)
	if err != nil {
		writeAdminSkillError(a, w, err, "update")
		return
	}
	respondJSON(w, detail)
}

// HandleGetSkill: GET /admin/api/skills/{name} — 全局 skill 详情（含 SKILL.md 全文）。
//
// @Summary      Get global skill detail (admin)
// @Description  Returns detail of a global skill including full SKILL.md body and files list. Requires admin:read.
// @Tags         Admin API
// @Produce      json
// @Security     AdminBearerAuth
// @Param        name  path      string  true  "Skill name"
// @Success      200   {object}  map[string]any
// @Failure      403   {object}  ErrorResponse  "Insufficient scope"
// @Failure      404   {object}  ErrorResponse  "SKILL_NOT_FOUND"
// @Router       /admin/api/skills/{name} [get]
func (a *AdminAPI) HandleGetSkill(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminRead) {
		return
	}
	if a.skillsLocator == nil && a.builtinSkills == nil {
		web.WriteAppError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "skills not configured")
		return
	}
	name := r.PathValue("name")
	if a.skillsLocator != nil {
		home, err := os.UserHomeDir()
		if err != nil {
			web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "resolve home failed")
			return
		}
		d, err := a.skillsLocator.Read(r.Context(), skills.ScopeGlobal, home, name)
		if err == nil {
			respondJSON(w, d)
			return
		}
		if !errors.Is(err, skills.ErrSkillNotFound) {
			writeAdminSkillError(a, w, err, "read")
			return
		}
		// A same-name external global skill is still a real user item and
		// shadows the embedded catalog, even though Admin CRUD only reads
		// managed .agents/skills details.
		listed, listErr := a.skillsLocator.List(r.Context(), home, "")
		if listErr != nil {
			writeAdminSkillError(a, w, listErr, "list")
			return
		}
		for _, skill := range listed {
			if skill.Name == name {
				writeAdminSkillError(a, w, skills.ErrSkillNotFound, "read")
				return
			}
		}
	}
	if a.builtinSkills != nil {
		d, err := a.builtinSkills.Read(r.Context(), name, "")
		if err == nil {
			respondJSON(w, d)
			return
		}
		if !errors.Is(err, skills.ErrSkillNotFound) {
			writeAdminSkillError(a, w, err, "read builtin")
			return
		}
	}
	writeAdminSkillError(a, w, skills.ErrSkillNotFound, "read")
}

// HandleDeleteSkill: DELETE /admin/api/skills/{name} — 移除全局 skill。审计为 AuditSkillDelete。
//
// @Summary      Delete global skill (admin)
// @Description  Deletes a global managed skill. Requires admin:write.
// @Tags         Admin API
// @Security     AdminBearerAuth
// @Param        name  path  string  true  "Skill name"
// @Success      204   "No Content"
// @Failure      403   {object}  ErrorResponse  "Insufficient scope"
// @Failure      404   {object}  ErrorResponse  "SKILL_NOT_FOUND"
// @Router       /admin/api/skills/{name} [delete]
func (a *AdminAPI) HandleDeleteSkill(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminWrite) {
		return
	}
	if a.skillsLocator == nil && a.builtinSkills == nil {
		web.WriteAppError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "skills not configured")
		return
	}
	name := r.PathValue("name")
	home := ""
	if a.skillsLocator != nil {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "resolve home failed")
			return
		}
	}
	builtinOnly, err := a.builtinOnly(r.Context(), home, name)
	if err != nil {
		writeAdminSkillError(a, w, err, "inspect")
		return
	}
	if builtinOnly {
		writeAdminSkillError(a, w, skills.ErrSkillBuiltinReadonly, "delete")
		return
	}
	if a.skillsLocator == nil {
		web.WriteAppError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "skills not configured")
		return
	}
	if err := a.skillsLocator.Delete(r.Context(), skills.ScopeGlobal, home, name); err != nil {
		writeAdminSkillError(a, w, err, "delete")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// parseAdminSkillUpload 解析 multipart zip 上传（MaxBytesReader 兜底 20MB）。
// 返回的 closer 必须在调用方消费完 zr（Install 返回）后调用：*os.File 分支下
// zip.Reader 持有 f 作 ReaderAt，提前 Close 会使 extractZip→ReadAt 读已关闭 fd
// （spec review P2#1，见 gateway.parseSkillUpload 同名注释）。
func parseAdminSkillUpload(w http.ResponseWriter, r *http.Request) (*zip.Reader, func(), bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAdminSkillUploadSize)
	if err := r.ParseMultipartForm(maxAdminSkillUploadSize); err != nil {
		web.WriteAppError(w, http.StatusBadRequest, "SKILL_INVALID_ZIP", "multipart parse failed: "+err.Error())
		return nil, nil, false
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		web.WriteAppError(w, http.StatusBadRequest, "SKILL_INVALID_ZIP", "missing 'file' field")
		return nil, nil, false
	}
	zr, err := skills.ZipReaderFromFile(f)
	if err != nil {
		_ = f.Close()
		web.WriteAppError(w, http.StatusBadRequest, "SKILL_INVALID_ZIP", "invalid zip archive")
		return nil, nil, false
	}
	return zr, func() { _ = f.Close() }, true
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
	case errors.Is(err, skills.ErrSkillBuiltinReadonly):
		web.WriteAppError(w, http.StatusConflict, "SKILL_BUILTIN_READONLY", err.Error())
	case errors.Is(err, skills.ErrSkillNotFound):
		web.WriteAppError(w, http.StatusNotFound, "SKILL_NOT_FOUND", err.Error())
	default:
		a.log.Error("admin: skill "+action, "err", err)
		web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", action+" failed")
	}
}
