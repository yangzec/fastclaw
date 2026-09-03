package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/bus"
	"github.com/fastclaw-ai/fastclaw/internal/config"
	"github.com/fastclaw-ai/fastclaw/internal/provider"
	"github.com/fastclaw-ai/fastclaw/internal/scope"
	"github.com/fastclaw-ai/fastclaw/internal/session"
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

func compactTestAgent(t *testing.T, prov provider.Provider) *Agent {
	t.Helper()
	return &Agent{
		name:      "compact-test",
		homePath:  t.TempDir(),
		provider:  prov,
		model:     "openai/tiny",
		maxTokens: 512,
		providers: map[string]config.ProviderConfig{
			"openai": {Models: []config.ModelEntry{{ID: "tiny", ContextWindow: 8000}}},
		},
		sessions: session.NewManager(t.TempDir()),
	}
}

func TestCompactSessionReplacesWorkingSet(t *testing.T) {
	f := &fakeSummarizer{}
	ag := compactTestAgent(t, f)
	sess := ag.sessions.Get("web", "", "chat-compact", "")
	blob := strings.Repeat("session-body-", 80)
	for i := 0; i < 40; i++ {
		sess.Append(provider.Message{Role: "user", Content: blob + "u", Origin: provider.OriginUser})
		sess.Append(provider.Message{Role: "assistant", Content: blob + "a", Origin: provider.OriginUser})
	}
	before := len(sess.GetMessages())
	if EstimateTokens(sess.GetMessages()) < ag.compactTokenThreshold("user-a") {
		t.Fatalf("fixture under threshold: tokens=%d thresh=%d", EstimateTokens(sess.GetMessages()), ag.compactTokenThreshold("user-a"))
	}

	msgs, notice, hint, did := ag.compactSession(context.Background(), sess, "user-a", CompactOptions{KeepRecentTokens: 800})
	if !did {
		t.Fatal("expected compactSession to prune")
	}
	if notice == "" || strings.Contains(notice, "/new") {
		t.Fatalf("summarize notice should be user-visible without /new, got %q", notice)
	}
	if hint == "" {
		t.Fatal("expected a model hint after summarize")
	}
	if len(sess.GetMessages()) >= before {
		t.Fatalf("session working set did not shrink: %d → %d", before, len(sess.GetMessages()))
	}
	if len(msgs) == 0 || !strings.Contains(msgs[0].Content, conversationSummaryMark) {
		t.Fatal("replaced working set should start with the conversation summary")
	}
	if !strings.Contains(f.gotSummaryRequest, "session-body-") {
		t.Fatal("summarizer should see the original session text")
	}
}

func TestRetryAfterOverflowMillionWindowStillShrinks(t *testing.T) {
	// Same shape as the 14:27 incident: configured window 1M, real
	// gateway already rejected ~80k tokens. Summarizer is nil so the
	// only way retryAfterOverflow can succeed is OverflowTokens.
	ag := compactTestAgent(t, nil)
	ag.model = "zhipu/glm-5.3"
	ag.maxTokens = 8192
	ag.providers = map[string]config.ProviderConfig{
		"zhipu": {Models: []config.ModelEntry{{ID: "glm-5.3", ContextWindow: 1_000_000}}},
	}
	thresh := ag.compactTokenThreshold("user-a")
	if thresh != CompactThreshold(1_000_000, 8192) {
		t.Fatalf("threshold = %d, want 1M-window compact threshold", thresh)
	}

	sess := ag.sessions.Get("web", "", "chat-million-overflow", "")
	blob := strings.Repeat("overflow-body-", 80)
	for i := 0; i < 80; i++ {
		sess.Append(provider.Message{Role: "user", Content: blob + "u", Origin: provider.OriginUser})
		sess.Append(provider.Message{Role: "assistant", Content: blob + "a", Origin: provider.OriginUser})
	}
	before := EstimateTokens(sess.GetMessages())
	if before >= thresh {
		t.Fatalf("fixture over configured threshold: tokens=%d thresh=%d", before, thresh)
	}

	msg := bus.InboundMessage{Channel: "web", ChatID: "chat-million-overflow"}
	rebuilt, hint, notice, ok := ag.retryAfterOverflow(context.Background(), sess, "user-a", "SYSTEM_PROMPT", msg, nil)
	if !ok {
		t.Fatal("1M-window overflow retry must still compact using the rejected size")
	}
	after := EstimateTokens(sess.GetMessages())
	if after >= before {
		t.Fatalf("session did not shrink: %d → %d", before, after)
	}
	if after > overflowHardTrimBudget(thresh, before, KeepRecentTokens) {
		t.Fatalf("session still over overflow budget: after=%d", after)
	}
	if notice == "" || hint == "" {
		t.Fatalf("expected notice and hint, got notice=%q hint=%q", notice, hint)
	}
	if len(rebuilt) < 1 || rebuilt[0].Content != "SYSTEM_PROMPT" {
		t.Fatalf("rebuilt turn should start with the system prompt, got %+v", rebuilt)
	}
}

func TestRetryAfterOverflowRebuildsTurnPrefix(t *testing.T) {
	f := &fakeSummarizer{}
	ag := compactTestAgent(t, f)
	sess := ag.sessions.Get("web", "", "chat-overflow", "")
	blob := strings.Repeat("overflow-body-", 40)
	for i := 0; i < 8; i++ {
		sess.Append(provider.Message{Role: "user", Content: blob, Origin: provider.OriginUser})
		sess.Append(provider.Message{Role: "assistant", Content: blob, Origin: provider.OriginUser})
	}
	msg := bus.InboundMessage{Channel: "web", ChatID: "chat-overflow"}
	rebuilt, hint, notice, ok := ag.retryAfterOverflow(context.Background(), sess, "user-a", "SYSTEM_PROMPT", msg, nil)
	if !ok {
		t.Fatal("force-compact after overflow should succeed")
	}
	if notice == "" {
		t.Fatal("expected a user-visible compact notice")
	}
	if hint == "" {
		t.Fatal("expected a model hint")
	}
	if len(rebuilt) < 2 || rebuilt[0].Role != "system" || rebuilt[0].Content != "SYSTEM_PROMPT" {
		t.Fatalf("rebuilt turn should start with the system prompt, got %+v", rebuilt)
	}
	foundSummary := false
	for _, m := range rebuilt {
		if strings.Contains(m.Content, conversationSummaryMark) {
			foundSummary = true
			break
		}
	}
	if !foundSummary {
		t.Fatal("rebuilt turn missing the conversation summary")
	}
}
