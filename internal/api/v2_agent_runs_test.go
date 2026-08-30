package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/agent"
	"github.com/fastclaw-ai/fastclaw/internal/auth"
	"github.com/fastclaw-ai/fastclaw/internal/bus"
	"github.com/fastclaw-ai/fastclaw/internal/config"
	"github.com/fastclaw-ai/fastclaw/internal/provider"
	"github.com/fastclaw-ai/fastclaw/internal/toolproviders"
	imagegenprovider "github.com/fastclaw-ai/fastclaw/internal/toolproviders/imagegen"
	"github.com/fastclaw-ai/fastclaw/internal/workspace"
)

func newV2TestServer(t *testing.T, ws workspace.Store) *Server {
	return newV2TestServerWithProvider(t, ws, chatCompletionsV1Provider{}, 1)
}

func newV2TestServerWithProvider(t *testing.T, ws workspace.Store, prov provider.Provider, maxToolIterations int) *Server {
	t.Helper()
	t.Setenv("FASTCLAW_HOME", t.TempDir())
	manager, err := agent.NewManager(
		[]config.ResolvedAgent{{
			ID:                "agt_test",
			UserID:            "user-1",
			Home:              filepath.Join(t.TempDir(), "agent"),
			Workspace:         filepath.Join(t.TempDir(), "workspace"),
			Model:             "fake/model",
			MaxTokens:         100,
			MaxToolIterations: maxToolIterations,
		}},
		prov,
		bus.New(),
		agent.WithUserID("user-1"),
		agent.WithSessionStore(newChatCompletionsV1SessionStore()),
		agent.WithWorkspaceStore(ws),
	)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	srv := &Server{resolver: chatCompletionsV1Resolver{space: &UserSpaceView{
		UserID: "user-1",
		Agents: manager,
		Config: &config.Config{},
	}}}
	srv.SetWorkspaceStore(ws)
	return srv
}

type v2InjectingResolver struct {
	space *UserSpaceView
	root  string
	calls []string
}

func (r *v2InjectingResolver) UserSpaceFor(string) (*UserSpaceView, error) {
	return r.space, nil
}

func (r *v2InjectingResolver) LocalAgentManager() *agent.Manager {
	return nil
}

func (r *v2InjectingResolver) IsCloudMode() bool {
	return false
}

func (r *v2InjectingResolver) EnsureAgent(_ context.Context, _ string, agentID string) error {
	r.calls = append(r.calls, agentID)
	return r.space.Agents.AddAgent(config.ResolvedAgent{
		ID:                agentID,
		UserID:            "owner-2",
		Home:              filepath.Join(r.root, "agent"),
		Workspace:         filepath.Join(r.root, "workspace"),
		Model:             "fake/model",
		MaxTokens:         100,
		MaxToolIterations: 1,
	}, chatCompletionsV1Provider{}, bus.New())
}

func TestV2AgentForRequestInjectsMissingExplicitAgent(t *testing.T) {
	srv := newV2TestServer(t, workspace.NewLocalFS(t.TempDir()))
	space := srv.resolver.(chatCompletionsV1Resolver).space
	resolver := &v2InjectingResolver{
		space: space,
		root:  t.TempDir(),
	}
	srv.resolver = resolver

	req := httptest.NewRequest(http.MethodPost, "/v2/agents/agt_late/runs", nil)
	req = req.WithContext(auth.WithIdentity(req.Context(), auth.Identity{
		UserID:     "user-1",
		AuthMethod: "apikey",
		APIKeyType: "admin",
	}))

	got, err := srv.v2AgentForRequest(req, "agt_late")
	if err != nil {
		t.Fatalf("v2AgentForRequest: %v", err)
	}
	if got.Name() != "agt_late" {
		t.Fatalf("agent = %q, want agt_late", got.Name())
	}
	if len(resolver.calls) != 1 || resolver.calls[0] != "agt_late" {
		t.Fatalf("EnsureAgent calls = %#v, want [agt_late]", resolver.calls)
	}
}

type toolThenStreamProvider struct {
	chatCalls      int
	continueStream <-chan struct{}
}

type delayedFinalStreamProvider struct {
	delay time.Duration
}

type imageToolThenStreamProvider struct {
	chatCalls int
}

func (p *imageToolThenStreamProvider) Chat(context.Context, []provider.Message, []provider.Tool, string, int, float64) (*provider.Response, error) {
	p.chatCalls++
	if p.chatCalls == 1 {
		return &provider.Response{ToolCalls: []provider.ToolCall{{
			ID:   "call_image",
			Type: "function",
			Function: provider.FunctionCall{
				Name:      "image_gen",
				Arguments: `{"prompt":"a launch poster"}`,
			},
		}}}, nil
	}
	return &provider.Response{Content: "The poster is ready."}, nil
}

func (p *imageToolThenStreamProvider) ChatStream(ctx context.Context, _ []provider.Message, _ []provider.Tool, _ string, _ int, _ float64) (*provider.StreamReader, error) {
	return textStreamProvider{content: "The poster is ready."}.ChatStream(ctx, nil, nil, "", 0, 0)
}

