package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSSHHostCreateMasksSecret(t *testing.T) {
	ctx := context.Background()
	t.Setenv("FASTCLAW_HOME", t.TempDir())
	s, resolver, admin, other := newAuthTestServer(t, ctx)

	body, _ := json.Marshal(map[string]any{
		"name":     "gpu-box",
		"host":     "10.0.4.21",
		"username": "deploy",
		"authType": "password",
		"password": "super-secret-pass",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/ssh-hosts", bytes.NewReader(body))
	cookie, err := resolver.IssueSession(ctx, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	s.authMiddleware(s.handleCreateSSHHost)(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "super-secret-pass") {
		t.Fatal("password leaked in create response")
	}

	listReq := authTestRequest(t, ctx, resolver, http.MethodGet, "/api/ssh-hosts", admin.ID)
	listRR := httptest.NewRecorder()
	s.authMiddleware(s.handleListSSHHosts)(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("list status=%d", listRR.Code)
	}
	if strings.Contains(listRR.Body.String(), "super-secret-pass") {
		t.Fatal("password leaked in list response")
	}
	var listed struct {
		Hosts []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			HasSecret bool   `json:"hasSecret"`
		} `json:"hosts"`
	}
	if err := json.Unmarshal(listRR.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Hosts) != 1 || listed.Hosts[0].Name != "gpu-box" || !listed.Hosts[0].HasSecret {
		t.Fatalf("listed %+v", listed.Hosts)
	}

	// Another user must not see or mutate this host.
	otherReq := authTestRequest(t, ctx, resolver, http.MethodGet, "/api/ssh-hosts", other.ID)
	otherRR := httptest.NewRecorder()
	s.authMiddleware(s.handleListSSHHosts)(otherRR, otherReq)
	var otherListed struct {
		Hosts []any `json:"hosts"`
	}
	_ = json.Unmarshal(otherRR.Body.Bytes(), &otherListed)
	if len(otherListed.Hosts) != 0 {
		t.Fatalf("other user saw hosts: %+v", otherListed.Hosts)
	}

	del := authTestRequest(t, ctx, resolver, http.MethodDelete, "/api/ssh-hosts/"+listed.Hosts[0].ID, other.ID)
	del.SetPathValue("id", listed.Hosts[0].ID)
	delRR := httptest.NewRecorder()
	s.authMiddleware(s.handleDeleteSSHHost)(delRR, del)
	if delRR.Code != http.StatusNotFound {
		t.Fatalf("other user delete status=%d body=%s", delRR.Code, delRR.Body.String())
	}
}

func TestSSHHostCreateRequiresCredential(t *testing.T) {
	ctx := context.Background()
	t.Setenv("FASTCLAW_HOME", t.TempDir())
	s, resolver, admin, _ := newAuthTestServer(t, ctx)

	body, _ := json.Marshal(map[string]any{
		"name": "x", "host": "h", "username": "u", "authType": "password",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/ssh-hosts", bytes.NewReader(body))
	cookie, _ := resolver.IssueSession(ctx, admin.ID)
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	s.authMiddleware(s.handleCreateSSHHost)(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}
