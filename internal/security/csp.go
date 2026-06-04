package security

import "strings"

// DefaultWebChatCSP is the fallback Content-Security-Policy applied to the
// embedded webchat SPA when the operator has not configured SecurityConfig.CSP.
//
// It is intentionally permissive: connect-src uses the CSP scheme keywords
// (http: / https: / ws: / wss:), which the browser interprets as "any host
// over the matching scheme". This lets the SPA reach backends on remote IPs
// (e.g. http://192.168.1.100:9999) without configuration — the explicit
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
// the same scheme-wide-open connect-src as the webchat default, plus jsDelivr
// for scripts and fonts (Scalar API Reference loads web fonts from CDN).
// Google Fonts have been localized (served from 'self') so no additional
// font/style allow-lists are needed beyond jsDelivr.
const DefaultDocsCSP = "default-src 'self'; " +
	"script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; " +
	"style-src 'self' 'unsafe-inline'; " +
	"connect-src 'self' http: https: ws: wss: data: blob:; " +
	"img-src 'self' data: blob:; " +
	"font-src 'self' data: https://cdn.jsdelivr.net"

// ResolveCSP returns the effective CSP for a service. If override is empty
// or contains only whitespace, defaultCSP is returned. Otherwise the trimmed
// override is used verbatim. Centralising the resolution prevents a stray
// space in YAML or env from shipping a malformed Content-Security-Policy
// header that browsers reject (a silent breakage where the SPA stops
// loading with no operator-visible warning).
func ResolveCSP(defaultCSP, override string) string {
	if s := strings.TrimSpace(override); s != "" {
		return s
	}
	return defaultCSP
}

// IsPermissiveCSP reports whether directive grants any-host access via:
//   - the bare wildcard "*" appearing as a source
//   - any network-scheme-only keyword source ("http:", "https:", "ws:",
//     "wss:") — these match every host over that scheme per CSP
//     source-list semantics
//   - any host-pattern ending in "://*" (e.g. "wss://*", "https://*")
//
// "data:" / "blob:" / "filesystem:" are intentionally NOT flagged — they
// are non-network schemes used for inline resources (base64 images, blob
// URLs from the SPA itself) and are part of the safe baseline.
//
// An empty directive is reported as not permissive: an empty policy is
// the browser's default-deny posture, which is strictly stricter than
// `*`. The "is the operator's value permissive?" check is a separate
// concern from "is anything configured at all" — callers should branch
// on override == "" themselves if they want the latter.
func IsPermissiveCSP(directive string) bool {
	if directive == "" {
		return false
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

// tokenizeCSP returns whitespace- and semicolon-separated tokens. It
// collapses runs of delimiters so empty strings are never emitted.
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
