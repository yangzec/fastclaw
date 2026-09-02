package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// generatedArtifactPath returns a session-relative path under dir, unique
// enough that two tool calls in the same second do not collide.
func generatedArtifactPath(dir, ext string) string {
	ext = strings.TrimSpace(ext)
	if ext != "" && !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	var rb [4]byte
	_, _ = rand.Read(rb[:])
	return fmt.Sprintf("%s/%s-%s%s",
		strings.Trim(dir, "/"),
		time.Now().UTC().Format("20060102T150405Z"),
		hex.EncodeToString(rb[:]),
		ext,
	)
}

func workspaceDisplayPath(rel string) string {
	rel = path.Clean("/" + strings.TrimPrefix(strings.TrimSpace(rel), "/"))
	return "/workspace" + rel
}

// archiveDisplayURL is the URL a tool result may show the model.
// Prefer a configured PublicURL (stable CDN). Otherwise the cookie-
// authed gateway file API with ?sessionId=<goalSessionKey>. Never a
// signed URL — those expire and must not land in session JSON.
//
// Do not put sessions/<session_key>/ in the path: HTTP GET resolves
// chat_id / coding collapse from ?sessionId=. Coding root omits
// sessionId so the handler reads projects/<pid>/.
func (r *Registry) archiveDisplayURL(ctx context.Context, rel string) string {
	rel = strings.TrimPrefix(filepath.ToSlash(rel), "/")
	if r.workspaceStore != nil {
		if u, err := r.workspaceStore.PublicURL(ctx, r.agentID, r.projectID, r.scopeSessionID(), rel); err == nil && strings.TrimSpace(u) != "" {
			return strings.TrimSpace(u)
		}
	}
	return r.archiveGatewayURL(rel)
}

func (r *Registry) archiveGatewayURL(rel string) string {
	agent := url.PathEscape(r.agentID)
	p := strings.TrimPrefix(filepath.ToSlash(rel), "/")
	parts := strings.Split(p, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	base := "/api/agents/" + agent + "/files/" + strings.Join(parts, "/")
	params := url.Values{}
	if pid := strings.TrimSpace(r.projectID); pid != "" {
		params.Set("projectId", pid)
	}
	if key := strings.TrimSpace(r.goalSessionKey); key != "" && !r.codingRootScope {
		params.Set("sessionId", key)
	}
	if qs := params.Encode(); qs != "" {
		return base + "?" + qs
	}
	return base
}

func (r *Registry) writeFileWorkspaceResult(ctx context.Context, argsPath, rel string, n int) string {
	return fmt.Sprintf("Written %d bytes to %s\nWorkspace path: %s\nURL: %s",
		n, argsPath, workspaceDisplayPath(rel), r.archiveDisplayURL(ctx, rel))
}
