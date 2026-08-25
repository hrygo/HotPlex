package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/messaging"
)

func TestAdapter_SendProactiveMessage_ToOpenID(t *testing.T) {
	t.Parallel()

	var receiveIDType string
	var body struct {
		ReceiveID string `json:"receive_id"`
	}
	client := lark.NewClient("test-app", "test-secret",
		lark.WithHttpClient(mediaHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/open-apis/auth/v3/tenant_access_token/internal":
				return feishuTestJSONResponse(req, `{"code":0,"tenant_access_token":"test-token","expire":7200}`), nil
			case "/open-apis/im/v1/messages":
				receiveIDType = req.URL.Query().Get("receive_id_type")
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					return nil, err
				}
				return feishuTestJSONResponse(req, `{"code":0,"msg":"success","data":{"message_id":"om_test"}}`), nil
			default:
				return nil, fmt.Errorf("unexpected SDK request path %q", req.URL.Path)
			}
		})),
	)
	adapter := &Adapter{
		BaseAdapter: messaging.BaseAdapter[*FeishuConn]{
			PlatformAdapter: messaging.PlatformAdapter{
				Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
			},
		},
		larkClient: client,
	}

	err := adapter.SendProactiveMessage(context.Background(), "Gateway ready", map[string]string{
		"open_id": "ou_operator",
	})

	require.NoError(t, err)
	require.Equal(t, "open_id", receiveIDType)
	require.Equal(t, "ou_operator", body.ReceiveID)
}

func feishuTestJSONResponse(req *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}
