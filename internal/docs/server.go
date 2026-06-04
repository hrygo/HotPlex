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
		rw := &cacheHeaderSniffer{ResponseWriter: w, path: r.URL.Path}
		fileServer.ServeHTTP(rw, r)
	})
	return security.SecurityHeaders(
		security.DefaultDocsCSP, csp,
		gzipMiddleware(h),
	)
}

// --- Cache-Control ---

// cacheHeaderSniffer delays Cache-Control header assignment until after the
// file server writes its status code. This prevents caching headers from
// being set on error responses (e.g. 404 Not Found).
type cacheHeaderSniffer struct {
	http.ResponseWriter
	path        string
	codeWritten bool
}

func (s *cacheHeaderSniffer) WriteHeader(code int) {
	if !s.codeWritten {
		s.codeWritten = true
		if code < 400 {
			setCacheHeaders(s.ResponseWriter, s.path)
		}
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *cacheHeaderSniffer) Write(b []byte) (int, error) {
	if !s.codeWritten {
		s.codeWritten = true
		// Implicit 200 — safe to cache.
		setCacheHeaders(s.ResponseWriter, s.path)
	}
	return s.ResponseWriter.Write(b)
}

// Unwrap returns the underlying ResponseWriter for http.ResponseController.
func (s *cacheHeaderSniffer) Unwrap() http.ResponseWriter {
	return s.ResponseWriter
}

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

// --- Gzip Compression ---

// precompressedExts lists file extensions for responses that should NOT be
// gzip-compressed (already compressed at the source).
var precompressedExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".webp": true, ".woff": true, ".woff2": true, ".ttf": true,
	".otf": true, ".eot": true, ".svgz": true, ".pdf": true,
}

// gzipMiddleware compresses responses with gzip when the client accepts it.
// It skips pre-compressed formats and responses with no body (1xx, 204, 304).
// Content-Encoding and Vary headers are deferred until the first Write call
// to avoid leaking gzip headers on bodyless responses (RFC 7232 §4.1).
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

		gw := &gzipResponseWriter{gw: gz, ResponseWriter: w}
		next.ServeHTTP(gw, r)

		if gw.compressed {
			_ = gz.Close()
		} else if gw.wroteHeader {
			// Bodyless response (304, 204, 1xx) — WriteHeader was stored
			// but never forwarded because Write was never called.
			w.WriteHeader(gw.code)
		}
	})
}

// gzipResponseWriter wraps http.ResponseWriter to write gzip-compressed data.
// It defers Content-Encoding/Vary header setup and gzip writer activation
// until the first Write call, so that bodyless responses (304, 204, 1xx)
// pass through without gzip artifacts.
//
// Crucially, WriteHeader is NOT forwarded to the underlying ResponseWriter
// until Write is called. This prevents Go's net/http from snapshotting the
// header map before Content-Encoding and Content-Length have been updated.
type gzipResponseWriter struct {
	gw *gzip.Writer
	http.ResponseWriter
	compressed  bool
	code        int
	wroteHeader bool
}

// shouldCompress reports whether the given status code allows a response body.
// 1xx, 204 (No Content), and 304 (Not Modified) MUST NOT include a body
// (RFC 7230 §3.3, RFC 7232 §4.1). 429 (Too Many Requests) is excluded because
// reverse proxies and CDNs may return short retry-after responses that should
// not be compressed (avoiding unnecessary CPU on rate-limited requests).
func shouldCompress(code int) bool {
	return code < 300 || (code >= 400 && code != 429)
}

func (w *gzipResponseWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.code = code
		w.wroteHeader = true
	}
	// Intentionally NOT forwarding to underlying ResponseWriter yet.
	// Go's net/http snapshots the header map on WriteHeader; we must
	// defer the forward until Write so that Content-Encoding and
	// Content-Length modifications are included in the snapshot.
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.code = 200
		w.wroteHeader = true
	}

	if !shouldCompress(w.code) {
		w.ResponseWriter.WriteHeader(w.code)
		return w.ResponseWriter.Write(b)
	}

	if !w.compressed {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")
		w.Header().Del("Content-Length")
		w.compressed = true
		w.ResponseWriter.WriteHeader(w.code)
	}
	return w.gw.Write(b)
}

func (w *gzipResponseWriter) Flush() {
	if w.compressed {
		_ = w.gw.Flush()
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap returns the underlying ResponseWriter for http.ResponseController.
func (w *gzipResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
