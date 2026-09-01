package channels

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/config"
	"github.com/fastclaw-ai/fastclaw/internal/store"
)

func TestWeComOAGetTokenAndAddSchedule(t *testing.T) {
	var gotAdd map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/cgi-bin/gettoken"):
			if r.URL.Query().Get("corpid") != "ww_corp" || r.URL.Query().Get("corpsecret") != "sec_1" {
				t.Errorf("gettoken query = %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode": 0, "errmsg": "ok", "access_token": "tok_abc", "expires_in": 7200,
			})
		case strings.HasPrefix(r.URL.Path, "/cgi-bin/oa/schedule/add"):
			if r.URL.Query().Get("access_token") != "tok_abc" {
				t.Errorf("add token = %q", r.URL.Query().Get("access_token"))
			}
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &gotAdd); err != nil {
				t.Errorf("decode add: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode": 0, "errmsg": "ok", "schedule_id": "sched_1",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewWeComOA("ww_corp", "sec_1", "1000014", srv.URL)
	id, err := c.AddSchedule(context.Background(), WeComSchedule{
		Summary:    "项目例会",
		StartUnix:  1773462000,
		EndUnix:    1773465600,
		Location:   "10F",
		Attendees:  []string{"zhangsan", "lisi", "zhangsan"},
		RemindSecs: 900,
	})
	if err != nil {
		t.Fatalf("AddSchedule: %v", err)
	}
	if id != "sched_1" {
		t.Fatalf("id = %q", id)
	}
	sched, _ := gotAdd["schedule"].(map[string]any)
	if sched["summary"] != "项目例会" {
		t.Fatalf("summary = %#v", sched["summary"])
	}
	atts, _ := sched["attendees"].([]any)
	if len(atts) != 2 {
		t.Fatalf("attendees = %#v", sched["attendees"])
	}
	if gotAdd["agentid"] != float64(1000014) {
		t.Fatalf("agentid = %#v", gotAdd["agentid"])
	}
}

func TestWeComValidateOARejectsBadToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errcode": 40013, "errmsg": "invalid corpid",
		})
	}))
	defer srv.Close()
	c := NewWeComOA("bad", "x", "", srv.URL)
	if _, err := c.AccessToken(context.Background()); err == nil || !strings.Contains(err.Error(), "40013") {
		t.Fatalf("want 40013, got %v", err)
	}
}

func TestWeComOAFromChannel(t *testing.T) {
	_, err := WeComOAFromChannel(&store.ChannelRecord{
		AccountID: "bot_1",
		Data: map[string]any{
			"accounts": map[string]any{
				"bot_1": map[string]any{"botToken": "long-conn"},
			},
		},
	})
	if err == nil {
		t.Fatal("expected missing OA creds")
	}

	c, err := WeComOAFromChannel(&store.ChannelRecord{
		AccountID: "bot_1",
		Data: map[string]any{
			"accounts": map[string]any{
				"bot_1": map[string]any{
					"botToken":    "long-conn",
					"corpId":      "ww_corp",
					"corpSecret":  "sec_1",
					"corpAgentId": "12",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("WeComOAFromChannel: %v", err)
	}
	if c.CorpID != "ww_corp" || c.CorpSecret != "sec_1" || c.AgentID != "12" {
		t.Fatalf("client = %+v", c)
	}
	if !WeComOAConfigured(config.AccountConfig{CorpID: "ww", CorpSecret: "s"}) {
		t.Fatal("expected configured")
	}
}
