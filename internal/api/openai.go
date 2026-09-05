package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/fastclaw-ai/fastclaw/internal/agent"
	"github.com/fastclaw-ai/fastclaw/internal/auth"
	"github.com/fastclaw-ai/fastclaw/internal/bus"
	"github.com/fastclaw-ai/fastclaw/internal/provider"
	"github.com/fastclaw-ai/fastclaw/internal/workspace"
)

// apiStreamHeartbeatInterval is how often /v1/chat/completions writes an
// SSE comment while the agent is silent (ReAct / tools / a stalled
// token). Upstream HTTP clients and proxies otherwise treat the idle
// body as a dead connection and hang up.
var apiStreamHeartbeatInterval = 10 * time.Second

// chatCompletionRequest mirrors the OpenAI chat completion request.
//
// User is OpenAI's standard "end-user identifier" field. When the
// request authenticates with an api_key, a non-empty value triggers
// rebinding the request identity to a fastclaw app_user keyed on
// (apikey_id, user) so sessions and agent_files partition per
// end-user. Clients that prefer a header-only contract can use
// X-Fastclaw-End-User instead — both arrive at the same code path.
type chatCompletionRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   *bool         `json:"stream,omitempty"`
	User     string        `json:"user,omitempty"`
	// AgentID is a fastclaw extension: lets the caller pick the agent
	// in the request body instead of (or in addition to) the
	// `x-fastclaw-agent-id` header. Body wins when both are set —
	// matches the pattern used for `user`. Optional.
	AgentID string `json:"agent_id,omitempty"`
	// Params is a fastclaw extension: a freeform structured-parameter
	// blob the calling app submits alongside the chat. Rendered into
	// a per-turn system message so the agent's LLM can honor it when
	// calling tools (e.g. a third-party app's "model selector" +
	// "settings" UI translate to {provider, aspect_ratio, n} here,
	// rather than the user typing those into the prompt). Optional
	// params.user_id (string) is also the chatter id for USER.md /
	// MEMORY.md when X-Fastclaw-Chatter is omitted; if both are set
	// they must match. Scope is
	// per-request — params don't persist across turns. OpenAI clients
	// that don't know about this field are unaffected (omitempty).
	Params map[string]any `json:"params,omitempty"`
	// Images is a fastclaw extension: image attachments for the
	// current turn. Each entry is one of:
	//   - HTTPS URL: "https://example.com/photo.jpg" (must be
	//     reachable from the LLM provider; not validated here)
	//   - Data URL:  "data:image/png;base64,iVBORw0KGgo..."
	//
	// Accepted MIME types depend on the LLM model. Anthropic / OpenAI
	// vision models all support png, jpeg, webp; gif is hit-or-miss.
	// Per-image and total-request size limits are also model-side
	// (Anthropic ~5MB/image, OpenAI ~20MB) — fastclaw does not enforce
	// its own ceiling, the upstream provider returns the rejection.
	Images []string `json:"images,omitempty"`
	// ImageURLs is an accepted alias for Images. The web-facing chat
	// endpoint historically calls this field `imageUrls`; allowing it
	// here means a caller writing one client against both endpoints
	// doesn't get silently dropped attachments when they pick the
	// wrong name.
	ImageURLs []string `json:"imageUrls,omitempty"`
	// Attachments is the typed, general-purpose attachment field. Each
	// entry can carry an optional Name which is sanitized and used as
	// the on-disk filename so the LLM sees `report.pdf` instead of
	// `image_3jk7l_0.pdf`. Unlike Images / ImageURLs, entries here are
	// NOT inlined as vision content parts — they only land in
	// /workspace and reach the LLM via the `[Attached: /workspace/X]`
	// breadcrumb. Use Images / ImageURLs (not Attachments) when you
	// want the bytes shown directly to a vision model.
	Attachments []attachmentRequest `json:"attachments,omitempty"`
}

// attachmentRequest is the wire form of a single attachment.
type attachmentRequest struct {
	URL  string `json:"url"`
	Name string `json:"name,omitempty"`
}

