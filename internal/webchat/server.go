package webchat

import (
	"io/fs"
	"net/http"
	"strings"
)

var spaFS, _ = fs.Sub(StaticFS, "out")

var fileServer = http.FileServerFS(spaFS)

// securityHeaders injects security response headers for all SPA responses.
// These headers provide defense-in-depth against XSS, clickjacking, and content-type sniffing.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline' 'unsafe-eval'; "+
				"style-src 'self' 'unsafe-inline'; "+
				"connect-src 'self' ws://localhost:* wss://*; "+
				"img-src 'self' data: blob:; "+
				"font-src 'self' data:")
		next.ServeHTTP(w, r)
	})
}

// Handler returns an http.Handler that serves the webchat SPA.
//
// Routing strategy:
//   - /_next/*  → static assets with aggressive cache headers (hashed filenames)
//   - exact file match (favicon, robots.txt) → serve directly
//   - everything else → fallback to index.html (client-side routing)
//
// Must be registered last on the ServeMux so explicit API/WS routes take priority.
func Handler() http.Handler {
	return securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		w.Header().Set("Cache-Control", "no-cache")
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	}))
}
