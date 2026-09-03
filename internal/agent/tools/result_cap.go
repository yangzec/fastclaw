package tools

// maxToolResultRunes is the ingest cap for any tool return value
// before it is stored on the session. A single grep/help dump at
// 500KB will otherwise sit in the keep-recent window and 502 the
// next model call.
const maxToolResultRunes = 64 * 1024

func clipToolResult(s string) string {
	runes := []rune(s)
	if len(runes) <= maxToolResultRunes {
		return s
	}
	return string(runes[:maxToolResultRunes]) +
		"\n\n[truncated: tool output exceeded 64KiB and was clipped so it cannot blow the model context. Re-run with a narrower command (head/tail/rg) if you need more.]"
}
