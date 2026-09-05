package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/auth"
)

func TestResolveAPIChatter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		header  string
		params  map[string]any
		want    string
		wantErr string
	}{
		{name: "neither", want: "api-user"},
		{name: "empty header and empty params", header: "  ", params: map[string]any{"user_id": "  "}, want: "api-user"},
		{name: "header only", header: " app:u_1 ", want: "app:u_1"},
		{name: "params only", params: map[string]any{"user_id": "app:u_2"}, want: "app:u_2"},
		{name: "both match", header: "app:u_3", params: map[string]any{"user_id": "app:u_3"}, want: "app:u_3"},
		{name: "both match after trim", header: " app:u_3 ", params: map[string]any{"user_id": "app:u_3"}, want: "app:u_3"},
		{name: "mismatch", header: "app:u_a", params: map[string]any{"user_id": "app:u_b"}, wantErr: "do not match"},
		{name: "params not a string", params: map[string]any{"user_id": 123}, wantErr: "must be a string"},
		{name: "body user field is ignored", params: map[string]any{"display_name": "Ada"}, want: "api-user"},
		{name: "nil user_id treated as absent", params: map[string]any{"user_id": nil}, want: "api-user"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveAPIChatter(tc.header, tc.params)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestChatCompletionChatterMismatchIs400(t *testing.T) {
	srv := newStreamTestServer(t, instantChatProvider{})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(
		`{"agent_id":"agent-1","messages":[{"role":"user","content":"hi"}],"params":{"user_id":"app:u_b"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(ChatterHeader, "app:u_a")
	req.Header.Set("X-Fastclaw-Session-Key", "app:u_a:c1")
	req = req.WithContext(auth.WithIdentity(req.Context(), auth.Identity{UserID: "user-1", AuthMethod: "session"}))

	rr := httptest.NewRecorder()
	srv.HandleChatCompletions(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	errObj, _ := body["error"].(map[string]any)
	msg, _ := errObj["message"].(string)
	if !strings.Contains(msg, "do not match") {
		t.Fatalf("error.message = %q", msg)
	}
}

func TestChatCompletionChatterFromHeaderReachesSession(t *testing.T) {
	srv := newStreamTestServer(t, instantChatProvider{})
	const callerKey = "app:u_9:c1"
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(
		`{"agent_id":"agent-1","messages":[{"role":"user","content":"hi"}],"params":{"display_name":"Ada"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(ChatterHeader, "app:u_9")
	req.Header.Set("X-Fastclaw-Session-Key", callerKey)
	req = req.WithContext(auth.WithIdentity(req.Context(), auth.Identity{UserID: "user-1", AuthMethod: "session"}))

	rr := httptest.NewRecorder()
	srv.HandleChatCompletions(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}

	space, err := srv.userSpaceFor(req)
	if err != nil {
		t.Fatalf("userSpaceFor: %v", err)
	}
	ag := space.Agents.AgentByID("agent-1")
	if ag == nil {
		t.Fatal("agent-1 missing")
	}
	sess := ag.Sessions().Get("api", "", callerKey, "")
	if got := sess.ChatterUserID(); got != "app:u_9" {
		t.Fatalf("session chatter = %q, want app:u_9", got)
	}
}

func TestChatCompletionBodyUserDoesNotSetChatter(t *testing.T) {
	srv := newStreamTestServer(t, instantChatProvider{})
	const callerKey = "app:ignored:c1"
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(
		`{"agent_id":"agent-1","user":"should-not-be-chatter","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Fastclaw-Session-Key", callerKey)
	req = req.WithContext(auth.WithIdentity(req.Context(), auth.Identity{UserID: "user-1", AuthMethod: "session"}))

	rr := httptest.NewRecorder()
	srv.HandleChatCompletions(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	space, err := srv.userSpaceFor(req)
	if err != nil {
		t.Fatalf("userSpaceFor: %v", err)
	}
	sess := space.Agents.AgentByID("agent-1").Sessions().Get("api", "", callerKey, "")
	if got := sess.ChatterUserID(); got != "api-user" {
		t.Fatalf("session chatter = %q, want api-user (body user must not apply)", got)
	}
}

func TestChatCompletionChatterFromParams(t *testing.T) {
	srv := newStreamTestServer(t, instantChatProvider{})
	const callerKey = "app:u_p:c1"
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(
		`{"agent_id":"agent-1","messages":[{"role":"user","content":"hi"}],"params":{"user_id":"app:u_p"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Fastclaw-Session-Key", callerKey)
	req = req.WithContext(auth.WithIdentity(req.Context(), auth.Identity{UserID: "user-1", AuthMethod: "session"}))

	rr := httptest.NewRecorder()
	srv.HandleChatCompletions(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	space, err := srv.userSpaceFor(req)
	if err != nil {
		t.Fatalf("userSpaceFor: %v", err)
	}
	sess := space.Agents.AgentByID("agent-1").Sessions().Get("api", "", callerKey, "")
	if got := sess.ChatterUserID(); got != "app:u_p" {
		t.Fatalf("session chatter = %q, want app:u_p", got)
	}
}
