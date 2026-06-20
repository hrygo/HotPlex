package admin

import (
	"net/http"
	"strconv"
)

const (
	defaultLimit = 100
	maxLimit     = 1000
)

// parsePagination extracts the limit/offset query params with safe defaults and bounds.
// limit defaults to 100 and is clamped to [1, maxLimit]; offset defaults to 0 and is
// clamped to >=0. Non-integer or out-of-range values fall back to the defaults.
//
// Centralizing this here ensures every admin list endpoint enforces the same upper
// bound (previously logs capped at 1000 while sessions had no cap at all).
func parsePagination(r *http.Request) (limit, offset int) {
	limit = defaultLimit
	offset = 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}
	return limit, offset
}
