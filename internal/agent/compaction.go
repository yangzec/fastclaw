package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/config"
	"github.com/fastclaw-ai/fastclaw/internal/provider"
)

const (
	// DefaultContextWindow matches the Models UI default when a model
	// row has no contextWindow set. Compaction used to ignore this and
	// fire at a hardcoded 80k — far below typical 128k/200k windows.
	DefaultContextWindow = 200000
	// compactPromptReserve covers system prompt + tool schemas added
	// after session-history compaction.
	compactPromptReserve = 16384
	// compactMinThreshold is the lowest trigger so a small window still
	// compact before overflowing.
	compactMinThreshold = 4096
	// PruneTurnAge is the number of recent turns to keep intact; older messages get pruned.
	PruneTurnAge = 20
	// truncatedPlaceholder replaces pruned tool results.
	truncatedPlaceholder = "[Result truncated - see memory logs]"
	// summarizeMaxRunes caps the text shipped to the summarizer so the
	// compression call itself cannot 400 on an oversized prompt. ~20k
	// tokens at chars/4 — enough to keep key facts, not the whole dump.
	summarizeMaxRunes = 80000
	// summarizeToolMaxRunes keeps each tool result in the summarizer
	// prompt without letting one exec/search dump eat the whole budget.
	summarizeToolMaxRunes = 4000
	droppedHistoryNotice  = "[Earlier turns were dropped to fit the model context window. Full history is in memory logs.]"
)

const (
	compactMethodPrune     = "prune"
	compactMethodSummarize = "summarize"
	compactMethodHardTrim  = "hard_trim"
)

// CompactThreshold is the session-history size at which compaction
// starts. It leaves room for the completion (maxTokens) and for the
// system prompt / tool schemas that are attached after compaction.
// contextWindow 0 falls back to DefaultContextWindow.
func CompactThreshold(contextWindow, maxTokens int) int {
	window := contextWindow
	if window <= 0 {
		window = DefaultContextWindow
	}
	reserve := maxTokens
	if reserve <= 0 {
		reserve = 8192
	}
	reserve += compactPromptReserve
	if tenth := window / 10; reserve < tenth {
		reserve = tenth
	}
	thresh := window - reserve
	if thresh < compactMinThreshold {
		thresh = window / 2
		if thresh < compactMinThreshold {
			thresh = compactMinThreshold
		}
	}
	return thresh
}

// lookupContextWindow finds the current model's contextWindow in the
// agent's provider catalog. "provider/modelId" prefers that provider;
// a bare model id searches every provider. 0 means not configured.
func lookupContextWindow(providers map[string]config.ProviderConfig, model string) int {
	if model == "" {
		return 0
	}
	provKey, modelID := provider.SplitProviderModel(model)
	if provKey != "" {
		if pc, ok := providers[provKey]; ok {
			if w := modelContextWindow(pc.Models, modelID); w > 0 {
				return w
			}
		}
	}
	for _, pc := range providers {
		if w := modelContextWindow(pc.Models, modelID); w > 0 {
			return w
		}
		if modelID != model {
			if w := modelContextWindow(pc.Models, model); w > 0 {
				return w
			}
		}
	}
	return config.KnownContextWindow(model)
}

func modelContextWindow(models []config.ModelEntry, id string) int {
	for _, m := range models {
		if m.ID == id && m.ContextWindow > 0 {
			return m.ContextWindow
		}
	}
	return 0
}

// EstimateTokens provides a rough token estimate: chars/4.
func EstimateTokens(messages []provider.Message) int {
	total := 0
	for _, m := range messages {
		if m.Content != "" {
			total += len(m.Content) / 4
		} else {
			for _, p := range m.ContentParts {
				total += len(p.Text) / 4
			}
		}
		if m.Thinking != "" {
			total += len(m.Thinking) / 4
		}
		for _, tc := range m.ToolCalls {
			total += len(tc.Function.Arguments) / 4
			total += len(tc.Function.Name) / 4
		}
	}
	return total
}

