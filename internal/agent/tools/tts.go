package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fastclaw-ai/fastclaw/internal/toolproviders"
)

// RegisterTTSChain registers the tts tool against a provider chain. Absent
// credentials ⇒ the tool isn't visible to the agent at all.
func RegisterTTSChain(r *Registry, chain *toolproviders.Chain) {
	if chain == nil {
		return
	}
	// "none" is a sentinel meaning the admin explicitly opted out of
	// fastclaw's tts. Detected anywhere in the chain → don't register
	// the tool at all so the model falls back to its own native audio
	// capability (or does without).
	for _, ref := range chain.Order {
		name := ref
		if i := strings.IndexByte(ref, '/'); i >= 0 {
			name = ref[:i]
		}
		if name == "none" {
			return
		}
	}
	if !chain.Available() {
		return
	}
	r.Register("tts", "Convert text to speech. Uses a configurable provider chain (OpenAI tts-1, MiniMax speech-02, …) with automatic fallback. The audio file is attached to the chat message automatically.", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"text": map[string]interface{}{
				"type":        "string",
				"description": "Text to synthesize",
			},
			"voice": map[string]interface{}{
				"type":        "string",
				"description": "Voice id (provider-specific; default picked automatically)",
			},
		},
		"required": []string{"text"},
	}, func(ctx context.Context, rawArgs json.RawMessage) (string, error) {
		var args map[string]any
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
		resp, err := chain.Execute(ctx, args)
		if err != nil {
			return "", err
		}
		return r.archiveTTSOutput(ctx, resp.Text)
	})
}

func (r *Registry) archiveTTSOutput(ctx context.Context, text string) (string, error) {
	if r.workspaceStore == nil || r.agentID == "" {
		return text, nil
	}
	var extras []string
	for _, hostPath := range mediaPathsFromOutput(text) {
		data, err := os.ReadFile(hostPath)
		if err != nil {
			return "", fmt.Errorf("tts archive read %s: %w", hostPath, err)
		}
		if len(data) == 0 {
			return "", fmt.Errorf("tts archive: empty audio file")
		}
		ext := strings.TrimPrefix(filepath.Ext(hostPath), ".")
		if ext == "" {
			ext = "mp3"
		}
		rel := generatedArtifactPath("generated-audio", ext)
		ctype := audioContentType(ext)
		if err := r.workspaceStore.Put(ctx, r.agentID, r.projectID, r.scopeSessionID(), rel, bytes.NewReader(data), int64(len(data)), ctype); err != nil {
			return "", fmt.Errorf("tts archive put: %w", err)
		}
		extras = append(extras, workspaceDisplayPath(rel))
	}
	if len(extras) == 0 {
		return text, nil
	}
	var sb strings.Builder
	sb.WriteString(strings.TrimRight(text, "\n"))
	sb.WriteString("\n")
	for _, p := range extras {
		fmt.Fprintf(&sb, "Workspace path: %s\n", p)
	}
	return sb.String(), nil
}

func mediaPathsFromOutput(output string) []string {
	var paths []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "MEDIA:") {
			continue
		}
		p := strings.TrimSpace(strings.TrimPrefix(line, "MEDIA:"))
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths
}

func audioContentType(ext string) string {
	switch strings.ToLower(ext) {
	case "mp3":
		return "audio/mpeg"
	case "wav":
		return "audio/wav"
	case "ogg":
		return "audio/ogg"
	case "m4a":
		return "audio/mp4"
	default:
		return "application/octet-stream"
	}
}
