package webchat

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/hrygo/hotplex/internal/security"
)

var spaFS, _ = fs.Sub(StaticFS, "out")

var fileServer = http.FileServerFS(spaFS)

// Handler returns an http.Handler that serves the webchat SPA.
//
// Pass an empty string for csp to use DefaultWebChatCSP; pass a custom
// directive when serving from a non-localhost host (e.g. reverse-prod on
// http://192.168.1.100:9999). Whitespace-only csp is treated as empty.
//
// cookieAuth, when non-nil, enables automatic HMAC cookie issuance on the
// SPA fallback path (index.html). Static assets (/_next/*) skip cookie handling.
//
// Routing strategy:
//   - /_next/*  → static assets with aggressive cache headers (hashed filenames)
//   - exact file match (favicon, robots.txt) → serve directly
//   - everything else → fallback to index.html (client-side routing)
//
// Must be registered last on the ServeMux so explicit API/WS routes take priority.
func Handler(csp string, cookieAuth *security.CookieAuth) http.Handler {
	return security.SecurityHeaders(security.DefaultWebChatCSP, csp, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Static assets with content-hashed filenames — cache for 1 year.
		if strings.HasPrefix(path, "/_next/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			fileServer.ServeHTTP(w, r)
			return
		}

		// Try exact file match (favicon.ico, robots.txt, etc.).
		// Next.js static export produces .html files for each route
		// alongside directories with the same name (e.g. admin.html + admin/).
		// When a path resolves to a directory, or has no match at all,
		// try the .html variant before falling back to SPA index.html.
		relPath := strings.TrimPrefix(path, "/")
		if relPath != "" {
			if f, err := spaFS.Open(relPath); err == nil {
				stat, serr := f.Stat()
				_ = f.Close()
				if serr == nil && stat.IsDir() {
					// Path matched a directory (e.g. /admin -> admin/);
					// serve the .html file instead.
					if hf, herr := spaFS.Open(relPath + ".html"); herr == nil {
						_ = hf.Close()
						r.URL.Path = path + ".html"
						fileServer.ServeHTTP(w, r)
						return
					}
				}
				fileServer.ServeHTTP(w, r)
				return
			}
			// No exact match; try .html variant (e.g. /admin/bots/detail -> admin/bots/detail.html).
			if hf, herr := spaFS.Open(relPath + ".html"); herr == nil {
				_ = hf.Close()
				r.URL.Path = path + ".html"
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// SPA fallback: serve index.html for all non-file paths.
		// Issue a cookie if cookieAuth is configured and request lacks a valid one.
		if cookieAuth != nil {
			_ = cookieAuth.SetCookie(w, r, "webchat_user")
		}
		w.Header().Set("Cache-Control", "no-cache")
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	}))
}
