package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// mockBotConfigProvider implements BotConfigProvider for AgentConfig handler
// tests and records both bot-level and platform-level calls.
type mockBotConfigProvider struct {
	getFn      func(ctx context.Context, platform string, file AgentConfigFileName) (*AgentConfigFile, error)
	writeFn    func(ctx context.Context, platform string, file AgentConfigFileName, content string) error
	botGetFn   func(ctx context.Context, botName string, file AgentConfigFileName) (*AgentConfigFile, error)
	botWriteFn func(ctx context.Context, botName string, file AgentConfigFileName, content string) error

	// Recorded call args for assertion.
	gotPlatform string
	gotFile     AgentConfigFileName
	gotContent  string
}

func (m *mockBotConfigProvider) GetBotConfig(context.Context, string) (*BotConfigEntry, error) {
	return nil, nil
}
func (m *mockBotConfigProvider) ListBotConfigs(context.Context) ([]BotConfigEntry, error) {
	return nil, nil
}
func (m *mockBotConfigProvider) GetAgentConfigFile(ctx context.Context, botName string, file AgentConfigFileName) (*AgentConfigFile, error) {
	m.gotPlatform = botName
	m.gotFile = file
	if m.botGetFn != nil {
		return m.botGetFn(ctx, botName, file)
	}
	return &AgentConfigFile{Content: "bot config", Source: "bot", Size: 10, File: string(file)}, nil
}
func (m *mockBotConfigProvider) GetSystemPromptPreview(context.Context, string) (string, error) {
	return "", nil
}
func (m *mockBotConfigProvider) UpdateBotConfig(context.Context, string, *BotConfigAttrs) error {
	return nil
}
func (m *mockBotConfigProvider) CreateBot(context.Context, string, *BotConfigAttrs) error {
	return nil
}
func (m *mockBotConfigProvider) DeleteBot(context.Context, string) error { return nil }
func (m *mockBotConfigProvider) WriteAgentConfigFile(ctx context.Context, botName string, file AgentConfigFileName, content string) error {
	m.gotPlatform = botName
	m.gotFile = file
	m.gotContent = content
	if m.botWriteFn != nil {
		return m.botWriteFn(ctx, botName, file, content)
	}
	return nil
}

func (m *mockBotConfigProvider) GetPlatformAgentConfigFile(ctx context.Context, platform string, file AgentConfigFileName) (*AgentConfigFile, error) {
	m.gotPlatform = platform
	m.gotFile = file
	if m.getFn != nil {
		return m.getFn(ctx, platform, file)
	}
	return &AgentConfigFile{Content: "channel default", Source: "platform", Size: 15, File: string(file)}, nil
}

func (m *mockBotConfigProvider) WritePlatformAgentConfigFile(ctx context.Context, platform string, file AgentConfigFileName, content string) error {
	m.gotPlatform = platform
	m.gotFile = file
	m.gotContent = content
	if m.writeFn != nil {
		return m.writeFn(ctx, platform, file, content)
	}
	return nil
}

// newPlatformRequest builds a request preloaded with platform/file path values
// and an optional scope, mirroring how ServeMux would populate PathValue.
func newPlatformRequest(t *testing.T, method, platform, file, scope string, body []byte) (*httptest.ResponseRecorder, *http.Request) {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	var r *http.Request
	if rdr != nil {
		r = httptest.NewRequest(method, "/admin/bots/platform/"+platform+"/config/"+file, rdr)
	} else {
		r = httptest.NewRequest(method, "/admin/bots/platform/"+platform+"/config/"+file, nil)
	}
	if scope != "" {
		r = withScope(r, scope)
	}
	r.SetPathValue("platform", platform)
	r.SetPathValue("file", file)
	return httptest.NewRecorder(), r
}

func newBotAgentConfigRequest(t *testing.T, method, botName, file, scope string, body []byte) (*httptest.ResponseRecorder, *http.Request) {
	t.Helper()
	var r *http.Request
	path := "/admin/bots/" + botName + "/config/" + file
	if body == nil {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, bytes.NewReader(body))
	}
	if scope != "" {
		r = withScope(r, scope)
	}
	r.SetPathValue("name", botName)
	r.SetPathValue("file", file)
	return httptest.NewRecorder(), r
}

func TestAgentConfigSummary_SerializesCanonicalToolsField(t *testing.T) {
	t.Parallel()

	entry := BotConfigEntry{
		AgentConfigs: &AgentConfigSummary{
			Tools: &AgentConfigMeta{Source: "bot", Size: 12},
		},
	}
	data, err := json.Marshal(entry)
	require.NoError(t, err)
	require.JSONEq(t, `{"name":"","platform":"","bot_id":"","status":"","agent_configs":{"tools":{"source":"bot","size":12}}}`, string(data))
	require.NotContains(t, string(data), `"skills"`)
}

