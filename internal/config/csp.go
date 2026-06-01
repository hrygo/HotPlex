package config

import "strings"

// DefaultWebChatCSP is the fallback Content-Security-Policy applied to the
// embedded webchat SPA when the operator has not configured SecurityConfig.CSP.
//
// It is intentionally permissive: connect-src uses the CSP scheme keywords
// (http: / https: / ws: / wss:), which the browser interprets as "any host
// over the matching scheme". This lets the SPA reach backends on remote IPs
// (e.g. http://10.102.78.2:9999) without configuration — the explicit
// trade-off the project chose between "out-of-the-box usability" and
// "strict-but-needs-config". Production deployments should override this
// with security.csp / HOTPLEX_SECURITY_CSP pinning the actual host(s).
const DefaultWebChatCSP = "default-src 'self'; " +
	"script-src 'self' 'unsafe-inline' 'unsafe-eval'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"connect-src 'self' http: https: ws: wss: data: blob:; " +
	"img-src 'self' data: blob:; " +
	"font-src 'self' data:"

// DefaultDocsCSP is the fallback CSP for the self-hosted docs portal. It is
// the same scheme-wide-open connect-src as the webchat default, plus the
// original relaxations for jsDelivr (scripts) and Google Fonts (styles +
// fonts) used by the docs theme.
const DefaultDocsCSP = "default-src 'self'; " +
	"script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; " +
	"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
	"connect-src 'self' http: https: ws: wss: data: blob:; " +
	"img-src 'self' data: blob:; " +
	"font-src 'self' data: https://fonts.gstatic.com"

// IsPermissiveCSP returns true when the directive is so broad that it grants
// any-host access. Triggers:
//   - the bare wildcard "*" appearing as a source
//   - any network-scheme-only keyword source ("http:", "https:", "ws:",
//     "wss:") — these match every host over that scheme per CSP
//     source-list semantics
//   - any host-pattern ending in "://*" (e.g. "wss://*", "https://*") —
//     the more explicit form of "all hosts over this scheme"
//
// Note: "data:" / "blob:" / "filesystem:" are intentionally NOT flagged.
// They are non-network schemes used to allow inline resources (base64
// images, blob URLs from the SPA itself) and are part of the safe baseline.
//
// The intent is to surface "this CSP doesn't actually restrict anything" to
// the operator via a startup log warning, not to make a precise security
// judgment. False positives are acceptable; missing a wide-open config is not.
func IsPermissiveCSP(directive string) bool {
	if directive == "" {
		return true
	}
	for _, tok := range tokenizeCSP(directive) {
		switch tok {
		case "*",
			"http:", "https:", "ws:", "wss:":
			return true
		}
		if strings.HasSuffix(tok, "://*") {
			return true
		}
	}
	return false
}

// tokenizeCSP returns whitespace- and semicolon-separated tokens. It collapses
// runs of delimiters so empty strings are never emitted.
func tokenizeCSP(directive string) []string {
	out := make([]string, 0, 16)
	start := -1
	for i, r := range directive {
		switch r {
		case ' ', '\t', '\n', '\r', ';':
			if start >= 0 {
				out = append(out, directive[start:i])
				start = -1
			}
		default:
			if start < 0 {
				start = i
			}
		}
	}
	if start >= 0 {
		out = append(out, directive[start:])
	}
	return out
}
