package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/store"
	"github.com/fastclaw-ai/fastclaw/internal/workspace"
)

// write_file("/workspace/chart.svg") and write_file("chart.svg") store the
// same session object. Chat markdown used to rewrite /workspace/X as
// sessions/<url-session-key>/X, which 404s when the store key is
// sessions/<chat_id>/X. The file GET must resolve a bare name (and a
// leftover workspace/ prefix) via ?sessionId=.
func TestAgentFileGetResolvesWorkspacePathViaSession(t *testing.T) {
	ctx := context.Background()
	s, resolver, owner, _ := newAuthTestServer(t, ctx)

	const (
		agentID    = "agt_wsfile"
		sessionKey = "s-url-token"
		chatID     = "web-chat-1"
	)
	now := time.Now().UTC()
	if err := s.dataStore.SaveAgent(ctx, &store.AgentRecord{
		ID: agentID, UserID: owner.ID, Name: "wsfile agent",
		Config: map[string]any{}, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("save agent: %v", err)
	}
	if err := s.dataStore.SaveSession(ctx, owner.ID, agentID, sessionKey, &store.SessionRecord{
		Channel: "web", ChatID: chatID,
	}); err != nil {
		t.Fatalf("save session: %v", err)
	}

	ws := workspace.NewLocalFS(t.TempDir())
	s.SetWorkspaceStore(ws)
	const body = "<svg xmlns='http://www.w3.org/2000/svg'></svg>"
	if err := ws.Put(ctx, agentID, "", chatID, "chart.svg", strings.NewReader(body), int64(len(body)), "image/svg+xml"); err != nil {
		t.Fatalf("put: %v", err)
	}

	get := func(path, query string) *httptest.ResponseRecorder {
		t.Helper()
		req := authTestRequest(t, ctx, resolver, http.MethodGet, "/api/agents/"+agentID+"/files/"+path+query, owner.ID)
		req.SetPathValue("id", agentID)
		req.SetPathValue("path", path)
		rr := httptest.NewRecorder()
		s.authMiddleware(s.handleAgentFile)(rr, req)
		return rr
	}

	for _, tc := range []struct {
		name  string
		path  string
		query string
	}{
		{"bare name + sessionId", "chart.svg", "?sessionId=" + sessionKey},
		{"bare name + chat_id (API session key)", "chart.svg", "?sessionId=" + chatID},
		{"workspace prefix + sessionId", "workspace/chart.svg", "?sessionId=" + sessionKey},
		{"full store key", "sessions/" + chatID + "/chart.svg", ""},
	} {
		rr := get(tc.path, tc.query)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: GET = %d: %s", tc.name, rr.Code, rr.Body.String())
		}
		if got := rr.Body.String(); got != body {
			t.Fatalf("%s: body = %q, want %q", tc.name, got, body)
		}
	}

	// A token that is neither this caller's session_key nor chat_id
	// must 404 — do not treat an unknown string as a raw directory name.
	if rr := get("chart.svg", "?sessionId=someone-elses-chat"); rr.Code != http.StatusNotFound {
		t.Fatalf("foreign sessionId = %d, want 404", rr.Code)
	}

	list := func(query string) []string {
		t.Helper()
		req := authTestRequest(t, ctx, resolver, http.MethodGet, "/api/agents/"+agentID+"/files"+query, owner.ID)
		req.SetPathValue("id", agentID)
		rr := httptest.NewRecorder()
		s.authMiddleware(s.handleAgentFileList)(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("LIST %s = %d: %s", query, rr.Code, rr.Body.String())
		}
		var body struct {
			Files []struct {
				Path string `json:"path"`
			} `json:"files"`
		}
		if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
			t.Fatalf("LIST decode %s: %v", query, err)
		}
		out := make([]string, 0, len(body.Files))
		for _, f := range body.Files {
			out = append(out, f.Path)
		}
		return out
	}
	wantPath := "sessions/" + chatID + "/chart.svg"
	for _, q := range []string{"?sessionId=" + sessionKey, "?sessionId=" + chatID} {
		got := list(q)
		if len(got) != 1 || got[0] != wantPath {
			t.Fatalf("LIST %s = %v, want [%s]", q, got, wantPath)
		}
	}
	if got := list("?sessionId=someone-elses-chat"); len(got) != 0 {
		t.Fatalf("LIST foreign sessionId = %v, want empty", got)
	}
}

