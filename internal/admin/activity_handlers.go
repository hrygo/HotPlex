package admin

import (
	"fmt"
	"net/http"
	"time"

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
	q := parseActivityQuery(r)
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
	// include_pii=true requires admin:write (spec section 5.9)
	if r.URL.Query().Get("include_pii") == "true" && !hasScope(r, ScopeAdminWrite) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "include_pii=true requires admin:write scope")
		return
	}
	q := parseActivityQuery(r)
	if userID != "" {
		q.UserID = userID
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
	if err != nil {
		api.log.Error("admin: activity export", "err", err)
		web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "export failed")
		return
	}
	// Meta-audit: emit system.audit_export row via collector (spec section 5.8)
	if api.auditCollector != nil {
		_ = api.auditCollector.Enqueue(r.Context(), &audit.UserActivity{
			Ts:         time.Now().UnixMilli(),
			UserID:     exporter,
			UserIDType: "registered",
			Platform:   "admin",
			Action:     audit.ActionSystemAuditExport,
			Outcome:    "success",
			DetailJSON: fmt.Sprintf(`{"target_user":%q,"format":%q,"rows":%d}`, userID, format, len(data)),
		})
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="activity-%s.%s"`, time.Now().UTC().Format("20060102-150405"), format))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
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
