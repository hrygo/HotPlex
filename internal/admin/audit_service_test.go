package admin

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/audit"
	"github.com/hrygo/hotplex/internal/dbutil"
)

// mockAuditStore implements audit.Store for ActivityService tests.
type mockAuditStore struct {
	queryFn func(ctx context.Context, q audit.Query) ([]audit.UserActivity, error)
}

func (m *mockAuditStore) BeginTx(ctx context.Context) (audit.Tx, error) {
	return nil, errors.New("not implemented")
}
func (m *mockAuditStore) Query(ctx context.Context, q audit.Query) ([]audit.UserActivity, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, q)
	}
	return nil, nil
}
func (m *mockAuditStore) DeleteBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	return 0, nil
}
func (m *mockAuditStore) SaveCheckpoint(ctx context.Context, c audit.Checkpoint) error {
	return nil
}
func (m *mockAuditStore) LatestCheckpoint(ctx context.Context) (*audit.Checkpoint, error) {
	return nil, nil
}
func (m *mockAuditStore) Close() error { return nil }
func (m *mockAuditStore) Dialect() dbutil.Dialect {
	return dbutil.DialectSQLite
}

func TestActivityService_Query_MasksPII(t *testing.T) {
	t.Parallel()
	store := &mockAuditStore{
		queryFn: func(ctx context.Context, q audit.Query) ([]audit.UserActivity, error) {
			return []audit.UserActivity{
				{
					ID:        1,
					Ts:        time.Now().UnixMilli(),
					UserID:    "u1",
					Action:    "auth.login",
					Outcome:   "success",
					IP:        "192.168.1.100",
					UserAgent: "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
				},
			}, nil
		},
	}
	svc := NewActivityService(store, slog.Default())

	rows, err := svc.Query(context.Background(), audit.Query{IncludePII: false})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	// IP last octet zeroed
	require.Equal(t, "192.168.1.0", rows[0].IP)
	// UserAgent truncated
	require.True(t, len(rows[0].UserAgent) <= 53) // 50 + "..."
	require.True(t, strings.HasSuffix(rows[0].UserAgent, "..."))
}

func TestActivityService_Query_IncludePII(t *testing.T) {
	t.Parallel()
	store := &mockAuditStore{
		queryFn: func(ctx context.Context, q audit.Query) ([]audit.UserActivity, error) {
			return []audit.UserActivity{
				{
					ID:        1,
					UserID:    "u1",
					Action:    "auth.login",
					Outcome:   "success",
					IP:        "192.168.1.100",
					UserAgent: "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
				},
			}, nil
		},
	}
	svc := NewActivityService(store, slog.Default())

	rows, err := svc.Query(context.Background(), audit.Query{IncludePII: true})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	// IP NOT masked
	require.Equal(t, "192.168.1.100", rows[0].IP)
	// UserAgent NOT truncated
	require.Contains(t, rows[0].UserAgent, "Chrome/120.0.0.0")
}

