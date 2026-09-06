package plugin

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// InstallFromZip unpacks a plugin zip into pluginsDir/<id>/.
// Requires plugin.json with a non-empty id at the plugin root
// (or under a single common top-level directory).
func InstallFromZip(zipData []byte, pluginsDir string) (*InstallResult, error) {
	if len(zipData) == 0 {
		return nil, fmt.Errorf("empty zip")
	}
	if int64(len(zipData)) > MaxUploadBytes {
		return nil, fmt.Errorf("zip too large")
	}
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, fmt.Errorf("not a valid zip: %w", err)
	}
	commonTop := detectCommonTopDir(zr.File)
	stripPrefix := ""
	if commonTop != "" {
		stripPrefix = commonTop + "/"
	}
	manifestData, err := findPluginJSON(zr, stripPrefix)
	if err != nil {
		return nil, err
	}
	var manifest struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("invalid plugin.json: %w", err)
	}
	if manifest.ID == "" {
		return nil, fmt.Errorf("plugin.json missing 'id' field")
	}
	id := filepath.Base(manifest.ID)
	if id == "." || id == ".." || id == "/" || strings.ContainsAny(id, `/\`) {
		return nil, fmt.Errorf("plugin.json has invalid 'id' field")
	}
	manifest.ID = id

	destDir := filepath.Join(pluginsDir, manifest.ID)
	_ = os.RemoveAll(destDir)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, err
	}
	destAbs, err := filepath.Abs(filepath.Clean(destDir))
	if err != nil {
		return nil, err
	}

	for _, entry := range zr.File {
		name := entry.Name
		if strings.HasPrefix(name, "__MACOSX/") {
			continue
		}
		if stripPrefix != "" {
			if name == strings.TrimSuffix(stripPrefix, "/") || name == stripPrefix {
				continue
			}
			if !strings.HasPrefix(name, stripPrefix) {
				continue
			}
			name = strings.TrimPrefix(name, stripPrefix)
		}
		if name == "" {
			continue
		}
		clean := filepath.Clean(name)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			slog.Warn("skipping unsafe zip entry", "name", entry.Name)
			continue
		}
		dest := filepath.Join(destAbs, clean)
		destEntryAbs, err := filepath.Abs(filepath.Clean(dest))
		if err != nil || (destEntryAbs != destAbs && !strings.HasPrefix(destEntryAbs, destAbs+string(os.PathSeparator))) {
			slog.Warn("skipping zip-slip entry", "name", entry.Name, "dest", destEntryAbs)
			continue
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			slog.Warn("skipping symlink in zip", "name", entry.Name)
			continue
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(destEntryAbs, 0o755); err != nil {
				return nil, err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(destEntryAbs), 0o755); err != nil {
			return nil, err
		}
		rc, err := entry.Open()
		if err != nil {
			return nil, err
		}
		out, err := os.OpenFile(destEntryAbs, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			rc.Close()
			return nil, err
		}
		if _, err := io.Copy(out, io.LimitReader(rc, MaxUploadBytes)); err != nil {
			rc.Close()
			out.Close()
			return nil, err
		}
		rc.Close()
		out.Close()
	}

	// Ensure plugin.json landed
	if _, err := os.Stat(filepath.Join(destAbs, "plugin.json")); err != nil {
		_ = os.RemoveAll(destAbs)
		return nil, fmt.Errorf("zip is not a valid plugin: plugin.json not found at the plugin root")
	}
	name := manifest.Name
	if name == "" {
		name = manifest.ID
	}
	return &InstallResult{
		ID:           manifest.ID,
		Name:         name,
		Path:         destAbs,
		NeedsRestart: true,
		Source:       "zip",
	}, nil
}

func findPluginJSON(zr *zip.Reader, stripPrefix string) ([]byte, error) {
	for _, entry := range zr.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name, "__MACOSX/") {
			continue
		}
		name := entry.Name
		if stripPrefix != "" && strings.HasPrefix(name, stripPrefix) {
			name = strings.TrimPrefix(name, stripPrefix)
		}
		if name != "plugin.json" {
			continue
		}
		rc, err := entry.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(rc, 1<<20))
		rc.Close()
		if err != nil {
			return nil, err
		}
		return data, nil
	}
	return nil, fmt.Errorf("zip is not a valid plugin: plugin.json not found at the plugin root")
}

func detectCommonTopDir(files []*zip.File) string {
	var top string
	for _, f := range files {
		n := f.Name
		if n == "" {
			continue
		}
		if strings.HasPrefix(n, "__MACOSX/") {
			continue
		}
		idx := strings.Index(n, "/")
		if idx <= 0 {
			return ""
		}
		seg := n[:idx]
		if top == "" {
			top = seg
		} else if top != seg {
			return ""
		}
	}
	return top
}
