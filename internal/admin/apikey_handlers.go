package admin

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// maskAPIKey returns a masked version showing only first 8 and last 4 chars.
func maskAPIKey(key string) string {
	if len(key) <= 12 {
		return "****"
	}
	return key[:8] + "****" + key[len(key)-4:]
}

// APIKeyUser represents a mapping from an API key to a user identity.
type APIKeyUser struct {
	APIKey      string `json:"api_key"`
	UserID      string `json:"user_id"`
	Description string `json:"description,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

type apiKeyUserStore struct {
	db          *sql.DB
	invalidator cacheInvalidator
}

// cacheInvalidator clears cached resolver entries after CUD operations.
type cacheInvalidator interface {
	Invalidate(key string)
}

func newAPIKeyUserStoreWithInvalidator(db *sql.DB, inv cacheInvalidator) *apiKeyUserStore {
	if db == nil {
		return nil
	}
	return &apiKeyUserStore{db: db, invalidator: inv}
}

func (s *apiKeyUserStore) list(ctx context.Context) ([]APIKeyUser, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT api_key, user_id, description, created_at, updated_at FROM api_key_users ORDER BY created_at DESC")
	if err != nil {
		return nil, fmt.Errorf("admin: list api key users: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []APIKeyUser
	for rows.Next() {
		var u APIKeyUser
		if err := rows.Scan(&u.APIKey, &u.UserID, &u.Description, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, fmt.Errorf("admin: scan api key user: %w", err)
		}
		result = append(result, u)
	}
	return result, rows.Err()
}

func (s *apiKeyUserStore) get(ctx context.Context, apiKey string) (*APIKeyUser, error) {
	var u APIKeyUser
	err := s.db.QueryRowContext(ctx,
		"SELECT api_key, user_id, description, created_at, updated_at FROM api_key_users WHERE api_key = ?", apiKey,
	).Scan(&u.APIKey, &u.UserID, &u.Description, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *apiKeyUserStore) create(ctx context.Context, u *APIKeyUser) error {
	if u.APIKey == "" {
		key := make([]byte, 24)
		if _, err := rand.Read(key); err != nil {
			return fmt.Errorf("admin: generate api key: %w", err)
		}
		u.APIKey = "hpk_" + hex.EncodeToString(key)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO api_key_users (api_key, user_id, description, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		u.APIKey, u.UserID, u.Description, now, now)
	if err != nil {
		return fmt.Errorf("admin: create api key user: %w", err)
	}
	return nil
}

func (s *apiKeyUserStore) update(ctx context.Context, apiKey string, u *APIKeyUser) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		"UPDATE api_key_users SET user_id = ?, description = ?, updated_at = ? WHERE api_key = ?",
		u.UserID, u.Description, now, apiKey)
	if err != nil {
		return fmt.Errorf("admin: update api key user: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("admin: api key user %q not found", apiKey)
	}
	return nil
}

func (s *apiKeyUserStore) delete(ctx context.Context, apiKey string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM api_key_users WHERE api_key = ?", apiKey)
	if err != nil {
		return fmt.Errorf("admin: delete api key user: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("admin: api key user %q not found", apiKey)
	}
	return nil
}

func (a *AdminAPI) HandleAPIKeyUserList(w http.ResponseWriter, r *http.Request) {
	if a.akStore == nil {
		respondJSON(w, []APIKeyUser{})
		return
	}
	users, err := a.akStore.list(r.Context())
	if err != nil {
		a.log.Error("admin: list api key users", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
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

func (a *AdminAPI) HandleAPIKeyUserCreate(w http.ResponseWriter, r *http.Request) {
	if a.akStore == nil {
		http.Error(w, "database resolver not enabled", http.StatusNotImplemented)
		return
	}
	var u APIKeyUser
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&u); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if u.UserID == "" || len(u.UserID) > 128 {
		http.Error(w, "user_id is required (max 128 chars)", http.StatusBadRequest)
		return
	}
	if len(u.Description) > 512 {
		http.Error(w, "description too long (max 512 chars)", http.StatusBadRequest)
		return
	}
	if err := a.akStore.create(r.Context(), &u); err != nil {
		a.log.Error("admin: create api key user", "error", err)
		http.Error(w, "create failed", http.StatusInternalServerError)
		return
	}
	if a.akStore.invalidator != nil {
		a.akStore.invalidator.Invalidate(u.APIKey)
	}
	w.WriteHeader(http.StatusCreated)
	respondJSON(w, u)
}

func (a *AdminAPI) HandleAPIKeyUserGet(w http.ResponseWriter, r *http.Request) {
	if a.akStore == nil {
		http.Error(w, "database resolver not enabled", http.StatusNotImplemented)
		return
	}
	apiKey := r.PathValue("key")
	u, err := a.akStore.get(r.Context(), apiKey)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	u.APIKey = maskAPIKey(u.APIKey)
	respondJSON(w, u)
}

func (a *AdminAPI) HandleAPIKeyUserUpdate(w http.ResponseWriter, r *http.Request) {
	if a.akStore == nil {
		http.Error(w, "database resolver not enabled", http.StatusNotImplemented)
		return
	}
	apiKey := r.PathValue("key")
	var u APIKeyUser
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&u); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if u.UserID == "" || len(u.UserID) > 128 {
		http.Error(w, "user_id is required (max 128 chars)", http.StatusBadRequest)
		return
	}
	if len(u.Description) > 512 {
		http.Error(w, "description too long (max 512 chars)", http.StatusBadRequest)
		return
	}
	if err := a.akStore.update(r.Context(), apiKey, &u); err != nil {
		a.log.Error("admin: update api key user", "error", err)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if a.akStore.invalidator != nil {
		a.akStore.invalidator.Invalidate(apiKey)
	}
	respondJSON(w, APIKeyUser{APIKey: apiKey, UserID: u.UserID, Description: u.Description})
}

func (a *AdminAPI) HandleAPIKeyUserDelete(w http.ResponseWriter, r *http.Request) {
	if a.akStore == nil {
		http.Error(w, "database resolver not enabled", http.StatusNotImplemented)
		return
	}
	apiKey := r.PathValue("key")
	if err := a.akStore.delete(r.Context(), apiKey); err != nil {
		a.log.Error("admin: delete api key user", "error", err)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if a.akStore.invalidator != nil {
		a.akStore.invalidator.Invalidate(apiKey)
	}
	w.WriteHeader(http.StatusNoContent)
}
