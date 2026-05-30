package acp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteMessage(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mustMarshal(1),
		Method:  "initialize",
		Params:  mustMarshal(map[string]any{"protocolVersion": 1}),
	}
	err := WriteMessage(&buf, req)
	require.NoError(t, err)

	line, err := buf.ReadString('\n')
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(line, "\n"), "must end with newline")

	var parsed JSONRPCRequest
	require.NoError(t, json.Unmarshal([]byte(line), &parsed))
	require.Equal(t, "2.0", parsed.JSONRPC)
	require.Equal(t, "initialize", parsed.Method)
}

func TestReadMessage_Response(t *testing.T) {
	t.Parallel()

	input := `{"jsonrpc":"2.0","id":1,"result":{"sessionId":"abc-123"}}` + "\n"
	r := bufio.NewReader(strings.NewReader(input))

	msg, err := ReadMessage(r)
	require.NoError(t, err)

	resp, ok := msg.(*JSONRPCResponse)
	require.True(t, ok, "expected JSONRPCResponse")
	require.Equal(t, "2.0", resp.JSONRPC)
	require.Equal(t, `1`, string(resp.ID))

	var result struct {
		SessionID string `json:"sessionId"`
	}
	require.NoError(t, json.Unmarshal(resp.Result, &result))
	require.Equal(t, "abc-123", result.SessionID)
}

func TestReadMessage_Notification(t *testing.T) {
	t.Parallel()

	input := `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s1"}}` + "\n"
	r := bufio.NewReader(strings.NewReader(input))

	msg, err := ReadMessage(r)
	require.NoError(t, err)

	notif, ok := msg.(*JSONRPCNotification)
	require.True(t, ok, "expected JSONRPCNotification")
	require.Equal(t, "session/update", notif.Method)
}

func TestReadMessage_Request(t *testing.T) {
	t.Parallel()

	input := `{"jsonrpc":"2.0","id":5,"method":"session/request_permission","params":{}}` + "\n"
	r := bufio.NewReader(strings.NewReader(input))

	msg, err := ReadMessage(r)
	require.NoError(t, err)

	req, ok := msg.(*JSONRPCRequest)
	require.True(t, ok, "expected JSONRPCRequest")
	require.Equal(t, "session/request_permission", req.Method)
}

func TestReadMessage_ErrorResponse(t *testing.T) {
	t.Parallel()

	input := `{"jsonrpc":"2.0","id":2,"error":{"code":-32600,"message":"Invalid params"}}` + "\n"
	r := bufio.NewReader(strings.NewReader(input))

	msg, err := ReadMessage(r)
	require.NoError(t, err)

	resp, ok := msg.(*JSONRPCResponse)
	require.True(t, ok)
	require.NotNil(t, resp.Error)
	require.Equal(t, -32600, resp.Error.Code)
	require.Equal(t, "Invalid params", resp.Error.Message)
}

func TestReadMessage_BlankLine(t *testing.T) {
	t.Parallel()

	input := "\n\n" + `{"jsonrpc":"2.0","id":1,"result":{}}` + "\n"
	r := bufio.NewReader(strings.NewReader(input))

	// Skip blank lines.
	msg, err := ReadMessage(r)
	require.NoError(t, err)
	require.Nil(t, msg)

	msg, err = ReadMessage(r)
	require.NoError(t, err)
	require.Nil(t, msg)

	msg, err = ReadMessage(r)
	require.NoError(t, err)
	_, ok := msg.(*JSONRPCResponse)
	require.True(t, ok)
}

func TestRoundtrip(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	orig := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mustMarshal(42),
		Method:  "session/prompt",
		Params:  mustMarshal(map[string]any{"sessionId": "s1"}),
	}
	require.NoError(t, WriteMessage(&buf, orig))

	msg, err := ReadMessage(bufio.NewReader(&buf))
	require.NoError(t, err)

	req, ok := msg.(*JSONRPCRequest)
	require.True(t, ok)
	require.Equal(t, "session/prompt", req.Method)
	require.Equal(t, `42`, string(req.ID))
}