func TestHandleAgentConfigFile_AcceptsCanonicalTools(t *testing.T) {
	t.Parallel()
	prov := &mockBotConfigProvider{}
	api := newTestAPI(func(d *Deps) { d.BotConfig = prov })

	wGet, rGet := newBotAgentConfigRequest(t, http.MethodGet, "helper", "TOOLS.md", ScopeAdminRead, nil)
	api.HandleGetAgentConfigFile(wGet, rGet)
	require.Equal(t, http.StatusOK, wGet.Code, "body=%q", wGet.Body.String())
	require.Equal(t, AgentConfigTools, prov.gotFile)

	body := []byte(`{"content":"tool guidance"}`)
	wPut, rPut := newBotAgentConfigRequest(t, http.MethodPut, "helper", "TOOLS.md", ScopeAdminWrite, body)
	api.HandleWriteAgentConfigFile(wPut, rPut)
	require.Equal(t, http.StatusNoContent, wPut.Code, "body=%q", wPut.Body.String())
	require.Equal(t, AgentConfigTools, prov.gotFile)
	require.Equal(t, "tool guidance", prov.gotContent)
}

func TestHandleAgentConfigFile_LegacySkillsIsReadOnly(t *testing.T) {
	t.Parallel()
	prov := &mockBotConfigProvider{}
	api := newTestAPI(func(d *Deps) { d.BotConfig = prov })

	wGet, rGet := newBotAgentConfigRequest(t, http.MethodGet, "helper", "SKILLS.md", ScopeAdminRead, nil)
	api.HandleGetAgentConfigFile(wGet, rGet)
	require.Equal(t, http.StatusOK, wGet.Code, "body=%q", wGet.Body.String())
	require.Equal(t, AgentConfigLegacySkills, prov.gotFile)

	wPut, rPut := newBotAgentConfigRequest(t, http.MethodPut, "helper", "SKILLS.md", ScopeAdminWrite, []byte(`{"content":"legacy"}`))
	api.HandleWriteAgentConfigFile(wPut, rPut)
	require.Equal(t, http.StatusBadRequest, wPut.Code)
	require.Empty(t, prov.gotContent, "provider must not be consulted for legacy writes")
}

func TestHandleGetPlatformAgentConfigFile_Success(t *testing.T) {
	t.Parallel()
	prov := &mockBotConfigProvider{}
	api := newTestAPI(func(d *Deps) { d.BotConfig = prov })

	w, r := newPlatformRequest(t, http.MethodGet, "webchat", "SOUL.md", ScopeAdminRead, nil)
	api.HandleGetPlatformAgentConfigFile(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "webchat", prov.gotPlatform)
	require.Equal(t, AgentConfigSoul, prov.gotFile)

	var got AgentConfigFile
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, "channel default", got.Content)
	require.Equal(t, "platform", got.Source)
	require.Equal(t, "SOUL.md", got.File)
}

func TestHandleWritePlatformAgentConfigFile_Success(t *testing.T) {
	t.Parallel()
	prov := &mockBotConfigProvider{}
	api := newTestAPI(func(d *Deps) { d.BotConfig = prov })

	body, err := json.Marshal(map[string]string{"content": "new webchat soul"})
	require.NoError(t, err)
	w, r := newPlatformRequest(t, http.MethodPut, "webchat", "SOUL.md", ScopeAdminWrite, body)
	api.HandleWritePlatformAgentConfigFile(w, r)

	require.Equal(t, http.StatusNoContent, w.Code)
	require.Equal(t, "new webchat soul", prov.gotContent)
	require.Equal(t, "webchat", prov.gotPlatform)
	require.Equal(t, AgentConfigSoul, prov.gotFile)
}

func TestHandlePlatformAgentConfigFile_ToolsCompatibility(t *testing.T) {
	t.Parallel()
	prov := &mockBotConfigProvider{}
	api := newTestAPI(func(d *Deps) { d.BotConfig = prov })

	wGet, rGet := newPlatformRequest(t, http.MethodGet, "webchat", "SKILLS.md", ScopeAdminRead, nil)
	api.HandleGetPlatformAgentConfigFile(wGet, rGet)
	require.Equal(t, http.StatusOK, wGet.Code, "body=%q", wGet.Body.String())
	require.Equal(t, AgentConfigLegacySkills, prov.gotFile)

	body := []byte(`{"content":"webchat tool guidance"}`)
	wPut, rPut := newPlatformRequest(t, http.MethodPut, "webchat", "TOOLS.md", ScopeAdminWrite, body)
	api.HandleWritePlatformAgentConfigFile(wPut, rPut)
	require.Equal(t, http.StatusNoContent, wPut.Code, "body=%q", wPut.Body.String())
	require.Equal(t, AgentConfigTools, prov.gotFile)
	require.Equal(t, "webchat tool guidance", prov.gotContent)
}

