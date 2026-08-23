package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/skills"
	"github.com/hrygo/hotplex/internal/skills/builtin"
)

func newTestAdminAPIWithSkills(t *testing.T) (*AdminAPI, *skills.Locator) {
	loc := skills.NewLocator(slog.Default(), time.Minute)
	t.Cleanup(func() { loc.Close() })
	api := &AdminAPI{
		log:           slog.Default(),
		skillsLocator: loc,
	}
	return api, loc
}

func requestWithAdminContext(req *http.Request, scopes ...string) *http.Request {
	ctx := context.WithValue(req.Context(), scopeContextKey{}, scopes)
	return req.WithContext(ctx)
}

func TestAdminAPI_HandleListSkills_PaginationAndSearch(t *testing.T) {
	api, loc := newTestAdminAPIWithSkills(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create 15 skills in global managed dir
	for i := 1; i <= 15; i++ {
		name := fmt.Sprintf("skill-%02d", i)
		body := fmt.Sprintf("---\nname: %s\ndescription: Skill description %d\n---\n# %s", name, i, name)
		_, err := loc.CreateText(context.Background(), skills.ScopeGlobal, home, "", name, body, false)
		require.NoError(t, err)
	}

	// 1. Default page=1, page_size=10
	req := httptest.NewRequest("GET", "/admin/api/skills", nil)
	req = requestWithAdminContext(req, ScopeAdminRead)
	rr := httptest.NewRecorder()
	api.HandleListSkills(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var res map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &res))
	require.Equal(t, float64(15), res["total"])
	require.Equal(t, float64(1), res["page"])
	require.Equal(t, float64(10), res["page_size"])

	skillsList, ok := res["skills"].([]any)
	require.True(t, ok)
	require.NotNil(t, skillsList)
	require.Len(t, skillsList, 10)
	require.NotContains(t, res, "tools")

	// 2. Page 2, page_size=10 -> 5 skills
	req2 := httptest.NewRequest("GET", "/admin/api/skills?page=2&page_size=10", nil)
	req2 = requestWithAdminContext(req2, ScopeAdminRead)
	rr2 := httptest.NewRecorder()
	api.HandleListSkills(rr2, req2)

	require.Equal(t, http.StatusOK, rr2.Code)
	var res2 map[string]any
	require.NoError(t, json.Unmarshal(rr2.Body.Bytes(), &res2))
	skillsList2 := res2["skills"].([]any)
	require.Len(t, skillsList2, 5)

	// 3. Search filtering
	req3 := httptest.NewRequest("GET", "/admin/api/skills?q=skill-05", nil)
	req3 = requestWithAdminContext(req3, ScopeAdminRead)
	rr3 := httptest.NewRecorder()
	api.HandleListSkills(rr3, req3)

	require.Equal(t, http.StatusOK, rr3.Code)
	var res3 map[string]any
	require.NoError(t, json.Unmarshal(rr3.Body.Bytes(), &res3))
	require.Equal(t, float64(1), res3["total"])
	skillsList3 := res3["skills"].([]any)
	require.Len(t, skillsList3, 1)
	firstSkill := skillsList3[0].(map[string]any)
	require.Equal(t, "skill-05", firstSkill["name"])
}

func TestAdminAPI_HandleUpdateSkill(t *testing.T) {
	api, loc := newTestAdminAPIWithSkills(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create initial skill
	initialBody := "---\nname: my-skill\ndescription: v1\n---\n# V1"
	_, err := loc.CreateText(context.Background(), skills.ScopeGlobal, home, "", "my-skill", initialBody, false)
	require.NoError(t, err)

	// Update skill
	updatedBody := "---\nname: my-skill\ndescription: v2 updated\n---\n# V2"
	jsonBody, _ := json.Marshal(map[string]string{"body": updatedBody})
	req := httptest.NewRequest("PUT", "/admin/api/skills/my-skill", bytes.NewReader(jsonBody))
	req.SetPathValue("name", "my-skill")
	req = requestWithAdminContext(req, ScopeAdminWrite)
	rr := httptest.NewRecorder()
	api.HandleUpdateSkill(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var res map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &res))
	require.Equal(t, "my-skill", res["name"])
	require.Equal(t, "v2 updated", res["description"])
	require.Equal(t, updatedBody, res["body"])
}

