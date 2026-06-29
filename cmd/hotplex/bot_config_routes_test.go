package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPlatformConfigRoute_Isolation guards the route design from issue #796:
// the literal "platform" segment coexists with the {name} wildcard without a
// ServeMux registration panic, and channel-default requests dispatch to the
// platform handlers rather than the bot-level handlers. Cases run sequentially
// (subtests share a dispatch marker).
func TestPlatformConfigRoute_Isolation(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()

	hit := ""
	botHandler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit = "bot" })
	platformHandler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit = "platform" })

	// Mirror the admin bot config routes from routes.go.
	mux.HandleFunc("GET /admin/bots/config", botHandler)
	mux.HandleFunc("GET /admin/bots/{name}", botHandler)
	mux.HandleFunc("GET /admin/bots/{name}/config", botHandler)
	mux.HandleFunc("GET /admin/bots/{name}/config/{file}", botHandler)
	mux.HandleFunc("GET /admin/bots/{name}/preview", botHandler)
	mux.HandleFunc("PUT /admin/bots/{name}/config/{file}", botHandler)
	// New channel-default (platform-level) routes.
	mux.HandleFunc("GET /admin/bots/platform/{platform}/config/{file}", platformHandler)
	mux.HandleFunc("PUT /admin/bots/platform/{platform}/config/{file}", platformHandler)

	cases := []struct {
		method, target, want string
	}{
		{http.MethodGet, "/admin/bots/platform/webchat/config/SOUL.md", "platform"},
		{http.MethodPut, "/admin/bots/platform/webchat/config/SOUL.md", "platform"},
		{http.MethodGet, "/admin/bots/my-bot/config/SOUL.md", "bot"},
		{http.MethodGet, "/admin/bots/my-bot/preview", "bot"},
		{http.MethodGet, "/admin/bots/my-bot", "bot"},
		{http.MethodGet, "/admin/bots/config", "bot"},
	}
	for _, tc := range cases {
		hit = ""
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(tc.method, tc.target, nil))
		require.Equal(t, http.StatusOK, w.Code, "target %s %s", tc.method, tc.target)
		require.Equal(t, tc.want, hit, "%s %s dispatched to wrong handler", tc.method, tc.target)
	}
}
