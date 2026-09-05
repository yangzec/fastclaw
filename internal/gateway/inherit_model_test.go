package gateway

import (
	"testing"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/config"
	"github.com/fastclaw-ai/fastclaw/internal/store"
)

func TestFillMissingAgentModelsUsesOldestSibling(t *testing.T) {
	old := time.Unix(10, 0)
	neu := time.Unix(20, 0)
	records := []store.AgentRecord{
		{ID: "agt_new", CreatedAt: neu},
		{ID: "agt_old", CreatedAt: old},
	}
	resolved := []config.ResolvedAgent{
		{ID: "agt_new", Model: ""},
		{ID: "agt_old", Model: "openai/gpt-5.5"},
	}
	fillMissingAgentModels(records, resolved)
	if resolved[0].Model != "openai/gpt-5.5" {
		t.Fatalf("new agent model = %q", resolved[0].Model)
	}
	if resolved[1].Model != "openai/gpt-5.5" {
		t.Fatalf("old agent model = %q", resolved[1].Model)
	}
}

func TestFillMissingAgentModelsLeavesExplicitEmptyWhenNobodyHasOne(t *testing.T) {
	resolved := []config.ResolvedAgent{{ID: "agt_1", Model: ""}}
	fillMissingAgentModels([]store.AgentRecord{{ID: "agt_1"}}, resolved)
	if resolved[0].Model != "" {
		t.Fatalf("got %q", resolved[0].Model)
	}
}
