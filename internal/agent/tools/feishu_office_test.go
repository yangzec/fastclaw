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
	"github.com/fastclaw-ai/fastclaw/internal/users"
)

func TestFeishuCreateEventInvitesSender(t *testing.T) {
	var gotEvent, gotTask, gotDoc map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "tenant_access_token"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0, "tenant_access_token": "tok", "expire": 7200,
			})
		case strings.HasSuffix(r.URL.Path, "/calendars/primary"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"calendars": []any{map[string]any{"calendar": map[string]any{"calendar_id": "cal_1"}}}},
			})
		case strings.HasSuffix(r.URL.Path, "/events"):
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &gotEvent)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"event": map[string]any{"event_id": "evt_9", "app_link": "https://applink.feishu.cn/e/9"}},
			})
		case strings.Contains(r.URL.Path, "/attendees"):
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0})
		case strings.HasSuffix(r.URL.Path, "/task/v2/tasks") && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &gotTask)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"task": map[string]any{"guid": "tg_1", "url": "https://applink.feishu.cn/t/1"}},
			})
		case strings.Contains(r.URL.Path, "/task/v2/tasks/") && r.Method == http.MethodPatch:
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0})
		case strings.HasSuffix(r.URL.Path, "/docx/v1/documents"):
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &gotDoc)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"document": map[string]any{"document_id": "dox_1"}},
			})
		case strings.Contains(r.URL.Path, "/children"), strings.Contains(r.URL.Path, "/permissions/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0})
		case strings.Contains(r.URL.Path, "/raw_content"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0, "data": map[string]any{"content": "hello body"},
			})
		case strings.Contains(r.URL.Path, "/docx/v1/documents/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0, "data": map[string]any{"document": map[string]any{"title": "纪要"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	db, err := store.NewDBStore("sqlite", "file:feishu-office?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveChannel(ctx, &store.ChannelRecord{
		UserID: "user-1", AgentID: "agent-1", Type: "feishu",
		AccountID: "cli_1", Enabled: true, BotToken: "sec",
		Data: map[string]any{"accounts": map[string]any{"cli_1": map[string]any{"botToken": "sec"}}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateUser(ctx, &store.UserRecord{
		ID: "u_chat", Username: "chat", Email: "c@x", PasswordHash: "x",
		Role: users.RoleUser, Status: users.StatusActive, AgentQuota: -1,
		ExternalID: "feishu:ou_sender",
		CreatedAt:  time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	orig := feishuOpenAPIFromChannel
	feishuOpenAPIFromChannel = func(ch *store.ChannelRecord) (*channels.FeishuOpenAPI, error) {
		c, err := channels.FeishuOpenAPIFromChannel(ch)
		if err != nil {
			return nil, err
		}
		c.BaseURL = srv.URL
		return c, nil
	}
	t.Cleanup(func() { feishuOpenAPIFromChannel = orig })

	r := NewRegistry(t.TempDir(), t.TempDir())
	r.SetOwnerUserID("user-1")
	r.SetChatterUserID("u_chat")
	r.SetMessageContext("feishu", "cli_1", "oc_dm")
	RegisterFeishuOfficeTools(r, db, "agent-1")

	out, err := r.Execute(ctx, "feishu_create_event", `{
		"summary":"项目例会",
		"start":"2026-09-02T15:00:00+08:00",
		"end":"2026-09-02T16:00:00+08:00",
		"location":"10F"
	}`)
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if !strings.Contains(out, "evt_9") || !strings.Contains(out, "ou_sender") {
		t.Fatalf("event result = %s", out)
	}
	if gotEvent["summary"] != "项目例会" {
		t.Fatalf("event payload = %#v", gotEvent)
	}

	out, err = r.Execute(ctx, "feishu_create_task", `{"summary":"写周报","due":"2026-09-03"}`)
	if err != nil {
		t.Fatalf("task: %v", err)
	}
	if !strings.Contains(out, "tg_1") || !strings.Contains(out, "ou_sender") {
		t.Fatalf("task result = %s", out)
	}

	out, err = r.Execute(ctx, "feishu_complete_task", `{"task_guid":"tg_1"}`)
	if err != nil || !strings.Contains(out, "tg_1") {
		t.Fatalf("complete: %s err=%v", out, err)
	}

	out, err = r.Execute(ctx, "feishu_create_doc", `{"title":"纪要","content":"hello"}`)
	if err != nil {
		t.Fatalf("doc: %v", err)
	}
	if !strings.Contains(out, "dox_1") || !strings.Contains(out, "ou_sender") {
		t.Fatalf("doc result = %s", out)
	}

	out, err = r.Execute(ctx, "feishu_read_doc", `{"document_id":"https://x.feishu.cn/docx/dox_1"}`)
	if err != nil || !strings.Contains(out, "纪要") || !strings.Contains(out, "hello body") {
		t.Fatalf("read: %s err=%v", out, err)
	}
}

func TestFeishuOfficeRequiresChannel(t *testing.T) {
	db, err := store.NewDBStore("sqlite", "file:feishu-office-missing?mode=memory&cache=shared")
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
	r.SetMessageContext("feishu", "cli_1", "oc_dm")
	RegisterFeishuOfficeTools(r, db, "agent-1")
	_, err = r.Execute(ctx, "feishu_create_event", `{"summary":"x","start":"2026-09-02T15:00:00Z"}`)
	if err == nil || !strings.Contains(err.Error(), "no Feishu bot") {
		t.Fatalf("want missing channel, got %v", err)
	}
}

func TestFeishuSenderOpenID(t *testing.T) {
	if feishuSenderOpenID(context.Background(), nil, nil) != "" {
		t.Fatal("nil registry")
	}
}
