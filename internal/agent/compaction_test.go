package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/config"
	"github.com/fastclaw-ai/fastclaw/internal/provider"
)

// fakeSummarizer captures the summarize-call prompt so tests can
// assert what compaction actually ships off to the LLM. The
// returned Response is whatever the test wants — we don't care
// about the summary content, only the input.
type fakeSummarizer struct {
	gotSummaryRequest string
}

func (f *fakeSummarizer) Chat(_ context.Context, msgs []provider.Message, _ []provider.Tool, _ string, _ int, _ float64) (*provider.Response, error) {
	// compressOlderMessages builds the user-role prompt as the
	// second message; the older-history text lives in its Content
	// after the "Summarize this conversation:\n\n" prefix.
	if len(msgs) >= 2 {
		f.gotSummaryRequest = msgs[1].Content
	}
	return &provider.Response{Content: "[fake summary]"}, nil
}

func (f *fakeSummarizer) ChatStream(_ context.Context, _ []provider.Message, _ []provider.Tool, _ string, _ int, _ float64) (*provider.StreamReader, error) {
	return nil, nil
}

// TestCompactionDropsGoalContextFromSummary pins design §5.3 (b):
// when compaction folds older messages, runtime-injected
// goal_context messages must be excluded from the summary — their
// content is synthetic audit scaffolding and the latest one is
// already preserved verbatim in the recent tail.
func TestCompactionDropsGoalContextFromSummary(t *testing.T) {
	// Build a history that's longer than PruneTurnAge so
	// compression actually runs. Interleave goal_context messages
	// among real user/assistant turns.
	var msgs []provider.Message
	for i := 0; i < PruneTurnAge+5; i++ {
		msgs = append(msgs,
			provider.Message{Role: "user", Content: "real user message", Origin: provider.OriginUser},
			provider.Message{Role: "user", Content: "RUNTIME_AUDIT_PROMPT", Origin: provider.OriginGoalContext},
			provider.Message{Role: "assistant", Content: "real assistant reply", Origin: provider.OriginUser},
		)
	}

	f := &fakeSummarizer{}
	out, err := compressOlderMessages(context.Background(), msgs, f, "fake-model", compactOpts{keepRecentTokens: 80})
	if err != nil {
		t.Fatalf("compress: %v", err)
	}
	if !strings.Contains(f.gotSummaryRequest, "real user message") {
		t.Errorf("summary input lost real user content: %s", f.gotSummaryRequest)
	}
	if strings.Contains(f.gotSummaryRequest, "RUNTIME_AUDIT_PROMPT") {
		t.Errorf("summary input included runtime-injected goal_context — should have been filtered:\n%s",
			f.gotSummaryRequest)
	}
	// The recent tail must still carry whatever was there; in
	// particular if the tail contained a goal_context the model
	// still needs it for the next audit.
	tailHasContext := false
	for _, m := range out[1:] /* skip the summary prepended at [0] */ {
		if m.Origin == provider.OriginGoalContext {
			tailHasContext = true
			break
		}
	}
	if !tailHasContext {
		t.Error("recent tail should still carry the live goal_context message")
	}
}

// TestCompactionPreservesContentWhenShortCircuits: when the input
// is already under PruneTurnAge, compressOlderMessages returns it
// unchanged. Goal_context filtering shouldn't change that fast path.
func TestCompactionPreservesContentWhenShortCircuits(t *testing.T) {
	in := []provider.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	}
	out, err := compressOlderMessages(context.Background(), in, nil, "", compactOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 2 {
		t.Errorf("short input should pass through; got %d messages", len(out))
	}
}

// --- safeCompactionCutoff coverage ---
//
// The cutoff guard is the load-bearing fix for the OpenAI 400
// "Messages with role 'tool' must be a response to a preceding
// message with 'tool_calls'" — exhaustively pin its behavior, with
// a final end-to-end assertion that the compressed output never
// starts with a tool message.

