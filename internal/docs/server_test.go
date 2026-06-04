package docs

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGzipMiddleware_CompressesHTML(t *testing.T) {
	t.Parallel()

	handler := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body>Hello Docs</body></html>"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/docs/index.html", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))

	gr, err := gzip.NewReader(rec.Body)
	require.NoError(t, err)
	body, err := io.ReadAll(gr)
	require.NoError(t, err)
	require.Contains(t, string(body), "Hello Docs")
}

func TestGzipMiddleware_SkipsWhenNoAcceptEncoding(t *testing.T) {
	t.Parallel()

	handler := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("plain"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/docs/index.html", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, rec.Header().Get("Content-Encoding"))
	require.Equal(t, "plain", rec.Body.String())
}

func TestGzipMiddleware_SkipsPrecompressedFormats(t *testing.T) {
	t.Parallel()

	for _, ext := range []string{".png", ".jpg", ".jpeg", ".webp", ".woff2", ".woff"} {
		t.Run(ext, func(t *testing.T) {
			t.Parallel()

			handler := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("binary"))
			}))

			req := httptest.NewRequest(http.MethodGet, "/docs/assets/logo"+ext, nil)
			req.Header.Set("Accept-Encoding", "gzip")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			require.Equal(t, http.StatusOK, rec.Code)
			require.Empty(t, rec.Header().Get("Content-Encoding"))
		})
	}
}

func TestCacheHeaders_StaticAssets(t *testing.T) {
	t.Parallel()

	cases := []struct {
		path     string
		expected string
	}{
		{"/docs/assets/logo.png", "public, max-age=31536000, immutable"},
		{"/docs/assets/mermaid.min.js", "public, max-age=31536000, immutable"},
		{"/docs/assets/fonts/fonts.css", "public, max-age=31536000, immutable"},
		{"/docs/assets/logo.webp", "public, max-age=31536000, immutable"},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			setCacheHeaders(rec, tc.path)
			require.Equal(t, tc.expected, rec.Header().Get("Cache-Control"))
		})
	}
}

func TestCacheHeaders_HTMLPages(t *testing.T) {
	t.Parallel()

	cases := []struct {
		path     string
		expected string
	}{
		{"/docs/index.html", "public, max-age=3600"},
		{"/docs/getting-started.html", "public, max-age=3600"},
		{"/docs/guides/user/chat-with-ai.html", "public, max-age=3600"},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			setCacheHeaders(rec, tc.path)
			require.Equal(t, tc.expected, rec.Header().Get("Cache-Control"))
		})
	}
}

func TestCacheHeaders_DirectoryIndex(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	setCacheHeaders(rec, "/docs/")
	require.Equal(t, "public, max-age=3600", rec.Header().Get("Cache-Control"))
}

func TestGzipMiddleware_SetsVaryOnCompressedResponses(t *testing.T) {
	t.Parallel()

	handler := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("some content"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/docs/index.html", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, "Accept-Encoding", rec.Header().Get("Vary"))
}

func TestGzipMiddleware_NoVaryOnSkippedResponses(t *testing.T) {
	t.Parallel()

	handler := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("binary"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/docs/assets/logo.png", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Empty(t, rec.Header().Get("Vary"), "precompressed formats should not get Vary header")
}

func TestCacheHeaderSniffer_Caches200Responses(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	sniffer := &cacheHeaderSniffer{ResponseWriter: rec, path: "/docs/index.html"}

	inner.ServeHTTP(sniffer, httptest.NewRequest(http.MethodGet, "/docs/index.html", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "public, max-age=3600", rec.Header().Get("Cache-Control"))
}

func TestCacheHeaderSniffer_Skips404Responses(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	rec := httptest.NewRecorder()
	sniffer := &cacheHeaderSniffer{ResponseWriter: rec, path: "/docs/nonexistent.html"}

	inner.ServeHTTP(sniffer, httptest.NewRequest(http.MethodGet, "/docs/nonexistent.html", nil))

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Empty(t, rec.Header().Get("Cache-Control"), "404 should not be cached")
}

func TestCacheHeaderSniffer_Implicit200(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("data")) // implicit 200
	})
	rec := httptest.NewRecorder()
	sniffer := &cacheHeaderSniffer{ResponseWriter: rec, path: "/docs/assets/logo.png"}

	inner.ServeHTTP(sniffer, httptest.NewRequest(http.MethodGet, "/docs/assets/logo.png", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "public, max-age=31536000, immutable", rec.Header().Get("Cache-Control"))
}

func TestGzipResponseWriter_WriteHeaderPassthrough(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	gw := gzip.NewWriter(rec)
	w := &gzipResponseWriter{gw: gw, ResponseWriter: rec}

	w.WriteHeader(http.StatusNotFound)
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.NoError(t, gw.Close())
}

func TestGzipResponseWriter_Write(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	gw := gzip.NewWriter(rec)
	w := &gzipResponseWriter{gw: gw, ResponseWriter: rec}

	_, err := w.Write([]byte("compressed data"))
	require.NoError(t, err)
	require.NoError(t, gw.Close())

	gr, err := gzip.NewReader(rec.Body)
	require.NoError(t, err)
	body, err := io.ReadAll(gr)
	require.NoError(t, err)
	require.Equal(t, "compressed data", string(body))
}

func TestGzipResponseWriter_Unwrap(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	w := &gzipResponseWriter{gw: nil, ResponseWriter: rec}

	require.Equal(t, rec, w.Unwrap())
}

func TestCacheHeaderSniffer_Unwrap(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	s := &cacheHeaderSniffer{ResponseWriter: rec, path: "/"}

	require.Equal(t, rec, s.Unwrap())
}
