package agent

import (
	"context"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/config"
	"github.com/fastclaw-ai/fastclaw/internal/scope"
	"github.com/fastclaw-ai/fastclaw/internal/store"
)

func TestCompactThresholdUsesSavedWindowFromStore(t *testing.T) {
	db, err := store.NewDBStore("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	userID := "user-a"
	agentID := "agt_x"

	if err := scope.SaveProvider(ctx, db, userID, "", "openai", config.ProviderConfig{
		APIBase: "https://api.openai.com/v1",
		Models:  []config.ModelEntry{{ID: "gpt-5.5", Name: "gpt-5.5", ContextWindow: 50_000}},
	}); err != nil {
		t.Fatalf("save provider: %v", err)
	}

	ag := &Agent{
		name:        agentID,
		agentID:     agentID,
		ownerUserID: userID,
		model:       "openai/gpt-5.5",
		maxTokens:   8192,
		dataStore:   db,
		// Stale boot-time catalog: official 1.05M. Compact must ignore
		// this once the user saved 50k on the Models page.
		providers: map[string]config.ProviderConfig{
			"openai": {Models: []config.ModelEntry{{ID: "gpt-5.5", ContextWindow: 1_050_000}}},
		},
	}

	got := ag.compactTokenThreshold(userID)
	want := CompactThreshold(50_000, 8192)
	if got != want {
		t.Fatalf("first save: threshold = %d, want %d (50k window)", got, want)
	}

	if err := scope.SaveProvider(ctx, db, userID, "", "openai", config.ProviderConfig{
		APIBase: "https://api.openai.com/v1",
		Models:  []config.ModelEntry{{ID: "gpt-5.5", Name: "gpt-5.5", ContextWindow: 400_000}},
	}); err != nil {
		t.Fatalf("update provider: %v", err)
	}

	got = ag.compactTokenThreshold(userID)
	want = CompactThreshold(400_000, 8192)
	if got != want {
		t.Fatalf("after edit: threshold = %d, want %d (400k window)", got, want)
	}
	if got == CompactThreshold(1_050_000, 8192) {
		t.Fatal("still using boot-time 1.05M window after Models save")
	}
}

func TestCompactThresholdAgentScopeBeatsStaleCatalog(t *testing.T) {
	db, err := store.NewDBStore("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	if err := scope.SaveProvider(ctx, db, "", "agt_x", "openai", config.ProviderConfig{
		Models: []config.ModelEntry{{ID: "gpt-5.5", ContextWindow: 128_000}},
	}); err != nil {
		t.Fatalf("save agent provider: %v", err)
	}

	ag := &Agent{
		name:        "agt_x",
		agentID:     "agt_x",
		ownerUserID: "user-a",
		model:       "openai/gpt-5.5",
		maxTokens:   8192,
		dataStore:   db,
	}
	got := ag.effectiveContextWindow("user-a")
	if got != 128_000 {
		t.Fatalf("agent-scope window = %d, want 128000", got)
	}
}