func TestSafeCompactionCutoffAdvancesPastLeadingTool(t *testing.T) {
	// History tail looks like [..., assistant(tool_calls), tool, tool, assistant_text, user]
	// with cutoff landing on the first `tool` — must advance to the
	// `assistant_text` position so the resulting tail is valid.
	msgs := []provider.Message{
		{Role: "user", Content: "ask"},
		{Role: "assistant", ToolCalls: []provider.ToolCall{{ID: "t1"}}},
		{Role: "tool", ToolCallID: "t1", Content: "r1"},
		{Role: "tool", ToolCallID: "t2", Content: "r2"},
		{Role: "assistant", Content: "ok"},
		{Role: "user", Content: "next"},
	}
	got := safeCompactionCutoff(msgs, 2) // points at first "tool"
	if msgs[got].Role != "assistant" || msgs[got].Content != "ok" {
		t.Errorf("expected cutoff to land on assistant_text; landed on %+v", msgs[got])
	}
}

func TestSafeCompactionCutoffNoAdvanceOnUser(t *testing.T) {
	msgs := []provider.Message{
		{Role: "assistant", Content: "x"},
		{Role: "user", Content: "y"},
	}
	if got := safeCompactionCutoff(msgs, 1); got != 1 {
		t.Errorf("cutoff = %d, want 1 (user is a valid tail start)", got)
	}
}

func TestSafeCompactionCutoffNoAdvanceOnAssistant(t *testing.T) {
	// An assistant message with tool_calls is a valid tail start —
	// its tool replies follow it inside the preserved tail.
	msgs := []provider.Message{
		{Role: "user", Content: "x"},
		{Role: "assistant", ToolCalls: []provider.ToolCall{{ID: "t1"}}},
		{Role: "tool", ToolCallID: "t1"},
	}
	if got := safeCompactionCutoff(msgs, 1); got != 1 {
		t.Errorf("cutoff = %d, want 1 (assistant w/ tool_calls is a valid tail start)", got)
	}
}

func TestSafeCompactionCutoffAdvancesToEnd(t *testing.T) {
	// Degenerate: every message from cutoff to end is a tool. The
	// guard advances past all of them — the tail ends up empty and
	// the caller emits just [summary], which is valid.
	msgs := []provider.Message{
		{Role: "user"},
		{Role: "assistant", ToolCalls: []provider.ToolCall{{ID: "t1"}, {ID: "t2"}}},
		{Role: "tool", ToolCallID: "t1"},
		{Role: "tool", ToolCallID: "t2"},
	}
	if got := safeCompactionCutoff(msgs, 2); got != len(msgs) {
		t.Errorf("cutoff = %d, want %d (entire tail was tool messages)", got, len(msgs))
	}
}

func TestSafeCompactionCutoffNegativeIsClamped(t *testing.T) {
	msgs := []provider.Message{{Role: "user"}}
	if got := safeCompactionCutoff(msgs, -5); got != 0 {
		t.Errorf("cutoff = %d, want 0 (negative input clamped)", got)
	}
}

// TestCompressOlderMessagesNeverStartsTailWithTool is the end-to-end
// assertion that closes the loop. Build a history where the naive
// cutoff lands squarely on a tool reply and verify the compressed
// output's first non-summary message is never a "tool" role. This
// mirrors the shape that was producing the OpenAI 400 in production
// /goal sessions.
func TestCompressOlderMessagesNeverStartsTailWithTool(t *testing.T) {
	// Each body is ~100 tokens so a 800-token hot tail lands inside a
	// tool pair. keepRecentCutoff must still advance past the tool.
	blob := strings.Repeat("x", 400)
	var msgs []provider.Message
	for i := 0; i < 5; i++ {
		msgs = append(msgs, provider.Message{Role: "user", Content: blob})
	}
	for i := 0; i < 7; i++ {
		msgs = append(msgs,
			provider.Message{Role: "assistant", Content: blob, ToolCalls: []provider.ToolCall{{ID: "ta", Function: provider.FunctionCall{Name: "exec"}}, {ID: "tb", Function: provider.FunctionCall{Name: "exec"}}}},
			provider.Message{Role: "tool", ToolCallID: "ta", Content: blob, Name: "exec"},
			provider.Message{Role: "tool", ToolCallID: "tb", Content: blob, Name: "exec"},
		)
	}
	const keep = 800
	naive := 0
	acc := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		acc += EstimateTokens([]provider.Message{msgs[i]})
		naive = i
		if acc >= keep {
			break
		}
	}
	if msgs[naive].Role != "tool" {
		t.Fatalf("fixture broken: naive token cutoff lands on %q (idx %d), want tool",
			msgs[naive].Role, naive)
	}

	f := &fakeSummarizer{}
	out, err := compressOlderMessages(context.Background(), msgs, f, "fake-model", compactOpts{keepRecentTokens: keep})
	if err != nil {
		t.Fatalf("compress: %v", err)
	}
	if len(out) < 2 {
		t.Fatalf("expected summary + tail, got %d messages", len(out))
	}
	if out[1].Role == "tool" {
		t.Errorf("compressed tail still starts with a tool message — the fix didn't take:\n%+v", out[1])
	}
	// Stronger invariant: every "tool" in the output must be preceded
	// somewhere upstream by an assistant.tool_calls. Spot-check by
	// looking for any tool that doesn't follow an assistant directly
	// (or after another tool from the same round).
	for i := 1; i < len(out); i++ {
		if out[i].Role != "tool" {
			continue
		}
		// Walk backwards skipping prior tools in the same round.
		j := i - 1
		for j >= 0 && out[j].Role == "tool" {
			j--
		}
		if j < 0 || out[j].Role != "assistant" || len(out[j].ToolCalls) == 0 {
			t.Errorf("tool at idx %d has no parent assistant.tool_calls in output", i)
		}
	}
}

