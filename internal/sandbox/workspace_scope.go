package sandbox

import (
	"path"
	"strings"

	"github.com/fastclaw-ai/fastclaw/internal/workspace"
)

// ChatSandboxDir is the sandbox directory that corresponds to one
// workspace.Store scope. The turn sandbox mounts that store prefix at
// /workspace, so every mode uses the same physical root:
//
//	loose chat     sessions/<sid>/     → /workspace
//	project chat   projects/<pid>/<sid>/ → /workspace
//	coding         projects/<pid>/     → /workspace
func ChatSandboxDir(projectID, sessionID string) string {
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
// when the path is outside /workspace.
//
// Project chats used to mount the project root and cwd into
// /workspace/<sid>/. Accept that leftover prefix so a recycled
// container still mirrors into the chat store; /workspace/file (what
// write_file and img.save use) maps to file either way.
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
		if strings.HasPrefix(rest, prefix) {
			rest = strings.TrimPrefix(rest, prefix)
		}
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
