package docs

import (
	"io/fs"
	"net/http"

	"github.com/hrygo/hotplex/internal/security"
)

var docsFS, _ = fs.Sub(StaticFS, "out")

var fileServer = http.FileServerFS(docsFS)

// Handler returns an http.Handler that serves the static documentation.
// Pass an empty string for csp to use DefaultDocsCSP; pass a custom directive
// to override (typically via cfg.Security.CSP). Whitespace-only csp is
// treated as empty.
func Handler(csp string) http.Handler {
	return security.SecurityHeaders(security.DefaultDocsCSP, csp, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// fileServer automatically handles index.html for directories.
		fileServer.ServeHTTP(w, r)
	}))
}
