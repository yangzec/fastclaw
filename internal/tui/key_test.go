package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestEnterSubmitsTurn(t *testing.T) {
	m := newTestModel()
	for _, r := range "hi" {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if got := m.input.Value(); got != "hi" {
		t.Fatalf("input value = %q", got)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.querying {
		t.Fatal("enter did not start a turn")
	}
	if cmd == nil {
		t.Fatal("no command returned from submit")
	}
}

func TestCtrlJInsertsNewlineWithoutSubmitting(t *testing.T) {
	m := newTestModel()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("first")})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	if m.querying {
		t.Fatal("Ctrl+J submitted instead of inserting a newline")
	}
	if cmd != nil {
		t.Fatal("Ctrl+J unexpectedly returned a submit command")
	}
	if got := m.input.RawValue(); got != "first\n" {
		t.Fatalf("input value = %q, want a trailing newline", got)
	}
}

func TestClipboardImageCanSubmitWithoutText(t *testing.T) {
	m := newTestModel()
	m.Update(clipboardImageMsg{dataURL: "data:image/png;base64,eA=="})
	if len(m.pendingImages) != 1 {
		t.Fatalf("pending images = %d", len(m.pendingImages))
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.querying || cmd == nil {
		t.Fatal("image-only enter did not start a turn")
	}
	if len(m.pendingImages) != 0 {
		t.Fatal("sent image remained attached to composer")
	}
	if len(m.blocks) != 1 || m.blocks[0].Content != "[image]" {
		t.Fatalf("image-only display block = %#v", m.blocks)
	}
}

func TestEnterDuringTurnQueuesInsteadOfSteering(t *testing.T) {
	m := newTestModel()
	m.querying = true
	for _, r := range "follow up" {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("queued follow-up should not start a command")
	}
	if !m.querying {
		t.Fatal("queueing must not end the in-flight turn")
	}
	if len(m.queued) != 1 || m.queued[0] != "follow up" {
		t.Fatalf("queued = %#v", m.queued)
	}
	if got := m.input.Value(); got != "" {
		t.Fatalf("composer should clear after queueing, got %q", got)
	}
}

func TestCtrlSDuringTurnSteers(t *testing.T) {
	m := newTestModel()
	m.querying = true
	for _, r := range "change course" {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("Ctrl+S should return a steer command")
	}
	if len(m.queued) != 0 {
		t.Fatalf("steer should not queue, queued = %#v", m.queued)
	}
	if got := m.input.Value(); got != "" {
		t.Fatalf("composer should clear after steer, got %q", got)
	}
}

func TestEnterDuringTurnWithImageDoesNotQueue(t *testing.T) {
	m := newTestModel()
	m.querying = true
	m.pendingImages = []string{"data:image/png;base64,eA=="}
	for _, r := range "caption" {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("image+text during a turn should not start a command")
	}
	if len(m.queued) != 0 {
		t.Fatalf("image follow-up must not queue, queued = %#v", m.queued)
	}
	if got := m.input.Value(); got != "caption" {
		t.Fatalf("composer should keep the text after the image-queue reject, got %q", got)
	}
	if !strings.Contains(m.errMsg, "image") {
		t.Fatalf("expected an image-queue error, got %q", m.errMsg)
	}
}

func TestEscapeClearsClipboardImages(t *testing.T) {
	m := newTestModel()
	m.pendingImages = []string{"data:image/png;base64,eA=="}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if len(m.pendingImages) != 0 {
		t.Fatal("escape did not clear attached image")
	}
}
