package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hrygo/hotplex/internal/audit"
	"github.com/hrygo/hotplex/internal/web"
)

// HandleUserActivity is GET /admin/users/{id}/activity
// Returns audit rows for the given user (filtered by query params).
// Requires admin:read scope.
func (api *AdminAPI) HandleUserActivity(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeAdminRead) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "insufficient scope: need admin:read")
		return
	}
	if !api.ensureActivityService(w) {
		return
	}
	userID := r.PathValue("id")
	q := parseActivityQuery(r)
	rows, err := api.activityService.QueryByUser(r.Context(), userID, q)
	if err != nil {
		api.log.Error("admin: user activity query", "err", err, "user_id", userID)
		web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "failed to query activity")
		return
	}
	respondJSON(w, map[string]any{
		"user_id": userID,
		"rows":    rows,
		"limit":   q.Limit,
		"offset":  q.Offset,
	})
}

// HandleAdminActivity is GET /admin/activity?user_id=...
// Returns audit rows matching the query (any user).
// Requires admin:read scope.
func (api *AdminAPI) HandleAdminActivity(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeAdminRead) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "insufficient scope: need admin:read")
		return
	}
	if !api.ensureActivityService(w) {
		return
	}
	q := parseActivityQuery(r)
	if principalUserID := r.URL.Query().Get("principal_user_id"); principalUserID != "" {
		rows, resolved, err := api.activityService.QueryPrincipal(r.Context(), principalUserID, q)
		if err != nil {
			api.log.Error("admin: principal activity query", "err", err, "principal_user_id", principalUserID)
			web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "failed to query activity")
			return
		}
		respondJSON(w, map[string]any{
			"principal_user_id": principalUserID,
			"resolved_user_ids": resolved,
			"rows":              rows,
			"limit":             q.Limit,
			"offset":            q.Offset,
		})
		return
	}
	if uid := r.URL.Query().Get("user_id"); uid != "" {
		q.UserID = uid
	}
	rows, err := api.activityService.Query(r.Context(), q)
	if err != nil {
		api.log.Error("admin: activity query", "err", err)
		web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "failed to query activity")
		return
	}
	respondJSON(w, map[string]any{
		"rows":   rows,
		"limit":  q.Limit,
		"offset": q.Offset,
	})
}

// HandleUserActivityExport is GET /admin/users/{id}/activity?format=json|csv
// Exports audit rows for the given user as a downloadable file.
// Requires admin:read scope; ?include_pii=true requires admin:write.
func (api *AdminAPI) HandleUserActivityExport(w http.ResponseWriter, r *http.Request) {
	api.handleActivityExport(w, r, r.PathValue("id"))
}

// HandleAdminActivityExport is GET /admin/activity?format=json|csv
// Exports audit rows matching the query as a downloadable file.
// Requires admin:read scope; ?include_pii=true requires admin:write.
func (api *AdminAPI) HandleAdminActivityExport(w http.ResponseWriter, r *http.Request) {
	api.handleActivityExport(w, r, r.URL.Query().Get("user_id"))
}