// The composer uploads attachments with a minted sessionId before the
// first /api/chat creates the session row. Those bytes must land under
// sessions/<minted-id>/ so list + GET + the agent's read_file agree.
func TestAgentFileUploadLandsInPendingSession(t *testing.T) {
	ctx := context.Background()
	s, resolver, owner, viewer := newAuthTestServer(t, ctx)

	const (
		agentID   = "agt_pending_upload"
		sessionID = "s-not-yet-in-db"
		payload   = "attach-token-xyz\n"
	)
	now := time.Now().UTC()
	if err := s.dataStore.SaveAgent(ctx, &store.AgentRecord{
		ID: agentID, UserID: owner.ID, Name: "pending upload",
		IsPublic: true, Config: map[string]any{}, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("save agent: %v", err)
	}
	ws := workspace.NewLocalFS(t.TempDir())
	s.SetWorkspaceStore(ws)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "secret-attach.txt")
	if err != nil {
		t.Fatalf("form file: %v", err)
	}
	if _, err := fw.Write([]byte(payload)); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}

	req := authTestRequest(t, ctx, resolver, http.MethodPost,
		"/api/agents/"+agentID+"/files?sessionId="+sessionID, owner.ID)
	req.SetPathValue("id", agentID)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Body = io.NopCloser(bytes.NewReader(body.Bytes()))
	req.ContentLength = int64(body.Len())
	rr := httptest.NewRecorder()
	s.authMiddleware(s.handleAgentFileUpload)(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("upload = %d: %s", rr.Code, rr.Body.String())
	}

	listReq := authTestRequest(t, ctx, resolver, http.MethodGet,
		"/api/agents/"+agentID+"/files?sessionId="+sessionID, owner.ID)
	listReq.SetPathValue("id", agentID)
	listRR := httptest.NewRecorder()
	s.authMiddleware(s.handleAgentFileList)(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("list = %d: %s", listRR.Code, listRR.Body.String())
	}
	var listed struct {
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.NewDecoder(listRR.Body).Decode(&listed); err != nil {
		t.Fatalf("list decode: %v", err)
	}
	want := "sessions/" + sessionID + "/secret-attach.txt"
	if len(listed.Files) != 1 || listed.Files[0].Path != want {
		t.Fatalf("list = %+v, want [%s]", listed.Files, want)
	}

	getReq := authTestRequest(t, ctx, resolver, http.MethodGet,
		"/api/agents/"+agentID+"/files/secret-attach.txt?sessionId="+sessionID, owner.ID)
	getReq.SetPathValue("id", agentID)
	getReq.SetPathValue("path", "secret-attach.txt")
	getRR := httptest.NewRecorder()
	s.authMiddleware(s.handleAgentFile)(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("get = %d: %s", getRR.Code, getRR.Body.String())
	}
	if got := getRR.Body.String(); got != payload {
		t.Fatalf("get body = %q, want %q", got, payload)
	}

	// A public-agent viewer who guesses the minted id must not list or
	// GET those bytes. pendingOwnerSessionID is owner-only.
	viewList := authTestRequest(t, ctx, resolver, http.MethodGet,
		"/api/agents/"+agentID+"/files?sessionId="+sessionID, viewer.ID)
	viewList.SetPathValue("id", agentID)
	viewListRR := httptest.NewRecorder()
	s.authMiddleware(s.handleAgentFileList)(viewListRR, viewList)
	if viewListRR.Code != http.StatusOK {
		t.Fatalf("viewer list = %d: %s", viewListRR.Code, viewListRR.Body.String())
	}
	var viewerListed struct {
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.NewDecoder(viewListRR.Body).Decode(&viewerListed); err != nil {
		t.Fatalf("viewer list decode: %v", err)
	}
	if len(viewerListed.Files) != 0 {
		t.Fatalf("viewer list = %+v, want empty", viewerListed.Files)
	}

	viewGet := authTestRequest(t, ctx, resolver, http.MethodGet,
		"/api/agents/"+agentID+"/files/secret-attach.txt?sessionId="+sessionID, viewer.ID)
	viewGet.SetPathValue("id", agentID)
	viewGet.SetPathValue("path", "secret-attach.txt")
	viewGetRR := httptest.NewRecorder()
	s.authMiddleware(s.handleAgentFile)(viewGetRR, viewGet)
	if viewGetRR.Code != http.StatusNotFound {
		t.Fatalf("viewer get = %d, want 404: %s", viewGetRR.Code, viewGetRR.Body.String())
	}
}
