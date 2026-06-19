package security

import "net/http"

// ResolveOrigin determines the Access-Control-Allow-Origin header value
// based on the request's Origin and the configured allowed origins list.
//
//   - If the list contains "*", returns "*" (backward-compatible wildcard mode).
//   - If the request Origin exactly matches an entry, returns that Origin
//     (echo-back mode for production with specific domains).
//   - Otherwise returns "" (no CORS headers → browser blocks the request).
func ResolveOrigin(requestOrigin string, allowedOrigins []string) string {
	for _, o := range allowedOrigins {
		if o == "*" {
			return "*"
		}
		if o == requestOrigin {
			return requestOrigin
		}
	}
	return ""
}

// CORSMiddleware returns an HTTP middleware that injects CORS response headers
// and handles OPTIONS preflight requests. The originsFn function is called on
// every request so that config hot-reload takes effect immediately.
//
// When the resolved origin is non-empty, the following headers are set:
//   - Access-Control-Allow-Origin: the matched origin (or "*" for wildcard)
//   - Access-Control-Allow-Methods: GET, POST, PUT, PATCH, DELETE, OPTIONS
//   - Access-Control-Allow-Headers: Content-Type, Authorization, X-Api-Key
//   - Vary: Origin (prevents cache poisoning when origin-specific; omitted for wildcard)
//
// OPTIONS requests receive a 200 response without calling the next handler.
func CORSMiddleware(originsFn func() []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			allowed := ResolveOrigin(origin, originsFn())
			if allowed != "" {
				writeCORSHeaders(w, allowed)
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SetCORSHeaders sets CORS response headers without handling OPTIONS.
// Use this in middleware chains that have their own OPTIONS handling
// (e.g. the admin API middleware).
//
// When allowedOrigins contains "*" or the request Origin matches an entry,
// the CORS headers are written. Otherwise no headers are set.
func SetCORSHeaders(w http.ResponseWriter, allowedOrigins []string, requestOrigin string) {
	allowed := ResolveOrigin(requestOrigin, allowedOrigins)
	if allowed != "" {
		writeCORSHeaders(w, allowed)
	}
}

// writeCORSHeaders writes the standard CORS response headers for the given
// resolved origin value.
//
// Vary: Origin is only set in echo-back mode (origin != "*") to avoid
// degrading CDN cache efficiency in wildcard mode where the response is
// identical for all origins.
func writeCORSHeaders(w http.ResponseWriter, origin string) {
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Api-Key")
	if origin != "*" {
		// Echo-back (pinned origin): emit Allow-Credentials so cookie auth works
		// cross-origin. Wildcard "*" cannot use credentials per the CORS spec.
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Vary", "Origin")
	}
}
