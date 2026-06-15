// Package security provides authentication and input validation middleware.
package security

import (
	"context"
	"errors"
)

// User is the canonical user record surfaced to the identity layer.
// Defined in security (not session) to keep the dependency direction single:
// session implements UserStore; security never imports session.
type User struct {
	ID           string
	Username     string
	PasswordHash string
	Role         string // "admin" | "user"
	Status       string // "active" | "disabled"
	DisplayName  string
}

// ErrUserNotFound is returned by UserStore when no user matches the lookup.
// session.SQLiteStore / session.pgStore return this sentinel.
var ErrUserNotFound = errors.New("security: user not found")

// UserStore is the persistence interface required by LocalAccountProvider.
// Implemented by session stores; defined here so security has no inbound
// dependency on session (avoids import cycle).
type UserStore interface {
	CreateUser(ctx context.Context, u *User, now int64) error
	GetUserByID(ctx context.Context, id string) (*User, error)
	GetUserByUsername(ctx context.Context, username string) (*User, error)
}

// Credentials is a marker interface for credential payloads.
type Credentials interface{ Kind() string }

// LoginCredentials carries username/password for the account-login channel.
type LoginCredentials struct {
	Username string
	Password string
}

// Kind implements Credentials.
func (LoginCredentials) Kind() string { return "login" }

// IdentityProvider authenticates credentials and looks up users.
// LocalAccountProvider is the first implementation (spec §8.1);
// OAuthProvider is a future second implementation that does not change callers.
type IdentityProvider interface {
	// Authenticate validates credentials and returns the user ID.
	Authenticate(ctx context.Context, creds Credentials) (userID string, err error)
	// Lookup returns the user by ID.
	Lookup(ctx context.Context, userID string) (*User, error)
}

// IdentityError is the error envelope for the identity layer (spec §12).
type IdentityError struct{ Code string }

// Error implements error.
func (e *IdentityError) Error() string { return "identity: " + e.Code }

// Sentinel identity error codes (spec §12).
const (
	ErrCodeInvalidCredentials = "INVALID_CREDENTIALS"
	ErrCodeUserDisabled       = "USER_DISABLED"
	ErrCodeUserNotFound       = "USER_NOT_FOUND"
)