func TestActivityService_QueryByUser(t *testing.T) {
	t.Parallel()
	var capturedQuery audit.Query
	store := &mockAuditStore{
		queryFn: func(ctx context.Context, q audit.Query) ([]audit.UserActivity, error) {
			capturedQuery = q
			return []audit.UserActivity{{ID: 1, UserID: "target-user"}}, nil
		},
	}
	svc := NewActivityService(store, slog.Default())

	rows, err := svc.QueryByUser(context.Background(), "target-user", audit.Query{Action: "auth.login"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "target-user", capturedQuery.UserID)
	require.Equal(t, "auth.login", capturedQuery.Action)
}

func TestActivityService_Query_StoreError(t *testing.T) {
	t.Parallel()
	store := &mockAuditStore{
		queryFn: func(ctx context.Context, q audit.Query) ([]audit.UserActivity, error) {
			return nil, errors.New("db down")
		},
	}
	svc := NewActivityService(store, slog.Default())

	_, err := svc.Query(context.Background(), audit.Query{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "audit query")
	require.Contains(t, err.Error(), "db down")
}

func TestActivityService_Export_JSON(t *testing.T) {
	t.Parallel()
	store := &mockAuditStore{
		queryFn: func(ctx context.Context, q audit.Query) ([]audit.UserActivity, error) {
			return []audit.UserActivity{
				{ID: 1, UserID: "u1", Action: "auth.login", Outcome: "success"},
			}, nil
		},
	}
	svc := NewActivityService(store, slog.Default())

	data, contentType, err := svc.Export(context.Background(), audit.Query{}, "json", "admin-user")
	require.NoError(t, err)
	require.Equal(t, "application/json; charset=utf-8", contentType)
	require.Contains(t, string(data), `"UserID": "u1"`)
}

func TestActivityService_Export_CSV(t *testing.T) {
	t.Parallel()
	store := &mockAuditStore{
		queryFn: func(ctx context.Context, q audit.Query) ([]audit.UserActivity, error) {
			return []audit.UserActivity{
				{ID: 1, Ts: 1700000000000, UserID: "u1", Action: "auth.login", Outcome: "success"},
			}, nil
		},
	}
	svc := NewActivityService(store, slog.Default())

	data, contentType, err := svc.Export(context.Background(), audit.Query{}, "csv", "admin-user")
	require.NoError(t, err)
	require.Equal(t, "text/csv; charset=utf-8", contentType)
	// Parse CSV
	r := csv.NewReader(strings.NewReader(string(data)))
	records, err := r.ReadAll()
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(records), 2) // header + 1 row
	require.Equal(t, "id", records[0][0])
	require.Equal(t, "1", records[1][0])
}

func TestActivityService_Export_DefaultFormat(t *testing.T) {
	t.Parallel()
	store := &mockAuditStore{
		queryFn: func(ctx context.Context, q audit.Query) ([]audit.UserActivity, error) {
			return []audit.UserActivity{{ID: 1}}, nil
		},
	}
	svc := NewActivityService(store, slog.Default())

	_, contentType, err := svc.Export(context.Background(), audit.Query{}, "", "admin-user")
	require.NoError(t, err)
	require.Equal(t, "application/json; charset=utf-8", contentType)
}

func TestActivityService_Export_UnknownFormat(t *testing.T) {
	t.Parallel()
	store := &mockAuditStore{
		queryFn: func(ctx context.Context, q audit.Query) ([]audit.UserActivity, error) {
			return []audit.UserActivity{{ID: 1}}, nil
		},
	}
	svc := NewActivityService(store, slog.Default())

	_, _, err := svc.Export(context.Background(), audit.Query{}, "xml", "admin-user")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown format")
}

func TestActivityService_Export_AnonymousExporter(t *testing.T) {
	t.Parallel()
	store := &mockAuditStore{
		queryFn: func(ctx context.Context, q audit.Query) ([]audit.UserActivity, error) {
			return []audit.UserActivity{{ID: 1}}, nil
		},
	}
	svc := NewActivityService(store, slog.Default())

	// Empty exporterUserID should default to "anonymous"
	data, _, err := svc.Export(context.Background(), audit.Query{}, "json", "")
	require.NoError(t, err)
	require.NotEmpty(t, data)
}

func TestMaskPII_IPv4(t *testing.T) {
	t.Parallel()
	ua := &audit.UserActivity{IP: "10.0.0.255"}
	maskPII(ua)
	require.Equal(t, "10.0.0.0", ua.IP)
}

func TestMaskPII_IPv6(t *testing.T) {
	t.Parallel()
	ua := &audit.UserActivity{IP: "2001:0db8:85a3:0000:0000:8a2e:0370:7334"}
	maskPII(ua)
	// Should keep first 4 groups
	require.Equal(t, "2001:0db8:85a3:0000:0:0:0:0", ua.IP)
}

func TestMaskPII_ShortUserAgent(t *testing.T) {
	t.Parallel()
	ua := &audit.UserActivity{UserAgent: "curl/7.68.0"}
	maskPII(ua)
	// Short UA not truncated
	require.Equal(t, "curl/7.68.0", ua.UserAgent)
}

func TestMaskPII_LongUserAgent(t *testing.T) {
	t.Parallel()
	longUA := "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	ua := &audit.UserActivity{UserAgent: longUA}
	maskPII(ua)
	require.True(t, strings.HasSuffix(ua.UserAgent, "..."))
	require.LessOrEqual(t, len(ua.UserAgent), 53)
}

func TestEncodeJSON(t *testing.T) {
	t.Parallel()
	rows := []audit.UserActivity{
		{ID: 1, UserID: "u1", Action: "auth.login"},
	}
	data, err := encodeJSON(rows)
	require.NoError(t, err)
	var decoded []audit.UserActivity
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Len(t, decoded, 1)
	require.Equal(t, "u1", decoded[0].UserID)
}

func TestEncodeCSV(t *testing.T) {
	t.Parallel()
	rows := []audit.UserActivity{
		{ID: 1, Ts: 1700000000000, UserID: "u1", Action: "auth.login", Outcome: "success"},
		{ID: 2, Ts: 1700000001000, UserID: "u2", Action: "session.create", Outcome: "success"},
	}
	data, err := encodeCSV(rows)
	require.NoError(t, err)
	r := csv.NewReader(strings.NewReader(string(data)))
	records, err := r.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 3) // header + 2 rows
	require.Equal(t, "id", records[0][0])
	require.Equal(t, "user_id", records[0][2])
	require.Equal(t, "1", records[1][0])
	require.Equal(t, "u1", records[1][2])
	require.Equal(t, "2", records[2][0])
	require.Equal(t, "u2", records[2][2])
}
