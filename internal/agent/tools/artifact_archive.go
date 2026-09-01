package tools

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path"
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
