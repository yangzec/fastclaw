package api

import (
	"bufio"
	"context"
	"encoding/json"
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

type chatCompletionsV1Resolver struct {
	space *UserSpaceView
}

func (r chatCompletionsV1Resolver) UserSpaceFor(userID string) (*UserSpaceView, error) {
	return r.space, nil
}

func (r chatCompletionsV1Resolver) LocalAgentManager() *agent.Manager { return r.space.Agents }
func (r chatCompletionsV1Resolver) IsCloudMode() bool                 { return false }

type chatCompletionsV1Provider struct{}

func (p chatCompletionsV1Provider) Chat(ctx context.Context, messages []provider.Message, tools []provider.Tool, model string, maxTokens int, temperature float64) (*provider.Response, error) {
	return &provider.Response{Content: "ok"}, nil
}

func (p chatCompletionsV1Provider) ChatStream(ctx context.Context, messages []provider.Message, tools []provider.Tool, model string, maxTokens int, temperature float64) (*provider.StreamReader, error) {
	ch := make(chan provider.StreamChunk, 2)
	ch <- provider.StreamChunk{Content: "ok"}
	ch <- provider.StreamChunk{Done: true}
	close(ch)
	return provider.NewStreamReader(ch), nil
}

type chatCompletionsV1SessionStore struct {
	sessions map[string][]provider.Message
	triples  map[string][3]string
}

func newChatCompletionsV1SessionStore() *chatCompletionsV1SessionStore {
	return &chatCompletionsV1SessionStore{
		sessions: map[string][]provider.Message{},
		triples:  map[string][3]string{},
	}
}

func (s *chatCompletionsV1SessionStore) GetSession(ctx context.Context, agentID, sessionKey string) ([]provider.Message, error) {
	msgs, ok := s.sessions[sessionKey]
	if !ok {
		return nil, nil
	}
	return append([]provider.Message(nil), msgs...), nil
}

func (s *chatCompletionsV1SessionStore) SaveSession(ctx context.Context, agentID, sessionKey, channel, accountID, chatID, projectID string, messages []provider.Message) error {
	s.sessions[sessionKey] = append([]provider.Message(nil), messages...)
	s.triples[sessionKey] = [3]string{channel, accountID, chatID}
	return nil
}

func (s *chatCompletionsV1SessionStore) AppendMessage(ctx context.Context, agentID, sessionKey string, msg provider.Message) error {
	return nil
}

func (s *chatCompletionsV1SessionStore) ListMessages(ctx context.Context, agentID, sessionKey string) ([]provider.Message, error) {
	return s.GetSession(ctx, agentID, sessionKey)
}

func (s *chatCompletionsV1SessionStore) ListWebSessions(ctx context.Context, agentID string) ([]session.WebSession, error) {
	return nil, nil
}
func (s *chatCompletionsV1SessionStore) DeleteSession(ctx context.Context, agentID, sessionKey string) error {
	return nil
}
func (s *chatCompletionsV1SessionStore) RenameSession(ctx context.Context, agentID, sessionKey, title string) error {
	return nil
}
func (s *chatCompletionsV1SessionStore) MoveSession(ctx context.Context, agentID, sessionKey, projectID string) error {
	return nil
}

func (s *chatCompletionsV1SessionStore) ResolveActiveSessionKey(ctx context.Context, agentID, channel, accountID, chatID string) (string, error) {
	for key, triple := range s.triples {
		if triple == [3]string{channel, accountID, chatID} {
			return key, nil
		}
	}
	return "", nil
}

func (s *chatCompletionsV1SessionStore) LookupSessionTriple(ctx context.Context, agentID, sessionKey string) (string, string, string, error) {
	triple := s.triples[sessionKey]
	return triple[0], triple[1], triple[2], nil
}

func (s *chatCompletionsV1SessionStore) LookupSessionProject(ctx context.Context, agentID, sessionKey string) (string, error) {
	return "", nil
}

type slowChatCompletionsV1Provider struct {
	ready   chan struct{}
	release chan struct{}
}

func (p slowChatCompletionsV1Provider) Chat(ctx context.Context, messages []provider.Message, tools []provider.Tool, model string, maxTokens int, temperature float64) (*provider.Response, error) {
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
	return &provider.Response{Content: "slow-ok"}, nil
}

func (p slowChatCompletionsV1Provider) ChatStream(ctx context.Context, messages []provider.Message, tools []provider.Tool, model string, maxTokens int, temperature float64) (*provider.StreamReader, error) {
	ch := make(chan provider.StreamChunk, 2)
	ch <- provider.StreamChunk{Content: "slow-ok"}
	ch <- provider.StreamChunk{Done: true}
	close(ch)
	return provider.NewStreamReader(ch), nil
}

func newChatCompletionsV1TestServerWithProvider(t *testing.T, prov provider.Provider) *Server {
	t.Helper()
	home := t.TempDir()
	store := newChatCompletionsV1SessionStore()
	mgr, err := agent.NewManager([]config.ResolvedAgent{{
		ID: "agent-1", UserID: "user-1", DisplayName: "Test Agent",
		Home: home + "/agent", Workspace: home + "/workspace", Model: "test-model",
		MaxTokens: 128, MaxToolIterations: 1,
	}}, prov, bus.New(), agent.WithUserID("user-1"), agent.WithSessionStore(store))
	if err != nil {
		t.Fatalf("new agent manager: %v", err)
	}
	return NewServer(chatCompletionsV1Resolver{space: &UserSpaceView{UserID: "user-1", Agents: mgr}}, nil, nil)
}

func newChatCompletionsV1TestServer(t *testing.T) *Server {
	t.Helper()
	home := t.TempDir()
	store := newChatCompletionsV1SessionStore()
	mgr, err := agent.NewManager([]config.ResolvedAgent{{
		ID: "agent-1", UserID: "user-1", DisplayName: "Test Agent",
		Home: home + "/agent", Workspace: home + "/workspace", Model: "test-model",
		MaxTokens: 128, MaxToolIterations: 1,
	}}, chatCompletionsV1Provider{}, bus.New(), agent.WithUserID("user-1"), agent.WithSessionStore(store))
	if err != nil {
		t.Fatalf("new agent manager: %v", err)
	}
	return NewServer(chatCompletionsV1Resolver{space: &UserSpaceView{UserID: "user-1", Agents: mgr}}, nil, nil)
}

func authedRequest(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := auth.WithIdentity(req.Context(), auth.Identity{UserID: "user-1", AuthMethod: "session"})
	return req.WithContext(ctx)
}

func TestNativeSessionKeyCreatesLookupRowBeforeResponseBody(t *testing.T) {
	home := t.TempDir()
	store := newChatCompletionsV1SessionStore()
	mgr, err := agent.NewManager([]config.ResolvedAgent{{
		ID: "agent-1", UserID: "user-1", DisplayName: "Test Agent",
		Home: home + "/agent", Workspace: home + "/workspace", Model: "test-model",
		MaxTokens: 128, MaxToolIterations: 1,
	}}, chatCompletionsV1Provider{}, bus.New(), agent.WithUserID("user-1"), agent.WithSessionStore(store))
	if err != nil {
		t.Fatalf("new agent manager: %v", err)
	}
	ag := mgr.AgentByID("agent-1")
	native := ag.NativeSessionKey(bus.InboundMessage{Channel: "api", ChatID: "sales-agent:user-1:chat-pre", UserID: "api-user"})

	if !strings.HasPrefix(native, "s-") {
		t.Fatalf("native session id = %q, want s-*", native)
	}
	if _, ok := store.sessions[native]; !ok {
		t.Fatalf("NativeSessionKey did not create a store row before the response body")
	}
	if got := store.triples[native]; got != [3]string{"api", "", "sales-agent:user-1:chat-pre"} {
		t.Fatalf("triple = %#v", got)
	}
}

func TestChatCompletionsV1ReturnsNativeSessionHeaders(t *testing.T) {
	srv := newChatCompletionsV1TestServer(t)
	body := `{"model":"test-model","agent_id":"agent-1","messages":[{"role":"user","content":"hello"}]}`
	req := authedRequest(http.MethodPost, "/v1/chat/completions-v1", body)
	req.Header.Set("X-Fastclaw-Session-Key", "sales-agent:user-1:chat-1")
	rr := httptest.NewRecorder()

	srv.HandleChatCompletionsV1(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	native := rr.Header().Get("X-Fastclaw-Session-Id")
	if !strings.HasPrefix(native, "s-") {
		t.Fatalf("X-Fastclaw-Session-Id = %q, want native s-* session id", native)
	}
	if native == "sales-agent:user-1:chat-1" {
		t.Fatalf("native session id should not echo external session key")
	}
	if got := rr.Header().Get("X-Fastclaw-Session-Key"); got != "sales-agent:user-1:chat-1" {
		t.Fatalf("X-Fastclaw-Session-Key = %q", got)
	}
	var resp chatCompletionResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Choices[0].Message.Content == "" {
		t.Fatalf("expected normal chat completion body")
	}
}

func TestAgentIDsToInjectAfterUserSwitch(t *testing.T) {
	ident := auth.Identity{AuthMethod: "apikey", APIKeyType: "agent", APIKeyAgents: []string{"agent-1", "agent-2", "agent-1", ""}}
	if got := agentIDsToInjectAfterUserSwitch(ident, "agent-2"); len(got) != 1 || got[0] != "agent-2" {
		t.Fatalf("explicit authorized ids = %#v", got)
	}
	if got := agentIDsToInjectAfterUserSwitch(ident, "agent-3"); len(got) != 0 {
		t.Fatalf("explicit unauthorized ids = %#v", got)
	}
	if got := agentIDsToInjectAfterUserSwitch(ident, ""); len(got) != 2 || got[0] != "agent-1" || got[1] != "agent-2" {
		t.Fatalf("implicit ids = %#v", got)
	}
}

func TestOriginalChatCompletionsDoesNotReturnNativeSessionHeaders(t *testing.T) {
	srv := newChatCompletionsV1TestServer(t)
	body := `{"model":"test-model","agent_id":"agent-1","messages":[{"role":"user","content":"hello"}]}`
	req := authedRequest(http.MethodPost, "/v1/chat/completions", body)
	req.Header.Set("X-Fastclaw-Session-Key", "sales-agent:user-1:chat-1")
	rr := httptest.NewRecorder()

	srv.HandleChatCompletions(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("X-Fastclaw-Session-Id"); got != "" {
		t.Fatalf("original endpoint unexpectedly set X-Fastclaw-Session-Id=%q", got)
	}
	if got := rr.Header().Get("X-Fastclaw-Session-Key"); got != "" {
		t.Fatalf("original endpoint unexpectedly set X-Fastclaw-Session-Key=%q", got)
	}
}

func TestChatCompletionsV1StreamSendsReadyBeforeModelWorkCompletes(t *testing.T) {
	prov := slowChatCompletionsV1Provider{ready: make(chan struct{}), release: make(chan struct{})}
	srv := newChatCompletionsV1TestServerWithProvider(t, prov)
	body := `{"model":"test-model","agent_id":"agent-1","messages":[{"role":"user","content":"hello"}],"stream":true}`
	req := authedRequest(http.MethodPost, "/v1/chat/completions-v1", body)
	req.Header.Set("X-Fastclaw-Session-Key", "sales-agent:user-1:chat-stream-ready")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.HandleChatCompletionsV1(w, req)
	}))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("get stream: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("Content-Type = %q", resp.Header.Get("Content-Type"))
	}
	if !strings.HasPrefix(resp.Header.Get("X-Fastclaw-Session-Id"), "s-") {
		t.Fatalf("missing native session header: %q", resp.Header.Get("X-Fastclaw-Session-Id"))
	}

	reader := bufio.NewReader(resp.Body)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read first event line: %v", err)
	}
	if line != "event: ready\n" {
		t.Fatalf("first stream line = %q, want ready event", line)
	}
	data, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read ready data: %v", err)
	}
	if !strings.Contains(data, `"sessionKey":"sales-agent:user-1:chat-stream-ready"`) || !strings.Contains(data, `"sessionId":"s-`) {
		t.Fatalf("ready data = %q", data)
	}

	select {
	case <-prov.ready:
		// model work has started, but the stream was already readable before release
	case <-time.After(time.Second):
		t.Fatalf("provider was not reached")
	}
	close(prov.release)
}

