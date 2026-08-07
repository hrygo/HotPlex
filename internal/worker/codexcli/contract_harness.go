package codexcli

// contract_harness.go is the Codex CLI half of the gateway native-command
// contract matrix (issue #958 T12b). It mirrors the claudecode.stream_fake.go
// precedent: an exported, non-test file so gateway contract tests can drive the
// REAL app-server JSON-RPC surface (skills/list catalog, turn/start structured
// skill items) against a caller-provided stdin writer without a real
// `codex app-server` process.

import (
	"io"
	"log/slog"
	"time"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/worker/base"
)

// NewManagerForContractTest returns a CodexAppServerManager that is armed as
// running (refs=1, state=Running) with every JSON-RPC request frame written to
// w. No process is started; callers answer skills/list and turn/start via
// DispatchFrameForTest exactly like the production readNotifications loop.
func NewManagerForContractTest(w io.Writer) *CodexAppServerManager {
	mgr := NewCodexAppServerManager(slog.Default(), config.CodexCLIConfig{
		IdleDrainPeriod: time.Minute,
		CallTimeout:     5 * time.Second,
	})
	mgr.stdin = struct {
		io.Writer
		io.Closer
	}{Writer: w, Closer: io.NopCloser(nil)}
	mgr.mu.Lock()
	mgr.refs = 1
	mgr.state = stateRunning
	mgr.mu.Unlock()
	return mgr
}

// DispatchFrameForTest routes a JSON-RPC frame into the manager exactly like
// the production readNotifications loop, correlating responses with pending
// calls by id.
func (m *CodexAppServerManager) DispatchFrameForTest(data []byte) {
	m.dispatchFrame(data)
}

// NewContractWorker builds an AppServerWorker bound to the contract-test
// manager and thread. InvokeSkill routes through the REAL turn/start structured
// skill items and the replay bookkeeping.
func NewContractWorker(mgr *CodexAppServerManager, threadID string) *AppServerWorker {
	return &AppServerWorker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
		manager:    mgr,
		threadID:   threadID,
	}
}
