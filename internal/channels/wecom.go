package channels

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/bus"
	"github.com/gorilla/websocket"
)

// WeCom (企业微信智能机器人) adapter.
//
// Official transport is the AI-bot long-connection protocol documented at
// https://developer.work.weixin.qq.com/document/path/101463
// Tencent publishes Node (`@wecom/aibot-node-sdk`) and Python SDKs for
// this JSON WebSocket; there is no official Go SDK, so this package
// speaks the same frames those SDKs send.
//
// Credentials: BotID + long-conn Secret from 管理工具 → 智能机器人 →
// API 模式 → 长连接, or from the official browser scan SDK
// `@wecom/wecom-aibot-sdk` (`openBotInfoAuthWindow`).
//
// One bot = one live socket. A second subscribe kicks the first
// (`disconnected_event`). The gateway registers this adapter as a
// singleton so dual FastClaw replicas don't fight.
//
// Storage mapping (same convention as Feishu / LINE):
//
//	accountID                = BotID
//	AccountConfig.BotToken   = long-conn Secret
//	AccountConfig.BaseURL    = optional private-deploy WS URL
//	AccountConfig.CorpID     = 自建应用 CorpID (official calendar / OA)
//	AccountConfig.CorpSecret = 自建应用 Secret
//	AccountConfig.CorpAgentID = optional 自建应用 AgentId
const (
	wecomDefaultWSURL   = "wss://openws.work.weixin.qq.com"
	wecomHeartbeatEvery = 30 * time.Second
	wecomWriteTimeout   = 10 * time.Second
	wecomReadTimeout    = 90 * time.Second
	wecomDialTimeout    = 15 * time.Second
	wecomMaxBackoff     = 30 * time.Second

	wecomCmdSubscribe      = "aibot_subscribe"
	wecomCmdPing           = "ping"
	wecomCmdRespond        = "aibot_respond_msg"
	wecomCmdRespondWelcome = "aibot_respond_welcome_msg"
	wecomCmdSend           = "aibot_send_msg"
	wecomCmdMsgCallback    = "aibot_msg_callback"
	wecomCmdEventCallback  = "aibot_event_callback"
	wecomEventEnterChat    = "enter_chat"
	wecomEventDisconnected = "disconnected_event"
	wecomTypingPlaceholder = "正在思考…"
	wecomWelcomeText       = "你好，我是 FastClaw 助手。直接发消息即可开始对话。"
)

// WeCom implements Channel for 企业微信智能机器人 long-conn.
type WeCom struct {
	bus       *bus.MessageBus
	accountID string
	botID     string
	secret    string
	wsURL     string

	writeMu sync.Mutex
	conn    *websocket.Conn

	mu       sync.Mutex
	pending  map[string]chan wecomFrame
	lastReq  map[string]string // chatID → callback req_id (24h reply window)
	chatType map[string]int    // chatID → 1 single / 2 group
	streamID map[string]string // chatID → in-flight typing/reply stream
}

type wecomFrame struct {
	Cmd     string          `json:"cmd,omitempty"`
	Headers wecomHeaders    `json:"headers"`
	Body    json.RawMessage `json:"body,omitempty"`
	ErrCode int             `json:"errcode"`
	ErrMsg  string          `json:"errmsg"`
}

type wecomHeaders struct {
	ReqID string `json:"req_id"`
}

type wecomCallbackBody struct {
	MsgID    string `json:"msgid"`
	AIBotID  string `json:"aibotid"`
	ChatID   string `json:"chatid"`
	ChatType string `json:"chattype"`
	From     struct {
		UserID string `json:"userid"`
	} `json:"from"`
	MsgType string `json:"msgtype"`
	Text    struct {
		Content string `json:"content"`
	} `json:"text"`
	Voice struct {
		Content string `json:"content"`
	} `json:"voice"`
	Mixed json.RawMessage `json:"mixed"`
	Event struct {
		EventType string `json:"eventtype"`
	} `json:"event"`
}

