package setup

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/store"
)

func TestOwnerUserMemorySystemFileIsNotAnOverride(t *testing.T) {
	ctx := context.Background()
	s, resolver, owner, chatter := newAuthTestServer(t, ctx)

	const agentID = "agt_sysfile"
	now := time.Now().UTC()
	if err := s.dataStore.SaveAgent(ctx, &store.AgentRecord{
		ID: agentID, UserID: owner.ID, Name: "sysfile agent",
		IsPublic: true, Config: map[string]any{}, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("save agent: %v", err)
	}
	if err := s.dataStore.SaveAgentFile(ctx, agentID, owner.ID, "USER.md", []byte("Name: Alice")); err != nil {
		t.Fatalf("save owner USER.md: %v", err)
	}
	if err := s.dataStore.SaveAgentFile(ctx, agentID, owner.ID, "MEMORY.md", []byte("Fact: likes tea")); err != nil {
		t.Fatalf("save owner MEMORY.md: %v", err)
	}

	getFile := func(userID, name string) map[string]any {
		t.Helper()
		req := authTestRequest(t, ctx, resolver, http.MethodGet, "/api/agents/"+agentID+"/system-files/"+name, userID)
		req.SetPathValue("id", agentID)
		req.SetPathValue("name", name)
		rr := httptest.NewRecorder()
		s.authMiddleware(s.handleGetAgentSystemFile)(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("GET %s as %s = %d: %s", name, userID, rr.Code, rr.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return out
	}

	ownerUser := getFile(owner.ID, "USER.md")
	if ownerUser["source"] != "owner" {
		t.Errorf("owner USER.md source = %v, want owner (not db/override)", ownerUser["source"])
	}
	if ownerUser["content"] != "Name: Alice" {
		t.Errorf("owner USER.md content = %v", ownerUser["content"])
	}
	if _, ok := ownerUser["baseContent"]; ok {
		t.Errorf("owner USER.md should not expose baseContent")
	}

	ownerMem := getFile(owner.ID, "MEMORY.md")
	if ownerMem["source"] != "owner" {
		t.Errorf("owner MEMORY.md source = %v, want owner", ownerMem["source"])
	}

	// A public-link chatter without their own row sees the owner's copy.
	chatterUser := getFile(chatter.ID, "USER.md")
	if chatterUser["source"] != "owner" {
		t.Errorf("chatter fallback USER.md source = %v, want owner", chatterUser["source"])
	}

	if err := s.dataStore.SaveAgentFile(ctx, agentID, chatter.ID, "USER.md", []byte("Name: Bob")); err != nil {
		t.Fatalf("save chatter USER.md: %v", err)
	}
	chatterOwn := getFile(chatter.ID, "USER.md")
	if chatterOwn["source"] != "db" {
		t.Errorf("chatter override USER.md source = %v, want db", chatterOwn["source"])
	}
	if chatterOwn["baseContent"] != "Name: Alice" {
		t.Errorf("chatter override baseContent = %v, want owner's row", chatterOwn["baseContent"])
	}

	// Identity files stay on the owner row regardless of caller.
	if err := s.dataStore.SaveAgentFile(ctx, agentID, owner.ID, "SOUL.md", []byte("Be kind")); err != nil {
		t.Fatalf("save SOUL.md: %v", err)
	}
	soul := getFile(owner.ID, "SOUL.md")
	if soul["source"] != "owner" {
		t.Errorf("SOUL.md source = %v, want owner", soul["source"])
	}

	// Public viewers must not download the persona spec over HTTP.
	req := authTestRequest(t, ctx, resolver, http.MethodGet, "/api/agents/"+agentID+"/system-files/SOUL.md", chatter.ID)
	req.SetPathValue("id", agentID)
	req.SetPathValue("name", "SOUL.md")
	rr := httptest.NewRecorder()
	s.authMiddleware(s.handleGetAgentSystemFile)(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("chatter GET SOUL.md = %d, want 403: %s", rr.Code, rr.Body.String())
	}

	// MEMORY.md has no owner fallback for a stranger — unlike USER.md.
	memReq := authTestRequest(t, ctx, resolver, http.MethodGet, "/api/agents/"+agentID+"/system-files/MEMORY.md", chatter.ID)
	memReq.SetPathValue("id", agentID)
	memReq.SetPathValue("name", "MEMORY.md")
	memRR := httptest.NewRecorder()
	s.authMiddleware(s.handleGetAgentSystemFile)(memRR, memReq)
	if memRR.Code != http.StatusOK {
		t.Fatalf("chatter GET MEMORY.md = %d: %s", memRR.Code, memRR.Body.String())
	}
	var mem map[string]any
	if err := json.Unmarshal(memRR.Body.Bytes(), &mem); err != nil {
		t.Fatalf("decode MEMORY.md: %v", err)
	}
	if mem["source"] != "default" || mem["content"] != "" {
		t.Fatalf("chatter MEMORY.md leaked owner row: %+v", mem)
	}
}