func TestCompactThresholdUsesModelWindow(t *testing.T) {
	got := CompactThreshold(200000, 8192)
	want := 200000 - 8192 - compactPromptReserve
	if got != want {
		t.Fatalf("CompactThreshold(200000, 8192) = %d, want %d", got, want)
	}
}

func TestCompactThresholdFallsBackToDefaultWindow(t *testing.T) {
	got := CompactThreshold(0, 0)
	want := CompactThreshold(DefaultContextWindow, 8192)
	if got != want {
		t.Fatalf("unset window = %d, want default %d", got, want)
	}
	if got <= 80000 {
		t.Fatalf("default compact threshold %d is still the old 80k floor", got)
	}
}

func TestCompactThresholdSmallWindowStillCompacts(t *testing.T) {
	got := CompactThreshold(32000, 8192)
	if got <= 0 || got >= 32000 {
		t.Fatalf("32k window threshold %d should leave room inside the window", got)
	}
}

func TestLookupContextWindowFallsBackToKnownPreset(t *testing.T) {
	provs := map[string]config.ProviderConfig{
		"openai": {Models: []config.ModelEntry{{ID: "gpt-5.5"}}},
	}
	if w := lookupContextWindow(provs, "openai/gpt-5.5"); w != 1_050_000 {
		t.Fatalf("zero-saved window should use preset, got %d", w)
	}
	if w := lookupContextWindow(nil, "anthropic/claude-opus-4-7"); w != 1_000_000 {
		t.Fatalf("no catalog should still resolve preset, got %d", w)
	}
	saved := map[string]config.ProviderConfig{
		"openai": {Models: []config.ModelEntry{{ID: "gpt-5.5", ContextWindow: 128000}}},
	}
	if w := lookupContextWindow(saved, "openai/gpt-5.5"); w != 128000 {
		t.Fatalf("saved window should win over preset, got %d", w)
	}
}

func TestLookupContextWindowPrefersProviderPrefix(t *testing.T) {
	provs := map[string]config.ProviderConfig{
		"openai": {Models: []config.ModelEntry{{ID: "gpt-4o", ContextWindow: 128000}}},
		"other":  {Models: []config.ModelEntry{{ID: "gpt-4o", ContextWindow: 32000}}},
	}
	if w := lookupContextWindow(provs, "openai/gpt-4o"); w != 128000 {
		t.Fatalf("prefixed lookup = %d, want 128000", w)
	}
	if w := lookupContextWindow(provs, "other/gpt-4o"); w != 32000 {
		t.Fatalf("other prefix = %d, want 32000", w)
	}
}

