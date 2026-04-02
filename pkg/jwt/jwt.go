// Package jwt provides JWT token generation and validation for HotPlex Worker Gateway.
// This package contains the core JWT logic shared by both the gateway server and client implementations.
package jwt

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// TokenTypeAccess is the token type for short-lived access tokens.
	TokenTypeAccess = "access"
	// TokenTypeRefresh is the token type for refresh tokens.
	TokenTypeRefresh = "refresh"
	// TokenTypeGateway is the token type for gateway tokens.
	TokenTypeGateway = "gateway"
)

// DefaultTTL returns the default TTL for a token type.
func DefaultTTL(tokenType string) time.Duration {
	switch tokenType {
	case TokenTypeAccess:
		return 5 * time.Minute
	case TokenTypeGateway:
		return 1 * time.Hour
	case TokenTypeRefresh:
		return 7 * 24 * time.Hour
	default:
		return 5 * time.Minute
	}
}

// Claims represents the JWT claims structure per RFC 7519 and HotPlex design.
type Claims struct {
	jwt.RegisteredClaims

	// HotPlex-specific claims
	UserID    string   `json:"user_id,omitempty"`
	Scopes    []string `json:"scopes,omitempty"`
	Role      string   `json:"role,omitempty"`
	BotID     string   `json:"bot_id,omitempty"`
	SessionID string   `json:"session_id,omitempty"`
	TokenType string   `json:"token_type,omitempty"`
}

// HasScope checks if the claims include a specific scope.
func (c *Claims) HasScope(scope string) bool {
	return slices.Contains(c.Scopes, scope)
}

// Generator generates JWT tokens.
type Generator struct {
	privateKey *ecdsa.PrivateKey
	audience   string
	issuer     string
}

// NewGenerator creates a new JWT token generator.
// The privateKey must be an ECDSA P-256 key for ES256 signing.
func NewGenerator(privateKey *ecdsa.PrivateKey, issuer, audience string) *Generator {
	return &Generator{
		privateKey: privateKey,
		audience:   audience,
		issuer:     issuer,
	}
}

// Generate generates a new JWT token with the given claims.
func (g *Generator) Generate(claims *Claims, ttl time.Duration) (string, error) {
	now := time.Now()

	if claims.ID == "" {
		jti, err := NewJTI()
		if err != nil {
			return "", fmt.Errorf("jwt: generate jti: %w", err)
		}
		claims.ID = jti
	}

	claims.IssuedAt = jwt.NewNumericDate(now)
	claims.ExpiresAt = jwt.NewNumericDate(now.Add(ttl))
	claims.NotBefore = jwt.NewNumericDate(now)
	claims.Issuer = g.issuer
	claims.Audience = jwt.ClaimStrings{g.audience}

	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	return token.SignedString(g.privateKey)
}

// Validator validates JWT tokens.
type Validator struct {
	publicKey any // *ecdsa.PublicKey or []byte (for HMAC)
	audience string
}

// NewValidator creates a new JWT validator.
// secret may be an *ecdsa.PrivateKey (for ES256) or a []byte (for HMAC verification only).
func NewValidator(secret any, audience string) *Validator {
	var publicKey any
	switch key := secret.(type) {
	case *ecdsa.PrivateKey:
		publicKey = key.Public()
	default:
		publicKey = key
	}

	return &Validator{
		publicKey: publicKey,
		audience:  audience,
	}
}

// ValidationError represents a token validation failure.
type ValidationError struct {
	Err error
}

func (e *ValidationError) Error() string {
	return e.Err.Error()
}

func (e *ValidationError) Unwrap() error {
	return e.Err
}

// ErrTokenRevoked is returned when a token's jti is on the blacklist.
var ErrTokenRevoked = errors.New("jwt: token revoked")

// ErrInvalidAudience is returned when the JWT audience claim is invalid.
var ErrInvalidAudience = errors.New("jwt: invalid audience")

// ErrInvalidClaims is returned when the JWT claims are invalid.
var ErrInvalidClaims = errors.New("jwt: invalid claims")

// JTIBlacklist is an in-memory TTL cache for revoked JWT IDs.
type JTIBlacklist struct {
	entries map[string]time.Time
	mu      sync.RWMutex
}

// NewJTIBlacklist creates a new JTI blacklist.
func NewJTIBlacklist() *JTIBlacklist {
	return &JTIBlacklist{entries: make(map[string]time.Time)}
}

// Revoke adds a jti to the blacklist until the given TTL expires.
func (b *JTIBlacklist) Revoke(jti string, ttl time.Duration) {
	if jti == "" {
		return
	}
	b.mu.Lock()
	b.entries[jti] = time.Now().Add(ttl)
	b.mu.Unlock()
}

