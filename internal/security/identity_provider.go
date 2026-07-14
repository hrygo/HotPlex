// Package security provides authentication and input validation middleware.
package security

import (
	"context"
	"errors"
	"strings"
)

// User is the canonical user record surfaced to the identity layer.
// Defined in security (not session) to keep the dependency direction single:
// session implements UserStore; security never imports session.
type User struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"`      // 永不序列化：bcrypt 哈希禁止离开服务端（防 AdminListUsers 泄漏，spec §11.2）
	Role         string `json:"role"`   // "admin" | "user"
	Status       string `json:"status"` // "active" | "disabled"
	DisplayName  string `json:"display_name"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
	LastLoginAt  int64  `json:"last_login_at"` // 0 = 从未登录（DB NULL）
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
	// ListByIDs batch-resolves users by ID. Used by admin views (sessions /
	// activity) to render readable display names instead of raw IDs. Returns a
	// map keyed by user ID; IDs with no row are absent (not an error). An empty
	// or nil input returns an empty map without hitting the DB.
	ListByIDs(ctx context.Context, ids []string) (map[string]*User, error)
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

// Username policy bounds (spec §8 account-login channel).
const (
	UsernameMinLen = 3
	UsernameMaxLen = 64
)

// ReservedUsernamePrefix is the namespace migration 018 uses to provision
// API-key pseudo-users (username = "apikey:" || user_id). A real account with
// this prefix would collide with that namespace: it becomes fully account-login
// able (blurring the API-key vs account-login boundary migration 018 relies on)
// and can hijack migration 018's WHERE NOT EXISTS guard into re-pointing a
// legitimate API key at the attacker-controlled account (identity takeover).
// ValidateUsername rejects it (code-review fix).
const ReservedUsernamePrefix = "apikey:"

// ErrInvalidUsername is returned by ValidateUsername on any policy violation.
var ErrInvalidUsername = errors.New("security: invalid username")

// ValidateUsername enforces the account-login username policy: length 3-64,
// charset [a-zA-Z0-9_.-], and must not collide with the reserved API-key
// namespace. Applied at every user-creation entry point (accept-invite, admin
// CLI) so the reserved prefix can never reach the users table (review fix).
func ValidateUsername(username string) error {
	if len(username) < UsernameMinLen || len(username) > UsernameMaxLen {
		return ErrInvalidUsername
	}
	// Explicit reserved-namespace guard (the charset check below also rejects
	// the ":" in the prefix; kept for documentation and to survive a future
	// charset relaxation).
	if strings.HasPrefix(username, ReservedUsernamePrefix) {
		return ErrInvalidUsername
	}
	for _, r := range username {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == '.':
		default:
			return ErrInvalidUsername
		}
	}
	return nil
}
