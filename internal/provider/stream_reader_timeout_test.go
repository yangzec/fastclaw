package provider

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStreamReaderNextContextReturnsDeadlineExceededWhenIdle(t *testing.T) {
	sr := NewStreamReader(make(chan StreamChunk))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, ok := sr.NextContext(ctx)
	if ok {
		t.Fatal("NextContext ok = true, want false after idle timeout")
	}
	if !errors.Is(sr.Err(), context.DeadlineExceeded) {
		t.Fatalf("Err() = %v, want context deadline exceeded", sr.Err())
	}
}

func TestStreamReaderNextContextReturnsChunkBeforeDeadline(t *testing.T) {
	ch := make(chan StreamChunk, 1)
	ch <- StreamChunk{Content: "hi"}
	sr := NewStreamReader(ch)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	chunk, ok := sr.NextContext(ctx)
	if !ok {
		t.Fatalf("NextContext ok = false, err=%v", sr.Err())
	}
	if chunk.Content != "hi" {
		t.Fatalf("chunk.Content = %q, want hi", chunk.Content)
	}
}
