package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParsePagination(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		query      string
		wantLimit  int
		wantOffset int
	}{
		{"defaults", "", defaultLimit, 0},
		{"explicit limit", "limit=50", 50, 0},
		{"limit over max clamped", "limit=5000", maxLimit, 0},
		{"limit at max", "limit=1000", maxLimit, 0},
		{"limit zero falls back", "limit=0", defaultLimit, 0},
		{"limit negative falls back", "limit=-5", defaultLimit, 0},
		{"limit non-integer falls back", "limit=abc", defaultLimit, 0},
		{"offset explicit", "offset=200", defaultLimit, 200},
		{"offset negative falls back", "offset=-1", defaultLimit, 0},
		{"limit and offset together", "limit=25&offset=10", 25, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/?"+tt.query, nil)
			limit, offset := parsePagination(req)
			require.Equal(t, tt.wantLimit, limit)
			require.Equal(t, tt.wantOffset, offset)
		})
	}
}
