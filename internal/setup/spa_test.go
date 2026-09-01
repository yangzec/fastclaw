package setup

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestSPABareAgentURLServesAgentShell(t *testing.T) {
	h := spaHandler{fs: fstest.MapFS{
		"index.html":                     {Data: []byte("ROOT")},
		"overview/index.html":            {Data: []byte("OVERVIEW")},
		"agents/default/index.html":      {Data: []byte("AGENT_SHELL")},
		"agents/default/chat/index.html": {Data: []byte("AGENT_CHAT")},
	}}

	get := func(path string) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: status %d", path, rr.Code)
		}
		body, _ := io.ReadAll(rr.Body)
		return string(body)
	}

	if got := get("/agents/agt_abc123/"); got != "AGENT_SHELL" {
		t.Fatalf("bare agent URL served %q, want AGENT_SHELL (not root/overview)", got)
	}
	if got := get("/agents/agt_abc123/chat/"); got != "AGENT_CHAT" {
		t.Fatalf("agent chat URL served %q, want AGENT_CHAT", got)
	}
	if got := get("/"); got != "ROOT" {
		t.Fatalf("root served %q, want ROOT", got)
	}
}
