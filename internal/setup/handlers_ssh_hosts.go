package setup

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/auth"
	"github.com/fastclaw-ai/fastclaw/internal/sshhosts"
	"github.com/fastclaw-ai/fastclaw/internal/store"
)

var sshHostNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

type sshHostWriteReq struct {
	Name           string `json:"name"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Username       string `json:"username"`
	AuthType       string `json:"authType"`
	Password       string `json:"password,omitempty"`
	PrivateKey     string `json:"privateKey,omitempty"`
	Passphrase     string `json:"passphrase,omitempty"`
	DefaultCWD     string `json:"defaultCwd,omitempty"`
	Enabled        *bool  `json:"enabled,omitempty"`
	IdleTimeoutSec *int   `json:"idleTimeoutSec,omitempty"`
	PersistTmux    *bool  `json:"persistTmux,omitempty"`
}

func sshHostPublicView(h store.SSHHostRecord) map[string]any {
	idle := h.IdleTimeoutSec
	if idle == 0 {
		idle = int(sshhosts.DefaultIdleTimeout / time.Second)
	}
	view := map[string]any{
		"id":             h.ID,
		"name":           h.Name,
		"host":           h.Host,
		"port":           h.Port,
		"username":       h.Username,
		"authType":       h.AuthType,
		"defaultCwd":     h.DefaultCWD,
		"enabled":        h.Enabled,
		"idleTimeoutSec": idle,
		"persistTmux":    h.PersistTmux,
		"hasSecret":      h.SecretEnc != "",
		"hasHostKey":     h.HostKey != "",
		"hasPassphrase":  false, // filled by caller after decrypt is not needed — we don't store a flag
		"createdAt":      h.CreatedAt,
		"updatedAt":      h.UpdatedAt,
	}
	if h.PersistTmux {
		view["tmuxSession"] = sshhosts.TmuxSessionName(h.Name)
	}
	if info := sshhosts.DefaultPool.Info(h.ID); info.Connected {
		view["connected"] = true
		view["lastUsedAt"] = info.LastUsed
	} else {
		view["connected"] = false
	}
	return view
}

func (s *Server) handleListSSHHosts(w http.ResponseWriter, r *http.Request) {
	ident, ok := auth.FromContext(r.Context())
	if !ok || s.dataStore == nil {
		jsonResponse(w, http.StatusUnauthorized, map[string]any{"ok": false})
		return
	}
	rows, err := s.dataStore.ListSSHHosts(r.Context(), ident.EffectiveUserID())
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, h := range rows {
		view := sshHostPublicView(h)
		// Never imply we decrypted — hasPassphrase is only "key auth
		// might have one". We don't persist a separate flag, so omit
		// it rather than guess.
		delete(view, "hasPassphrase")
		out = append(out, view)
	}
	jsonResponse(w, http.StatusOK, map[string]any{"ok": true, "hosts": out})
}

func (s *Server) handleCreateSSHHost(w http.ResponseWriter, r *http.Request) {
	ident, ok := auth.FromContext(r.Context())
	if !ok || ident.ReadOnly() || s.dataStore == nil {
		jsonResponse(w, http.StatusForbidden, map[string]any{"ok": false, "error": "read-only"})
		return
	}
	var req sshHostWriteReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid request"})
		return
	}
	rec, creds, err := buildSSHHost(ident.EffectiveUserID(), "", req, nil)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	box, err := sshhosts.OpenBox()
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if err := sshhosts.ValidateCreds(rec.AuthType, creds); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	enc, err := box.Seal(creds)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	rec.SecretEnc = enc
	if err := s.dataStore.SaveSSHHost(r.Context(), rec); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrSSHHostNameTaken) {
			status = http.StatusConflict
		}
		jsonResponse(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	view := sshHostPublicView(*rec)
	delete(view, "hasPassphrase")
	jsonResponse(w, http.StatusOK, map[string]any{"ok": true, "host": view})
}

func (s *Server) handleUpdateSSHHost(w http.ResponseWriter, r *http.Request) {
	ident, ok := auth.FromContext(r.Context())
	if !ok || ident.ReadOnly() || s.dataStore == nil {
		jsonResponse(w, http.StatusForbidden, map[string]any{"ok": false, "error": "read-only"})
		return
	}
	existing, err := s.ownedSSHHost(r, ident.EffectiveUserID(), r.PathValue("id"))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		jsonResponse(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	var req sshHostWriteReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid request"})
		return
	}
	rec, creds, err := buildSSHHost(ident.EffectiveUserID(), existing.ID, req, existing)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	rec.SecretEnc = existing.SecretEnc
	rec.HostKey = existing.HostKey
	if req.Password != "" || req.PrivateKey != "" || req.Passphrase != "" {
		box, err := sshhosts.OpenBox()
		if err != nil {
			jsonResponse(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if req.Password == "" && req.PrivateKey == "" {
			// Passphrase-only update: merge onto existing key material.
			old, err := box.Open(existing.SecretEnc)
			if err != nil {
				jsonResponse(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			if creds.PrivateKey == "" {
				creds.PrivateKey = old.PrivateKey
			}
			if creds.Password == "" {
				creds.Password = old.Password
			}
		}
		if err := sshhosts.ValidateCreds(rec.AuthType, creds); err != nil {
			jsonResponse(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		enc, err := box.Seal(creds)
		if err != nil {
			jsonResponse(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		rec.SecretEnc = enc
		rec.HostKey = "" // new creds → re-TOFU
	}
	if err := s.dataStore.SaveSSHHost(r.Context(), rec); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrSSHHostNameTaken) {
			status = http.StatusConflict
		}
		jsonResponse(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if rec.Host != existing.Host || rec.Port != existing.Port || rec.Username != existing.Username ||
		rec.AuthType != existing.AuthType || rec.SecretEnc != existing.SecretEnc || !rec.Enabled {
		sshhosts.DefaultPool.Drop(rec.ID)
	}
	view := sshHostPublicView(*rec)
	delete(view, "hasPassphrase")
	jsonResponse(w, http.StatusOK, map[string]any{"ok": true, "host": view})
}

func (s *Server) handleDeleteSSHHost(w http.ResponseWriter, r *http.Request) {
	ident, ok := auth.FromContext(r.Context())
	if !ok || ident.ReadOnly() || s.dataStore == nil {
		jsonResponse(w, http.StatusForbidden, map[string]any{"ok": false, "error": "read-only"})
		return
	}
	if _, err := s.ownedSSHHost(r, ident.EffectiveUserID(), r.PathValue("id")); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		jsonResponse(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if err := s.dataStore.DeleteSSHHost(r.Context(), r.PathValue("id")); err != nil {
		jsonResponse(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sshhosts.DefaultPool.Drop(r.PathValue("id"))
	jsonResponse(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleTestSSHHost(w http.ResponseWriter, r *http.Request) {
	ident, ok := auth.FromContext(r.Context())
	if !ok || s.dataStore == nil {
		jsonResponse(w, http.StatusUnauthorized, map[string]any{"ok": false})
		return
	}
	rec, err := s.ownedSSHHost(r, ident.EffectiveUserID(), r.PathValue("id"))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		jsonResponse(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	box, err := sshhosts.OpenBox()
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	creds, err := box.Open(rec.SecretEnc)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	res, err := sshhosts.Run(r.Context(), *rec, creds, "echo ok", 0)
	if err != nil {
		jsonResponse(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if res.PinnedHostKey != "" && rec.HostKey == "" {
		rec.HostKey = res.PinnedHostKey
		_ = s.dataStore.SaveSSHHost(r.Context(), rec)
	}
	jsonResponse(w, http.StatusOK, map[string]any{"ok": true, "output": res.Output, "connected": true, "tmuxSession": sshhosts.TmuxSessionName(rec.Name)})
}

func (s *Server) handleDisconnectSSHHost(w http.ResponseWriter, r *http.Request) {
	ident, ok := auth.FromContext(r.Context())
	if !ok || ident.ReadOnly() || s.dataStore == nil {
		jsonResponse(w, http.StatusForbidden, map[string]any{"ok": false, "error": "read-only"})
		return
	}
	rec, err := s.ownedSSHHost(r, ident.EffectiveUserID(), r.PathValue("id"))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		jsonResponse(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sshhosts.DefaultPool.Drop(rec.ID)
	jsonResponse(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) ownedSSHHost(r *http.Request, userID, id string) (*store.SSHHostRecord, error) {
	if id == "" {
		return nil, store.ErrNotFound
	}
	rec, err := s.dataStore.GetSSHHost(r.Context(), id)
	if err != nil {
		return nil, err
	}
	if rec.UserID != userID {
		return nil, store.ErrNotFound
	}
	return rec, nil
}

func buildSSHHost(userID, id string, req sshHostWriteReq, existing *store.SSHHostRecord) (*store.SSHHostRecord, sshhosts.Creds, error) {
	name := strings.ToLower(strings.TrimSpace(req.Name))
	host := strings.TrimSpace(req.Host)
	user := strings.TrimSpace(req.Username)
	authType := strings.TrimSpace(req.AuthType)
	if existing != nil {
		if name == "" {
			name = existing.Name
		}
		if host == "" {
			host = existing.Host
		}
		if user == "" {
			user = existing.Username
		}
		if authType == "" {
			authType = existing.AuthType
		}
	}
	if !sshHostNameRe.MatchString(name) {
		return nil, sshhosts.Creds{}, errors.New("name must be lowercase letters, digits, and hyphens (e.g. gpu-box)")
	}
	if host == "" || strings.ContainsAny(host, " \t\n") || len(host) > 253 {
		return nil, sshhosts.Creds{}, errors.New("host is required")
	}
	if user == "" {
		return nil, sshhosts.Creds{}, errors.New("username is required")
	}
	if authType != store.SSHAuthKey && authType != store.SSHAuthPassword {
		return nil, sshhosts.Creds{}, errors.New("authType must be key or password")
	}
	port := req.Port
	if port == 0 && existing != nil {
		port = existing.Port
	}
	if port == 0 {
		port = 22
	}
	if port < 1 || port > 65535 {
		return nil, sshhosts.Creds{}, errors.New("port must be 1–65535")
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	} else if existing != nil {
		enabled = existing.Enabled
	}
	cwd := strings.TrimSpace(req.DefaultCWD)
	if existing != nil && req.DefaultCWD == "" {
		cwd = existing.DefaultCWD
	}

	idle := 7200
	if req.IdleTimeoutSec != nil {
		idle = *req.IdleTimeoutSec
		if idle < -1 {
			return nil, sshhosts.Creds{}, errors.New("idleTimeoutSec must be -1 (until restart) or a positive number of seconds")
		}
	} else if existing != nil {
		idle = existing.IdleTimeoutSec
	}

	persistTmux := true
	if req.PersistTmux != nil {
		persistTmux = *req.PersistTmux
	} else if existing != nil {
		persistTmux = existing.PersistTmux
	}

	creds := sshhosts.Creds{
		Password:   req.Password,
		PrivateKey: req.PrivateKey,
		Passphrase: req.Passphrase,
	}
	if existing == nil {
		if authType == store.SSHAuthPassword && creds.Password == "" {
			return nil, creds, errors.New("password is required")
		}
		if authType == store.SSHAuthKey && creds.PrivateKey == "" {
			return nil, creds, errors.New("private key is required")
		}
	}

	rec := &store.SSHHostRecord{
		ID:             id,
		UserID:         userID,
		Name:           name,
		Host:           host,
		Port:           port,
		Username:       user,
		AuthType:       authType,
		DefaultCWD:     cwd,
		Enabled:        enabled,
		IdleTimeoutSec: idle,
		PersistTmux:    persistTmux,
	}
	if existing != nil {
		rec.CreatedAt = existing.CreatedAt
	}
	return rec, creds, nil
}
