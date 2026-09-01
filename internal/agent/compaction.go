package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/provider"
)

const (
	// DefaultTokenThreshold is the default threshold at which compaction triggers (80K tokens).
	DefaultTokenThreshold = 80000
	// PruneTurnAge is the number of recent turns to keep intact; older messages get pruned.
	PruneTurnAge = 20
	// truncatedPlaceholder replaces pruned tool results.
	truncatedPlaceholder = "[Result truncated - see memory logs]"
	// summarizeMaxRunes caps the text shipped to the summarizer so the
	// compression call itself cannot 400 on an oversized prompt. ~20k
	// tokens at chars/4 — enough to keep key facts, not the whole dump.
	summarizeMaxRunes    = 80000
	droppedHistoryNotice = "[Earlier turns were dropped to fit the model context window. Full history is in memory logs.]"
)

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
	LogFile  string
}

// CompactMessages prunes and optionally compresses the message history when it exceeds the token threshold.
// Step 1 (Pruning): For messages older than PruneTurnAge, strip tool result content.
// Step 2 (Compression): If still over threshold after pruning, summarize older messages
// using the LLM and write full history to a log file.
// Step 3 (Hard trim): If compression fails or the result is still over
// the threshold, drop oldest turns (keeping a valid tool-call tail)
// until the history fits. Without this step a 400k-token session that
// failed to summarize would keep sending 400k-token requests and the
// upstream gateway would 400 every turn.
func CompactMessages(ctx context.Context, messages []provider.Message, workspace string, prov provider.Provider, model string) (*CompactResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	tokens := EstimateTokens(messages)
	if tokens < DefaultTokenThreshold {
		return &CompactResult{Messages: messages}, nil
	}

	slog.Info("context compaction triggered", "tokens", tokens, "threshold", DefaultTokenThreshold, "message_count", len(messages))

	// Write full history to log file before any modifications
	logFile, err := writeHistoryLog(messages, workspace)
	if err != nil {
		slog.Warn("failed to write history log", "error", err)
	}

	// Step 1: Pruning - strip tool results from older messages
	pruned := pruneOldToolResults(messages)
	prunedTokens := EstimateTokens(pruned)

	slog.Info("after pruning", "tokens_before", tokens, "tokens_after", prunedTokens)

	if prunedTokens < DefaultTokenThreshold {
		return &CompactResult{
			Messages: pruned,
			Pruned:   true,
			LogFile:  logFile,
		}, nil
	}

	// Step 2: Compression - summarize older messages
	compressed, err := compressOlderMessages(ctx, pruned, prov, model)
	if err != nil {
		slog.Warn("compression failed, hard-trimming to fit window", "error", err)
		return &CompactResult{
			Messages: hardTrimMessages(pruned, DefaultTokenThreshold),
			Pruned:   true,
			LogFile:  logFile,
		}, nil
	}

	after := EstimateTokens(compressed)
	slog.Info("after compression", "tokens_before", prunedTokens, "tokens_after", after)
	if after >= DefaultTokenThreshold {
		slog.Warn("compression still over threshold, hard-trimming", "tokens", after, "threshold", DefaultTokenThreshold)
		compressed = hardTrimMessages(compressed, DefaultTokenThreshold)
	}

	return &CompactResult{
		Messages: compressed,
		Pruned:   true,
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
		threshold = DefaultTokenThreshold
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
	// Tool results are already placeholders after pruning and add
	// noise to the summarizer prompt, so they are skipped too.
	var text string
	for _, m := range olderMessages {
		if m.Origin != provider.OriginUser {
			continue
		}
		if m.Role == "tool" {
			continue
		}
		text += fmt.Sprintf("[%s] %s\n", m.Role, m.Content)
	}

	summaryPrompt := []provider.Message{
		{
			Role:    "system",
			Content: "You are a conversation summarizer. Summarize the following conversation history into a compact summary that preserves key facts, decisions, and context. Be concise but don't lose important details.",
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
