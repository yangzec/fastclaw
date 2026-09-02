package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/bus"
	"github.com/gorilla/websocket"
)

func (w *WeCom) connectOnce(ctx context.Context) error {
	dialCtx, cancel := context.WithTimeout(ctx, wecomDialTimeout)
	defer cancel()
	if err := w.dial(dialCtx); err != nil {
		return err
	}
	defer w.closeConn()

	runCtx, runCancel := context.WithCancel(ctx)
	defer runCancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- w.readLoop(runCtx)
	}()
	if err := w.subscribe(runCtx); err != nil {
		return err
	}
	slog.Info("wecom long-conn subscribed", "account", w.accountID)
	go w.heartbeat(runCtx)

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		return err
	}
}

func (w *WeCom) dial(ctx context.Context) error {
	d := websocket.Dialer{HandshakeTimeout: wecomDialTimeout}
	conn, _, err := d.DialContext(ctx, w.wsURL, http.Header{})
	if err != nil {
		return fmt.Errorf("wecom dial: %w", err)
	}
	w.writeMu.Lock()
	w.conn = conn
	w.writeMu.Unlock()
	return nil
}

func (w *WeCom) closeConn() {
	w.writeMu.Lock()
	conn := w.conn
	w.conn = nil
	w.writeMu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func (w *WeCom) subscribe(ctx context.Context) error {
	return w.request(ctx, wecomCmdSubscribe, wecomNewReqID(), wecomSubscribeBody(w.botID, w.secret))
}

func (w *WeCom) heartbeat(ctx context.Context) {
	t := time.NewTicker(wecomHeartbeatEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pingCtx, cancel := context.WithTimeout(ctx, wecomWriteTimeout)
			err := w.request(pingCtx, wecomCmdPing, wecomNewReqID(), nil)
			cancel()
			if err != nil && ctx.Err() == nil {
				slog.Debug("wecom ping failed", "account", w.accountID, "error", err)
			}
		}
	}
}

func (w *WeCom) request(ctx context.Context, cmd, reqID string, body any) error {
	_, err := w.requestFrame(ctx, cmd, reqID, body)
	return err
}

func (w *WeCom) requestFrame(ctx context.Context, cmd, reqID string, body any) (wecomFrame, error) {
	var zero wecomFrame
	if reqID == "" {
		reqID = wecomNewReqID()
	}
	ch := make(chan wecomFrame, 1)
	w.mu.Lock()
	w.pending[reqID] = ch
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		delete(w.pending, reqID)
		w.mu.Unlock()
	}()
	if err := w.writeFrame(cmd, reqID, body); err != nil {
		return zero, err
	}
	select {
	case <-ctx.Done():
		return zero, ctx.Err()
	case fr := <-ch:
		if fr.ErrCode != 0 {
			return fr, fmt.Errorf("wecom %s: %d %s", cmd, fr.ErrCode, fr.ErrMsg)
		}
		return fr, nil
	}
}

func (w *WeCom) writeFrame(cmd, reqID string, body any) error {
	raw, err := wecomFrameJSON(cmd, reqID, body)
	if err != nil {
		return err
	}
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	if w.conn == nil {
		return fmt.Errorf("wecom: not connected")
	}
	_ = w.conn.SetWriteDeadline(time.Now().Add(wecomWriteTimeout))
	return w.conn.WriteMessage(websocket.TextMessage, raw)
}

func (w *WeCom) readLoop(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		w.writeMu.Lock()
		conn := w.conn
		w.writeMu.Unlock()
		if conn == nil {
			return fmt.Errorf("wecom: connection closed")
		}
		_ = conn.SetReadDeadline(time.Now().Add(wecomReadTimeout))
		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		var fr wecomFrame
		if err := json.Unmarshal(data, &fr); err != nil {
			slog.Debug("wecom skip non-json frame", "account", w.accountID, "error", err)
			continue
		}
		w.dispatchFrame(fr)
	}
}

func (w *WeCom) dispatchFrame(fr wecomFrame) {
	switch fr.Cmd {
	case wecomCmdMsgCallback:
		w.handleMsgCallback(fr)
	case wecomCmdEventCallback:
		w.handleEventCallback(fr)
	default:
		w.mu.Lock()
		ch := w.pending[fr.Headers.ReqID]
		w.mu.Unlock()
		if ch != nil {
			select {
			case ch <- fr:
			default:
			}
		}
	}
}

func (w *WeCom) handleMsgCallback(fr wecomFrame) {
	var body wecomCallbackBody
	if err := json.Unmarshal(fr.Body, &body); err != nil {
		slog.Warn("wecom callback decode failed", "account", w.accountID, "error", err)
		return
	}
	text := wecomInboundText(body)
	if text == "" {
		slog.Debug("wecom unsupported message skipped", "account", w.accountID, "type", body.MsgType)
		return
	}
	chatID, group := wecomSessionChatID(body)
	if chatID == "" {
		return
	}
	w.rememberInbound(chatID, fr.Headers.ReqID, group)
	peerKind := "dm"
	if group {
		peerKind = "group"
	}
	msgID := body.MsgID
	if msgID == "" {
		msgID = fr.Headers.ReqID
	}
	slog.Info("wecom message received",
		"account", w.accountID,
		"from", body.From.UserID,
		"chat", chatID,
		"len", len(text))
	if w.bus == nil {
		return
	}
	w.bus.Inbound <- bus.InboundMessage{
		Channel:   "wecom",
		AccountID: w.accountID,
		ChatID:    chatID,
		UserID:    body.From.UserID,
		MessageID: msgID,
		Text:      text,
		PeerKind:  peerKind,
	}
}

func (w *WeCom) handleEventCallback(fr wecomFrame) {
	var body wecomCallbackBody
	if err := json.Unmarshal(fr.Body, &body); err != nil {
		slog.Warn("wecom event decode failed", "account", w.accountID, "error", err)
		return
	}
	switch body.Event.EventType {
	case wecomEventEnterChat:
		chatID, group := wecomSessionChatID(body)
		if chatID != "" {
			w.rememberInbound(chatID, fr.Headers.ReqID, group)
		}
		if err := w.respondWelcome(fr.Headers.ReqID, wecomWelcomeText); err != nil {
			slog.Warn("wecom welcome failed", "account", w.accountID, "error", err)
		}
	case wecomEventDisconnected:
		slog.Info("wecom kicked by new connection", "account", w.accountID)
	default:
		slog.Debug("wecom event ignored", "account", w.accountID, "type", body.Event.EventType)
	}
}
