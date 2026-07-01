package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatBannerURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		scheme string
		addr   string
		path   string
		want   string
	}{
		{name: "empty addr", scheme: "http", addr: "", path: "", want: ""},
		{name: "0.0.0.0 with port", scheme: "http", addr: "0.0.0.0:8888", path: "", want: "http://127.0.0.1:8888"},
		{name: "0.0.0.0 without port", scheme: "http", addr: "0.0.0.0", path: "", want: "http://127.0.0.1"},
		{name: "colon-only port", scheme: "http", addr: ":8888", path: "", want: "http://127.0.0.1:8888"},
		{name: "localhost with port", scheme: "ws", addr: "localhost:8888", path: "/ws", want: "ws://localhost:8888/ws"},
		{name: "explicit host with path", scheme: "http", addr: "192.168.1.1:9999", path: "/admin", want: "http://192.168.1.1:9999/admin"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := formatBannerURL(false, tc.scheme, tc.addr, tc.path)
			require.Equal(t, tc.want, got)
		})
	}
}
