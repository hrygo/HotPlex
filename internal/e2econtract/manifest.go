// Package e2econtract defines the fixed platform-worker combination matrix
// and capability profile used by the platform-worker E2E alignment tests.
//
// It is a leaf test-contract package: it depends only on stdlib and
// internal/worker (for the worker.WorkerType constants). It must NOT import
// internal/gateway or internal/messaging.
package e2econtract

import "github.com/hrygo/hotplex/internal/worker"

// CapabilityMode describes how a platform-worker combination realizes a
// given capability. The literal values are part of the E2E contract and are
// asserted verbatim by manifest_test.go.
type CapabilityMode string

// Platform is a first-class messaging or chat platform in the contract matrix.
type Platform string

const (
	PlatformFeishu  Platform = "feishu"
	PlatformSlack   Platform = "slack"
	PlatformWebChat Platform = "webchat"

	Native          CapabilityMode = "native"
	GatewayFallback CapabilityMode = "gateway_fallback"
	Unsupported     CapabilityMode = "unsupported"
	NotApplicable   CapabilityMode = "not_applicable"
)

// WorkerProfile is the fixed capability profile of a single worker type,
// independent of platform.
type WorkerProfile struct {
	Type                                           worker.WorkerType
	Stop, Reset, Resume, Interaction, MidTurnInput CapabilityMode
}

// Combination identifies one platform-worker pair in the fixed E2E matrix.
type Combination struct {
	ID       string
	Platform Platform
	Worker   worker.WorkerType
}

// WorkerProfiles returns the fixed capability profile for every worker type
// in the contract matrix. Stop/reset/resume/interaction are all Native for
// the four matrix workers; mid-turn input is Native where the worker owns the
// channel (Claude/Codex) and GatewayFallback where the gateway holds the
// session data (OpenCode Server/ACP).
func WorkerProfiles() []WorkerProfile {
	return []WorkerProfile{
		{Type: worker.TypeClaudeCode, Stop: Native, Reset: Native, Resume: Native, Interaction: Native, MidTurnInput: Native},
		{Type: worker.TypeOpenCodeSrv, Stop: Native, Reset: Native, Resume: Native, Interaction: Native, MidTurnInput: GatewayFallback},
		{Type: worker.TypeCodexCLI, Stop: Native, Reset: Native, Resume: Native, Interaction: Native, MidTurnInput: Native},
		{Type: worker.TypeACP, Stop: Native, Reset: Native, Resume: Native, Interaction: Native, MidTurnInput: GatewayFallback},
	}
}

// Combinations returns the fixed 12-row platform-worker matrix in canonical
// order (platforms feishu, slack, webchat; workers claude_code, opencode_server,
// codex_cli, acp). The exact rows are asserted verbatim by TestCombinations_ExactMatrix.
func Combinations() []Combination {
	return []Combination{
		{ID: "F-C", Platform: PlatformFeishu, Worker: worker.TypeClaudeCode},
		{ID: "F-O", Platform: PlatformFeishu, Worker: worker.TypeOpenCodeSrv},
		{ID: "F-X", Platform: PlatformFeishu, Worker: worker.TypeCodexCLI},
		{ID: "F-A", Platform: PlatformFeishu, Worker: worker.TypeACP},
		{ID: "S-C", Platform: PlatformSlack, Worker: worker.TypeClaudeCode},
		{ID: "S-O", Platform: PlatformSlack, Worker: worker.TypeOpenCodeSrv},
		{ID: "S-X", Platform: PlatformSlack, Worker: worker.TypeCodexCLI},
		{ID: "S-A", Platform: PlatformSlack, Worker: worker.TypeACP},
		{ID: "W-C", Platform: PlatformWebChat, Worker: worker.TypeClaudeCode},
		{ID: "W-O", Platform: PlatformWebChat, Worker: worker.TypeOpenCodeSrv},
		{ID: "W-X", Platform: PlatformWebChat, Worker: worker.TypeCodexCLI},
		{ID: "W-A", Platform: PlatformWebChat, Worker: worker.TypeACP},
	}
}

// CombinationID returns the fixed two-character ID for a platform-worker pair.
// It returns the empty string when the pair is not part of the matrix.
func CombinationID(platform Platform, wt worker.WorkerType) string {
	for _, c := range Combinations() {
		if c.Platform == platform && c.Worker == wt {
			return c.ID
		}
	}
	return ""
}