type v2StaticImageProvider struct{}

func (v2StaticImageProvider) Category() string { return imagegenprovider.Category }
func (v2StaticImageProvider) Name() string     { return "static" }
func (v2StaticImageProvider) Execute(context.Context, toolproviders.Request) (toolproviders.Response, error) {
	png := []byte("\x89PNG\r\n\x1a\n")
	return toolproviders.Response{Raw: imagegenprovider.Output{
		Base64: []string{base64.StdEncoding.EncodeToString(png)},
	}}, nil
}

type textStreamProvider struct {
	content string
}

func (p textStreamProvider) Chat(context.Context, []provider.Message, []provider.Tool, string, int, float64) (*provider.Response, error) {
	return &provider.Response{Content: p.content}, nil
}

func (p textStreamProvider) ChatStream(ctx context.Context, _ []provider.Message, _ []provider.Tool, _ string, _ int, _ float64) (*provider.StreamReader, error) {
	ch := make(chan provider.StreamChunk, 2)
	go func() {
		defer close(ch)
		select {
		case ch <- provider.StreamChunk{Content: p.content}:
		case <-ctx.Done():
			return
		}
		ch <- provider.StreamChunk{Done: true}
	}()
	return provider.NewStreamReader(ch), nil
}

func (p delayedFinalStreamProvider) Chat(context.Context, []provider.Message, []provider.Tool, string, int, float64) (*provider.Response, error) {
	return &provider.Response{Content: "done"}, nil
}

func (p delayedFinalStreamProvider) ChatStream(ctx context.Context, _ []provider.Message, _ []provider.Tool, _ string, _ int, _ float64) (*provider.StreamReader, error) {
	ch := make(chan provider.StreamChunk, 2)
	go func() {
		defer close(ch)
		select {
		case <-time.After(p.delay):
		case <-ctx.Done():
			return
		}
		ch <- provider.StreamChunk{Content: "done"}
		ch <- provider.StreamChunk{Done: true}
	}()
	return provider.NewStreamReader(ch), nil
}

func (p *toolThenStreamProvider) Chat(context.Context, []provider.Message, []provider.Tool, string, int, float64) (*provider.Response, error) {
	p.chatCalls++
	if p.chatCalls == 1 {
		return &provider.Response{
			ToolCalls: []provider.ToolCall{{
				ID:   "call_write",
				Type: "function",
				Function: provider.FunctionCall{
					Name:      "write_file",
					Arguments: `{"path":"nvda_research_visual.html","content":"<!doctype html><h1>NVDA</h1>"}`,
				},
			}},
		}, nil
	}
	return &provider.Response{Content: "The report is ready."}, nil
}

func (p *toolThenStreamProvider) ChatStream(ctx context.Context, _ []provider.Message, _ []provider.Tool, _ string, _ int, _ float64) (*provider.StreamReader, error) {
	ch := make(chan provider.StreamChunk, 3)
	go func() {
		defer close(ch)
		select {
		case ch <- provider.StreamChunk{Content: "The report "}:
		case <-ctx.Done():
			return
		}
		if p.continueStream != nil {
			select {
			case <-p.continueStream:
			case <-ctx.Done():
				return
			}
		}
		select {
		case ch <- provider.StreamChunk{Content: "is ready."}:
		case <-ctx.Done():
			return
		}
		select {
		case ch <- provider.StreamChunk{Done: true}:
		case <-ctx.Done():
		}
	}()
	return provider.NewStreamReader(ch), nil
}

func v2Request(t *testing.T, method, target string, body io.Reader) *http.Request {
	return v2RequestForUser(t, method, target, body, "user-1")
}

func v2RequestForUser(t *testing.T, method, target string, body io.Reader, userID string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, target, body)
	ctx := auth.WithIdentity(req.Context(), auth.Identity{
		UserID:       userID,
		AuthMethod:   "apikey",
		APIKeyType:   "admin",
		APIKeyAgents: []string{"agt_test"},
	})
	return req.WithContext(ctx)
}

func setV2PathValues(req *http.Request, agentID, sessionID, fileID, runID string) {
	req.SetPathValue("agentId", agentID)
	req.SetPathValue("sessionId", sessionID)
	req.SetPathValue("fileId", fileID)
	req.SetPathValue("runId", runID)
}

func registerV2TestImageGen(t *testing.T, srv *Server) {
	t.Helper()
	req := v2Request(t, http.MethodGet, "/", nil)
	ag, err := srv.v2AgentForRequest(req, "agt_test")
	if err != nil {
		t.Fatalf("resolve agent: %v", err)
	}
	providers := toolproviders.NewRegistry()
	providers.Register(v2StaticImageProvider{})
	ag.RegisterImageGenChain(&toolproviders.Chain{
		Category:     imagegenprovider.Category,
		Order:        []string{"static/test"},
		AutoFallback: false,
		Registry:     providers,
		GetConfig: func(string) toolproviders.ProviderConfig {
			return toolproviders.ProviderConfig{APIKey: "test-key"}
		},
	})
}