// CompactResult holds the result of a compaction operation.
type CompactResult struct {
	Messages []provider.Message
	Pruned   bool
	// Method is prune, summarize, or hard_trim when Pruned is true.
	Method  string
	LogFile string
}

// compactionNotice is the user-visible line after auto-compaction.
// Chat UI still shows the archived full thread, but the model only
// sees the compacted working set — so we tell the user and point
// them at /new rather than letting the model silently "forget".
func compactionNotice(r *CompactResult) string {
	if r == nil || !r.Pruned {
		return ""
	}
	switch r.Method {
	case compactMethodHardTrim:
		return "⚠️ This session was too long to keep intact — older turns were dropped so the model request would not be rejected. Send /new to start a clean session."
	case compactMethodSummarize:
		return "📦 Earlier turns were automatically summarized to fit the context window. Send /new if you want a clean session."
	default:
		return "📦 Context was compacted (older tool results trimmed). Send /new if replies start to miss earlier details."
	}
}

// compactionModelHint is injected as a system message for the current
// turn so the model does not pretend it still has the dropped history.
func compactionModelHint(method string) string {
	switch method {
	case compactMethodHardTrim:
		return "The session working set was just hard-trimmed to fit the model context window. Older turns are gone from your messages. Do not claim you remember details that are not present. If the user needs the earlier thread, tell them to send /new."
	case compactMethodSummarize:
		return "The session working set was just compacted: older turns were replaced with a short summary. Treat the summary as incomplete. If the user needs the original thread, tell them to send /new."
	default:
		return "Older tool results in this session were just truncated to fit the context window. If the user refers to a missing tool output, tell them to send /new."
	}
}

// CompactMessages compresses the message history when it exceeds the
// token threshold. Full history is written to a log file first.
//
// Step 1 (Summarize): Ask the current model to fold older turns into a
// short summary. Tool findings stay in the summarizer input (capped)
// so the cheap local "delete old tool dumps" pass is not the first
// knife — those results are usually the facts the next turn needs.
//
// Step 2 (Prune): If summarize is unavailable or fails, strip oversized
// tool results older than PruneTurnAge. Last resort before dropping
// whole turns.
//
// Step 3 (Hard trim): Drop oldest turns (keeping a valid tool-call
// tail) until the history fits. Without this a session that failed to
// summarize would keep sending oversized requests and 400 every turn.
//
// threshold 0 uses CompactThreshold with the default context window.
func CompactMessages(ctx context.Context, messages []provider.Message, workspace string, prov provider.Provider, model string, threshold int) (*CompactResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if threshold <= 0 {
		threshold = CompactThreshold(0, 0)
	}
	tokens := EstimateTokens(messages)
	if tokens < threshold {
		return &CompactResult{Messages: messages}, nil
	}

	slog.Info("context compaction triggered", "tokens", tokens, "threshold", threshold, "message_count", len(messages))

	// Write full history to log file before any modifications
	logFile, err := writeHistoryLog(messages, workspace)
	if err != nil {
		slog.Warn("failed to write history log", "error", err)
	}

	// Step 1: LLM summary first — preserve meaning, including tool
	// findings, instead of blanking old tool results just to save a call.
	if len(messages) > PruneTurnAge && prov != nil {
		compressed, err := compressOlderMessages(ctx, messages, prov, model)
		if err == nil {
			after := EstimateTokens(compressed)
			slog.Info("after compression", "tokens_before", tokens, "tokens_after", after)
			method := compactMethodSummarize
			if after >= threshold {
				slog.Warn("compression still over threshold, hard-trimming", "tokens", after, "threshold", threshold)
				compressed = hardTrimMessages(compressed, threshold)
				method = compactMethodHardTrim
			}
			return &CompactResult{
				Messages: compressed,
				Pruned:   true,
				Method:   method,
				LogFile:  logFile,
			}, nil
		}
		slog.Warn("compression failed, falling back to local shrink", "error", err)
	}

	// Step 2: Local prune — only when summarize did not run or failed.
	pruned := pruneOldToolResults(messages)
	prunedTokens := EstimateTokens(pruned)
	slog.Info("after pruning", "tokens_before", tokens, "tokens_after", prunedTokens)
	if prunedTokens < threshold {
		return &CompactResult{
			Messages: pruned,
			Pruned:   true,
			Method:   compactMethodPrune,
			LogFile:  logFile,
		}, nil
	}

	// Step 3: Hard trim
	return &CompactResult{
		Messages: hardTrimMessages(pruned, threshold),
		Pruned:   true,
		Method:   compactMethodHardTrim,
		LogFile:  logFile,
	}, nil
}

