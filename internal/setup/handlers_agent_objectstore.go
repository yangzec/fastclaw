package setup

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/auth"
	"github.com/fastclaw-ai/fastclaw/internal/config"
	"github.com/fastclaw-ai/fastclaw/internal/scope"
	"github.com/fastclaw-ai/fastclaw/internal/store"
	"github.com/fastclaw-ai/fastclaw/internal/users"
	"github.com/fastclaw-ai/fastclaw/internal/workspace"
)

const agentObjectStoreNamespace = "objectstore"

type agentObjectStoreReq struct {
	AccountID string `json:"accountId"`
	Bucket    string `json:"bucket"`
	Prefix    string `json:"prefix"`
	Endpoint  string `json:"endpoint"`
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
}

type agentObjectStoreResp struct {
	Configured   bool   `json:"configured"`
	Enabled      bool   `json:"enabled"`
	Source       string `json:"source"`
	Type         string `json:"type,omitempty"`
	AccountID    string `json:"accountId,omitempty"`
	Bucket       string `json:"bucket,omitempty"`
	Prefix       string `json:"prefix,omitempty"`
	Endpoint     string `json:"endpoint,omitempty"`
	UseSSL       bool   `json:"useSSL"`
	HasAccessKey bool   `json:"hasAccessKey"`
	HasSecretKey bool   `json:"hasSecretKey"`
}

type agentStoreCacheInvalidator interface{ ClearAgentStoreCache(agentID string) }
type userStoreCacheInvalidator interface{ ClearUserInheritedStoreCache(userID string) }

func (s *Server) handleGetAgentObjectStore(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	if s.requireAgentOwner(w, r, agentID) == nil {
		return
	}
	resp, err := s.resolveAgentObjectStoreResponse(r.Context(), agentID)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, map[string]any{"error": "load objectstore"})
		return
	}
	jsonResponse(w, http.StatusOK, resp)
}

func (s *Server) handleTestAgentObjectStore(w http.ResponseWriter, r *http.Request) {
	if !s.requireWritable(w, r) {
		return
	}
	agentID := r.PathValue("id")
	if s.requireAgentOwner(w, r, agentID) == nil {
		return
	}
	cfg, err := s.objectStoreConfigFromRequest(r.Context(), "", agentID, r.Body)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	latency, err := testObjectStoreConnection(r.Context(), agentID, cfg)
	if err != nil {
		jsonResponse(w, http.StatusBadGateway, map[string]any{"error": "objectstore test failed"})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"ok": true, "latencyMs": latency.Milliseconds()})
}

func (s *Server) handlePutAgentObjectStore(w http.ResponseWriter, r *http.Request) {
	if !s.requireWritable(w, r) {
		return
	}
	agentID := r.PathValue("id")
	if s.requireAgentOwner(w, r, agentID) == nil {
		return
	}
	cfg, err := s.objectStoreConfigFromRequest(r.Context(), "", agentID, r.Body)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	latency, err := testObjectStoreConnection(r.Context(), agentID, cfg)
	if err != nil {
		jsonResponse(w, http.StatusBadGateway, map[string]any{"error": "objectstore test failed"})
		return
	}
	if err := scope.SaveSetting(r.Context(), s.dataStore, "", agentID, agentObjectStoreNamespace, objectStoreData(cfg)); err != nil {
		jsonResponse(w, http.StatusInternalServerError, map[string]any{"error": "save objectstore"})
		return
	}
	s.clearAgentObjectStoreCache(agentID)
	resp := objectStoreResponse(cfg, true, "agent", true)
	jsonResponse(w, http.StatusOK, map[string]any{"ok": true, "latencyMs": latency.Milliseconds(), "objectstore": resp})
}

func (s *Server) handleDeleteAgentObjectStore(w http.ResponseWriter, r *http.Request) {
	if !s.requireWritable(w, r) {
		return
	}
	agentID := r.PathValue("id")
	if s.requireAgentOwner(w, r, agentID) == nil {
		return
	}
	if err := scope.SaveSetting(r.Context(), s.dataStore, "", agentID, agentObjectStoreNamespace, nil); err != nil {
		jsonResponse(w, http.StatusInternalServerError, map[string]any{"error": "delete objectstore"})
		return
	}
	s.clearAgentObjectStoreCache(agentID)
	resp, _ := s.resolveAgentObjectStoreResponse(r.Context(), agentID)
	jsonResponse(w, http.StatusOK, map[string]any{"ok": true, "objectstore": resp})
}

func (s *Server) handleGetUserObjectStore(w http.ResponseWriter, r *http.Request) {
	uid, ok := userObjectStoreID(r)
	if !ok {
		jsonResponse(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	cfg, okOwn, err := s.loadScopedObjectStore(r.Context(), uid, "")
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, map[string]any{"error": "load objectstore"})
		return
	}
	if okOwn {
		jsonResponse(w, http.StatusOK, objectStoreResponse(cfg, true, "user", true))
		return
	}
	global, okGlobal, _ := s.loadScopedObjectStore(r.Context(), "", "")
	jsonResponse(w, http.StatusOK, objectStoreResponse(global, okGlobal, "global", false))
}

