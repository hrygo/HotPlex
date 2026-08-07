package gateway

// native_command_contract_test.go is the 3-channel × 4-worker native-command
// contract matrix (issue #958 T12b, spec §9 Slice 4 + §10). It is table-driven
// over e2econtract.Combinations() (12 rows) × contract phases, and — unlike the
// C01-C08 platform matrix — uses PER-PROTOCOL fakes: each fixture wraps a REAL
// adapter worker (claude_code stream-json pipe, opencode_server httptest,
// codex_cli app-server JSON-RPC pipes, acp advertised-command pipes) so the
// wire-shape assertions hit the adapters' actual protocol bytes. The generic
// contracttest.WorkerProbe is deliberately NOT used.
//
// Phases per combination:
//   1. Parser    — "/worker oracle-dba 10.0.0.1" + "/oracle-dba 10.0.0.1"
//                  resolve through the merged catalog; unknown → NOT_SUPPORTED.
//   2. Catalog   — the Worker authoritative catalog drives callable status;
//                  filesystem-only skills stay discoverable (§10.1/§10.5).
//   3. Wire      — the REAL per-protocol invocation wire shape (§10.3/§10.4).
//   4. Busy      — a second /worker input while the gate is held is buffered
//                  with the NativeCommandInvocation stashed, never injected
//                  mid-turn as text.
//   5. Replay    — crash replay carries InputReplay.Skill (*NativeCommandInvocation);
//                  duplicate client message id is idempotent (§10.7).
//   6. Terminal  — done converges an unknown runtime to completed.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/e2econtract"
	"github.com/hrygo/hotplex/internal/execution"
	"github.com/hrygo/hotplex/internal/skills"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/acp"
	"github.com/hrygo/hotplex/internal/worker/claudecode"
	"github.com/hrygo/hotplex/internal/worker/codexcli"
	"github.com/hrygo/hotplex/internal/worker/opencodeserver"
	"github.com/hrygo/hotplex/pkg/events"
)

// Contract fixture constants shared by every protocol.
const (
	contractFSPath     = "/workspace/.agents/skills/oracle-dba/SKILL.md"
	contractCodexPath  = "/codex/.agents/skills/oracle-dba/SKILL.md"
	contractSessionID  = "sess-test-123"
	contractWorkDir    = "/workspace"
	contractSkillName  = "oracle-dba"
	contractSkillArgs  = "10.0.0.1"
	contractTempSkill  = "temp-skill"
	contractTempPath   = "/workspace/.agents/skills/temp-skill/SKILL.md"
	contractSkillInput = "/worker " + contractSkillName + " " + contractSkillArgs
	contractSlashInput = "/" + contractSkillName + " " + contractSkillArgs
)

// contractFSSkills is the HotPlex filesystem tier injected into every fixture.
// "oracle-dba" is claimed by the worker authoritative catalog on opencode/codex/acp
// (callable) and stays discoverable on claude (no authoritative tier). "temp-skill"
// is filesystem-only everywhere.
func contractFSSkills() []skills.Skill {
	return []skills.Skill{
		{Name: contractSkillName, FilePath: contractFSPath},
		{Name: contractTempSkill, FilePath: contractTempPath},
	}
}

// ─── fixture ──────────────────────────────────────────────────────────────────

// nativeContractFixture couples a REAL adapter worker wired to a per-protocol
// fake with the per-protocol catalog and wire-shape expectations.
type nativeContractFixture struct {
	combo    e2econtract.Combination
	worker   worker.Worker
	fsSkills []skills.Skill

	// longFormPath is the invocation path the explicit "/worker <name>" entry
	// resolves to (descriptor path; empty when the authoritative descriptor
	// carries none). shortFormPath is what the "/<name>" entry resolves to
	// (filesystem path unless the authoritative descriptor overrides it).
	longFormPath  string
	shortFormPath string

	// wantCallable / wantDiscoverable are the merged-catalog classifications.
	wantCallable     []string
	wantDiscoverable []string

	// wireCount returns how many native invocations reached the protocol wire.
	wireCount func() int
	// verifyWire asserts the REAL protocol wire for the given invocation.
	verifyWire func(t *testing.T, inv worker.NativeCommandInvocation)
	// verifyCatalog is the per-protocol authoritative-catalog drive check.
	verifyCatalog func(t *testing.T, merged []worker.NativeCommandDescriptor)
}

