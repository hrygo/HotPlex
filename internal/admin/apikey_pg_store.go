package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/hrygo/hotplex/internal/dbutil"
)

// apiKeyUserPGStore implements APIKeyUserStorer backed by PostgreSQL.
type apiKeyUserPGStore struct {
	db          *dbutil.DB
	dialect     dbutil.Dialect
	invalidator cacheInvalidator
}

// NewAPIKeyUserPGStore creates a PostgreSQL-backed API key user store.
func NewAPIKeyUserPGStore(db *dbutil.DB, inv cacheInvalidator) APIKeyUserStorer {
	if db == nil {
		return nil
	}
	return &apiKeyUserPGStore{
		db:          db,
		dialect:     db.Dialect(),
		invalidator: inv,
	}
}

var (
	_ APIKeyUserStorer = (*apiKeyUserPGStore)(nil)
	// Ensure constructor is referenced (used by DI wiring in cmd/hotplex)
	_ = NewAPIKeyUserPGStore
)

func (s *apiKeyUserPGStore) Invalidator() cacheInvalidator { return s.invalidator }

func (s *apiKeyUserPGStore) list(ctx context.Context) ([]APIKeyUser, error) {
	rows, err := s.db.QueryContext(ctx,
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
	return result, rows.Err()
}

func (s *apiKeyUserPGStore) get(ctx context.Context, id int64) (*APIKeyUser, error) {
	var u APIKeyUser
	query := s.dialect.Rebind(
		"SELECT id, api_key, user_id, description, created_at, updated_at FROM api_key_users WHERE id = ?")
	err := s.db.QueryRowContext(ctx, query, id).
		Scan(&u.ID, &u.APIKey, &u.UserID, &u.Description, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *apiKeyUserPGStore) create(ctx context.Context, u *APIKeyUser) error {
	if u.APIKey == "" {
		key := make([]byte, 24)
		if _, err := rand.Read(key); err != nil {
			return fmt.Errorf("admin: generate api key: %w", err)
		}
		u.APIKey = "hpk_" + hex.EncodeToString(key)
	}
	// created_at and updated_at use DEFAULT NOW() in the Postgres schema.
	query := "INSERT INTO api_key_users (api_key, user_id, description) VALUES ($1, $2, $3) RETURNING id"
	return s.db.QueryRowContext(ctx, query, u.APIKey, u.UserID, u.Description).Scan(&u.ID)
}

func (s *apiKeyUserPGStore) update(ctx context.Context, id int64, u *APIKeyUser) error {
	query := s.dialect.Rebind("UPDATE api_key_users SET user_id = ?, description = ?, updated_at = NOW() WHERE id = ?")
	res, err := s.db.ExecContext(ctx, query, u.UserID, u.Description, id)
	if err != nil {
		return fmt.Errorf("admin: update api key user: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("admin: api key user ID %d not found", id)
	}
	return nil
}

func (s *apiKeyUserPGStore) delete(ctx context.Context, id int64) error {
	query := s.dialect.Rebind("DELETE FROM api_key_users WHERE id = ?")
	res, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("admin: delete api key user: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("admin: api key user ID %d not found", id)
	}
	return nil
}
