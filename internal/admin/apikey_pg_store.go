package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/hrygo/hotplex/internal/dbutil"
)

// pgStore implements APIKeyUserStorer backed by PostgreSQL.
type pgStore struct {
	db          *dbutil.DB
	dialect     dbutil.Dialect
	mu          sync.Mutex
	invalidator cacheInvalidator
}

// NewAPIKeyUserPGStore creates a PG-backed API key store.
func NewAPIKeyUserPGStore(db *dbutil.DB, inv cacheInvalidator) APIKeyUserStorer {
	if db == nil {
		return nil
	}
	return &pgStore{
		db:          db,
		dialect:     db.Dialect(),
		invalidator: inv,
	}
}

// SetInvalidator sets the cache invalidator used after API key CRUD operations.
// Safe for concurrent use — last call wins.
func (s *pgStore) SetInvalidator(inv cacheInvalidator) {
	s.mu.Lock()
	s.invalidator = inv
	s.mu.Unlock()
}

var (
	_ APIKeyUserStorer = (*pgStore)(nil)
	_                  = NewAPIKeyUserPGStore
)

func (s *pgStore) Invalidator() cacheInvalidator {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.invalidator
}

func (s *pgStore) list(ctx context.Context) ([]APIKeyUser, error) {
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
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("admin: iterate api key users: %w", err)
	}
	return result, nil
}

func (s *pgStore) get(ctx context.Context, id int64) (*APIKeyUser, error) {
	var u APIKeyUser
	query := s.dialect.Rebind(
		"SELECT id, api_key, user_id, description, created_at, updated_at FROM api_key_users WHERE id = ?")
	err := s.db.QueryRowContext(ctx, query, id).
		Scan(&u.ID, &u.APIKey, &u.UserID, &u.Description, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("admin: get api key user: %w", err)
	}
	return &u, nil
}

func (s *pgStore) create(ctx context.Context, u *APIKeyUser) error {
	if u.APIKey == "" {
		key := make([]byte, 24)
		if _, err := rand.Read(key); err != nil {
			return fmt.Errorf("admin: generate api key: %w", err)
		}
		u.APIKey = "hpk_" + hex.EncodeToString(key)
	}
	// created_at and updated_at use DEFAULT NOW() in the Postgres schema.
	query := s.dialect.Rebind("INSERT INTO api_key_users (api_key, user_id, description) VALUES (?, ?, ?) RETURNING id")
	if err := s.db.QueryRowContext(ctx, query, u.APIKey, u.UserID, u.Description).Scan(&u.ID); err != nil {
		return fmt.Errorf("admin: create api key user: %w", err)
	}
	return nil
}

func (s *pgStore) update(ctx context.Context, id int64, u *APIKeyUser) error {
	// NOTE: api_key is immutable after creation — never add it to SET clause
	// without also calling KeyValidator.RemoveKey(old) + AddKey(new).
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

func (s *pgStore) delete(ctx context.Context, id int64) error {
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
