package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/config"
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

func TestConnectWeComOAPersistsAndLists(t *testing.T) {
	ctx := context.Background()
	s, resolver, admin, _ := newAuthTestServer(t, ctx)
	agentID := "agent-wecom-oa"
	if err := s.dataStore.SaveAgent(ctx, &store.AgentRecord{
		ID: agentID, UserID: admin.ID, Name: "WeCom OA",
	}); err != nil {
		t.Fatalf("SaveAgent: %v", err)
	}

	origBot := wecomValidateCredentials
	wecomValidateCredentials = func(context.Context, string, string, string) error { return nil }
	t.Cleanup(func() { wecomValidateCredentials = origBot })
	origOA := wecomValidateOA
	wecomValidateOA = func(context.Context, string, string) error { return nil }
	t.Cleanup(func() { wecomValidateOA = origOA })

	cookie, err := resolver.IssueSession(ctx, admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{"botId": "bot_oa_1", "secret": "long-conn"})
	req := httptest.NewRequest(http.MethodPost, "/api/agents/"+agentID+"/channels/wecom", bytes.NewReader(body))
	req.SetPathValue("id", agentID)
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	s.authMiddleware(s.handleConnectAgentWeCom)(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("connect bot status = %d body = %s", rr.Code, rr.Body.String())
	}

	oaBody, _ := json.Marshal(map[string]any{
		"corpId": "ww_corp", "secret": "corp-secret", "agentId": "1000014",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/agents/"+agentID+"/channels/wecom/bot_oa_1/oa", bytes.NewReader(oaBody))
	req.SetPathValue("id", agentID)
	req.SetPathValue("accountId", "bot_oa_1")
	req.AddCookie(cookie)
	rr = httptest.NewRecorder()
	s.authMiddleware(s.handleConnectAgentWeComOA)(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("connect oa status = %d body = %s", rr.Code, rr.Body.String())
	}

	ch, err := s.dataStore.LookupChannel(ctx, "wecom", "bot_oa_1")
	if err != nil || ch == nil {
		t.Fatalf("LookupChannel: %v", err)
	}
	if ch.BotToken != "long-conn" {
		t.Fatalf("bot secret clobbered: %q", ch.BotToken)
	}
	acct := config.ChannelConfigFromData(ch.Data).Accounts["bot_oa_1"]
	if acct.CorpID != "ww_corp" || acct.CorpSecret != "corp-secret" || acct.CorpAgentID != "1000014" {
		t.Fatalf("oa acct = %+v", acct)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/agents/"+agentID+"/channels", nil)
	req.SetPathValue("id", agentID)
	req.AddCookie(cookie)
	rr = httptest.NewRecorder()
	s.authMiddleware(s.handleListAgentChannels)(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list status = %d body = %s", rr.Code, rr.Body.String())
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte(`"oaEnabled":true`)) {
		t.Fatalf("list missing oaEnabled: %s", rr.Body.String())
	}
	if bytes.Contains(rr.Body.Bytes(), []byte("corp-secret")) {
		t.Fatalf("list leaked corp secret: %s", rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/agents/"+agentID+"/channels/wecom/bot_oa_1/oa", nil)
	req.SetPathValue("id", agentID)
	req.SetPathValue("accountId", "bot_oa_1")
	req.AddCookie(cookie)
	rr = httptest.NewRecorder()
	s.authMiddleware(s.handleDisconnectAgentWeComOA)(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("disconnect oa status = %d body = %s", rr.Code, rr.Body.String())
	}
	ch, _ = s.dataStore.LookupChannel(ctx, "wecom", "bot_oa_1")
	acct = config.ChannelConfigFromData(ch.Data).Accounts["bot_oa_1"]
	if acct.CorpID != "" || acct.CorpSecret != "" {
		t.Fatalf("oa not cleared: %+v", acct)
	}
	if ch.BotToken != "long-conn" {
		t.Fatalf("bot disconnected by oa delete")
	}
}

func TestConnectWeComOARequiresBot(t *testing.T) {
	ctx := context.Background()
	s, resolver, admin, _ := newAuthTestServer(t, ctx)
	agentID := "agent-wecom-oa-empty"
	if err := s.dataStore.SaveAgent(ctx, &store.AgentRecord{
		ID: agentID, UserID: admin.ID, Name: "WeCom OA",
	}); err != nil {
		t.Fatalf("SaveAgent: %v", err)
	}
	origOA := wecomValidateOA
	wecomValidateOA = func(context.Context, string, string) error { return nil }
	t.Cleanup(func() { wecomValidateOA = origOA })

	cookie, err := resolver.IssueSession(ctx, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"corpId": "ww", "secret": "s"})
	req := httptest.NewRequest(http.MethodPost, "/api/agents/"+agentID+"/channels/wecom/missing/oa", bytes.NewReader(body))
	req.SetPathValue("id", agentID)
	req.SetPathValue("accountId", "missing")
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	s.authMiddleware(s.handleConnectAgentWeComOA)(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d body = %s", rr.Code, rr.Body.String())
	}
}
