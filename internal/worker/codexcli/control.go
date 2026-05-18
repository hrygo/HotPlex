package codexcli

import (
	"context"
	"fmt"
)

type ControlHandler struct{}

func NewControlHandler() *ControlHandler {
	return &ControlHandler{}
}

func (c *ControlHandler) SendControlRequest(ctx context.Context, subtype string, body map[string]any) (map[string]any, error) {
	return nil, fmt.Errorf("codexcli: control requests not supported in one-shot mode")
}
