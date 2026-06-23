package admin

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/hrygo/hotplex/internal/dbutil"
	"github.com/hrygo/hotplex/internal/sqlutil"
	"github.com/hrygo/hotplex/internal/web"
)

// ErrUserIDExists indicates a duplicate user_id when creating/updating an API key user.
var ErrUserIDExists = errors.New("admin: user_id already exists")

// maskAPIKey returns a masked version showing only first 8 and last 4 chars.
func maskAPIKey(key string) string {
	if len(key) <= 12 {
		return "****"
	}
	return key[:8] + "****" + key[len(key)-4:]
}

// APIKeyUser represents a mapping from an API key to a user identity.
type APIKeyUser struct {
	ID          int64  `json:"id"`
	APIKey      string `json:"api_key"`
	UserID      string `json:"user_id"`
	Description string `json:"description,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

// apiKeyStoreBase provides shared CRUD logic for API key user stores.
// Both SQLite (apiKeyUserStore) and PG (pgStore) embed this struct,
// eliminating duplication of list/get/delete and invalidator methods.
type apiKeyStoreBase struct {
	db          DBExecutor
	mu          sync.Mutex
	invalidator cacheInvalidator
	dialect     dbutil.Dialect   // DialectSQLite or DialectPostgres; controls placeholder style
	writeMu     *sqlutil.WriteMu // nil-safe; PG dialect = no-op
}

// ensureAPIKey generates a random API key (hpk_ prefix) if not already set.
func (b *apiKeyStoreBase) ensureAPIKey(u *APIKeyUser) error {
	if u.APIKey != "" {
		return nil
	}
	key := make([]byte, 24)
	if _, err := rand.Read(key); err != nil {
		return fmt.Errorf("admin: generate api key: %w", err)
	}
	u.APIKey = "hpk_" + hex.EncodeToString(key)
	return nil
}

func (b *apiKeyStoreBase) Invalidator() cacheInvalidator {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.invalidator
}

func (b *apiKeyStoreBase) SetInvalidator(inv cacheInvalidator) {
	b.mu.Lock()
	b.invalidator = inv
	b.mu.Unlock()
}

func (b *apiKeyStoreBase) list(ctx context.Context) ([]APIKeyUser, error) {
	rows, err := b.db.QueryContext(ctx,
		"SELECT id, api_key, user_id, description, created_at, updated_at FROM api_key_users ORDER BY created_at DESC")
	if err != nil {
		return nil, fmt.Errorf("admin: list api key users: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []APIKeyUser
	for rows.Next() {
		var u APIKeyUser
		if err := rows.Scan(&u.ID, &u.APIKey, &u.UserID, &u.Description, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, fmt.Errorf("admin: scan api key user: %w", err)
		}
		result = append(result, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("admin: iterate api key users: %w", err)
	}
	return result, nil
}

func (b *apiKeyStoreBase) get(ctx context.Context, id int64) (*APIKeyUser, error) {
	var u APIKeyUser
	err := b.db.QueryRowContext(ctx,
		b.dialect.Rebind("SELECT id, api_key, user_id, description, created_at, updated_at FROM api_key_users WHERE id = ?"), id,
	).Scan(&u.ID, &u.APIKey, &u.UserID, &u.Description, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("admin: get api key user: %w", err)
	}
	return &u, nil
}

func (b *apiKeyStoreBase) delete(ctx context.Context, id int64) error {
	query := b.dialect.Rebind("DELETE FROM api_key_users WHERE id = ?")
	return b.writeMu.WithLock(func() error {
		res, err := b.db.ExecContext(ctx, query, id)
		if err != nil {
			return fmt.Errorf("admin: delete api key user: %w", err)
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return fmt.Errorf("admin: api key user ID %d not found: %w", id, sql.ErrNoRows)
		}
		return nil
	})
}

// getByUserID returns the API key user with the given user_id, or nil if none.
func (b *apiKeyStoreBase) getByUserID(ctx context.Context, userID string) (*APIKeyUser, error) {
	query := b.dialect.Rebind("SELECT id, api_key, user_id, description, created_at, updated_at FROM api_key_users WHERE user_id = ?")
	var u APIKeyUser
	err := b.db.QueryRowContext(ctx, query, userID).Scan(&u.ID, &u.APIKey, &u.UserID, &u.Description, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("admin: get api key user by user_id: %w", err)
	}
	return &u, nil
}

// requireUniqueUserID checks that no other API key user (excluding excludeID) uses the given userID.
// Returns ErrUserIDExists on conflict, or any DB error from the lookup.
func (a *AdminAPI) requireUniqueUserID(ctx context.Context, userID string, excludeID int64) error {
	existing, err := a.akStore.getByUserID(ctx, userID)
	if err != nil {
		return fmt.Errorf("admin: check unique user_id: %w", err)
	}
	if existing != nil && existing.ID != excludeID {
		return fmt.Errorf("admin: check unique user_id: %w", ErrUserIDExists)
	}
	return nil
}

// apiKeyUserStore implements APIKeyUserStorer backed by SQLite.
// PG-backed callers use pgStore (apikey_pg_store.go) instead.
type apiKeyUserStore struct {
	apiKeyStoreBase
}

// APIKeyUserStorer defines CRUD operations for API key user records.
type APIKeyUserStorer interface {
	list(ctx context.Context) ([]APIKeyUser, error)
	get(ctx context.Context, id int64) (*APIKeyUser, error)
	getByUserID(ctx context.Context, userID string) (*APIKeyUser, error)
	create(ctx context.Context, u *APIKeyUser) error
	update(ctx context.Context, id int64, u *APIKeyUser) error
	delete(ctx context.Context, id int64) error
	Invalidator() cacheInvalidator
	SetInvalidator(cacheInvalidator)
}

// cacheInvalidator clears cached resolver entries after CUD operations.
type cacheInvalidator interface {
	Invalidate(key string)
}

// KeyValidator syncs database-sourced API keys into the authentication layer.
// Implemented by security.Authenticator; injected via Deps.
type KeyValidator interface {
	AddKey(key string)
	RemoveKey(key string)
}

func newAPIKeyUserStoreWithInvalidator(db DBExecutor, inv cacheInvalidator, writeMu *sqlutil.WriteMu) APIKeyUserStorer {
	if db == nil {
		return nil
	}
	return &apiKeyUserStore{
		apiKeyStoreBase: apiKeyStoreBase{
			db:          db,
			invalidator: inv,
			dialect:     dbutil.DialectSQLite,
			writeMu:     writeMu,
		},
	}
}

var _ APIKeyUserStorer = (*apiKeyUserStore)(nil)

func (s *apiKeyUserStore) create(ctx context.Context, u *APIKeyUser) error {
	if err := s.ensureAPIKey(u); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	return s.writeMu.WithLock(func() error {
		res, err := s.db.ExecContext(ctx,
			"INSERT INTO api_key_users (api_key, user_id, description, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
			u.APIKey, u.UserID, u.Description, now, now)
		if err != nil {
			if s.dialect.IsUniqueViolation(err) {
				return fmt.Errorf("admin: create api key user: %w", ErrUserIDExists)
			}
			return fmt.Errorf("admin: create api key user: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("admin: get inserted api key user id: %w", err)
		}
		u.ID = id
		return nil
	})
}

func (s *apiKeyUserStore) update(ctx context.Context, id int64, u *APIKeyUser) error {
	now := time.Now().UTC().Format(time.RFC3339)
	// NOTE: api_key is immutable after creation — never add it to SET clause
	// without also calling KeyValidator.RemoveKey(old) + AddKey(new).
	return s.writeMu.WithLock(func() error {
		res, err := s.db.ExecContext(ctx,
			"UPDATE api_key_users SET user_id = ?, description = ?, updated_at = ? WHERE id = ?",
			u.UserID, u.Description, now, id)
		if err != nil {
			if s.dialect.IsUniqueViolation(err) {
				return fmt.Errorf("admin: update api key user: %w", ErrUserIDExists)
			}
			return fmt.Errorf("admin: update api key user: %w", err)
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return fmt.Errorf("admin: api key user ID %d not found: %w", id, sql.ErrNoRows)
		}
		return nil
	})
}

// HandleAPIKeyUserList returns all API key users.
//
// @Summary      List API key users
// @Description  Returns all API key user records. API keys are masked in the response. Requires admin:read scope.
// @Tags         Admin API
// @Produce      json
// @Security     AdminBearerAuth
// @Success      200  {array}   APIKeyUser
// @Failure      403  {object}  ErrorResponse  "Insufficient scope: need admin:read"
// @Failure      500  {object}  ErrorResponse  "Internal error"
// @Router       /admin/api-keys [get]
func (a *AdminAPI) HandleAPIKeyUserList(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeAdminRead) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "insufficient scope: need admin:read")
		return
	}
	if a.akStore == nil {
		respondJSON(w, []APIKeyUser{})
		return
	}
	users, err := a.akStore.list(r.Context())
	if err != nil {
		a.log.Error("admin: list api key users", "error", err)
		web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	if users == nil {
		users = []APIKeyUser{}
	}
	for i := range users {
		users[i].APIKey = maskAPIKey(users[i].APIKey)
	}
	respondJSON(w, users)
}

// HandleAPIKeyUserCreate creates a new API key user.
//
// @Summary      Create API key user
// @Description  Creates a new API key user. If api_key is omitted, one is auto-generated. Returns the full API key only on creation. Requires admin:write scope.
// @Tags         Admin API
// @Accept       json
// @Produce      json
// @Security     AdminBearerAuth
// @Param        body  body      CreateAPIKeyRequest  true  "API key user to create"
// @Success      201   {object}  APIKeyUser
// @Failure      400   {object}  ErrorResponse  "Invalid JSON or validation failed"
// @Failure      403   {object}  ErrorResponse  "Insufficient scope: need admin:write"
// @Failure      409   {object}  ErrorResponse  "user_id already exists"
// @Failure      500   {object}  ErrorResponse  "Create failed"
// @Router       /admin/api-keys [post]
func (a *AdminAPI) HandleAPIKeyUserCreate(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeAdminWrite) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "insufficient scope: need admin:write")
		return
	}
	if a.akStore == nil {
		web.WriteAppError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "database resolver not enabled")
		return
	}
	var u APIKeyUser
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&u); err != nil {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid json")
		return
	}
	if u.UserID == "" || len(u.UserID) > 128 {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "user_id is required (max 128 chars)")
		return
	}
	if len(u.Description) > 512 {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "description too long (max 512 chars)")
		return
	}
	if err := a.requireUniqueUserID(r.Context(), u.UserID, 0); err != nil {
		respondStoreError(w, a.log, "admin: check unique user_id", err)
		return
	}
	if err := a.akStore.create(r.Context(), &u); err != nil {
		respondStoreError(w, a.log, "admin: create api key user", err)
		return
	}
	if inv := a.akStore.Invalidator(); inv != nil {
		inv.Invalidate(u.APIKey)
	}
	if a.keyValidator != nil {
		a.keyValidator.AddKey(u.APIKey)
	}
	w.WriteHeader(http.StatusCreated)
	respondJSON(w, u)
}

// HandleAPIKeyUserGet returns a single API key user.
//
// @Summary      Get API key user
// @Description  Returns a single API key user by ID. API key is masked. Requires admin:read scope.
// @Tags         Admin API
// @Produce      json
// @Security     AdminBearerAuth
// @Param        id   path      int  true  "API key user ID"
// @Success      200  {object}  APIKeyUser
// @Failure      400  {object}  ErrorResponse  "Invalid ID"
// @Failure      403  {object}  ErrorResponse  "Insufficient scope: need admin:read"
// @Failure      404  {object}  ErrorResponse  "Not found"
// @Failure      500  {object}  ErrorResponse  "Internal error"
// @Router       /admin/api-keys/{id} [get]
func (a *AdminAPI) HandleAPIKeyUserGet(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeAdminRead) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "insufficient scope: need admin:read")
		return
	}
	if a.akStore == nil {
		web.WriteAppError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "database resolver not enabled")
		return
	}
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid id")
		return
	}
	u, err := a.akStore.get(r.Context(), id)
	if err != nil {
		// Distinguish not-found (404) from transient DB failures (500);
		// get() wraps sql.ErrNoRows via %w so respondStoreError can detect it.
		respondStoreError(w, a.log, "admin: get api key user", err)
		return
	}
	u.APIKey = maskAPIKey(u.APIKey)
	respondJSON(w, u)
}

// HandleAPIKeyUserUpdate updates an existing API key user.
//
// @Summary      Update API key user
// @Description  Updates the user_id and description of an existing API key user. The api_key itself is immutable. Requires admin:write scope.
// @Tags         Admin API
// @Accept       json
// @Produce      json
// @Security     AdminBearerAuth
// @Param        id    path      int              true  "API key user ID"
// @Param        body  body      CreateAPIKeyRequest  true  "Fields to update"
// @Success      200   {object}  APIKeyUser
// @Failure      400   {object}  ErrorResponse  "Invalid JSON or validation failed"
// @Failure      403   {object}  ErrorResponse  "Insufficient scope: need admin:write"
// @Failure      404   {object}  ErrorResponse  "Not found"
// @Failure      409   {object}  ErrorResponse  "user_id already exists"
// @Failure      500   {object}  ErrorResponse  "Internal error"
// @Router       /admin/api-keys/{id} [patch]
func (a *AdminAPI) HandleAPIKeyUserUpdate(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeAdminWrite) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "insufficient scope: need admin:write")
		return
	}
	if a.akStore == nil {
		web.WriteAppError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "database resolver not enabled")
		return
	}
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid id")
		return
	}
	var u APIKeyUser
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&u); err != nil {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid json")
		return
	}
	if u.UserID == "" || len(u.UserID) > 128 {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "user_id is required (max 128 chars)")
		return
	}
	if len(u.Description) > 512 {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "description too long (max 512 chars)")
		return
	}

	oldUser, err := a.akStore.get(r.Context(), id)
	if err != nil {
		// Distinguish not-found (404) from transient DB failures (500).
		respondStoreError(w, a.log, "admin: get api key user for update", err)
		return
	}
	if u.UserID != oldUser.UserID {
		if err := a.requireUniqueUserID(r.Context(), u.UserID, id); err != nil {
			respondStoreError(w, a.log, "admin: check unique user_id", err)
			return
		}
	}

	if err := a.akStore.update(r.Context(), id, &u); err != nil {
		respondStoreError(w, a.log, "admin: update api key user", err)
		return
	}
	if inv := a.akStore.Invalidator(); inv != nil {
		inv.Invalidate(oldUser.APIKey)
	}
	respondJSON(w, APIKeyUser{ID: id, APIKey: maskAPIKey(oldUser.APIKey), UserID: u.UserID, Description: u.Description})
}

// HandleAPIKeyUserDelete deletes an API key user.
//
// @Summary      Delete API key user
// @Description  Permanently deletes an API key user and revokes the associated API key. Requires admin:write scope.
// @Tags         Admin API
// @Security     AdminBearerAuth
// @Param        id   path  int  true  "API key user ID"
// @Success      204  "Deleted"
// @Failure      400  {object}  ErrorResponse  "Invalid ID"
// @Failure      403  {object}  ErrorResponse  "Insufficient scope: need admin:write"
// @Failure      404  {object}  ErrorResponse  "Not found"
// @Failure      500  {object}  ErrorResponse  "Internal error"
// @Router       /admin/api-keys/{id} [delete]
func (a *AdminAPI) HandleAPIKeyUserDelete(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeAdminWrite) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "insufficient scope: need admin:write")
		return
	}
	if a.akStore == nil {
		web.WriteAppError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "database resolver not enabled")
		return
	}
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid id")
		return
	}

	u, err := a.akStore.get(r.Context(), id)
	if err != nil {
		// Distinguish not-found (404) from transient DB failures (500).
		respondStoreError(w, a.log, "admin: get api key user for delete", err)
		return
	}

	if err := a.akStore.delete(r.Context(), id); err != nil {
		respondStoreError(w, a.log, "admin: delete api key user", err)
		return
	}
	if inv := a.akStore.Invalidator(); inv != nil {
		inv.Invalidate(u.APIKey)
	}
	if a.keyValidator != nil {
		a.keyValidator.RemoveKey(u.APIKey)
	}
	w.WriteHeader(http.StatusNoContent)
}
