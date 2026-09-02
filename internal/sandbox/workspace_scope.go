package sandbox

import (
	"path"
	"strings"

	"github.com/fastclaw-ai/fastclaw/internal/workspace"
)

// ChatSandboxDir is the sandbox directory that corresponds to one
// workspace.Store scope.
//
//	project="", session=x  → /workspace          (mount is already sessions/x)
//	project=p,  session="" → /workspace          (coding / project root)
//	project=p,  session=x  → /workspace/x        (project chat subdir)
func ChatSandboxDir(projectID, sessionID string) string {
	if projectID != "" && sessionID != "" {
		return "/workspace/" + sessionID
	}
	return "/workspace"
}

// ChatSandboxFile joins rel onto ChatSandboxDir.
func ChatSandboxFile(projectID, sessionID, rel string) string {
	rel = strings.TrimPrefix(path.Clean("/"+rel), "/")
	dir := ChatSandboxDir(projectID, sessionID)
	if rel == "" || rel == "." {
		return dir
	}
	return dir + "/" + rel
}

// RelFromSandboxPath maps an absolute sandbox path back to the
// store-relative key for this (project, session) scope. Returns ok=false
// when the path is outside /workspace or outside this chat's subdir.
func RelFromSandboxPath(projectID, sessionID, sandboxPath string) (string, bool) {
	clean := path.Clean("/" + strings.TrimSpace(sandboxPath))
	rest, ok := strings.CutPrefix(clean, "/workspace/")
	if !ok {
		return "", false
	}
	if projectID != "" && sessionID != "" {
		prefix := sessionID + "/"
		if rest == sessionID {
			return "", false
		}
		if !strings.HasPrefix(rest, prefix) {
			return "", false
		}
		rest = strings.TrimPrefix(rest, prefix)
	}
	if rest == "" || rest == "." {
		return "", false
	}
	return rest, true
}

// skipSnapshotRel reports whether a workspace-relative snapshot path
// should be dropped (dependency / build trees).
func skipSnapshotRel(rel string) bool {
	rel = strings.TrimPrefix(path.Clean("/"+rel), "/")
	if rel == "" || rel == "." {
		return true
	}
	for _, seg := range strings.Split(rel, "/") {
		if workspace.IsBuildArtifactDir(seg) {
			return true
		}
	}
	return false
}
