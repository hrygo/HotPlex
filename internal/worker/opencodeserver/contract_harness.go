package opencodeserver

// contract_harness.go is the OpenCode Server half of the gateway native-command
// contract matrix (issue #958 T12b). It mirrors the claudecode.stream_fake.go
// precedent: an exported, non-test file so gateway contract tests can drive the
// REAL adapter's command catalog (GET /command) and skill invocation
// (POST /session/{id}/command) against an in-process httptest server without a
// real `opencode serve` process.

import (
	"log/slog"
	"net/http"

	"github.com/hrygo/hotplex/internal/worker/base"
)

// NewContractWorker builds a Worker bound to the given fake OpenCode HTTP
// server. The commander is wired with the fixed contract session id, so the
// catalog query and skill invocation hit the caller-provided httptest handler.
func NewContractWorker(client *http.Client, baseURL string) *Worker {
	w := &Worker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
		client:     client,
		httpAddr:   baseURL,
	}
	w.cmd = &ServerCommander{
		client:    client,
		baseURL:   baseURL,
		sessionID: "sess-test-123",
	}
	return w
}
