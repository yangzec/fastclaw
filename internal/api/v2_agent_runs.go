package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/agent"
	"github.com/fastclaw-ai/fastclaw/internal/auth"
	"github.com/fastclaw-ai/fastclaw/internal/bus"
	"github.com/fastclaw-ai/fastclaw/internal/provider"
)

type v2RunStatus string

const (
	v2RunRunning   v2RunStatus = "running"
	v2RunCompleted v2RunStatus = "completed"
	v2RunFailed    v2RunStatus = "failed"

	v2RunRecoveryWindow = 5 * time.Minute
)

type v2Run struct {
	ID              string       `json:"runId"`
	AgentID         string       `json:"agentId"`
	SessionID       string       `json:"sessionId"`
	NativeSessionID string       `json:"nativeSessionId"`
	Status          v2RunStatus  `json:"status"`
	Output          string       `json:"output,omitempty"`
	Error           string       `json:"error,omitempty"`
	Artifacts       []v2Artifact `json:"artifacts,omitempty"`
	CreatedAt       time.Time    `json:"createdAt"`
	CompletedAt     *time.Time   `json:"completedAt,omitempty"`
	UserID          string       `json:"-"`
}

type v2CreateRunRequest struct {
	Input       string              `json:"input"`
	SessionID   string              `json:"sessionId,omitempty"`
	User        string              `json:"user,omitempty"`
	Params      map[string]any      `json:"params,omitempty"`
	Images      []string            `json:"images,omitempty"`
	ImageURLs   []string            `json:"imageUrls,omitempty"`
	Attachments []attachmentRequest `json:"attachments,omitempty"`
}

var v2SessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
var streamHeartbeatInterval = apiStreamProgressInterval

var (
	v2GeneratedImageIntentPattern = regexp.MustCompile(`(?i)(?:\b(?:image|poster|cover|illustration|logo|photo|picture|banner|wallpaper|hero image|visual)\b|\b(?:draw|paint|sketch|illustrate|render)\s+|图片|海报|封面|插画|照片|配图|视觉图|宣传图|壁纸|(?:画|绘制|绘画)(?:一张|一幅|一个|一只|这张|该)?)`)
	v2StructuredVisualPattern     = regexp.MustCompile(`(?i)(?:\b(?:html|svg|markdown|md|pptx?|slides?|chart|graph|diagram|flowchart|architecture|dashboard|data visualization|table)\b|图表|流程图|架构图|数据可视化|仪表盘|表格|幻灯片)`)
	v2ImageAnalysisPattern        = regexp.MustCompile(`(?i)(?:\b(?:analy[sz]e|inspect|describe|summari[sz]e)\s+(?:this|the|an)?\s*image\b|分析(?:这张|该|这个)?图片|识别(?:这张|该|这个)?图片)`)
	v2MarkdownLinkPattern         = regexp.MustCompile(`(!?)\[([^\]]*)\]\(([^)\s]+)(?:\s+["'][^"']*["'])?\)`)
	v2LocalArtifactRefPattern     = regexp.MustCompile(`(?i)(?:(?:sandbox|file):[^\s)\]}>,"']*|/(?:mnt/data|workspace)/[^\s)\]}>,"']+)`)
)

func requiresV2ImageGen(input string) bool {
	if strings.Contains(strings.ToLower(input), "draw conclusions") {
		return false
	}
	if v2ImageAnalysisPattern.MatchString(input) || v2StructuredVisualPattern.MatchString(input) {
		return false
	}
	return v2GeneratedImageIntentPattern.MatchString(input)
}

