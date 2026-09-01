package setup

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/larksuite/oapi-sdk-go/v3/scene/registration"

	"github.com/fastclaw-ai/fastclaw/internal/channels"
)

// Feishu QR scan-to-create. Official Feishu/Lark Open Platform flow:
// OAuth 2.0 Device Authorization Grant (RFC 8628) via
// registration.RegisterApp. The user scans a QR in the Feishu/Lark
// phone app; the platform creates a PersonalAgent app with the bot
// capability + requested scopes/events and returns App ID + Secret.
// Docs: https://open.feishu.cn/document/mcp_open_tools/scan-to-create-an-app-in-one-click
//
//   POST   /api/agents/{id}/channels/feishu/login
//     → start the device-flow, wait for the verification URL, return
//       {sessionId, url, expireIn}. Body: {brand?: "feishu"|"lark"}.
//
//   GET    /api/agents/{id}/channels/feishu/login/status?session=<id>
//     → read the in-memory session. Returns
//       {status: wait|confirmed|expired|denied|error, connected, ...}.
//       On confirmed, persists the channel + binding (long-conn).
//
//   DELETE /api/agents/{id}/channels/feishu/login?session=<id>
//     → cancel an in-flight session (dialog closed).

const (
	feishuLoginWait      = "wait"
	feishuLoginConfirmed = "confirmed"
	feishuLoginExpired   = "expired"
	feishuLoginDenied    = "denied"
	feishuLoginError     = "error"
	feishuLoginConsumed  = "consumed"

	feishuBrandFeishu = "feishu"
	feishuBrandLark   = "lark"
)

// feishuQRWait is how long the start handler waits for OnQRCode
// before giving up. Overridden in tests.
var feishuQRWait = 15 * time.Second

// feishuRegisterApp is the official one-click create entry point.
// Tests swap this so we never hit accounts.feishu.cn.
var feishuRegisterApp = registration.RegisterApp

// feishuValidateCredentials is the post-scan /bot/v3/info lookup.
// Tests swap this so a confirmed QR session does not call Feishu.
var feishuValidateCredentials = channels.FeishuValidateCredentials

// feishuSendWelcome DMs the scanning user after persist. Tests swap
// this so a confirmed session does not call Feishu.
var feishuSendWelcome = channels.FeishuSendWelcome

// feishuLoginSession tracks one in-flight QR scan. Memory-only — the
// platform-side device_code expires in ~10 minutes anyway.
type feishuLoginSession struct {
	agentID   string
	userID    string
	scopeID   string
	cancel    context.CancelFunc
	createdAt time.Time

	mu        sync.Mutex
	url       string
	expireIn  int
	status    string
	errMsg    string
	result    *registration.RegisterAppResult
	accountID string
	botName   string
}

func (s *feishuLoginSession) setQR(url string, expireIn int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.url = url
	s.expireIn = expireIn
	if s.status == "" {
		s.status = feishuLoginWait
	}
}

func (s *feishuLoginSession) complete(result *registration.RegisterAppResult, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status == feishuLoginConfirmed || s.status == feishuLoginConsumed {
		return
	}
	if err != nil {
		var denied *registration.AccessDeniedError
		var expired *registration.ExpiredError
		switch {
		case errors.As(err, &denied):
			s.status = feishuLoginDenied
			s.errMsg = "authorization denied"
		case errors.As(err, &expired) || errors.Is(err, context.DeadlineExceeded):
			s.status = feishuLoginExpired
			s.errMsg = "QR code expired"
		case errors.Is(err, context.Canceled):
			s.status = feishuLoginExpired
			s.errMsg = "canceled"
		default:
			s.status = feishuLoginError
			s.errMsg = err.Error()
		}
		return
	}
	if result == nil || result.ClientID == "" || result.ClientSecret == "" {
		s.status = feishuLoginError
		s.errMsg = "registration returned empty credentials"
		return
	}
	s.result = result
	s.status = feishuLoginConfirmed
}

func (s *feishuLoginSession) snapshot() (status, url, errMsg, accountID, botName string, expireIn int, result *registration.RegisterAppResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status, s.url, s.errMsg, s.accountID, s.botName, s.expireIn, s.result
}

func (s *feishuLoginSession) markPersisted(appID, botName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accountID = appID
	s.botName = botName
	s.status = feishuLoginConsumed
	s.result = nil
}

type feishuLoginRegistry struct {
	mu       sync.Mutex
	sessions map[string]*feishuLoginSession
}

