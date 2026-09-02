package agent

import (
	"strings"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/bus"
	"github.com/fastclaw-ai/fastclaw/internal/config"
	"github.com/fastclaw-ai/fastclaw/internal/provider"
)

func TestRenderOfficeRoutingHint(t *testing.T) {
	feishu := renderOfficeRoutingHint(bus.InboundMessage{Channel: "feishu"}, "")
	if !strings.Contains(feishu, "feishu_create_task") || !strings.Contains(feishu, "feishu_create_event") {
		t.Fatalf("feishu hint = %q", feishu)
	}
	if strings.Contains(feishu, "wecom_create_schedule") {
		t.Fatalf("feishu hint leaked wecom: %q", feishu)
	}
	if !strings.Contains(feishu, "do NOT ask") {
		t.Fatalf("feishu hint must forbid asking which calendar: %q", feishu)
	}

	wecom := renderOfficeRoutingHint(bus.InboundMessage{Channel: "WeCom"}, "")
	if !strings.Contains(wecom, "wecom_create_schedule") {
		t.Fatalf("wecom hint = %q", wecom)
	}
	if !strings.Contains(wecom, "no official todo") {
		t.Fatalf("wecom hint should say there is no official todo API: %q", wecom)
	}
	if !strings.Contains(wecom, "do NOT ask") {
		t.Fatalf("wecom hint must forbid asking which calendar: %q", wecom)
	}

	if got := renderOfficeRoutingHint(bus.InboundMessage{Channel: "web"}, ""); got != "" {
		t.Fatalf("web should have no office hint, got %q", got)
	}
	if got := renderOfficeRoutingHint(bus.InboundMessage{Channel: "telegram"}, ""); got != "" {
		t.Fatalf("telegram should have no office hint, got %q", got)
	}
	if got := renderOfficeRoutingHint(bus.InboundMessage{Channel: "feishu"}, config.PromptModeChatbot); got != "" {
		t.Fatalf("chatbot should skip office hint, got %q", got)
	}
	if got := renderOfficeRoutingHint(bus.InboundMessage{Channel: "wecom"}, config.PromptModeCustomize); got != "" {
		t.Fatalf("customize should skip office hint, got %q", got)
	}
}

func TestAssembleTurnMessagesIncludesOfficeRouting(t *testing.T) {
	a := &Agent{}
	msgs := a.assembleTurnMessages("SYS", bus.InboundMessage{Channel: "feishu"}, nil, nil, "")
	found := false
	for _, m := range msgs {
		if m.Role == "system" && strings.Contains(m.Content, "feishu_create_task") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected office routing system message, got %#v", msgs)
	}

	web := a.assembleTurnMessages("SYS", bus.InboundMessage{Channel: "web"}, nil, nil, "")
	for _, m := range web {
		if strings.Contains(m.Content, "feishu_create_task") || strings.Contains(m.Content, "Official calendar") {
			t.Fatalf("web turn should not include office routing: %#v", web)
		}
	}

	a.promptMode = config.PromptModeChatbot
	chatbot := a.assembleTurnMessages("SYS", bus.InboundMessage{Channel: "feishu"}, nil, nil, "")
	for _, m := range chatbot {
		if strings.Contains(m.Content, "feishu_create_task") {
			t.Fatalf("chatbot turn should not include office routing: %#v", chatbot)
		}
	}
}

func TestWorkspaceUpdateRoutesOfficialByChannel(t *testing.T) {
	section := workspaceUpdateContent
	idx := strings.Index(section, "# Scheduling Time-Bound Tasks")
	if idx < 0 {
		t.Fatal("missing scheduling section")
	}
	sched := section[idx:]
	for _, needle := range []string{
		`Current Channel is "feishu"`,
		`Current Channel is "wecom"`,
		"feishu_create_event",
		"feishu_create_task",
		"wecom_create_schedule",
		"wake up and speak",
		"以后待办都写飞书",
		"Never ask",
	} {
		if !strings.Contains(sched, needle) {
			t.Fatalf("scheduling section missing %q", needle)
		}
	}
	officialAt := strings.Index(sched, "**Official calendar")
	cronAt := strings.Index(sched, "**FastClaw scheduler")
	if officialAt < 0 || cronAt < 0 || cronAt < officialAt {
		t.Fatal("official routing should come before the cron section")
	}
	if strings.Contains(sched, "call the create_cron_job tool") {
		t.Fatal("scheduling section still leads with cron as the default")
	}
}

func TestFilterOfficeToolsForChannelHidesTheOtherCalendar(t *testing.T) {
	defs := []provider.Tool{
		{Type: "function", Function: provider.ToolFunction{Name: "feishu_create_event"}},
		{Type: "function", Function: provider.ToolFunction{Name: "feishu_create_task"}},
		{Type: "function", Function: provider.ToolFunction{Name: "wecom_create_schedule"}},
		{Type: "function", Function: provider.ToolFunction{Name: "wecom_get_schedule"}},
		{Type: "function", Function: provider.ToolFunction{Name: "create_cron_job"}},
	}

	feishu := namesOf(filterOfficeToolsForChannel(defs, "feishu"))
	if feishu["wecom_create_schedule"] || feishu["wecom_get_schedule"] {
		t.Fatalf("feishu turn still sees WeCom calendar tools: %v", feishu)
	}
	if !feishu["feishu_create_event"] || !feishu["create_cron_job"] {
		t.Fatalf("feishu turn dropped its own tools: %v", feishu)
	}

	wecom := namesOf(filterOfficeToolsForChannel(defs, "wecom"))
	if wecom["feishu_create_event"] {
		t.Fatalf("wecom turn still sees Feishu calendar tool: %v", wecom)
	}
	if !wecom["wecom_create_schedule"] || !wecom["feishu_create_task"] {
		t.Fatalf("wecom turn dropped the wrong tools: %v", wecom)
	}

	web := namesOf(filterOfficeToolsForChannel(defs, "web"))
	if len(web) != len(defs) {
		t.Fatalf("web should keep every office tool, got %v", web)
	}
}

func namesOf(defs []provider.Tool) map[string]bool {
	out := make(map[string]bool, len(defs))
	for _, d := range defs {
		out[d.Function.Name] = true
	}
	return out
}
