package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/auth"
	"github.com/fastclaw-ai/fastclaw/internal/config"
	"github.com/fastclaw-ai/fastclaw/internal/store"
	"github.com/fastclaw-ai/fastclaw/internal/users"
)

func TestAgentObjectStoreGetDoesNotLeakSecretsAndOwnerOnly(t *testing.T) {
	ctx := context.Background()
	s, _, _, owner := newAuthTestServer(t, ctx)
	other := &users.Account{ID: "other", Role: users.RoleUser}
	agentID := "agent-r2"
	if err := s.dataStore.SaveAgent(ctx, &store.AgentRecord{ID: agentID, UserID: owner.ID, Name: "r2"}); err != nil {
		t.Fatal(err)
	}
	cfg := config.ObjectStoreCfg{Type: "cloudflare-r2", AccountID: "acct"}
	cfg.S3.Bucket = "bucket"
	cfg.S3.AccessKey = "ak-secret"
	cfg.S3.SecretKey = "sk-secret"
	cfg.S3.UseSSL = true
	if err := s.dataStore.SaveConfig(ctx, &store.ConfigRecord{Kind: store.KindSetting, AgentID: agentID, Name: agentObjectStoreNamespace, Enabled: true, Data: objectStoreData(cfg)}); err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	req := agentReq(http.MethodGet, agentID, owner, nil)
	s.handleGetAgentObjectStore(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("owner GET status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if strings.Contains(body, "ak-secret") || strings.Contains(body, "sk-secret") {
		t.Fatalf("GET leaked secret material: %s", body)
	}
	if !strings.Contains(body, "hasAccessKey") || !strings.Contains(body, "hasSecretKey") {
		t.Fatalf("GET did not include key presence booleans: %s", body)
	}

	rr = httptest.NewRecorder()
	req = agentReq(http.MethodGet, agentID, other, nil)
	s.handleGetAgentObjectStore(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("non-owner GET status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestAgentObjectStorePutKeepsBlankSecretsAndTestFailureDoesNotSave(t *testing.T) {
	ctx := context.Background()
	s, _, _, owner := newAuthTestServer(t, ctx)
	agentID := "agent-r2"
	if err := s.dataStore.SaveAgent(ctx, &store.AgentRecord{ID: agentID, UserID: owner.ID, Name: "r2"}); err != nil {
		t.Fatal(err)
	}
	cfg := config.ObjectStoreCfg{Type: "cloudflare-r2", AccountID: "acct"}
	cfg.S3.Bucket = "old"
	cfg.S3.AccessKey = "old-ak"
	cfg.S3.SecretKey = "old-sk"
	cfg.S3.UseSSL = true
	if err := s.dataStore.SaveConfig(ctx, &store.ConfigRecord{Kind: store.KindSetting, AgentID: agentID, Name: agentObjectStoreNamespace, Enabled: true, Data: objectStoreData(cfg)}); err != nil {
		t.Fatal(err)
	}

	oldTest := testObjectStoreConnection
	defer func() { testObjectStoreConnection = oldTest }()
	testObjectStoreConnection = func(ctx context.Context, agentID string, cfg config.ObjectStoreCfg) (time.Duration, error) {
		if cfg.S3.AccessKey != "old-ak" || cfg.S3.SecretKey != "old-sk" {
			t.Fatalf("blank secrets were not preserved: %#v", cfg.S3)
		}
		return time.Millisecond, nil
	}
	body := `{"accountId":"acct","bucket":"new","accessKey":"","secretKey":""}`
	rr := httptest.NewRecorder()
	req := agentReq(http.MethodPut, agentID, owner, bytes.NewBufferString(body))
	s.handlePutAgentObjectStore(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", rr.Code, rr.Body.String())
	}
	got, ok, err := s.loadAgentObjectStore(ctx, agentID)
	if err != nil || !ok {
		t.Fatalf("load saved: %v ok=%v", err, ok)
	}
	if got.S3.Bucket != "new" || got.S3.AccessKey != "old-ak" || got.S3.SecretKey != "old-sk" {
		t.Fatalf("saved config mismatch: %#v", got.S3)
	}

	testObjectStoreConnection = func(context.Context, string, config.ObjectStoreCfg) (time.Duration, error) {
		return 0, fmt.Errorf("boom")
	}
	body = `{"accountId":"acct","bucket":"bad","accessKey":"ak","secretKey":"sk"}`
	rr = httptest.NewRecorder()
	req = agentReq(http.MethodPut, agentID, owner, bytes.NewBufferString(body))
	s.handlePutAgentObjectStore(rr, req)
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("failed test PUT status=%d body=%s", rr.Code, rr.Body.String())
	}
	got, _, _ = s.loadAgentObjectStore(ctx, agentID)
	if got.S3.Bucket != "new" {
		b, _ := json.Marshal(got)
		t.Fatalf("failed test saved config: %s", b)
	}
}

func TestUserObjectStoreSecretsBlankKeysAndAuthorization(t *testing.T) {
	ctx := context.Background()
	s, _, admin, owner := newAuthTestServer(t, ctx)
	oldTest := testObjectStoreConnection
	defer func() { testObjectStoreConnection = oldTest }()
	testObjectStoreConnection = func(ctx context.Context, agentID string, cfg config.ObjectStoreCfg) (time.Duration, error) {
		return time.Millisecond, nil
	}

	body := `{"accountId":"acct","bucket":"user-bucket","accessKey":"ak-secret","secretKey":"sk-secret"}`
	rr := httptest.NewRecorder()
	s.handlePutUserObjectStore(rr, userReq(http.MethodPut, owner, bytes.NewBufferString(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	s.handleGetUserObjectStore(rr, userReq(http.MethodGet, owner, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Body.String(); strings.Contains(got, "ak-secret") || strings.Contains(got, "sk-secret") || !strings.Contains(got, `"source":"user"`) {
		t.Fatalf("GET leaked keys or wrong source: %s", got)
	}

	testObjectStoreConnection = func(ctx context.Context, agentID string, cfg config.ObjectStoreCfg) (time.Duration, error) {
		if cfg.S3.AccessKey != "ak-secret" || cfg.S3.SecretKey != "sk-secret" {
			t.Fatalf("blank user keys were not preserved: %#v", cfg.S3)
		}
		return time.Millisecond, nil
	}
	body = `{"accountId":"acct","bucket":"updated","accessKey":"","secretKey":""}`
	rr = httptest.NewRecorder()
	s.handlePutUserObjectStore(rr, userReq(http.MethodPut, owner, bytes.NewBufferString(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("blank key PUT status=%d body=%s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	body = `{"accountId":"acct","bucket":"first-empty","accessKey":"","secretKey":""}`
	s.handlePutUserObjectStore(rr, userReq(http.MethodPut, admin, bytes.NewBufferString(body)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("first empty keys status=%d body=%s", rr.Code, rr.Body.String())
	}

	appUser := &users.Account{ID: "app", Role: users.RoleAppUser}
	rr = httptest.NewRecorder()
	s.handlePutUserObjectStore(rr, userReq(http.MethodPut, appUser, bytes.NewBufferString(`{}`)))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("app_user write status=%d body=%s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/me/objectstore", bytes.NewBufferString(`{}`))
	ident := auth.Identity{UserID: admin.ID, Role: users.RoleSuperAdmin, AuthMethod: "session", ActAsUserID: owner.ID}
	s.handlePutUserObjectStore(rr, req.WithContext(auth.WithIdentity(req.Context(), ident)))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("actAs write status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestUserObjectStoreProbeFailureDoesNotSaveAndDeleteFallsBack(t *testing.T) {
	ctx := context.Background()
	s, _, _, owner := newAuthTestServer(t, ctx)
	oldTest := testObjectStoreConnection
	defer func() { testObjectStoreConnection = oldTest }()
	testObjectStoreConnection = func(context.Context, string, config.ObjectStoreCfg) (time.Duration, error) {
		return 0, fmt.Errorf("probe failed")
	}
	body := `{"accountId":"acct","bucket":"bad","accessKey":"ak","secretKey":"sk"}`
	rr := httptest.NewRecorder()
	s.handlePutUserObjectStore(rr, userReq(http.MethodPut, owner, bytes.NewBufferString(body)))
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("failed probe status=%d body=%s", rr.Code, rr.Body.String())
	}
	if _, ok, _ := s.loadScopedObjectStore(ctx, owner.ID, ""); ok {
		t.Fatal("failed probe saved user objectstore")
	}

	testObjectStoreConnection = func(context.Context, string, config.ObjectStoreCfg) (time.Duration, error) {
		return time.Millisecond, nil
	}
	body = `{"accountId":"acct","bucket":"ok","accessKey":"ak","secretKey":"sk"}`
	rr = httptest.NewRecorder()
	s.handlePutUserObjectStore(rr, userReq(http.MethodPut, owner, bytes.NewBufferString(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("save status=%d body=%s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	s.handleDeleteUserObjectStore(rr, userReq(http.MethodDelete, owner, nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"source":"global"`) {
		t.Fatalf("delete status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestConfigObjectStoreBypassClosedForRegularUser(t *testing.T) {
	ctx := context.Background()
	s, _, admin, owner := newAuthTestServer(t, ctx)
	global := config.ObjectStoreCfg{Type: "cloudflare-r2", AccountID: "acct"}
	global.S3.Bucket = "global"
	global.S3.AccessKey = "global-ak"
	global.S3.SecretKey = "global-sk"
	global.S3.UseSSL = true
	if err := s.dataStore.SaveConfig(ctx, &store.ConfigRecord{Kind: store.KindSetting, Name: agentObjectStoreNamespace, Enabled: true, Data: objectStoreData(global)}); err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, userReq(http.MethodGet, owner, nil))
	if got := rr.Body.String(); strings.Contains(got, "global-ak") || strings.Contains(got, "global-sk") {
		t.Fatalf("/api/config leaked objectstore keys: %s", got)
	}

	rr = httptest.NewRecorder()
	s.handleUpdateConfig(rr, userReq(http.MethodPost, owner, bytes.NewBufferString(`{"objectStore":{"type":"cloudflare-r2"}}`)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("regular objectStore POST status=%d body=%s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	s.handleUpdateConfig(rr, userReq(http.MethodPost, owner, bytes.NewBufferString(`{"prefs":{"timezone":"Asia/Shanghai"}}`)))
	if rr.Code != http.StatusOK {
		t.Fatalf("regular prefs POST status=%d body=%s", rr.Code, rr.Body.String())
	}
	if _, ok, _ := s.loadScopedObjectStore(ctx, owner.ID, ""); ok {
		t.Fatal("regular config POST created user objectstore row")
	}

	rr = httptest.NewRecorder()
	s.handleUpdateConfig(rr, userReq(http.MethodPost, admin, bytes.NewBufferString(`{"objectStore":{"type":"cloudflare-r2","accountId":"acct2","s3":{"bucket":"sys","accessKey":"ak","secretKey":"sk"}}}`)))
	if rr.Code != http.StatusOK {
		t.Fatalf("admin objectStore POST status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func userReq(method string, acct *users.Account, body *bytes.Buffer) *http.Request {
	var rbody *bytes.Buffer
	if body == nil {
		rbody = bytes.NewBuffer(nil)
	} else {
		rbody = body
	}
	req := httptest.NewRequest(method, "/api/me/objectstore", rbody)
	ident := auth.Identity{UserID: acct.ID, Role: acct.Role, AuthMethod: "session"}
	return req.WithContext(auth.WithIdentity(req.Context(), ident))
}

func agentReq(method, agentID string, acct *users.Account, body *bytes.Buffer) *http.Request {
	var rbody *bytes.Buffer
	if body == nil {
		rbody = bytes.NewBuffer(nil)
	} else {
		rbody = body
	}
	req := httptest.NewRequest(method, "/api/agents/"+agentID+"/objectstore", rbody)
	req.SetPathValue("id", agentID)
	ident := auth.Identity{UserID: acct.ID, Role: acct.Role, AuthMethod: "session"}
	return req.WithContext(auth.WithIdentity(req.Context(), ident))
}
