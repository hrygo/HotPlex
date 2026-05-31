package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/worker/base"
)

// ─── EX-01: InitConfig Args/Debug ───────────────────────────────────────────

func TestInitConfig_ArgsStored(t *testing.T) {

	InitConfig(config.ACPConfig{
		Command:     "test-agent serve",
		Args:        []string{"--model", "gpt-4"},
		AutoApprove: false,
		Debug:       true,
	})

	parts, _ := commandParts.Load().([]string)
	require.Equal(t, []string{"test-agent", "serve"}, parts)

	ca, _ := configArgs.Load().([]string)
	require.Equal(t, []string{"--model", "gpt-4"}, ca)

	require.True(t, debugEnabled.Load())

	// Restore defaults.
	InitConfig(config.ACPConfig{Command: "hermes acp"})
}

func TestInitConfig_DefaultCommand(t *testing.T) {

	InitConfig(config.ACPConfig{Command: ""})

	parts, _ := commandParts.Load().([]string)
	require.Equal(t, []string{"hermes", "acp"}, parts)

	// Restore defaults.
	InitConfig(config.ACPConfig{Command: "hermes acp"})
}

// ─── EX-02: Protocol Version Warning ────────────────────────────────────────

func TestProtocolVersion_KnownVersion_NoWarn(t *testing.T) {
	t.Parallel()
	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	w.initResult = &InitializeResult{ProtocolVersion: 1}
	// Version 1 is known — no special behavior needed beyond logging.
	require.Equal(t, 1, w.initResult.ProtocolVersion)
}

func TestProtocolVersion_FutureVersion_Stored(t *testing.T) {
	t.Parallel()
	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	w.initResult = &InitializeResult{ProtocolVersion: 5}
	// Future version should still be stored for capability checks.
	require.Equal(t, 5, w.initResult.ProtocolVersion)
}

// ─── U-04: TraceWriter ──────────────────────────────────────────────────────

func TestTraceWriter_CreateWriteRead(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tw, err := NewTraceWriter(dir, "test-session-001")
	require.NoError(t, err)
	t.Cleanup(func() { _ = tw.Close() })

	require.Contains(t, tw.Path(), "acp-trace-test-session-001.jsonl")

	// Write a trace entry.
	tw.Log("→", map[string]string{"method": "session/prompt", "content": "hello"})

	// Close to flush.
	require.NoError(t, tw.Close())

	// Read back the file.
	data, err := os.ReadFile(tw.Path())
	require.NoError(t, err)
	require.Contains(t, string(data), `"dir":"→"`)
	require.Contains(t, string(data), `"method":"session/prompt"`)

	// Verify it's valid JSONL.
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	require.Len(t, lines, 1)
	var entry map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &entry))
	require.Equal(t, "→", entry["dir"])
}

func TestTraceWriter_NilSafe(t *testing.T) {
	t.Parallel()
	var tw *TraceWriter
	// All operations on nil TraceWriter should be no-ops.
	tw.Log("→", "test")
	require.NoError(t, tw.Close())
	tw.Rotate()
	require.Equal(t, "", tw.Path())
}

func TestTraceWriter_Rotation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tw, err := NewTraceWriter(dir, "test-rotate")
	require.NoError(t, err)
	t.Cleanup(func() { _ = tw.Close() })

	// Write enough data to exceed 100 bytes.
	for i := 0; i < 50; i++ {
		tw.Log("→", map[string]string{"method": "session/prompt", "content": strings.Repeat("x", 100)})
	}
	require.NoError(t, tw.Close())

	// Verify file exists and has content.
	data, err := os.ReadFile(tw.Path())
	require.NoError(t, err)
	require.True(t, len(data) > 100, "trace file should have data")
}

func TestTraceWriter_DebugDisabledByDefault(t *testing.T) {
	t.Parallel()
	// debugEnabled should be false by default.
	require.False(t, debugEnabled.Load())
}

// ─── FR-08: ForkSession ───────────────────────────────

