package security

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

// SecurityTxtHandler returns an HTTP handler for /.well-known/security.txt
// (RFC 9116). The contactFn function is called on every request so that config
// hot-reload takes effect immediately.
//
// When contactFn returns an empty string, the handler responds with 404
// (security.txt is only advertised when explicitly configured).
//
// No domains or contact information is hardcoded — all values come from
// the security.contact config field or HOTPLEX_SECURITY_SECURITY_CONTACT env var.
func SecurityTxtHandler(contactFn func() string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		contact := contactFn()
		if contact == "" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=86400")

		_, _ = io.WriteString(w, "# HotPlex Security Policy\n")
		_, _ = fmt.Fprintf(w, "Contact: %s\n", contact)
		_, _ = io.WriteString(w, "Preferred-Languages: zh, en\n")
		_, _ = fmt.Fprintf(w, "Expires: %s\n", time.Now().AddDate(1, 0, 0).Format(time.RFC3339))
	})
}
