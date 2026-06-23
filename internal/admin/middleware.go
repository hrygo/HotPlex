package admin

import (
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"

	"github.com/hrygo/hotplex/internal/web"
)

type scopeContextKey struct{}

func getScopes(r *http.Request) []string {
	if scopes, ok := r.Context().Value(scopeContextKey{}).([]string); ok {
		return scopes
	}
	return nil
}

// scopeImplies maps a scope to the lower-privilege scopes it grants.
var scopeImplies = map[string][]string{
	ScopeAdminWrite: {ScopeAdminRead},
	ScopeAdminRead:  {ScopeConfigRead},
}

func hasScope(r *http.Request, required string) bool {
	granted := getScopes(r)
	if slices.Contains(granted, required) {
		return true
	}
	// Check implied scopes with transitive closure:
	// admin:write → admin:read → config:read
	visited := map[string]bool{}
	queue := make([]string, len(granted))
	copy(queue, granted)
	for len(queue) > 0 {
		s := queue[0]
		queue = queue[1:]
		if visited[s] {
			continue
		}
		visited[s] = true
		if implied, ok := scopeImplies[s]; ok {
			if slices.Contains(implied, required) {
				return true
			}
			queue = append(queue, implied...)
		}
	}
	return false
}

// requireScope checks that the request has the required scope.
// Returns false and writes 403 if not.
func requireScope(w http.ResponseWriter, r *http.Request, scope string) bool {
	if !hasScope(r, scope) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "insufficient scope: need "+scope)
		return false
	}
	return true
}

// clientIP extracts the client IP from the request.
// It uses r.RemoteAddr directly to prevent IP spoofing via X-Forwarded-For.
// Deploy behind a reverse proxy that strips untrusted XFF headers.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
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