func TestV2SessionFilesExposeOpaqueServerURLsAndServeSupportedArtifacts(t *testing.T) {
	ws := workspace.NewLocalFS(t.TempDir())
	srv := newV2TestServer(t, ws)
	sessionID := "session-one"
	workspaceSessionID := v2WorkspaceSessionID("user-1", sessionID)
	files := []struct {
		path        string
		contentType string
		body        []byte
	}{
		{path: "reports/page.html", contentType: "text/html; charset=utf-8", body: []byte("<!doctype html><h1>NVDA</h1>")},
		{path: "images/chart.png", contentType: "image/png", body: []byte("\x89PNG\r\n\x1a\nfake")},
		{path: "images/vector.svg", contentType: "image/svg+xml", body: []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)},
		{path: "notes/research.md", contentType: "text/markdown; charset=utf-8", body: []byte("# Research\n")},
	}
	for _, file := range files {
		if err := ws.Put(context.Background(), "agt_test", "", workspaceSessionID, file.path, bytes.NewReader(file.body), int64(len(file.body)), file.contentType); err != nil {
			t.Fatalf("Put(%s): %v", file.path, err)
		}
	}

	listReq := v2Request(t, http.MethodGet, "/v2/agents/agt_test/sessions/session-one/files", nil)
	setV2PathValues(listReq, "agt_test", sessionID, "", "")
	listRec := httptest.NewRecorder()
	srv.HandleV2ListSessionFiles(listRec, listReq)

	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	var listed struct {
		Files []v2Artifact `json:"files"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Files) != len(files) {
		t.Fatalf("files=%d want=%d body=%s", len(listed.Files), len(files), listRec.Body.String())
	}

	expectedByName := make(map[string][]byte, len(files))
	for _, file := range files {
		expectedByName[filepath.Base(file.path)] = file.body
	}
	for _, artifact := range listed.Files {
		if artifact.ID == "" || strings.Contains(artifact.ID, "/") || strings.Contains(artifact.ID, artifact.Name) {
			t.Fatalf("file id is not opaque: %#v", artifact)
		}
		if strings.Contains(artifact.PreviewURL, "reports/") || strings.Contains(artifact.DownloadURL, "images/") {
			t.Fatalf("server URL leaked storage path: %#v", artifact)
		}
		if !strings.HasSuffix(artifact.DownloadURL, "?download=1") {
			t.Fatalf("downloadUrl=%q", artifact.DownloadURL)
		}

		getReq := v2Request(t, http.MethodGet, artifact.PreviewURL, nil)
		setV2PathValues(getReq, "agt_test", sessionID, artifact.ID, "")
		getRec := httptest.NewRecorder()
		srv.HandleV2GetSessionFile(getRec, getReq)
		if getRec.Code != http.StatusOK {
			t.Fatalf("preview %s status=%d body=%s", artifact.Name, getRec.Code, getRec.Body.String())
		}
		if !bytes.Equal(getRec.Body.Bytes(), expectedByName[artifact.Name]) {
			t.Fatalf("preview %s body mismatch", artifact.Name)
		}
		if artifact.Name == "page.html" && !strings.Contains(getRec.Header().Get("Content-Security-Policy"), "sandbox") {
			t.Fatalf("html preview lacks sandbox CSP: %q", getRec.Header().Get("Content-Security-Policy"))
		}
		if artifact.Name == "vector.svg" {
			csp := getRec.Header().Get("Content-Security-Policy")
			if !strings.Contains(csp, "sandbox") || strings.Contains(csp, "allow-scripts") {
				t.Fatalf("svg preview CSP is unsafe: %q", csp)
			}
		}

		downloadReq := v2Request(t, http.MethodGet, artifact.DownloadURL, nil)
		setV2PathValues(downloadReq, "agt_test", sessionID, artifact.ID, "")
		downloadRec := httptest.NewRecorder()
		srv.HandleV2GetSessionFile(downloadRec, downloadReq)
		if got := downloadRec.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment;") {
			t.Fatalf("download disposition=%q", got)
		}
	}
}

func TestV2SessionFileRejectsUnknownOpaqueID(t *testing.T) {
	ws := workspace.NewLocalFS(t.TempDir())
	srv := newV2TestServer(t, ws)
	req := v2Request(t, http.MethodGet, "/v2/agents/agt_test/sessions/session-one/files/forged", nil)
	setV2PathValues(req, "agt_test", "session-one", "forged", "")
	rec := httptest.NewRecorder()

	srv.HandleV2GetSessionFile(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d want=404 body=%s", rec.Code, rec.Body.String())
	}
}

func TestV2SessionFilesAreIsolatedPerEndUser(t *testing.T) {
	ws := workspace.NewLocalFS(t.TempDir())
	srv := newV2TestServerWithProvider(t, ws, &toolThenStreamProvider{}, 2)

	runReq := v2RequestForUser(
		t,
		http.MethodPost,
		"/v2/agents/agt_test/runs",
		strings.NewReader(`{"input":"Create an HTML report","sessionId":"shared-session"}`),
		"user-1",
	)
	setV2PathValues(runReq, "agt_test", "", "", "")
	runRec := httptest.NewRecorder()
	srv.HandleV2CreateRun(runRec, runReq)
	if runRec.Code != http.StatusOK {
		t.Fatalf("user-1 run status=%d body=%s", runRec.Code, runRec.Body.String())
	}

	listReq := v2RequestForUser(
		t,
		http.MethodGet,
		"/v2/agents/agt_test/sessions/shared-session/files",
		nil,
		"user-2",
	)
	setV2PathValues(listReq, "agt_test", "shared-session", "", "")
	listRec := httptest.NewRecorder()
	srv.HandleV2ListSessionFiles(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("user-2 list status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	var listed struct {
		Files []v2Artifact `json:"files"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode user-2 files: %v", err)
	}
	if len(listed.Files) != 0 {
		t.Fatalf("user-2 can see user-1 artifacts: %#v", listed.Files)
	}
}

