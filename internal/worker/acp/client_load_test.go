package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLoadSession_EchoesRequestSessionID verifies that LoadSession does NOT
// require the agent to return a sessionId in the response. Per ACP spec,
// LoadSessionResponse has no sessionId field — the loaded session keeps the ID
// the client passed in. HotPlex must echo it back so callers (worker.go) get a
// non-empty SessionResult.SessionID.
//
// Regression for issue: "agent returned empty sessionId" on every session/load,
// causing HISTORY_LOST fallback + UNIQUE constraint collisions on resume.
func TestLoadSession_EchoesRequestSessionID(t *testing.T) {
	t.Parallel()

	const requestSessionID = "3ce7059d-c5bf-4661-9229-471ee0f80847"

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	t.Cleanup(func() {
		_ = stdinR.Close()
		_ = stdinW.Close()
		_ = stdoutR.Close()
		_ = stdoutW.Close()
	})

	c := NewACPClient(stdinW, bufio.NewReader(stdoutR), slog.Default())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go c.readLoop(ctx)

	var wg sync.WaitGroup
	var result *SessionResult
	var callErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		result, callErr = c.LoadSession(ctx, requestSessionID, "/tmp", nil)
	}()

	// Read the outgoing session/load request.
	line, err := bufio.NewReader(stdinR).ReadString('\n')
	require.NoError(t, err)

	var req JSONRPCRequest
	require.NoError(t, json.Unmarshal([]byte(line[:len(line)-1]), &req))
	require.Equal(t, "session/load", req.Method)

	// Respond with a LoadSessionResponse that has NO sessionId field (per ACP spec).
	resp := `{"jsonrpc":"2.0","id":` + string(req.ID) + `,"result":{"models":{"default":"glm-5.2"},"modes":[{"id":"code","name":"Code"}]}}` + "\n"
	_, err = stdoutW.Write([]byte(resp))
	require.NoError(t, err)

	wg.Wait()

	require.NoError(t, callErr)
	require.NotNil(t, result)
	require.Equal(t, requestSessionID, result.SessionID,
		"LoadSession must echo the request sessionId back")

	_ = stdoutW.Close()
	<-c.Done()
}

// TestResumeSession_EchoesRequestSessionID verifies the same fix for
// session/resume (ResumeSessionResponse also has no sessionId field).
func TestResumeSession_EchoesRequestSessionID(t *testing.T) {
	t.Parallel()

	const requestSessionID = "abc-123-resume"

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	t.Cleanup(func() {
		_ = stdinR.Close()
		_ = stdinW.Close()
		_ = stdoutR.Close()
		_ = stdoutW.Close()
	})

	c := NewACPClient(stdinW, bufio.NewReader(stdoutR), slog.Default())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go c.readLoop(ctx)

	var wg sync.WaitGroup
	var result *SessionResult
	var callErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		result, callErr = c.ResumeSession(ctx, requestSessionID, "/tmp", nil)
	}()

	line, err := bufio.NewReader(stdinR).ReadString('\n')
	require.NoError(t, err)

	var req JSONRPCRequest
	require.NoError(t, json.Unmarshal([]byte(line[:len(line)-1]), &req))
	require.Equal(t, "session/resume", req.Method)

	resp := `{"jsonrpc":"2.0","id":` + string(req.ID) + `,"result":{"models":{"default":"glm-5.2"}}}` + "\n"
	_, err = stdoutW.Write([]byte(resp))
	require.NoError(t, err)

	wg.Wait()

	require.NoError(t, callErr)
	require.NotNil(t, result)
	require.Equal(t, requestSessionID, result.SessionID)

	_ = stdoutW.Close()
	<-c.Done()
}
