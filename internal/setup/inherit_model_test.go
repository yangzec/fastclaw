package setup

import (
	"testing"
	"time"
)

func TestPickInheritedModelPrefersUserDefault(t *testing.T) {
	got := pickInheritedModel("openai/gpt-5.5", []siblingModel{
		{id: "agt_old", model: "openai/other", createdAt: time.Unix(1, 0)},
	}, "")
	if got != "openai/gpt-5.5" {
		t.Fatalf("got %q", got)
	}
}

func TestPickInheritedModelUsesOldestSibling(t *testing.T) {
	got := pickInheritedModel("", []siblingModel{
		{id: "agt_new", model: "openai/new", createdAt: time.Unix(20, 0)},
		{id: "agt_old", model: "openai/gpt-5.5", createdAt: time.Unix(10, 0)},
	}, "agt_skip")
	if got != "openai/gpt-5.5" {
		t.Fatalf("got %q", got)
	}
}

func TestPickInheritedModelSkipsSelfAndEmpty(t *testing.T) {
	got := pickInheritedModel("", []siblingModel{
		{id: "agt_self", model: "openai/self", createdAt: time.Unix(1, 0)},
		{id: "agt_empty", model: "", createdAt: time.Unix(2, 0)},
		{id: "agt_ok", model: "openai/ok", createdAt: time.Unix(3, 0)},
	}, "agt_self")
	if got != "openai/ok" {
		t.Fatalf("got %q", got)
	}
}

func TestPickInheritedModelEmptyWhenNothingToCopy(t *testing.T) {
	if got := pickInheritedModel("  ", nil, ""); got != "" {
		t.Fatalf("got %q", got)
	}
}
