package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/sqlutil"
)

func setupAPIKeyStore(t *testing.T) (*AdminAPI, func()) {
	t.Helper()
	ctx := context.Background()
	db, err := sqlutil.OpenDB(":memory:", &config.DBConfig{}, sqlutil.DialectSQLite, "test", sqlutil.PoolOpts{})
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	// Create table manually (no goose in test).
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS api_key_users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		api_key TEXT NOT NULL UNIQUE,
		user_id TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL DEFAULT (datetime('now')),
		updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
	)`)
	require.NoError(t, err)

	api := newTestAPI(func(d *Deps) { d.DB = db })
	return api, func() {}
}

func setupAPIKeyStoreWithWorkspaceStore(t *testing.T) (*AdminAPI, *session.SQLiteStore) {
	t.Helper()
	cfg := config.Default()
	cfg.DB.Path = filepath.Join(t.TempDir(), "test.db")
	cfg.DB.SQLite.Path = cfg.DB.Path
	cfg.DB.WALMode = true

	store, err := session.NewSQLiteStore(context.Background(), cfg, sqlutil.NewWriteMu(sqlutil.DialectSQLite))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	api := newTestAPI(func(d *Deps) {
		d.DB = store.DB()
		d.WorkspaceStore = store
	})
	return api, store
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
	r = httptest.NewRequest("GET", "/admin/api-keys/{id}", nil)
	r.SetPathValue("id", strconv.FormatInt(created.ID, 10))
	r = withScope(r, ScopeAdminRead)
	tw2 := httptest.NewRecorder()
	api.HandleAPIKeyUserGet(tw2, r)
	require.Equal(t, http.StatusOK, tw2.Code)
	var got APIKeyUser
	require.NoError(t, json.NewDecoder(tw2.Body).Decode(&got))
	require.Contains(t, got.APIKey, "****", "get should mask API key")
	require.Equal(t, "alice", got.UserID)
}

func TestHandleAPIKeyUserCreate_BindsExistingUsernameToUsersID(t *testing.T) {
	api, store := setupAPIKeyStoreWithWorkspaceStore(t)
	ctx := context.Background()
	require.NoError(t, store.CreateUser(ctx, &security.User{
		ID: "u-alice", Username: "alice", Role: "user", Status: "active",
	}, 1700000000))

	body := `{"user_id":"alice","description":"test user"}`
	r := httptest.NewRequest("POST", "/admin/api-keys", strings.NewReader(body))
	r = withScope(r, ScopeAdminWrite)
	w := httptest.NewRecorder()
	api.HandleAPIKeyUserCreate(w, r)
	require.Equal(t, http.StatusCreated, w.Code)

	var created APIKeyUser
	require.NoError(t, json.NewDecoder(w.Body).Decode(&created))
	require.Equal(t, "alice", created.UserID, "response should stay user-facing")

	raw, err := api.akStore.get(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "u-alice", raw.UserID, "stored api_key_users.user_id must be normalized to users.id")
}

func TestHandleAPIKeyUserCreate_ProvisionsPseudoUser(t *testing.T) {
	api, store := setupAPIKeyStoreWithWorkspaceStore(t)
	ctx := context.Background()

	body := `{"user_id":"alice","description":"service key"}`
	r := httptest.NewRequest("POST", "/admin/api-keys", strings.NewReader(body))
	r = withScope(r, ScopeAdminWrite)
	w := httptest.NewRecorder()
	api.HandleAPIKeyUserCreate(w, r)
	require.Equal(t, http.StatusCreated, w.Code)

	var created APIKeyUser
	require.NoError(t, json.NewDecoder(w.Body).Decode(&created))
	require.Equal(t, "alice", created.UserID, "response should keep the original logical identifier")

	raw, err := api.akStore.get(ctx, created.ID)
	require.NoError(t, err)
	require.NotEqual(t, "alice", raw.UserID, "stored api_key_users.user_id must be a concrete users.id")

	provisioned, err := store.GetUserByID(ctx, raw.UserID)
	require.NoError(t, err)
	require.Equal(t, "apikey:alice", provisioned.Username)
	require.Empty(t, provisioned.PasswordHash, "pseudo-user must remain API-key-only")
	require.Equal(t, "active", provisioned.Status)
}

func TestHandleAPIKeyUserCreate_RejectsWhitespaceUserID(t *testing.T) {
	api, store := setupAPIKeyStoreWithWorkspaceStore(t)
	ctx := context.Background()

	r := httptest.NewRequest("POST", "/admin/api-keys", strings.NewReader(`{"user_id":"   "}`))
	r = withScope(r, ScopeAdminWrite)
	w := httptest.NewRecorder()
	api.HandleAPIKeyUserCreate(w, r)

	require.Equal(t, http.StatusBadRequest, w.Code)
	_, err := store.GetUserByUsername(ctx, security.ReservedUsernamePrefix)
	require.ErrorIs(t, err, security.ErrUserNotFound)
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
	r = httptest.NewRequest("PATCH", "/admin/api-keys/{id}", strings.NewReader(body))
	r.SetPathValue("id", strconv.FormatInt(created.ID, 10))
	r = withScope(r, ScopeAdminWrite)
	tw := httptest.NewRecorder()
	api.HandleAPIKeyUserUpdate(tw, r)
	require.Equal(t, http.StatusOK, tw.Code)
	var updated APIKeyUser
	require.NoError(t, json.NewDecoder(tw.Body).Decode(&updated))
	require.Equal(t, "alice-updated", updated.UserID)
}

func TestHandleAPIKeyUserUpdate_NotFoundDoesNotProvisionPseudoUser(t *testing.T) {
	api, store := setupAPIKeyStoreWithWorkspaceStore(t)
	ctx := context.Background()

	body := `{"user_id":"ghost","description":"updated"}`
	r := httptest.NewRequest("PATCH", "/admin/api-keys/{id}", strings.NewReader(body))
	r.SetPathValue("id", "9999")
	r = withScope(r, ScopeAdminWrite)
	w := httptest.NewRecorder()
	api.HandleAPIKeyUserUpdate(w, r)

	require.Equal(t, http.StatusNotFound, w.Code)
	_, err := store.GetUserByUsername(ctx, security.ReservedUsernamePrefix+"ghost")
	require.True(t, errors.Is(err, security.ErrUserNotFound), "unexpected user lookup error: %v", err)
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

	// List keys to verify they are present
	r = httptest.NewRequest("GET", "/admin/api-keys", nil)
	r = withScope(r, ScopeAdminRead)
	twList := httptest.NewRecorder()
	api.HandleAPIKeyUserList(twList, r)
	require.Equal(t, http.StatusOK, twList.Code)
	var list []APIKeyUser
	require.NoError(t, json.NewDecoder(twList.Body).Decode(&list))
	require.Len(t, list, 1)
	require.Equal(t, created.ID, list[0].ID)

	// Delete using the ID
	r = httptest.NewRequest("DELETE", "/admin/api-keys/{id}", nil)
	r.SetPathValue("id", strconv.FormatInt(created.ID, 10))
	r = withScope(r, ScopeAdminWrite)
	tw2 := httptest.NewRecorder()
	api.HandleAPIKeyUserDelete(tw2, r)
	require.Equal(t, http.StatusNoContent, tw2.Code)

	// Verify deleted
	r = httptest.NewRequest("GET", "/admin/api-keys", nil)
	r = withScope(r, ScopeAdminRead)
	tw3 := httptest.NewRecorder()
	api.HandleAPIKeyUserList(tw3, r)
	var list2 []APIKeyUser
	require.NoError(t, json.NewDecoder(tw3.Body).Decode(&list2))
	require.Empty(t, list2)
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

// mockKeyValidator records AddKey/RemoveKey calls for test assertions.
type mockKeyValidator struct {
	added   []string
	removed []string
}

func (m *mockKeyValidator) AddKey(key string)    { m.added = append(m.added, key) }
func (m *mockKeyValidator) RemoveKey(key string) { m.removed = append(m.removed, key) }

func TestHandleAPIKeyUserCreate_SyncsKeyValidator(t *testing.T) {
	kv := &mockKeyValidator{}
	api, _ := setupAPIKeyStore(t)
	api.keyValidator = kv

	body := `{"user_id":"alice","description":"test"}`
	r := httptest.NewRequest("POST", "/admin/api-keys", strings.NewReader(body))
	r = withScope(r, ScopeAdminWrite)
	w := httptest.NewRecorder()
	api.HandleAPIKeyUserCreate(w, r)
	require.Equal(t, http.StatusCreated, w.Code)

	var created APIKeyUser
	require.NoError(t, json.NewDecoder(w.Body).Decode(&created))
	require.Len(t, kv.added, 1, "AddKey should be called once")
	require.Equal(t, created.APIKey, kv.added[0], "AddKey should receive the generated key")
	require.Empty(t, kv.removed)
}

func TestHandleAPIKeyUserDelete_SyncsKeyValidator(t *testing.T) {
	kv := &mockKeyValidator{}
	api, _ := setupAPIKeyStore(t)
	api.keyValidator = kv

	// Create a key first.
	body := `{"user_id":"alice"}`
	r := httptest.NewRequest("POST", "/admin/api-keys", strings.NewReader(body))
	r = withScope(r, ScopeAdminWrite)
	w := httptest.NewRecorder()
	api.HandleAPIKeyUserCreate(w, r)
	require.Equal(t, http.StatusCreated, w.Code)
	var created APIKeyUser
	require.NoError(t, json.NewDecoder(w.Body).Decode(&created))

	// Reset added to focus on delete behavior.
	kv.added = nil

	// Delete.
	r = httptest.NewRequest("DELETE", "/admin/api-keys/{id}", nil)
	r.SetPathValue("id", strconv.FormatInt(created.ID, 10))
	r = withScope(r, ScopeAdminWrite)
	tw := httptest.NewRecorder()
	api.HandleAPIKeyUserDelete(tw, r)
	require.Equal(t, http.StatusNoContent, tw.Code)

	require.Len(t, kv.removed, 1, "RemoveKey should be called once")
	require.True(t, strings.HasPrefix(kv.removed[0], "hpk_"), "RemoveKey should receive the full key")
	require.Empty(t, kv.added)
}

// TestHandleAPIKeyUser_NotFound locks in the not-found path after the get()
// error-handling refactor: a missing record surfaces sql.ErrNoRows through %w
// and respondStoreError must map it to 404 (not 500).
func TestHandleAPIKeyUser_NotFound(t *testing.T) {
	api, _ := setupAPIKeyStore(t) // empty table → every lookup misses

	// GET non-existent → 404
	r := httptest.NewRequest("GET", "/admin/api-keys/{id}", nil)
	r.SetPathValue("id", "9999")
	r = withScope(r, ScopeAdminRead)
	w := httptest.NewRecorder()
	api.HandleAPIKeyUserGet(w, r)
	require.Equal(t, http.StatusNotFound, w.Code)

	// UPDATE non-existent → 404 (valid body so validation passes; failure is at get())
	body := `{"user_id":"ghost","description":""}`
	r = httptest.NewRequest("PATCH", "/admin/api-keys/{id}", strings.NewReader(body))
	r.SetPathValue("id", "9999")
	r = withScope(r, ScopeAdminWrite)
	w = httptest.NewRecorder()
	api.HandleAPIKeyUserUpdate(w, r)
	require.Equal(t, http.StatusNotFound, w.Code)

	// DELETE non-existent → 404
	r = httptest.NewRequest("DELETE", "/admin/api-keys/{id}", nil)
	r.SetPathValue("id", "9999")
	r = withScope(r, ScopeAdminWrite)
	w = httptest.NewRecorder()
	api.HandleAPIKeyUserDelete(w, r)
	require.Equal(t, http.StatusNotFound, w.Code)
}