// NewWeCom creates a WeCom adapter. wsURL may be empty (public default)
// or a private-deploy long-conn address from the admin console.
func NewWeCom(botID, secret, wsURL, accountID string, mb *bus.MessageBus) (*WeCom, error) {
	if botID == "" || secret == "" {
		return nil, errors.New("wecom: botId and secret required")
	}
	if accountID == "" {
		accountID = botID
	}
	if wsURL == "" {
		wsURL = wecomDefaultWSURL
	}
	return &WeCom{
		bus:       mb,
		accountID: accountID,
		botID:     botID,
		secret:    secret,
		wsURL:     wsURL,
		pending:   map[string]chan wecomFrame{},
		lastReq:   map[string]string{},
		chatType:  map[string]int{},
		streamID:  map[string]string{},
	}, nil
}

func (w *WeCom) Name() string        { return "wecom" }
func (w *WeCom) AccountID() string   { return w.accountID }
func (w *WeCom) BotUsername() string { return w.botID }

// Start opens the official long-conn and reconnects until ctx is cancelled.
func (w *WeCom) Start(ctx context.Context) error {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return nil
		}
		err := w.connectOnce(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			slog.Warn("wecom long-conn dropped", "account", w.accountID, "error", err, "retry_in", backoff)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		if backoff < wecomMaxBackoff {
			backoff *= 2
			if backoff > wecomMaxBackoff {
				backoff = wecomMaxBackoff
			}
		}
	}
}

// Send posts plain text. Used by tools / test paths.
func (w *WeCom) Send(chatID, text string) error {
	return w.SendMessage(bus.OutboundMessage{ChatID: chatID, Text: text})
}

// SendMessage replies on the last inbound req_id when we have one
// (aibot_respond_msg), otherwise proactive-pushes (aibot_send_msg).
// An in-flight typing stream is finished with this text so the user
// sees one updating bubble — the official long-conn status pattern.
func (w *WeCom) SendMessage(msg bus.OutboundMessage) error {
	if strings.TrimSpace(msg.Text) == "" && len(msg.MediaItems) == 0 {
		return nil
	}
	if len(msg.MediaItems) > 0 {
		slog.Debug("wecom attachments skipped in v1", "account", w.accountID, "n", len(msg.MediaItems))
	}
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return nil
	}
	w.mu.Lock()
	reqID := w.lastReq[msg.ChatID]
	streamID := w.streamID[msg.ChatID]
	chatType := w.chatType[msg.ChatID]
	w.mu.Unlock()

	if streamID != "" && reqID != "" {
		if err := w.respondStream(reqID, streamID, text, true); err != nil {
			return err
		}
		w.clearStream(msg.ChatID)
		return nil
	}
	if reqID != "" {
		return w.respondMarkdown(reqID, text)
	}
	return w.sendMarkdown(msg.ChatID, chatType, text)
}

// SendTyping starts (or refreshes) an official stream bubble.
func (w *WeCom) SendTyping(chatID string) error {
	w.mu.Lock()
	reqID := w.lastReq[chatID]
	streamID := w.streamID[chatID]
	if streamID == "" {
		streamID = wecomNewReqID()
		w.streamID[chatID] = streamID
	}
	w.mu.Unlock()
	if reqID == "" {
		return nil
	}
	return w.respondStream(reqID, streamID, wecomTypingPlaceholder, false)
}

// ClearTyping finishes an unfinished stream when the turn produced no reply.
func (w *WeCom) ClearTyping(chatID string) error {
	w.mu.Lock()
	reqID := w.lastReq[chatID]
	streamID := w.streamID[chatID]
	w.mu.Unlock()
	if reqID == "" || streamID == "" {
		return nil
	}
	err := w.respondStream(reqID, streamID, wecomTypingPlaceholder, true)
	w.clearStream(chatID)
	return err
}

func (w *WeCom) clearStream(chatID string) {
	w.mu.Lock()
	delete(w.streamID, chatID)
	w.mu.Unlock()
}

