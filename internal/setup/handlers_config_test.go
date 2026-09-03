package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/auth"
	"github.com/fastclaw-ai/fastclaw/internal/config"
	"github.com/fastclaw-ai/fastclaw/internal/store"
)

// Round-trip regression tests for the masked-secret write-back path of
// POST /api/config: the dashboard GETs masked values ("sk-1****abcd")
// and POSTs them back verbatim on save. mergeSkillEntry must resolve
// those placeholders to the stored originals — before it was wired in,
// a no-op save replaced real keys with literal "****" strings.

func TestUpdateConfigMaskedGlobalSkillEntryKeepsStoredSecret(t *testing.T) {
	ctx := context.Background()
	s, resolver, adminUser, _ := newAuthTestServer(t, ctx)

	post := func(body map[string]any) *httptest.ResponseRecorder {
		t.Helper()
		rr := httptest.NewRecorder()
		s.authMiddleware(s.handleUpdateConfig)(rr, configTestRequest(t, ctx, resolver, http.MethodPost, "/api/config", adminUser.ID, body))
		if rr.Code != http.StatusOK {
			t.Fatalf("POST /api/config status = %d, body = %s", rr.Code, rr.Body.String())
		}
		return rr
	}
	storedEntry := func() config.SkillEntryCfg {
		t.Helper()
		rec, err := s.dataStore.GetConfigByName(ctx, store.KindSetting, "", "", "skills.entries")
		if err != nil || rec == nil {
			t.Fatalf("GetConfigByName: rec=%v err=%v", rec, err)
		}
		blob, _ := json.Marshal(rec.Data)
		var entries map[string]config.SkillEntryCfg
		if err := json.Unmarshal(blob, &entries); err != nil {
			t.Fatalf("decode entries: %v", err)
		}
		return entries["my-skill"]
	}

	post(map[string]any{"skills": map[string]any{"entries": map[string]any{
		"my-skill": map[string]any{
			"enabled": true,
			"apiKey":  "real-secret-key-123456",
			"env":     map[string]any{"NORMAL_VAR": "plain", "API_TOKEN": "real-env-secret-987654"},
		},
	}}})
	if got := storedEntry(); got.APIKey != "real-secret-key-123456" || got.Env["API_TOKEN"] != "real-env-secret-987654" {
		t.Fatalf("initial save lost secrets: %+v", got)
	}

	// Simulate the dashboard: GET (masked) → POST the masked entry back.
	get := httptest.NewRecorder()
	s.authMiddleware(s.handleGetConfig)(get, configTestRequest(t, ctx, resolver, http.MethodGet, "/api/config", adminUser.ID, nil))
	if get.Code != http.StatusOK {
		t.Fatalf("GET /api/config status = %d", get.Code)
	}
	var view struct {
		Skills struct {
			Entries map[string]config.SkillEntryCfg `json:"entries"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}
	maskedEntry := view.Skills.Entries["my-skill"]
	if maskedEntry.APIKey == "real-secret-key-123456" || maskedEntry.Env["API_TOKEN"] == "real-env-secret-987654" {
		t.Fatalf("GET leaked plaintext secrets: %+v", maskedEntry)
	}

	post(map[string]any{"skills": map[string]any{"entries": map[string]any{"my-skill": maskedEntry}}})
	if got := storedEntry(); got.APIKey != "real-secret-key-123456" {
		t.Fatalf("masked write-back clobbered apiKey: got %q", got.APIKey)
	}
	if got := storedEntry(); got.Env["API_TOKEN"] != "real-env-secret-987654" {
		t.Fatalf("masked write-back clobbered env API_TOKEN: got %q", got.Env["API_TOKEN"])
	}
	if got := storedEntry(); got.Env["NORMAL_VAR"] != "plain" || !got.Enabled {
		t.Fatalf("non-secret fields should round-trip unchanged: %+v", got)
	}
}

func TestUpdateConfigMaskedAgentSkillEntryKeepsStoredSecret(t *testing.T) {
	ctx := context.Background()
	s, resolver, adminUser, _ := newAuthTestServer(t, ctx)

	if err := s.dataStore.SaveAgent(ctx, &store.AgentRecord{ID: "agent-1", UserID: adminUser.ID, Name: "Test Agent"}); err != nil {
		t.Fatalf("SaveAgent: %v", err)
	}

	post := func(body map[string]any) *httptest.ResponseRecorder {
		t.Helper()
		rr := httptest.NewRecorder()
		s.authMiddleware(s.handleUpdateConfig)(rr, configTestRequest(t, ctx, resolver, http.MethodPost, "/api/config", adminUser.ID, body))
		if rr.Code != http.StatusOK {
			t.Fatalf("POST /api/config status = %d, body = %s", rr.Code, rr.Body.String())
		}
		return rr
	}
	storedEntry := func() config.SkillEntryCfg {
		t.Helper()
		rec, err := s.dataStore.GetConfigByName(ctx, store.KindSetting, "", "agent-1", "skills.entries")
		if err != nil || rec == nil {
			t.Fatalf("GetConfigByName: rec=%v err=%v", rec, err)
		}
		blob, _ := json.Marshal(rec.Data)
		var entries map[string]config.SkillEntryCfg
		if err := json.Unmarshal(blob, &entries); err != nil {
			t.Fatalf("decode entries: %v", err)
		}
		return entries["my-skill"]
	}

	post(map[string]any{"skills": map[string]any{"agentEntries": map[string]any{
		"agent-1": map[string]any{
			"my-skill": map[string]any{
				"apiKey": "agent-secret-key-abcdef",
				"env":    map[string]any{"DB_PASSWORD": "real-db-pass-123456"},
			},
		},
	}}})
	if got := storedEntry(); got.APIKey != "agent-secret-key-abcdef" || got.Env["DB_PASSWORD"] != "real-db-pass-123456" {
		t.Fatalf("initial save lost secrets: %+v", got)
	}

	get := httptest.NewRecorder()
	s.authMiddleware(s.handleGetConfig)(get, configTestRequest(t, ctx, resolver, http.MethodGet, "/api/config", adminUser.ID, nil))
	var view struct {
		Skills struct {
			AgentEntries map[string]map[string]config.SkillEntryCfg `json:"agentEntries"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}
	maskedEntry := view.Skills.AgentEntries["agent-1"]["my-skill"]
	if maskedEntry.APIKey == "agent-secret-key-abcdef" {
		t.Fatalf("GET leaked plaintext apiKey: %+v", maskedEntry)
	}

	post(map[string]any{"skills": map[string]any{"agentEntries": map[string]any{
		"agent-1": map[string]any{"my-skill": maskedEntry},
	}}})
	if got := storedEntry(); got.APIKey != "agent-secret-key-abcdef" {
		t.Fatalf("masked write-back clobbered apiKey: got %q", got.APIKey)
	}
	if got := storedEntry(); got.Env["DB_PASSWORD"] != "real-db-pass-123456" {
		t.Fatalf("masked write-back clobbered env DB_PASSWORD: got %q", got.Env["DB_PASSWORD"])
	}
}

func TestMergeSkillEntry(t *testing.T) {
	existing := config.SkillEntryCfg{
		Enabled: true,
		APIKey:  "real-secret-key-123456",
		Env:     map[string]string{"API_TOKEN": "real-env-secret-987654", "NORMAL_VAR": "old"},
		Inherit: config.InheritAll,
	}
	in := config.SkillEntryCfg{
		Enabled: false,
		APIKey:  maskAPIKey("real-secret-key-123456"),
		Env:     map[string]string{"API_TOKEN": maskAPIKey("real-env-secret-987654"), "NORMAL_VAR": "new"},
	}
	out := mergeSkillEntry(existing, in)
	if out.APIKey != existing.APIKey {
		t.Errorf("masked apiKey should resolve to stored original, got %q", out.APIKey)
	}
	if out.Env["API_TOKEN"] != existing.Env["API_TOKEN"] {
		t.Errorf("masked env value should resolve to stored original, got %q", out.Env["API_TOKEN"])
	}
	if out.Env["NORMAL_VAR"] != "new" {
		t.Errorf("plain env value should follow the request, got %q", out.Env["NORMAL_VAR"])
	}
	if out.Enabled != in.Enabled {
		t.Errorf("enabled should follow the request, got %v", out.Enabled)
	}
	if out.Inherit != existing.Inherit {
		t.Errorf("omitted inherit should keep stored value, got %q", out.Inherit)
	}

	// A genuinely new value (no mask) must replace the stored one.
	out = mergeSkillEntry(existing, config.SkillEntryCfg{APIKey: "brand-new-key-000000"})
	if out.APIKey != "brand-new-key-000000" {
		t.Errorf("unmasked apiKey should replace the stored one, got %q", out.APIKey)
	}
}

func TestGetConfigIsolatesSystemSkillsAndPluginsFromOtherTenants(t *testing.T) {
	ctx := context.Background()
	s, resolver, adminUser, regularUser := newAuthTestServer(t, ctx)

	rr := httptest.NewRecorder()
	s.authMiddleware(s.handleUpdateConfig)(rr, configTestRequest(t, ctx, resolver, http.MethodPost, "/api/config", adminUser.ID, map[string]any{
		"skills": map[string]any{
			"entries": map[string]any{
				"image-gen": map[string]any{"apiKey": "sk-system-skill-secret-9999", "inherit": "all"},
			},
		},
		"plugins": map[string]any{
			"enabled": true,
			"entries": map[string]any{
				"mem0": map[string]any{"enabled": true, "inherit": "all", "config": map[string]any{"token": "plugin-secret"}},
			},
		},
	}))
	if rr.Code != http.StatusOK {
		t.Fatalf("admin POST status = %d body = %s", rr.Code, rr.Body.String())
	}

	get := httptest.NewRecorder()
	s.authMiddleware(s.handleGetConfig)(get, configTestRequest(t, ctx, resolver, http.MethodGet, "/api/config", regularUser.ID, nil))
	if get.Code != http.StatusOK {
		t.Fatalf("user GET status = %d", get.Code)
	}
	var view struct {
		Skills struct {
			Entries map[string]config.SkillEntryCfg `json:"entries"`
		} `json:"skills"`
		Plugins struct {
			Entries map[string]config.PluginEntryCfg `json:"entries"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := view.Skills.Entries["image-gen"]; ok {
		t.Fatalf("tenant B must not see system skill catalog: %+v", view.Skills.Entries)
	}
	if _, ok := view.Plugins.Entries["mem0"]; ok {
		t.Fatalf("tenant B must not see system plugin catalog: %+v", view.Plugins.Entries)
	}
}

func TestGetConfigIsolatesSystemMCPFromOtherTenants(t *testing.T) {
	ctx := context.Background()
	s, resolver, adminUser, regularUser := newAuthTestServer(t, ctx)

	rr := httptest.NewRecorder()
	s.authMiddleware(s.handleUpdateConfig)(rr, configTestRequest(t, ctx, resolver, http.MethodPost, "/api/config", adminUser.ID, map[string]any{
		"mcpServers": map[string]any{
			"platform-secret": map[string]any{
				"type":    "http",
				"url":     "https://mcp.example/private",
				"headers": map[string]any{"Authorization": "Bearer tenant-a-secret-9999"},
			},
		},
	}))
	if rr.Code != http.StatusOK {
		t.Fatalf("admin POST status = %d body = %s", rr.Code, rr.Body.String())
	}

	get := httptest.NewRecorder()
	s.authMiddleware(s.handleGetConfig)(get, configTestRequest(t, ctx, resolver, http.MethodGet, "/api/config", regularUser.ID, nil))
	if get.Code != http.StatusOK {
		t.Fatalf("user GET status = %d", get.Code)
	}
	var view struct {
		MCPServers map[string]config.MCPServerConfig `json:"mcpServers"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := view.MCPServers["platform-secret"]; ok {
		t.Fatalf("tenant B must not see tenant-A/system MCP catalog: %+v", view.MCPServers)
	}
}

func TestUpdateConfigMaskedMCPServerKeepsStoredSecret(t *testing.T) {
	ctx := context.Background()
	s, resolver, adminUser, _ := newAuthTestServer(t, ctx)

	post := func(body map[string]any) *httptest.ResponseRecorder {
		t.Helper()
		rr := httptest.NewRecorder()
		s.authMiddleware(s.handleUpdateConfig)(rr, configTestRequest(t, ctx, resolver, http.MethodPost, "/api/config", adminUser.ID, body))
		if rr.Code != http.StatusOK {
			t.Fatalf("POST /api/config status = %d, body = %s", rr.Code, rr.Body.String())
		}
		return rr
	}
	stored := func() config.MCPServerConfig {
		t.Helper()
		rec, err := s.dataStore.GetConfigByName(ctx, store.KindSetting, "", "", "mcpServers")
		if err != nil || rec == nil {
			t.Fatalf("GetConfigByName: rec=%v err=%v", rec, err)
		}
		blob, _ := json.Marshal(rec.Data)
		var servers map[string]config.MCPServerConfig
		if err := json.Unmarshal(blob, &servers); err != nil {
			t.Fatalf("decode mcpServers: %v", err)
		}
		return servers["postgres"]
	}

	post(map[string]any{"mcpServers": map[string]any{
		"postgres": map[string]any{
			"type":    "http",
			"url":     "https://mcp.example.com",
			"headers": map[string]any{"Authorization": "Bearer real-mcp-token-123456"},
			"env":     map[string]any{"API_KEY": "real-mcp-key-abcdef", "REGION": "us"},
		},
	}})
	if got := stored(); got.Headers["Authorization"] != "Bearer real-mcp-token-123456" || got.Env["API_KEY"] != "real-mcp-key-abcdef" {
		t.Fatalf("initial save lost secrets: %+v", got)
	}

	get := httptest.NewRecorder()
	s.authMiddleware(s.handleGetConfig)(get, configTestRequest(t, ctx, resolver, http.MethodGet, "/api/config", adminUser.ID, nil))
	if get.Code != http.StatusOK {
		t.Fatalf("GET /api/config status = %d", get.Code)
	}
	var view struct {
		MCPServers map[string]config.MCPServerConfig `json:"mcpServers"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	masked := view.MCPServers["postgres"]
	if masked.Headers["Authorization"] == "Bearer real-mcp-token-123456" || masked.Env["API_KEY"] == "real-mcp-key-abcdef" {
		t.Fatalf("GET leaked plaintext MCP secrets: %+v", masked)
	}
	if masked.Env["REGION"] != "us" {
		t.Fatalf("non-secret env should stay plaintext, got %q", masked.Env["REGION"])
	}

	post(map[string]any{"mcpServers": map[string]any{"postgres": masked}})
	if got := stored(); got.Headers["Authorization"] != "Bearer real-mcp-token-123456" {
		t.Fatalf("masked write-back clobbered header: got %q", got.Headers["Authorization"])
	}
	if got := stored(); got.Env["API_KEY"] != "real-mcp-key-abcdef" {
		t.Fatalf("masked write-back clobbered env API_KEY: got %q", got.Env["API_KEY"])
	}
}

func configTestRequest(t *testing.T, ctx context.Context, resolver *auth.Resolver, method, path, userID string, body map[string]any) *http.Request {
	t.Helper()

	cookie, err := resolver.IssueSession(ctx, userID)
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}
	var reader *bytes.Reader
	if body != nil {
		blob, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(blob)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.AddCookie(cookie)
	return req
}