func TestV2CreateRunStreamsDeltasAndPersistsRunState(t *testing.T) {
	ws := workspace.NewLocalFS(t.TempDir())
	srv := newV2TestServer(t, ws)
	req := v2Request(t, http.MethodPost, "/v2/agents/agt_test/runs", strings.NewReader(`{
		"input":"hello",
		"sessionId":"session-one",
		"params":{"market":"US"}
	}`))
	req.Header.Set("Content-Type", "application/json")
	setV2PathValues(req, "agt_test", "", "", "")
	rec := httptest.NewRecorder()

	srv.HandleV2CreateRun(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, event := range []string{"event: run.created", "event: message.delta", "event: run.completed"} {
		if !strings.Contains(body, event) {
			t.Fatalf("missing %q in SSE:\n%s", event, body)
		}
	}
	if !strings.Contains(body, `"delta":"ok"`) {
		t.Fatalf("missing streamed delta in SSE:\n%s", body)
	}

	runID := sseDataString(t, body, "run.created", "runId")
	getReq := v2Request(t, http.MethodGet, "/v2/agents/agt_test/runs/"+url.PathEscape(runID), nil)
	setV2PathValues(getReq, "agt_test", "", "", runID)
	getRec := httptest.NewRecorder()
	srv.HandleV2GetRun(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get run status=%d body=%s", getRec.Code, getRec.Body.String())
	}
	var run v2Run
	if err := json.Unmarshal(getRec.Body.Bytes(), &run); err != nil {
		t.Fatalf("decode run: %v", err)
	}
	if run.Status != v2RunCompleted || run.Output != "ok" || run.SessionID != "session-one" {
		t.Fatalf("run=%#v", run)
	}
	if run.NativeSessionID == "" {
		t.Fatalf("nativeSessionId missing: %#v", run)
	}
}

func TestV2InputAttachmentsAreNotReportedAsCreatedArtifacts(t *testing.T) {
	srv := newV2TestServer(t, workspace.NewLocalFS(t.TempDir()))
	req := v2Request(t, http.MethodPost, "/v2/agents/agt_test/runs", strings.NewReader(`{
		"input":"Summarize the attachment",
		"sessionId":"session-attachment",
		"attachments":[{
			"name":"input.md",
			"url":"data:text/markdown;base64,IyBJbnB1dA=="
		}]
	}`))
	setV2PathValues(req, "agt_test", "", "", "")
	rec := httptest.NewRecorder()

	srv.HandleV2CreateRun(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "event: artifact.created") {
		t.Fatalf("input attachment was reported as generated output:\n%s", rec.Body.String())
	}
}

func TestV2CreateRunCompletesRunWhenRequestContextIsCanceled(t *testing.T) {
	srv := newV2TestServerWithProvider(
		t,
		workspace.NewLocalFS(t.TempDir()),
		delayedFinalStreamProvider{delay: 20 * time.Millisecond},
		1,
	)
	baseReq := v2Request(t, http.MethodPost, "/v2/agents/agt_test/runs", strings.NewReader(`{
		"input":"hello",
		"sessionId":"session-detached"
	}`))
	ctx, cancel := context.WithCancel(baseReq.Context())
	cancel()
	req := baseReq.WithContext(ctx)
	setV2PathValues(req, "agt_test", "", "", "")
	rec := httptest.NewRecorder()

	srv.HandleV2CreateRun(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	runs := srv.v2Runs
	if len(runs) != 1 {
		t.Fatalf("runs=%d want 1", len(runs))
	}
	for _, run := range runs {
		if run.Status != v2RunCompleted || run.Output != "done" {
			t.Fatalf("run after canceled request=%#v, want completed with output", run)
		}
	}
}

func TestV2CreateRunSendsHeartbeatWhileWaitingForFinalChunk(t *testing.T) {
	oldHeartbeat := streamHeartbeatInterval
	streamHeartbeatInterval = 10 * time.Millisecond
	t.Cleanup(func() { streamHeartbeatInterval = oldHeartbeat })

	srv := newV2TestServerWithProvider(
		t,
		workspace.NewLocalFS(t.TempDir()),
		delayedFinalStreamProvider{delay: 80 * time.Millisecond},
		1,
	)
	req := v2Request(t, http.MethodPost, "/v2/agents/agt_test/runs", strings.NewReader(`{
		"input":"hello",
		"sessionId":"session-heartbeat"
	}`))
	setV2PathValues(req, "agt_test", "", "", "")
	rec := httptest.NewRecorder()

	srv.HandleV2CreateRun(rec, req)

	body := rec.Body.String()
	firstDelta := strings.Index(body, "event: message.delta")
	heartbeat := strings.Index(body, ": ping")
	if heartbeat < 0 || firstDelta < 0 || heartbeat > firstDelta {
		t.Fatalf("heartbeat was not sent while waiting for final chunk:\n%s", body)
	}
}

func TestV2CreateRunStreamsToolLifecycleAndCreatedArtifact(t *testing.T) {
	ws := workspace.NewLocalFS(t.TempDir())
	srv := newV2TestServerWithProvider(t, ws, &toolThenStreamProvider{}, 2)
	req := v2Request(t, http.MethodPost, "/v2/agents/agt_test/runs", strings.NewReader(`{
		"input":"Create an HTML report",
		"sessionId":"session-tools"
	}`))
	setV2PathValues(req, "agt_test", "", "", "")
	rec := httptest.NewRecorder()

	srv.HandleV2CreateRun(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, event := range []string{"event: tool.started", "event: tool.completed", "event: artifact.created"} {
		if !strings.Contains(body, event) {
			t.Fatalf("missing %q in SSE:\n%s", event, body)
		}
	}
	if !strings.Contains(body, `"name":"write_file"`) {
		t.Fatalf("tool name missing in SSE:\n%s", body)
	}
	if !strings.Contains(body, `"name":"nvda_research_visual.html"`) {
		t.Fatalf("artifact missing in SSE:\n%s", body)
	}
}

func TestV2GeneratedImageIntentClassification(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "english poster", input: "Create a polished poster image for our launch", want: true},
		{name: "chinese visual", input: "画一张视觉图介绍这个 Agent 的能力", want: true},
		{name: "english draw request", input: "Draw a cat in a garden", want: true},
		{name: "chinese draw request", input: "画一只在花园里的猫", want: true},
		{name: "visual report poster", input: "Create a visual report poster for investors", want: true},
		{name: "html visualization", input: "Create an HTML data visualization dashboard", want: false},
		{name: "flowchart", input: "Draw a flowchart for the approval process", want: false},
		{name: "image analysis", input: "Analyze this image and write a report", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := requiresV2ImageGen(tt.input); got != tt.want {
				t.Fatalf("requiresV2ImageGen(%q)=%v want=%v", tt.input, got, tt.want)
			}
		})
	}
}