func (w *WeCom) rememberInbound(chatID, reqID string, group bool) {
	w.mu.Lock()
	w.lastReq[chatID] = reqID
	if group {
		w.chatType[chatID] = 2
	} else {
		w.chatType[chatID] = 1
	}
	w.mu.Unlock()
}

func (w *WeCom) respondStream(reqID, streamID, content string, finish bool) error {
	body := map[string]any{
		"msgtype": "stream",
		"stream": map[string]any{
			"id":      streamID,
			"finish":  finish,
			"content": content,
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), wecomWriteTimeout)
	defer cancel()
	return w.request(ctx, wecomCmdRespond, reqID, body)
}

func (w *WeCom) respondMarkdown(reqID, content string) error {
	body := map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]any{
			"content": content,
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), wecomWriteTimeout)
	defer cancel()
	return w.request(ctx, wecomCmdRespond, reqID, body)
}

func (w *WeCom) sendMarkdown(chatID string, chatType int, content string) error {
	if chatID == "" {
		return errors.New("wecom: chat id required for proactive send")
	}
	body := map[string]any{
		"chatid":   chatID,
		"msgtype":  "markdown",
		"markdown": map[string]any{"content": content},
	}
	if chatType == 1 || chatType == 2 {
		body["chat_type"] = chatType
	}
	ctx, cancel := context.WithTimeout(context.Background(), wecomWriteTimeout)
	defer cancel()
	return w.request(ctx, wecomCmdSend, wecomNewReqID(), body)
}

func (w *WeCom) respondWelcome(reqID, content string) error {
	body := map[string]any{
		"msgtype": "text",
		"text":    map[string]any{"content": content},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return w.request(ctx, wecomCmdRespondWelcome, reqID, body)
}

// WeComValidateCredentials proves BotID + Secret by completing the
// official aibot_subscribe handshake, then disconnects. Used by the
// dashboard connect handler so a typo doesn't persist a dead bot.
func WeComValidateCredentials(ctx context.Context, botID, secret, wsURL string) error {
	w, err := NewWeCom(botID, secret, wsURL, botID, nil)
	if err != nil {
		return err
	}
	dialCtx, cancel := context.WithTimeout(ctx, wecomDialTimeout)
	defer cancel()
	if err := w.dial(dialCtx); err != nil {
		return err
	}
	defer w.closeConn()
	subCtx, subCancel := context.WithTimeout(ctx, wecomDialTimeout)
	defer subCancel()
	go w.readLoop(subCtx)
	if err := w.subscribe(subCtx); err != nil {
		return err
	}
	return nil
}

func wecomNewReqID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func wecomSessionChatID(body wecomCallbackBody) (chatID string, group bool) {
	if body.ChatType == "group" {
		return body.ChatID, true
	}
	if body.From.UserID != "" {
		return body.From.UserID, false
	}
	return body.ChatID, false
}

func wecomInboundText(body wecomCallbackBody) string {
	switch body.MsgType {
	case "text":
		return strings.TrimSpace(body.Text.Content)
	case "voice":
		return strings.TrimSpace(body.Voice.Content)
	case "mixed":
		return wecomMixedText(body.Mixed)
	default:
		return ""
	}
}

func wecomMixedText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var parsed struct {
		Items []struct {
			Type string `json:"type"`
			Text struct {
				Content string `json:"content"`
			} `json:"text"`
		} `json:"item"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ""
	}
	var parts []string
	for _, it := range parsed.Items {
		if it.Type == "text" || it.Text.Content != "" {
			if s := strings.TrimSpace(it.Text.Content); s != "" {
				parts = append(parts, s)
			}
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func wecomSubscribeBody(botID, secret string) map[string]string {
	return map[string]string{"bot_id": botID, "secret": secret}
}

func wecomFrameJSON(cmd, reqID string, body any) ([]byte, error) {
	fr := wecomFrame{Cmd: cmd, Headers: wecomHeaders{ReqID: reqID}}
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		fr.Body = raw
	}
	return json.Marshal(fr)
}
