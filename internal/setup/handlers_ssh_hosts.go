package setup

import (
	"context"
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

// sshHostProbe is the connection check run before a host is persisted
// and again on /test. Tests replace it so handlers never dial a real
// network.
var sshHostProbe = sshhosts.Run

type sshHostWriteReq struct {
	Name       string `json:"name"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	AuthType   string `json:"authType"`
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"privateKey,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
	DefaultCWD string `json:"defaultCwd,omitempty"`
	Enabled    *bool  `json:"enabled,omitempty"`
}

func sshHostPublicView(h store.SSHHostRecord) map[string]any {
	return map[string]any{
		"id":             h.ID,
		"name":           h.Name,
		"host":           h.Host,
		"port":           h.Port,
		"username":       h.Username,
		"authType":       h.AuthType,
		"defaultCwd":     h.DefaultCWD,
		"enabled":        h.Enabled,
		"hasSecret":      h.SecretEnc != "",
		"hasHostKey":     h.HostKey != "",
		"lastTestStatus": h.LastTestStatus,
		"lastTestError":  h.LastTestError,
		"lastTestedAt":   h.LastTestedAt,
		"createdAt":      h.CreatedAt,
		"updatedAt":      h.UpdatedAt,
	}
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
		out = append(out, sshHostPublicView(h))
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
	if err := s.sshHostNameTaken(r.Context(), rec.UserID, rec.Name, ""); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrSSHHostNameTaken) {
			status = http.StatusConflict
		}
		jsonResponse(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	enc, err := box.Seal(creds)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	rec.SecretEnc = enc
	if err := probeSSHHost(r.Context(), rec, creds); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "connection failed: " + err.Error()})
		return
	}
	if err := s.dataStore.SaveSSHHost(r.Context(), rec); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrSSHHostNameTaken) {
			status = http.StatusConflict
		}
		jsonResponse(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"ok": true, "host": sshHostPublicView(*rec)})
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
	rec.LastTestStatus = existing.LastTestStatus
	rec.LastTestError = existing.LastTestError
	rec.LastTestedAt = existing.LastTestedAt
	credsChanged := req.Password != "" || req.PrivateKey != "" || req.Passphrase != ""
	if credsChanged {
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
	if rec.Host != existing.Host || rec.Port != existing.Port {
		rec.HostKey = ""
	}
	if err := s.sshHostNameTaken(r.Context(), rec.UserID, rec.Name, rec.ID); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrSSHHostNameTaken) {
			status = http.StatusConflict
		}
		jsonResponse(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if sshHostNeedsRetest(rec, existing, credsChanged) {
		probeCreds, err := credsForProbe(creds, existing, credsChanged)
		if err != nil {
			jsonResponse(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if err := probeSSHHost(r.Context(), rec, probeCreds); err != nil {
			jsonResponse(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "connection failed: " + err.Error()})
			return
		}
	}
	if err := s.dataStore.SaveSSHHost(r.Context(), rec); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrSSHHostNameTaken) {
			status = http.StatusConflict
		}
		jsonResponse(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"ok": true, "host": sshHostPublicView(*rec)})
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
	probeErr := probeSSHHost(r.Context(), rec, creds)
	_ = s.dataStore.SaveSSHHost(r.Context(), rec)
	if probeErr != nil {
		jsonResponse(w, http.StatusBadGateway, map[string]any{
			"ok":             false,
			"error":          probeErr.Error(),
			"lastTestStatus": rec.LastTestStatus,
			"lastTestError":  rec.LastTestError,
			"lastTestedAt":   rec.LastTestedAt,
		})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{
		"ok":             true,
		"output":         "ok",
		"lastTestStatus": rec.LastTestStatus,
		"lastTestedAt":   rec.LastTestedAt,
	})
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

func (s *Server) sshHostNameTaken(ctx context.Context, userID, name, exceptID string) error {
	existing, err := s.dataStore.GetSSHHostByName(ctx, userID, name)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	}
	if existing != nil && existing.ID != exceptID {
		return store.ErrSSHHostNameTaken
	}
	return nil
}

func probeSSHHost(ctx context.Context, rec *store.SSHHostRecord, creds sshhosts.Creds) error {
	res, err := sshHostProbe(ctx, *rec, creds, "echo ok", 20*time.Second)
	now := time.Now().UTC()
	rec.LastTestedAt = &now
	if err != nil {
		rec.LastTestStatus = store.SSHTestFail
		rec.LastTestError = clipSSHTestError(err.Error())
		return err
	}
	rec.LastTestStatus = store.SSHTestOK
	rec.LastTestError = ""
	if res.PinnedHostKey != "" && rec.HostKey == "" {
		rec.HostKey = res.PinnedHostKey
	}
	return nil
}

func credsForProbe(creds sshhosts.Creds, existing *store.SSHHostRecord, credsChanged bool) (sshhosts.Creds, error) {
	if credsChanged {
		return creds, nil
	}
	box, err := sshhosts.OpenBox()
	if err != nil {
		return creds, err
	}
	return box.Open(existing.SecretEnc)
}

func sshHostNeedsRetest(rec *store.SSHHostRecord, existing *store.SSHHostRecord, credsChanged bool) bool {
	if existing == nil || credsChanged {
		return true
	}
	if rec.Host != existing.Host || rec.Port != existing.Port || rec.Username != existing.Username || rec.AuthType != existing.AuthType {
		return true
	}
	if rec.DefaultCWD != existing.DefaultCWD {
		return true
	}
	return false
}

func clipSSHTestError(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 400 {
		return s[:400]
	}
	return s
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
		ID:         id,
		UserID:     userID,
		Name:       name,
		Host:       host,
		Port:       port,
		Username:   user,
		AuthType:   authType,
		DefaultCWD: cwd,
		Enabled:    enabled,
	}
	if existing != nil {
		rec.CreatedAt = existing.CreatedAt
	}
	return rec, creds, nil
}
