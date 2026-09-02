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

func TestFeishuOpenAPICreateEventTaskDoc(t *testing.T) {
	var gotEvent, gotAtt, gotTask, gotDoc, gotShare map[string]any
	var gotChildren []any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" && r.Header.Get("Authorization") != "Bearer tok_fs" &&
			!strings.Contains(r.URL.Path, "tenant_access_token") {
			t.Errorf("auth %q path %s", r.Header.Get("Authorization"), r.URL.Path)
		}
		switch {
		case strings.Contains(r.URL.Path, "tenant_access_token"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0, "tenant_access_token": "tok_fs", "expire": 7200,
			})
		case strings.HasSuffix(r.URL.Path, "/calendars/primary"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"calendars": []any{map[string]any{"calendar": map[string]any{"calendar_id": "cal_bot"}}},
				},
			})
		case strings.HasSuffix(r.URL.Path, "/events") && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &gotEvent)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"event": map[string]any{"event_id": "evt_1", "app_link": "https://applink.feishu.cn/e/1"}},
			})
		case strings.Contains(r.URL.Path, "/attendees"):
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &gotAtt)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0})
		case strings.HasSuffix(r.URL.Path, "/task/v2/tasks") && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &gotTask)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"task": map[string]any{"guid": "task_1", "url": "https://applink.feishu.cn/t/1"}},
			})
		case strings.HasSuffix(r.URL.Path, "/docx/v1/documents") && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &gotDoc)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"document": map[string]any{"document_id": "doc_1", "title": "纪要"}},
			})
		case strings.Contains(r.URL.Path, "/children"):
			var payload map[string]any
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &payload)
			gotChildren, _ = payload["children"].([]any)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0})
		case strings.Contains(r.URL.Path, "/permissions/"):
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &gotShare)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewFeishuOpenAPI("cli_app", "sec", srv.URL)
	ctx := context.Background()

	eid, cal, link, err := c.CreateEvent(ctx, FeishuEvent{
		Summary:    "项目例会",
		StartUnix:  1773462000,
		EndUnix:    1773465600,
		Timezone:   "Asia/Shanghai",
		Location:   "10F",
		Attendees:  []string{"ou_me", "ou_me"},
		RemindMins: 15,
	})
	if err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	if eid != "evt_1" || cal != "cal_bot" || !strings.Contains(link, "applink") {
		t.Fatalf("event %s %s %s", eid, cal, link)
	}
	if gotEvent["summary"] != "项目例会" {
		t.Fatalf("event payload = %#v", gotEvent)
	}
	atts, _ := gotAtt["attendees"].([]any)
	if len(atts) != 1 {
		t.Fatalf("attendees = %#v", gotAtt)
	}

	gid, turl, err := c.CreateTask(ctx, FeishuTask{
		Summary:   "写周报",
		DueUnix:   1773462000,
		Assignees: []string{"ou_me"},
	})
	if err != nil || gid != "task_1" || turl == "" {
		t.Fatalf("CreateTask %s %s err=%v", gid, turl, err)
	}
	if gotTask["summary"] != "写周报" {
		t.Fatalf("task payload = %#v", gotTask)
	}

	did, durl, err := c.CreateDoc(ctx, FeishuDoc{
		Title:   "纪要",
		Content: "第一段\n第二段",
		Share:   []string{"ou_me"},
	})
	if err != nil || did != "doc_1" || !strings.Contains(durl, "doc_1") {
		t.Fatalf("CreateDoc %s %s err=%v", did, durl, err)
	}
	if gotDoc["title"] != "纪要" {
		t.Fatalf("doc payload = %#v", gotDoc)
	}
	if len(gotChildren) != 2 {
		t.Fatalf("children = %#v", gotChildren)
	}
	if gotShare["member_id"] != "ou_me" || gotShare["perm"] != "full_access" {
		t.Fatalf("share = %#v", gotShare)
	}
}

func TestFeishuOpenAPIMapsMissingScope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "tenant_access_token") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0, "tenant_access_token": "tok", "expire": 7200,
			})
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 99991663, "msg": "required scope not granted",
		})
	}))
	defer srv.Close()
	c := NewFeishuOpenAPI("cli_app", "sec", srv.URL)
	_, err := c.PrimaryCalendarID(context.Background())
	if err == nil || !strings.Contains(err.Error(), "scan again") {
		t.Fatalf("want scope hint, got %v", err)
	}
}

func TestFeishuOpenAPIFromChannel(t *testing.T) {
	_, err := FeishuOpenAPIFromChannel(&store.ChannelRecord{
		Type: "feishu", AccountID: "cli_1",
		Data: map[string]any{"accounts": map[string]any{"cli_1": map[string]any{"botToken": "sec"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = FeishuOpenAPIFromChannel(&store.ChannelRecord{
		Type: "feishu", AccountID: "cli_1",
		Data: map[string]any{"accounts": map[string]any{"cli_1": map[string]any{}}},
	})
	if err == nil || !strings.Contains(err.Error(), "Secret") {
		t.Fatalf("want missing secret, got %v", err)
	}
}

func TestFeishuDocumentID(t *testing.T) {
	if got := feishuDocumentID("https://acme.feishu.cn/docx/doxcnABC?x=1"); got != "doxcnABC" {
		t.Fatalf("got %q", got)
	}
	if got := feishuDocumentID("doxcnABC"); got != "doxcnABC" {
		t.Fatalf("plain %q", got)
	}
}
