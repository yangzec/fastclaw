package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/store"
)

func TestConnectWeComPersistsAndMasksSecret(t *testing.T) {
	ctx := context.Background()
	s, resolver, admin, _ := newAuthTestServer(t, ctx)
	agentID := "agent-wecom"
	if err := s.dataStore.SaveAgent(ctx, &store.AgentRecord{
		ID: agentID, UserID: admin.ID, Name: "WeCom Bot",
	}); err != nil {
		t.Fatalf("SaveAgent: %v", err)
	}

	orig := wecomValidateCredentials
	wecomValidateCredentials = func(context.Context, string, string, string) error { return nil }
	t.Cleanup(func() { wecomValidateCredentials = orig })

	body, _ := json.Marshal(map[string]any{
		"botId":  "bot_official_1",
		"secret": "long-conn-secret",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/agents/"+agentID+"/channels/wecom", bytes.NewReader(body))
	req.SetPathValue("id", agentID)
	cookie, err := resolver.IssueSession(ctx, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	s.authMiddleware(s.handleConnectAgentWeCom)(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rr.Code, rr.Body.String())
	}
	var out struct {
		OK    bool   `json:"ok"`
		BotID string `json:"botId"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.OK || out.BotID != "bot_official_1" {
		t.Fatalf("payload = %+v", out)
	}

	ch, err := s.dataStore.LookupChannel(ctx, "wecom", "bot_official_1")
	if err != nil || ch == nil {
		t.Fatalf("LookupChannel: %v %+v", err, ch)
	}
	if ch.BotToken != "long-conn-secret" {
		t.Fatalf("secret not persisted")
	}
	if !channelUsesLongConn(ch) {
		t.Fatalf("expected useLongConn")
	}
}

func TestConnectWeComRequiresCredentials(t *testing.T) {
	ctx := context.Background()
	s, resolver, admin, _ := newAuthTestServer(t, ctx)
	agentID := "agent-wecom-empty"
	if err := s.dataStore.SaveAgent(ctx, &store.AgentRecord{
		ID: agentID, UserID: admin.ID, Name: "WeCom Bot",
	}); err != nil {
		t.Fatalf("SaveAgent: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/agents/"+agentID+"/channels/wecom", bytes.NewReader([]byte(`{}`)))
	req.SetPathValue("id", agentID)
	cookie, err := resolver.IssueSession(ctx, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	s.authMiddleware(s.handleConnectAgentWeCom)(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", rr.Code, rr.Body.String())
	}
}
