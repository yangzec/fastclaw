package tools

import "strings"

// maxToolResultRunes is the ingest cap for any tool return value
// before it is stored on the session. Count is runes, not bytes: a
// 500KB grep dump otherwise sits in the keep-recent window and 502s
// the next model call.
const maxToolResultRunes = 64 * 1024

// ErrorAnalyzeHint is appended once to failed tool results so the
// model switches strategy instead of retrying the same call.
const ErrorAnalyzeHint = "[Analyze the error above and try a different approach.]"

func clipToolResult(s string) string {
	runes := []rune(s)
	if len(runes) <= maxToolResultRunes {
		return s
	}
	return string(runes[:maxToolResultRunes]) +
		"\n\n[truncated: tool output exceeded 65536 characters and was clipped so it cannot blow the model context. Re-run with a narrower command (head/tail/rg) if you need more.]"
}

func appendErrorHint(s string) string {
	if strings.Contains(s, ErrorAnalyzeHint) {
		return s
	}
	if s == "" {
		return ErrorAnalyzeHint
	}
	return s + "\n" + ErrorAnalyzeHint
}