func TestV2GeneratedImagePostconditionRejectsMissingOrNonImageArtifact(t *testing.T) {
	image := v2Artifact{Name: "poster.png", ContentType: "image/png"}
	html := v2Artifact{Name: "poster.html", ContentType: "text/html"}
	tests := []struct {
		name       string
		toolOK     bool
		artifacts  []v2Artifact
		wantErrSub string
	}{
		{name: "tool not successful", artifacts: []v2Artifact{image}, wantErrSub: "image_gen did not complete successfully"},
		{name: "no artifact", toolOK: true, wantErrSub: "image_gen produced no image artifact"},
		{name: "wrong artifact", toolOK: true, artifacts: []v2Artifact{html}, wantErrSub: "image_gen produced no image artifact"},
		{name: "success", toolOK: true, artifacts: []v2Artifact{image}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateV2GeneratedImageResult(tt.toolOK, tt.artifacts)
			if tt.wantErrSub == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErrSub) {
				t.Fatalf("error=%v want substring %q", err, tt.wantErrSub)
			}
		})
	}
}

func TestV2ImageGenSuccessRequiresSuccessfulToolResult(t *testing.T) {
	tests := []struct {
		name  string
		event agent.ChatEvent
		want  bool
	}{
		{
			name:  "successful image tool result",
			event: agent.ChatEvent{Type: "tool_result", Data: map[string]any{"name": "image_gen", "success": true}},
			want:  true,
		},
		{
			name:  "failed image tool result",
			event: agent.ChatEvent{Type: "tool_result", Data: map[string]any{"name": "image_gen", "success": false}},
			want:  false,
		},
		{
			name:  "different tool",
			event: agent.ChatEvent{Type: "tool_result", Data: map[string]any{"name": "write_file", "success": true}},
			want:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSuccessfulV2ImageGenEvent(tt.event); got != tt.want {
				t.Fatalf("isSuccessfulV2ImageGenEvent()=%v want=%v", got, tt.want)
			}
		})
	}
}

