package agentcli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/store"
	"github.com/fastclaw-ai/fastclaw/internal/users"
)

// freshStore opens an in-memory sqlite store for the test, migrated and
// ready to use.
func freshStore(t *testing.T) store.Store {
	t.Helper()
	t.Setenv("FASTCLAW_HOME", t.TempDir())
	st, err := store.New(&store.StorageConfig{
		Type:        store.StorageSQLite,
		AutoMigrate: true,
	}, t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestInitCreatesAgentAndOwner(t *testing.T) {
	st := freshStore(t)
	t.Setenv("OPENAI_API_KEY", "test-key")

	res, err := Init(context.Background(), st, "alpha", InitOptions{
		Description: "T1",
		Provider:    "openai",
		Model:       "openai/gpt-4o-mini",
		APIKeyEnv:   "OPENAI_API_KEY",
	})
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if !res.Created {
		t.Fatal("expected Created=true on first init")
	}
	if !res.OwnerCreated || res.GeneratedPassword == "" {
		t.Fatal("expected an admin to be created with a generated password")
	}
	if res.Agent.Name != "alpha" {
		t.Fatalf("agent name = %q", res.Agent.Name)
	}
	if !strings.HasPrefix(res.Agent.ID, "agt_") {
		t.Fatalf("agent id should start with agt_, got %q", res.Agent.ID)
	}
	if res.Agent.Config["description"] != "T1" {
		t.Fatalf("description not saved: %#v", res.Agent.Config)
	}

	// Provider config went to system scope.
	rec, err := st.GetConfigByName(context.Background(), store.KindProvider, "", "", "openai")
	if err != nil {
		t.Fatalf("get provider: %v", err)
	}
	if rec.Data["apiBase"] != "https://api.openai.com/v1" {
		t.Fatalf("preset apiBase missing: %#v", rec.Data["apiBase"])
	}
	// Model went to agent scope.
	model, err := GetConfig(context.Background(), st, res.Agent.ID, "model")
	if err != nil {
		t.Fatalf("get model: %v", err)
	}
	if model != "openai/gpt-4o-mini" {
		t.Fatalf("model not saved at agent scope: %#v", model)
	}
}

func TestInitProviderPreflightRunsBeforeWrites(t *testing.T) {
	st := freshStore(t)
	t.Setenv("OPENAI_API_KEY", "")

	_, err := Init(context.Background(), st, "alpha", InitOptions{
		Provider:  "openai",
		Model:     "openai/gpt-4o-mini",
		APIKeyEnv: "OPENAI_API_KEY",
	})
	if err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Fatalf("expected missing API key env error, got %v", err)
	}
	if n, err := st.CountUsers(context.Background()); err != nil || n != 0 {
		t.Fatalf("preflight failure must not create users: n=%d err=%v", n, err)
	}
	agents, err := st.ListAllAgents(context.Background())
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	if len(agents) != 0 {
		t.Fatalf("preflight failure must not create agents: %#v", agents)
	}
	if rec, err := st.GetConfigByName(context.Background(), store.KindProvider, "", "", "openai"); err == nil || rec != nil {
		t.Fatalf("preflight failure must not save provider config: rec=%#v err=%v", rec, err)
	}
}

func TestInitRejectsDuplicateName(t *testing.T) {
	st := freshStore(t)

	res1, err := Init(context.Background(), st, "alpha", InitOptions{Description: "first"})
	if err != nil {
		t.Fatalf("init 1: %v", err)
	}
	_, err = Init(context.Background(), st, "alpha", InitOptions{Description: "second"})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected duplicate-name error, got %v", err)
	}
	rec, err := st.GetAgent(context.Background(), res1.Agent.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if rec.Config["description"] != "first" {
		t.Fatalf("description was overwritten despite the rejection: %#v", rec.Config)
	}
}

func TestInitUpdatesByExplicitID(t *testing.T) {
	st := freshStore(t)

	res1, err := Init(context.Background(), st, "alpha", InitOptions{Description: "first"})
	if err != nil {
		t.Fatalf("init 1: %v", err)
	}
	res2, err := Init(context.Background(), st, "alpha", InitOptions{
		AgentID:     res1.Agent.ID,
		Description: "second",
	})
	if err != nil {
		t.Fatalf("update by --id: %v", err)
	}
	if res2.Created {
		t.Fatal("expected --id to update, not create")
	}
	if res2.Agent.ID != res1.Agent.ID {
		t.Fatalf("agent id changed: %s -> %s", res1.Agent.ID, res2.Agent.ID)
	}
	if res2.Agent.Config["description"] != "second" {
		t.Fatalf("description not updated: %#v", res2.Agent.Config)
	}
}

func TestInitExplicitMissingIDDoesNotFallbackToName(t *testing.T) {
	st := freshStore(t)

	if _, err := Init(context.Background(), st, "alpha", InitOptions{}); err != nil {
		t.Fatalf("seed alpha: %v", err)
	}
	_, err := Init(context.Background(), st, "alpha", InitOptions{AgentID: "agt_missing"})
	if err == nil || !strings.Contains(err.Error(), `agent id "agt_missing" not found`) {
		t.Fatalf("expected missing id error, got %v", err)
	}
	agents, err := st.ListAllAgents(context.Background())
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	if len(agents) != 1 || agents[0].Name != "alpha" {
		t.Fatalf("missing --id must not create or update by name: %#v", agents)
	}
}

func TestInitRejectsRebindToOtherUser(t *testing.T) {
	st := freshStore(t)

	res1, err := Init(context.Background(), st, "alpha", InitOptions{Username: "alice"})
	if err != nil {
		t.Fatalf("seed alice: %v", err)
	}
	if !res1.OwnerCreated {
		t.Fatal("expected alice to be created on first init")
	}

	// Manually create bob.
	accts, _ := users.NewAccounts(st)
	if _, err := accts.Create(context.Background(), users.CreateInput{
		Username:    "bob",
		Email:       "bob@local",
		Password:    "secret-bob",
		DisplayName: "Bob",
		Role:        users.RoleUser,
	}); err != nil {
		t.Fatalf("create bob: %v", err)
	}

	// Update via --id with --username bob: must refuse the silent rebind.
	_, err = Init(context.Background(), st, "alpha", InitOptions{
		AgentID:  res1.Agent.ID,
		Username: "bob",
	})
	if err == nil || !strings.Contains(err.Error(), "is owned by user") {
		t.Fatalf("expected rebind refusal, got %v", err)
	}

	// Update via --id without --username: owner preserved.
	res3, err := Init(context.Background(), st, "alpha", InitOptions{AgentID: res1.Agent.ID})
	if err != nil {
		t.Fatalf("update by --id: %v", err)
	}
	if res3.Agent.UserID != res1.Agent.UserID {
		t.Fatalf("owner silently rebound: %s -> %s", res1.Agent.UserID, res3.Agent.UserID)
	}
}

func TestEnsureOwnerRejectsMissingExplicitUser(t *testing.T) {
	st := freshStore(t)
	if _, err := Init(context.Background(), st, "alpha", InitOptions{}); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	_, err := Init(context.Background(), st, "beta", InitOptions{Username: "ghost"})
	if err == nil || !strings.Contains(err.Error(), `"ghost"`) {
		t.Fatalf("expected ghost-not-found error, got %v", err)
	}
}

func TestInitDefaultsToAdmin(t *testing.T) {
	st := freshStore(t)

	res, err := Init(context.Background(), st, "alpha", InitOptions{})
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if !res.OwnerCreated {
		t.Fatal("expected admin to be created on first init of empty DB")
	}

	accts, _ := users.NewAccounts(st)
	acct, err := accts.Get(context.Background(), res.Agent.UserID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if acct.Username != "admin" {
		t.Fatalf("default owner should be admin, got %q", acct.Username)
	}
	if acct.Role != users.RoleSuperAdmin {
		t.Fatalf("default admin should be super_admin, got %q", acct.Role)
	}
}

func TestInitFallsBackToFirstSuperAdmin(t *testing.T) {
	st := freshStore(t)

	// Seed alice as the only user (no admin in the DB). She is created
	// as a super_admin by ensureOwner.
	res1, err := Init(context.Background(), st, "alpha", InitOptions{Username: "alice"})
	if err != nil {
		t.Fatalf("seed alice: %v", err)
	}

	// New agent with no --username: admin doesn't exist, but alice does
	// and is a super_admin, so init should adopt her instead of erroring.
	res2, err := Init(context.Background(), st, "beta", InitOptions{})
	if err != nil {
		t.Fatalf("init beta: %v", err)
	}
	if res2.Agent.UserID != res1.Agent.UserID {
		t.Fatalf("expected beta to be owned by alice (%s), got %s", res1.Agent.UserID, res2.Agent.UserID)
	}
	if res2.OwnerCreated {
		t.Fatal("fallback to existing super_admin must not create a new user")
	}
}

func TestInitFailsWhenNoSuperAdmin(t *testing.T) {
	st := freshStore(t)

	// Seed admin via the empty-DB path so we can demote them and leave
	// the DB with users but no super_admin.
	res, err := Init(context.Background(), st, "alpha", InitOptions{})
	if err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	accts, _ := users.NewAccounts(st)
	if _, err := accts.Update(context.Background(), res.Agent.UserID, "", users.RoleUser, "", nil); err != nil {
		t.Fatalf("demote admin: %v", err)
	}
	// Rename so the "admin" lookup doesn't short-circuit before the
	// super_admin scan.
	rec, _ := st.GetUser(context.Background(), res.Agent.UserID)
	rec.Username = "alice"
	if err := st.UpdateUser(context.Background(), rec); err != nil {
		t.Fatalf("rename: %v", err)
	}

	_, err = Init(context.Background(), st, "beta", InitOptions{})
	if err == nil || !strings.Contains(err.Error(), "super_admin") {
		t.Fatalf("expected no-super_admin error, got %v", err)
	}
}

func TestAssertAgentScopedConfig(t *testing.T) {
	if err := AssertAgentScopedConfig("provider.openai.apiKey"); err == nil {
		t.Fatal("provider key must be rejected")
	} else if !strings.Contains(err.Error(), "Models") || !strings.Contains(err.Error(), "Do not retry") {
		t.Fatalf("provider refusal should name Models and forbid retry, got: %v", err)
	}
	if err := AssertAgentScopedConfig("tools.providers"); err == nil {
		t.Fatal("system namespace must be rejected")
	} else if !strings.Contains(err.Error(), "fastclaw tools provider-set") {
		t.Fatalf("tools.providers should point at the tools CLI, got: %v", err)
	}
	if err := AssertAgentScopedConfig("mcpServers"); err == nil {
		t.Fatal("mcpServers must be rejected")
	} else {
		msg := err.Error()
		for _, need := range []string{"agent → MCP", "mcp.json", "Do not retry", "Supported keys:"} {
			if !strings.Contains(msg, need) {
				t.Fatalf("mcpServers refusal missing %q: %s", need, msg)
			}
		}
		if !strings.Contains(msg, "model") {
			t.Fatalf("mcpServers refusal should list supported keys, got: %s", msg)
		}
	}
	if err := AssertAgentScopedConfig("model"); err != nil {
		t.Fatalf("agent-scope model: %v", err)
	}
	if err := AssertAgentScopedConfig("sandbox.enabled"); err != nil {
		t.Fatalf("agent-scope sandbox: %v", err)
	}
}

func TestSetGetConfigAgentScope(t *testing.T) {
	st := freshStore(t)
	res, err := Init(context.Background(), st, "alpha", InitOptions{})
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := SetConfig(context.Background(), st, res.Agent.ID, "temperature", "0.42"); err != nil {
		t.Fatalf("set temp: %v", err)
	}
	if err := SetConfig(context.Background(), st, res.Agent.ID, "sandbox.enabled", "true"); err != nil {
		t.Fatalf("set sandbox.enabled: %v", err)
	}
	temp, _ := GetConfig(context.Background(), st, res.Agent.ID, "temperature")
	if got, ok := temp.(float64); !ok || got != 0.42 {
		t.Fatalf("temperature round-trip: %#v", temp)
	}
	box, _ := GetConfig(context.Background(), st, res.Agent.ID, "sandbox")
	m, _ := box.(map[string]interface{})
	if m["enabled"] != true || m["backend"] != "docker" {
		t.Fatalf("sandbox auto-default-backend missing: %#v", m)
	}
	// Different agent doesn't see this scope.
	res2, _ := Init(context.Background(), st, "beta", InitOptions{})
	temp2, _ := GetConfig(context.Background(), st, res2.Agent.ID, "temperature")
	if temp2 != nil {
		t.Fatalf("agent-scope leak: beta sees %#v", temp2)
	}
}

func TestSetProviderModelRejectsEmptyValue(t *testing.T) {
	st := freshStore(t)
	t.Setenv("OPENAI_API_KEY", "test-key")

	if _, err := Init(context.Background(), st, "alpha", InitOptions{
		Provider:  "openai",
		Model:     "openai/gpt-4.1",
		APIKeyEnv: "OPENAI_API_KEY",
	}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := SetConfig(context.Background(), st, "ignored", "provider.openai.model", ""); err == nil {
		t.Fatal("empty model id must be rejected")
	}
	rec, err := st.GetConfigByName(context.Background(), store.KindProvider, "", "", "openai")
	if err != nil {
		t.Fatalf("get provider: %v", err)
	}
	models, _ := rec.Data["models"].([]interface{})
	if len(models) == 0 {
		t.Fatal("models list was wiped despite the rejection")
	}
	if err := SetConfig(context.Background(), st, "ignored", "provider.openai.models", "[]"); err != nil {
		t.Fatalf("explicit clear: %v", err)
	}
	rec, _ = st.GetConfigByName(context.Background(), store.KindProvider, "", "", "openai")
	if models, _ := rec.Data["models"].([]interface{}); len(models) != 0 {
		t.Fatalf("models not cleared: %#v", models)
	}
}

func TestPutGetListFiles(t *testing.T) {
	st := freshStore(t)
	res, err := Init(context.Background(), st, "alpha", InitOptions{})
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := PutFile(context.Background(), st, res.Agent.ID, res.Agent.UserID, "SOUL.md", []byte("hi")); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := GetFile(context.Background(), st, res.Agent.ID, res.Agent.UserID, "SOUL.md")
	if err != nil || string(got) != "hi" {
		t.Fatalf("get: data=%q err=%v", got, err)
	}
	files, err := ListFiles(context.Background(), st, res.Agent.ID, res.Agent.UserID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(files) != 1 || files[0] != "SOUL.md" {
		t.Fatalf("list returned %#v", files)
	}
}

func TestPutFileRejectsUnsupportedFilename(t *testing.T) {
	st := freshStore(t)
	res, err := Init(context.Background(), st, "alpha", InitOptions{})
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	err = PutFile(context.Background(), st, res.Agent.ID, res.Agent.UserID, "NOTES.md", []byte("x"))
	if err == nil {
		t.Fatal("expected rejection of NOTES.md")
	}
	if errors.Is(err, store.ErrNotFound) {
		t.Fatalf("validation must run before store: %v", err)
	}
}

func TestRemoveDeletesAgentAndFiles(t *testing.T) {
	st := freshStore(t)
	res, err := Init(context.Background(), st, "alpha", InitOptions{})
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := PutFile(context.Background(), st, res.Agent.ID, res.Agent.UserID, "SOUL.md", []byte("x")); err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, err := Remove(context.Background(), st, "alpha"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := Remove(context.Background(), st, "alpha"); err == nil {
		t.Fatal("expected error removing missing agent")
	}
	files, _ := st.ListAgentFiles(context.Background(), res.Agent.ID, res.Agent.UserID)
	if len(files) != 0 {
		t.Fatalf("files leak after remove: %#v", files)
	}
}

func TestResolveByNameAndID(t *testing.T) {
	st := freshStore(t)
	res, _ := Init(context.Background(), st, "alpha", InitOptions{})

	r1, err := Resolve(context.Background(), st, "alpha")
	if err != nil || r1.ID != res.Agent.ID {
		t.Fatalf("resolve by name: %v / %+v", err, r1)
	}
	r2, err := Resolve(context.Background(), st, res.Agent.ID)
	if err != nil || r2.ID != res.Agent.ID {
		t.Fatalf("resolve by id: %v / %+v", err, r2)
	}
	if _, err := Resolve(context.Background(), st, "missing"); err == nil {
		t.Fatal("expected error resolving missing")
	}
}

func TestResolveAgtPrefixDisplayName(t *testing.T) {
	st := freshStore(t)
	res, err := Init(context.Background(), st, "agt_demo", InitOptions{})
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	got, err := Resolve(context.Background(), st, "agt_demo")
	if err != nil {
		t.Fatalf("resolve agt_ display name: %v", err)
	}
	if got.ID != res.Agent.ID {
		t.Fatalf("resolved wrong agent: got %s want %s", got.ID, res.Agent.ID)
	}
}

func TestResolveAmbiguousIDAndDisplayName(t *testing.T) {
	st := freshStore(t)

	byID, err := Init(context.Background(), st, "alpha", InitOptions{})
	if err != nil {
		t.Fatalf("seed alpha: %v", err)
	}
	if err := st.SaveAgent(context.Background(), &store.AgentRecord{
		ID:     "agt_other",
		UserID: byID.Agent.UserID,
		Name:   byID.Agent.ID,
		Config: map[string]interface{}{},
	}); err != nil {
		t.Fatalf("seed name collision: %v", err)
	}

	_, err = Resolve(context.Background(), st, byID.Agent.ID)
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous id/name reference, got %v", err)
	}
}

func TestResolveAmbiguousName(t *testing.T) {
	st := freshStore(t)
	// Create two agents with the same display name (the dashboard
	// allows this since IDs are random).
	if _, err := Init(context.Background(), st, "alpha", InitOptions{}); err != nil {
		t.Fatalf("init 1: %v", err)
	}
	// Force a second one with a different id.
	id, _ := generateAgentID()
	if err := st.SaveAgent(context.Background(), &store.AgentRecord{
		ID: id, UserID: "u_x", Name: "alpha", Config: map[string]interface{}{},
	}); err != nil {
		t.Fatalf("seed dup: %v", err)
	}
	_, err := Resolve(context.Background(), st, "alpha")
	if !errors.Is(err, ErrAmbiguousName) {
		t.Fatalf("expected ErrAmbiguousName, got %v", err)
	}
}

func TestParseValueTypes(t *testing.T) {
	cases := map[string]interface{}{
		`true`:          true,
		`false`:         false,
		`0.5`:           0.5,
		`8192`:          float64(8192),
		`"hello"`:       "hello",
		`{"a":1}`:       map[string]interface{}{"a": float64(1)},
		"plain-string":  "plain-string",
		`openai/gpt-4o`: "openai/gpt-4o",
	}
	for raw, want := range cases {
		got := parseValue(raw)
		switch w := want.(type) {
		case map[string]interface{}:
			gm, _ := got.(map[string]interface{})
			if gm["a"] != w["a"] {
				t.Errorf("parseValue(%q) map mismatch: %#v", raw, got)
			}
		default:
			if got != want {
				t.Errorf("parseValue(%q) = %#v, want %#v", raw, got, want)
			}
		}
	}
}

func TestSettingKeyRouting(t *testing.T) {
	cases := []struct {
		key            string
		ns             string
		path           []string
		wantAgentScope bool
	}{
		{"model", "agents.defaults", []string{"model"}, true},
		{"temperature", "agents.defaults", []string{"temperature"}, true},
		{"sandbox", "sandbox", nil, true},
		{"sandbox.enabled", "sandbox", []string{"enabled"}, true},
		{"plugins", "plugins", nil, false},
		{"plugins.foo", "plugins", []string{"foo"}, false},
	}
	for _, tc := range cases {
		ns, path, isAgent, err := settingKey(tc.key)
		if err != nil {
			t.Errorf("settingKey(%q) error: %v", tc.key, err)
			continue
		}
		if ns != tc.ns || isAgent != tc.wantAgentScope {
			t.Errorf("settingKey(%q): ns=%q isAgentScope=%v (want ns=%q isAgentScope=%v)", tc.key, ns, isAgent, tc.ns, tc.wantAgentScope)
		}
		if len(path) != len(tc.path) {
			t.Errorf("settingKey(%q): path %v want %v", tc.key, path, tc.path)
			continue
		}
		for i, p := range path {
			if p != tc.path[i] {
				t.Errorf("settingKey(%q)[%d] = %q, want %q", tc.key, i, p, tc.path[i])
			}
		}
	}
	if _, _, _, err := settingKey("bogus"); err == nil {
		t.Error("expected error for unknown key")
	} else if !strings.Contains(err.Error(), "Supported keys:") || !strings.Contains(err.Error(), "Do not retry") {
		t.Errorf("unknown-key error should list supported keys and forbid retry, got: %v", err)
	}
	if _, _, _, err := settingKey("mcpServers"); err == nil {
		t.Error("mcpServers must not route through generic config set")
	} else if !strings.Contains(err.Error(), "agent → MCP") {
		t.Errorf("mcpServers error should name the dashboard door, got: %v", err)
	}
	if _, _, _, err := settingKey("bindings"); err == nil {
		t.Error("bindings must not be exposed through generic config set")
	}
	if _, _, _, err := settingKey("bindings.list"); err == nil {
		t.Error("bindings.* must not be exposed through generic config set")
	}
}

func TestProviderPresetOfficialBases(t *testing.T) {
	cases := []struct {
		name, apiBase, apiKeyEnv string
	}{
		{"zhipu", "https://open.bigmodel.cn/api/paas/v4", "ZHIPUAI_API_KEY"},
		{"zai", "https://open.bigmodel.cn/api/paas/v4", "ZHIPUAI_API_KEY"},
		{"kimi", "https://api.moonshot.cn/v1", "MOONSHOT_API_KEY"},
		{"moonshot", "https://api.moonshot.cn/v1", "MOONSHOT_API_KEY"},
		{"grok", "https://api.x.ai/v1", "XAI_API_KEY"},
		{"xai", "https://api.x.ai/v1", "XAI_API_KEY"},
		{"openai", "https://api.openai.com/v1", "OPENAI_API_KEY"},
	}
	for _, tc := range cases {
		got := providerPreset(tc.name)
		if got.apiBase != tc.apiBase || got.apiKeyEnv != tc.apiKeyEnv || got.apiType != "openai-chat" {
			t.Errorf("providerPreset(%q) = %+v, want base %s env %s", tc.name, got, tc.apiBase, tc.apiKeyEnv)
		}
	}
}

func TestAppendModelFillsKnownContextWindow(t *testing.T) {
	got := appendModel(nil, "glm-5.3")
	if len(got) != 1 || got[0].ContextWindow != 1_000_000 {
		t.Fatalf("glm-5.3 window = %+v, want 1000000", got)
	}
	got = appendModel(got, "grok-4.6")
	if got[1].ContextWindow != 500_000 {
		t.Fatalf("grok-4.6 window = %d, want 500000", got[1].ContextWindow)
	}
	got = appendModel(got, "kimi-k3")
	if got[2].ContextWindow != 1_048_576 {
		t.Fatalf("kimi-k3 window = %d, want 1048576", got[2].ContextWindow)
	}
}
