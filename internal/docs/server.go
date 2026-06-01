package docs

import (
	"io/fs"
	"net/http"

	"github.com/hrygo/hotplex/internal/config"
)

var docsFS, _ = fs.Sub(StaticFS, "out")

var fileServer = http.FileServerFS(docsFS)

// securityHeaders injects security response headers for all docs responses.
// If cspOverride is non-empty it replaces config.DefaultDocsCSP — same
// contract as internal/webchat so the two services stay in lockstep under
// one config. The default keeps the docs theme's jsDelivr + Google Fonts
// relaxations while still letting connect-src reach any host.
func securityHeaders(cspOverride string, next http.Handler) http.Handler {
	csp := config.DefaultDocsCSP
	if cspOverride != "" {
		csp = cspOverride
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", csp)
		next.ServeHTTP(w, r)
	})
}

// Handler returns an http.Handler that serves the static documentation.
// Pass an empty string for csp to use DefaultCSP; pass a custom directive to
// override (typically via cfg.Security.CSP).
func Handler(csp string) http.Handler {
	return securityHeaders(csp, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// fileServer automatically handles index.html for directories.
		fileServer.ServeHTTP(w, r)
	}))
}
