package admin

import (
	"log/slog"
	"net/http"
	"strings"
)

// Audit action enumeration (issue #788 A5). Persisted as the "action" field of
// admin_audit slog records. Stable strings — never rename without a consumer
// audit, log dashboards filter on these.
const (
	AuditGatewayRestart     = "gateway.restart"
	AuditBotCreate          = "bot.create"
	AuditBotUpdate          = "bot.update"
	AuditBotDelete          = "bot.delete"
	AuditAPIKeyCreate       = "apikey.create"
	AuditAPIKeyUpdate       = "apikey.update"
	AuditAPIKeyDelete       = "apikey.delete"
	AuditCronCreate         = "cron.create"
	AuditCronUpdate         = "cron.update"
	AuditCronDelete         = "cron.delete"
	AuditCronTrigger        = "cron.trigger"
	AuditSessionDelete      = "session.delete"
	AuditSessionTerminate   = "session.terminate"
	AuditConfigRollback     = "config.rollback"
	AuditConfigValidate     = "config.validate"
	AuditMemberStatusUpdate = "member.status.update"
	AuditInvitationCreate   = "invitation.create"
	AuditInvitationDelete   = "invitation.delete"
)

// auditLogger is slog.Default() so admin_audit records flow through the same
// JSON handler as the rest of the gateway. Swap via SetAuditLogger (tests).
var auditLogger = slog.Default()

// SetAuditLogger redirects admin_audit records. Tests use it to capture audits
// without spinning the real slog pipeline; production never calls it.
func SetAuditLogger(l *slog.Logger) {
	if l != nil {
		auditLogger = l
	}
}

// AdminAudit records a structured admin action for compliance and incident
// forensics (issue #788 A5). No new storage — rides the existing slog pipeline.
// actor is a uid (cookie channel) or "admin-token" (Bearer channel); target is
// the request path (with ids); result is "ok"|"failed".
func AdminAudit(actor, action, target, result string) {
	auditLogger.Info("admin_audit",
		"actor_user_id", actor,
		"action", action,
		"target", target,
		"result", result,
	)
}

// isWriteMethod reports whether the method mutates server state (issue #788 A5).
func isWriteMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// adminActionFor maps a (method, path) to a stable audit action enum. Falls back
// to "<method> <path>" for unmapped routes so no write is silently lost. Order
// matters: specific sub-resources (/terminate, /run, /config/*) are matched
// before their parent collections (/sessions, /cron/jobs).
func adminActionFor(method, path string) string {
	switch {
	case strings.Contains(path, "/restart"):
		return AuditGatewayRestart
	case strings.Contains(path, "/terminate"):
		return AuditSessionTerminate
	case strings.HasSuffix(path, "/run") && strings.Contains(path, "/cron/"):
		return AuditCronTrigger
	case strings.Contains(path, "/config/rollback"):
		return AuditConfigRollback
	case strings.Contains(path, "/config/validate"):
		return AuditConfigValidate
	case strings.Contains(path, "/api-keys"):
		return apiKeyAction(method)
	case strings.Contains(path, "/cron/jobs"):
		return cronAction(method)
	case strings.Contains(path, "/bots"):
		return botAction(method)
	case strings.Contains(path, "/sessions"):
		return sessionAction(method)
	}
	return method + " " + path
}

func botAction(method string) string {
	switch method {
	case http.MethodPost:
		return AuditBotCreate
	case http.MethodPatch, http.MethodPut:
		return AuditBotUpdate
	case http.MethodDelete:
		return AuditBotDelete
	}
	return "bot." + strings.ToLower(method)
}

func apiKeyAction(method string) string {
	switch method {
	case http.MethodPost:
		return AuditAPIKeyCreate
	case http.MethodPatch, http.MethodPut:
		return AuditAPIKeyUpdate
	case http.MethodDelete:
		return AuditAPIKeyDelete
	}
	return "apikey." + strings.ToLower(method)
}

func cronAction(method string) string {
	switch method {
	case http.MethodPost:
		return AuditCronCreate
	case http.MethodPatch, http.MethodPut:
		return AuditCronUpdate
	case http.MethodDelete:
		return AuditCronDelete
	}
	return "cron." + strings.ToLower(method)
}

// sessionAction covers DELETE; terminate is matched earlier in adminActionFor
// (POST /admin/sessions/{id}/terminate → AuditSessionTerminate, not here).
func sessionAction(method string) string {
	if method == http.MethodDelete {
		return AuditSessionDelete
	}
	return "session." + strings.ToLower(method)
}
