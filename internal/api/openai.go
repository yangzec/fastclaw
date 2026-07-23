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

	"github.com/fastclaw-ai/fastclaw/internal/agent"
	"github.com/fastclaw-ai/fastclaw/internal/auth"
	"github.com/fastclaw-ai/fastclaw/internal/bus"
	"github.com/fastclaw-ai/fastclaw/internal/provider"
	"github.com/fastclaw-ai/fastclaw/internal/workspace"
)

const apiAgentTurnTimeout = 45 * time.Minute

var apiStreamProgressInterval = 10 * time.Second

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
	// rather than the user typing those into the prompt). Scope is
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
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []completionChoice `json:"choices"`
	Usage   completionUsage    `json:"usage"`
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

type chatCompletionOptions struct {
	returnSessionHeaders bool
}

// HandleChatCompletions handles POST /v1/chat/completions.
func (s *Server) HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	s.handleChatCompletions(w, r, chatCompletionOptions{})
}

// HandleChatCompletionsV1 handles POST /v1/chat/completions-v1.
// It is wire-compatible with /v1/chat/completions, but additionally
// surfaces the FastClaw native session id in response headers so upstream
// product clients can call history/files APIs without reverse lookup.
func (s *Server) HandleChatCompletionsV1(w http.ResponseWriter, r *http.Request) {
	s.handleChatCompletions(w, r, chatCompletionOptions{returnSessionHeaders: true})
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request, opts chatCompletionOptions) {
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

	// Body field beats header — same precedence as `user`. Lets app
	// callers send everything in one JSON without juggling headers.
	requestedAgentID := r.Header.Get("x-fastclaw-agent-id")
	if req.AgentID != "" {
		requestedAgentID = req.AgentID
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
				for _, id := range agentIDsToInjectAfterUserSwitch(next, requestedAgentID) {
					if injector, ok := s.resolver.(AgentInjector); ok {
						if err := injector.EnsureAgent(r.Context(), next.UserID, id); err != nil {
							slog.Warn("app_user agent injection failed", "user", next.UserID, "agent", id, "error", err)
						}
					}
				}
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

	ag := resolveAgent(space, requestedAgentID)
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
		UserID:    "api-user",
		Text:      userText,
		PeerKind:  "dm",
		Params:    req.Params,
		PhotoURLs: req.inlineImageURLs(),
	}

	nativeSessionID := ""
	if opts.returnSessionHeaders {
		nativeSessionID = ag.NativeSessionKey(msg)
		if nativeSessionID != "" {
			w.Header().Set("X-Fastclaw-Session-Id", nativeSessionID)
		}
		w.Header().Set("X-Fastclaw-Session-Key", sessionKey)
		w.Header().Set("Access-Control-Expose-Headers", "X-Fastclaw-Session-Id, X-Fastclaw-Session-Key")
	}

	slog.Info("chat completion request",
		"agent", ag.Name(),
		"session", sessionKey,
		"stream", req.Stream != nil && *req.Stream,
	)

	model := ag.Model()
	if req.Model != "" {
		model = req.Model
	}
	chatID := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	now := time.Now().Unix()

	isStream := req.Stream != nil && *req.Stream
	agentCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), apiAgentTurnTimeout)
	defer cancel()
	started := time.Now()
	defer func() {
		if err := agentCtx.Err(); err != nil {
			slog.Warn("chat completion agent turn ended with context error",
				"agent", ag.Name(),
				"session", sessionKey,
				"channel", channel,
				"stream", isStream,
				"duration", time.Since(started).String(),
				"error", err,
			)
		} else {
			slog.Info("chat completion agent turn completed",
				"agent", ag.Name(),
				"session", sessionKey,
				"channel", channel,
				"stream", isStream,
				"duration", time.Since(started).String(),
			)
		}
	}()
	if isStream {
		if opts.returnSessionHeaders {
			s.streamResponseFromAgentV1(w, r.WithContext(agentCtx), ag, msg, chatID, model, now, nativeSessionID, sessionKey, space.Workspace)
		} else {
			s.streamResponseFromAgent(w, r.WithContext(agentCtx), ag, msg, chatID, model, now, space.Workspace)
		}
	} else {
		// Get reply from agent. Use the detached turn context so a caller-side
		// 60s HTTP timeout does not kill the agent between tool execution and
		// final synthesis; the hard cap above is the server-side bound.
		reply := ag.HandleMessage(agentCtx, msg)
		sessionScope := sessionKey
		if native := ag.NativeSessionKey(msg); native != "" {
			sessionScope = native
		}
		reply = rewriteWorkspaceURLsToPublic(agentCtx, space.Workspace, ag.Name(), "", sessionScope, reply)
		s.fullResponse(w, reply, chatID, model, now)
	}
}

// HandleResolveChatSessionID resolves an external FastClaw session key to the
// native s-* session id used by history/files APIs.
func (s *Server) HandleResolveChatSessionID(w http.ResponseWriter, r *http.Request) {
	space, err := s.userSpaceFor(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]string{"message": err.Error(), "type": "authentication_error"}})
		return
	}
	agentID := r.URL.Query().Get("agent_id")
	if agentID == "" {
		agentID = r.URL.Query().Get("agentId")
	}
	if agentID == "" {
		agentID = r.Header.Get("x-fastclaw-agent-id")
	}
	if agentID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"message": "agent_id is required", "type": "invalid_request_error"}})
		return
	}
	ag := space.Agents.AgentByID(agentID)
	if ag == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": map[string]string{"message": "agent not found", "type": "invalid_request_error"}})
		return
	}
	sessionKey := r.URL.Query().Get("session_key")
	if sessionKey == "" {
		sessionKey = r.URL.Query().Get("sessionKey")
	}
	if sessionKey == "" {
		sessionKey = r.Header.Get("x-fastclaw-session-key")
	}
	if sessionKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"message": "session_key is required", "type": "invalid_request_error"}})
		return
	}
	channel := r.URL.Query().Get("channel")
	if channel == "" {
		channel = r.Header.Get("x-fastclaw-channel")
	}
	if channel == "" {
		channel = "api"
	}
	native := ag.NativeSessionKey(bus.InboundMessage{Channel: channel, ChatID: sessionKey, UserID: "api-user", PeerKind: "dm"})
	if native == "" {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": map[string]string{"message": "session not found", "type": "invalid_request_error"}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessionId": native, "sessionKey": sessionKey})
}

