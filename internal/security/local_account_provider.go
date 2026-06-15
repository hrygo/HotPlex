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
// All failure modes except "disabled" return INVALID_CREDENTIALS to prevent
// username enumeration.
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
	if u.Status == "disabled" {
		return "", errUserDisabled
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(lc.Password)); err != nil {
		return "", errInvalidCredentials
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
