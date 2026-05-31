package codexcli

import (
	"context"
	"encoding/json"
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
	case "mcp_status":
		resp, err := sc.manager.ListMCPServerStatus()
		if err != nil {
			return nil, fmt.Errorf("codexcli: mcp_status: %w", err)
		}
		return map[string]any{"status": json.RawMessage(resp)}, nil
	case "mcp_refresh":
		serverName, _ := body["server_name"].(string)
		if err := sc.manager.RefreshMCPServer(serverName); err != nil {
			return nil, fmt.Errorf("codexcli: mcp_refresh: %w", err)
		}
		return map[string]any{"status": "ok"}, nil
	case "mcp_oauth":
		serverName, _ := body["server_name"].(string)
		resp, err := sc.manager.MCPServerOAuthLogin(serverName)
		if err != nil {
			return nil, fmt.Errorf("codexcli: mcp_oauth: %w", err)
		}
		return map[string]any{"oauth_url": string(resp)}, nil
	default:
		return nil, fmt.Errorf("codexcli: unknown control subtype: %s", subtype)
	}
}

func (sc *ServerCommander) Compact(ctx context.Context, _ map[string]any) error {
	_, err := sc.manager.CompactThread(sc.threadID)
	return err
}

func (sc *ServerCommander) Clear(ctx context.Context) error {
	_ = sc.manager.InterruptTurn(sc.threadID)
	return nil
}

func (sc *ServerCommander) Rewind(ctx context.Context, targetID string) error {
	_, err := sc.manager.RollbackThread(sc.threadID, targetID)
	return err
}