func (s *Server) streamResponseFromAgentV1(w http.ResponseWriter, r *http.Request, ag *agent.Agent, msg bus.InboundMessage, chatID, model string, created int64, sessionID, sessionKey string, ws workspace.Store) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	flush := func() {
		if ok {
			flusher.Flush()
		}
	}

	s.writeNamedSSE(w, "ready", map[string]string{"sessionId": sessionID, "sessionKey": sessionKey})
	flush()

	events := make(chan agent.ChatEvent, 64)
	agentCtx := agent.ContextWithChatEvents(r.Context(), events)
	srCh := make(chan *provider.StreamReader, 1)
	go func() {
		srCh <- ag.HandleMessageStream(agentCtx, msg)
	}()

	ticker := time.NewTicker(apiStreamProgressInterval)
	defer ticker.Stop()

	var sr *provider.StreamReader
	for sr == nil {
		select {
		case sr = <-srCh:
		case evt := <-events:
			s.writeAgentEventSSE(w, evt)
			flush()
		case <-ticker.C:
			s.writeNamedSSE(w, "heartbeat", map[string]any{"type": "heartbeat"})
			flush()
		case <-r.Context().Done():
			return
		}
	}

	// Send the OpenAI-compatible role chunk after the native ready/status phase.
	s.writeSSEChunk(w, chatID, model, created, "assistant", "", nil)
	flush()

	chunkCh := make(chan provider.StreamChunk, 1)
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
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

	streamDone := false
	var content strings.Builder
	for !streamDone {
		select {
		case chunk := <-chunkCh:
			if chunk.Content != "" {
				content.WriteString(chunk.Content)
			}
			if chunk.Done {
				streamDone = true
			}
		case evt := <-events:
			s.writeAgentEventSSE(w, evt)
			flush()
		case <-ticker.C:
			s.writeNamedSSE(w, "heartbeat", map[string]any{"type": "heartbeat"})
			flush()
		case <-doneCh:
			streamDone = true
		case <-r.Context().Done():
			return
		}
	}

	finalContent := rewriteWorkspaceURLsToPublic(r.Context(), ws, ag.Name(), msg.ProjectID, msg.ChatID, content.String())
	if finalContent != "" {
		s.writeSSEChunk(w, chatID, model, created, "", finalContent, nil)
		flush()
	}

	done := "stop"
	s.writeSSEChunk(w, chatID, model, created, "", "", &done)
	fmt.Fprint(w, "data: [DONE]\n\n")
	flush()
}

func (s *Server) writeAgentEventSSE(w http.ResponseWriter, evt agent.ChatEvent) {
	switch evt.Type {
	case "status", "tool_call", "tool_call_delta", "tool_result", "tool_progress", "heartbeat":
		s.writeNamedSSE(w, evt.Type, map[string]any{"type": evt.Type, "data": evt.Data})
	}
}

func (s *Server) writeNamedSSE(w http.ResponseWriter, event string, data any) {
	blob, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, blob)
}

func (s *Server) streamResponseFromAgent(w http.ResponseWriter, r *http.Request, ag *agent.Agent, msg bus.InboundMessage, chatID, model string, created int64, ws workspace.Store) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)

	sr := ag.HandleMessageStream(r.Context(), msg)

	// Send role chunk
	s.writeSSEChunk(w, chatID, model, created, "assistant", "", nil)
	if ok {
		flusher.Flush()
	}

	// Forward chunks from StreamReader. Buffer content so workspace links that
	// span model chunks can be rewritten to the configured public object-store URL.
	var content strings.Builder
	for {
		chunk, more := sr.Next()
		if chunk.Content != "" {
			content.WriteString(chunk.Content)
		}
		if chunk.Done || !more {
			break
		}
	}
	finalContent := rewriteWorkspaceURLsToPublic(r.Context(), ws, ag.Name(), msg.ProjectID, msg.ChatID, content.String())
	if finalContent != "" {
		s.writeSSEChunk(w, chatID, model, created, "", finalContent, nil)
		if ok {
			flusher.Flush()
		}
	}

	// Send finish chunk
	done := "stop"
	s.writeSSEChunk(w, chatID, model, created, "", "", &done)
	fmt.Fprint(w, "data: [DONE]\n\n")
	if ok {
		flusher.Flush()
	}
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

func (s *Server) fullResponse(w http.ResponseWriter, reply, chatID, model string, created int64) {
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
	}
	writeJSON(w, http.StatusOK, resp)
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

func agentIDsToInjectAfterUserSwitch(ident auth.Identity, requestedAgentID string) []string {
	if requestedAgentID != "" {
		if ident.CanAccessAgent(requestedAgentID) {
			return []string{requestedAgentID}
		}
		return nil
	}
	seen := make(map[string]bool, len(ident.APIKeyAgents))
	ids := make([]string, 0, len(ident.APIKeyAgents))
	for _, id := range ident.APIKeyAgents {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}

var workspaceURLRefRegex = regexp.MustCompile(`https?://[^\s)]+/workspace/[^\s)]+|/workspace/[^\s)]+`)

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
