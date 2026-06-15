package security

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"golang.org/x/crypto/bcrypt"
)

// stubUserStore is an in-memory UserStore for provider tests.
type stubUserStore struct {
	byUsername map[string]*User
	byID       map[string]*User
}

func (s *stubUserStore) CreateUser(_ context.Context, u *User, _ int64) error {
	s.byUsername[u.Username] = u
	s.byID[u.ID] = u
	return nil
}
func (s *stubUserStore) GetUserByID(_ context.Context, id string) (*User, error) {
	u, ok := s.byID[id]
	if !ok {
		return nil, ErrUserNotFound
	}
	return u, nil
}
func (s *stubUserStore) GetUserByUsername(_ context.Context, username string) (*User, error) {
	u, ok := s.byUsername[username]
	if !ok {
		return nil, ErrUserNotFound
	}
	return u, nil
}

// testBcryptCost is lower than production (12) to keep tests fast.
const testBcryptCost = 10

func mustHash(t *testing.T, pw string, cost int) string {
	t.Helper()
	b, err := bcrypt.GenerateFromPassword([]byte(pw), cost)
	require.NoError(t, err)
	return string(b)
}

func requireIdentityCode(t *testing.T, err error, want string) {
	t.Helper()
	var ie *IdentityError
	require.ErrorAs(t, err, &ie)
	require.Equal(t, want, ie.Code)
}

func TestLocalAccountProvider_AuthenticateSuccess(t *testing.T) {
	t.Parallel()
	store := &stubUserStore{byUsername: map[string]*User{}, byID: map[string]*User{}}
	prov := NewLocalAccountProvider(store, testBcryptCost)
	store.byUsername["alice"] = &User{ID: "u-1", Username: "alice", PasswordHash: mustHash(t, "s3cret", testBcryptCost), Role: "user", Status: "active"}

	uid, err := prov.Authenticate(context.Background(), LoginCredentials{Username: "alice", Password: "s3cret"})
	require.NoError(t, err)
	require.Equal(t, "u-1", uid)
}

func TestLocalAccountProvider_WrongPassword(t *testing.T) {
	t.Parallel()
	store := &stubUserStore{byUsername: map[string]*User{}}
	prov := NewLocalAccountProvider(store, testBcryptCost)
	store.byUsername["alice"] = &User{ID: "u-1", Username: "alice", PasswordHash: mustHash(t, "s3cret", testBcryptCost), Status: "active"}

	_, err := prov.Authenticate(context.Background(), LoginCredentials{Username: "alice", Password: "wrong"})
	requireIdentityCode(t, err, ErrCodeInvalidCredentials)
}

func TestLocalAccountProvider_DisabledUser(t *testing.T) {
	t.Parallel()
	store := &stubUserStore{byUsername: map[string]*User{}}
	prov := NewLocalAccountProvider(store, testBcryptCost)
	store.byUsername["bob"] = &User{ID: "u-2", Username: "bob", PasswordHash: mustHash(t, "s3cret", testBcryptCost), Status: "disabled"}

	_, err := prov.Authenticate(context.Background(), LoginCredentials{Username: "bob", Password: "s3cret"})
	requireIdentityCode(t, err, ErrCodeUserDisabled)
}

func TestLocalAccountProvider_EmptyHashBlocksLogin(t *testing.T) {
	t.Parallel()
	store := &stubUserStore{byUsername: map[string]*User{}}
	prov := NewLocalAccountProvider(store, testBcryptCost)
	// API-key provisioned user: password_hash='' forbids account login (spec §13.2).
	store.byUsername["apikey:x"] = &User{ID: "u-3", Username: "apikey:x", PasswordHash: "", Status: "active"}

	_, err := prov.Authenticate(context.Background(), LoginCredentials{Username: "apikey:x", Password: "anything"})
	requireIdentityCode(t, err, ErrCodeInvalidCredentials)
}

func TestLocalAccountProvider_UserNotFoundIsInvalidCredentials(t *testing.T) {
	t.Parallel()
	prov := NewLocalAccountProvider(&stubUserStore{byUsername: map[string]*User{}}, testBcryptCost)

	_, err := prov.Authenticate(context.Background(), LoginCredentials{Username: "ghost", Password: "x"})
	requireIdentityCode(t, err, ErrCodeInvalidCredentials) // anti-enumeration
}

func TestLocalAccountProvider_HashPassword(t *testing.T) {
	t.Parallel()
	prov := NewLocalAccountProvider(&stubUserStore{byUsername: map[string]*User{}}, testBcryptCost)
	hash, err := prov.HashPassword("plaintext")
	require.NoError(t, err)
	require.NotEmpty(t, hash)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(hash), []byte("plaintext")))
}