func newNativeContractFixture(t *testing.T, combo e2econtract.Combination) *nativeContractFixture {
	t.Helper()
	switch combo.Worker {
	case worker.TypeClaudeCode:
		return newClaudeContractFixture(t)
	case worker.TypeOpenCodeSrv:
		return newOpenCodeContractFixture(t)
	case worker.TypeCodexCLI:
		return newCodexContractFixture(t)
	case worker.TypeACP:
		return newACPContractFixture(t)
	default:
		require.FailNow(t, "native command contract matrix: no fixture for worker %s", combo.Worker)
		return nil
	}
}

func (f *nativeContractFixture) mode() worker.SkillInvocationMode {
	return worker.NativeModeForType(f.combo.Worker)
}

// ─── claude_code (stream-json fake, no authoritative catalog) ────────────────

func newClaudeContractFixture(t *testing.T) *nativeContractFixture {
	t.Helper()
	w := claudecode.New()
	fake, err := claudecode.NewStreamFake()
	require.NoError(t, err, "claude contract fixture: stream fake")
	t.Cleanup(func() { require.NoError(t, fake.Close()) })
	fake.Attach(w, "contract-user", contractSessionID)

	return &nativeContractFixture{
		combo:            e2econtract.Combination{Worker: worker.TypeClaudeCode},
		worker:           w,
		fsSkills:         contractFSSkills(),
		longFormPath:     contractFSPath,
		shortFormPath:    contractFSPath,
		wantCallable:     nil,
		wantDiscoverable: []string{contractSkillName, contractTempSkill},
		wireCount:        func() int { return len(fake.Frames()) },
		verifyWire: func(t *testing.T, inv worker.NativeCommandInvocation) {
			fake.AssertUserFrame(t, inv.Name, inv.Args)
			require.Equal(t, canonicalSlashText(inv), fake.Conn().LastInput(),
				"claude: crash replay must retain the canonical slash text")
		},
		verifyCatalog: func(t *testing.T, merged []worker.NativeCommandDescriptor) {
			// No authoritative catalog: the worker tier contributes nothing, so
			// oracle-dba comes from the filesystem tier and must stay
			// discoverable (§10.1).
			desc := findNativeDescriptor(merged, contractSkillName)
			require.NotNil(t, desc, "claude: oracle-dba must be in the merged catalog (filesystem tier)")
			require.Equal(t, worker.NativeCommandKindSkill, desc.Kind)
			require.True(t, desc.StartsTurn)
		},
	}
}

func canonicalSlashText(inv worker.NativeCommandInvocation) string {
	text := "/" + strings.TrimSpace(inv.Name)
	if args := strings.TrimSpace(inv.Args); args != "" {
		text += " " + args
	}
	return text
}

// ─── opencode_server (httptest GET /command + POST /session/{id}/command) ────

// opencodeRecorder captures the command-catalog and skill-invocation traffic
// against the fake OpenCode HTTP server.
type opencodeRecorder struct {
	mu       sync.Mutex
	catalog  []map[string]any
	commands []opencodeCommandPost
}

type opencodeCommandPost struct {
	Path   string
	Body   map[string]any
	Method string
}

func (r *opencodeRecorder) commandCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.commands)
}

func (r *opencodeRecorder) lastCommand() (opencodeCommandPost, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.commands) == 0 {
		return opencodeCommandPost{}, false
	}
	return r.commands[len(r.commands)-1], true
}

