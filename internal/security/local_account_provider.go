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

	// dummyBcryptHash is a pre-computed bcrypt hash used for constant-time
	// authentication responses. When a user is not found or has no password
	// hash, we compare against this dummy to prevent timing-based username
	// enumeration (the bcrypt work takes ~200ms regardless of user existence).
	dummyBcryptHash = func() string {
		h, _ := bcrypt.GenerateFromPassword([]byte("hotplex-dummy-never-matches"), BcryptCostDefault)
		return string(h)
	}()
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
//
// Timing-attack defense: bcrypt.CompareHashAndPassword is always executed,
// even when the user does not exist, so response times are indistinguishable.
func (p *LocalAccountProvider) Authenticate(ctx context.Context, creds Credentials) (string, error) {
	lc, ok := creds.(LoginCredentials)
	if !ok {
		return "", errInvalidCredentials
	}

	// Look up user; capture whether we found a real hash to compare against.
	var (
		hashToCompare = dummyBcryptHash
		realUser      *User
	)
	u, err := p.store.GetUserByUsername(ctx, lc.Username)
	if err != nil && !errors.Is(err, ErrUserNotFound) {
		return "", err
	}
	if u != nil && u.PasswordHash != "" {
		hashToCompare = u.PasswordHash
		realUser = u
	}

	// Always perform bcrypt comparison to prevent timing-based enumeration.
	// For non-existent users or API-key-only users, this compares against
	// dummyBcryptHash which will never match any real password.
	bcryptErr := bcrypt.CompareHashAndPassword([]byte(hashToCompare), []byte(lc.Password))

	// If we never found a real user with a password hash, reject regardless.
	if realUser == nil {
		return "", errInvalidCredentials
	}

	// Password mismatch on a real user.
	if bcryptErr != nil {
		return "", errInvalidCredentials
	}

	// Password correct: now safe to surface the disabled state to the genuine
	// account holder without leaking it to password-probing attackers.
	if realUser.Status == "disabled" {
		return "", errUserDisabled
	}
	return realUser.ID, nil
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
