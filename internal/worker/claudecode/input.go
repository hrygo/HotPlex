package claudecode

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/base"
)

// Claude Code stream-json input message types.
// These are protocol-specific to Claude Code's stdin format and do not belong
// in the shared base package.
type streamUserMessage struct {
	Type    string        `json:"type"`
	Message streamUserMsg `json:"message"`
}

type streamUserMsg struct {
	Role    string              `json:"role"`
	Content []streamTextContent `json:"content"`
}

type streamTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// writeStreamInput constructs a Claude Code stream-json user message and writes
// it to the given stdin file descriptor, serialized under the provided mutex.
// This replaces the former base.Conn.SendUserMessage method.
func writeStreamInput(stdin *os.File, mu *sync.Mutex, content string) error {
	mu.Lock()
	defer mu.Unlock()

	msg := streamUserMessage{
		Type: "user",
		Message: streamUserMsg{
			Role: "user",
			Content: []streamTextContent{
				{Type: "text", Text: content},
			},
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("claudecode: marshal user message: %w", err)
	}
	data = append(data, '\n')

	err = base.WriteAll(int(stdin.Fd()), data)
	if err != nil {
		if base.IsDeadProcessError(err) {
			return &worker.WorkerError{Kind: worker.ErrKindUnavailable, Message: "claudecode: worker process is not running or stdin is closed", Cause: err}
		}
		return fmt.Errorf("claudecode: write user message: %w", err)
	}

	return nil
}