func newOpenCodeContractFixture(t *testing.T) *nativeContractFixture {
	t.Helper()
	rec := &opencodeRecorder{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/command" {
			rec.mu.Lock()
			rec.catalog = append(rec.catalog, map[string]any{"name": contractSkillName, "description": "DBA helper"})
			rec.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"name": contractSkillName, "description": "DBA helper"},
			})
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/command") {
			body, _ := io.ReadAll(r.Body)
			var parsed map[string]any
			_ = json.Unmarshal(body, &parsed)
			rec.mu.Lock()
			rec.commands = append(rec.commands, opencodeCommandPost{Path: r.URL.Path, Body: parsed, Method: r.Method})
			rec.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "msg-1"})
			return
		}
		http.NotFound(w, r)
	})
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	w := opencodeserver.NewContractWorker(ts.Client(), ts.URL)

	return &nativeContractFixture{
		combo:            e2econtract.Combination{Worker: worker.TypeOpenCodeSrv},
		worker:           w,
		fsSkills:         contractFSSkills(),
		longFormPath:     "", // authoritative descriptors carry no path
		shortFormPath:    contractFSPath,
		wantCallable:     []string{contractSkillName},
		wantDiscoverable: []string{contractTempSkill},
		wireCount:        rec.commandCount,
		verifyWire: func(t *testing.T, inv worker.NativeCommandInvocation) {
			got, ok := rec.lastCommand()
			require.True(t, ok, "opencode: exactly one POST /session/{id}/command must reach the server")
			require.Equal(t, "/session/"+contractSessionID+"/command", got.Path,
				"opencode: skill invocation must POST the command endpoint")
			require.Equal(t, inv.Name, got.Body["command"])
			require.Equal(t, inv.Args, got.Body["arguments"])
		},
		verifyCatalog: func(t *testing.T, merged []worker.NativeCommandDescriptor) {
			desc := findNativeDescriptor(merged, contractSkillName)
			require.NotNil(t, desc, "opencode: oracle-dba must be advertised by GET /command")
			require.Equal(t, worker.SkillModeRPCCommand, desc.Mode)
		},
	}
}

// ─── codex_cli (app-server JSON-RPC pipes, skills/list authoritative) ────────

// codexRecorder captures the app-server request stream (skills/list + turn/start)
// and counts turn/start invocations.
type codexRecorder struct {
	mu       sync.Mutex
	requests []string
}

func (r *codexRecorder) add(line string) {
	r.mu.Lock()
	r.requests = append(r.requests, line)
	r.mu.Unlock()
}

func (r *codexRecorder) requestCount(method string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, line := range r.requests {
		var req struct {
			Method string `json:"method"`
		}
		if json.Unmarshal([]byte(line), &req) == nil && req.Method == method {
			n++
		}
	}
	return n
}

func (r *codexRecorder) lastTurnStart() (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.requests) - 1; i >= 0; i-- {
		var req struct {
			Method string `json:"method"`
		}
		if json.Unmarshal([]byte(r.requests[i]), &req) == nil && req.Method == "turn/start" {
			return r.requests[i], true
		}
	}
	return "", false
}

func newCodexContractFixture(t *testing.T) *nativeContractFixture {
	t.Helper()
	rec := &codexRecorder{}
	stdinR, stdinW := io.Pipe()
	_, agentCancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		agentCancel()
		_ = stdinR.Close()
	})
	mgr := codexcli.NewManagerForContractTest(stdinW)
	w := codexcli.NewContractWorker(mgr, "thread-contract")

	go func() {
		sc := bufio.NewScanner(stdinR)
		for sc.Scan() {
			line := sc.Text()
			rec.add(line)
			var req struct {
				ID     int64  `json:"id"`
				Method string `json:"method"`
			}
			if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
				continue
			}
			switch req.Method {
			case "skills/list":
				frame := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"data":[{"cwd":"%s","errors":[],"skills":[{"name":"%s","description":"DBA helper","enabled":true,"path":"%s","scope":"project"}]}]}}`,
					req.ID, contractWorkDir, contractSkillName, contractCodexPath)
				mgr.DispatchFrameForTest([]byte(frame))
			case "turn/start":
				frame := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"turn":{"id":"turn-1"}}}`, req.ID)
				mgr.DispatchFrameForTest([]byte(frame))
			}
		}
	}()

	return &nativeContractFixture{
		combo:            e2econtract.Combination{Worker: worker.TypeCodexCLI},
		worker:           w,
		fsSkills:         contractFSSkills(),
		longFormPath:     contractCodexPath,
		shortFormPath:    contractCodexPath,
		wantCallable:     []string{contractSkillName},
		wantDiscoverable: []string{contractTempSkill},
		wireCount:        func() int { return rec.requestCount("turn/start") },
		verifyWire: func(t *testing.T, inv worker.NativeCommandInvocation) {
			line, ok := rec.lastTurnStart()
			require.True(t, ok, "codex: exactly one turn/start must reach the app-server stdin")
			// §10.4: the structured skill item must carry the skills/list
			// descriptor path, never the HotPlex filesystem guess.
			require.Contains(t, line, `"type":"skill"`, "codex: wire must carry the structured skill item")
			require.Contains(t, line, `"name":"`+inv.Name+`"`)
			require.Contains(t, line, `"path":"`+contractCodexPath+`"`, "codex: skill item path must equal the skills/list descriptor path")
			require.Contains(t, line, `"type":"text"`)
			require.Contains(t, line, `"text":"$`+inv.Name+` `+inv.Args+`"`)
		},
		verifyCatalog: func(t *testing.T, merged []worker.NativeCommandDescriptor) {
			desc := findNativeDescriptor(merged, contractSkillName)
			require.NotNil(t, desc, "codex: oracle-dba must come from skills/list")
			require.Equal(t, contractCodexPath, desc.Path, "codex: descriptor path must be the skills/list path")
		},
	}
}

