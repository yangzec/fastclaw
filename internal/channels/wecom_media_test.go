package channels

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/bus"
	"github.com/gorilla/websocket"
)

func TestWeComMediaKind(t *testing.T) {
	cases := []struct {
		name string
		item bus.MediaItem
		want string
	}{
		{"png", bus.MediaItem{Filename: "a.png", ContentType: "image/png"}, "image"},
		{"pdf", bus.MediaItem{Filename: "a.pdf", ContentType: "application/pdf"}, "file"},
		{"mp4", bus.MediaItem{Filename: "a.mp4"}, "video"},
		{"webp stays file", bus.MediaItem{Filename: "a.webp", ContentType: "image/webp"}, "file"},
	}
	for _, tc := range cases {
		if got := wecomMediaKind(tc.item); got != tc.want {
			t.Fatalf("%s: got %s want %s", tc.name, got, tc.want)
		}
	}
}

func TestWeComSendMediaUploadsAndResponds(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	got := make(chan wecomFrame, 16)
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
			body := json.RawMessage(`{}`)
			switch fr.Cmd {
			case wecomCmdUploadInit:
				body = json.RawMessage(`{"upload_id":"up_1"}`)
			case wecomCmdUploadFinish:
				body = json.RawMessage(`{"media_id":"mid_1"}`)
			}
			reply, _ := json.Marshal(wecomFrame{
				Headers: wecomHeaders{ReqID: fr.Headers.ReqID},
				Body:    body,
				ErrCode: 0,
				ErrMsg:  "ok",
			})
			_ = conn.WriteMessage(websocket.TextMessage, reply)
		}
	}))
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	ch, err := NewWeCom("bot_test", "secret_test", wsURL, "bot_test", bus.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = ch.Start(ctx) }()

	deadline := time.After(3 * time.Second)
	for {
		select {
		case fr := <-got:
			if fr.Cmd == wecomCmdSubscribe {
				goto subscribed
			}
		case <-deadline:
			t.Fatal("timeout waiting for subscribe")
		}
	}
subscribed:
	ch.rememberInbound("u1", "req-media", false)
	err = ch.SendMessage(bus.OutboundMessage{
		ChatID: "u1",
		Text:   "这是文件",
		MediaItems: []bus.MediaItem{{
			Filename:    "report.pdf",
			ContentType: "application/pdf",
			Bytes:       []byte("hello-wecom-file"),
		}},
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	var sawInit, sawChunk, sawFinish, sawFile bool
	deadline = time.After(3 * time.Second)
	for !sawInit || !sawChunk || !sawFinish || !sawFile {
		select {
		case fr := <-got:
			switch fr.Cmd {
			case wecomCmdUploadInit:
				var body map[string]any
				_ = json.Unmarshal(fr.Body, &body)
				if body["type"] != "file" || body["filename"] != "report.pdf" {
					t.Fatalf("init body = %#v", body)
				}
				sawInit = true
			case wecomCmdUploadChunk:
				var body map[string]any
				_ = json.Unmarshal(fr.Body, &body)
				raw, _ := body["base64_data"].(string)
				decoded, _ := base64.StdEncoding.DecodeString(raw)
				if string(decoded) != "hello-wecom-file" {
					t.Fatalf("chunk = %q", decoded)
				}
				sawChunk = true
			case wecomCmdUploadFinish:
				sawFinish = true
			case wecomCmdRespond:
				var body map[string]any
				_ = json.Unmarshal(fr.Body, &body)
				if body["msgtype"] == "file" {
					file, _ := body["file"].(map[string]any)
					if file["media_id"] != "mid_1" {
						t.Fatalf("file respond = %#v", body)
					}
					sawFile = true
				}
			}
		case <-deadline:
			t.Fatalf("upload frames init=%v chunk=%v finish=%v file=%v", sawInit, sawChunk, sawFinish, sawFile)
		}
	}
}
