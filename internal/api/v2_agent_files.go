package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/workspace"
)

type v2Artifact struct {
	ID          string    `json:"fileId"`
	Name        string    `json:"name"`
	ContentType string    `json:"contentType"`
	Size        int64     `json:"size"`
	ModifiedAt  time.Time `json:"modifiedAt"`
	PreviewURL  string    `json:"previewUrl"`
	DownloadURL string    `json:"downloadUrl"`
}

func (s *Server) HandleV2ListSessionFiles(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("agentId")
	if _, err := s.v2AgentForRequest(r, agentID); err != nil {
		writeV2AgentError(w, err)
		return
	}
	sessionID := r.PathValue("sessionId")
	if !v2SessionIDPattern.MatchString(sessionID) {
		writeV2Error(w, http.StatusBadRequest, "invalid sessionId")
		return
	}
	userID, ok := v2RequestUserID(r)
	if !ok {
		writeV2Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	objects, err := s.listV2Workspace(r.Context(), agentID, v2WorkspaceSessionID(userID, sessionID))
	if err != nil {
		writeV2Error(w, http.StatusInternalServerError, "failed to list session files")
		return
	}
	files := make([]v2Artifact, 0, len(objects))
	for _, object := range objects {
		files = append(files, newV2Artifact(agentID, sessionID, object))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"agentId": agentID, "sessionId": sessionID, "files": files,
	})
}

func (s *Server) HandleV2GetSessionFile(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("agentId")
	if _, err := s.v2AgentForRequest(r, agentID); err != nil {
		writeV2AgentError(w, err)
		return
	}
	sessionID := r.PathValue("sessionId")
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
	if s.workspace == nil {
		writeV2Error(w, http.StatusServiceUnavailable, "workspace store is not configured")
		return
	}

	objects, err := s.workspace.List(r.Context(), agentID, "", workspaceSessionID)
	if err != nil {
		writeV2Error(w, http.StatusInternalServerError, "failed to list session files")
		return
	}
	fileID := r.PathValue("fileId")
	var matched *workspace.ObjectInfo
	for i := range objects {
		if v2FileID(objects[i].Path) == fileID {
			matched = &objects[i]
			break
		}
	}
	if matched == nil {
		writeV2Error(w, http.StatusNotFound, "file not found")
		return
	}

	reader, err := s.workspace.Get(r.Context(), agentID, "", workspaceSessionID, matched.Path)
	if errors.Is(err, workspace.ErrNotFound) {
		writeV2Error(w, http.StatusNotFound, "file not found")
		return
	}
	if err != nil {
		writeV2Error(w, http.StatusInternalServerError, "failed to read file")
		return
	}
	defer reader.Close()

	contentType := v2ContentType(*matched)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	disposition := "inline"
	if r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{
		"filename": filepath.Base(matched.Path),
	}))
	if strings.HasPrefix(contentType, "text/html") {
		// Scripts may be needed for generated charts, but the sandbox keeps
		// the document in a unique origin so it cannot read FastClaw cookies
		// or DOM state from the hosting application.
		w.Header().Set("Content-Security-Policy", "sandbox allow-scripts; default-src 'none'; img-src data: blob: https:; style-src 'unsafe-inline' https:; script-src 'unsafe-inline' https:; font-src data: https:")
	} else if strings.HasPrefix(contentType, "image/svg+xml") {
		// SVG may contain script. Keep it renderable as an image while
		// forbidding active content and same-origin access.
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src data:")
	}
	if matched.Size >= 0 {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", matched.Size))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, reader)
}

func (s *Server) listV2Workspace(ctx context.Context, agentID, sessionID string) ([]workspace.ObjectInfo, error) {
	if s.workspace == nil {
		return nil, nil
	}
	objects, err := s.workspace.List(ctx, agentID, "", sessionID)
	if err != nil {
		return nil, err
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].Path < objects[j].Path })
	return objects, nil
}

func newV2Artifact(agentID, sessionID string, object workspace.ObjectInfo) v2Artifact {
	fileID := v2FileID(object.Path)
	base := "/v2/agents/" + url.PathEscape(agentID) +
		"/sessions/" + url.PathEscape(sessionID) +
		"/files/" + url.PathEscape(fileID)
	return v2Artifact{
		ID:          fileID,
		Name:        filepath.Base(object.Path),
		ContentType: v2ContentType(object),
		Size:        object.Size,
		ModifiedAt:  object.ModTime.UTC(),
		PreviewURL:  base,
		DownloadURL: base + "?download=1",
	}
}

func v2FileID(path string) string {
	hash := sha256.Sum256([]byte(path))
	return base64.RawURLEncoding.EncodeToString(hash[:18])
}

func v2WorkspaceSessionID(userID, sessionID string) string {
	hash := sha256.Sum256([]byte(userID))
	userScope := base64.RawURLEncoding.EncodeToString(hash[:12])
	return "user_" + userScope + "_" + sessionID
}

func v2ContentType(object workspace.ObjectInfo) string {
	contentType := strings.TrimSpace(object.ContentType)
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(object.Path))
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return contentType
	}
	if strings.HasPrefix(mediaType, "text/") {
		if _, exists := params["charset"]; !exists {
			params["charset"] = "utf-8"
		}
	}
	return mime.FormatMediaType(mediaType, params)
}

func diffV2Artifacts(agentID, sessionID string, before, after []workspace.ObjectInfo) []v2Artifact {
	previous := make(map[string]workspace.ObjectInfo, len(before))
	for _, object := range before {
		previous[object.Path] = object
	}
	var artifacts []v2Artifact
	for _, object := range after {
		old, existed := previous[object.Path]
		if existed && old.Size == object.Size && old.ModTime.Equal(object.ModTime) && old.ContentType == object.ContentType {
			continue
		}
		artifacts = append(artifacts, newV2Artifact(agentID, sessionID, object))
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Name < artifacts[j].Name })
	return artifacts
}