// TestHandlePlatformAgentConfigFile_Rejections covers input validation and
// authorization boundaries that must hold before the provider is consulted.
func TestHandlePlatformAgentConfigFile_Rejections(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		method   string
		scope    string
		platform string
		file     string
		want     int
	}{
		{"get invalid platform", http.MethodGet, ScopeAdminRead, "telegram", "SOUL.md", http.StatusBadRequest},
		{"get invalid file traversal", http.MethodGet, ScopeAdminRead, "webchat", "../../etc/passwd", http.StatusBadRequest},
		{"get unknown file", http.MethodGet, ScopeAdminRead, "webchat", "META-COGNITION.md", http.StatusBadRequest},
		{"get no scope", http.MethodGet, "", "webchat", "SOUL.md", http.StatusForbidden},
		{"put invalid platform", http.MethodPut, ScopeAdminWrite, "telegram", "SOUL.md", http.StatusBadRequest},
		{"put invalid file", http.MethodPut, ScopeAdminWrite, "webchat", "README.md", http.StatusBadRequest},
		{"put legacy tools alias", http.MethodPut, ScopeAdminWrite, "webchat", "SKILLS.md", http.StatusBadRequest},
		{"put no scope", http.MethodPut, "", "webchat", "SOUL.md", http.StatusForbidden},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prov := &mockBotConfigProvider{}
			api := newTestAPI(func(d *Deps) { d.BotConfig = prov })

			var body []byte
			if tc.method == http.MethodPut {
				body = []byte(`{"content":"x"}`)
			}
			w, r := newPlatformRequest(t, tc.method, tc.platform, tc.file, tc.scope, body)

			if tc.method == http.MethodGet {
				api.HandleGetPlatformAgentConfigFile(w, r)
			} else {
				api.HandleWritePlatformAgentConfigFile(w, r)
			}

			require.Equal(t, tc.want, w.Code, "body=%q", w.Body.String())
			// Provider must not be reached on rejection.
			require.Empty(t, prov.gotPlatform, "provider should not be consulted on %d", tc.want)
		})
	}
}

// A nil provider surfaces as 503, consistent with the bot-level endpoints.
func TestHandlePlatformAgentConfigFile_NilProvider(t *testing.T) {
	t.Parallel()
	api := newTestAPI() // BotConfig left nil

	wGet, rGet := newPlatformRequest(t, http.MethodGet, "webchat", "SOUL.md", ScopeAdminRead, nil)
	api.HandleGetPlatformAgentConfigFile(wGet, rGet)
	require.Equal(t, http.StatusServiceUnavailable, wGet.Code)

	wPut, rPut := newPlatformRequest(t, http.MethodPut, "webchat", "SOUL.md", ScopeAdminWrite, []byte(`{"content":"x"}`))
	api.HandleWritePlatformAgentConfigFile(wPut, rPut)
	require.Equal(t, http.StatusServiceUnavailable, wPut.Code)
}

// A provider error on write surfaces as 400 (write failed), matching the
// bot-level write handler's contract.
func TestHandleWritePlatformAgentConfigFile_ProviderError(t *testing.T) {
	t.Parallel()
	prov := &mockBotConfigProvider{
		writeFn: func(context.Context, string, AgentConfigFileName, string) error {
			return errors.New("write failed")
		},
	}
	api := newTestAPI(func(d *Deps) { d.BotConfig = prov })

	w, r := newPlatformRequest(t, http.MethodPut, "webchat", "SOUL.md", ScopeAdminWrite, []byte(`{"content":"x"}`))
	api.HandleWritePlatformAgentConfigFile(w, r)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

// TestHandlePlatformAgentConfigFile_AllPlatforms asserts the endpoint serves
// every recognized platform — not only webchat — making the multi-platform
// contract explicit rather than implicit. The IsValidPlatform whitelist accepts
// webchat/slack/feishu; each must round-trip through both verbs. See #796.
func TestHandlePlatformAgentConfigFile_AllPlatforms(t *testing.T) {
	t.Parallel()

	platforms := []string{"webchat", "slack", "feishu"}
	for _, p := range platforms {
		t.Run(p+"_read", func(t *testing.T) {
			t.Parallel()
			prov := &mockBotConfigProvider{}
			api := newTestAPI(func(d *Deps) { d.BotConfig = prov })

			w, r := newPlatformRequest(t, http.MethodGet, p, "AGENTS.md", ScopeAdminRead, nil)
			api.HandleGetPlatformAgentConfigFile(w, r)

			require.Equal(t, http.StatusOK, w.Code)
			require.Equal(t, p, prov.gotPlatform)
			require.Equal(t, AgentConfigAgents, prov.gotFile)
		})
		t.Run(p+"_write", func(t *testing.T) {
			t.Parallel()
			prov := &mockBotConfigProvider{}
			api := newTestAPI(func(d *Deps) { d.BotConfig = prov })

			body, err := json.Marshal(map[string]string{"content": "team default for " + p})
			require.NoError(t, err)
			w, r := newPlatformRequest(t, http.MethodPut, p, "AGENTS.md", ScopeAdminWrite, body)
			api.HandleWritePlatformAgentConfigFile(w, r)

			require.Equal(t, http.StatusNoContent, w.Code)
			require.Equal(t, p, prov.gotPlatform)
			require.Equal(t, AgentConfigAgents, prov.gotFile)
			require.Equal(t, "team default for "+p, prov.gotContent)
		})
	}
}
