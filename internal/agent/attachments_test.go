package agent

import (
	"context"
	"encoding/base64"
	"io"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	coderuntime "github.com/fastclaw-ai/fastclaw/internal/runtime"
	"github.com/fastclaw-ai/fastclaw/internal/sandbox"
	"github.com/fastclaw-ai/fastclaw/internal/workspace"
)

func TestSanitizeAttachmentName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"report.pdf", "report.pdf"},
		{"../etc/passwd", "passwd"},
		{"../../foo.txt", "foo.txt"},
		{"a/b/c.csv", "c.csv"},
		{`C:\windows\notes.md`, "notes.md"},
		{"..", ""},
		{".", ""},
		{".hiddenrc", "hiddenrc"},
		{"weird\x00name.txt", "weirdname.txt"},
		{"control\x07char.csv", "controlchar.csv"},
		{"  spaced.txt  ", "spaced.txt"},
		{"", ""},
	}
	for _, c := range cases {
		if got := sanitizeAttachmentName(c.in); got != c.want {
			t.Errorf("sanitize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSanitizeAttachmentNameTruncates(t *testing.T) {
	long := strings.Repeat("a", 200) + ".pdf"
	got := sanitizeAttachmentName(long)
	if len(got) > maxAttachmentNameLen {
		t.Fatalf("len = %d, want <= %d", len(got), maxAttachmentNameLen)
	}
	if !strings.HasSuffix(got, ".pdf") {
		t.Errorf("extension lost: %q", got)
	}
}

// Regression: byte-slicing a multi-byte UTF-8 stem (e.g. CJK filenames
// at 3 bytes/char) used to chop the last rune in half and leave invalid
// UTF-8 on disk. Truncation must back off to a rune boundary.
func TestSanitizeAttachmentNameUTF8Safe(t *testing.T) {
	stem := strings.Repeat("中", 40) // 120 bytes, well over the 96 cap
	got := sanitizeAttachmentName(stem + ".pdf")
	if !utf8.ValidString(got) {
		t.Errorf("invalid UTF-8 after truncate: %q", got)
	}
	if !strings.HasSuffix(got, ".pdf") {
		t.Errorf("ext lost: %q", got)
	}
	if len(got) > maxAttachmentNameLen {
		t.Errorf("over cap: len=%d", len(got))
	}
}

func TestBuildAttachmentName(t *testing.T) {
	used := map[string]struct{}{}

	// Empty name → historical shape
	if got := buildAttachmentName("", "abc12", 0, ".png", used); got != "image_abc12_0.png" {
		t.Errorf("empty-name fallback = %q", got)
	}

	// Provided name, with extension → preserved
	if got := buildAttachmentName("report.pdf", "abc12", 0, ".pdf", used); got != "report.pdf" {
		t.Errorf("named = %q, want report.pdf", got)
	}
	used["report.pdf"] = struct{}{}

	// Duplicate name → disambiguated with index
	if got := buildAttachmentName("report.pdf", "abc12", 1, ".pdf", used); got != "report-1.pdf" {
		t.Errorf("dup = %q, want report-1.pdf", got)
	}

	// Name without extension picks up the MIME-derived ext
	used2 := map[string]struct{}{}
	if got := buildAttachmentName("notes", "abc12", 0, ".md", used2); got != "notes.md" {
		t.Errorf("ext append = %q, want notes.md", got)
	}

	// Path-escape attempts fall through to sanitize → fallback
	used3 := map[string]struct{}{}
	got := buildAttachmentName("..", "abc12", 0, ".bin", used3)
	if got != "image_abc12_0.bin" {
		t.Errorf("dotdot fallback = %q", got)
	}
}

// Regression: a later attachment hits the disambiguation slot of an
// earlier one. Walk:
//   - i=0  Name="report-1-2.pdf"  → "report-1-2.pdf"
//   - i=1  Name="report-1.pdf"    → "report-1.pdf"
//   - i=2  Name="report-1.pdf"    → dup; candidate "report-1-2.pdf"
//     ALSO dup → token-fallback rescues it as "report-1-tok99-2.pdf"
func TestBuildAttachmentNameSecondaryCollision(t *testing.T) {
	used := map[string]struct{}{}

	n0 := buildAttachmentName("report-1-2.pdf", "tok99", 0, ".pdf", used)
	used[n0] = struct{}{}
	n1 := buildAttachmentName("report-1.pdf", "tok99", 1, ".pdf", used)
	used[n1] = struct{}{}
	n2 := buildAttachmentName("report-1.pdf", "tok99", 2, ".pdf", used)
	used[n2] = struct{}{}

	if n0 != "report-1-2.pdf" || n1 != "report-1.pdf" {
		t.Fatalf("baseline wrong: n0=%q n1=%q", n0, n1)
	}
	if n2 == n0 || n2 == n1 {
		t.Fatalf("collision: n2=%q hit n0=%q or n1=%q", n2, n0, n1)
	}
	if n2 != "report-1-tok99-2.pdf" {
		t.Errorf("n2 = %q, want report-1-tok99-2.pdf", n2)
	}
}

func TestExtFromMIMECoversCommonDocs(t *testing.T) {
	cases := map[string]string{
		"application/pdf":            ".pdf",
		"application/pdf; charset=x": ".pdf",
		"text/plain":                 ".txt",
		"text/markdown":              ".md",
		"text/csv":                   ".csv",
		"application/json":           ".json",
		"application/zip":            ".zip",
		"image/png":                  ".png",
		"image/jpg":                  ".jpg",
		"application/x-unknown-type": "",
	}
	for ct, want := range cases {
		if got := extFromMIME(ct); got != want {
			t.Errorf("extFromMIME(%q) = %q, want %q", ct, got, want)
		}
	}
}

func TestDecodeDataURLEnforcesSizeCap(t *testing.T) {
	// Build a base64-encoded payload that exceeds maxAttachmentBytes.
	huge := make([]byte, maxAttachmentBytes+1)
	url := "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(huge)
	if _, _, err := decodeDataURL(url); err == nil {
		t.Fatal("expected size-cap error, got nil")
	}
}

func TestDecodeDataURLPDF(t *testing.T) {
	payload := []byte("%PDF-1.4 hello")
	url := "data:application/pdf;base64," + base64.StdEncoding.EncodeToString(payload)
	data, ext, err := decodeDataURL(url)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(data) != string(payload) {
		t.Errorf("body mismatch")
	}
	if ext != ".pdf" {
		t.Errorf("ext = %q, want .pdf", ext)
	}
}

// Compile-time sanity: decodeAttachment honors data URLs without a ctx.
var _ = context.Background

type attachExec struct {
	writes map[string]string
}

func (e *attachExec) Exec(context.Context, string, time.Duration) (string, error) { return "", nil }
func (e *attachExec) ReadFile(context.Context, string) (string, error)            { return "", nil }
func (e *attachExec) WriteFile(_ context.Context, path, content string) (string, error) {
	if e.writes == nil {
		e.writes = map[string]string{}
	}
	e.writes[path] = content
	return "", nil
}
func (e *attachExec) ListDir(context.Context, string) (string, error) { return "", nil }
func (e *attachExec) Backend() string                                 { return "fake" }
func (e *attachExec) Close() error                                    { return nil }

type attachPool struct {
	ex                          *attachExec
	agentID, projectID, session string
}

func (p *attachPool) Get(_ context.Context, agentID, projectID, sessionID string) (sandbox.Executor, error) {
	p.agentID, p.projectID, p.session = agentID, projectID, sessionID
	return p.ex, nil
}
func (p *attachPool) Release(string, string, string) error { return nil }
func (p *attachPool) CloseAll()                            {}
func (p *attachPool) Backend() string                      { return "fake" }

func TestWriteSessionAttachmentsProjectChatStoreAndSandbox(t *testing.T) {
	ws := workspace.NewLocalFS(t.TempDir())
	ex := &attachExec{}
	pool := &attachPool{ex: ex}
	a := &Agent{
		name:           "bot",
		agentID:        "agt",
		workspaceStore: ws,
		sandboxPool:    pool,
	}
	url := "data:text/plain;base64," + base64.StdEncoding.EncodeToString([]byte("hello"))
	paths := a.WriteSessionAttachments(context.Background(), "chat-1", "proj-1", []Attachment{{URL: url, Name: "note.txt"}})
	if len(paths) != 1 || paths[0] != "note.txt" {
		t.Fatalf("paths = %#v", paths)
	}
	if pool.projectID != "proj-1" || pool.session != "chat-1" {
		t.Fatalf("sandbox Get = (%q,%q), want (proj-1, chat-1)", pool.projectID, pool.session)
	}
	if got := ex.writes["/workspace/chat-1/note.txt"]; got != "hello" {
		t.Fatalf("sandbox write = %#v", ex.writes)
	}
	rc, err := ws.Get(context.Background(), "agt", "proj-1", "chat-1", "note.txt")
	if err != nil {
		t.Fatalf("store get: %v", err)
	}
	defer rc.Close()
	body, _ := io.ReadAll(rc)
	if string(body) != "hello" {
		t.Fatalf("store body = %q", body)
	}
}

func TestWriteSessionAttachmentsCodingUsesProjectRoot(t *testing.T) {
	ws := workspace.NewLocalFS(t.TempDir())
	ex := &attachExec{}
	pool := &attachPool{ex: ex}
	a := &Agent{
		name:           "bot",
		agentID:        "agt",
		workspaceStore: ws,
		sandboxPool:    pool,
		projectRuntime: &coderuntime.Manager{},
	}
	url := "data:text/plain;base64," + base64.StdEncoding.EncodeToString([]byte("hi"))
	a.WriteSessionAttachments(context.Background(), "chat-1", "proj-1", []Attachment{{URL: url, Name: "note.txt"}})
	if pool.session != "" {
		t.Fatalf("coding sandbox session = %q, want empty (shared project root)", pool.session)
	}
	if got := ex.writes["/workspace/note.txt"]; got != "hi" {
		t.Fatalf("sandbox write = %#v", ex.writes)
	}
	if _, err := ws.Get(context.Background(), "agt", "proj-1", "", "note.txt"); err != nil {
		t.Fatalf("project-root store get: %v", err)
	}
	if _, err := ws.Get(context.Background(), "agt", "proj-1", "chat-1", "note.txt"); err == nil {
		t.Fatal("coding attachment must not land in the per-chat prefix")
	}
}