// ─── acp (advertised commands, session/prompt JSON-RPC pipes) ────────────────

// acpRecorder captures the JSON-RPC requests the worker writes to the fake
// agent and counts session/prompt invocations.
type acpRecorder struct {
	mu       sync.Mutex
	requests []string
}

func (r *acpRecorder) add(line string) {
	r.mu.Lock()
	r.requests = append(r.requests, line)
	r.mu.Unlock()
}

func (r *acpRecorder) promptCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, line := range r.requests {
		var req struct {
			Method string `json:"method"`
		}
		if json.Unmarshal([]byte(line), &req) == nil && req.Method == "session/prompt" {
			n++
		}
	}
	return n
}

func (r *acpRecorder) lastPrompt() (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.requests) - 1; i >= 0; i-- {
		var req struct {
			Method string `json:"method"`
		}
		if json.Unmarshal([]byte(r.requests[i]), &req) == nil && req.Method == "session/prompt" {
			return r.requests[i], true
		}
	}
	return "", false
}

func newACPContractFixture(t *testing.T) *nativeContractFixture {
	t.Helper()
	rec := &acpRecorder{}
	stdinR, stdinW := io.Pipe()   // worker → fake agent requests
	stdoutR, stdoutW := io.Pipe() // fake agent → worker responses
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		_ = stdinR.Close()
		_ = stdoutW.Close()
	})

	client := acp.NewACPClient(stdinW, bufio.NewReader(stdoutR), slog.Default())
	client.StartReadLoop(ctx)

	advertised := []worker.SkillDescriptor{{Name: contractSkillName, Description: "DBA helper"}}
	w := acp.NewContractWorker(advertised, client, contractSessionID)

	go func() {
		sc := bufio.NewScanner(stdinR)
		for sc.Scan() {
			line := sc.Text()
			rec.add(line)
			var req struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
				continue
			}
			if req.Method == "session/prompt" {
				result, _ := json.Marshal(acp.PromptResult{StopReason: "end_turn"})
				resp := &acp.JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
				_ = acp.WriteMessage(stdoutW, resp)
			}
		}
	}()

	return &nativeContractFixture{
		combo:            e2econtract.Combination{Worker: worker.TypeACP},
		worker:           w,
		fsSkills:         contractFSSkills(),
		longFormPath:     "",
		shortFormPath:    contractFSPath,
		wantCallable:     []string{contractSkillName},
		wantDiscoverable: []string{contractTempSkill},
		wireCount:        rec.promptCount,
		verifyWire: func(t *testing.T, inv worker.NativeCommandInvocation) {
			line, ok := rec.lastPrompt()
			require.True(t, ok, "acp: exactly one session/prompt must reach the agent stdin")
			var req struct {
				Method string `json:"method"`
				Params struct {
					SessionID string `json:"sessionId"`
					Prompt    []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"prompt"`
				} `json:"params"`
			}
			require.NoError(t, json.Unmarshal([]byte(line), &req))
			require.Equal(t, "session/prompt", req.Method)
			require.Equal(t, contractSessionID, req.Params.SessionID)
			require.Len(t, req.Params.Prompt, 1)
			require.Equal(t, canonicalSlashText(inv), req.Params.Prompt[0].Text,
				"acp: the advertised command must be sent as a slash prompt, not raw text")
		},
		verifyCatalog: func(t *testing.T, merged []worker.NativeCommandDescriptor) {
			desc := findNativeDescriptor(merged, contractSkillName)
			require.NotNil(t, desc, "acp: oracle-dba must be advertised via available_commands_update")
			require.Equal(t, worker.SkillModeAdvertisedCommand, desc.Mode)
		},
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func findNativeDescriptor(descriptors []worker.NativeCommandDescriptor, name string) *worker.NativeCommandDescriptor {
	for i := range descriptors {
		if descriptors[i].Name == name {
			return &descriptors[i]
		}
	}
	return nil
}

// newContractHandler wires a full dispatch handler (mock session manager + real
// hub + platform conn + merged catalog store) around the fixture worker.
func (f *nativeContractFixture) newContractHandler(t *testing.T, store execution.Store) (*Handler, *mockInputSM, *mockPlatformConn) {
	t.Helper()
	sm := new(mockInputSM)
	sm.On("Get", "s1").Return(sessionWithWorkDir("s1"), nil).Maybe()
	sm.On("GetWorker", "s1").Return(f.worker).Maybe()

	hub := newTestHub(t)
	conn := &mockPlatformConn{}
	hub.JoinPlatformSession("s1", conn)

	h := &Handler{
		log:             testLogger(t),
		hub:             hub,
		sm:              sm,
		catalogStore:    newSessionCatalogStore(slog.Default(), fixedSkillsLocator{items: f.fsSkills}),
		skillsLocator:   fixedSkillsLocator{items: f.fsSkills},
		executionStore:  store,
		ownerInstanceID: "contract-matrix",
	}
	return h, sm, conn
}

// assertDeliveredAck asserts the durable path emitted the terminal delivered
// InputAck for the given envelope's client message id.
func assertDeliveredAck(t *testing.T, conn *mockPlatformConn, wantStatus events.ExecutionStatus) {
	t.Helper()
	require.Eventually(t, func() bool {
		for _, got := range conn.envelopes() {
			if got.Event.Type != events.InputAck {
				continue
			}
			data, ok := got.Event.Data.(events.InputAckData)
			if ok && data.Status == wantStatus {
				return true
			}
		}
		return false
	}, time.Second, 10*time.Millisecond, "expected an InputAck with status %s", wantStatus)
}

// ─── the 12-combo matrix ──────────────────────────────────────────────────────

func TestNativeCommandContractMatrix(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{}, len(e2econtract.Combinations()))
	for _, combo := range e2econtract.Combinations() {
		combo := combo
		t.Run(combo.ID, func(t *testing.T) {
			t.Parallel()
			f := newNativeContractFixture(t, combo)
			runNativeContractPhases(t, f)
		})
		seen[combo.ID] = struct{}{}
	}
	require.Len(t, seen, 12, "the native command contract matrix must cover all 12 platform×worker rows")
	require.Len(t, e2econtract.Combinations(), 12)
}

func runNativeContractPhases(t *testing.T, f *nativeContractFixture) {
	t.Helper()

	// The phases run sequentially on ONE fixture: the per-protocol wire
	// recorder is shared, so the phase assertions use deltas. The per-combo
	// subtest itself still runs t.Parallel() against the other 11 rows.

	// Phase 1 + 3 — parser resolution against the merged catalog and the REAL
	// per-protocol invocation wire shape.
	f.phaseParser(t)
	f.phaseCatalog(t)
	f.phaseBusy(t)
	f.phaseReplay(t)
	f.phaseDuplicate(t)
	f.phaseTerminal(t)
}

// settleWire polls the protocol wire recorder until its count is stable (the
// claude stream fake decodes captured frames asynchronously) and returns it.
// Baselines must settle before a no-growth assertion can distinguish a genuine
// non-invocation from a late-arriving capture.
func (f *nativeContractFixture) settleWire(t *testing.T) int {
	t.Helper()
	var last, stable int
	require.Eventually(t, func() bool {
		cur := f.wireCount()
		if cur == last {
			stable++
		} else {
			last = cur
			stable = 0
		}
		return stable >= 3
	}, time.Second, 5*time.Millisecond, "%s: wire recorder must settle before the assertion", f.combo.ID)
	return last
}

// phaseParser drives both the explicit "/worker <name>" and the short
// "/<name>" entry through the gateway dispatch and asserts the resolution
// reaches the worker's native invoker with the protocol-correct invocation,
// plus the REAL wire shape. Unknown names resolve to NOT_SUPPORTED with no wire.
func (f *nativeContractFixture) phaseParser(t *testing.T) {
	t.Helper()

	// Explicit "/worker oracle-dba 10.0.0.1".
	h, _, conn := f.newContractHandler(t, &fakeExecutionStore{record: testExecutionRecord(execution.StatusAccepted)})
	require.NoError(t, h.handleInput(t.Context(), inputEnvelopeWithMetadata("s1", contractSkillInput, nil)),
		"%s: explicit /worker dispatch must succeed", f.combo.ID)
	require.Eventually(t, func() bool { return f.wireCount() >= 1 }, time.Second, 10*time.Millisecond,
		"%s: explicit /worker dispatch must reach the protocol wire", f.combo.ID)
	f.verifyWire(t, worker.NativeCommandInvocation{
		Name: contractSkillName, Args: contractSkillArgs, Path: f.longFormPath, Mode: f.mode(),
	})
	assertDeliveredAck(t, conn, events.ExecutionStatusDelivered)

	// Short "/oracle-dba 10.0.0.1" resolves through the filesystem catalog and
	// re-dispatches through the same native invoker.
	before := f.settleWire(t)
	h2, _, _ := f.newContractHandler(t, &fakeExecutionStore{record: testExecutionRecord(execution.StatusAccepted)})
	require.NoError(t, h2.handleInput(t.Context(), inputEnvelopeWithMetadata("s1", contractSlashInput, nil)),
		"%s: short /oracle-dba dispatch must succeed", f.combo.ID)
	require.Eventually(t, func() bool { return f.wireCount() == before+1 }, time.Second, 10*time.Millisecond,
		"%s: short form must invoke exactly once", f.combo.ID)
	f.verifyWire(t, worker.NativeCommandInvocation{
		Name: contractSkillName, Args: contractSkillArgs, Path: f.shortFormPath, Mode: f.mode(),
	})

	// Unknown name → NOT_SUPPORTED, never falls through to an ordinary prompt.
	unknownBefore := f.settleWire(t)
	h3, _, _ := f.newContractHandler(t, &fakeExecutionStore{record: testExecutionRecord(execution.StatusAccepted)})
	err := h3.handleInput(t.Context(), inputEnvelopeWithMetadata("s1", "/worker unknown-thing", nil))
	require.Error(t, err, "%s: unknown /worker name must be rejected", f.combo.ID)
	require.ErrorContains(t, err, string(events.ErrCodeNotSupported), "%s: unknown name must map to NOT_SUPPORTED", f.combo.ID)
	f.settleWire(t)
	require.Equal(t, unknownBefore, f.wireCount(), "%s: an unknown /worker name must never reach the wire", f.combo.ID)
}

// phaseCatalog asserts the merged catalog classifies the worker-authoritative
// and filesystem tiers per protocol (§10.1/§10.5).
func (f *nativeContractFixture) phaseCatalog(t *testing.T) {
	t.Helper()

	store := newSessionCatalogStore(slog.Default(), fixedSkillsLocator{items: f.fsSkills})
	merged, lookupErr := store.Lookup(t.Context(), "s1", contractWorkDir, f.worker, 0)
	require.NoError(t, lookupErr, "%s: authoritative catalog must be fetchable", f.combo.ID)

	// The per-protocol authoritative drive (paths, modes, discovery boundaries).
	f.verifyCatalog(t, merged)

	// Status classification via the /skills entry builder.
	entries := buildSkillEntriesFromCatalog(merged, f.fsSkills, f.worker, lookupErr == nil)
	byName := make(map[string]events.SkillEntry, len(entries))
	for _, e := range entries {
		byName[e.Name] = e
	}
	require.Equal(t, "gateway", byName["reset"].Source, "fixed gateway commands stay present")

	for _, name := range f.wantCallable {
		require.Equal(t, events.SkillStatusCallable, byName[name].Status, "%s: %q must be callable", f.combo.ID, name)
	}
	for _, name := range f.wantDiscoverable {
		require.Equal(t, events.SkillStatusDiscoverable, byName[name].Status, "%s: %q must stay discoverable", f.combo.ID, name)
	}
}

// phaseBusy holds the active gate and asserts a second /worker input is
// buffered with the resolved NativeCommandInvocation stashed — never injected
// mid-turn as raw text.
func (f *nativeContractFixture) phaseBusy(t *testing.T) {
	t.Helper()

	h, sm, _, bridge := newBusyTestHandler(t, f.worker)
	h.catalogStore = newSessionCatalogStore(slog.Default(), fixedSkillsLocator{items: f.fsSkills})
	h.skillsLocator = fixedSkillsLocator{items: f.fsSkills}
	h.executionStore = &fakeExecutionStore{
		acceptErr:    execution.ErrSessionBusy,
		activeRecord: testExecutionRecord(execution.StatusDelivered),
	}
	sm.On("Get", "s").Return(sessionWithWorkDir("s"), nil).Maybe()

	env := newInputEnvelope(t, "s", contractSkillInput)
	env.Seq = 5
	require.NoError(t, h.handleInput(t.Context(), env))

	merged, repr, ok := bridge.pending.DrainAndMerge("s")
	require.True(t, ok, "%s: busy /worker input must be buffered for post-Done replay", f.combo.ID)
	require.Equal(t, contractSkillInput, merged)
	got, ok := invocationFromMetadata(repr.Metadata, merged)
	require.True(t, ok, "%s: buffered envelope must carry the resolved native invocation stash", f.combo.ID)
	require.Equal(t, contractSkillName, got.Name)
	require.Equal(t, contractSkillArgs, got.Args)
	require.Equal(t, f.longFormPath, got.Path, "%s: stash must preserve the descriptor path", f.combo.ID)
	require.Equal(t, f.mode(), got.Mode, "%s: stash must preserve the protocol mode", f.combo.ID)
}

// phaseReplay asserts crash replay carries InputReplay.Skill as a structured
// NativeCommandInvocation and re-dispatches it through the native invoker.
func (f *nativeContractFixture) phaseReplay(t *testing.T) {
	t.Helper()

	b := &Bridge{log: testLogger(t)}
	inv := worker.NativeCommandInvocation{
		Name: contractSkillName,
		Args: contractSkillArgs,
		Path: f.shortFormPath,
		Mode: f.mode(),
	}
	replay := worker.InputReplay{Content: contractSlashInput, Skill: &inv}
	require.NoError(t, b.deliverInputReplay(t.Context(), f.worker, replay),
		"%s: crash replay must re-dispatch the native invocation", f.combo.ID)
	f.verifyWire(t, inv)
}

// phaseDuplicate asserts a duplicate client message id is suppressed by the
// durable accept gate — the native command is not invoked a second time (§10.7).
func (f *nativeContractFixture) phaseDuplicate(t *testing.T) {
	t.Helper()

	before := f.settleWire(t)
	h, _, _ := f.newContractHandler(t, &fakeExecutionStore{
		record:    testExecutionRecord(execution.StatusAccepted),
		duplicate: true,
	})
	env := inputEnvelopeWithMetadata("s1", contractSkillInput, nil)
	env.ID = "evt-client-1"
	require.NoError(t, h.handleInput(t.Context(), env))
	f.settleWire(t)
	require.Equal(t, before, f.wireCount(), "%s: a duplicate client message id must not re-invoke", f.combo.ID)
}

// phaseTerminal asserts a done emitted after completion converges the execution
// runtime from unknown to completed.
func (f *nativeContractFixture) phaseTerminal(t *testing.T) {
	t.Helper()

	store := &fakeExecutionStore{openRecord: testExecutionRecord(execution.StatusUnknown)}
	b := &Bridge{executionStore: store, hub: newTestHub(t), log: testLogger(t)}
	fc := &forwardContext{sessionID: "s1", workerRunID: "run-contract"}
	done := events.NewEnvelope("evt-done", "s1", 1, events.Done, events.DoneData{Success: true})

	b.finishRuntimeOnDone("s1", fc, done)
	require.Equal(t, "run-contract", store.finishRunID, "%s: done must correlate with the emitting forwarder run", f.combo.ID)
	require.Equal(t, execution.RuntimeCompleted, store.finishStatus,
		"%s: a late done must converge an unknown runtime to completed", f.combo.ID)
}

// ─── protocol-specific contract edges (§10.1/§10.5, outside the 12-row loop) ─

// TestNativeCommandContractOpenCodeAuthFailure covers the catalog degradation
// contract: a 401 on GET /command is "cannot confirm", so the command is
// discoverable-only and dispatch resolves to NOT_SUPPORTED — never sent.
func TestNativeCommandContractOpenCodeAuthFailure(t *testing.T) {
	t.Parallel()

	rec := &opencodeRecorder{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/command" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(ts.Close)

	w := opencodeserver.NewContractWorker(ts.Client(), ts.URL)
	fsSkills := contractFSSkills()
	store := newSessionCatalogStore(slog.Default(), fixedSkillsLocator{items: fsSkills})

	merged, err := store.Lookup(t.Context(), "s1", contractWorkDir, w, 0)
	require.Error(t, err, "a 401 catalog query must degrade with an explicit error")
	require.ErrorContains(t, err, "catalog unavailable")

	entries := buildSkillEntriesFromCatalog(merged, fsSkills, w, false)
	byName := make(map[string]events.SkillEntry, len(entries))
	for _, e := range entries {
		byName[e.Name] = e
	}
	require.Equal(t, events.SkillStatusDiscoverable, byName[contractSkillName].Status,
		"opencode: 401 catalog means the command cannot be confirmed callable")

	// Dispatch resolves to NOT_SUPPORTED with no command POST.
	sm := new(mockInputSM)
	sm.On("Get", "s1").Return(sessionWithWorkDir("s1"), nil).Maybe()
	sm.On("GetWorker", "s1").Return(w).Maybe()
	hub := newTestHub(t)
	conn := &mockPlatformConn{}
	hub.JoinPlatformSession("s1", conn)
	h := &Handler{
		log:            testLogger(t),
		hub:            hub,
		sm:             sm,
		catalogStore:   store,
		skillsLocator:  fixedSkillsLocator{items: fsSkills},
		executionStore: &fakeExecutionStore{record: testExecutionRecord(execution.StatusAccepted)},
	}

	err = h.handleInput(t.Context(), inputEnvelopeWithMetadata("s1", contractSkillInput, nil))
	require.Error(t, err)
	require.ErrorContains(t, err, string(events.ErrCodeNotSupported))
	require.Equal(t, 0, rec.commandCount(), "opencode: a 401 catalog must never dispatch a command POST")
}

// TestNativeCommandContractACPNeverSendsNonAdvertisedAsPrompt covers §10.5: a
// NON-advertised command (present only as a HotPlex filesystem temp skill) is
// rejected NOT_SUPPORTED and never reaches the agent as an ordinary prompt.
func TestNativeCommandContractACPNeverSendsNonAdvertisedAsPrompt(t *testing.T) {
	t.Parallel()

	rec := &acpRecorder{}
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		_ = stdinR.Close()
		_ = stdoutW.Close()
	})
	client := acp.NewACPClient(stdinW, bufio.NewReader(stdoutR), slog.Default())
	client.StartReadLoop(ctx)

	advertised := []worker.SkillDescriptor{{Name: contractSkillName, Description: "DBA helper"}}
	w := acp.NewContractWorker(advertised, client, contractSessionID)

	go func() {
		sc := bufio.NewScanner(stdinR)
		for sc.Scan() {
			rec.add(sc.Text())
		}
	}()

	fsSkills := contractFSSkills()
	sm := new(mockInputSM)
	sm.On("Get", "s1").Return(sessionWithWorkDir("s1"), nil).Maybe()
	sm.On("GetWorker", "s1").Return(w).Maybe()
	hub := newTestHub(t)
	conn := &mockPlatformConn{}
	hub.JoinPlatformSession("s1", conn)
	h := &Handler{
		log:            testLogger(t),
		hub:            hub,
		sm:             sm,
		catalogStore:   newSessionCatalogStore(slog.Default(), fixedSkillsLocator{items: fsSkills}),
		skillsLocator:  fixedSkillsLocator{items: fsSkills},
		executionStore: &fakeExecutionStore{record: testExecutionRecord(execution.StatusAccepted)},
	}

	for _, content := range []string{"/worker temp-skill", "/temp-skill"} {
		err := h.handleInput(t.Context(), inputEnvelopeWithMetadata("s1", content, nil))
		require.Error(t, err, "acp: non-advertised %q must be rejected", content)
		require.ErrorContains(t, err, string(events.ErrCodeNotSupported), "acp: non-advertised %q maps to NOT_SUPPORTED", content)
	}
	require.Equal(t, 0, rec.promptCount(),
		"acp: a non-advertised command must never be sent to the agent as an ordinary prompt")
}

// TestNativeCommandContractAllWorkerModesInvokable guards the matrix's
// capability baseline: every matrix worker type maps to a distinct non-empty
// native invocation mode.
func TestNativeCommandContractAllWorkerModesInvokable(t *testing.T) {
	t.Parallel()

	for _, wt := range []worker.WorkerType{
		worker.TypeClaudeCode,
		worker.TypeOpenCodeSrv,
		worker.TypeCodexCLI,
		worker.TypeACP,
	} {
		require.NotEmpty(t, worker.NativeModeForType(wt), "worker %s must have a native invocation mode", wt)
	}
}