// allAttachments flattens the three input shapes (Images, ImageURLs,
// Attachments) into one ordered slice for materialization into
// /workspace. Clients normally pick one; mixing is allowed.
func (r chatCompletionRequest) allAttachments() []agent.Attachment {
	n := len(r.Images) + len(r.ImageURLs) + len(r.Attachments)
	if n == 0 {
		return nil
	}
	out := make([]agent.Attachment, 0, n)
	for _, u := range r.Images {
		out = append(out, agent.Attachment{URL: u})
	}
	for _, u := range r.ImageURLs {
		out = append(out, agent.Attachment{URL: u})
	}
	for _, a := range r.Attachments {
		out = append(out, agent.Attachment{URL: a.URL, Name: a.Name})
	}
	return out
}

// inlineImageURLs returns just the URLs eligible for vision inline
// (PhotoURLs → image_url content blocks). Only Images and ImageURLs
// qualify — by contract they're caller-asserted images. The general
// Attachments field is excluded: feeding a PDF / zip URL through the
// vision channel returns HTTP 400 from upstream providers and sinks
// the whole turn. Attachments reach the LLM via the
// `[Attached: /workspace/<file>]` breadcrumb instead.
func (r chatCompletionRequest) inlineImageURLs() []string {
	if len(r.Images) == 0 && len(r.ImageURLs) == 0 {
		return nil
	}
	out := make([]string, 0, len(r.Images)+len(r.ImageURLs))
	out = append(out, r.Images...)
	out = append(out, r.ImageURLs...)
	return out
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatCompletionChunk is a single SSE chunk in streaming mode.
type chatCompletionChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []chunkChoice `json:"choices"`
}

type chunkChoice struct {
	Index        int        `json:"index"`
	Delta        chunkDelta `json:"delta"`
	FinishReason *string    `json:"finish_reason"`
}

type chunkDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// chatCompletionResponse is the non-streaming response.
type chatCompletionResponse struct {
	ID         string             `json:"id"`
	Object     string             `json:"object"`
	Created    int64              `json:"created"`
	Model      string             `json:"model"`
	Choices    []completionChoice `json:"choices"`
	Usage      completionUsage    `json:"usage"`
	SessionID  string             `json:"session_id,omitempty"`
	SessionKey string             `json:"session_key,omitempty"`
}