// IsRevoked returns true if the jti is currently on the blacklist.
func (b *JTIBlacklist) IsRevoked(jti string) bool {
	if jti == "" {
		return false
	}
	b.mu.Lock()
	exp, ok := b.entries[jti]
	if !ok {
		b.mu.Unlock()
		return false
	}
	if time.Now().After(exp) {
		delete(b.entries, jti)
		b.mu.Unlock()
		return false
	}
	b.mu.Unlock()
	return true
}

// Size returns the approximate number of entries in the blacklist.
func (b *JTIBlacklist) Size() int {
	b.mu.RLock()
	n := len(b.entries)
	b.mu.RUnlock()
	return n
}

// JTIValidator composes a Validator with an optional blacklist.
type JTIValidator struct {
	*Validator
	blacklist *JTIBlacklist
}

// NewJTIValidator wraps a Validator with a blacklist.
func NewJTIValidator(v *Validator) *JTIValidator {
	return &JTIValidator{Validator: v, blacklist: NewJTIBlacklist()}
}

// Revoke adds a jti to the revocation blacklist.
func (v *JTIValidator) Revoke(jti string, ttl time.Duration) {
	v.blacklist.Revoke(jti, ttl)
}

// IsRevoked checks if a jti is currently revoked.
func (v *JTIValidator) IsRevoked(jti string) bool {
	return v.blacklist.IsRevoked(jti)
}

// Validate parses and validates a JWT token string.
// Returns claims if valid, or a ValidationError.
func (v *Validator) Validate(tokenString string) (*Claims, error) {
	tokenString = strings.TrimSpace(tokenString)
	if tokenString == "" {
		return nil, &ValidationError{Err: errors.New("jwt: empty token")}
	}

	tokenString = strings.TrimPrefix(tokenString, "Bearer ")

	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (any, error) {
		// Only ES256 is accepted per security design.
		if token.Method.Alg() != "ES256" {
			return nil, fmt.Errorf("jwt: rejected signing method: %v (only ES256 is allowed)", token.Header["alg"])
		}
		return v.publicKey, nil
	})

	if err != nil {
		return nil, &ValidationError{Err: err}
	}

	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, &ValidationError{Err: ErrInvalidClaims}
	}

	// Validate audience
	if !v.validateAudience(claims.Audience) {
		return nil, &ValidationError{Err: ErrInvalidAudience}
	}

	return claims, nil
}

func (v *Validator) validateAudience(audience jwt.ClaimStrings) bool {
	if v.audience == "" {
		return true // Skip validation if no audience configured
	}
	return slices.Contains(audience, v.audience)
}

// CompareKeys performs a constant-time comparison of two strings.
// This prevents timing attacks on API key comparisons.
func CompareKeys(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// GenerateJTI generates a new JWT ID (jti) using crypto/rand.
func NewJTI() (string, error) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return "", fmt.Errorf("jwt: crypto/rand read: %w", err)
	}
	return uuid(b), nil
}

// uuid formats a byte slice as a UUID string.
func uuid(b []byte) string {
	const hexChars = "0123456789abcdef"
	result := make([]byte, 36)
	for i, j := 0, 0; i < 16; i++ {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			result[i+j] = '-'
			j++
		}
		result[i+j] = hexChars[b[i]>>4]
		result[i+j+1] = hexChars[b[i]&0x0f]
		if j == 1 {
			// After first hyphen, adjust i to account for extra position
			i--
		}
	}
	return string(result)
}

// GenerateECDSAKey generates a new ECDSA P-256 key pair.
// This is useful for testing or for generating new gateway keys.
func GenerateECDSAKey() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}

// VerifySignature verifies an ECDSA signature.
func VerifySignature(pub *ecdsa.PublicKey, hash, signature []byte) bool {
	return ecdsa.VerifyASN1(pub, hash, signature)
}

// Sign signs data with an ECDSA key using ES256.
func Sign(priv *ecdsa.PrivateKey, hash []byte) ([]byte, error) {
	return ecdsa.SignASN1(rand.Reader, priv, hash)
}

// PublicKey returns the public key corresponding to priv.
func PublicKey(priv *ecdsa.PrivateKey) *ecdsa.PublicKey {
	return &priv.PublicKey
}

// BigIntToBytes converts a big.Int to a fixed-size byte slice.
func BigIntToBytes(n *big.Int, size int) []byte {
	b := n.Bytes()
	if len(b) < size {
		result := make([]byte, size)
		copy(result[size-len(b):], b)
		return result
	}
	return b
}