func TestAdminAPI_HandleInstallSkill_JSONText(t *testing.T) {
	api, _ := newTestAdminAPIWithSkills(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	body := "---\nname: json-created-skill\ndescription: created via json\n---\n# Skill"
	payload, _ := json.Marshal(map[string]any{
		"name": "json-created-skill",
		"body": body,
	})

	req := httptest.NewRequest("POST", "/admin/api/skills", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req = requestWithAdminContext(req, ScopeAdminWrite)
	rr := httptest.NewRecorder()
	api.HandleInstallSkill(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var res skillInstallResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &res))
	require.Equal(t, "json-created-skill", res.Name)
	require.Equal(t, "created via json", res.Description)
}

func TestAdminAPI_HandleDeleteSkill(t *testing.T) {
	api, loc := newTestAdminAPIWithSkills(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	body := "---\nname: to-del\ndescription: to delete\n---\n# Skill"
	_, err := loc.CreateText(context.Background(), skills.ScopeGlobal, home, "", "to-del", body, false)
	require.NoError(t, err)

	req := httptest.NewRequest("DELETE", "/admin/api/skills/to-del", nil)
	req.SetPathValue("name", "to-del")
	req = requestWithAdminContext(req, ScopeAdminWrite)
	rr := httptest.NewRecorder()
	api.HandleDeleteSkill(rr, req)

	require.Equal(t, http.StatusNoContent, rr.Code)

	// Verify deleted
	_, err = loc.Read(context.Background(), skills.ScopeGlobal, home, "to-del")
	require.ErrorIs(t, err, skills.ErrSkillNotFound)
}

func TestAdminAPI_SkillScopes(t *testing.T) {
	t.Parallel()
	api, _ := newTestAdminAPIWithSkills(t)

	// GET requires ScopeAdminRead
	req := httptest.NewRequest("GET", "/admin/api/skills", nil)
	req = requestWithAdminContext(req) // no scopes
	rr := httptest.NewRecorder()
	api.HandleListSkills(rr, req)
	require.Equal(t, http.StatusForbidden, rr.Code)

	// PUT requires ScopeAdminWrite
	reqPut := httptest.NewRequest("PUT", "/admin/api/skills/x", nil)
	reqPut = requestWithAdminContext(reqPut, ScopeAdminRead) // read only
	rrPut := httptest.NewRecorder()
	api.HandleUpdateSkill(rrPut, reqPut)
	require.Equal(t, http.StatusForbidden, rrPut.Code)
}

func TestAdminListUserSkillShadowsBuiltinSameName(t *testing.T) {
	api, loc := newTestAdminAPIWithSkills(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	api.SetBuiltinSkillsCatalog(builtin.NewPublicCatalog(registry))

	body := "---\nname: hotplex-cli\ndescription: user override\n---\n# User\n"
	_, err = loc.CreateText(context.Background(), skills.ScopeGlobal, home, "", "hotplex-cli", body, false)
	require.NoError(t, err)

	req := requestWithAdminContext(httptest.NewRequest("GET", "/admin/api/skills", nil), ScopeAdminRead)
	rr := httptest.NewRecorder()
	api.HandleListSkills(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	var res struct {
		Skills []skills.Skill `json:"skills"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &res))
	var matches []skills.Skill
	for _, skill := range res.Skills {
		if skill.Name == "hotplex-cli" {
			matches = append(matches, skill)
		}
	}
	require.Len(t, matches, 1)
	require.Equal(t, "user override", matches[0].Description)
	require.False(t, matches[0].Builtin)
}

func TestAdminBuiltinDetailIsReadableButNotMutable(t *testing.T) {
	api, _ := newTestAdminAPIWithSkills(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	api.SetBuiltinSkillsCatalog(builtin.NewPublicCatalog(registry))

	getReq := httptest.NewRequest("GET", "/admin/api/skills/hotplex-cli", nil)
	getReq.SetPathValue("name", "hotplex-cli")
	getReq = requestWithAdminContext(getReq, ScopeAdminRead)
	getRR := httptest.NewRecorder()
	api.HandleGetSkill(getRR, getReq)
	require.Equal(t, http.StatusOK, getRR.Code)
	require.Contains(t, getRR.Body.String(), `"builtin":true`)

	updateReq := httptest.NewRequest("PUT", "/admin/api/skills/hotplex-cli", bytes.NewBufferString(`{"body":"---\nname: hotplex-cli\ndescription: changed\n---\n"}`))
	updateReq.SetPathValue("name", "hotplex-cli")
	updateReq = requestWithAdminContext(updateReq, ScopeAdminWrite)
	updateRR := httptest.NewRecorder()
	api.HandleUpdateSkill(updateRR, updateReq)
	require.Equal(t, http.StatusConflict, updateRR.Code)
	require.Contains(t, updateRR.Body.String(), "SKILL_BUILTIN_READONLY")

	deleteReq := httptest.NewRequest("DELETE", "/admin/api/skills/hotplex-cli", nil)
	deleteReq.SetPathValue("name", "hotplex-cli")
	deleteReq = requestWithAdminContext(deleteReq, ScopeAdminWrite)
	deleteRR := httptest.NewRecorder()
	api.HandleDeleteSkill(deleteRR, deleteReq)
	require.Equal(t, http.StatusConflict, deleteRR.Code)
	require.Contains(t, deleteRR.Body.String(), "SKILL_BUILTIN_READONLY")

	// Creating a same-name user override remains an ordinary install path.
	installBody := `{"name":"hotplex-cli","body":"---\nname: hotplex-cli\ndescription: override\n---\n# override"}`
	installReq := httptest.NewRequest("POST", "/admin/api/skills", bytes.NewBufferString(installBody))
	installReq.Header.Set("Content-Type", "application/json")
	installReq = requestWithAdminContext(installReq, ScopeAdminWrite)
	installRR := httptest.NewRecorder()
	api.HandleInstallSkill(installRR, installReq)
	require.Equal(t, http.StatusOK, installRR.Code)

	updateReq = httptest.NewRequest("PUT", "/admin/api/skills/hotplex-cli", bytes.NewBufferString(`{"body":"---\nname: hotplex-cli\ndescription: updated override\n---\n"}`))
	updateReq.SetPathValue("name", "hotplex-cli")
	updateReq = requestWithAdminContext(updateReq, ScopeAdminWrite)
	updateRR = httptest.NewRecorder()
	api.HandleUpdateSkill(updateRR, updateReq)
	require.Equal(t, http.StatusOK, updateRR.Code)

	deleteReq = httptest.NewRequest("DELETE", "/admin/api/skills/hotplex-cli", nil)
	deleteReq.SetPathValue("name", "hotplex-cli")
	deleteReq = requestWithAdminContext(deleteReq, ScopeAdminWrite)
	deleteRR = httptest.NewRecorder()
	api.HandleDeleteSkill(deleteRR, deleteReq)
	require.Equal(t, http.StatusNoContent, deleteRR.Code)
}

func TestAdminExternalSkillShadowsBuiltinOnGet(t *testing.T) {
	api, _ := newTestAdminAPIWithSkills(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	api.SetBuiltinSkillsCatalog(builtin.NewPublicCatalog(registry))

	externalPath := filepath.Join(home, ".claude", "skills", "hotplex-cli")
	require.NoError(t, os.MkdirAll(externalPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(externalPath, "SKILL.md"), []byte("---\nname: hotplex-cli\ndescription: external override\n---\n# external\n"), 0o644))

	getReq := httptest.NewRequest("GET", "/admin/api/skills/hotplex-cli", nil)
	getReq.SetPathValue("name", "hotplex-cli")
	getReq = requestWithAdminContext(getReq, ScopeAdminRead)
	getRR := httptest.NewRecorder()
	api.HandleGetSkill(getRR, getReq)
	require.Equal(t, http.StatusNotFound, getRR.Code)
	require.NotContains(t, getRR.Body.String(), `"builtin":true`)
}
