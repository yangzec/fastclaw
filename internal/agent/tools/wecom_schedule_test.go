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

func TestWeComCreateScheduleUsesOAAndInvitesSender(t *testing.T) {
	var gotAdd map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "gettoken"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode": 0, "access_token": "tok", "expires_in": 7200,
			})
		case strings.Contains(r.URL.Path, "schedule/add"):
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &gotAdd)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode": 0, "schedule_id": "sid_9",
			})
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
		UserID:    "user-1",
		AgentID:   "agent-1",
		Type:      "wecom",
		AccountID: "bot_1",
		Enabled:   true,
		BotToken:  "long-conn",
		Data: map[string]any{
			"accounts": map[string]any{
				"bot_1": map[string]any{
					"botToken":   "long-conn",
					"corpId":     "ww_corp",
					"corpSecret": "sec_1",
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	orig := wecomOAFromChannel
	wecomOAFromChannel = func(ch *store.ChannelRecord) (*channels.WeComOA, error) {
		c, err := channels.WeComOAFromChannel(ch)
		if err != nil {
			return nil, err
		}
		c.BaseURL = srv.URL
		return c, nil
	}
	t.Cleanup(func() { wecomOAFromChannel = orig })

	r := NewRegistry(t.TempDir(), t.TempDir())
	r.SetOwnerUserID("user-1")
	r.SetChatterUserID("user-1")
	r.SetMessageContext("wecom", "bot_1", "zhangsan")
	RegisterWeComScheduleTools(r, db, "agent-1")

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
	sched, _ := gotAdd["schedule"].(map[string]any)
	if sched["summary"] != "项目例会" {
		t.Fatalf("payload = %#v", gotAdd)
	}
}

func TestWeComCreateScheduleRequiresOA(t *testing.T) {
	db, err := store.NewDBStore("sqlite", "file:wecom-oa-missing?mode=memory&cache=shared")
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
		Data: map[string]any{"accounts": map[string]any{"bot_1": map[string]any{"botToken": "long-conn"}}},
	}); err != nil {
		t.Fatal(err)
	}
	r := NewRegistry(t.TempDir(), t.TempDir())
	r.SetOwnerUserID("user-1")
	r.SetMessageContext("wecom", "bot_1", "u1")
	RegisterWeComScheduleTools(r, db, "agent-1")
	_, err = r.Execute(ctx, "wecom_create_schedule", `{"summary":"x","start":"2026-09-02T15:00:00Z"}`)
	if err == nil || !strings.Contains(err.Error(), "not enabled") {
		t.Fatalf("want OA missing error, got %v", err)
	}
}
