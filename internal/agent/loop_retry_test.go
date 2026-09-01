package agent

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/provider"
)

func TestLLMAPIErrorStatus(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{nil, 0},
		{errors.New("send request: connection reset"), 0},
		{fmt.Errorf("API error 400: invalid_request_error"), 400},
		{fmt.Errorf("summarize conversation: API error 400: too long"), 400},
		{errors.New("API error 429: rate limited"), 429},
		{errors.New("API error 502: bad gateway"), 502},
		{errors.New("API error 40: truncated"), 0},
	}
	for _, tc := range cases {
		if got := llmAPIErrorStatus(tc.err); got != tc.want {
			t.Errorf("llmAPIErrorStatus(%v) = %d, want %d", tc.err, got, tc.want)
		}
	}
}

func TestIsTransientLLMError(t *testing.T) {
	if isTransientLLMError(errors.New("API error 400: invalid_request_error")) {
		t.Fatal("400 must not be retried")
	}
	if isTransientLLMError(errors.New("API error 401: unauthorized")) {
		t.Fatal("401 must not be retried")
	}
	if !isTransientLLMError(errors.New("API error 429: rate limited")) {
		t.Fatal("429 should be retried")
	}
	if !isTransientLLMError(errors.New("API error 502: bad gateway")) {
		t.Fatal("502 should be retried")
	}
	if !isTransientLLMError(errors.New("send request: EOF")) {
		t.Fatal("network errors should be retried")
	}
}

func TestLLMRetryDoesNotRetryInvalidRequest(t *testing.T) {
	calls := 0
	_, err := llmRetry(context.Background(), "test", func(context.Context) (*provider.Response, error) {
		calls++
		return nil, fmt.Errorf("API error 400: invalid_request_error")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (400 is not retryable)", calls)
	}
}

func TestLLMRetryRetriesTransientThenSucceeds(t *testing.T) {
	calls := 0
	start := time.Now()
	resp, err := llmRetry(context.Background(), "test", func(context.Context) (*provider.Response, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("send request: EOF")
		}
		return &provider.Response{Content: "ok"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "ok" {
		t.Fatalf("content = %q", resp.Content)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if time.Since(start) < time.Second {
		t.Fatal("expected the 1s backoff before retry")
	}
}
