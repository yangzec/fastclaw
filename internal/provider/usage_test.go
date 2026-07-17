package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestOpenAIParseSSEUsage feeds a canned SSE stream through the OpenAI
// parser and checks Usage was extracted from the terminal include_usage
// chunk. This is the path goal-budget accounting + admin metering rely
// on for every web-chat streaming turn.
func TestOpenAIParseSSEUsage(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"hello"},"finish_reason":null}]}`,
		`data: {"choices":[],"usage":{"prompt_tokens":120,"completion_tokens":15,"prompt_tokens_details":{"cached_tokens":80}}}`,
		`data: [DONE]`,
		``,
	}, "\n")

	p := &OpenAIProvider{}
	resp, err := p.parseSSE(strings.NewReader(sse))
	if err != nil {
		t.Fatalf("parseSSE: %v", err)
	}
	if resp.Content != "hello" {
		t.Errorf("content = %q, want %q", resp.Content, "hello")
	}
	// openaiUsageToProvider subtracts cached_tokens from prompt_tokens
	// to expose the uncached billable portion as InputTokens. So with
	// prompt_tokens=120 + cached=80 we expect InputTokens=40 and
	// CacheReadTokens=80.
	if resp.Usage.InputTokens != 40 {
		t.Errorf("InputTokens = %d, want 40 (120 prompt − 80 cached)", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 15 {
		t.Errorf("OutputTokens = %d, want 15", resp.Usage.OutputTokens)
	}
	if resp.Usage.CacheReadTokens != 80 {
		t.Errorf("CacheReadTokens = %d, want 80", resp.Usage.CacheReadTokens)
	}
}

// TestOpenAIParseSSENoUsage exercises the providers-don't-report-usage
// path (legacy endpoints, Ollama, etc.). Usage should land as the
// zero-value struct so goal-budget code can detect "can't measure"
// via an explicit zero check.
func TestOpenAIParseSSENoUsage(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
		``,
	}, "\n")
	p := &OpenAIProvider{}
	resp, err := p.parseSSE(strings.NewReader(sse))
	if err != nil {
		t.Fatalf("parseSSE: %v", err)
	}
	if resp.Usage.InputTokens != 0 || resp.Usage.OutputTokens != 0 ||
		resp.Usage.CacheReadTokens != 0 || resp.Usage.CacheCreationTokens != 0 {
		t.Errorf("Usage = %+v, want zero-value for streams that omit usage", resp.Usage)
	}
}

// TestAnthropicParseSSEUsage exercises the Anthropic SSE shape:
// usage rides on message_start (prompt + cache fields) and the final
// output_tokens count lands on message_delta.
func TestAnthropicParseSSEUsage(t *testing.T) {
	sse := strings.Join(anthropicSSELines("data: "), "\n")

	p := &AnthropicProvider{}
	resp, err := p.parseSSE(strings.NewReader(sse))
	if err != nil {
		t.Fatalf("parseSSE: %v", err)
	}
	assertAnthropicSSEParsed(t, resp, "hi")
}

func TestAnthropicParseSSEAcceptsDataLineWithoutSpace(t *testing.T) {
	sse := strings.Join(anthropicSSELines("data:"), "\n")

	p := &AnthropicProvider{}
	resp, err := p.parseSSE(strings.NewReader(sse))
	if err != nil {
		t.Fatalf("parseSSE: %v", err)
	}
	assertAnthropicSSEParsed(t, resp, "hi")
}

func TestAnthropicChatStreamAcceptsDataLineWithoutSpace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, line := range anthropicSSELines("data:") {
			fmt.Fprintln(w, line)
		}
	}))
	defer srv.Close()

	p := NewAnthropic("test-key", srv.URL)
	sr, err := p.ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, "claude-test", 1024, 0)
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	var content string
	var usage Usage
	var sawDone bool
	for chunk, ok := sr.Next(); ok; chunk, ok = sr.Next() {
		content += chunk.Content
		if chunk.Done {
			sawDone = true
			usage = chunk.Usage
		}
	}
	if err := sr.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if content != "hi" {
		t.Fatalf("stream content = %q, want %q", content, "hi")
	}
	if !sawDone {
		t.Fatalf("stream did not emit done chunk")
	}
	assertAnthropicUsage(t, usage)
}

func anthropicSSELines(prefix string) []string {
	return []string{
		prefix + `{"type":"message_start","message":{"usage":{"input_tokens":50,"output_tokens":1,"cache_read_input_tokens":30,"cache_creation_input_tokens":10}}}`,
		prefix + `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		prefix + `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
		prefix + `{"type":"message_delta","usage":{"input_tokens":50,"output_tokens":42}}`,
		prefix + `{"type":"message_stop"}`,
		``,
	}
}

func assertAnthropicSSEParsed(t *testing.T, resp *Response, wantContent string) {
	t.Helper()
	if resp.Content != wantContent {
		t.Errorf("content = %q, want %q", resp.Content, wantContent)
	}
	assertAnthropicUsage(t, resp.Usage)
}

func assertAnthropicUsage(t *testing.T, usage Usage) {
	t.Helper()
	if usage.InputTokens != 50 {
		t.Errorf("InputTokens = %d, want 50", usage.InputTokens)
	}
	if usage.OutputTokens != 42 {
		t.Errorf("OutputTokens = %d, want 42 (from message_delta)", usage.OutputTokens)
	}
	if usage.CacheReadTokens != 30 {
		t.Errorf("CacheReadTokens = %d, want 30 (from message_start)", usage.CacheReadTokens)
	}
	if usage.CacheCreationTokens != 10 {
		t.Errorf("CacheCreationTokens = %d, want 10 (from message_start)", usage.CacheCreationTokens)
	}
}
