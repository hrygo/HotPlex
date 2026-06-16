package security

import (
	"context"
	"errors"

	"golang.org/x/crypto/bcrypt"
)

// BcryptCostDefault is the project's standard bcrypt cost (spec 附录 B).
const BcryptCostDefault = 12

var (
	errInvalidCredentials = &IdentityError{Code: ErrCodeInvalidCredentials}
	errUserDisabled       = &IdentityError{Code: ErrCodeUserDisabled}
)

// LocalAccountProvider authenticates against the users table via bcrypt (spec §8.1).
// The first IdentityProvider implementation; OAuthProvider is a future second
// implementation that does not change callers.
type LocalAccountProvider struct {
	store UserStore
	cost  int
}

// NewLocalAccountProvider constructs a provider with the given bcrypt cost.
func NewLocalAccountProvider(store UserStore, bcryptCost int) *LocalAccountProvider {
	return &LocalAccountProvider{store: store, cost: bcryptCost}
}

// Authenticate validates login credentials and returns the user ID (spec §8.2).
// Anti-enumeration invariant: a caller WITHOUT the correct password must never
// learn whether the username exists or is disabled. All wrong-password cases
// (user-not-found, API-key-provisioned, disabled, bad-password) collapse to a
// single INVALID_CREDENTIALS response. USER_DISABLED is surfaced ONLY after the
// password is verified correct — so an attacker probing with a guessed password
// cannot distinguish a disabled real account from a nonexistent one (review fix).
func (p *LocalAccountProvider) Authenticate(ctx context.Context, creds Credentials) (string, error) {
	lc, ok := creds.(LoginCredentials)
	if !ok {
		return "", errInvalidCredentials
	}
	u, err := p.store.GetUserByUsername(ctx, lc.Username)
	// User-not-found also returns INVALID_CREDENTIALS (anti-enumeration).
	if errors.Is(err, ErrUserNotFound) || u == nil {
		return "", errInvalidCredentials
	}
	if err != nil {
		return "", err
	}
	// Empty hash = API-key provisioned user; account-login channel forbidden (spec §13.2).
	if u.PasswordHash == "" {
		return "", errInvalidCredentials
	}
	// Verify the password FIRST. A wrong password must never reveal account
	// existence or status — return INVALID_CREDENTIALS regardless of disabled.
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(lc.Password)); err != nil {
		return "", errInvalidCredentials
	}
	// Password correct: now safe to surface the disabled state to the genuine
	// account holder without leaking it to password-probing attackers.
	if u.Status == "disabled" {
		return "", errUserDisabled
	}
	return u.ID, nil
}

// Lookup returns the user by ID (spec §8.1).
func (p *LocalAccountProvider) Lookup(ctx context.Context, userID string) (*User, error) {
	return p.store.GetUserByID(ctx, userID)
}

// HashPassword hashes a plaintext password at the provider's cost.
func (p *LocalAccountProvider) HashPassword(plaintext string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plaintext), p.cost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
