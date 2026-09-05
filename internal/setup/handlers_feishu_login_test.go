package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/larksuite/oapi-sdk-go/v3/scene/registration"

	"github.com/fastclaw-ai/fastclaw/internal/store"
)

func TestFeishuQRLoginCreatesChannelOnConfirm(t *testing.T) {
	ctx := context.Background()
	s, resolver, admin, _ := newAuthTestServer(t, ctx)
	agentID := "agent-feishu-qr"
	if err := s.dataStore.SaveAgent(ctx, &store.AgentRecord{
		ID: agentID, UserID: admin.ID, Name: "Office Bot",
	}); err != nil {
		t.Fatalf("SaveAgent: %v", err)
	}

	release := make(chan struct{})
	var captured *registration.Options
	restore := swapFeishuLoginHooks(
		func(_ context.Context, opts *registration.Options) (*registration.RegisterAppResult, error) {
			captured = opts
			if opts.OnQRCode != nil {
				opts.OnQRCode(&registration.QRCodeInfo{
					URL:      "https://accounts.feishu.cn/oauth/v1/app/registration?user_code=ABC",
					ExpireIn: 600,
				})
			}
			<-release
			return &registration.RegisterAppResult{
				ClientID:     "cli_qr_test_app",
				ClientSecret: "secret-from-scan",
			}, nil
		},
		func(context.Context, string, string) (string, string, error) {
			return "Office Bot", "ou_scanner", nil
		},
		func(context.Context, string, string, string) error { return nil },
	)
	defer restore()
	t.Cleanup(func() { close(release) })

	start := doFeishuLogin(t, s, resolver, admin.ID, http.MethodPost,
		"/api/agents/"+agentID+"/channels/feishu/login", agentID, map[string]any{})
	if start.Code != http.StatusOK {
		t.Fatalf("start status = %d body = %s", start.Code, start.Body.String())
	}
	var started struct {
		SessionID string `json:"sessionId"`
		URL       string `json:"url"`
		ExpireIn  int    `json:"expireIn"`
	}
	if err := json.Unmarshal(start.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode start: %v", err)
	}
	if started.SessionID == "" || started.URL == "" || started.ExpireIn != 600 {
		t.Fatalf("start payload = %+v", started)
	}
	if captured == nil || captured.Source != "fastclaw" || !captured.CreateOnly {
		t.Fatalf("registration options = %+v", captured)
	}
	if captured.AppPreset == nil || captured.AppPreset.Name != "{user}'s Office Bot" {
		t.Fatalf("app preset = %+v", captured.AppPreset)
	}
	if !containsStr(captured.Addons.Events.Items.Tenant, "im.message.receive_v1") {
		t.Fatalf("events = %v", captured.Addons.Events.Items.Tenant)
	}
	if !containsStr(captured.Addons.Scopes.Tenant, "im:message.p2p_msg:readonly") {
		t.Fatalf("missing p2p receive scope: %v", captured.Addons.Scopes.Tenant)
	}
	if !containsStr(captured.Addons.Callbacks.Items, "card.action.trigger") {
		t.Fatalf("missing card callback: %v", captured.Addons.Callbacks.Items)
	}

	wait := doFeishuLogin(t, s, resolver, admin.ID, http.MethodGet,
		"/api/agents/"+agentID+"/channels/feishu/login/status?session="+started.SessionID,
		agentID, nil)
	if wait.Code != http.StatusOK {
		t.Fatalf("wait status = %d body = %s", wait.Code, wait.Body.String())
	}
	if bodyJSON(t, wait)["status"] != feishuLoginWait {
		t.Fatalf("expected wait, got %s", wait.Body.String())
	}

	release <- struct{}{}
	deadline := time.Now().Add(2 * time.Second)
	var confirmed map[string]any
	for {
		poll := doFeishuLogin(t, s, resolver, admin.ID, http.MethodGet,
			"/api/agents/"+agentID+"/channels/feishu/login/status?session="+started.SessionID,
			agentID, nil)
		if poll.Code != http.StatusOK {
			t.Fatalf("poll status = %d body = %s", poll.Code, poll.Body.String())
		}
		confirmed = bodyJSON(t, poll)
		if confirmed["status"] == feishuLoginConfirmed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("never confirmed: %s", poll.Body.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if confirmed["connected"] != true || confirmed["accountId"] != "cli_qr_test_app" {
		t.Fatalf("confirm payload = %v", confirmed)
	}
	if confirmed["botName"] != "Office Bot" {
		t.Fatalf("botName = %v", confirmed["botName"])
	}

	ch, err := s.dataStore.LookupChannel(ctx, "feishu", "cli_qr_test_app")
	if err != nil || ch == nil {
		t.Fatalf("LookupChannel: ch=%v err=%v", ch, err)
	}
	if ch.BotToken != "secret-from-scan" || ch.UserID != admin.ID || ch.AgentID != agentID {
		t.Fatalf("saved channel = %+v", ch)
	}
	if !channelUsesLongConn(ch) {
		t.Fatalf("expected long-conn on QR-created channel, data=%v", ch.Data)
	}
}

func TestFeishuQRLoginDeniedAndExpired(t *testing.T) {
	ctx := context.Background()
	s, resolver, admin, _ := newAuthTestServer(t, ctx)
	agentID := "agent-feishu-denied"
	if err := s.dataStore.SaveAgent(ctx, &store.AgentRecord{
		ID: agentID, UserID: admin.ID, Name: "Denied",
	}); err != nil {
		t.Fatalf("SaveAgent: %v", err)
	}

	cases := []struct {
		name   string
		err    error
		status string
	}{
		{
			name: "denied",
			err: &registration.AccessDeniedError{RegisterAppError: &registration.RegisterAppError{
				Code: "access_denied", Description: "nope",
			}},
			status: feishuLoginDenied,
		},
		{
			name: "expired",
			err: &registration.ExpiredError{RegisterAppError: &registration.RegisterAppError{
				Code: "expired_token", Description: "too late",
			}},
			status: feishuLoginExpired,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			release := make(chan struct{})
			restore := swapFeishuLoginHooks(
				func(_ context.Context, opts *registration.Options) (*registration.RegisterAppResult, error) {
					opts.OnQRCode(&registration.QRCodeInfo{URL: "https://example.test/qr", ExpireIn: 60})
					<-release
					return nil, tc.err
				},
				func(context.Context, string, string) (string, string, error) {
					return "", "", nil
				},
				func(context.Context, string, string, string) error { return nil },
			)
			defer restore()

			start := doFeishuLogin(t, s, resolver, admin.ID, http.MethodPost,
				"/api/agents/"+agentID+"/channels/feishu/login", agentID, map[string]any{})
			if start.Code != http.StatusOK {
				t.Fatalf("start status = %d body = %s", start.Code, start.Body.String())
			}
			sessionID := bodyJSON(t, start)["sessionId"].(string)
			close(release)

			deadline := time.Now().Add(2 * time.Second)
			for {
				poll := doFeishuLogin(t, s, resolver, admin.ID, http.MethodGet,
					"/api/agents/"+agentID+"/channels/feishu/login/status?session="+sessionID,
					agentID, nil)
				got := bodyJSON(t, poll)
				if got["status"] == tc.status {
					if got["connected"] != false {
						t.Fatalf("expected not connected: %v", got)
					}
					return
				}
				if time.Now().After(deadline) {
					t.Fatalf("wanted status %s, last=%v", tc.status, got)
				}
				time.Sleep(20 * time.Millisecond)
			}
		})
	}
}

func TestFeishuQRLoginSessionIsolationAndCancel(t *testing.T) {
	ctx := context.Background()
	s, resolver, admin, regular := newAuthTestServer(t, ctx)
	agentID := "agent-feishu-iso"
	if err := s.dataStore.SaveAgent(ctx, &store.AgentRecord{
		ID: agentID, UserID: admin.ID, Name: "Iso",
	}); err != nil {
		t.Fatalf("SaveAgent: %v", err)
	}

	block := make(chan struct{})
	restore := swapFeishuLoginHooks(
		func(ctx context.Context, opts *registration.Options) (*registration.RegisterAppResult, error) {
			opts.OnQRCode(&registration.QRCodeInfo{URL: "https://example.test/qr", ExpireIn: 60})
			select {
			case <-block:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return nil, errors.New("should have been canceled")
		},
		func(context.Context, string, string) (string, string, error) { return "", "", nil },
		func(context.Context, string, string, string) error { return nil },
	)
	defer restore()
	t.Cleanup(func() { close(block) })

	start := doFeishuLogin(t, s, resolver, admin.ID, http.MethodPost,
		"/api/agents/"+agentID+"/channels/feishu/login", agentID, map[string]any{})
	if start.Code != http.StatusOK {
		t.Fatalf("start status = %d body = %s", start.Code, start.Body.String())
	}
	sessionID := bodyJSON(t, start)["sessionId"].(string)

	foreign := doFeishuLogin(t, s, resolver, regular.ID, http.MethodGet,
		"/api/agents/"+agentID+"/channels/feishu/login/status?session="+sessionID,
		agentID, nil)
	if foreign.Code != http.StatusNotFound && foreign.Code != http.StatusForbidden {
		t.Fatalf("foreign poll status = %d body = %s", foreign.Code, foreign.Body.String())
	}

	cancel := doFeishuLogin(t, s, resolver, admin.ID, http.MethodDelete,
		"/api/agents/"+agentID+"/channels/feishu/login?session="+sessionID,
		agentID, nil)
	if cancel.Code != http.StatusOK {
		t.Fatalf("cancel status = %d body = %s", cancel.Code, cancel.Body.String())
	}
	if feishuLogins.get(sessionID) != nil {
		t.Fatal("session still registered after cancel")
	}
}

func TestFeishuQRLoginLarkBrandSetsDomain(t *testing.T) {
	ctx := context.Background()
	s, resolver, admin, _ := newAuthTestServer(t, ctx)
	agentID := "agent-feishu-lark"
	if err := s.dataStore.SaveAgent(ctx, &store.AgentRecord{
		ID: agentID, UserID: admin.ID, Name: "Lark",
	}); err != nil {
		t.Fatalf("SaveAgent: %v", err)
	}

	var captured *registration.Options
	restore := swapFeishuLoginHooks(
		func(_ context.Context, opts *registration.Options) (*registration.RegisterAppResult, error) {
			captured = opts
			opts.OnQRCode(&registration.QRCodeInfo{URL: "https://accounts.larksuite.com/x", ExpireIn: 30})
			return nil, context.Canceled
		},
		func(context.Context, string, string) (string, string, error) { return "", "", nil },
		func(context.Context, string, string, string) error { return nil },
	)
	defer restore()

	start := doFeishuLogin(t, s, resolver, admin.ID, http.MethodPost,
		"/api/agents/"+agentID+"/channels/feishu/login", agentID, map[string]any{"brand": "lark"})
	if start.Code != http.StatusOK {
		t.Fatalf("start status = %d body = %s", start.Code, start.Body.String())
	}
	if captured == nil || captured.Domain != "https://accounts.larksuite.com" {
		t.Fatalf("lark domain not set: %+v", captured)
	}
}

func TestFeishuRegistrationOptionsDefaults(t *testing.T) {
	opts := feishuRegistrationOptions(feishuBrandFeishu, "Demo", func(*registration.QRCodeInfo) {})
	if opts.Domain != "" {
		t.Fatalf("feishu brand should keep SDK default domain, got %q", opts.Domain)
	}
	if opts.Source != "fastclaw" || !opts.CreateOnly {
		t.Fatalf("opts = %+v", opts)
	}
	for _, scope := range []string{
		"im:message.p2p_msg:readonly",
		"im:message.group_at_msg:readonly",
		"im:message:send_as_bot",
		"im:message:update",
		"cardkit:card:write",
		"application:bot.basic_info:read",
		"im:chat.members:bot_access",
		"calendar:calendar",
		"calendar:calendar.event:create",
		"task:task:read",
		"task:task:write",
		"task:task:readonly",
		"task:task",
		"docx:document:create",
		"docs:permission.member:create",
	} {
		if !containsStr(opts.Addons.Scopes.Tenant, scope) {
			t.Fatalf("missing scope %s in %v", scope, opts.Addons.Scopes.Tenant)
		}
	}
	if !containsStr(opts.Addons.Scopes.User, "offline_access") {
		t.Fatalf("missing user scope offline_access: %v", opts.Addons.Scopes.User)
	}
	for _, ev := range []string{
		"im.message.receive_v1",
		"im.chat.member.bot.added_v1",
		"drive.notice.comment_add_v1",
	} {
		if !containsStr(opts.Addons.Events.Items.Tenant, ev) {
			t.Fatalf("missing event %s in %v", ev, opts.Addons.Events.Items.Tenant)
		}
	}
	if !containsStr(opts.Addons.Callbacks.Items, "card.action.trigger") {
		t.Fatalf("missing card callback: %v", opts.Addons.Callbacks.Items)
	}
}

func TestFeishuQRLoginPersistsWithoutPoll(t *testing.T) {
	ctx := context.Background()
	s, resolver, admin, _ := newAuthTestServer(t, ctx)
	agentID := "agent-feishu-nopoll"
	if err := s.dataStore.SaveAgent(ctx, &store.AgentRecord{
		ID: agentID, UserID: admin.ID, Name: "No Poll",
	}); err != nil {
		t.Fatalf("SaveAgent: %v", err)
	}

	release := make(chan struct{})
	restore := swapFeishuLoginHooks(
		func(_ context.Context, opts *registration.Options) (*registration.RegisterAppResult, error) {
			opts.OnQRCode(&registration.QRCodeInfo{URL: "https://example.test/qr", ExpireIn: 60})
			<-release
			return &registration.RegisterAppResult{
				ClientID:     "cli_nopoll",
				ClientSecret: "secret-nopoll",
				UserInfo:     &registration.UserInfo{OpenID: "ou_scanner"},
			}, nil
		},
		func(context.Context, string, string) (string, string, error) {
			return "No Poll Bot", "ou_bot", nil
		},
		func(context.Context, string, string, string) error { return nil },
	)
	defer restore()
	t.Cleanup(func() { close(release) })

	start := doFeishuLogin(t, s, resolver, admin.ID, http.MethodPost,
		"/api/agents/"+agentID+"/channels/feishu/login", agentID, map[string]any{})
	if start.Code != http.StatusOK {
		t.Fatalf("start status = %d body = %s", start.Code, start.Body.String())
	}
	release <- struct{}{}

	deadline := time.Now().Add(2 * time.Second)
	for {
		ch, err := s.dataStore.LookupChannel(ctx, "feishu", "cli_nopoll")
		if err == nil && ch != nil && ch.BotToken == "secret-nopoll" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("credentials were not persisted without a status poll, last err=%v ch=%v", err, ch)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func swapFeishuLoginHooks(
	reg func(context.Context, *registration.Options) (*registration.RegisterAppResult, error),
	validate func(context.Context, string, string) (string, string, error),
	welcome func(context.Context, string, string, string) error,
) (restore func()) {
	origReg := feishuRegisterApp
	origVal := feishuValidateCredentials
	origWelcome := feishuSendWelcome
	origWait := feishuQRWait
	feishuRegisterApp = reg
	feishuValidateCredentials = validate
	if welcome != nil {
		feishuSendWelcome = welcome
	} else {
		feishuSendWelcome = func(context.Context, string, string, string) error { return nil }
	}
	feishuQRWait = 2 * time.Second
	return func() {
		feishuRegisterApp = origReg
		feishuValidateCredentials = origVal
		feishuSendWelcome = origWelcome
		feishuQRWait = origWait
	}
}

func containsStr(xs []string, want string) bool {
	for _, s := range xs {
		if s == want {
			return true
		}
	}
	return false
}

func doFeishuLogin(t *testing.T, s *Server, resolver interface {
	IssueSession(ctx context.Context, userID string) (*http.Cookie, error)
}, userID, method, path, agentID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		blob, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reader = bytes.NewReader(blob)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.SetPathValue("id", agentID)
	cookie, err := resolver.IssueSession(req.Context(), userID)
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	switch method {
	case http.MethodPost:
		s.authMiddleware(s.handleStartAgentFeishuLogin)(rr, req)
	case http.MethodGet:
		s.authMiddleware(s.handleAgentFeishuLoginStatus)(rr, req)
	case http.MethodDelete:
		s.authMiddleware(s.handleCancelAgentFeishuLogin)(rr, req)
	default:
		t.Fatalf("method %s", method)
	}
	return rr
}

func bodyJSON(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %q: %v", rr.Body.String(), err)
	}
	return out
}

func channelUsesLongConn(ch *store.ChannelRecord) bool {
	if ch == nil || ch.Data == nil {
		return false
	}
	accounts, _ := ch.Data["accounts"].(map[string]any)
	acct, _ := accounts[ch.AccountID].(map[string]any)
	v, _ := acct["useLongConn"].(bool)
	return v
}
