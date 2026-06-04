package admin

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/dbutil"
)

func newAPIKeyPGMock(t *testing.T) (*pgStore, sqlmock.Sqlmock, func()) {
	t.Helper()
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)

	db := &dbutil.DB{DB: mockDB}

	store := &pgStore{
		db:      db,
		dialect: dbutil.DialectPostgres,
	}

	return store, mock, func() {
		require.NoError(t, mock.ExpectationsWereMet())
		mockDB.Close()
	}
}

func TestAPIKeyUserPGStore_create(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newAPIKeyPGMock(t)
	defer cleanup()

	q := dbutil.DialectPostgres.Rebind(
		`INSERT INTO api_key_users (api_key, user_id, description) VALUES (?, ?, ?) RETURNING id`)

	rows := sqlmock.NewRows([]string{"id"}).AddRow(int64(42))

	mock.ExpectQuery(regexp.QuoteMeta(q)).
		WithArgs("hpk_testkey", "user-1", "test user").
		WillReturnRows(rows)

	u := &APIKeyUser{APIKey: "hpk_testkey", UserID: "user-1", Description: "test user"}
	err := store.create(context.Background(), u)
	require.NoError(t, err)
	require.Equal(t, int64(42), u.ID)
}

func TestAPIKeyUserPGStore_get(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newAPIKeyPGMock(t)
	defer cleanup()

	q := dbutil.DialectPostgres.Rebind(
		`SELECT id, api_key, user_id, description, created_at, updated_at FROM api_key_users WHERE id = ?`)

	rows := sqlmock.NewRows([]string{"id", "api_key", "user_id", "description", "created_at", "updated_at"}).
		AddRow(int64(1), "hpk_abc", "user-x", "desc", "2025-01-01", "2025-01-02")

	mock.ExpectQuery(regexp.QuoteMeta(q)).WithArgs(int64(1)).WillReturnRows(rows)

	result, err := store.get(context.Background(), 1)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, int64(1), result.ID)
	require.Equal(t, "hpk_abc", result.APIKey)
	require.Equal(t, "user-x", result.UserID)
	require.Equal(t, "desc", result.Description)
	require.Equal(t, "2025-01-01", result.CreatedAt)
	require.Equal(t, "2025-01-02", result.UpdatedAt)
}

func TestAPIKeyUserPGStore_list(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newAPIKeyPGMock(t)
	defer cleanup()

	q := `SELECT id, api_key, user_id, description, created_at, updated_at FROM api_key_users ORDER BY created_at DESC`

	rows := sqlmock.NewRows([]string{"id", "api_key", "user_id", "description", "created_at", "updated_at"}).
		AddRow(int64(2), "hpk_def", "user-b", "second", "2025-01-02", "2025-01-03").
		AddRow(int64(1), "hpk_abc", "user-a", "first", "2025-01-01", "2025-01-02")

	mock.ExpectQuery(regexp.QuoteMeta(q)).WillReturnRows(rows)

	results, err := store.list(context.Background())
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, int64(2), results[0].ID)
	require.Equal(t, "hpk_def", results[0].APIKey)
	require.Equal(t, int64(1), results[1].ID)
	require.Equal(t, "hpk_abc", results[1].APIKey)
}

func TestAPIKeyUserPGStore_delete(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newAPIKeyPGMock(t)
	defer cleanup()

	q := dbutil.DialectPostgres.Rebind(`DELETE FROM api_key_users WHERE id = ?`)

	mock.ExpectExec(regexp.QuoteMeta(q)).WithArgs(int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := store.delete(context.Background(), 1)
	require.NoError(t, err)
}