// safeCompactionCutoff advances cutoff forward past any leading tool
// messages so the recent tail never starts with a "tool" role. If we
// shipped a list of the form [summary_user, tool, ...] to the
// provider, OpenAI-compatible APIs would reject with:
//
//	"Messages with role 'tool' must be a response to a preceding
//	 message with 'tool_calls'"
//
// — the previous assistant.tool_calls that "tool" was answering got
// swallowed into the summary on its own side of cutoff. Anthropic
// won't 400 on this but the contract is the same: a tool reply
// without its parent call is semantic garbage.
//
// We only need to step past leading tool messages. If the tail begins
// with assistant(tool_calls), that's fine — its tool replies (if any)
// come AFTER it in the tail.
//
// Pure function; no allocation; safe with cutoff at any value.
func safeCompactionCutoff(messages []provider.Message, cutoff int) int {
	if cutoff < 0 {
		cutoff = 0
	}
	for cutoff < len(messages) && messages[cutoff].Role == "tool" {
		cutoff++
	}
	return cutoff
}

// pruneOldToolResults strips tool result content from messages older than PruneTurnAge.
func pruneOldToolResults(messages []provider.Message) []provider.Message {
	if len(messages) <= PruneTurnAge {
		return messages
	}
	return pruneToolResultsBefore(messages, len(messages)-PruneTurnAge)
}

func pruneToolResultsBefore(messages []provider.Message, cutoff int) []provider.Message {
	if cutoff <= 0 {
		return messages
	}
	if cutoff > len(messages) {
		cutoff = len(messages)
	}
	result := make([]provider.Message, len(messages))
	copy(result, messages)

	for i := 0; i < cutoff; i++ {
		if result[i].Role == "tool" && len(result[i].Content) > 200 {
			result[i] = provider.Message{
				Role:       "tool",
				Content:    truncatedPlaceholder,
				ToolCallID: result[i].ToolCallID,
				Name:       result[i].Name,
			}
		}
	}

	return result
}

// hardTrimMessages is the last-resort fit: truncate every oversized
// tool result, then drop oldest messages until EstimateTokens is
// under threshold. The tail never starts with a lone "tool" role.
func hardTrimMessages(messages []provider.Message, threshold int) []provider.Message {
	if threshold <= 0 {
		threshold = CompactThreshold(0, 0)
	}
	trimmed := pruneToolResultsBefore(messages, len(messages))
	if EstimateTokens(trimmed) <= threshold {
		return trimmed
	}

	notice := provider.Message{Role: "user", Content: droppedHistoryNotice}
	budget := threshold - EstimateTokens([]provider.Message{notice})
	if budget < 1 {
		budget = threshold
	}

	cutoff := 0
	for cutoff < len(trimmed) && EstimateTokens(trimmed[cutoff:]) > budget {
		cutoff++
	}
	cutoff = safeCompactionCutoff(trimmed, cutoff)
	if cutoff <= 0 {
		return trimmed
	}
	if cutoff >= len(trimmed) {
		last := trimmed[len(trimmed)-1]
		for i := len(trimmed) - 1; i >= 0; i-- {
			if trimmed[i].Role != "tool" {
				last = trimmed[i]
				break
			}
		}
		return []provider.Message{notice, shrinkMessage(last, budget)}
	}
	return append([]provider.Message{notice}, trimmed[cutoff:]...)
}