type completionChoice struct {
	Index        int         `json:"index"`
	Message      chatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type completionUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// HandleChatCompletions handles POST /v1/chat/completions.
func (s *Server) HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	var req chatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": "invalid request body", "type": "invalid_request_error"},
		})
		return
	}

	if len(req.Messages) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": "messages is required", "type": "invalid_request_error"},
		})
		return
	}

	// OpenAI's `user` body field, when present on an api_key call,
	// rebinds the identity to the corresponding app_user (lazy mint).
	// Header X-Fastclaw-End-User does the same job pre-handler in the
	// auth middleware; we run this *after* the middleware so the body
	// value wins iff both are present (the body field is more
	// specific to this call than a static header). Errors here are
	// non-fatal — request continues under the unswitched identity.
	if req.User != "" && s.authResolver != nil {
		if ident, ok := auth.FromContext(r.Context()); ok {
			if next, swErr := s.authResolver.SwitchToAppUser(r.Context(), ident, req.User); swErr == nil {
				r = r.WithContext(auth.WithIdentity(r.Context(), next))
			}
		}
	}

	// Resolve the caller's user space (set by authMiddleware) and pick an
	// agent out of it.
	space, err := s.userSpaceFor(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": map[string]string{"message": err.Error(), "type": "authentication_error"},
		})
		return
	}

	// Body field beats header — same precedence as `user`. Lets app
	// callers send everything in one JSON without juggling headers.
	agentID := r.Header.Get("x-fastclaw-agent-id")
	if req.AgentID != "" {
		agentID = req.AgentID
	}
	ag := resolveAgent(space, agentID)
	if ag == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": map[string]string{"message": "agent not found", "type": "not_found_error"},
		})
		return
	}
	// Apikey ACL gate. UserSpaceFor loads every agent the owner has,
	// regardless of which subset this particular apikey is scoped to.
	// Without this check a type=agent apikey scoped to one agent
	// could pass `x-fastclaw-agent-id: <sibling>` (or omit it and
	// fall back to default / all[0]) and talk to any of the owner's
	// agents. The /v1/agents listing already filters by
	// CanAccessAgent — mirror that here so apikey scope is enforced
	// uniformly. Use 404 (not 403) so the response is identical to
	// the genuine "no such agent" case and the ACL doesn't leak the
	// existence of out-of-scope agents.
	if ident, ok := auth.FromContext(r.Context()); ok && !ident.CanAccessAgent(ag.Name()) {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": map[string]string{"message": "agent not found", "type": "not_found_error"},
		})
		return
	}

	// Build session key from header
	sessionKey := r.Header.Get("x-fastclaw-session-key")
	if sessionKey == "" {
		sessionKey = "api-" + fmt.Sprintf("%d", time.Now().UnixNano())
	}

	// Extract the last user message
	var userText string
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			userText = req.Messages[i].Content
			break
		}
	}
	if userText == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": "no user message found", "type": "invalid_request_error"},
		})
		return
	}

	chatter, chatterErr := resolveAPIChatter(r.Header.Get(ChatterHeader), req.Params)
	if chatterErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": chatterErr.Error(), "type": "invalid_request_error"},
		})
		return
	}

	// Materialize attached images into the agent's session workspace and
	// prepend the same `[Attached: /workspace/<file>]` breadcrumb the web
	// UI uses (web/src/app/agents/[id]/chat/page.tsx:639-645) so the wire
	// shape is identical across web and API entry points. Verbose "do not
	// probe" notes here actively backfire — models reflexively run
	// which/ls/file to "verify" the path when the prompt foregrounds it.
	// PhotoURLs is preserved so vision LLMs still see the image inline.
	// API clients can't address a project today — chat completions only
	// know session_key — so attachments always land in the loose-chat
	// scope. When/if we expose project addressing here, look up the
	// session row and pass its project_id instead of "".
	atts := req.allAttachments()
	attachmentPaths := ag.WriteSessionAttachments(r.Context(), sessionKey, "", atts)
	if len(attachmentPaths) > 0 {
		var b strings.Builder
		for _, p := range attachmentPaths {
			b.WriteString("[Attached: /workspace/")
			b.WriteString(p)
			b.WriteString("]\n")
		}
		b.WriteString(userText)
		userText = b.String()
	}

	// Build inbound message.
	// X-Fastclaw-Channel lets callers override the reply channel so
	// cron jobs created during this turn route through the right
	// adapter (e.g. "pinclaw" → plugin channel.send → Cloud API).
	channel := r.Header.Get("x-fastclaw-channel")
	if channel == "" {
		channel = "api"
	}
	msg := bus.InboundMessage{
		Channel:   channel,
		ChatID:    sessionKey,
		UserID:    chatter,
		Text:      userText,
		PeerKind:  "dm",
		Params:    req.Params,
		PhotoURLs: req.inlineImageURLs(),
	}
	nativeSessionID := ag.SessionKeyFor(msg)
	writeSessionHeaders(w, sessionKey, nativeSessionID)

	slog.Info("chat completion request",
		"agent", ag.Name(),
		"session", sessionKey,
		"session_id", nativeSessionID,
		"chatter", chatter,
		"stream", req.Stream != nil && *req.Stream,
	)

	model := ag.Model()
	if req.Model != "" {
		model = req.Model
	}
	chatID := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	now := time.Now().Unix()

	isStream := req.Stream != nil && *req.Stream
	// API chats have no project addressing today — attachments and
	// write_file land in the loose-chat scope keyed by ChatID. Rewrite
	// uses that same store session so PublicURL matches the object.
	storeSess := ag.StoreSessionID("", msg.ChatID)
	if isStream {
		s.streamResponseFromAgent(w, r, ag, msg, chatID, model, now, storeSess)
	} else {
		// Get reply from agent. Rewrite /workspace/ → PublicURL on the
		// HTTP response only; the stored session markdown stays as
		// /workspace/<rel> so history survives CDN / source switches.
		reply := ag.HandleMessage(r.Context(), msg)
		reply = rewriteWorkspaceURLsToPublic(r.Context(), ag.WorkspaceStore(), ag.Name(), "", storeSess, reply)
		s.fullResponse(w, reply, chatID, model, now, sessionKey, nativeSessionID)
	}
}

