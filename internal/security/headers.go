package security

import "net/http"

// SecurityHeaders returns an HTTP middleware that injects defense-in-depth
// response headers on every request: X-Content-Type-Options (MIME sniffing),
// X-Frame-Options (clickjacking), Referrer-Policy (referrer leakage), and
// Content-Security-Policy (XSS / data-exfiltration).
//
// The effective CSP is ResolveCSP(defaultCSP, override): an empty or
// whitespace-only override falls back to defaultCSP, so a stray space in
// YAML or env cannot ship a malformed Content-Security-Policy header that
// browsers reject silently.
//
// Call sites:
//
//	webchat:  security.SecurityHeaders(security.DefaultWebChatCSP, csp, h)
//	docs:     security.SecurityHeaders(security.DefaultDocsCSP,   csp, h)
//
// Header additions (HSTS, Permissions-Policy, COOP, COEP, …) belong here —
// adding them in one place propagates to every consumer.
func SecurityHeaders(defaultCSP, override string, next http.Handler) http.Handler {
	csp := ResolveCSP(defaultCSP, override)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", csp)
		next.ServeHTTP(w, r)
	})
}
