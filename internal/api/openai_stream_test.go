package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/agent"
	"github.com/fastclaw-ai/fastclaw/internal/auth"
	"github.com/fastclaw-ai/fastclaw/internal/bus"
	"github.com/fastclaw-ai/fastclaw/internal/config"
	"github.com/fastclaw-ai/fastclaw/internal/provider"
	"github.com/fastclaw-ai/fastclaw/internal/session"
)

type streamTestResolver struct{ space *UserSpaceView }

func (r streamTestResolver) UserSpaceFor(string) (*UserSpaceView, error) { return r.space, nil }
func (r streamTestResolver) LocalAgentManager() *agent.Manager           { return r.space.Agents }
func (r streamTestResolver) IsCloudMode() bool                           { return false }

type memSessionStore struct{}

func (memSessionStore) GetSession(context.Context, string, string) ([]provider.Message, error) {
	return nil, nil
}
func (memSessionStore) SaveSession(context.Context, string, string, string, string, string, string, []provider.Message) error {
	return nil
}
func (memSessionStore) AppendMessage(context.Context, string, string, provider.Message) error {
	return nil
}
func (memSessionStore) ListMessages(context.Context, string, string) ([]provider.Message, error) {
	return nil, nil
}
func (memSessionStore) ListWebSessions(context.Context, string) ([]session.WebSession, error) {
	return nil, nil
}
func (memSessionStore) DeleteSession(context.Context, string, string) error { return nil }
func (memSessionStore) RenameSession(context.Context, string, string, string) error {
	return nil
}
func (memSessionStore) MoveSession(context.Context, string, string, string) error {
	return nil
}
func (memSessionStore) ResolveActiveSessionKey(context.Context, string, string, string, string) (string, error) {
	return "", nil
}
func (memSessionStore) LookupSessionTriple(context.Context, string, string) (string, string, string, error) {
	return "", "", "", nil
}
func (memSessionStore) LookupSessionProject(context.Context, string, string) (string, error) {
	return "", nil
}

type blockingChatProvider struct {
	ready   chan struct{}
	release chan struct{}
}

