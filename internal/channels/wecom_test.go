package channels

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/bus"
	"github.com/gorilla/websocket"
)

func TestWeComOfficialSubscribeFrame(t *testing.T) {
	raw, err := wecomFrameJSON(wecomCmdSubscribe, "req-1", wecomSubscribeBody("botA", "secB"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["cmd"] != wecomCmdSubscribe {
		t.Fatalf("cmd = %v", got["cmd"])
	}
	headers := got["headers"].(map[string]any)
	if headers["req_id"] != "req-1" {
		t.Fatalf("req_id = %v", headers["req_id"])
	}
	body := got["body"].(map[string]any)
	if body["bot_id"] != "botA" || body["secret"] != "secB" {
		t.Fatalf("body = %v", body)
	}
}

func TestWeComSessionChatIDAndText(t *testing.T) {
	dm := wecomCallbackBody{ChatType: "single", From: struct {
		UserID string `json:"userid"`
	}{UserID: "user_1"}, MsgType: "text"}
	dm.Text.Content = "  hello  "
	chat, group := wecomSessionChatID(dm)
	if chat != "user_1" || group {
		t.Fatalf("dm chat=%s group=%v", chat, group)
	}
	if wecomInboundText(dm) != "hello" {
		t.Fatalf("text = %q", wecomInboundText(dm))
	}

	grp := wecomCallbackBody{ChatType: "group", ChatID: "wr_group", From: struct {
		UserID string `json:"userid"`
	}{UserID: "user_2"}, MsgType: "text"}
	grp.Text.Content = "@Bot hi"
	chat, group = wecomSessionChatID(grp)
	if chat != "wr_group" || !group {
		t.Fatalf("group chat=%s group=%v", chat, group)
	}
}

func TestWeComLongConnReceiveAndReply(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	got := make(chan wecomFrame, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var fr wecomFrame
			if err := json.Unmarshal(data, &fr); err != nil {
				continue
			}
			got <- fr
			switch fr.Cmd {
			case wecomCmdSubscribe, wecomCmdPing, wecomCmdRespond, wecomCmdRespondWelcome, wecomCmdSend:
				reply, _ := json.Marshal(wecomFrame{
					Headers: wecomHeaders{ReqID: fr.Headers.ReqID},
					ErrCode: 0,
					ErrMsg:  "ok",
				})
				_ = conn.WriteMessage(websocket.TextMessage, reply)
			}
			if fr.Cmd == wecomCmdSubscribe {
				cb, _ := json.Marshal(wecomFrame{
					Cmd:     wecomCmdMsgCallback,
					Headers: wecomHeaders{ReqID: "cb-req"},
					Body:    json.RawMessage(`{"msgid":"m1","chattype":"single","from":{"userid":"u1"},"msgtype":"text","text":{"content":"ping"}}`),
				})
				_ = conn.WriteMessage(websocket.TextMessage, cb)
			}
		}
	}))
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	mb := bus.New()
	ch, err := NewWeCom("bot_test", "secret_test", wsURL, "bot_test", mb)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = ch.Start(ctx) }()

	select {
	case inbound := <-mb.Inbound:
		if inbound.Channel != "wecom" || inbound.ChatID != "u1" || inbound.Text != "ping" || inbound.PeerKind != "dm" {
			t.Fatalf("inbound = %+v", inbound)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for inbound")
	}

	if err := ch.SendTyping("u1"); err != nil {
		t.Fatalf("SendTyping: %v", err)
	}
	if err := ch.SendMessage(bus.OutboundMessage{ChatID: "u1", Text: "pong"}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	var sawSubscribe, sawStream, sawFinish bool
	deadline := time.After(3 * time.Second)
	for !sawSubscribe || !sawFinish {
		select {
		case fr := <-got:
			switch fr.Cmd {
			case wecomCmdSubscribe:
				sawSubscribe = true
			case wecomCmdRespond:
				var body struct {
					MsgType string `json:"msgtype"`
					Stream  struct {
						ID      string `json:"id"`
						Finish  bool   `json:"finish"`
						Content string `json:"content"`
					} `json:"stream"`
				}
				_ = json.Unmarshal(fr.Body, &body)
				if body.MsgType == "stream" && !body.Stream.Finish {
					sawStream = true
				}
				if body.MsgType == "stream" && body.Stream.Finish && body.Stream.Content == "pong" {
					sawFinish = true
				}
			}
		case <-deadline:
			t.Fatalf("frames subscribe=%v stream=%v finish=%v", sawSubscribe, sawStream, sawFinish)
		}
	}
}

func TestWeComValidateCredentialsRejectsBadSecret(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var fr wecomFrame
		_ = json.Unmarshal(data, &fr)
		reply, _ := json.Marshal(wecomFrame{
			Headers: wecomHeaders{ReqID: fr.Headers.ReqID},
			ErrCode: 40014,
			ErrMsg:  "invalid secret",
		})
		_ = conn.WriteMessage(websocket.TextMessage, reply)
	}))
	t.Cleanup(srv.Close)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	err := WeComValidateCredentials(context.Background(), "botX", "bad", wsURL)
	if err == nil || !strings.Contains(err.Error(), "40014") {
		t.Fatalf("want invalid-secret error, got %v", err)
	}
}