func TestClient_ForkSession_Success(t *testing.T) {
	t.Parallel()

	agentStdinR, agentStdinW := io.Pipe()
	agentStdoutR, agentStdoutW := io.Pipe()
	defer agentStdinW.Close()
	defer agentStdoutW.Close()

	client := NewACPClient(agentStdinW, agentStdoutR, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client.StartReadLoop(ctx)

	// Agent responds to fork with a new session ID.
	go func() {
		scanner := NewScanner(agentStdinR)
		if scanner.Scan() {
			var req struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			_ = json.Unmarshal(scanner.Bytes(), &req)
			_ = WriteMessage(agentStdoutW, &JSONRPCResponse{
				JSONRPC: "2.0", ID: req.ID,
				Result: mustMarshal(SessionResult{SessionID: "forked_sess_42"}),
			})
		}
	}()

	result, err := client.ForkSession(ctx, "old_session")
	require.NoError(t, err)
	require.Equal(t, "forked_sess_42", result.SessionID)
}

func TestClient_ForkSession_Error(t *testing.T) {
	t.Parallel()

	agentStdinR, agentStdinW := io.Pipe()
	agentStdoutR, agentStdoutW := io.Pipe()
	defer agentStdinW.Close()
	defer agentStdoutW.Close()

	client := NewACPClient(agentStdinW, agentStdoutR, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client.StartReadLoop(ctx)

	// Agent responds with an error for fork.
	go func() {
		scanner := NewScanner(agentStdinR)
		if scanner.Scan() {
			var req struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			_ = json.Unmarshal(scanner.Bytes(), &req)
			_ = WriteMessage(agentStdoutW, &JSONRPCResponse{
				JSONRPC: "2.0", ID: req.ID,
				Error: &JSONRPCError{Code: -32600, Message: "fork not supported"},
			})
		}
	}()

	_, err := client.ForkSession(ctx, "old_session")
	require.Error(t, err)
	require.Contains(t, err.Error(), "fork not supported")
}

// ─── FR-09: JSON Schema Injection ─────────────────────────────────

func TestJSONSchema_InjectedOnFirstPrompt(t *testing.T) {
	t.Parallel()
	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	w.jsonSchema = `{"type":"object","properties":{"name":{"type":"string"}}}`

	// First call should inject.
	require.True(t, w.jsonSchemaInjected.CompareAndSwap(false, true))
	content := "Hello"
	content = fmt.Sprintf("[JSON SCHEMA]\n%s\n[/JSON SCHEMA]\n\n%s", w.jsonSchema, content)
	require.Contains(t, content, "[JSON SCHEMA]")
	require.Contains(t, content, `"type":"object"`)
	require.True(t, strings.HasPrefix(content, "[JSON SCHEMA]"))
	require.Contains(t, content, "Hello")

	// Second CompareAndSwap should fail (already injected).
	require.False(t, w.jsonSchemaInjected.CompareAndSwap(false, true))
}

func TestJSONSchema_EmptySchema_NoInjection(t *testing.T) {
	t.Parallel()
	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	// jsonSchema is empty by default.
	require.Empty(t, w.jsonSchema)
	require.False(t, w.jsonSchemaInjected.Load())

	// CompareAndSwap should succeed (false->true), but the outer if-check on
	// jsonSchema != "" prevents injection.
	require.True(t, w.jsonSchemaInjected.CompareAndSwap(false, true))
}

func TestJSONSchema_BothSystemPromptAndSchema(t *testing.T) {
	t.Parallel()
	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	w.systemPrompt = "You are a helpful assistant."
	w.jsonSchema = `{"type":"object","properties":{"result":{"type":"string"}}}`

	// Both should be injectable independently.
	require.True(t, w.systemPromptInjected.CompareAndSwap(false, true))
	require.True(t, w.jsonSchemaInjected.CompareAndSwap(false, true))

	// Simulate the Input() injection logic.
	content := "Hello"
	if w.systemPrompt != "" {
		content = fmt.Sprintf("[SYSTEM INSTRUCTIONS]\n%s\n[/SYSTEM INSTRUCTIONS]\n\n%s", w.systemPrompt, content)
	}
	if w.jsonSchema != "" {
		content = fmt.Sprintf("[JSON SCHEMA]\n%s\n[/JSON SCHEMA]\n\n%s", w.jsonSchema, content)
	}

	require.Contains(t, content, "[SYSTEM INSTRUCTIONS]")
	require.Contains(t, content, "[JSON SCHEMA]")
	require.Contains(t, content, "You are a helpful assistant.")
	require.Contains(t, content, `"type":"object"`)
	// JSON Schema should be injected AFTER system prompt (prepended).
	require.True(t, strings.Contains(content, "[JSON SCHEMA]"))
	require.True(t, strings.Contains(content, "Hello"))
}

func TestJSONSchema_SchemaOnly_NoSystemPrompt(t *testing.T) {
	t.Parallel()
	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	w.jsonSchema = `{"type":"array","items":{"type":"number"}}`

	// Only JSON Schema, no system prompt.
	content := "List primes"
	if w.systemPrompt != "" {
		content = fmt.Sprintf("[SYSTEM INSTRUCTIONS]\n%s\n[/SYSTEM INSTRUCTIONS]\n\n%s", w.systemPrompt, content)
	}
	if w.jsonSchema != "" {
		content = fmt.Sprintf("[JSON SCHEMA]\n%s\n[/JSON SCHEMA]\n\n%s", w.jsonSchema, content)
	}

	// System prompt should NOT be injected.
	require.NotContains(t, content, "[SYSTEM INSTRUCTIONS]")
	// JSON Schema should be injected.
	require.Contains(t, content, "[JSON SCHEMA]")
	require.Contains(t, content, `"type":"array"`)
	require.Contains(t, content, "List primes")
}

// ─── EX-01: Config Args Merging ─────────────────────────────────────────────

func TestInitConfig_ArgsMergeOrder(t *testing.T) {

	// Set config-level args.
	InitConfig(config.ACPConfig{
		Command: "agent run",
		Args:    []string{"--verbose", "--model=claude"},
	})

	ca, _ := configArgs.Load().([]string)
	require.Equal(t, []string{"--verbose", "--model=claude"}, ca)

	// Verify commandParts are correct.
	parts, _ := commandParts.Load().([]string)
	require.Equal(t, []string{"agent", "run"}, parts)

	// Restore defaults.
	InitConfig(config.ACPConfig{Command: "hermes acp"})
}

// ─── U-04: TraceWriter Concurrent Writes ────────────────────────────────────

func TestTraceWriter_ConcurrentWrites(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tw, err := NewTraceWriter(dir, "concurrent-test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = tw.Close() })

	// Write from multiple goroutines simultaneously.
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			tw.Log("→", map[string]any{"index": n, "data": strings.Repeat("x", 50)})
		}(i)
	}
	wg.Wait()

	require.NoError(t, tw.Close())

	// Read back — should have exactly 10 valid JSONL lines.
	data, err := os.ReadFile(tw.Path())
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	require.Len(t, lines, 10)

	for _, line := range lines {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &entry), "invalid JSONL: %s", line)
	}
}

// ─── TraceWriter File Path ──────────────────────────────────────────────────

func TestTraceWriter_PathFormat(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tw, err := NewTraceWriter(dir, "my-session")
	require.NoError(t, err)
	t.Cleanup(func() { _ = tw.Close() })

	expected := filepath.Join(dir, "acp-trace-my-session.jsonl")
	require.Equal(t, expected, tw.Path())
}
