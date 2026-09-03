package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	// PruneTurnAge is the fallback message-count floor used only when
	// token-based keepRecentCutoff cannot run (empty/short history).
	PruneTurnAge = 20
	// KeepRecentTokens is the Pi-style hot tail: recent messages kept
	// verbatim after a summary. ~20k tokens of raw history, not 20 turns.
	KeepRecentTokens = 20000
	// truncatedPlaceholder replaces pruned tool results.
	truncatedPlaceholder = "[Result truncated - see memory logs]"
	// summarizeMaxRunes caps the text shipped to the summarizer so the
	// compression call itself cannot 400 on an oversized prompt.
	summarizeMaxRunes = 80000
	// summarizeToolMaxRunes matches Pi's serializeConversation cap so
	// one exec/read dump cannot blow the summarizer prompt.
	summarizeToolMaxRunes = 2000
	// compactSummaryMaxTokens is the structured-handoff budget.
	compactSummaryMaxTokens = 4096
	droppedHistoryNotice    = "[Earlier turns were dropped to fit the model context window. Full history is in memory logs.]"
	conversationSummaryMark = "[Conversation Summary]"
	compactionSummaryMark   = "[Compaction]"
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

// lookupMaxTokens finds the current model's completion budget in the
// agent's provider catalog. Same resolution as lookupContextWindow.
// 0 means not configured.
func lookupMaxTokens(providers map[string]config.ProviderConfig, model string) int {
	if model == "" {
		return 0
	}
	provKey, modelID := provider.SplitProviderModel(model)
	if provKey != "" {
		if pc, ok := providers[provKey]; ok {
			if n := modelMaxTokens(pc.Models, modelID); n > 0 {
				return n
			}
		}
	}
	for _, pc := range providers {
		if n := modelMaxTokens(pc.Models, modelID); n > 0 {
			return n
		}
		if modelID != model {
			if n := modelMaxTokens(pc.Models, model); n > 0 {
				return n
			}
		}
	}
	// Unlike contextWindow, an unset maxTokens must not jump to the
	// official capability (often 128k). That number is a form
	// prefill; the request budget stays agents.defaults (8192)
	// until the operator saves a value on the Models page.
	return 0
}

func modelMaxTokens(models []config.ModelEntry, id string) int {
	for _, m := range models {
		if m.ID == id && m.MaxTokens > 0 {
			return m.MaxTokens
		}
	}
	return 0
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
		return "📦 Earlier turns were summarized. Full history is still in this chat; the model will continue from the summary and the recent messages."
	default:
		return "📦 Older tool results were trimmed to fit the context window. Full history is still in this chat."
	}
}

// compactionModelHint is injected as a system message for the current
// turn so the model does not pretend it still has the dropped history.
func compactionModelHint(method string) string {
	switch method {
	case compactMethodHardTrim:
		return "The session working set was just hard-trimmed to fit the model context window. Older turns are gone from your messages. Do not claim you remember details that are not present. If the user needs the earlier thread, tell them to send /new."
	case compactMethodSummarize:
		return "The session was just compacted. Continue from the structured summary plus the recent verbatim messages. Read listed files if you need details. Do not invent dropped history."
	default:
		return "Older tool results were truncated to fit the context window. Re-read files or re-run tools if you need the full output."
	}
}

// CompactOptions tunes one CompactMessages run. Zero values use defaults.
type CompactOptions struct {
	// KeepRecentTokens overrides the hot-tail budget. 0 → KeepRecentTokens.
	KeepRecentTokens int
	// Instructions are extra /compact focus notes for the summarizer.
	Instructions string
	// Force compact even when the estimate is still under threshold
	// (overflow recovery after a provider 400).
	Force bool
}

// CompactMessages compresses the message history when it exceeds the
// token threshold. See CompactMessagesWith for options.
func CompactMessages(ctx context.Context, messages []provider.Message, workspace string, prov provider.Provider, model string, threshold int) (*CompactResult, error) {
	return CompactMessagesWith(ctx, messages, workspace, prov, model, threshold, CompactOptions{})
}

