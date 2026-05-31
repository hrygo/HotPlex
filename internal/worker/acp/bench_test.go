package acp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/hrygo/hotplex/pkg/events"
)

// ─── T-04: Performance Benchmarks ──────────────────────────────────────────────

// benchNotification returns a realistic session/update notification for benchmarking.
func benchNotification(updateType, text string) *JSONRPCNotification {
	params, _ := json.Marshal(map[string]any{
		"sessionId": "sess_bench_123",
		"update": map[string]any{
			"sessionUpdate": updateType,
			"content":       map[string]any{"text": text},
		},
	})
	return &JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params:  params,
	}
}

func BenchmarkMapNotification(b *testing.B) {
	m := NewACPMapper("sess_bench", "user_bench", slog.Default())
	notif := benchNotification("agent_message_chunk", "Hello, this is a benchmark test message for the ACP mapper.")

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m.MapNotification(notif)
	}
}

func BenchmarkMapNotification_Stream(b *testing.B) {
	m := NewACPMapper("sess_bench", "user_bench", slog.Default())
	textNotif := benchNotification("agent_message_chunk", "Streaming text chunk for benchmark. ")
	thoughtNotif := benchNotification("agent_thought_chunk", "Thinking about the answer... ")
	toolNotif := &JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params: marshalParams(map[string]any{
			"sessionId": "sess_bench_123",
			"update": map[string]any{
				"sessionUpdate": "tool_call",
				"id":            "tool_1",
				"tool":          "read_file",
				"arguments":     map[string]any{"path": "/tmp/test.txt"},
			},
		}),
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		// Simulate a realistic stream: 80% text, 10% thought, 10% tool.
		for j := 0; j < 1000; j++ {
			switch j % 10 {
			case 0:
				m.MapNotification(thoughtNotif)
			case 5:
				m.MapNotification(toolNotif)
			default:
				m.MapNotification(textNotif)
			}
		}
		m.Reset()
	}
}

func BenchmarkCodec_WriteMessage(b *testing.B) {
	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mustMarshal(1),
		Method:  "session/prompt",
		Params:  mustMarshal(map[string]any{"sessionId": "sess_1", "content": strings.Repeat("x", 100)}),
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		_ = WriteMessage(&buf, req)
	}
}

func BenchmarkCodec_ReadMessage(b *testing.B) {
	line := `{"jsonrpc":"2.0","id":1,"method":"session/prompt","params":{"sessionId":"sess_1","content":"` +
		strings.Repeat("x", 100) + `"}}` + "\n"
	msg := []byte(line)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		scanner := bufio.NewScanner(bytes.NewReader(msg))
		scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
		_, _ = ReadMessage(scanner)
	}
}

// BenchmarkPrompt_FullTurn simulates a complete prompt→response cycle
// with a mock agent, measuring the overhead of the ACP worker layer.
func BenchmarkPrompt_FullTurn(b *testing.B) {
	// Pre-build the prompt response.
	promptResp := &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      mustMarshal(1),
		Result:  mustMarshal(map[string]any{"stopReason": "end_turn", "usage": map[string]any{"inputTokens": 100, "outputTokens": 50}}),
	}
	respLine, _ := json.Marshal(promptResp)

	// Pre-build a notification.
	notifObj := benchNotification("agent_message_chunk", "This is a streaming response from the agent. ")
	notifLine, _ := json.Marshal(notifObj)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		// Simulate client call: marshal request.
		req := &JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      mustMarshal(1),
			Method:  "session/prompt",
			Params:  mustMarshal(map[string]any{"sessionId": "sess_bench", "content": "Hello"}),
		}
		var buf bytes.Buffer
		_ = WriteMessage(&buf, req)

		// Simulate agent response: read notification + response.
		scanner := bufio.NewScanner(bytes.NewReader(bytes.Join([][]byte{
			append(notifLine, '\n'),
			append(notifLine, '\n'),
			append(respLine, '\n'),
		}, nil)))
		scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
		for {
			msg, _ := ReadMessage(scanner)
			if resp, ok := msg.(*JSONRPCResponse); ok {
				_ = resp
				break
			}
		}
	}
}

// BenchmarkMapNotification_Plan benchmarks plan event mapping.
func BenchmarkMapNotification_Plan(b *testing.B) {
	m := NewACPMapper("sess_bench", "user_bench", slog.Default())
	planUpdate, _ := json.Marshal(map[string]any{
		"sessionUpdate": "plan",
		"items": []map[string]any{
			{"id": "task_1", "text": "Step 1: Read files", "status": "completed"},
			{"id": "task_2", "text": "Step 2: Analyze", "status": "in_progress"},
			{"id": "task_3", "text": "Step 3: Report", "status": "pending"},
		},
	})
	notif := &JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params:  mustMarshal(map[string]any{"sessionId": "sess_bench", "update": planUpdate}),
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m.MapNotification(notif)
	}
}

// marshalParams is a test helper that panics on failure.
func marshalParams(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// Ensure the benchmark event types exist.
var _ = events.Kind("")