func (api *AdminAPI) handleActivityExport(w http.ResponseWriter, r *http.Request, userID string) {
	if !hasScope(r, ScopeAdminRead) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "insufficient scope: need admin:read")
		return
	}
	if !api.ensureActivityService(w) {
		return
	}
	// include_pii=true requires admin:write (spec section 5.9)
	if r.URL.Query().Get("include_pii") == "true" && !hasScope(r, ScopeAdminWrite) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "include_pii=true requires admin:write scope")
		return
	}
	q := parseActivityQuery(r)
	if userID != "" {
		q.UserID = userID
	}
	if principalUserID := r.URL.Query().Get("principal_user_id"); principalUserID != "" {
		resolved, resolveErr := api.activityService.ResolvePrincipalUserIDs(r.Context(), principalUserID)
		if resolveErr != nil {
			api.log.Error("admin: activity export principal resolve", "err", resolveErr, "principal_user_id", principalUserID)
			web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "export failed")
			return
		}
		q.UserID = ""
		q.UserIDs = resolved
		userID = principalUserID
	}
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}
	exporter := r.Header.Get("X-Admin-Actor")
	if exporter == "" {
		exporter = audit.AnonymousUserID
	}
	data, contentType, err := api.activityService.Export(r.Context(), q, format, exporter)
	// Meta-audit fires on BOTH success and failure paths (spec §5.8: "every
	// export"). A failed export attempt is at least as forensically interesting
	// as a successful one — an attacker probing the endpoint, or an insider
	// attempting bulk exfiltration, would generate failures. Emit before the
	// error return so the row is recorded even when Export errors.
	if api.auditCollector != nil {
		outcome := audit.OutcomeSuccess
		detail := map[string]any{
			"target_user": userID,
			"format":      format,
		}
		if err != nil {
			outcome = audit.OutcomeFailure
			detail["error"] = err.Error()
		} else {
			detail["rows"] = len(data)
		}
		// json.Marshal produces spec-compliant detail_json and is robust
		// against attacker-influenced userID values (review M5: %q quoting
		// is not JSON-safe for all runes). Marshal failure is impossible
		// for these scalar values; fall back to a static stub defensively.
		raw, mErr := json.Marshal(detail)
		if mErr != nil {
			raw = []byte(`{}`)
		}
		_ = api.auditCollector.Enqueue(r.Context(), &audit.UserActivity{
			Ts:         time.Now().UnixMilli(),
			UserID:     exporter,
			UserIDType: audit.UserIDTypeRegistered,
			Platform:   audit.PlatformAdmin,
			Action:     audit.ActionSystemAuditExport,
			Outcome:    outcome,
			DetailJSON: string(raw),
		})
	}
	if err != nil {
		api.log.Error("admin: activity export", "err", err)
		web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "export failed")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="activity-%s.%s"`, time.Now().UTC().Format("20060102-150405"), format))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

type identityLinkRequest struct {
	PrincipalUserID string `json:"principal_user_id"`
	Provider        string `json:"provider"`
	Subject         string `json:"subject"`
	SubjectType     string `json:"subject_type"`
	DisplayName     string `json:"display_name"`
	Email           string `json:"email"`
}

// HandleAuditIdentityLinks is GET /admin/audit/identity-links.
// It lists explicit cross-channel mappings used by principal activity queries.
func (api *AdminAPI) HandleAuditIdentityLinks(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeAdminRead) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "insufficient scope: need admin:read")
		return
	}
	if !api.ensureActivityService(w) {
		return
	}
	links, err := api.activityService.ListIdentityLinks(r.Context(), r.URL.Query().Get("principal_user_id"))
	if err != nil {
		api.log.Error("admin: audit identity links list", "err", err)
		web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "failed to list identity links")
		return
	}
	respondJSON(w, map[string]any{"links": links})
}

// HandleCreateAuditIdentityLink is POST /admin/audit/identity-links.
func (api *AdminAPI) HandleCreateAuditIdentityLink(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeAdminWrite) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "insufficient scope: need admin:write")
		return
	}
	if !api.ensureActivityService(w) {
		return
	}
	var req identityLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		web.WriteAppError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}
	link, err := identityLinkFromRequest(req)
	if err != nil {
		web.WriteAppError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	if err := api.activityService.UpsertIdentityLink(r.Context(), link); err != nil {
		api.log.Error("admin: audit identity link upsert", "err", err)
		web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "failed to save identity link")
		return
	}
	respondJSON(w, map[string]any{"link": link})
}

// HandleDeleteAuditIdentityLink is DELETE /admin/audit/identity-links/{id}.
func (api *AdminAPI) HandleDeleteAuditIdentityLink(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeAdminWrite) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "insufficient scope: need admin:write")
		return
	}
	if !api.ensureActivityService(w) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		web.WriteAppError(w, http.StatusBadRequest, "INVALID_REQUEST", "id is required")
		return
	}
	if err := api.activityService.DeleteIdentityLink(r.Context(), id); err != nil {
		api.log.Error("admin: audit identity link delete", "err", err, "id", id)
		web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "failed to delete identity link")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (api *AdminAPI) ensureActivityService(w http.ResponseWriter) bool {
	if api.activityService != nil {
		return true
	}
	web.WriteAppError(w, http.StatusServiceUnavailable, "AUDIT_DISABLED", "audit activity service is not available")
	return false
}

func identityLinkFromRequest(req identityLinkRequest) (audit.IdentityLink, error) {
	principal := strings.TrimSpace(req.PrincipalUserID)
	provider := strings.TrimSpace(req.Provider)
	subject := strings.TrimSpace(req.Subject)
	if principal == "" || provider == "" || subject == "" {
		return audit.IdentityLink{}, fmt.Errorf("principal_user_id, provider, and subject are required")
	}
	subjectType := strings.TrimSpace(req.SubjectType)
	if subjectType == "" {
		subjectType = audit.UserIDTypePlatform
	}
	switch subjectType {
	case audit.UserIDTypeRegistered, audit.UserIDTypePlatform, audit.UserIDTypeSystem, audit.UserIDTypeAnonymous:
	default:
		return audit.IdentityLink{}, fmt.Errorf("subject_type must be registered, platform, system, or anonymous")
	}
	now := time.Now().UnixMilli()
	return audit.IdentityLink{
		ID:              uuid.NewString(),
		PrincipalUserID: principal,
		Provider:        provider,
		Subject:         subject,
		SubjectType:     subjectType,
		DisplayName:     strings.TrimSpace(req.DisplayName),
		Email:           strings.TrimSpace(req.Email),
		CreatedAt:       now,
		UpdatedAt:       now,
	}, nil
}

func parseActivityQuery(r *http.Request) audit.Query {
	q := audit.Query{}
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			q.From = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			q.To = t
		}
	}
	q.Action = r.URL.Query().Get("action")
	q.Outcome = r.URL.Query().Get("outcome")
	if v := r.URL.Query().Get("include_pii"); v == "true" {
		q.IncludePII = true
	}
	q.Limit, q.Offset = web.ParsePagination(r)
	// Sanitize: ensure limit doesn't exceed max (web.ParsePagination already clamps)
	if q.Limit <= 0 {
		q.Limit = 100
	}
	return q
}