func (s *Server) streamResponseFromAgent(w http.ResponseWriter, r *http.Request, ag *agent.Agent, msg bus.InboundMessage, chatID, model string, created int64, storeSess string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	flush := func() {
		if ok {
			flusher.Flush()
		}
	}

	// Role chunk first — HandleMessageStream runs the whole ReAct /
	// tool loop before it returns a reader. Without an immediate body
	// write the connection stays empty and API clients drop it.
	s.writeSSEChunk(w, chatID, model, created, "assistant", "", nil)
	flush()

	srCh := make(chan *provider.StreamReader, 1)
	go func() {
		srCh <- ag.HandleMessageStream(r.Context(), msg)
	}()

	ticker := time.NewTicker(apiStreamHeartbeatInterval)
	defer ticker.Stop()

	var sr *provider.StreamReader
	for sr == nil {
		select {
		case sr = <-srCh:
		case <-ticker.C:
			writeSSEComment(w, "heartbeat")
			flush()
		case <-r.Context().Done():
			return
		}
	}

	chunkCh := make(chan provider.StreamChunk, 8)
	go func() {
		defer close(chunkCh)
		for {
			chunk, more := sr.Next()
			select {
			case chunkCh <- chunk:
			case <-r.Context().Done():
				return
			}
			if chunk.Done || !more {
				return
			}
		}
	}()

	rewriter := newWorkspaceURLStreamRewriter(r.Context(), ag.WorkspaceStore(), ag.Name(), "", storeSess)
	for {
		select {
		case chunk, open := <-chunkCh:
			if !open {
				if flushed := rewriter.Flush(); flushed != "" {
					s.writeSSEChunk(w, chatID, model, created, "", flushed, nil)
					flush()
				}
				done := "stop"
				s.writeSSEChunk(w, chatID, model, created, "", "", &done)
				fmt.Fprint(w, "data: [DONE]\n\n")
				flush()
				return
			}
			if chunk.Content != "" {
				if content := rewriter.Write(chunk.Content); content != "" {
					s.writeSSEChunk(w, chatID, model, created, "", content, nil)
					flush()
				}
			}
		case <-ticker.C:
			writeSSEComment(w, "heartbeat")
			flush()
		case <-r.Context().Done():
			return
		}
	}
}

func writeSSEComment(w http.ResponseWriter, comment string) {
	fmt.Fprintf(w, ": %s\n\n", comment)
}

func (s *Server) writeSSEChunk(w http.ResponseWriter, id, model string, created int64, role, content string, finishReason *string) {
	chunk := chatCompletionChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model,
		Choices: []chunkChoice{
			{
				Index: 0,
				Delta: chunkDelta{
					Role:    role,
					Content: content,
				},
				FinishReason: finishReason,
			},
		},
	}
	data, _ := json.Marshal(chunk)
	fmt.Fprintf(w, "data: %s\n\n", data)
}

func (s *Server) fullResponse(w http.ResponseWriter, reply, chatID, model string, created int64, sessionKey, nativeSessionID string) {
	resp := chatCompletionResponse{
		ID:      chatID,
		Object:  "chat.completion",
		Created: created,
		Model:   model,
		Choices: []completionChoice{
			{
				Index:        0,
				Message:      chatMessage{Role: "assistant", Content: reply},
				FinishReason: "stop",
			},
		},
		Usage: completionUsage{
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
		},
		SessionID:  nativeSessionID,
		SessionKey: sessionKey,
	}
	writeJSON(w, http.StatusOK, resp)
}

func writeSessionHeaders(w http.ResponseWriter, sessionKey, nativeSessionID string) {
	if sessionKey != "" {
		w.Header().Set("X-Fastclaw-Session-Key", sessionKey)
	}
	if nativeSessionID != "" {
		w.Header().Set("X-Fastclaw-Session-Id", nativeSessionID)
	}
	w.Header().Set("Access-Control-Expose-Headers", "X-Fastclaw-Session-Id, X-Fastclaw-Session-Key")
}

// resolveAgent picks an agent out of the caller's user space, preferring an
// explicit agent ID from the x-fastclaw-agent-id header and falling back to
// the default / first agent.
func resolveAgent(space *UserSpaceView, agentID string) *agent.Agent {
	mgr := space.Agents
	if agentID != "" {
		if ag := mgr.AgentByID(agentID); ag != nil {
			return ag
		}
	}
	if def := mgr.DefaultAgent(); def != nil {
		return def
	}
	all := mgr.All()
	if len(all) > 0 {
		return all[0]
	}
	return nil
}

var workspaceURLRefRegex = regexp.MustCompile(`https?://[^\s)]+/workspace/[^\s)]+|/workspace/[^\s)]+`)

