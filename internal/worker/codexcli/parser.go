package codexcli

import (
	"encoding/json"
	"fmt"
)

type Parser struct{}

func NewParser() *Parser {
	return &Parser{}
}

func (p *Parser) ParseNotification(line string) (method string, params json.RawMessage, err error) {
	var frame struct {
		ID     *int64          `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal([]byte(line), &frame); err != nil {
		return "", nil, fmt.Errorf("parse jsonrpc: %w", err)
	}
	if frame.Method == "" {
		return "", nil, fmt.Errorf("parse jsonrpc: missing method")
	}
	return frame.Method, frame.Params, nil
}

func (p *Parser) ParseResponse(line string) (id int64, result json.RawMessage, rpcErr *JSONRPCError, err error) {
	var frame struct {
		ID     int64           `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *JSONRPCError   `json:"error"`
	}
	if err := json.Unmarshal([]byte(line), &frame); err != nil {
		return 0, nil, nil, fmt.Errorf("parse jsonrpc response: %w", err)
	}
	return frame.ID, frame.Result, frame.Error, nil
}
