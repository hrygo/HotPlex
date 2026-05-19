package codexcli

import (
	"context"
	"fmt"
)

type ServerCommander struct {
	manager  *CodexAppServerManager
	threadID string
}

func NewServerCommander(manager *CodexAppServerManager, threadID string) *ServerCommander {
	return &ServerCommander{manager: manager, threadID: threadID}
}

func (sc *ServerCommander) SendControlRequest(ctx context.Context, subtype string, body map[string]any) (map[string]any, error) {
	switch subtype {
	case "set_model":
		return nil, fmt.Errorf("codexcli: set_model not supported")
	case "get_context_usage":
		resp, err := sc.manager.Call("thread/read", map[string]string{
			"threadId": sc.threadID,
		})
		if err != nil {
			return nil, fmt.Errorf("codexcli: get_context_usage: %w", err)
		}
		result := map[string]any{
			"raw": string(resp),
		}
		return result, nil
	default:
		return nil, fmt.Errorf("codexcli: unknown control subtype: %s", subtype)
	}
}