func (p blockingChatProvider) Chat(ctx context.Context, messages []provider.Message, tools []provider.Tool, model string, maxTokens int, temperature float64) (*provider.Response, error) {
	select {
	case <-p.ready:
	default:
		close(p.ready)
	}
	select {
	case <-p.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return &provider.Response{Content: "done-after-tools"}, nil
}

func (p blockingChatProvider) ChatStream(ctx context.Context, messages []provider.Message, tools []provider.Tool, model string, maxTokens int, temperature float64) (*provider.StreamReader, error) {
	ch := make(chan provider.StreamChunk, 2)
	ch <- provider.StreamChunk{Content: "done-after-tools"}
	ch <- provider.StreamChunk{Done: true}
	close(ch)
	return provider.NewStreamReader(ch), nil
}

func newStreamTestServer(t *testing.T, prov provider.Provider) *Server {
	t.Helper()
	t.Setenv("FASTCLAW_HOME", t.TempDir())
	home := t.TempDir()
	mgr, err := agent.NewManager([]config.ResolvedAgent{{
		ID: "agent-1", UserID: "user-1", DisplayName: "Test Agent",
		Home: home + "/agent", Workspace: home + "/workspace", Model: "test-model",
		MaxTokens: 128, MaxToolIterations: 1,
	}}, prov, bus.New(), agent.WithUserID("user-1"), agent.WithSessionStore(memSessionStore{}))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return NewServer(streamTestResolver{space: &UserSpaceView{UserID: "user-1", Agents: mgr}}, nil, nil)
}

type instantChatProvider struct{}

func (instantChatProvider) Chat(ctx context.Context, messages []provider.Message, tools []provider.Tool, model string, maxTokens int, temperature float64) (*provider.Response, error) {
	return &provider.Response{Content: "hello-from-agent"}, nil
}

func (instantChatProvider) ChatStream(ctx context.Context, messages []provider.Message, tools []provider.Tool, model string, maxTokens int, temperature float64) (*provider.StreamReader, error) {
	ch := make(chan provider.StreamChunk, 2)
	ch <- provider.StreamChunk{Content: "hello-from-agent"}
	ch <- provider.StreamChunk{Done: true}
	close(ch)
	return provider.NewStreamReader(ch), nil
}

func TestChatCompletionHTTPReturnsBothSessionIds(t *testing.T) {
	srv := newStreamTestServer(t, instantChatProvider{})
	const callerKey = "app:user:conv-accept"
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(
		`{"agent_id":"agent-1","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Fastclaw-Session-Key", callerKey)
	req = req.WithContext(auth.WithIdentity(req.Context(), auth.Identity{UserID: "user-1", AuthMethod: "session"}))

	rr := httptest.NewRecorder()
	srv.HandleChatCompletions(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("X-Fastclaw-Session-Key"); got != callerKey {
		t.Fatalf("X-Fastclaw-Session-Key = %q", got)
	}
	native := rr.Header().Get("X-Fastclaw-Session-Id")
	if !strings.HasPrefix(native, "s-") {
		t.Fatalf("X-Fastclaw-Session-Id = %q, want s-...", native)
	}
	if native == callerKey {
		t.Fatal("native session id echoed the caller key")
	}
	var body chatCompletionResponse
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.SessionKey != callerKey || body.SessionID != native {
		t.Fatalf("json session_key=%q session_id=%q headers key=%q id=%q",
			body.SessionKey, body.SessionID, callerKey, native)
	}
	if body.Choices[0].Message.Content != "hello-from-agent" {
		t.Fatalf("content = %q", body.Choices[0].Message.Content)
	}
}

func streamAuthedRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Fastclaw-Session-Key", "app:user:stream-keep-alive")
	ctx := auth.WithIdentity(req.Context(), auth.Identity{UserID: "user-1", AuthMethod: "session"})
	return req.WithContext(ctx)
}

func TestChatCompletionStreamKeepsAliveBeforeModelTokens(t *testing.T) {
	prev := apiStreamHeartbeatInterval
	apiStreamHeartbeatInterval = 40 * time.Millisecond
	t.Cleanup(func() { apiStreamHeartbeatInterval = prev })

	prov := blockingChatProvider{ready: make(chan struct{}), release: make(chan struct{})}
	srv := newStreamTestServer(t, prov)
	req := streamAuthedRequest(`{"agent_id":"agent-1","stream":true,"messages":[{"role":"user","content":"hello"}]}`)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.HandleChatCompletions(w, req)
	}))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("GET stream: %v", err)
	}
	defer resp.Body.Close()

	if !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("Content-Type = %q", resp.Header.Get("Content-Type"))
	}
	if !strings.HasPrefix(resp.Header.Get("X-Fastclaw-Session-Id"), "s-") {
		t.Fatalf("missing session header: %q", resp.Header.Get("X-Fastclaw-Session-Id"))
	}

	reader := bufio.NewReader(resp.Body)
	deadline := time.After(2 * time.Second)
	sawRole := false
	sawHeartbeat := false
	for !sawRole || !sawHeartbeat {
		lineCh := make(chan string, 1)
		errCh := make(chan error, 1)
		go func() {
			line, err := reader.ReadString('\n')
			if err != nil {
				errCh <- err
				return
			}
			lineCh <- line
		}()
		select {
		case err := <-errCh:
			t.Fatalf("read stream before model released: %v", err)
		case line := <-lineCh:
			if strings.Contains(line, `"role":"assistant"`) {
				sawRole = true
			}
			if strings.HasPrefix(strings.TrimRight(line, "\n"), ": heartbeat") {
				sawHeartbeat = true
			}
		case <-deadline:
			t.Fatalf("timed out waiting for role chunk and heartbeat; role=%v heartbeat=%v", sawRole, sawHeartbeat)
		}
	}

	select {
	case <-prov.ready:
	case <-time.After(time.Second):
		t.Fatal("provider Chat was not reached")
	}
	close(prov.release)

	rest, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if !strings.Contains(string(rest), "done-after-tools") {
		t.Fatalf("missing final content: %s", rest)
	}
	if !strings.Contains(string(rest), "data: [DONE]") {
		t.Fatalf("missing [DONE]: %s", rest)
	}
}
