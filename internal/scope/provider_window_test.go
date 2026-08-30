package scope

import (
	"context"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/config"
	"github.com/fastclaw-ai/fastclaw/internal/store"
)

func TestProviderContextWindowRoundTrip(t *testing.T) {
	db, err := store.NewDBStore("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()

	if err := SaveProvider(ctx, db, "user-a", "", "openai", config.ProviderConfig{
		APIBase: "https://api.openai.com/v1",
		Models: []config.ModelEntry{{
			ID:            "gpt-5.5",
			Name:          "GPT-5.5",
			ContextWindow: 400_000,
		}},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	provs, err := Providers(ctx, db, "user-a", "")
	if err != nil {
		t.Fatalf("providers: %v", err)
	}
	pc, ok := provs["openai"]
	if !ok {
		t.Fatal("missing openai provider after save")
	}
	if len(pc.Models) != 1 || pc.Models[0].ContextWindow != 400_000 {
		t.Fatalf("contextWindow did not persist: %+v", pc.Models)
	}
}