func (s *Server) handleTestUserObjectStore(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireUserObjectStoreWrite(w, r)
	if !ok {
		return
	}
	cfg, err := s.objectStoreConfigFromRequest(r.Context(), uid, "", r.Body)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	latency, err := testObjectStoreConnection(r.Context(), uid, cfg)
	if err != nil {
		jsonResponse(w, http.StatusBadGateway, map[string]any{"error": "objectstore test failed"})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"ok": true, "latencyMs": latency.Milliseconds()})
}

func (s *Server) handlePutUserObjectStore(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireUserObjectStoreWrite(w, r)
	if !ok {
		return
	}
	cfg, err := s.objectStoreConfigFromRequest(r.Context(), uid, "", r.Body)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	latency, err := testObjectStoreConnection(r.Context(), uid, cfg)
	if err != nil {
		jsonResponse(w, http.StatusBadGateway, map[string]any{"error": "objectstore test failed"})
		return
	}
	if err := scope.SaveSetting(r.Context(), s.dataStore, uid, "", agentObjectStoreNamespace, objectStoreData(cfg)); err != nil {
		jsonResponse(w, http.StatusInternalServerError, map[string]any{"error": "save objectstore"})
		return
	}
	s.clearUserObjectStoreCache(uid)
	jsonResponse(w, http.StatusOK, map[string]any{"ok": true, "latencyMs": latency.Milliseconds(), "objectstore": objectStoreResponse(cfg, true, "user", true)})
}

func (s *Server) handleDeleteUserObjectStore(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireUserObjectStoreWrite(w, r)
	if !ok {
		return
	}
	if err := scope.SaveSetting(r.Context(), s.dataStore, uid, "", agentObjectStoreNamespace, nil); err != nil {
		jsonResponse(w, http.StatusInternalServerError, map[string]any{"error": "delete objectstore"})
		return
	}
	s.clearUserObjectStoreCache(uid)
	global, okGlobal, _ := s.loadScopedObjectStore(r.Context(), "", "")
	jsonResponse(w, http.StatusOK, map[string]any{"ok": true, "objectstore": objectStoreResponse(global, okGlobal, "global", false)})
}

func userObjectStoreID(r *http.Request) (string, bool) {
	ident, ok := auth.FromContext(r.Context())
	if !ok {
		return "", false
	}
	uid := ident.EffectiveUserID()
	if uid == "" {
		uid = config.UserIDFromContext(r.Context())
	}
	return uid, uid != ""
}

func (s *Server) requireUserObjectStoreWrite(w http.ResponseWriter, r *http.Request) (string, bool) {
	if !s.requireWritable(w, r) {
		return "", false
	}
	ident, ok := auth.FromContext(r.Context())
	if !ok {
		jsonResponse(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return "", false
	}
	if ident.Role == users.RoleAppUser {
		jsonResponse(w, http.StatusForbidden, map[string]any{"ok": false, "error": "app_user cannot manage user-scope configs"})
		return "", false
	}
	uid := ident.EffectiveUserID()
	if uid == "" {
		jsonResponse(w, http.StatusForbidden, map[string]any{"ok": false, "error": "user identity required"})
		return "", false
	}
	return uid, true
}

func (s *Server) clearAgentObjectStoreCache(agentID string) {
	if c, ok := s.workspaceStore.(agentStoreCacheInvalidator); ok {
		c.ClearAgentStoreCache(agentID)
	}
}

func (s *Server) clearUserObjectStoreCache(userID string) {
	if c, ok := s.workspaceStore.(userStoreCacheInvalidator); ok {
		c.ClearUserInheritedStoreCache(userID)
	}
}

func (s *Server) objectStoreConfigFromRequest(ctx context.Context, userID, agentID string, body io.Reader) (config.ObjectStoreCfg, error) {
	var req agentObjectStoreReq
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		return config.ObjectStoreCfg{}, fmt.Errorf("invalid json")
	}
	old, _, _ := s.loadScopedObjectStore(ctx, userID, agentID)
	accessKey := strings.TrimSpace(req.AccessKey)
	secretKey := strings.TrimSpace(req.SecretKey)
	if accessKey == "" {
		accessKey = old.S3.AccessKey
	}
	if secretKey == "" {
		secretKey = old.S3.SecretKey
	}
	endpoint, err := normalizeR2Endpoint(req.Endpoint)
	if err != nil {
		return config.ObjectStoreCfg{}, err
	}
	cfg := config.ObjectStoreCfg{Type: "cloudflare-r2", AccountID: strings.TrimSpace(req.AccountID)}
	cfg.S3.Bucket = strings.TrimSpace(req.Bucket)
	cfg.S3.Prefix = strings.Trim(strings.TrimSpace(req.Prefix), "/")
	cfg.S3.Endpoint = endpoint
	cfg.S3.AccessKey = accessKey
	cfg.S3.SecretKey = secretKey
	cfg.S3.UseSSL = true
	if cfg.AccountID == "" && cfg.S3.Endpoint == "" {
		return cfg, fmt.Errorf("accountId is required when endpoint is empty")
	}
	if cfg.S3.Bucket == "" {
		return cfg, fmt.Errorf("bucket is required")
	}
	if cfg.S3.AccessKey == "" || cfg.S3.SecretKey == "" {
		return cfg, fmt.Errorf("access key and secret key are required")
	}
	return cfg, nil
}

