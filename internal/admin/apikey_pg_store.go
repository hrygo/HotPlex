package admin

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/hrygo/hotplex/internal/dbutil"
)

// pgStore implements APIKeyUserStorer backed by PostgreSQL.
// It embeds apiKeyStoreBase for shared list/get/delete/invalidator logic.
type pgStore struct {
	apiKeyStoreBase
}

// NewAPIKeyUserPGStore creates a PG-backed API key store.
func NewAPIKeyUserPGStore(db *dbutil.DB, inv cacheInvalidator) APIKeyUserStorer {
	if db == nil {
		return nil
	}
	return &pgStore{
		apiKeyStoreBase: apiKeyStoreBase{
			db:          db,
			invalidator: inv,
			dialect:     dbutil.DialectPostgres,
			writeMu:     nil, // PG handles concurrency natively; WriteMu.WithLock is nil-safe
		},
	}
}

var (
	_ APIKeyUserStorer = (*pgStore)(nil)
	_                  = NewAPIKeyUserPGStore
)

func (s *pgStore) create(ctx context.Context, u *APIKeyUser) error {
	if err := s.ensureAPIKey(u); err != nil {
		return err
	}
	// created_at and updated_at use DEFAULT NOW() in the Postgres schema.
	return s.writeMu.WithLock(func() error {
		query := s.dialect.Rebind("INSERT INTO api_key_users (api_key, user_id, description) VALUES (?, ?, ?) RETURNING id")
		if err := s.db.QueryRowContext(ctx, query, u.APIKey, u.UserID, u.Description).Scan(&u.ID); err != nil {
			if s.dialect.IsUniqueViolation(err) {
				return fmt.Errorf("admin: create api key user: %w", ErrUserIDExists)
			}
			return fmt.Errorf("admin: create api key user: %w", err)
		}
		return nil
	})
}

func (s *pgStore) update(ctx context.Context, id int64, u *APIKeyUser) error {
	// NOTE: api_key is immutable after creation — never add it to SET clause
	// without also calling KeyValidator.RemoveKey(old) + AddKey(new).
	return s.writeMu.WithLock(func() error {
		query := s.dialect.Rebind("UPDATE api_key_users SET user_id = ?, description = ?, updated_at = NOW() WHERE id = ?")
		res, err := s.db.ExecContext(ctx, query, u.UserID, u.Description, id)
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
