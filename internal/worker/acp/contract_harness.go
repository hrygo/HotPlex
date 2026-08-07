package acp

// contract_harness.go is the ACP half of the gateway native-command contract
// matrix (issue #958 T12b). It mirrors the claudecode.stream_fake.go precedent:
// an exported, non-test file so gateway contract tests can drive the REAL
// advertised-command catalog (available_commands_update) and the REAL
// session/prompt JSON-RPC wire against caller-provided pipes without a real
// agent process.

import (
	"log/slog"

	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/base"
)

// NewContractWorker builds a Worker whose advertised commands and ACP client
// are injected for the gateway contract matrix. The client drives the real
// session/prompt request over the caller-provided pipes; availableCommands
// gates InvokeSkill exactly like the production advertisement path.
func NewContractWorker(advertised []worker.SkillDescriptor, client *ACPClient, sessionID string) *Worker {
	cmds := make(map[string]worker.SkillDescriptor, len(advertised))
	for _, d := range advertised {
		cmds[d.Name] = d
	}
	w := &Worker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
	}
	w.availableCommands = cmds
	w.client = client
	w.conn = newACPConn("contract-user", sessionID, slog.Default())
	w.mapper = NewACPMapper(sessionID, "contract-user", slog.Default())
	// No readLoop in the contract harness: the drain handshake is a no-op.
	w.drainCh = make(chan struct{}, 1)
	w.drainDoneCh = make(chan struct{})
	close(w.drainDoneCh)
	w.SetWorkerSessionID(sessionID)
	return w
}
