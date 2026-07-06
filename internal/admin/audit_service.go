package admin

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/hrygo/hotplex/internal/audit"
)

// ActivityService provides by-user audit query + export per spec section 5.8.
type ActivityService struct {
	store audit.Store
	log   *slog.Logger
}

// NewActivityService returns an ActivityService backed by the given audit store.
// A nil log falls back to slog.Default().
func NewActivityService(store audit.Store, log *slog.Logger) *ActivityService {
	if log == nil {
		log = slog.Default()
	}
	return &ActivityService{store: store, log: log}
}

// Query returns audit rows matching the query, with PII redaction when
// q.IncludePII is false (spec §5.9).
func (s *ActivityService) Query(ctx context.Context, q audit.Query) ([]audit.UserActivity, error) {
	rows, err := s.store.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("audit query: %w", err)
	}
	if !q.IncludePII {
		for i := range rows {
			maskPII(&rows[i])
		}
	}
	return rows, nil
}

// QueryByUser is a convenience wrapper that sets q.UserID.
func (s *ActivityService) QueryByUser(ctx context.Context, userID string, q audit.Query) ([]audit.UserActivity, error) {
	q.UserID = userID
	return s.Query(ctx, q)
}

// Export returns the audit rows in the requested format ("json" or "csv").
// An empty format defaults to JSON. Always includes PII — the HTTP layer
// gates ?include_pii=true on admin:write scope. The caller (HTTP handler)
// is responsible for emitting the system.audit_export meta-audit row.
func (s *ActivityService) Export(ctx context.Context, q audit.Query, format, exporterUserID string) ([]byte, string, error) {
	if exporterUserID == "" {
		exporterUserID = audit.AnonymousUserID
	}
	// Export always includes PII; the gate is at the HTTP layer.
	q.IncludePII = true
	rows, err := s.store.Query(ctx, q)
	if err != nil {
		return nil, "", fmt.Errorf("audit export query: %w", err)
	}
	var data []byte
	var contentType string
	switch format {
	case "csv":
		data, err = encodeCSV(rows)
		if err != nil {
			return nil, "", fmt.Errorf("audit export csv: %w", err)
		}
		contentType = "text/csv; charset=utf-8"
	case "json", "":
		data, err = encodeJSON(rows)
		if err != nil {
			return nil, "", fmt.Errorf("audit export json: %w", err)
		}
		contentType = "application/json; charset=utf-8"
	default:
		return nil, "", fmt.Errorf("audit export: unknown format %q", format)
	}
	s.log.Info("audit export",
		"exporter", exporterUserID,
		"format", format,
		"rows", len(rows),
		"user_id_filter", q.UserID,
	)
	return data, contentType, nil
}

// maskPII redacts ip (last octet → 0 for IPv4, last 4 groups → 0 for IPv6)
// and truncates user_agent to browser family (first 50 chars + "...").
// Per spec §5.9.
func maskPII(ua *audit.UserActivity) {
	if ua.IP != "" {
		if idx := strings.LastIndex(ua.IP, "."); idx > 0 {
			// IPv4: zero last octet
			ua.IP = ua.IP[:idx+1] + "0"
		} else if idx := strings.LastIndex(ua.IP, ":"); idx > 0 {
			// IPv6: keep first 4 groups, zero the rest
			groups := strings.Split(ua.IP, ":")
			if len(groups) > 4 {
				ua.IP = strings.Join(groups[:4], ":") + ":0:0:0:0"
			}
		}
	}
	if ua.UserAgent != "" && len(ua.UserAgent) > 50 {
		ua.UserAgent = ua.UserAgent[:50] + "..."
	}
}

func encodeJSON(rows []audit.UserActivity) ([]byte, error) {
	return json.MarshalIndent(rows, "", "  ")
}

func encodeCSV(rows []audit.UserActivity) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write([]string{
		"id", "ts", "user_id", "user_id_type", "platform", "session_id",
		"action", "resource_type", "resource_id", "outcome",
		"ip", "user_agent", "self_hash",
	}); err != nil {
		return nil, err
	}
	for _, r := range rows {
		if err := w.Write([]string{
			fmt.Sprintf("%d", r.ID),
			time.UnixMilli(r.Ts).UTC().Format(time.RFC3339Nano),
			r.UserID, r.UserIDType, r.Platform, r.SessionID,
			r.Action, r.ResourceType, r.ResourceID, r.Outcome,
			r.IP, r.UserAgent, r.SelfHash,
		}); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}