func (s *Server) loadAgentObjectStore(ctx context.Context, agentID string) (config.ObjectStoreCfg, bool, error) {
	return s.loadScopedObjectStore(ctx, "", agentID)
}

func (s *Server) loadScopedObjectStore(ctx context.Context, userID, agentID string) (config.ObjectStoreCfg, bool, error) {
	rec, err := s.dataStore.GetConfigByName(ctx, store.KindSetting, userID, agentID, agentObjectStoreNamespace)
	if errors.Is(err, store.ErrNotFound) {
		return config.ObjectStoreCfg{}, false, nil
	}
	if err != nil || rec == nil || !rec.Enabled || len(rec.Data) == 0 {
		return config.ObjectStoreCfg{}, false, err
	}
	var cfg config.ObjectStoreCfg
	b, err := json.Marshal(rec.Data)
	if err != nil {
		return cfg, false, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, false, err
	}
	return cfg, true, nil
}

func (s *Server) resolveAgentObjectStoreResponse(ctx context.Context, agentID string) (agentObjectStoreResp, error) {
	cfg, ok, err := s.loadScopedObjectStore(ctx, "", agentID)
	if err != nil {
		return agentObjectStoreResp{}, err
	}
	if ok {
		return objectStoreResponse(cfg, true, "agent", true), nil
	}
	rec, err := s.dataStore.GetAgent(ctx, agentID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return agentObjectStoreResp{}, err
	}
	if rec != nil && rec.UserID != "" {
		if cfg, ok, err := s.loadScopedObjectStore(ctx, rec.UserID, ""); err != nil {
			return agentObjectStoreResp{}, err
		} else if ok {
			return objectStoreResponse(cfg, true, "user", false), nil
		}
	}
	global, okGlobal, _ := s.loadScopedObjectStore(ctx, "", "")
	return objectStoreResponse(global, okGlobal, "global", false), nil
}

func objectStoreResponse(cfg config.ObjectStoreCfg, ok bool, source string, exposeKeyPresence bool) agentObjectStoreResp {
	resp := agentObjectStoreResp{Source: source, UseSSL: true}
	if !ok {
		if source == "" {
			resp.Source = "global"
		}
		return resp
	}
	resp.Configured = source == "agent" || source == "user"
	resp.Enabled = source == "agent" || source == "user"
	resp.Type = cfg.Type
	resp.AccountID = cfg.AccountID
	resp.Bucket = cfg.S3.Bucket
	resp.Prefix = cfg.S3.Prefix
	resp.Endpoint = cfg.S3.Endpoint
	if exposeKeyPresence {
		resp.HasAccessKey = cfg.S3.AccessKey != ""
		resp.HasSecretKey = cfg.S3.SecretKey != ""
	}
	return resp
}

func objectStoreData(cfg config.ObjectStoreCfg) map[string]interface{} {
	b, _ := json.Marshal(cfg)
	var m map[string]interface{}
	_ = json.Unmarshal(b, &m)
	return m
}

func normalizeR2Endpoint(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", fmt.Errorf("endpoint must be an HTTPS hostname without path, query, or fragment")
	}
	return u.Host, nil
}

var testObjectStoreConnection = func(ctx context.Context, agentID string, cfg config.ObjectStoreCfg) (time.Duration, error) {
	start := time.Now()
	ws, err := workspace.Factory{Type: cfg.Type, AccountID: cfg.AccountID, S3: workspace.S3Config{Endpoint: cfg.S3.Endpoint, Bucket: cfg.S3.Bucket, Prefix: cfg.S3.Prefix, AccessKey: cfg.S3.AccessKey, SecretKey: cfg.S3.SecretKey, UseSSL: true}}.New("")
	if err != nil {
		return 0, err
	}
	var rb [12]byte
	if _, err := rand.Read(rb[:]); err != nil {
		return 0, err
	}
	path := "health-check/" + hex.EncodeToString(rb[:]) + ".txt"
	want := []byte("fastclaw-objectstore-health-check")
	if err := ws.Put(ctx, agentID, "", "", path, bytes.NewReader(want), int64(len(want)), "text/plain"); err != nil {
		return 0, err
	}
	defer ws.Delete(context.Background(), agentID, "", "", path)
	rc, err := ws.Get(ctx, agentID, "", "", path)
	if err != nil {
		return 0, err
	}
	got, err := io.ReadAll(io.LimitReader(rc, int64(len(want)+1)))
	rc.Close()
	if err != nil {
		return 0, err
	}
	if !bytes.Equal(got, want) {
		return 0, fmt.Errorf("health check content mismatch")
	}
	if err := ws.Delete(ctx, agentID, "", "", path); err != nil {
		return 0, err
	}
	return time.Since(start), nil
}
