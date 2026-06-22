// Package web provides shared HTTP helpers for request parsing used across the
// admin API (port 9999) and WebChat multitenancy endpoints (port 8888).
package web

import (
	"net/http"
	"strconv"
)

const (
	// DefaultLimit is the page size used when limit is absent or invalid.
	DefaultLimit = 100
	// MaxLimit is the upper bound a client may request; larger values are clamped.
	MaxLimit = 1000
)

// ParsePagination extracts the limit/offset query params with safe defaults and bounds.
// limit defaults to 100 and is clamped to [1, MaxLimit]; offset defaults to 0 and is
// clamped to >=0. Non-integer or out-of-range values fall back to the defaults.
//
// Shared by admin and workspace list endpoints so both enforce the same upper bound
// and identical clamp semantics: a too-large limit is clamped down to MaxLimit rather
// than silently dropping to the default (the previous divergence between the admin and
// gateway ports — see PR #764 review).
func ParsePagination(r *http.Request) (limit, offset int) {
	limit = DefaultLimit
	offset = 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}
	return limit, offset
}