var feishuLogins = &feishuLoginRegistry{sessions: map[string]*feishuLoginSession{}}

func (r *feishuLoginRegistry) put(id string, s *feishuLoginSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[id] = s
	cutoff := time.Now().Add(-15 * time.Minute)
	for k, v := range r.sessions {
		if v.createdAt.Before(cutoff) {
			if v.cancel != nil {
				v.cancel()
			}
			delete(r.sessions, k)
		}
	}
}

func (r *feishuLoginRegistry) get(id string) *feishuLoginSession {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sessions[id]
}

func (r *feishuLoginRegistry) delete(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s := r.sessions[id]; s != nil && s.cancel != nil {
		s.cancel()
	}
	delete(r.sessions, id)
}

func (r *feishuLoginRegistry) cancelFor(userID, agentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, v := range r.sessions {
		if v.userID == userID && v.agentID == agentID {
			if v.cancel != nil {
				v.cancel()
			}
			delete(r.sessions, k)
		}
	}
}

type startFeishuLoginRequest struct {
	Brand string `json:"brand"`
}

func (s *Server) handleStartAgentFeishuLogin(w http.ResponseWriter, r *http.Request) {
	if !s.requireWritable(w, r) {
		return
	}
	id := r.PathValue("id")
	uid, aid, ok := s.resolveChannelBindingScope(w, r, id)
	if !ok {
		return
	}

	var req startFeishuLoginRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	brand := strings.ToLower(strings.TrimSpace(req.Brand))
	if brand == "" {
		brand = feishuBrandFeishu
	}
	if brand != feishuBrandFeishu && brand != feishuBrandLark {
		jsonResponse(w, http.StatusBadRequest, map[string]any{"error": "brand must be feishu or lark"})
		return
	}

	feishuLogins.cancelFor(uid, id)

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	sessionID := "fsq_" + uuid.NewString()
	sess := &feishuLoginSession{
		agentID:   id,
		userID:    uid,
		scopeID:   aid,
		cancel:    cancel,
		createdAt: time.Now(),
		status:    feishuLoginWait,
	}
	feishuLogins.put(sessionID, sess)

	appName := "FastClaw"
	if rec, err := s.dataStore.GetAgent(r.Context(), id); err == nil && rec != nil && strings.TrimSpace(rec.Name) != "" {
		appName = strings.TrimSpace(rec.Name)
	}

	qrReady := make(chan *registration.QRCodeInfo, 1)
	done := make(chan error, 1)
	opts := feishuRegistrationOptions(brand, appName, func(info *registration.QRCodeInfo) {
		if info == nil {
			return
		}
		sess.setQR(info.URL, info.ExpireIn)
		select {
		case qrReady <- info:
		default:
		}
	})

	go func() {
		result, err := feishuRegisterApp(ctx, opts)
		if err == nil && result != nil && result.ClientID != "" && result.ClientSecret != "" {
			// Persist here — do not wait for the dashboard poll. Closing
			// the dialog after the phone confirms used to cancel the
			// session and drop the credentials, leaving a live Feishu
			// app with no long-conn listener.
			if botName, _, pErr := s.finishFeishuQRLogin(sess, result); pErr != nil {
				slog.Warn("feishu qr login persist failed", "app", result.ClientID, "error", pErr)
				sess.complete(nil, pErr)
			} else {
				sess.complete(result, nil)
				sess.markPersisted(result.ClientID, botName)
			}
		} else {
			sess.complete(result, err)
		}
		select {
		case done <- err:
		default:
		}
	}()

	select {
	case info := <-qrReady:
		jsonResponse(w, http.StatusOK, map[string]any{
			"sessionId": sessionID,
			"url":       info.URL,
			"expireIn":  info.ExpireIn,
			"brand":     brand,
		})
	case err := <-done:
		feishuLogins.delete(sessionID)
		msg := "failed to start Feishu QR login"
		if err != nil {
			msg = err.Error()
		}
		jsonResponse(w, http.StatusBadGateway, map[string]any{"error": msg})
	case <-time.After(feishuQRWait):
		feishuLogins.delete(sessionID)
		jsonResponse(w, http.StatusBadGateway, map[string]any{"error": "timed out waiting for Feishu QR code"})
	case <-r.Context().Done():
		feishuLogins.delete(sessionID)
	}
}

func (s *Server) handleAgentFeishuLoginStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, _, ok := s.resolveChannelBindingScope(w, r, id); !ok {
		return
	}
	uid := s.effectiveUserID(r)
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		jsonResponse(w, http.StatusBadRequest, map[string]any{"error": "session required"})
		return
	}
	sess := feishuLogins.get(sessionID)
	if sess == nil || sess.userID != uid || sess.agentID != id {
		jsonResponse(w, http.StatusNotFound, map[string]any{"error": "session not found or expired"})
		return
	}

	status, _, errMsg, accountID, botName, _, _ := sess.snapshot()
	switch status {
	case feishuLoginConsumed:
		jsonResponse(w, http.StatusOK, map[string]any{
			"status":    feishuLoginConfirmed,
			"connected": true,
			"accountId": accountID,
			"botName":   botName,
			"appId":     accountID,
		})
		return
	case feishuLoginConfirmed:
		// Persist runs in the register goroutine; keep the client waiting
		// until markPersisted flips the session to consumed.
		jsonResponse(w, http.StatusOK, map[string]any{
			"status":    feishuLoginWait,
			"connected": false,
		})
		return
	case feishuLoginDenied, feishuLoginExpired, feishuLoginError:
		feishuLogins.delete(sessionID)
		resp := map[string]any{
			"status":    status,
			"connected": false,
		}
		if errMsg != "" {
			resp["error"] = errMsg
		}
		jsonResponse(w, http.StatusOK, resp)
		return
	default:
		jsonResponse(w, http.StatusOK, map[string]any{
			"status":    feishuLoginWait,
			"connected": false,
		})
	}
}

func (s *Server) handleCancelAgentFeishuLogin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, _, ok := s.resolveChannelBindingScope(w, r, id); !ok {
		return
	}
	uid := s.effectiveUserID(r)
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		jsonResponse(w, http.StatusBadRequest, map[string]any{"error": "session required"})
		return
	}
	sess := feishuLogins.get(sessionID)
	if sess == nil || sess.userID != uid || sess.agentID != id {
		jsonResponse(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	feishuLogins.delete(sessionID)
	jsonResponse(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) finishFeishuQRLogin(sess *feishuLoginSession, result *registration.RegisterAppResult) (botName, botOpenID string, err error) {
	ctx := context.Background()
	botName, botOpenID, vErr := feishuValidateCredentials(ctx, result.ClientID, result.ClientSecret)
	if vErr != nil {
		slog.Warn("feishu qr login: credentials issued but bot info fetch failed",
			"app", result.ClientID, "error", vErr)
	}
	if err := s.persistFeishuAccount(ctx, sess.userID, sess.scopeID, sess.agentID,
		result.ClientID, result.ClientSecret, "", "", true); err != nil {
		return botName, botOpenID, err
	}
	if result.UserInfo != nil && result.UserInfo.OpenID != "" {
		openID := result.UserInfo.OpenID
		appID, secret := result.ClientID, result.ClientSecret
		go func() {
			if err := feishuSendWelcome(context.Background(), appID, secret, openID); err != nil {
				slog.Warn("feishu welcome dm failed", "app", appID, "error", err)
			}
		}()
	}
	return botName, botOpenID, nil
}

func feishuRegistrationOptions(brand, appName string, onQR func(*registration.QRCodeInfo)) *registration.Options {
	// Official receive-message event requires these scopes — generic
	// im:message is not enough. See
	// https://open.feishu.cn/document/server-docs/im-v1/message/events/receive
	// and the Agent app configuration checklist:
	// https://open.feishu.cn/document/mcp_open_tools/integrating-agents-with-feishu/overview
	// Default preset stays on so the PersonalAgent template still
	// enables WebSocket long-connection event delivery.
	opts := &registration.Options{
		Source:     "fastclaw",
		CreateOnly: true,
		AppPreset: &registration.AppPreset{
			Name: "{user}'s " + appName,
			Desc: "Chat with this FastClaw agent from Feishu / Lark",
		},
		Addons: &registration.AppAddons{
			Scopes: registration.AppAddonsScopes{
				Tenant: []string{
					"im:message.p2p_msg:readonly",
					"im:message.group_at_msg:readonly",
					"im:message.group_at_msg.include_bot:readonly",
					"im:message:send_as_bot",
					"im:message:readonly",
					"im:resource",
				},
			},
			Events: registration.AppAddonsEvents{
				Items: registration.AppAddonsEventItems{
					Tenant: []string{"im.message.receive_v1"},
				},
			},
		},
		OnQRCode: onQR,
	}
	if brand == feishuBrandLark {
		opts.Domain = "https://accounts.larksuite.com"
	}
	return opts
}
