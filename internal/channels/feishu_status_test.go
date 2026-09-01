package channels

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/bus"
)

func TestFeishuTypingReactionAndReply(t *testing.T) {
	var posts, deletes, replies, sends int
	var lastEmoji, lastReplyPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/reactions"):
			posts++
			var body struct {
				ReactionType struct {
					EmojiType string `json:"emoji_type"`
				} `json:"reaction_type"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			lastEmoji = body.ReactionType.EmojiType
			_, _ = w.Write([]byte(`{"code":0,"data":{"reaction_id":"react_1"}}`))
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/reactions/"):
			deletes++
			_, _ = w.Write([]byte(`{"code":0}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/reply"):
			replies++
			lastReplyPath = r.URL.Path
			_, _ = io.Copy(io.Discard, r.Body)
			_, _ = w.Write([]byte(`{"code":0}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/im/v1/messages"):
			sends++
			_, _ = io.Copy(io.Discard, r.Body)
			_, _ = w.Write([]byte(`{"code":0}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	ch, err := NewFeishu("cli_test", "secret", "verify-token", "", false, "cli_test", bus.New())
	if err != nil {
		t.Fatalf("NewFeishu: %v", err)
	}
	ch.httpClient = server.Client()
	ch.apiBaseURL = server.URL
	ch.accessTok = "tok"
	ch.accessTokExp = time.Now().Add(time.Hour)

	event := map[string]any{
		"schema": "2.0",
		"header": map[string]any{
			"event_id":   "ev_1",
			"event_type": "im.message.receive_v1",
			"token":      "verify-token",
			"app_id":     "cli_test",
		},
		"event": map[string]any{
			"sender": map[string]any{
				"sender_id":   map[string]any{"open_id": "ou_sender"},
				"sender_type": "user",
			},
			"message": map[string]any{
				"message_id":   "om_user_1",
				"chat_id":      "oc_dm",
				"chat_type":    "p2p",
				"message_type": "text",
				"content":      `{"text":"hi"}`,
			},
		},
	}
	body, _ := json.Marshal(event)
	if _, status, err := ch.HandleWebhook(body); err != nil || status != 200 {
		t.Fatalf("HandleWebhook status=%d err=%v", status, err)
	}

	if err := ch.SendTyping("oc_dm"); err != nil {
		t.Fatalf("SendTyping: %v", err)
	}
	if posts != 1 || lastEmoji != feishuTypingEmoji {
		t.Fatalf("typing posts=%d emoji=%q", posts, lastEmoji)
	}
	if err := ch.SendTyping("oc_dm"); err != nil {
		t.Fatalf("SendTyping keepalive: %v", err)
	}
	if posts != 1 {
		t.Fatalf("keepalive re-added typing: posts=%d", posts)
	}

	if err := ch.SendMessage(bus.OutboundMessage{
		ChatID:       "oc_dm",
		Text:         "pong",
		ReplyToMsgID: "om_user_1",
	}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if replies != 1 || !strings.Contains(lastReplyPath, "/om_user_1/reply") {
		t.Fatalf("reply path=%q replies=%d sends=%d", lastReplyPath, replies, sends)
	}
	if sends != 0 {
		t.Fatalf("expected in-thread reply, not a new chat send (sends=%d)", sends)
	}
	if deletes != 1 {
		t.Fatalf("expected Typing reaction cleared, deletes=%d", deletes)
	}
}