func TestCompactMessagesRespectsThreshold(t *testing.T) {
	msgs := []provider.Message{{Role: "user", Content: "hi"}}
	res, err := CompactMessages(context.Background(), msgs, t.TempDir(), nil, "m", 1_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if res.Pruned {
		t.Fatal("short history should not compact under a high threshold")
	}

	var long []provider.Message
	for i := 0; i < 25; i++ {
		long = append(long,
			provider.Message{Role: "user", Content: "u"},
			provider.Message{Role: "tool", ToolCallID: "t", Content: strings.Repeat("x", 400)},
		)
	}
	// No provider → summarize is skipped. A small hot tail lets local
	// prune blank the older tool dumps and fit under 2000.
	res, err = CompactMessagesWith(context.Background(), long, t.TempDir(), nil, "m", 2000, CompactOptions{KeepRecentTokens: 200})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Pruned {
		t.Fatal("oversized history should compact when threshold is 2000")
	}
	if res.Method != compactMethodPrune {
		t.Fatalf("nil provider should fall back to prune, got %q", res.Method)
	}
}

func TestCompactMessagesPrefersSummarizeOverLocalPrune(t *testing.T) {
	var msgs []provider.Message
	for i := 0; i < 25; i++ {
		msgs = append(msgs,
			provider.Message{Role: "user", Content: "u", Origin: provider.OriginUser},
			provider.Message{Role: "tool", Name: "web_search", ToolCallID: "t", Content: strings.Repeat("finding-"+fmt.Sprint(i)+"-", 40), Origin: provider.OriginUser},
		)
	}
	// Prune-alone would fit (old tool dumps become placeholders), but a
	// live summarizer must win so those findings reach the summary.
	f := &fakeSummarizer{}
	res, err := CompactMessagesWith(context.Background(), msgs, t.TempDir(), f, "m", 2000, CompactOptions{KeepRecentTokens: 200})
	if err != nil {
		t.Fatal(err)
	}
	if res.Method != compactMethodSummarize {
		t.Fatalf("method = %q, want %s", res.Method, compactMethodSummarize)
	}
	if !strings.Contains(f.gotSummaryRequest, "finding-0-") {
		t.Fatalf("summarizer should see original tool results, got %q", f.gotSummaryRequest)
	}
	if strings.Contains(f.gotSummaryRequest, truncatedPlaceholder) {
		t.Fatal("local prune must not blank tool results before summarize")
	}
}

func TestCompressOlderMessagesIncludesToolResults(t *testing.T) {
	var msgs []provider.Message
	for i := 0; i < PruneTurnAge+3; i++ {
		msgs = append(msgs,
			provider.Message{Role: "user", Content: "u", Origin: provider.OriginUser},
			provider.Message{Role: "tool", Name: "read_file", Content: "SECRET_FILE_BODY", Origin: provider.OriginUser},
		)
	}
	f := &fakeSummarizer{}
	if _, err := compressOlderMessages(context.Background(), msgs, f, "m", compactOpts{keepRecentTokens: 40}); err != nil {
		t.Fatalf("compress: %v", err)
	}
	if !strings.Contains(f.gotSummaryRequest, "SECRET_FILE_BODY") {
		t.Fatalf("tool result missing from summarizer input: %s", f.gotSummaryRequest)
	}
	if !strings.Contains(f.gotSummaryRequest, "[tool read_file]") {
		t.Fatalf("tool label missing from summarizer input: %s", f.gotSummaryRequest)
	}
	if !strings.Contains(f.gotSummaryRequest, "## Goal") {
		t.Fatalf("structured handoff sections missing from summarizer prompt: %s", f.gotSummaryRequest)
	}
}

type ctxProbe struct {
	gotNil bool
}

func (c *ctxProbe) Chat(ctx context.Context, _ []provider.Message, _ []provider.Tool, _ string, _ int, _ float64) (*provider.Response, error) {
	if ctx == nil {
		c.gotNil = true
		return nil, fmt.Errorf("net/http: nil Context")
	}
	return &provider.Response{Content: "ok"}, nil
}

func (c *ctxProbe) ChatStream(context.Context, []provider.Message, []provider.Tool, string, int, float64) (*provider.StreamReader, error) {
	return nil, nil
}

func TestCompressOlderMessagesPassesNonNilContext(t *testing.T) {
	var msgs []provider.Message
	blob := strings.Repeat("u", 80)
	for i := 0; i < PruneTurnAge+3; i++ {
		msgs = append(msgs,
			provider.Message{Role: "user", Content: blob},
			provider.Message{Role: "assistant", Content: blob},
		)
	}
	p := &ctxProbe{}
	if _, err := compressOlderMessages(context.Background(), msgs, p, "m", compactOpts{keepRecentTokens: 40}); err != nil {
		t.Fatalf("compress: %v", err)
	}
	if p.gotNil {
		t.Fatal("summarizer received a nil context — that is net/http: nil Context")
	}
}

type failSummarizer struct{}

func (failSummarizer) Chat(context.Context, []provider.Message, []provider.Tool, string, int, float64) (*provider.Response, error) {
	return nil, fmt.Errorf("API error 400: invalid_request_error")
}

func (failSummarizer) ChatStream(context.Context, []provider.Message, []provider.Tool, string, int, float64) (*provider.StreamReader, error) {
	return nil, nil
}

func TestCompactMessagesHardTrimsWhenSummarizeFails(t *testing.T) {
	var msgs []provider.Message
	blob := strings.Repeat("x", 8000)
	for i := 0; i < 40; i++ {
		msgs = append(msgs,
			provider.Message{Role: "user", Content: blob},
			provider.Message{Role: "assistant", Content: blob},
		)
	}
	const threshold = 80000
	if EstimateTokens(msgs) < threshold {
		t.Fatalf("fixture too small: tokens=%d", EstimateTokens(msgs))
	}
	res, err := CompactMessages(context.Background(), msgs, t.TempDir(), failSummarizer{}, "m", threshold)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || !res.Pruned {
		t.Fatal("expected a pruned result after failed compression")
	}
	if res.Method != compactMethodHardTrim {
		t.Fatalf("method = %q, want %s", res.Method, compactMethodHardTrim)
	}
	if got := EstimateTokens(res.Messages); got >= threshold {
		t.Fatalf("hard-trim left %d tokens (threshold %d)", got, threshold)
	}
	if notice := compactionNotice(res); !strings.Contains(notice, "/new") {
		t.Fatalf("hard-trim notice should suggest /new, got %q", notice)
	}
}

func TestCompactionNoticeSuggestsNew(t *testing.T) {
	if compactionNotice(nil) != "" || compactionNotice(&CompactResult{}) != "" {
		t.Fatal("empty result should have no notice")
	}
	if strings.Contains(compactionNotice(&CompactResult{Pruned: true, Method: compactMethodSummarize}), "/new") {
		t.Fatal("successful summarize should not push /new as the recovery")
	}
	if strings.Contains(compactionNotice(&CompactResult{Pruned: true, Method: compactMethodPrune}), "/new") {
		t.Fatal("local prune should not push /new as the recovery")
	}
	got := compactionNotice(&CompactResult{Pruned: true, Method: compactMethodHardTrim})
	if !strings.Contains(got, "/new") {
		t.Fatalf("hard-trim notice should suggest /new, got %q", got)
	}
	if !strings.Contains(got, "dropped") {
		t.Error("hard-trim notice should say older turns were dropped")
	}
}

func TestKeepRecentCutoffNeverStartsOnTool(t *testing.T) {
	blob := strings.Repeat("y", 400)
	msgs := []provider.Message{
		{Role: "user", Content: blob},
		{Role: "assistant", Content: blob, ToolCalls: []provider.ToolCall{{ID: "t1", Function: provider.FunctionCall{Name: "read_file", Arguments: `{"path":"a.go"}`}}}},
		{Role: "tool", ToolCallID: "t1", Content: blob, Name: "read_file"},
		{Role: "assistant", Content: blob},
	}
	got := keepRecentCutoff(msgs, 150)
	if got >= len(msgs) {
		t.Fatalf("cutoff %d past end", got)
	}
	if msgs[got].Role == "tool" {
		t.Fatalf("keepRecentCutoff landed on tool at %d", got)
	}
}

func TestIsContextOverflowError(t *testing.T) {
	if isContextOverflowError(nil) {
		t.Fatal("nil is not overflow")
	}
	if isContextOverflowError(fmt.Errorf("API error 401: unauthorized")) {
		t.Fatal("auth error is not overflow")
	}
	if !isContextOverflowError(fmt.Errorf("API error 400: this model's maximum context length is 200000 tokens")) {
		t.Fatal("context length 400 should match")
	}
	if !isContextOverflowError(fmt.Errorf("prompt is too long")) {
		t.Fatal("prompt too long should match")
	}
	if !isContextOverflowError(fmt.Errorf("context_length_exceeded")) {
		t.Fatal("context_length_exceeded should match")
	}
}

func TestCompressOlderMessagesAddsFileTagsAndInstructions(t *testing.T) {
	blob := strings.Repeat("z", 200)
	var msgs []provider.Message
	for i := 0; i < 8; i++ {
		msgs = append(msgs, provider.Message{Role: "user", Content: blob, Origin: provider.OriginUser})
	}
	msgs = append([]provider.Message{
		{Role: "assistant", Content: blob, ToolCalls: []provider.ToolCall{
			{ID: "r1", Function: provider.FunctionCall{Name: "read_file", Arguments: `{"path":"internal/agent/loop.go"}`}},
			{ID: "w1", Function: provider.FunctionCall{Name: "write_file", Arguments: `{"path":"internal/agent/compaction.go"}`}},
		}},
		{Role: "tool", ToolCallID: "r1", Name: "read_file", Content: blob, Origin: provider.OriginUser},
		{Role: "tool", ToolCallID: "w1", Name: "write_file", Content: "Wrote internal/agent/compaction.go", Origin: provider.OriginUser},
	}, msgs...)

	f := &fakeSummarizer{}
	out, err := compressOlderMessages(context.Background(), msgs, f, "m", compactOpts{
		keepRecentTokens: 80,
		instructions:     "keep the API contract",
	})
	if err != nil {
		t.Fatalf("compress: %v", err)
	}
	if !strings.Contains(f.gotSummaryRequest, "keep the API contract") {
		t.Fatalf("instructions missing from summarizer prompt: %s", f.gotSummaryRequest)
	}
	if !strings.Contains(f.gotSummaryRequest, "## Next Steps") {
		t.Fatalf("structured sections missing: %s", f.gotSummaryRequest)
	}
	if len(out) == 0 || !strings.Contains(out[0].Content, conversationSummaryMark) {
		t.Fatal("compressed output should start with a conversation summary")
	}
	if !strings.Contains(out[0].Content, "<read-files>") || !strings.Contains(out[0].Content, "internal/agent/loop.go") {
		t.Fatalf("read-files tag missing: %s", out[0].Content)
	}
	if !strings.Contains(out[0].Content, "<modified-files>") || !strings.Contains(out[0].Content, "internal/agent/compaction.go") {
		t.Fatalf("modified-files tag missing: %s", out[0].Content)
	}
}

func TestCompactMessagesForceSummarizesUnderThreshold(t *testing.T) {
	blob := strings.Repeat("w", 200)
	msgs := []provider.Message{
		{Role: "user", Content: blob, Origin: provider.OriginUser},
		{Role: "assistant", Content: blob, Origin: provider.OriginUser},
		{Role: "user", Content: blob, Origin: provider.OriginUser},
		{Role: "assistant", Content: blob, Origin: provider.OriginUser},
	}
	f := &fakeSummarizer{}
	res, err := CompactMessagesWith(context.Background(), msgs, t.TempDir(), f, "m", 1_000_000, CompactOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Method != compactMethodSummarize {
		t.Fatalf("force under threshold should still summarize, got %q", res.Method)
	}
	if !res.Pruned {
		t.Fatal("force summarize should report pruned")
	}
}

func TestKeepRecentCutoffKeepsTokenBudget(t *testing.T) {
	blob := strings.Repeat("n", 400) // 100 tokens each
	var msgs []provider.Message
	for i := 0; i < 10; i++ {
		msgs = append(msgs, provider.Message{Role: "user", Content: blob})
	}
	got := keepRecentCutoff(msgs, 250)
	if got != 7 {
		t.Fatalf("cutoff = %d, want 7 (keep last 3 × 100-token messages)", got)
	}
	if EstimateTokens(msgs[got:]) != 300 {
		t.Fatalf("tail tokens = %d, want 300", EstimateTokens(msgs[got:]))
	}
	if keepRecentCutoff(msgs, 2000) != 0 {
		t.Fatal("history inside the budget should have cutoff 0")
	}
}

func TestSerializeConversationCapsToolResults(t *testing.T) {
	huge := strings.Repeat("DUMP", 1000) // 4000 runes
	got := serializeConversation([]provider.Message{
		{Role: "tool", Name: "exec", Content: huge, Origin: provider.OriginUser},
	})
	if strings.Contains(got, huge) {
		t.Fatal("summarizer serialization must cap oversized tool results")
	}
	if !strings.Contains(got, "[tool exec]") {
		t.Fatalf("tool label missing: %s", got)
	}
	if n := len([]rune(got)); n > summarizeToolMaxRunes+80 {
		t.Fatalf("serialized tool dump still too long: %d runes", n)
	}
}

func TestCompressOlderMessagesFeedsPreviousSummary(t *testing.T) {
	blob := strings.Repeat("p", 200)
	prev := conversationSummaryMark + "\n## Goal\nship the compact fix"
	msgs := []provider.Message{
		{Role: "user", Content: prev, Origin: provider.OriginUser},
		{Role: "assistant", Content: blob, Origin: provider.OriginUser},
		{Role: "user", Content: blob, Origin: provider.OriginUser},
		{Role: "assistant", Content: blob, Origin: provider.OriginUser},
		{Role: "user", Content: blob, Origin: provider.OriginUser},
		{Role: "assistant", Content: blob, Origin: provider.OriginUser},
	}
	f := &fakeSummarizer{}
	if _, err := compressOlderMessages(context.Background(), msgs, f, "m", compactOpts{keepRecentTokens: 80}); err != nil {
		t.Fatalf("compress: %v", err)
	}
	if !strings.Contains(f.gotSummaryRequest, "Previous summary") {
		t.Fatalf("iterative compact should send the last handoff back in: %s", f.gotSummaryRequest)
	}
	if !strings.Contains(f.gotSummaryRequest, "ship the compact fix") {
		t.Fatalf("previous goal missing from summarizer prompt: %s", f.gotSummaryRequest)
	}
}

func TestCompactMessagesWithOversizedToolSession(t *testing.T) {
	// Mirrors the original overflow shape: many exec dumps that would
	// 400 a 32k-class window if left intact. Summarize must win, keep
	// a recent verbatim tail, and write the full JSONL archive.
	dump := strings.Repeat("search-hit-42-", 400) // 5600 chars ≈ 1400 tokens
	var msgs []provider.Message
	for i := 0; i < 30; i++ {
		msgs = append(msgs,
			provider.Message{Role: "user", Content: fmt.Sprintf("round-%d", i), Origin: provider.OriginUser},
			provider.Message{Role: "assistant", Content: "working", Origin: provider.OriginUser, ToolCalls: []provider.ToolCall{
				{ID: fmt.Sprintf("t%d", i), Function: provider.FunctionCall{Name: "web_search", Arguments: `{"query":"fastclaw"}`}},
			}},
			provider.Message{Role: "tool", Name: "web_search", ToolCallID: fmt.Sprintf("t%d", i), Content: dump, Origin: provider.OriginUser},
		)
	}
	const threshold = 8000
	before := EstimateTokens(msgs)
	if before < threshold {
		t.Fatalf("fixture too small: tokens=%d", before)
	}

	dir := t.TempDir()
	f := &fakeSummarizer{}
	res, err := CompactMessagesWith(context.Background(), msgs, dir, f, "m", threshold, CompactOptions{KeepRecentTokens: 2000})
	if err != nil {
		t.Fatal(err)
	}
	if res.Method != compactMethodSummarize {
		t.Fatalf("method = %q, want %s", res.Method, compactMethodSummarize)
	}
	after := EstimateTokens(res.Messages)
	if after >= threshold {
		t.Fatalf("working set still over budget: %d >= %d", after, threshold)
	}
	if after >= before {
		t.Fatalf("compaction did not shrink tokens: before=%d after=%d", before, after)
	}
	if len(res.Messages) == 0 || !strings.Contains(res.Messages[0].Content, conversationSummaryMark) {
		t.Fatal("working set should start with the structured summary")
	}
	foundTail := false
	for _, m := range res.Messages[1:] {
		if m.Role == "user" && m.Content == "round-29" {
			foundTail = true
			break
		}
	}
	if !foundTail {
		t.Fatal("most recent user turn missing from the verbatim tail")
	}
	if !strings.Contains(f.gotSummaryRequest, "search-hit-42-") {
		t.Fatal("summarizer should see original tool findings, not placeholders")
	}
	if strings.Contains(f.gotSummaryRequest, truncatedPlaceholder) {
		t.Fatal("local prune must not blank tool results before summarize")
	}
	if !strings.Contains(f.gotSummaryRequest, "## Goal") {
		t.Fatal("structured handoff prompt missing")
	}
	if res.LogFile == "" {
		t.Fatal("expected a JSONL history archive")
	}
	if notice := compactionNotice(res); strings.Contains(notice, "/new") {
		t.Fatalf("successful summarize should not push /new, got %q", notice)
	}
}
