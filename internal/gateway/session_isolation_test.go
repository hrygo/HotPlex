package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/session"
)

// TestAuthorizeSession_WorkspaceForbidden: a session bound to another user's
// workspace is rejected even if UserID matches (defense-in-depth, spec §9.3).
func TestAuthorizeSession_WorkspaceForbidden(t *testing.T) {
	t.Parallel()
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	ws := new(mockAPIWorkspace)
	ws.On("GetWorkspaceByID", mock.Anything, "ws-x").Return(&session.Workspace{
		ID: "ws-x", OwnerUserID: "someone-else", WorkDir: "/tmp/p", Status: "active",
	}, nil)
	api := newTestAPIWithWorkspace(t, sm, bridge, ws)

	// Session owned by "anonymous" (passes UserID check) but bound to another's workspace.
	sm.On("Get", mock.Anything).Return(&session.SessionInfo{
		ID: "sess-1", UserID: "anonymous", WorkspaceID: "ws-x",
	}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/sess-1", nil)
	req.SetPathValue("id", "sess-1")
	req.Header.Set("X-API-Key", "test-key")
	w := httptest.NewRecorder()
	api.GetSession(w, req)

	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "WORKSPACE_FORBIDDEN")
}

// TestAuthorizeSession_WorkspaceOK: a session bound to the caller's own workspace passes.
func TestAuthorizeSession_WorkspaceOK(t *testing.T) {
	t.Parallel()
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	ws := new(mockAPIWorkspace)
	ws.On("GetWorkspaceByID", mock.Anything, "ws-mine").Return(&session.Workspace{
		ID: "ws-mine", OwnerUserID: "anonymous", WorkDir: "/tmp/p", Status: "active",
	}, nil)
	api := newTestAPIWithWorkspace(t, sm, bridge, ws)

	sm.On("Get", mock.Anything).Return(&session.SessionInfo{
		ID: "sess-1", UserID: "anonymous", WorkspaceID: "ws-mine",
	}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/sess-1", nil)
	req.SetPathValue("id", "sess-1")
	req.Header.Set("X-API-Key", "test-key")
	w := httptest.NewRecorder()
	api.GetSession(w, req)

	require.Equal(t, http.StatusOK, w.Code)
}

// TestAuthorizeSession_PlatformSessionSkipsWorkspaceCheck: a platform/cron session
// (workspace_id empty) bypasses the workspace check — backward compatible.
func TestAuthorizeSession_PlatformSessionSkipsWorkspaceCheck(t *testing.T) {
	t.Parallel()
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	// wsStore nil simulates platform path (no workspace binding expected).
	api := newTestAPI(t, sm, bridge)

	sm.On("Get", mock.Anything).Return(&session.SessionInfo{
		ID: "sess-1", UserID: "anonymous", WorkspaceID: "", // platform/cron session
	}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/sess-1", nil)
	req.SetPathValue("id", "sess-1")
	req.Header.Set("X-API-Key", "test-key")
	w := httptest.NewRecorder()
	api.GetSession(w, req)

	require.Equal(t, http.StatusOK, w.Code)
}

// TestListSessions_WorkspaceFilterForbidden: listing with another user's workspace_id
// is rejected before the List call (spec §9.3).
func TestListSessions_WorkspaceFilterForbidden(t *testing.T) {
	t.Parallel()
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	ws := new(mockAPIWorkspace)
	ws.On("GetWorkspaceByID", mock.Anything, "ws-other").Return(&session.Workspace{
		ID: "ws-other", OwnerUserID: "someone-else", WorkDir: "/tmp/p", Status: "active",
	}, nil)
	api := newTestAPIWithWorkspace(t, sm, bridge, ws)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions?workspace_id=ws-other", nil)
	req.Header.Set("X-API-Key", "test-key")
	w := httptest.NewRecorder()
	api.ListSessions(w, req)

	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "WORKSPACE_FORBIDDEN")
	sm.AssertNotCalled(t, "List") // ownership fails before listing
}
