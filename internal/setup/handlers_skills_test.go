package setup

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/auth"
	"github.com/fastclaw-ai/fastclaw/internal/store"
	"github.com/fastclaw-ai/fastclaw/internal/users"
)

func TestListSkillsRequiresAuth(t *testing.T) {
	ctx := context.Background()
	s, resolver, adminUser, regularUser := newAuthTestServer(t, ctx)
	t.Setenv("FASTCLAW_HOME", t.TempDir())

	handler := s.authMiddleware(s.handleListSkills)

	t.Run("unauthenticated request is rejected", func(t *testing.T) {
		rr := httptest.NewRecorder()
		handler(rr, httptest.NewRequest(http.MethodGet, "/api/skills", nil))
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
		}
	})

	t.Run("regular user is allowed", func(t *testing.T) {
		rr := httptest.NewRecorder()
		handler(rr, authTestRequest(t, ctx, resolver, http.MethodGet, "/api/skills", regularUser.ID))
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
		}
		if got := strings.TrimSpace(rr.Body.String()); got != "[]" {
			t.Fatalf("body = %q, want []", got)
		}
	})

	t.Run("super admin is allowed", func(t *testing.T) {
		rr := httptest.NewRecorder()
		handler(rr, authTestRequest(t, ctx, resolver, http.MethodGet, "/api/skills", adminUser.ID))
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
		}
		if got := strings.TrimSpace(rr.Body.String()); got != "[]" {
			t.Fatalf("body = %q, want []", got)
		}
	})
}

func TestListAgentSkillsIncludesInheritedGlobal(t *testing.T) {
	ctx := context.Background()
	s, resolver, admin, _ := newAuthTestServer(t, ctx)
	home := t.TempDir()
	t.Setenv("FASTCLAW_HOME", home)

	agentID := "agt_inherit"
	if err := s.dataStore.SaveAgent(ctx, &store.AgentRecord{
		ID: agentID, UserID: admin.ID, Name: "Office Bot",
	}); err != nil {
		t.Fatalf("SaveAgent: %v", err)
	}

	writeSkill := func(dir, name, desc string) {
		t.Helper()
		root := filepath.Join(dir, name)
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "---\nname: " + name + "\ndescription: " + desc + "\n---\n# " + name + "\n"
		if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeSkill(filepath.Join(home, "skills"), "global-only", "from global")
	writeSkill(filepath.Join(home, "skills"), "shared", "global copy")
	writeSkill(filepath.Join(home, "agents", agentID, "agent", "skills"), "shared", "agent copy")
	writeSkill(filepath.Join(home, "agents", agentID, "agent", "skills"), "agent-only", "from agent")

	req := authTestRequest(t, ctx, resolver, http.MethodGet, "/api/agents/"+agentID+"/skills", admin.ID)
	req.SetPathValue("id", agentID)
	rr := httptest.NewRecorder()
	s.authMiddleware(s.handleListAgentSkills)(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rr.Code, rr.Body.String())
	}
	var got []struct {
		Name   string `json:"name"`
		Source string `json:"source"`
		Desc   string `json:"description"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v body = %s", err, rr.Body.String())
	}
	byName := map[string]struct {
		Source string
		Desc   string
	}{}
	for _, s := range got {
		byName[s.Name] = struct {
			Source string
			Desc   string
		}{s.Source, s.Desc}
	}
	if byName["global-only"].Source != "inherited" {
		t.Fatalf("global-only = %+v", byName["global-only"])
	}
	if byName["agent-only"].Source != "agent" {
		t.Fatalf("agent-only = %+v", byName["agent-only"])
	}
	if byName["shared"].Source != "agent" || byName["shared"].Desc != "agent copy" {
		t.Fatalf("shared should be agent shadow, got %+v", byName["shared"])
	}
	if _, ok := byName["shared"]; !ok || len(got) != 3 {
		t.Fatalf("want 3 skills (shadowed global omitted), got %+v", got)
	}
}

func TestMergeAgentSkillList(t *testing.T) {
	out := mergeAgentSkillList(
		[]map[string]any{{"name": "local", "description": "a"}},
		[]map[string]any{
			{"name": "local", "description": "global-hidden"},
			{"name": "shared", "description": "g"},
		},
		nil,
	)
	if len(out) != 2 {
		t.Fatalf("len = %d", len(out))
	}
	if out[0]["source"] != "agent" || out[0]["name"] != "local" {
		t.Fatalf("first = %+v", out[0])
	}
	if out[1]["source"] != "inherited" || out[1]["name"] != "shared" {
		t.Fatalf("second = %+v", out[1])
	}

	hidden := mergeAgentSkillList(
		[]map[string]any{{"name": "local"}},
		[]map[string]any{{"name": "catalog-only"}, {"name": "shared"}},
		map[string]bool{"catalog-only": true},
	)
	if len(hidden) != 2 {
		t.Fatalf("inherit=none should drop catalog-only, got %+v", hidden)
	}
	if hidden[1]["name"] != "shared" {
		t.Fatalf("shared should remain, got %+v", hidden)
	}
}

func newAuthTestServer(t *testing.T, ctx context.Context) (*Server, *auth.Resolver, *users.Account, *users.Account) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "fastclaw.db")
	st, err := store.NewDBStore("sqlite", "file:"+dbPath+"?cache=shared")
	if err != nil {
		t.Fatalf("NewDBStore: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	accts, err := users.NewAccounts(st)
	if err != nil {
		t.Fatalf("NewAccounts: %v", err)
	}
	adminUser := createAuthTestUser(t, ctx, accts, "admin", users.RoleSuperAdmin)
	regularUser := createAuthTestUser(t, ctx, accts, "user", users.RoleUser)
	resolver, err := auth.NewResolver(st)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	s := NewServer(0)
	s.SetStore(st)
	s.SetAuth(resolver)
	return s, resolver, adminUser, regularUser
}

func createAuthTestUser(t *testing.T, ctx context.Context, accts *users.Accounts, username, role string) *users.Account {
	t.Helper()

	acct, err := accts.Create(ctx, users.CreateInput{
		Username: username,
		Email:    username + "@example.test",
		Password: "password",
		Role:     role,
	})
	if err != nil {
		t.Fatalf("Create(%s): %v", username, err)
	}
	return acct
}

func authTestRequest(t *testing.T, ctx context.Context, resolver *auth.Resolver, method, path, userID string) *http.Request {
	t.Helper()

	cookie, err := resolver.IssueSession(ctx, userID)
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}
	req := httptest.NewRequest(method, path, nil)
	req.AddCookie(cookie)
	return req
}
