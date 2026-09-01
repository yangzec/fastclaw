package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

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
	out, err := compressOlderMessages(context.Background(), msgs, f, "fake-model")
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
	out, err := compressOlderMessages(context.Background(), in, nil, "")
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
	// Rounds of [assistant(2 tool_calls), tool, tool] — 3 messages
	// each. 7 rounds = 21 messages. With 5 user fillers in front,
	// total len = 26 and naive cutoff = 26-PruneTurnAge = 6, which
	// indexes a tool reply (assistant at 5, tool at 6, tool at 7).
	var msgs []provider.Message
	for i := 0; i < 5; i++ {
		msgs = append(msgs, provider.Message{Role: "user", Content: "filler"})
	}
	for i := 0; i < 7; i++ {
		msgs = append(msgs,
			provider.Message{Role: "assistant", ToolCalls: []provider.ToolCall{{ID: "ta"}, {ID: "tb"}}},
			provider.Message{Role: "tool", ToolCallID: "ta", Content: "ra"},
			provider.Message{Role: "tool", ToolCallID: "tb", Content: "rb"},
		)
	}
	// Pin the fixture: without the fix, the tail would start with a
	// "tool" message and OpenAI would 400.
	naive := len(msgs) - PruneTurnAge
	if msgs[naive].Role != "tool" {
		t.Fatalf("fixture broken: naive cutoff lands on %q (idx %d, len %d), want tool",
			msgs[naive].Role, naive, len(msgs))
	}

	f := &fakeSummarizer{}
	out, err := compressOlderMessages(context.Background(), msgs, f, "fake-model")
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

// ctxProbe records whether compressOlderMessages passed a nil context —
// the production bug that surfaced as `net/http: nil Context`.
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
	for i := 0; i < PruneTurnAge+3; i++ {
		msgs = append(msgs,
			provider.Message{Role: "user", Content: "u"},
			provider.Message{Role: "assistant", Content: "a"},
		)
	}
	p := &ctxProbe{}
	if _, err := compressOlderMessages(context.Background(), msgs, p, "m"); err != nil {
		t.Fatalf("compress: %v", err)
	}
	if p.gotNil {
		t.Fatal("summarizer received a nil context — that is net/http: nil Context")
	}
}

func TestCompressOlderMessagesNilContextFallsBackToBackground(t *testing.T) {
	var msgs []provider.Message
	for i := 0; i < PruneTurnAge+3; i++ {
		msgs = append(msgs, provider.Message{Role: "user", Content: "u"})
	}
	p := &ctxProbe{}
	if _, err := compressOlderMessages(nil, msgs, p, "m"); err != nil {
		t.Fatalf("nil ctx should be replaced with Background: %v", err)
	}
	if p.gotNil {
		t.Fatal("nil ctx leaked through to Chat")
	}
}

type failSummarizer struct{}

func (failSummarizer) Chat(context.Context, []provider.Message, []provider.Tool, string, int, float64) (*provider.Response, error) {
	return nil, fmt.Errorf("API error 400: invalid_request_error")
}

func (failSummarizer) ChatStream(context.Context, []provider.Message, []provider.Tool, string, int, float64) (*provider.StreamReader, error) {
	return nil, nil
}

func oversizedHistory() []provider.Message {
	var msgs []provider.Message
	// Far above DefaultTokenThreshold even after old-tool prune:
	// 40 user+assistant pairs × 8k chars ≈ 160k tokens.
	blob := strings.Repeat("x", 8000)
	for i := 0; i < 40; i++ {
		msgs = append(msgs,
			provider.Message{Role: "user", Content: blob},
			provider.Message{Role: "assistant", Content: blob},
		)
	}
	return msgs
}