func TestV2GeneratedImageRequestFailsWhenImageGenIsUnavailable(t *testing.T) {
	srv := newV2TestServer(t, workspace.NewLocalFS(t.TempDir()))
	req := v2Request(t, http.MethodPost, "/v2/agents/agt_test/runs", strings.NewReader(`{
		"input":"Create a polished poster image for our launch",
		"sessionId":"session-image-required"
	}`))
	setV2PathValues(req, "agt_test", "", "", "")
	rec := httptest.NewRecorder()

	srv.HandleV2CreateRun(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d want=422 body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "image_gen is required but unavailable") {
		t.Fatalf("missing explicit image_gen error: %s", rec.Body.String())
	}
}

func TestV2GeneratedImageRequestFailsInsteadOfAcceptingFileToolFallback(t *testing.T) {
	srv := newV2TestServerWithProvider(
		t,
		workspace.NewLocalFS(t.TempDir()),
		&toolThenStreamProvider{},
		2,
	)
	registerV2TestImageGen(t, srv)
	req := v2Request(t, http.MethodPost, "/v2/agents/agt_test/runs", strings.NewReader(`{
		"input":"Create a polished poster image for our launch",
		"sessionId":"session-image-no-fallback"
	}`))
	setV2PathValues(req, "agt_test", "", "", "")
	rec := httptest.NewRecorder()

	srv.HandleV2CreateRun(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "event: run.failed") ||
		!strings.Contains(body, "image_gen did not complete successfully") {
		t.Fatalf("missing explicit image_gen failure:\n%s", body)
	}
	if strings.Contains(body, "event: artifact.created") ||
		strings.Contains(body, "event: run.completed") {
		t.Fatalf("file-tool fallback was exposed as successful:\n%s", body)
	}
}

func TestV2GeneratedImageRequestCompletesAfterImageGenCreatesImage(t *testing.T) {
	srv := newV2TestServerWithProvider(
		t,
		workspace.NewLocalFS(t.TempDir()),
		&imageToolThenStreamProvider{},
		2,
	)
	registerV2TestImageGen(t, srv)
	req := v2Request(t, http.MethodPost, "/v2/agents/agt_test/runs", strings.NewReader(`{
		"input":"Create a polished poster image for our launch",
		"sessionId":"session-image-success"
	}`))
	setV2PathValues(req, "agt_test", "", "", "")
	rec := httptest.NewRecorder()

	srv.HandleV2CreateRun(rec, req)

	body := rec.Body.String()
	for _, event := range []string{"event: tool.completed", "event: artifact.created", "event: message.completed", "event: run.completed"} {
		if !strings.Contains(body, event) {
			t.Fatalf("missing %q:\n%s", event, body)
		}
	}
	if !strings.Contains(body, `"name":"image_gen"`) ||
		!strings.Contains(body, `"contentType":"image/png"`) {
		t.Fatalf("missing successful image_gen artifact:\n%s", body)
	}
	if strings.Contains(body, "event: run.failed") {
		t.Fatalf("successful image run failed:\n%s", body)
	}
}