func TestResolveChatCompletionSessionByExternalKey(t *testing.T) {
	srv := newChatCompletionsV1TestServer(t)
	body := `{"model":"test-model","agent_id":"agent-1","messages":[{"role":"user","content":"hello"}]}`
	createReq := authedRequest(http.MethodPost, "/v1/chat/completions-v1", body)
	createReq.Header.Set("X-Fastclaw-Session-Key", "sales-agent:user-1:chat-resolve")
	createRR := httptest.NewRecorder()
	srv.HandleChatCompletionsV1(createRR, createReq)
	wantNative := createRR.Header().Get("X-Fastclaw-Session-Id")
	if wantNative == "" {
		t.Fatalf("create response missing native session id")
	}

	lookupReq := authedRequest(http.MethodGet, "/v1/chat/session-id?agent_id=agent-1&session_key=sales-agent:user-1:chat-resolve", "")
	lookupRR := httptest.NewRecorder()
	srv.HandleResolveChatSessionID(lookupRR, lookupReq)
	if lookupRR.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", lookupRR.Code, lookupRR.Body.String())
	}
	var out struct {
		SessionID  string `json:"sessionId"`
		SessionKey string `json:"sessionKey"`
	}
	if err := json.NewDecoder(lookupRR.Body).Decode(&out); err != nil {
		t.Fatalf("decode lookup: %v", err)
	}
	if out.SessionID != wantNative || out.SessionKey != "sales-agent:user-1:chat-resolve" {
		t.Fatalf("lookup = %#v, want sessionId %q", out, wantNative)
	}
}
