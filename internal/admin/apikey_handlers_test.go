package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/sqlutil"
)

func setupAPIKeyStore(t *testing.T) (*AdminAPI, func()) {
	t.Helper()
	ctx := context.Background()
	db, err := sqlutil.OpenDB(":memory:", &config.DBConfig{}, "test", sqlutil.PoolOpts{})
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	// Create table manually (no goose in test).
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS api_key_users (
		api_key TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL DEFAULT (datetime('now')),
		updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
	)`)
	require.NoError(t, err)

	api := newTestAPI(func(d *Deps) { d.DB = db })
	return api, func() {}
}

func TestHandleAPIKeyUserList_Empty(t *testing.T) {
	api, _ := setupAPIKeyStore(t)
	r := httptest.NewRequest("GET", "/admin/api-keys", nil)
	r = withScope(r, ScopeAdminRead)
	w := httptest.NewRecorder()
	api.HandleAPIKeyUserList(w, r)
	require.Equal(t, http.StatusOK, w.Code)
	var result []APIKeyUser
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	require.Empty(t, result)
}

func TestHandleAPIKeyUserCreateAndGet(t *testing.T) {
	api, _ := setupAPIKeyStore(t)

	// Create
	body := `{"user_id":"alice","description":"test user"}`
	r := httptest.NewRequest("POST", "/admin/api-keys", strings.NewReader(body))
	r = withScope(r, ScopeAdminWrite)
	w := httptest.NewRecorder()
	api.HandleAPIKeyUserCreate(w, r)
	require.Equal(t, http.StatusCreated, w.Code)
	var created APIKeyUser
	require.NoError(t, json.NewDecoder(w.Body).Decode(&created))
	require.Equal(t, "alice", created.UserID)
	require.NotEmpty(t, created.APIKey)
	require.True(t, strings.HasPrefix(created.APIKey, "hpk_"), "auto-generated key should have hpk_ prefix")

	// List — key should be masked
	r = httptest.NewRequest("GET", "/admin/api-keys", nil)
	r = withScope(r, ScopeAdminRead)
	tw := httptest.NewRecorder()
	api.HandleAPIKeyUserList(tw, r)
	require.Equal(t, http.StatusOK, tw.Code)
	var list []APIKeyUser
	require.NoError(t, json.NewDecoder(tw.Body).Decode(&list))
	require.Len(t, list, 1)
	require.Contains(t, list[0].APIKey, "****", "list should mask API key")

	// Get — key should be masked
	r = httptest.NewRequest("GET", "/admin/api-keys/{key}", nil)
	r.SetPathValue("key", created.APIKey)
	r = withScope(r, ScopeAdminRead)
	tw2 := httptest.NewRecorder()
	api.HandleAPIKeyUserGet(tw2, r)
	require.Equal(t, http.StatusOK, tw2.Code)
	var got APIKeyUser
	require.NoError(t, json.NewDecoder(tw2.Body).Decode(&got))
	require.Contains(t, got.APIKey, "****", "get should mask API key")
	require.Equal(t, "alice", got.UserID)
}

func TestHandleAPIKeyUserUpdate(t *testing.T) {
	api, _ := setupAPIKeyStore(t)

	// Create first
	body := `{"user_id":"alice","description":"original"}`
	r := httptest.NewRequest("POST", "/admin/api-keys", strings.NewReader(body))
	r = withScope(r, ScopeAdminWrite)
	w := httptest.NewRecorder()
	api.HandleAPIKeyUserCreate(w, r)
	var created APIKeyUser
	require.NoError(t, json.NewDecoder(w.Body).Decode(&created))

	// Update
	body = `{"user_id":"alice-updated","description":"updated"}`
	r = httptest.NewRequest("PATCH", "/admin/api-keys/{key}", strings.NewReader(body))
	r.SetPathValue("key", created.APIKey)
	r = withScope(r, ScopeAdminWrite)
	tw := httptest.NewRecorder()
	api.HandleAPIKeyUserUpdate(tw, r)
	require.Equal(t, http.StatusOK, tw.Code)
	var updated APIKeyUser
	require.NoError(t, json.NewDecoder(tw.Body).Decode(&updated))
	require.Equal(t, "alice-updated", updated.UserID)
}

func TestHandleAPIKeyUserDelete(t *testing.T) {
	api, _ := setupAPIKeyStore(t)

	// Create first
	body := `{"user_id":"alice"}`
	r := httptest.NewRequest("POST", "/admin/api-keys", strings.NewReader(body))
	r = withScope(r, ScopeAdminWrite)
	tw := httptest.NewRecorder()
	api.HandleAPIKeyUserCreate(tw, r)
	var created APIKeyUser
	require.NoError(t, json.NewDecoder(tw.Body).Decode(&created))

	// Delete
	r = httptest.NewRequest("DELETE", "/admin/api-keys/{key}", nil)
	r.SetPathValue("key", created.APIKey)
	r = withScope(r, ScopeAdminWrite)
	tw2 := httptest.NewRecorder()
	api.HandleAPIKeyUserDelete(tw2, r)
	require.Equal(t, http.StatusNoContent, tw2.Code)

	// Verify deleted
	r = httptest.NewRequest("GET", "/admin/api-keys", nil)
	r = withScope(r, ScopeAdminRead)
	tw3 := httptest.NewRecorder()
	api.HandleAPIKeyUserList(tw3, r)
	var list []APIKeyUser
	require.NoError(t, json.NewDecoder(tw3.Body).Decode(&list))
	require.Empty(t, list)
}

func TestHandleAPIKeyUserCreate_Validation(t *testing.T) {
	api, _ := setupAPIKeyStore(t)

	tests := []struct {
		name   string
		body   string
		status int
	}{
		{"empty user_id", `{"user_id":""}`, http.StatusBadRequest},
		{"user_id too long", `{"user_id":"` + strings.Repeat("x", 129) + `"}`, http.StatusBadRequest},
		{"description too long", `{"user_id":"a","description":"` + strings.Repeat("x", 513) + `"}`, http.StatusBadRequest},
		{"invalid json", `{not json}`, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/admin/api-keys", strings.NewReader(tt.body))
			r = withScope(r, ScopeAdminWrite)
			w := httptest.NewRecorder()
			api.HandleAPIKeyUserCreate(w, r)
			require.Equal(t, tt.status, w.Code)
		})
	}
}

func TestHandleAPIKeyUser_NilStore(t *testing.T) {
	api := newTestAPI() // no DB → akStore is nil
	r := httptest.NewRequest("GET", "/admin/api-keys", nil)
	r = withScope(r, ScopeAdminRead)
	w := httptest.NewRecorder()
	api.HandleAPIKeyUserList(w, r)
	require.Equal(t, http.StatusOK, w.Code) // returns empty array
}