func TestV2MessageCompletedRemovesLocalOnlyArtifactLinks(t *testing.T) {
	raw := "Ready.\n\n![poster](sandbox:/mnt/data/poster.png)\n\n[report](sandbox:/workspace/report.html)\n\nDirect: sandbox:/mnt/data/raw.md and /workspace/notes.txt\n\n![remote](https://cdn.example.com/remote.png)"
	srv := newV2TestServerWithProvider(
		t,
		workspace.NewLocalFS(t.TempDir()),
		textStreamProvider{content: raw},
		1,
	)
	req := v2Request(t, http.MethodPost, "/v2/agents/agt_test/runs", strings.NewReader(`{
		"input":"Summarize the existing files",
		"sessionId":"session-sanitize"
	}`))
	setV2PathValues(req, "agt_test", "", "", "")
	rec := httptest.NewRecorder()

	srv.HandleV2CreateRun(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	completed := sseDataString(t, body, "message.completed", "content")
	for _, unsafe := range []string{"sandbox:", "/mnt/data", "/workspace"} {
		if strings.Contains(completed, unsafe) {
			t.Fatalf("message.completed retained %q: %s", unsafe, completed)
		}
	}
	if !strings.Contains(completed, "report") {
		t.Fatalf("file label was lost: %s", completed)
	}
	if !strings.Contains(completed, "raw.md") || !strings.Contains(completed, "notes.txt") {
		t.Fatalf("plain local file names were lost: %s", completed)
	}
	if !strings.Contains(completed, "https://cdn.example.com/remote.png") {
		t.Fatalf("remote image was removed: %s", completed)
	}
	runOutput := sseDataString(t, body, "run.completed", "output")
	if runOutput != completed {
		t.Fatalf("run output=%q want authoritative message=%q", runOutput, completed)
	}
}

func TestV2SessionMessagesReturnSanitizedDurableHistory(t *testing.T) {
	raw := "Ready.\n\n![poster](sandbox:/mnt/data/poster.png)\n\n[report](/workspace/report.html)"
	srv := newV2TestServerWithProvider(
		t,
		workspace.NewLocalFS(t.TempDir()),
		textStreamProvider{content: raw},
		1,
	)
	runReq := v2Request(t, http.MethodPost, "/v2/agents/agt_test/runs", strings.NewReader(`{
		"input":"Summarize the existing files",
		"sessionId":"session-history"
	}`))
	setV2PathValues(runReq, "agt_test", "", "", "")
	runRec := httptest.NewRecorder()
	srv.HandleV2CreateRun(runRec, runReq)
	if runRec.Code != http.StatusOK {
		t.Fatalf("run status=%d body=%s", runRec.Code, runRec.Body.String())
	}

	historyReq := v2Request(t, http.MethodGet, "/v2/agents/agt_test/sessions/session-history/messages", nil)
	setV2PathValues(historyReq, "agt_test", "session-history", "", "")
	historyRec := httptest.NewRecorder()
	srv.HandleV2ListSessionMessages(historyRec, historyReq)

	if historyRec.Code != http.StatusOK {
		t.Fatalf("history status=%d body=%s", historyRec.Code, historyRec.Body.String())
	}
	var body struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(historyRec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(body.Messages) != 2 {
		t.Fatalf("messages=%#v", body.Messages)
	}
	assistantContent, _ := body.Messages[1]["content"].(string)
	for _, unsafe := range []string{"sandbox:", "/mnt/data", "/workspace"} {
		if strings.Contains(assistantContent, unsafe) {
			t.Fatalf("history retained %q: %s", unsafe, assistantContent)
		}
	}
	if !strings.Contains(assistantContent, "report") {
		t.Fatalf("history lost file label: %s", assistantContent)
	}
}

func TestV2SessionMessagesHideInternalToolTranscript(t *testing.T) {
	srv := newV2TestServerWithProvider(
		t,
		workspace.NewLocalFS(t.TempDir()),
		&toolThenStreamProvider{},
		2,
	)
	runReq := v2Request(t, http.MethodPost, "/v2/agents/agt_test/runs", strings.NewReader(`{
		"input":"Create the report",
		"sessionId":"session-tool-history"
	}`))
	setV2PathValues(runReq, "agt_test", "", "", "")
	runRec := httptest.NewRecorder()
	srv.HandleV2CreateRun(runRec, runReq)
	if runRec.Code != http.StatusOK {
		t.Fatalf("run status=%d body=%s", runRec.Code, runRec.Body.String())
	}

	historyReq := v2Request(t, http.MethodGet, "/v2/agents/agt_test/sessions/session-tool-history/messages", nil)
	setV2PathValues(historyReq, "agt_test", "session-tool-history", "", "")
	historyRec := httptest.NewRecorder()
	srv.HandleV2ListSessionMessages(historyRec, historyReq)

	if historyRec.Code != http.StatusOK {
		t.Fatalf("history status=%d body=%s", historyRec.Code, historyRec.Body.String())
	}
	var body struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(historyRec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(body.Messages) != 2 {
		t.Fatalf("messages=%#v", body.Messages)
	}
	if body.Messages[0]["role"] != "user" || body.Messages[0]["content"] != "Create the report" {
		t.Fatalf("unexpected user message: %#v", body.Messages[0])
	}
	if body.Messages[1]["role"] != "assistant" || body.Messages[1]["content"] != "The report is ready." {
		t.Fatalf("unexpected assistant message: %#v", body.Messages[1])
	}
	for _, message := range body.Messages {
		if _, exists := message["toolCalls"]; exists {
			t.Fatalf("history exposed tool calls: %#v", message)
		}
		if _, exists := message["toolCallId"]; exists {
			t.Fatalf("history exposed tool result identity: %#v", message)
		}
	}
}

func TestV2HTTPStreamsBeforeCompletionThenServesArtifactURL(t *testing.T) {
	ws := workspace.NewLocalFS(t.TempDir())
	continueStream := make(chan struct{})
	srv := newV2TestServerWithProvider(t, ws, &toolThenStreamProvider{continueStream: continueStream}, 2)

	withIdentity := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			ctx := auth.WithIdentity(r.Context(), auth.Identity{
				UserID:     "user-1",
				AuthMethod: "apikey",
				APIKeyType: "admin",
			})
			next(w, r.WithContext(ctx))
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v2/agents/{agentId}/runs", withIdentity(srv.HandleV2CreateRun))
	mux.HandleFunc("GET /v2/agents/{agentId}/sessions/{sessionId}/files", withIdentity(srv.HandleV2ListSessionFiles))
	mux.HandleFunc("GET /v2/agents/{agentId}/sessions/{sessionId}/files/{fileId}", withIdentity(srv.HandleV2GetSessionFile))
	httpServer := httptest.NewServer(mux)
	t.Cleanup(httpServer.Close)

	response, err := http.Post(
		httpServer.URL+"/v2/agents/agt_test/runs",
		"application/json",
		strings.NewReader(`{"input":"Create an HTML report","sessionId":"session-http"}`),
	)
	if err != nil {
		t.Fatalf("POST run: %v", err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}

	reader := bufio.NewReader(response.Body)
	var streamed strings.Builder
	for !strings.Contains(streamed.String(), `"delta":"The report "`) {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read first delta before completion: %v\n%s", err, streamed.String())
		}
		streamed.WriteString(line)
	}
	if strings.Contains(streamed.String(), "event: run.completed") {
		t.Fatalf("run completed before the blocked second chunk:\n%s", streamed.String())
	}
	close(continueStream)
	rest, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read remaining stream: %v", err)
	}
	streamed.Write(rest)
	if !strings.Contains(streamed.String(), `"delta":"is ready."`) || !strings.Contains(streamed.String(), "event: run.completed") {
		t.Fatalf("incomplete SSE:\n%s", streamed.String())
	}

	listResponse, err := http.Get(httpServer.URL + "/v2/agents/agt_test/sessions/session-http/files")
	if err != nil {
		t.Fatalf("GET files: %v", err)
	}
	t.Cleanup(func() { _ = listResponse.Body.Close() })
	var listed struct {
		Files []v2Artifact `json:"files"`
	}
	if err := json.NewDecoder(listResponse.Body).Decode(&listed); err != nil {
		t.Fatalf("decode files: %v", err)
	}
	if len(listed.Files) != 1 {
		t.Fatalf("files=%#v", listed.Files)
	}

	previewResponse, err := http.Get(httpServer.URL + listed.Files[0].PreviewURL)
	if err != nil {
		t.Fatalf("GET preview: %v", err)
	}
	t.Cleanup(func() { _ = previewResponse.Body.Close() })
	previewBody, err := io.ReadAll(previewResponse.Body)
	if err != nil {
		t.Fatalf("read preview: %v", err)
	}
	if previewResponse.StatusCode != http.StatusOK || !bytes.Contains(previewBody, []byte("<h1>NVDA</h1>")) {
		t.Fatalf("preview status=%d body=%q", previewResponse.StatusCode, previewBody)
	}
}

func TestV2CreateRunRejectsUnsafeSessionID(t *testing.T) {
	srv := newV2TestServer(t, workspace.NewLocalFS(t.TempDir()))
	req := v2Request(t, http.MethodPost, "/v2/agents/agt_test/runs", strings.NewReader(`{
		"input":"hello",
		"sessionId":"../../other-user"
	}`))
	setV2PathValues(req, "agt_test", "", "", "")
	rec := httptest.NewRecorder()

	srv.HandleV2CreateRun(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want=400 body=%s", rec.Code, rec.Body.String())
	}
}

func TestV2RoutesAreAdditiveAndV1RoutesRemainRegistered(t *testing.T) {
	srv := &Server{}
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	tests := []struct {
		method string
		path   string
	}{
		{method: http.MethodPost, path: "/v1/chat/completions"},
		{method: http.MethodPost, path: "/v1/chat/completions-v1"},
		{method: http.MethodPost, path: "/v2/agents/agt_test/runs"},
		{method: http.MethodGet, path: "/v2/agents/agt_test/runs/run_test"},
		{method: http.MethodGet, path: "/v2/agents/agt_test/sessions/session-one/messages"},
		{method: http.MethodGet, path: "/v2/agents/agt_test/sessions/session-one/files"},
		{method: http.MethodGet, path: "/v2/agents/agt_test/sessions/session-one/files/file_test"},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, tt.path, nil)
		_, pattern := mux.Handler(req)
		if pattern == "" {
			t.Fatalf("route %s %s is not registered", tt.method, tt.path)
		}
	}
}

func sseDataString(t *testing.T, body, eventName, field string) string {
	t.Helper()
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if line != "event: "+eventName || i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], "data: ") {
			continue
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[i+1], "data: ")), &data); err != nil {
			t.Fatalf("decode %s event: %v", eventName, err)
		}
		value, _ := data[field].(string)
		if value == "" {
			t.Fatalf("%s.%s missing in %#v", eventName, field, data)
		}
		return value
	}
	t.Fatalf("event %s not found in:\n%s", eventName, body)
	return ""
}

func TestV2ArtifactDiffIncludesNewAndChangedFiles(t *testing.T) {
	now := time.Now().UTC()
	before := []workspace.ObjectInfo{
		{Path: "same.md", Size: 4, ModTime: now},
		{Path: "changed.html", Size: 10, ModTime: now},
	}
	after := []workspace.ObjectInfo{
		{Path: "same.md", Size: 4, ModTime: now},
		{Path: "changed.html", Size: 20, ModTime: now.Add(time.Second)},
		{Path: "new.png", Size: 30, ModTime: now},
	}

	got := diffV2Artifacts("agt_test", "session-one", before, after)

	if len(got) != 2 || got[0].Name != "changed.html" || got[1].Name != "new.png" {
		t.Fatalf("artifacts=%#v", got)
	}
}

func TestV2RunStateExpiresAfterRecoveryWindow(t *testing.T) {
	srv := &Server{}
	srv.storeV2Run(v2Run{
		ID:        "run_expired",
		AgentID:   "agt_test",
		SessionID: "session-one",
		Status:    v2RunCompleted,
		CreatedAt: time.Now().UTC().Add(-6 * time.Minute),
	})

	if _, ok := srv.loadV2Run("run_expired"); ok {
		t.Fatal("expired run state is still available")
	}
}
