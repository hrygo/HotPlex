package docs

import (
	"compress/gzip"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/hrygo/hotplex/internal/security"
)

var docsFS, _ = fs.Sub(StaticFS, "out")

var fileServer = http.FileServerFS(docsFS)

// Handler returns an http.Handler that serves the static documentation.
// Pass an empty string for csp to use DefaultDocsCSP; pass a custom directive
// to override (typically via cfg.Security.CSP). Whitespace-only csp is
// treated as empty.
func Handler(csp string) http.Handler {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setCacheHeaders(w, r.URL.Path)
		fileServer.ServeHTTP(w, r)
	})
	return security.SecurityHeaders(
		security.DefaultDocsCSP, csp,
		cacheControlMiddleware(
			gzipMiddleware(h),
		),
	)
}

// --- Cache-Control ---

// setCacheHeaders sets Cache-Control based on file extension:
//   - Static assets (.js, .css, .png, .webp, .jpg, .svg, .woff2): 1 year, immutable
//   - HTML pages (.html, directories): 1 hour
func setCacheHeaders(w http.ResponseWriter, path string) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".js", ".css", ".png", ".webp", ".jpg", ".jpeg", ".gif", ".svg",
		".woff", ".woff2", ".ttf", ".otf", ".eot":
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	case ".html":
		w.Header().Set("Cache-Control", "public, max-age=3600")
	default:
		// Directory requests (no ext) serve index.html — short cache.
		if ext == "" && strings.HasSuffix(path, "/") {
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}
	}
}

// cacheControlMiddleware sets Vary: Accept-Encoding so caches (CDN, browser)
// store separate entries for compressed and uncompressed variants.
func cacheControlMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Accept-Encoding")
		next.ServeHTTP(w, r)
	})
}

// --- Gzip Compression ---

// precompressedExts lists file extensions for responses that should NOT be
// gzip-compressed (already compressed at the source).
var precompressedExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".webp": true, ".woff": true, ".woff2": true, ".ttf": true,
	".otf": true, ".eot": true, ".svgz": true, ".pdf": true,
}

// gzipMiddleware compresses responses with gzip when the client accepts it.
// It skips pre-compressed formats and small responses.
func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		ext := strings.ToLower(filepath.Ext(r.URL.Path))
		if precompressedExts[ext] {
			next.ServeHTTP(w, r)
			return
		}

		gz, err := gzip.NewWriterLevel(w, gzip.BestSpeed)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		defer func() { _ = gz.Close() }()

		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Del("Content-Length")
		next.ServeHTTP(&gzipResponseWriter{gw: gz, ResponseWriter: w}, r)
	})
}

// gzipResponseWriter wraps http.ResponseWriter to write gzip-compressed data.
type gzipResponseWriter struct {
	gw *gzip.Writer
	http.ResponseWriter
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	return w.gw.Write(b)
}

func (w *gzipResponseWriter) Flush() {
	_ = w.gw.Flush()
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap returns the underlying ResponseWriter for http.ResponseController.
func (w *gzipResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