func v2AgentHasTool(ag *agent.Agent, name string) bool {
	for _, tool := range ag.RegisteredTools() {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func validateV2GeneratedImageResult(toolSucceeded bool, artifacts []v2Artifact) error {
	if !toolSucceeded {
		return errors.New("image_gen did not complete successfully")
	}
	for _, artifact := range artifacts {
		if strings.HasPrefix(strings.ToLower(artifact.ContentType), "image/") {
			return nil
		}
	}
	return errors.New("image_gen produced no image artifact")
}

func isLocalOnlyV2ArtifactRef(ref string) bool {
	value := strings.ToLower(strings.TrimSpace(ref))
	return strings.HasPrefix(value, "sandbox:") ||
		strings.HasPrefix(value, "file:") ||
		strings.HasPrefix(value, "data:") ||
		strings.HasPrefix(value, "/mnt/data/") ||
		strings.HasPrefix(value, "/workspace/") ||
		strings.HasPrefix(value, "workspace/")
}

func sanitizeV2Message(content string) string {
	sanitized := v2MarkdownLinkPattern.ReplaceAllStringFunc(content, func(link string) string {
		match := v2MarkdownLinkPattern.FindStringSubmatch(link)
		if len(match) != 4 || !isLocalOnlyV2ArtifactRef(match[3]) {
			return link
		}
		if match[1] == "!" {
			return ""
		}
		return match[2]
	})
	sanitized = v2LocalArtifactRefPattern.ReplaceAllStringFunc(sanitized, func(ref string) string {
		value := ref
		if separator := strings.IndexByte(value, ':'); separator >= 0 {
			value = value[separator+1:]
		}
		value = strings.TrimRight(value, ".,;:")
		value = strings.TrimSpace(value)
		if value == "" {
			return ""
		}
		return path.Base(value)
	})
	for strings.Contains(sanitized, "\n\n\n") {
		sanitized = strings.ReplaceAll(sanitized, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(sanitized)
}

func isSuccessfulV2ImageGenEvent(event agent.ChatEvent) bool {
	if event.Type != "tool_result" {
		return false
	}
	name, _ := event.Data["name"].(string)
	if name != "image_gen" {
		return false
	}
	if success, exists := event.Data["success"].(bool); exists && !success {
		return false
	}
	return true
}

// HandleV2CreateRun starts a first-party Agent run and streams named SSE
// lifecycle events. It reuses Agent.HandleMessageStream, so model/tool/session
// behavior remains owned by the existing runtime instead of being forked into
// an API-specific implementation.
func (s *Server) HandleV2CreateRun(w http.ResponseWriter, r *http.Request) {
	var req v2CreateRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeV2Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Input = strings.TrimSpace(req.Input)
	if req.Input == "" {
		writeV2Error(w, http.StatusBadRequest, "input is required")
		return
	}

	if req.User != "" && s.authResolver != nil {
		if ident, ok := auth.FromContext(r.Context()); ok {
			next, err := s.authResolver.SwitchToAppUser(r.Context(), ident, req.User)
			if err != nil {
				writeV2Error(w, http.StatusBadRequest, err.Error())
				return
			}
			r = r.WithContext(auth.WithIdentity(r.Context(), next))
		}
	}

	agentID := r.PathValue("agentId")
	ag, err := s.v2AgentForRequest(r, agentID)
	if err != nil {
		writeV2AgentError(w, err)
		return
	}
	imageGenRequired := requiresV2ImageGen(req.Input)
	if imageGenRequired && !v2AgentHasTool(ag, "image_gen") {
		writeV2Error(w, http.StatusUnprocessableEntity, "image_gen is required but unavailable")
		return
	}

	runID, err := newV2ID("run")
	if err != nil {
		writeV2Error(w, http.StatusInternalServerError, "failed to create run id")
		return
	}
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID, err = newV2ID("session")
		if err != nil {
			writeV2Error(w, http.StatusInternalServerError, "failed to create session id")
			return
		}
	}
	if !v2SessionIDPattern.MatchString(sessionID) {
		writeV2Error(w, http.StatusBadRequest, "invalid sessionId")
		return
	}

	userID, ok := v2RequestUserID(r)
	if !ok {
		writeV2Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	workspaceSessionID := v2WorkspaceSessionID(userID, sessionID)

	// sessionId is the public V2 conversation identifier. Internally the
	// runtime and workspace use a user-namespaced scope so two end users of
	// the same public Agent cannot collide or read each other's artifacts.
	nativeSessionID := ag.NativeSessionKey(bus.InboundMessage{
		Channel:  "api-v2",
		ChatID:   workspaceSessionID,
		UserID:   userID,
		PeerKind: "dm",
		AgentID:  ag.Name(),
	})
	if nativeSessionID == "" {
		writeV2Error(w, http.StatusInternalServerError, "failed to create session")
		return
	}
	run := v2Run{
		ID:              runID,
		AgentID:         ag.Name(),
		SessionID:       sessionID,
		NativeSessionID: nativeSessionID,
		Status:          v2RunRunning,
		CreatedAt:       time.Now().UTC(),
		UserID:          userID,
	}
	s.storeV2Run(run)

	attachments := make([]agent.Attachment, 0, len(req.Images)+len(req.ImageURLs)+len(req.Attachments))
	for _, imageURL := range req.Images {
		attachments = append(attachments, agent.Attachment{URL: imageURL})
	}
	for _, imageURL := range req.ImageURLs {
		attachments = append(attachments, agent.Attachment{URL: imageURL})
	}
	for _, attachment := range req.Attachments {
		attachments = append(attachments, agent.Attachment{URL: attachment.URL, Name: attachment.Name})
	}
	attachmentPaths := ag.WriteSessionAttachments(r.Context(), workspaceSessionID, "", attachments)
	input := req.Input
	if len(attachmentPaths) > 0 {
		var prefix strings.Builder
		for _, path := range attachmentPaths {
			fmt.Fprintf(&prefix, "[Attached: /workspace/%s]\n", path)
		}
		prefix.WriteString(input)
		input = prefix.String()
	}
	// Input attachments belong to the request, not the Agent's output. Take
	// the baseline after materializing them so only files created or changed
	// by the run produce artifact.created events.
	before, err := s.listV2Workspace(r.Context(), ag.Name(), workspaceSessionID)
	if err != nil {
		s.failV2Run(runID, err)
		writeV2Error(w, http.StatusInternalServerError, "failed to inspect session files")
		return
	}

	msg := bus.InboundMessage{
		Channel:   "api-v2",
		ChatID:    workspaceSessionID,
		UserID:    userID,
		Text:      input,
		PeerKind:  "dm",
		AgentID:   ag.Name(),
		Params:    req.Params,
		PhotoURLs: append(append([]string(nil), req.Images...), req.ImageURLs...),
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	flusher, canFlush := w.(http.Flusher)

	writeV2SSE(w, "run.created", map[string]any{
		"runId": runID, "agentId": ag.Name(), "sessionId": sessionID,
		"nativeSessionId": nativeSessionID, "status": v2RunRunning,
	})
	if canFlush {
		flusher.Flush()
	}

	agentEvents := make(chan agent.ChatEvent, 256)
	turnCtx, cancelTurn := context.WithTimeout(context.WithoutCancel(r.Context()), apiAgentTurnTimeout)
	defer cancelTurn()
	agentCtx := agent.ContextWithChatEvents(turnCtx, agentEvents)
	streamReady := make(chan *provider.StreamReader, 1)
	go func() {
		streamReady <- ag.HandleMessageStream(agentCtx, msg)
	}()

	heartbeat := time.NewTicker(streamHeartbeatInterval)
	defer heartbeat.Stop()
	var stream *provider.StreamReader
	imageGenSucceeded := false
	clientGone := false
	reqDone := r.Context().Done()
	for stream == nil {
		select {
		case <-reqDone:
			clientGone = true
			reqDone = nil
		case <-heartbeat.C:
			if !clientGone {
				if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
					clientGone = true
					break
				}
				if canFlush {
					flusher.Flush()
				}
			}
		case event := <-agentEvents:
			if isSuccessfulV2ImageGenEvent(event) {
				imageGenSucceeded = true
			}
			if !clientGone && forwardV2AgentEvent(w, runID, event) && canFlush {
				flusher.Flush()
			}
		case stream = <-streamReady:
		}
	}
	for {
		select {
		case event := <-agentEvents:
			if isSuccessfulV2ImageGenEvent(event) {
				imageGenSucceeded = true
			}
			if !clientGone {
				forwardV2AgentEvent(w, runID, event)
			}
		default:
			goto eventsDrained
		}
	}

eventsDrained:
	if !clientGone && canFlush {
		flusher.Flush()
	}

	chunks := make(chan provider.StreamChunk, 1)
	go func() {
		defer close(chunks)
		for {
			chunk, more := stream.Next()
			if !more {
				return
			}
			select {
			case chunks <- chunk:
			case <-turnCtx.Done():
				return
			}
			if chunk.Done {
				return
			}
		}
	}()

	var output strings.Builder
streamChunks:
	for {
		select {
		case <-reqDone:
			clientGone = true
			reqDone = nil
		case <-heartbeat.C:
			if !clientGone {
				if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
					clientGone = true
					break
				}
				if canFlush {
					flusher.Flush()
				}
			}
		case chunk, more := <-chunks:
			if !more {
				break streamChunks
			}
			if chunk.Content != "" {
				output.WriteString(chunk.Content)
				if !clientGone {
					writeV2SSE(w, "message.delta", map[string]any{
						"runId": runID,
						"delta": chunk.Content,
					})
					if canFlush {
						flusher.Flush()
					}
				}
			}
			if chunk.Done {
				break streamChunks
			}
		}
	}
	if err := stream.Err(); err != nil {
		s.failV2Run(runID, err)
		if !clientGone {
			writeV2SSE(w, "run.failed", map[string]any{"runId": runID, "error": err.Error()})
			if canFlush {
				flusher.Flush()
			}
		}
		return
	}

	after, err := s.listV2Workspace(turnCtx, ag.Name(), workspaceSessionID)
	if err != nil {
		s.failV2Run(runID, err)
		if !clientGone {
			writeV2SSE(w, "run.failed", map[string]any{"runId": runID, "error": "failed to inspect session files"})
			if canFlush {
				flusher.Flush()
			}
		}
		return
	}
	artifacts := diffV2Artifacts(ag.Name(), sessionID, before, after)
	if imageGenRequired {
		if err := validateV2GeneratedImageResult(imageGenSucceeded, artifacts); err != nil {
			s.failV2Run(runID, err)
			if !clientGone {
				writeV2SSE(w, "run.failed", map[string]any{"runId": runID, "error": err.Error()})
				if canFlush {
					flusher.Flush()
				}
			}
			return
		}
	}
	if !clientGone {
		for _, artifact := range artifacts {
			writeV2SSE(w, "artifact.created", artifact)
		}
	}

	completedAt := time.Now().UTC()
	run.Output = sanitizeV2Message(output.String())
	run.Status = v2RunCompleted
	run.Artifacts = artifacts
	run.CompletedAt = &completedAt
	s.storeV2Run(run)
	if !clientGone {
		writeV2SSE(w, "message.completed", map[string]any{
			"runId":   runID,
			"content": run.Output,
		})
		writeV2SSE(w, "run.completed", run)
		if canFlush {
			flusher.Flush()
		}
	}
}

// HandleV2GetRun returns the latest state retained by this API process. The
// endpoint is primarily a short recovery window for clients reconnecting to a
// recently-finished stream; durable conversation history remains in sessions.
func (s *Server) HandleV2GetRun(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("agentId")
	if _, err := s.v2AgentForRequest(r, agentID); err != nil {
		writeV2AgentError(w, err)
		return
	}
	run, ok := s.loadV2Run(r.PathValue("runId"))
	if !ok || run.AgentID != agentID {
		writeV2Error(w, http.StatusNotFound, "run not found")
		return
	}
	if ident, exists := auth.FromContext(r.Context()); exists && run.UserID != "" && run.UserID != ident.EffectiveUserID() {
		writeV2Error(w, http.StatusNotFound, "run not found")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) v2AgentForRequest(r *http.Request, agentID string) (*agent.Agent, error) {
	if agentID == "" {
		return nil, errV2AgentNotFound
	}
	space, err := s.userSpaceFor(r)
	if err != nil {
		return nil, errV2Unauthorized
	}
	ag := space.Agents.AgentByID(agentID)
	if ag == nil {
		if ident, ok := auth.FromContext(r.Context()); ok {
			if injector, canInject := s.resolver.(AgentInjector); canInject && ident.AuthMethod == "apikey" && ident.CanAccessAgent(agentID) {
				if err := injector.EnsureAgent(r.Context(), space.UserID, agentID); err == nil {
					ag = space.Agents.AgentByID(agentID)
				}
			}
		}
	}
	if ag == nil || ag.Name() != agentID {
		return nil, errV2AgentNotFound
	}
	if ident, ok := auth.FromContext(r.Context()); ok && !ident.CanAccessAgent(agentID) {
		return nil, errV2AgentNotFound
	}
	return ag, nil
}

func v2RequestUserID(r *http.Request) (string, bool) {
	ident, ok := auth.FromContext(r.Context())
	if !ok || ident.EffectiveUserID() == "" {
		return "", false
	}
	return ident.EffectiveUserID(), true
}

var (
	errV2Unauthorized  = errors.New("unauthorized")
	errV2AgentNotFound = errors.New("agent not found")
)

func writeV2AgentError(w http.ResponseWriter, err error) {
	if errors.Is(err, errV2Unauthorized) {
		writeV2Error(w, http.StatusUnauthorized, err.Error())
		return
	}
	writeV2Error(w, http.StatusNotFound, "agent not found")
}

func writeV2Error(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{"message": message},
	})
}

func writeV2SSE(w io.Writer, event string, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload)
}

func forwardV2AgentEvent(w io.Writer, runID string, event agent.ChatEvent) bool {
	var eventName string
	switch event.Type {
	case "tool_call":
		eventName = "tool.started"
	case "tool_result":
		eventName = "tool.completed"
	default:
		return false
	}
	data := make(map[string]any, len(event.Data)+1)
	data["runId"] = runID
	for key, value := range event.Data {
		data[key] = value
	}
	writeV2SSE(w, eventName, data)
	return true
}

func newV2ID(prefix string) (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(raw[:]), nil
}

func (s *Server) storeV2Run(run v2Run) {
	s.v2RunsMu.Lock()
	defer s.v2RunsMu.Unlock()
	if s.v2Runs == nil {
		s.v2Runs = make(map[string]v2Run)
	}
	cutoff := time.Now().UTC().Add(-v2RunRecoveryWindow)
	for id, existing := range s.v2Runs {
		if existing.CreatedAt.Before(cutoff) {
			delete(s.v2Runs, id)
		}
	}
	if run.CreatedAt.Before(cutoff) {
		return
	}
	s.v2Runs[run.ID] = run
}

func (s *Server) loadV2Run(runID string) (v2Run, bool) {
	s.v2RunsMu.Lock()
	defer s.v2RunsMu.Unlock()
	run, ok := s.v2Runs[runID]
	if ok && run.CreatedAt.Before(time.Now().UTC().Add(-v2RunRecoveryWindow)) {
		delete(s.v2Runs, runID)
		return v2Run{}, false
	}
	return run, ok
}

func (s *Server) failV2Run(runID string, err error) {
	run, ok := s.loadV2Run(runID)
	if !ok {
		return
	}
	completedAt := time.Now().UTC()
	run.Status = v2RunFailed
	run.Error = err.Error()
	run.CompletedAt = &completedAt
	s.storeV2Run(run)
}
