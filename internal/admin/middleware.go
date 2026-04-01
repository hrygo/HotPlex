package admin

import (
	"net/http"
	"net/netip"
	"slices"
	"strings"
)

// scopeContextKey is the context key for authenticated scopes.
type scopeContextKey struct{}

// getScopes returns the scopes stored in the request context by Middleware.
func getScopes(r *http.Request) []string {
	if scopes, ok := r.Context().Value(scopeContextKey{}).([]string); ok {
		return scopes
	}
	return nil
}

// hasScope reports whether the request has the required scope.
func hasScope(r *http.Request, required string) bool {
	return slices.Contains(getScopes(r), required)
}

// clientIP extracts the real client IP from the request, respecting X-Forwarded-For.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx != -1 {
			xff = xff[:idx]
		}
		return strings.TrimSpace(xff)
	}
	host, _, _ := strings.Cut(r.RemoteAddr, ":")
	return host
}

// extractBearerToken extracts the bearer token from Authorization header or query string.
func extractBearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return h[7:]
	}
	return r.URL.Query().Get("access_token")
}

// ipAllowed reports whether addr matches any allowed CIDR.
func ipAllowed(addr string, cidrs []string) bool {
	if len(cidrs) == 0 {
		return true
	}
	ip, err := netip.ParseAddr(addr)
	if err != nil {
		return false
	}
	for _, cidr := range cidrs {
		p, err := netip.ParsePrefix(cidr)
		if err != nil {
			continue
		}
		if p.Contains(ip) {
			return true
		}
	}
	return false
}
