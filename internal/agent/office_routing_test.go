package agent

import (
	"strings"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/bus"
	"github.com/fastclaw-ai/fastclaw/internal/config"
)

func TestRenderOfficeRoutingHint(t *testing.T) {
	feishu := renderOfficeRoutingHint(bus.InboundMessage{Channel: "feishu"}, "")
	if !strings.Contains(feishu, "feishu_create_task") || !strings.Contains(feishu, "feishu_create_event") {
		t.Fatalf("feishu hint = %q", feishu)
	}
	if strings.Contains(feishu, "wecom_create_schedule") {
		t.Fatalf("feishu hint leaked wecom: %q", feishu)
	}

	wecom := renderOfficeRoutingHint(bus.InboundMessage{Channel: "WeCom"}, "")
	if !strings.Contains(wecom, "wecom_create_schedule") {
		t.Fatalf("wecom hint = %q", wecom)
	}
	if !strings.Contains(wecom, "no official todo") {
		t.Fatalf("wecom hint should say there is no official todo API: %q", wecom)
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
	} {
		if !strings.Contains(sched, needle) {
			t.Fatalf("scheduling section missing %q", needle)
		}
	}
	// Cron must not be the first recommendation in the scheduling block.
	cronAt := strings.Index(sched, "create_cron_job")
	feishuAt := strings.Index(sched, "feishu_create_event")
	if cronAt < 0 || feishuAt < 0 || cronAt < feishuAt {
		t.Fatalf("official Feishu tools should be recommended before create_cron_job")
	}
}