// rewriteWorkspaceURLsToPublic rewrites /workspace/<rel> (and host-absolutized
// variants) to Store.PublicURL on the HTTP response only. Missing PublicURL
// leaves the session-stable /workspace/ path in place.
func rewriteWorkspaceURLsToPublic(ctx context.Context, ws workspace.Store, agentID, projectID, sessionID, text string) string {
	if text == "" || ws == nil || agentID == "" {
		return text
	}
	return workspaceURLRefRegex.ReplaceAllStringFunc(text, func(raw string) string {
		rel := raw
		if i := strings.Index(rel, "/workspace/"); i >= 0 {
			rel = rel[i+len("/workspace/"):]
		}
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			return raw
		}
		if u, err := ws.PublicURL(ctx, agentID, projectID, sessionID, rel); err == nil && strings.TrimSpace(u) != "" {
			return strings.TrimSpace(u)
		}
		return raw
	})
}

type workspaceURLStreamRewriter struct {
	ctx       context.Context
	ws        workspace.Store
	agentID   string
	projectID string
	sessionID string
	pending   string
}

func newWorkspaceURLStreamRewriter(ctx context.Context, ws workspace.Store, agentID, projectID, sessionID string) *workspaceURLStreamRewriter {
	return &workspaceURLStreamRewriter{
		ctx:       ctx,
		ws:        ws,
		agentID:   agentID,
		projectID: projectID,
		sessionID: sessionID,
	}
}

func (r *workspaceURLStreamRewriter) Write(content string) string {
	if content == "" {
		return ""
	}
	if r.ws == nil || r.agentID == "" {
		return content
	}

	r.pending += content
	start := workspaceURLStreamPendingStart(r.pending)
	if start == 0 {
		return ""
	}
	if start < 0 {
		content, r.pending = r.pending, ""
	} else {
		content, r.pending = r.pending[:start], r.pending[start:]
	}
	return rewriteWorkspaceURLsToPublic(r.ctx, r.ws, r.agentID, r.projectID, r.sessionID, content)
}

func (r *workspaceURLStreamRewriter) Flush() string {
	if r.pending == "" {
		return ""
	}
	content := r.pending
	r.pending = ""
	return rewriteWorkspaceURLsToPublic(r.ctx, r.ws, r.agentID, r.projectID, r.sessionID, content)
}

func workspaceURLStreamPendingStart(text string) int {
	if text == "" {
		return -1
	}

	const workspaceMarker = "/workspace/"
	if marker := strings.LastIndex(text, workspaceMarker); marker >= 0 {
		tail := text[marker+len(workspaceMarker):]
		if !workspaceURLTokenTerminated(tail) {
			return workspaceURLTokenStart(text, marker)
		}
	} else if n := longestSuffixPrefix(text, workspaceMarker); n > 0 {
		return workspaceURLTokenStart(text, len(text)-n)
	}

	if start := lastUnterminatedHTTPURLStart(text); start >= 0 {
		return start
	}
	return -1
}

func workspaceURLTokenStart(text string, workspaceMarkerStart int) int {
	prefix := text[:workspaceMarkerStart]
	httpsStart := strings.LastIndex(prefix, "https://")
	httpStart := strings.LastIndex(prefix, "http://")
	if httpStart > httpsStart {
		httpsStart = httpStart
	}
	if httpsStart >= 0 && !workspaceURLTokenTerminated(prefix[httpsStart:]) {
		return httpsStart
	}
	return workspaceMarkerStart
}

func lastUnterminatedHTTPURLStart(text string) int {
	httpsStart := strings.LastIndex(text, "https://")
	httpStart := strings.LastIndex(text, "http://")
	if httpStart > httpsStart {
		httpsStart = httpStart
	}
	if httpsStart >= 0 && !workspaceURLTokenTerminated(text[httpsStart:]) {
		return httpsStart
	}
	for _, prefix := range []string{"https://", "http://"} {
		if n := longestSuffixPrefix(text, prefix); n > 0 {
			return len(text) - n
		}
	}
	return -1
}

func workspaceURLTokenTerminated(text string) bool {
	return strings.IndexFunc(text, func(r rune) bool {
		return unicode.IsSpace(r) || r == ')'
	}) >= 0
}

func longestSuffixPrefix(text, prefix string) int {
	limit := len(text)
	if limit >= len(prefix) {
		limit = len(prefix) - 1
	}
	for n := limit; n > 0; n-- {
		if strings.HasSuffix(text, prefix[:n]) {
			return n
		}
	}
	return 0
}
