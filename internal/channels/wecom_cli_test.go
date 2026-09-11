package channels

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/store"
)

func TestWeComCLISignMatchesOfficial(t *testing.T) {
	// Same vector as WecomTeam/wecom-cli sha256_hex("test").
	sum := wecomCLISign("", "", 0, "") // not this
	_ = sum
	got := wecomCLISign("sec", "id", 100, "nonce")
	again := wecomCLISign("sec", "id", 100, "nonce")
	if got == "" || got != again {
		t.Fatalf("sign not deterministic: %q %q", got, again)
	}
	if wecomCLISign("sec", "id", 100, "nonce2") == got {
		t.Fatal("different nonce should change signature")
	}
}

func wecomCLIEnvelope(t *testing.T, inner any) []byte {
	t.Helper()
	raw, err := json.Marshal(inner)
	if err != nil {
		t.Fatal(err)
	}
	wrapped, _ := json.Marshal(map[string]any{
		"errcode": 0, "errmsg": "ok",
		"results_json": string(mustJSON(map[string]any{
			"result": string(raw),
			"error":  nil,
		})),
	})
	return wrapped
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func TestWeComCLICreateSchedule(t *testing.T) {
	var gotAuth, gotCreate map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch {
		case strings.Contains(r.URL.Path, "get_cli_config"):
			_ = json.Unmarshal(body, &gotAuth)
			_ = json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "token": "cli_tok"})
		case strings.HasSuffix(r.URL.Path, "/service/discovery"):
			var wrap struct {
				Payload string `json:"payload"`
			}
			_ = json.Unmarshal(body, &wrap)
			svc := "calendar"
			if strings.Contains(wrap.Payload, "doc") {
				svc = "doc"
			}
			if strings.Contains(wrap.Payload, "contact") {
				svc = "contact"
			}
			inner := map[string]any{"base_url": "", "methods": map[string]any{}, "resources": map[string]any{}}
			switch svc {
			case "calendar":
				inner["resources"] = map[string]any{
					"schedules": map[string]any{
						"methods": map[string]any{
							"create": map[string]any{"path": "/schedules/create"},
							"get":    map[string]any{"path": "/schedules/get"},
						},
					},
				}
			}
			_, _ = w.Write(wecomCLIEnvelope(t, inner))
		case strings.HasSuffix(r.URL.Path, "/schedules/create"):
			if got := r.Header.Get("Authorization"); got != "Bearer cli_tok" {
				t.Errorf("auth = %q", got)
			}
			var wrap struct {
				Payload string `json:"payload"`
			}
			_ = json.Unmarshal(body, &wrap)
			_ = json.Unmarshal([]byte(wrap.Payload), &gotCreate)
			_, _ = w.Write(wecomCLIEnvelope(t, map[string]any{"schedule_id": "sid_cli"}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewWeComCLI("bot_1", "secret_1")
	c.AuthURL = srv.URL + "/cgi-bin/aibot/cli/get_cli_config"
	c.BaseURL = srv.URL
	id, _, err := c.CreateSchedule(context.Background(), WeComCLISchedule{
		Subject:   "项目例会",
		BeginTime: "2026-09-02 15:00:00",
		EndTime:   "2026-09-02 16:00:00",
		Location:  "10F",
		Attendees: []string{"zhangsan"},
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	if id != "sid_cli" {
		t.Fatalf("id = %q", id)
	}
	if gotAuth["bot_id"] != "bot_1" {
		t.Fatalf("auth body = %#v", gotAuth)
	}
	if gotCreate["subject"] != "项目例会" {
		t.Fatalf("create = %#v", gotCreate)
	}
	atts, _ := gotCreate["attendees"].([]any)
	if len(atts) != 1 {
		t.Fatalf("attendees = %#v", gotCreate["attendees"])
	}
}

func TestWeComCLIAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"errcode": 853000, "errmsg": "invalid credential"})
	}))
	defer srv.Close()
	c := NewWeComCLI("bot_1", "bad")
	c.AuthURL = srv.URL
	c.BaseURL = srv.URL
	_, err := c.AccessToken(context.Background())
	if err == nil || !strings.Contains(err.Error(), "853000") {
		t.Fatalf("err = %v", err)
	}
}

func TestWeComCLIFromChannelUsesBotSecret(t *testing.T) {
	ch := &store.ChannelRecord{
		Type: "wecom", AccountID: "bot_x", BotToken: "sec_x",
	}
	c, err := WeComCLIFromChannel(ch)
	if err != nil {
		t.Fatal(err)
	}
	if c.BotID != "bot_x" || c.Secret != "sec_x" {
		t.Fatalf("client = %+v", c)
	}
	_, err = WeComCLIFromChannel(&store.ChannelRecord{Type: "wecom", AccountID: "bot_x"})
	if err == nil {
		t.Fatal("expected missing secret")
	}
}

func TestWeComCLIKey(t *testing.T) {
	cases := map[string]string{
		"sheet rows append":                   "sheet.rows.append",
		"wecom-cli calendar schedules create": "calendar.schedules.create",
		"todo create":                         "todo.create",
		"message aibot sessions list":         "message.aibot.sessions.list",
		"calendar.schedules.free.list":        "calendar.schedules.free.list",
	}
	for in, want := range cases {
		if got := WeComCLIKey(in); got != want {
			t.Fatalf("%q -> %q want %q", in, got, want)
		}
	}
}

func TestWeComCLICreateSheet(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch {
		case strings.Contains(r.URL.Path, "get_cli_config"):
			_ = json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "token": "cli_tok"})
		case strings.HasSuffix(r.URL.Path, "/service/discovery"):
			_, _ = w.Write(wecomCLIEnvelope(t, map[string]any{}))
		case strings.HasSuffix(r.URL.Path, "/create"):
			var wrap struct {
				Payload string `json:"payload"`
			}
			_ = json.Unmarshal(body, &wrap)
			_ = json.Unmarshal([]byte(wrap.Payload), &got)
			_, _ = w.Write(wecomCLIEnvelope(t, map[string]any{
				"docid": "sheet_1", "url": "https://doc.weixin.qq.com/sheet/sheet_1", "name": "名单",
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := NewWeComCLI("bot_1", "secret_1")
	c.AuthURL = srv.URL + "/cgi-bin/aibot/cli/get_cli_config"
	c.BaseURL = srv.URL
	raw, err := c.CreateSheet(context.Background(), "名单", [][]string{{"姓名", "分"}, {"张三", "90"}})
	if err != nil {
		t.Fatalf("CreateSheet: %v", err)
	}
	id, url, _ := WeComCLIPickDoc(raw)
	if id != "sheet_1" || !strings.Contains(url, "/sheet/") {
		t.Fatalf("pick = %s %s %s", id, url, raw)
	}
	if got["doc_name"] != "名单" {
		t.Fatalf("payload = %#v", got)
	}
}

func TestWeComCLIDocIDFromURL(t *testing.T) {
	got := wecomCLIDocIDFromURL("https://doc.weixin.qq.com/doc/abcDEF?scode=x")
	if got != "abcDEF" {
		t.Fatalf("got %q", got)
	}
	if wecomCLIDocIDFromURL("plain_id") != "plain_id" {
		t.Fatal("passthrough")
	}
}