func shrinkMessage(m provider.Message, tokenBudget int) provider.Message {
	if tokenBudget < 1 {
		m.Content = truncatedPlaceholder
		m.Thinking = ""
		m.ContentParts = nil
		m.ToolCalls = nil
		return m
	}
	maxChars := tokenBudget * 4
	if len(m.Content) > maxChars {
		m.Content = m.Content[:maxChars] + "…"
	}
	if len(m.Thinking) > maxChars/4 {
		m.Thinking = ""
	}
	return m
}

func formatMessageForSummary(m provider.Message) string {
	content := m.Content
	if m.Role == "tool" {
		label := m.Name
		if label == "" {
			label = "tool"
		}
		return fmt.Sprintf("[tool %s] %s\n", label, capRunes(content, summarizeToolMaxRunes))
	}
	return fmt.Sprintf("[%s] %s\n", m.Role, content)
}

func capRunes(text string, max int) string {
	if max <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max]) + "…"
}

func capSummaryText(text string) string {
	runes := []rune(text)
	if len(runes) <= summarizeMaxRunes {
		return text
	}
	return string(runes[:summarizeMaxRunes]) + "\n\n[... older turns omitted from summarizer input ...]"
}

// compressOlderMessages asks the LLM to summarize older messages into a compact summary.
func compressOlderMessages(ctx context.Context, messages []provider.Message, prov provider.Provider, model string) ([]provider.Message, error) {
	if len(messages) <= PruneTurnAge {
		return messages, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if prov == nil {
		return nil, fmt.Errorf("summarize conversation: no provider")
	}

	cutoff := safeCompactionCutoff(messages, len(messages)-PruneTurnAge)
	olderMessages := messages[:cutoff]

	// Build a text representation of older messages for summarization.
	// Skip runtime-injected messages (currently only goal_context
	// continuations): their content is synthetic audit scaffolding,
	// not conversation worth summarizing — and the latest one is
	// already preserved verbatim in the recent tail below, so the
	// model never loses the current audit context. This is the
	// pinned-head protection design §5.3 (b) calls for: old
	// goal_context messages are dropped entirely from the
	// compaction output; the live one rides through unchanged.
	// Tool results stay in — they usually hold the facts (search hits,
	// file reads, exec output) the next turn still needs. Each one is
	// capped so a single dump cannot blow the summarizer prompt.
	var text string
	for _, m := range olderMessages {
		if m.Origin != provider.OriginUser {
			continue
		}
		text += formatMessageForSummary(m)
	}

	summaryPrompt := []provider.Message{
		{
			Role:    "system",
			Content: "You are a conversation summarizer. Summarize the following conversation history into a compact summary that preserves key facts, decisions, tool findings, and context. Be concise but don't lose important details.",
		},
		{
			Role:    "user",
			Content: fmt.Sprintf("Summarize this conversation:\n\n%s", capSummaryText(text)),
		},
	}

	resp, err := prov.Chat(ctx, summaryPrompt, nil, model, 2048, 0.3)
	if err != nil {
		return nil, fmt.Errorf("summarize conversation: %w", err)
	}

	// Build new message list: summary + recent messages
	compressed := make([]provider.Message, 0, PruneTurnAge+1)
	compressed = append(compressed, provider.Message{
		Role:    "user",
		Content: fmt.Sprintf("[Conversation Summary]\n%s", resp.Content),
	})
	compressed = append(compressed, messages[cutoff:]...)

	return compressed, nil
}

// writeHistoryLog writes the full message history to a JSONL log file.
func writeHistoryLog(messages []provider.Message, workspace string) (string, error) {
	logDir := filepath.Join(workspace, "memory", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return "", fmt.Errorf("create log dir: %w", err)
	}

	timestamp := time.Now().Format("20060102_150405")
	logFile := filepath.Join(logDir, fmt.Sprintf("history_%s.jsonl", timestamp))

	f, err := os.Create(logFile)
	if err != nil {
		return "", fmt.Errorf("create log file: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, m := range messages {
		if err := enc.Encode(m); err != nil {
			return logFile, fmt.Errorf("encode message: %w", err)
		}
	}

	slog.Info("wrote history log", "file", logFile, "messages", len(messages))
	return logFile, nil
}
