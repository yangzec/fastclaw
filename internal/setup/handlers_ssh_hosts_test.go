package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/sshhosts"
	"github.com/fastclaw-ai/fastclaw/internal/store"
)

func stubSSHHostProbe(t *testing.T, fn func(ctx context.Context, host store.SSHHostRecord, creds sshhosts.Creds, command string, timeout time.Duration) (sshhosts.Result, error)) {
	t.Helper()
	orig := sshHostProbe
	sshHostProbe = fn
	t.Cleanup(func() { sshHostProbe = orig })
}

func TestSSHHostCreateMasksSecret(t *testing.T) {
	ctx := context.Background()
	t.Setenv("FASTCLAW_HOME", t.TempDir())
	s, resolver, admin, other := newAuthTestServer(t, ctx)
	stubSSHHostProbe(t, func(ctx context.Context, host store.SSHHostRecord, creds sshhosts.Creds, command string, timeout time.Duration) (sshhosts.Result, error) {
		if creds.Password != "super-secret-pass" {
			t.Fatalf("probe saw password %q", creds.Password)
		}
		return sshhosts.Result{PinnedHostKey: "ssh-ed25519 AAAA"}, nil
	})

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
			ID             string  `json:"id"`
			Name           string  `json:"name"`
			HasSecret      bool    `json:"hasSecret"`
			LastTestStatus string  `json:"lastTestStatus"`
			LastTestedAt   *string `json:"lastTestedAt"`
		} `json:"hosts"`
	}
	if err := json.Unmarshal(listRR.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Hosts) != 1 || listed.Hosts[0].Name != "gpu-box" || !listed.Hosts[0].HasSecret {
		t.Fatalf("listed %+v", listed.Hosts)
	}
	if listed.Hosts[0].LastTestStatus != store.SSHTestOK || listed.Hosts[0].LastTestedAt == nil {
		t.Fatalf("expected recorded ok status, got %+v", listed.Hosts[0])
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

func TestSSHHostCreateRefusesWhenProbeFails(t *testing.T) {
	ctx := context.Background()
	t.Setenv("FASTCLAW_HOME", t.TempDir())
	s, resolver, admin, _ := newAuthTestServer(t, ctx)
	stubSSHHostProbe(t, func(ctx context.Context, host store.SSHHostRecord, creds sshhosts.Creds, command string, timeout time.Duration) (sshhosts.Result, error) {
		return sshhosts.Result{}, errors.New("dial 10.0.4.21:22: connection refused")
	})

	body, _ := json.Marshal(map[string]any{
		"name": "gpu-box", "host": "10.0.4.21", "username": "deploy",
		"authType": "password", "password": "secret",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/ssh-hosts", bytes.NewReader(body))
	cookie, _ := resolver.IssueSession(ctx, admin.ID)
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	s.authMiddleware(s.handleCreateSSHHost)(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "connection failed") {
		t.Fatalf("expected connection failed error, got %s", rr.Body.String())
	}

	listReq := authTestRequest(t, ctx, resolver, http.MethodGet, "/api/ssh-hosts", admin.ID)
	listRR := httptest.NewRecorder()
	s.authMiddleware(s.handleListSSHHosts)(listRR, listReq)
	var listed struct {
		Hosts []any `json:"hosts"`
	}
	_ = json.Unmarshal(listRR.Body.Bytes(), &listed)
	if len(listed.Hosts) != 0 {
		t.Fatalf("failed probe still saved a host: %+v", listed.Hosts)
	}
}

func TestSSHHostUpdateSkipsProbeWhenOnlyAliasChanges(t *testing.T) {
	ctx := context.Background()
	t.Setenv("FASTCLAW_HOME", t.TempDir())
	s, resolver, admin, _ := newAuthTestServer(t, ctx)
	probes := 0
	stubSSHHostProbe(t, func(ctx context.Context, host store.SSHHostRecord, creds sshhosts.Creds, command string, timeout time.Duration) (sshhosts.Result, error) {
		probes++
		return sshhosts.Result{}, nil
	})

	createBody, _ := json.Marshal(map[string]any{
		"name": "gpu-box", "host": "10.0.4.21", "username": "deploy",
		"authType": "password", "password": "secret",
	})
	create := httptest.NewRequest(http.MethodPost, "/api/ssh-hosts", bytes.NewReader(createBody))
	cookie, _ := resolver.IssueSession(ctx, admin.ID)
	create.AddCookie(cookie)
	createRR := httptest.NewRecorder()
	s.authMiddleware(s.handleCreateSSHHost)(createRR, create)
	if createRR.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}
	var created struct {
		Host struct {
			ID string `json:"id"`
		} `json:"host"`
	}
	if err := json.Unmarshal(createRR.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	updateBody, _ := json.Marshal(map[string]any{"name": "gpu-box-2"})
	upd := httptest.NewRequest(http.MethodPut, "/api/ssh-hosts/"+created.Host.ID, bytes.NewReader(updateBody))
	upd.AddCookie(cookie)
	upd.SetPathValue("id", created.Host.ID)
	updRR := httptest.NewRecorder()
	s.authMiddleware(s.handleUpdateSSHHost)(updRR, upd)
	if updRR.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updRR.Code, updRR.Body.String())
	}
	if probes != 1 {
		t.Fatalf("alias-only update should not re-probe, probes=%d", probes)
	}
}

func TestSSHHostTestRecordsFailure(t *testing.T) {
	ctx := context.Background()
	t.Setenv("FASTCLAW_HOME", t.TempDir())
	s, resolver, admin, _ := newAuthTestServer(t, ctx)
	calls := 0
	stubSSHHostProbe(t, func(ctx context.Context, host store.SSHHostRecord, creds sshhosts.Creds, command string, timeout time.Duration) (sshhosts.Result, error) {
		calls++
		if calls == 1 {
			return sshhosts.Result{}, nil
		}
		return sshhosts.Result{}, errors.New("handshake timeout")
	})

	createBody, _ := json.Marshal(map[string]any{
		"name": "gpu-box", "host": "10.0.4.21", "username": "deploy",
		"authType": "password", "password": "secret",
	})
	create := httptest.NewRequest(http.MethodPost, "/api/ssh-hosts", bytes.NewReader(createBody))
	cookie, _ := resolver.IssueSession(ctx, admin.ID)
	create.AddCookie(cookie)
	createRR := httptest.NewRecorder()
	s.authMiddleware(s.handleCreateSSHHost)(createRR, create)
	if createRR.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}
	var created struct {
		Host struct {
			ID string `json:"id"`
		} `json:"host"`
	}
	if err := json.Unmarshal(createRR.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	testReq := authTestRequest(t, ctx, resolver, http.MethodPost, "/api/ssh-hosts/"+created.Host.ID+"/test", admin.ID)
	testReq.SetPathValue("id", created.Host.ID)
	testRR := httptest.NewRecorder()
	s.authMiddleware(s.handleTestSSHHost)(testRR, testReq)
	if testRR.Code != http.StatusBadGateway {
		t.Fatalf("test status=%d body=%s", testRR.Code, testRR.Body.String())
	}

	got, err := s.dataStore.GetSSHHost(ctx, created.Host.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastTestStatus != store.SSHTestFail || !strings.Contains(got.LastTestError, "handshake timeout") {
		t.Fatalf("expected recorded fail, got %+v", got)
	}
}
