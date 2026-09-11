package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/channels"
	"github.com/fastclaw-ai/fastclaw/internal/store"
)

func TestParseWeComWhen(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	unix, day, err := parseWeComWhen("2026-09-02 15:00", loc)
	if err != nil || day {
		t.Fatalf("got %d day=%v err=%v", unix, day, err)
	}
	want := time.Date(2026, 9, 2, 15, 0, 0, 0, loc).Unix()
	if unix != want {
		t.Fatalf("unix = %d want %d", unix, want)
	}
	unix, day, err = parseWeComWhen("2026-09-02", loc)
	if err != nil || !day {
		t.Fatalf("date-only: %d day=%v err=%v", unix, day, err)
	}
	if _, _, err := parseWeComWhen("nope", loc); err == nil {
		t.Fatal("expected parse error")
	}
}

func wecomCLITestEnvelope(inner any) []byte {
	raw, _ := json.Marshal(inner)
	outer, _ := json.Marshal(map[string]any{
		"errcode": 0, "errmsg": "ok",
		"results_json": string(func() []byte {
			b, _ := json.Marshal(map[string]any{"result": string(raw), "error": nil})
			return b
		}()),
	})
	return outer
}

func TestWeComCreateScheduleUsesCLIAndInvitesSender(t *testing.T) {
	var gotCreate map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch {
		case strings.Contains(r.URL.Path, "get_cli_config"):
			_ = json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "token": "cli_tok"})
		case strings.HasSuffix(r.URL.Path, "/service/discovery"):
			_, _ = w.Write(wecomCLITestEnvelope(map[string]any{
				"resources": map[string]any{
					"schedules": map[string]any{
						"methods": map[string]any{"create": map[string]any{"path": "/schedules/create"}},
					},
				},
			}))
		case strings.HasSuffix(r.URL.Path, "/schedules/create"):
			var wrap struct {
				Payload string `json:"payload"`
			}
			_ = json.Unmarshal(body, &wrap)
			_ = json.Unmarshal([]byte(wrap.Payload), &gotCreate)
			_, _ = w.Write(wecomCLITestEnvelope(map[string]any{"schedule_id": "sid_9"}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	db, err := store.NewDBStore("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveChannel(ctx, &store.ChannelRecord{
		UserID: "user-1", AgentID: "agent-1", Type: "wecom",
		AccountID: "bot_1", Enabled: true, BotToken: "long-conn",
	}); err != nil {
		t.Fatal(err)
	}

	orig := wecomCLIFromChannel
	wecomCLIFromChannel = func(ch *store.ChannelRecord) (*channels.WeComCLI, error) {
		c, err := channels.WeComCLIFromChannel(ch)
		if err != nil {
			return nil, err
		}
		c.AuthURL = srv.URL + "/cgi-bin/aibot/cli/get_cli_config"
		c.BaseURL = srv.URL
		return c, nil
	}
	t.Cleanup(func() { wecomCLIFromChannel = orig })

	r := NewRegistry(t.TempDir(), t.TempDir())
	r.SetOwnerUserID("user-1")
	r.SetChatterUserID("user-1")
	r.SetMessageContext("wecom", "bot_1", "zhangsan")
	RegisterWeComOfficeTools(r, db, "agent-1")

	out, err := r.Execute(ctx, "wecom_create_schedule", `{
		"summary":"项目例会",
		"start":"2026-09-02T15:00:00+08:00",
		"end":"2026-09-02T16:00:00+08:00",
		"location":"10F"
	}`)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.Contains(out, "sid_9") || !strings.Contains(out, "zhangsan") {
		t.Fatalf("result = %s", out)
	}
	if gotCreate["subject"] != "项目例会" {
		t.Fatalf("payload = %#v", gotCreate)
	}
}

func TestWeComCreateScheduleRequiresBot(t *testing.T) {
	db, err := store.NewDBStore("sqlite", "file:wecom-cli-missing?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	r := NewRegistry(t.TempDir(), t.TempDir())
	r.SetOwnerUserID("user-1")
	r.SetMessageContext("wecom", "bot_1", "u1")
	RegisterWeComOfficeTools(r, db, "agent-1")
	_, err = r.Execute(ctx, "wecom_create_schedule", `{"summary":"x","start":"2026-09-02T15:00:00Z"}`)
	if err == nil || !strings.Contains(err.Error(), "not connected") && !strings.Contains(err.Error(), "no WeCom bot") {
		t.Fatalf("want missing bot error, got %v", err)
	}
}

func TestWeComCreateDocAndCancelConfirm(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "get_cli_config"):
			_ = json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "token": "cli_tok"})
		case strings.HasSuffix(r.URL.Path, "/service/discovery"):
			_, _ = w.Write(wecomCLITestEnvelope(map[string]any{}))
		case strings.HasSuffix(r.URL.Path, "/create"):
			_, _ = w.Write(wecomCLITestEnvelope(map[string]any{
				"docid": "doc_1", "url": "https://doc.weixin.qq.com/doc/doc_1", "name": "纪要",
			}))
		case strings.Contains(r.URL.Path, "/members/update"):
			_, _ = w.Write(wecomCLITestEnvelope(map[string]any{}))
		case strings.HasSuffix(r.URL.Path, "/schedules/cancel"):
			_, _ = w.Write(wecomCLITestEnvelope(map[string]any{}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	db, err := store.NewDBStore("sqlite", "file:wecom-office?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveChannel(ctx, &store.ChannelRecord{
		UserID: "user-1", AgentID: "agent-1", Type: "wecom",
		AccountID: "bot_1", Enabled: true, BotToken: "long-conn",
	}); err != nil {
		t.Fatal(err)
	}
	orig := wecomCLIFromChannel
	wecomCLIFromChannel = func(ch *store.ChannelRecord) (*channels.WeComCLI, error) {
		c, err := channels.WeComCLIFromChannel(ch)
		if err != nil {
			return nil, err
		}
		c.AuthURL = srv.URL + "/cgi-bin/aibot/cli/get_cli_config"
		c.BaseURL = srv.URL
		return c, nil
	}
	t.Cleanup(func() { wecomCLIFromChannel = orig })

	r := NewRegistry(t.TempDir(), t.TempDir())
	r.SetOwnerUserID("user-1")
	r.SetMessageContext("wecom", "bot_1", "zhangsan")
	RegisterWeComOfficeTools(r, db, "agent-1")

	out, err := r.Execute(ctx, "wecom_create_doc", `{"title":"纪要","content":"hello"}`)
	if err != nil {
		t.Fatalf("create doc: %v", err)
	}
	if !strings.Contains(out, "doc.weixin.qq.com") {
		t.Fatalf("doc result = %s", out)
	}

	out, err = r.Execute(ctx, "wecom_cancel_schedule", `{"schedule_id":"sid_1"}`)
	if err != nil || !strings.Contains(out, "NOT APPLIED") || !strings.Contains(out, "confirm_token=") {
		t.Fatalf("preview = %q err=%v", out, err)
	}
	const mark = "confirm_token="
	i := strings.Index(out, mark)
	tok := strings.Fields(out[i+len(mark):])[0]
	tok = strings.TrimRight(tok, "().")
	out, err = r.Execute(ctx, "wecom_cancel_schedule", `{"schedule_id":"sid_1","confirm_token":"`+tok+`"}`)
	if err != nil || !strings.Contains(out, "Cancelled") {
		t.Fatalf("cancel = %q err=%v", out, err)
	}
}