func TestCompactMessagesHardTrimsWhenSummarizeFails(t *testing.T) {
	msgs := oversizedHistory()
	before := EstimateTokens(msgs)
	if before < DefaultTokenThreshold {
		t.Fatalf("fixture too small: tokens=%d", before)
	}
	res, err := CompactMessages(context.Background(), msgs, t.TempDir(), failSummarizer{}, "m")
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || !res.Pruned {
		t.Fatal("expected a pruned result after failed compression")
	}
	if res.Method != compactMethodHardTrim {
		t.Fatalf("method = %q, want %s", res.Method, compactMethodHardTrim)
	}
	if notice := compactionNotice(res); !strings.Contains(notice, "/new") {
		t.Fatalf("hard-trim notice should suggest /new, got %q", notice)
	}
	got := EstimateTokens(res.Messages)
	if got >= DefaultTokenThreshold {
		t.Fatalf("hard-trim left %d tokens (threshold %d) — session would still 400", got, DefaultTokenThreshold)
	}
	if len(res.Messages) == 0 {
		t.Fatal("hard-trim dropped the entire history")
	}
	if res.Messages[0].Content != droppedHistoryNotice && got > 0 {
		// notice is prepended unless the whole list already fit after tool prune
		if EstimateTokens(msgs) >= DefaultTokenThreshold && res.Messages[0].Role != "user" {
			t.Errorf("expected dropped-history notice or a user tail start, got %+v", res.Messages[0])
		}
	}
}

func TestCapSummaryTextTruncatesOversizedInput(t *testing.T) {
	long := strings.Repeat("你", summarizeMaxRunes+50)
	got := capSummaryText(long)
	if len([]rune(got)) <= summarizeMaxRunes {
		t.Fatalf("capped text should include the omission marker, got %d runes", len([]rune(got)))
	}
	if !strings.Contains(got, "omitted from summarizer") {
		t.Fatalf("missing omission marker: %s", got[len(got)-80:])
	}
	if !strings.HasPrefix(got, strings.Repeat("你", 8)) {
		t.Fatal("cap should keep the start of the conversation")
	}
}

func TestHardTrimMessagesNeverStartsWithTool(t *testing.T) {
	var msgs []provider.Message
	blob := strings.Repeat("y", 4000)
	for i := 0; i < 30; i++ {
		msgs = append(msgs,
			provider.Message{Role: "user", Content: blob},
			provider.Message{Role: "assistant", ToolCalls: []provider.ToolCall{{ID: "t"}}},
			provider.Message{Role: "tool", ToolCallID: "t", Content: blob},
		)
	}
	out := hardTrimMessages(msgs, 2000)
	if EstimateTokens(out) > 2000 {
		t.Fatalf("tokens=%d want <= 2000", EstimateTokens(out))
	}
	for i := 1; i < len(out); i++ {
		if out[i].Role != "tool" {
			continue
		}
		j := i - 1
		for j >= 0 && out[j].Role == "tool" {
			j--
		}
		if j < 0 || out[j].Role != "assistant" || len(out[j].ToolCalls) == 0 {
			t.Fatalf("tool at %d has no parent assistant.tool_calls", i)
		}
	}
}

func TestCompactionNoticeSuggestsNew(t *testing.T) {
	if compactionNotice(nil) != "" {
		t.Fatal("nil result should have no notice")
	}
	if compactionNotice(&CompactResult{}) != "" {
		t.Fatal("unpruned result should have no notice")
	}
	for _, method := range []string{compactMethodPrune, compactMethodSummarize, compactMethodHardTrim} {
		got := compactionNotice(&CompactResult{Pruned: true, Method: method})
		if !strings.Contains(got, "/new") {
			t.Errorf("method %s notice missing /new: %q", method, got)
		}
	}
	if !strings.Contains(compactionNotice(&CompactResult{Pruned: true, Method: compactMethodHardTrim}), "dropped") {
		t.Error("hard-trim notice should say older turns were dropped")
	}
}

func TestEstimateTokensCountsThinkingAndParts(t *testing.T) {
	msgs := []provider.Message{
		{Role: "assistant", Content: strings.Repeat("a", 40), Thinking: strings.Repeat("t", 40)},
		{Role: "user", ContentParts: []provider.ContentPart{{Type: "text", Text: strings.Repeat("p", 40)}}},
	}
	got := EstimateTokens(msgs)
	if got != 30 { // 40/4 + 40/4 + 40/4
		t.Fatalf("EstimateTokens = %d, want 30", got)
	}
}
