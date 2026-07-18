package tools

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/toolproviders/imagegen"
)

const maxArchivedImageBytes = 25 << 20

func (r *Registry) archiveImageGenOutput(ctx context.Context, out imagegen.Output) (string, error) {
	if r.workspaceStore == nil {
		return "", fmt.Errorf("image_gen archive: workspace store is not configured")
	}
	if r.agentID == "" {
		return "", fmt.Errorf("image_gen archive: agent id is not configured")
	}
	var written []string
	rollback := func() {
		for _, p := range written {
			_ = r.workspaceStore.Delete(context.Background(), r.agentID, r.projectID, r.scopeSessionID(), p)
		}
	}
	idx := 0
	var urls []string
	archive := func(data []byte) error {
		idx++
		ctype := http.DetectContentType(data)
		ext, ok := imageExt(ctype)
		if !ok {
			return fmt.Errorf("image_gen archive: unsupported content type %s", ctype)
		}
		path := archiveImagePath(idx, ext)
		if err := r.workspaceStore.Put(ctx, r.agentID, r.projectID, r.scopeSessionID(), path, bytes.NewReader(data), int64(len(data)), ctype); err != nil {
			return fmt.Errorf("image_gen archive put: %w", err)
		}
		written = append(written, path)
		urls = append(urls, r.archiveDisplayURL(ctx, path))
		return nil
	}
	for _, u := range out.URLs {
		data, err := fetchImageForArchive(ctx, u)
		if err != nil {
			rollback()
			return "", err
		}
		if err := archive(data); err != nil {
			rollback()
			return "", err
		}
	}
	for _, b64 := range out.Base64 {
		data, err := decodeImageBase64(b64)
		if err != nil {
			rollback()
			return "", err
		}
		if err := archive(data); err != nil {
			rollback()
			return "", err
		}
	}
	if len(urls) == 0 {
		return "", fmt.Errorf("image_gen archive: no images to archive")
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Archived %d generated image(s):\n\n", len(urls))
	for i, u := range urls {
		fmt.Fprintf(&sb, "%d. ![image %d](%s)\n", i+1, i+1, u)
	}
	return sb.String(), nil
}

func fetchImageForArchive(ctx context.Context, raw string) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("image_gen archive: image URL must be http or https")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", fetchUserAgent)
	resp, err := safeFetchClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("image_gen archive fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("image_gen archive fetch HTTP %d", resp.StatusCode)
	}
	return readLimitedImage(resp.Body)
}

func decodeImageBase64(raw string) ([]byte, error) {
	if i := strings.Index(raw, ","); strings.HasPrefix(raw, "data:") && i >= 0 {
		raw = raw[i+1:]
	}
	return readLimitedImage(base64.NewDecoder(base64.StdEncoding, strings.NewReader(raw)))
}

func readLimitedImage(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxArchivedImageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxArchivedImageBytes {
		return nil, fmt.Errorf("image_gen archive: image exceeds 25 MiB")
	}
	return data, nil
}

func imageExt(ctype string) (string, bool) {
	switch ctype {
	case "image/png":
		return ".png", true
	case "image/jpeg":
		return ".jpg", true
	case "image/webp":
		return ".webp", true
	case "image/gif":
		return ".gif", true
	default:
		return "", false
	}
}

func archiveImagePath(index int, ext string) string {
	var rb [4]byte
	_, _ = rand.Read(rb[:])
	return fmt.Sprintf("generated-images/%s-%s-%d%s", time.Now().UTC().Format("20060102T150405Z"), hex.EncodeToString(rb[:]), index, ext)
}

func (r *Registry) archiveDisplayURL(ctx context.Context, path string) string {
	if r.workspaceStore != nil {
		if u, err := r.workspaceStore.PublicURL(ctx, r.agentID, r.projectID, r.scopeSessionID(), path); err == nil && strings.TrimSpace(u) != "" {
			return u
		}
	}
	return r.archiveURL(path)
}

func (r *Registry) archiveURL(path string) string {
	agent := url.PathEscape(r.agentID)
	p := strings.TrimPrefix(filepath.ToSlash(path), "/")
	if r.projectID != "" {
		return "/api/agents/" + agent + "/files/projects/" + url.PathEscape(r.projectID) + "/" + url.PathEscape(r.scopeSessionID()) + "/" + p
	}
	return "/api/agents/" + agent + "/files/sessions/" + url.PathEscape(r.scopeSessionID()) + "/" + p
}