// CompactMessagesWith is the Pi-style pipeline:
//
//  1. Summarize everything older than a token-budget hot tail
//     (keepRecentTokens). The summarizer gets a structured handoff
//     prompt, capped tool results, and cumulative file lists.
//  2. If summarize is unavailable or fails, strip oversized tool
//     results outside the hot tail.
//  3. Hard-trim oldest turns until the estimate fits.
func CompactMessagesWith(ctx context.Context, messages []provider.Message, workspace string, prov provider.Provider, model string, threshold int, opts CompactOptions) (*CompactResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if threshold <= 0 {
		threshold = CompactThreshold(0, 0)
	}
	keep := opts.KeepRecentTokens
	if keep <= 0 {
		keep = KeepRecentTokens
	}
	tokens := EstimateTokens(messages)
	if !opts.Force && tokens < threshold {
		return &CompactResult{Messages: messages}, nil
	}

	slog.Info("context compaction triggered", "tokens", tokens, "threshold", threshold, "message_count", len(messages), "keep_recent", keep, "force", opts.Force)

	logFile, err := writeHistoryLog(messages, workspace)
	if err != nil {
		slog.Warn("failed to write history log", "error", err)
	}

	cutoff := keepRecentCutoff(messages, keep)
	if prov != nil && (cutoff > 0 || opts.Force) {
		compressed, err := compressOlderMessages(ctx, messages, prov, model, compactOpts{
			keepRecentTokens: keep,
			instructions:     opts.Instructions,
			force:            opts.Force,
		})
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

	pruned := pruneToolResultsBefore(messages, keepRecentCutoff(messages, keep))
	prunedTokens := EstimateTokens(pruned)
	slog.Info("after pruning", "tokens_before", tokens, "tokens_after", prunedTokens)
	if prunedTokens < threshold {
		if prunedTokens < tokens {
			return &CompactResult{
				Messages: pruned,
				Pruned:   true,
				Method:   compactMethodPrune,
				LogFile:  logFile,
			}, nil
		}
		return &CompactResult{Messages: messages, LogFile: logFile}, nil
	}

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

type compactOpts struct {
	keepRecentTokens int
	instructions     string
	force            bool
}

// keepRecentCutoff is the first index not in the verbatim hot tail.
// The tail is a token budget (KeepRecentTokens), not a message count.
// The cut never lands on a lone tool reply.
func keepRecentCutoff(messages []provider.Message, keepRecentTokens int) int {
	if keepRecentTokens <= 0 {
		keepRecentTokens = KeepRecentTokens
	}
	if len(messages) == 0 {
		return 0
	}
	total := EstimateTokens(messages)
	if total <= keepRecentTokens {
		return 0
	}
	acc := 0
	cutoff := len(messages)
	for i := len(messages) - 1; i >= 0; i-- {
		acc += EstimateTokens([]provider.Message{messages[i]})
		cutoff = i
		if acc >= keepRecentTokens {
			break
		}
	}
	return safeCompactionCutoff(messages, cutoff)
}

func serializeConversation(messages []provider.Message) string {
	var b strings.Builder
	for _, m := range messages {
		if m.Origin != provider.OriginUser {
			continue
		}
		b.WriteString(formatMessageForSummary(m))
		if len(m.ToolCalls) > 0 {
			b.WriteString("tool_calls: ")
			for i, tc := range m.ToolCalls {
				if i > 0 {
					b.WriteString(", ")
				}
				b.WriteString(tc.Function.Name)
				args := compactSummarizeArgs(tc.Function.Arguments)
				if args != "" {
					b.WriteByte('(')
					b.WriteString(args)
					b.WriteByte(')')
				}
			}
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func formatMessageForSummary(m provider.Message) string {
	content := m.TextContent()
	if m.Role == "tool" {
		label := m.Name
		if label == "" {
			label = "tool"
		}
		return fmt.Sprintf("[tool %s] %s\n", label, capRunes(content, summarizeToolMaxRunes))
	}
	return fmt.Sprintf("[%s] %s\n", m.Role, content)
}

func extractFileOps(messages []provider.Message) (readFiles, modifiedFiles []string) {
	seenRead := map[string]struct{}{}
	seenMod := map[string]struct{}{}
	for _, m := range messages {
		if m.Role == "assistant" {
			for _, tc := range m.ToolCalls {
				path := filePathFromArgs(tc.Function.Arguments)
				if path == "" {
					continue
				}
				switch tc.Function.Name {
				case "read_file", "read", "cat":
					if _, ok := seenRead[path]; !ok {
						seenRead[path] = struct{}{}
						readFiles = append(readFiles, path)
					}
				case "write_file", "edit_file", "write", "edit":
					if _, ok := seenMod[path]; !ok {
						seenMod[path] = struct{}{}
						modifiedFiles = append(modifiedFiles, path)
					}
				}
			}
		}
		if m.Role == "tool" && strings.HasPrefix(strings.TrimSpace(m.Content), "Wrote ") {
			path := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(m.Content), "Wrote "))
			if path != "" {
				if _, ok := seenMod[path]; !ok {
					seenMod[path] = struct{}{}
					modifiedFiles = append(modifiedFiles, path)
				}
			}
		}
	}
	sort.Strings(readFiles)
	sort.Strings(modifiedFiles)
	return readFiles, modifiedFiles
}

func filePathFromArgs(arguments string) string {
	if arguments == "" {
		return ""
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(arguments), &raw); err != nil {
		return ""
	}
	for _, key := range []string{"path", "file_path", "filename"} {
		if v, ok := raw[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func formatFileTag(tag string, files []string) string {
	if len(files) == 0 {
		return ""
	}
	return "<" + tag + ">\n" + strings.Join(files, "\n") + "\n</" + tag + ">"
}

func lastConversationSummary(messages []provider.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		c := messages[i].Content
		if strings.Contains(c, conversationSummaryMark) || strings.Contains(c, compactionSummaryMark) {
			return c
		}
	}
	return ""
}

func compactSummarizeArgs(arguments string) string {
	if arguments == "" {
		return ""
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(arguments), &raw); err != nil {
		return capRunes(arguments, 160)
	}
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		s := fmt.Sprintf("%v", raw[k])
		if len([]rune(s)) > 80 {
			s = capRunes(s, 80)
		}
		parts = append(parts, k+"="+s)
	}
	return strings.Join(parts, ", ")
}

func isContextOverflowError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	needles := []string{
		"context length",
		"context window",
		"maximum context",
		"max context",
		"too many tokens",
		"prompt is too long",
		"prompt too long",
		"token limit",
		"context_length_exceeded",
		"request too large",
		"payload too large",
	}
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
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

// compressOlderMessages asks the LLM for a structured handoff of
// everything older than the token-budget hot tail.
func compressOlderMessages(ctx context.Context, messages []provider.Message, prov provider.Provider, model string, opt compactOpts) ([]provider.Message, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	keep := opt.keepRecentTokens
	if keep <= 0 {
		keep = KeepRecentTokens
	}
	cutoff := keepRecentCutoff(messages, keep)
	if cutoff <= 0 || cutoff >= len(messages) {
		if !opt.force {
			return messages, nil
		}
		cutoff = safeCompactionCutoff(messages, len(messages)/2)
		if cutoff <= 0 || cutoff >= len(messages) {
			return messages, fmt.Errorf("nothing to summarize")
		}
	}
	if prov == nil {
		return nil, fmt.Errorf("summarize conversation: no provider")
	}

	olderMessages := messages[:cutoff]
	prev := lastConversationSummary(olderMessages)
	readFiles, modifiedFiles := extractFileOps(olderMessages)
	// serializeConversation skips OriginGoalContext (design §5.3 (b)):
	// old audit scaffolding is dropped; the live one stays in the tail.
	serial := serializeConversation(olderMessages)

	var b strings.Builder
	b.WriteString("Summarize this conversation into a structured handoff so another agent can continue without rereading the full history.\n")
	if strings.TrimSpace(opt.instructions) != "" {
		b.WriteString("\nAdditional focus from the user:\n")
		b.WriteString(strings.TrimSpace(opt.instructions))
		b.WriteByte('\n')
	}
	if prev != "" {
		b.WriteString("\nPrevious summary (update, do not discard still-relevant facts):\n")
		b.WriteString(prev)
		b.WriteByte('\n')
	}
	b.WriteString("\nConversation:\n")
	b.WriteString(capSummaryText(serial))
	b.WriteString("\nReturn markdown with these sections:\n")
	b.WriteString("## Goal\n")
	b.WriteString("## Constraints\n")
	b.WriteString("## Progress\n")
	b.WriteString("## Decisions\n")
	b.WriteString("## Next Steps\n")
	b.WriteString("## Critical Context\n")
	b.WriteString("Preserve concrete file paths, commands, errors, and unfinished work. ")
	b.WriteString("Omit greetings and repeated tool dumps.\n")

	summaryPrompt := []provider.Message{
		{
			Role:    "system",
			Content: "You compress agent transcripts into a durable structured handoff. Be concrete and complete.",
		},
		{
			Role:    "user",
			Content: b.String(),
		},
	}

	resp, err := prov.Chat(ctx, summaryPrompt, nil, model, compactSummaryMaxTokens, 0.3)
	if err != nil {
		return nil, fmt.Errorf("summarize conversation: %w", err)
	}
	if strings.TrimSpace(resp.Content) == "" {
		return nil, fmt.Errorf("summarize conversation: empty summary")
	}

	summary := strings.TrimSpace(resp.Content)
	if tag := formatFileTag("read-files", readFiles); tag != "" {
		summary += "\n\n" + tag
	}
	if tag := formatFileTag("modified-files", modifiedFiles); tag != "" {
		summary += "\n\n" + tag
	}

	compressed := make([]provider.Message, 0, len(messages)-cutoff+1)
	compressed = append(compressed, provider.Message{
		Role:    "user",
		Content: conversationSummaryMark + "\n" + summary + "\n\nContinue from this summary. Do not greet or restart.",
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
