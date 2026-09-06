package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const hubRepo = "fastclaw-ai/fastclaw"

const MaxUploadBytes = 64 << 20

type InstallResult struct {
	ID           string `json:"id"`
	Name         string `json:"name,omitempty"`
	Path         string `json:"path,omitempty"`
	NeedsRestart bool   `json:"needsRestart"`
	Source       string `json:"source,omitempty"`
}

func IsLocalPath(s string) bool {
	return strings.HasPrefix(s, "./") || strings.HasPrefix(s, "/") || strings.HasPrefix(s, "../")
}

func IsGitHubRef(s string) bool {
	return strings.HasPrefix(s, "github.com/") || strings.HasPrefix(s, "https://github.com/")
}

func IsNpmPackage(s string) bool {
	return strings.HasPrefix(s, "@")
}

var errLocalAPI = "local paths not allowed via API"

func RejectLocalSource(source string) error {
	if IsLocalPath(source) {
		return fmt.Errorf("%s", errLocalAPI)
	}
	lower := strings.ToLower(source)
	if strings.HasPrefix(lower, "file:") || strings.Contains(source, ":\\") {
		return fmt.Errorf("%s", errLocalAPI)
	}
	return nil
}

func Install(source, pluginsDir string) (*InstallResult, error) {
	switch {
	case IsLocalPath(source):
		return InstallFromLocal(source, pluginsDir)
	case IsGitHubRef(source):
		return InstallFromGitHub(source, pluginsDir)
	case IsNpmPackage(source):
		return InstallFromNpm(source, pluginsDir)
	default:
		return InstallFromHub(source, pluginsDir)
	}
}

func InstallFromLocal(srcDir, pluginsDir string) (*InstallResult, error) {
	manifestPath := filepath.Join(srcDir, "plugin.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", manifestPath, err)
	}

	var manifest struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("invalid plugin.json: %w", err)
	}
	if manifest.ID == "" {
		return nil, fmt.Errorf("plugin.json missing 'id' field")
	}

	destDir := filepath.Join(pluginsDir, manifest.ID)
	os.RemoveAll(destDir)
	if err := os.MkdirAll(filepath.Dir(destDir), 0o755); err != nil {
		return nil, err
	}

	cpCmd := exec.Command("cp", "-r", srcDir, destDir)
	if out, err := cpCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("copy failed: %s: %w", string(out), err)
	}

	return &InstallResult{ID: manifest.ID, Name: manifest.ID, Path: destDir, NeedsRestart: true, Source: "local"}, nil

}

func InstallFromGitHub(source, pluginsDir string) (*InstallResult, error) {
	// Normalize URL
	repo := strings.TrimPrefix(source, "https://")
	repo = strings.TrimPrefix(repo, "github.com/")
	repo = strings.TrimSuffix(repo, ".git")
	repoURL := "https://github.com/" + repo

	// Clone to temp dir
	tmpDir, err := os.MkdirTemp("", "fastclaw-plugin-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	cloneCmd := exec.Command("git", "clone", "--depth=1", repoURL, tmpDir)
	if out, err := cloneCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git clone failed: %s: %w", string(out), err)
	}

	res, err := InstallFromLocal(tmpDir, pluginsDir)
	if err != nil {
		return nil, err
	}
	res.Source = "github"
	return res, nil
}

func InstallFromHub(name, pluginsDir string) (*InstallResult, error) {
	tmpDir, err := os.MkdirTemp("", "fastclaw-plugin-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	tarballURL := fmt.Sprintf("https://github.com/%s/archive/refs/heads/main.tar.gz", hubRepo)

	// Download tarball
	tarball := filepath.Join(tmpDir, "repo.tar.gz")
	dlCmd := exec.Command("curl", "-fsSL", "-o", tarball, tarballURL)
	if out, err := dlCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("download failed: %s: %w", string(out), err)
	}

	// Extract full tarball
	extractDir := filepath.Join(tmpDir, "extract")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return nil, err
	}
	tarCmd := exec.Command("tar", "-xzf", tarball, "-C", extractDir)
	if out, err := tarCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("extract failed: %s: %w", string(out), err)
	}

	// Find top-level dir (name varies: fastclaw-main, fastclaw-v0.16.0, etc.)
	entries, _ := os.ReadDir(extractDir)
	if len(entries) == 0 {
		return nil, fmt.Errorf("extract failed: empty archive")
	}
	pluginDir := filepath.Join(extractDir, entries[0].Name(), "plugins", name)
	if _, err := os.Stat(pluginDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("plugin %q not found in FastClaw Hub", name)
	}

	// Check if it has plugin.json (standard plugin) or is a utility
	if _, err := os.Stat(filepath.Join(pluginDir, "plugin.json")); err == nil {
		res, err := InstallFromLocal(pluginDir, pluginsDir)
		if err != nil {
			return nil, err
		}
		res.Source = "hub"
		return res, nil
	}

	// No plugin.json — copy as utility (e.g. plugin-bridge)
	toolsDir := filepath.Join(filepath.Dir(pluginsDir), "tools")
	os.MkdirAll(toolsDir, 0o755)
	destDir := filepath.Join(toolsDir, name)
	os.RemoveAll(destDir)
	cpCmd := exec.Command("cp", "-r", pluginDir, destDir)
	if out, err := cpCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("copy failed: %s: %w", string(out), err)
	}
	return &InstallResult{ID: name, Name: name, Path: destDir, NeedsRestart: true, Source: "hub"}, nil

}

func InstallFromNpm(pkg, pluginsDir string) (*InstallResult, error) {
	homeDir := filepath.Dir(pluginsDir)

	// Derive plugin ID from package name
	pluginID := pkg
	if i := strings.LastIndex(pluginID, "/"); i >= 0 {
		pluginID = pluginID[i+1:]
	}
	pluginID = strings.TrimPrefix(pluginID, "fastclaw-")

	// 1. npm install to temp dir to inspect the package
	tmpDir, err := os.MkdirTemp("", "fastclaw-npm-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	npmCmd := exec.Command("npm", "install", "--production", pkg)
	npmCmd.Dir = tmpDir
	if out, err := npmCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("npm install failed: %s: %w", string(out), err)
	}

	// 2. Check if it's a compatible plugin (supports fastclaw or openclaw plugin format)
	pkgDir := filepath.Join(tmpDir, "node_modules", pkg)
	isPlugin := false
	for _, marker := range []string{"fastclaw.plugin.json", "openclaw.plugin.json"} {
		if _, err := os.Stat(filepath.Join(pkgDir, marker)); err == nil {
			isPlugin = true
			break
		}
	}
	// Also check package.json for fastclaw/openclaw field
	if !isPlugin {
		if data, err := os.ReadFile(filepath.Join(pkgDir, "package.json")); err == nil {
			var pj map[string]json.RawMessage
			if json.Unmarshal(data, &pj) == nil {
				for _, key := range []string{"fastclaw", "openclaw"} {
					if _, ok := pj[key]; ok {
						isPlugin = true
						break
					}
				}
			}
		}
	}
	if !isPlugin {
		return nil, fmt.Errorf("%s is not a compatible plugin", pkg)
	}

	// 3. Find entry file
	entryFile := ""
	for _, name := range []string{"index.ts", "index.js"} {
		if _, err := os.Stat(filepath.Join(pkgDir, name)); err == nil {
			entryFile = fmt.Sprintf("./node_modules/%s/%s", pkg, name)
			break
		}
	}
	if entryFile == "" {
		return nil, fmt.Errorf("cannot find entry file for %s", pkg)
	}

	// 4. Test bridgeability — run proxy and check if tools are registered
	proxyDir := filepath.Join(homeDir, "tools", "plugin-bridge")
	proxyJS := filepath.Join(proxyDir, "proxy.js")
	if _, err := os.Stat(proxyJS); os.IsNotExist(err) {
		if _, err := InstallFromHub("plugin-bridge", pluginsDir); err != nil {
			return nil, fmt.Errorf("failed to install plugin-bridge: %w", err)
		}
		depCmd := exec.Command("npm", "install", "--production")
		depCmd.Dir = proxyDir
		if out, err := depCmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("npm install proxy deps failed: %s: %w", string(out), err)
		}
	}

	absEntry := filepath.Join(pkgDir, filepath.Base(entryFile))
	testInput := `{"jsonrpc":"2.0","method":"initialize","params":{"config":{}},"id":1}
{"jsonrpc":"2.0","method":"tool.list","id":2}
{"jsonrpc":"2.0","method":"shutdown","id":3}
`
	testCmd := exec.Command("npx", "tsx", proxyJS, absEntry)
	testCmd.Stdin = strings.NewReader(testInput)
	testCmd.Dir = tmpDir
	testOut, testErr := testCmd.CombinedOutput()

	toolCount := 0
	hasChannel := false
	for _, line := range strings.Split(string(testOut), "\n") {
		if !strings.HasPrefix(line, "{") {
			// Check stderr for channel registration
			if strings.Contains(line, "registered channel") {
				hasChannel = true
			}
			continue
		}
		var resp map[string]json.RawMessage
		if json.Unmarshal([]byte(line), &resp) == nil {
			if result, ok := resp["result"]; ok {
				var toolList struct {
					Tools []json.RawMessage `json:"tools"`
				}
				if json.Unmarshal(result, &toolList) == nil && len(toolList.Tools) > 0 {
					toolCount = len(toolList.Tools)
				}
			}
		}
	}

	if testErr != nil && toolCount == 0 {
		if hasChannel {
			return nil, fmt.Errorf("cannot install %s: this is a channel plugin that requires a separate runtime. Consider writing a native FastClaw plugin instead", pkg)
		}
		return nil, fmt.Errorf("cannot install %s: plugin is not compatible with FastClaw bridge", pkg)
	}

	if toolCount == 0 {
		return nil, fmt.Errorf("cannot install %s: no tools detected. Only plugins that register tools can be bridged", pkg)
	}

	// 5. Compatible! Move to plugins dir
	destDir := filepath.Join(pluginsDir, pluginID)
	os.RemoveAll(destDir)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, err
	}

	// npm install in final location
	npmCmd2 := exec.Command("npm", "install", "--production", pkg)
	npmCmd2.Dir = destDir
	if out, err := npmCmd2.CombinedOutput(); err != nil {
		os.RemoveAll(destDir)
		return nil, fmt.Errorf("npm install failed: %s: %w", string(out), err)
	}

	// Generate plugin.json
	manifest := map[string]any{
		"id":           pluginID,
		"name":         fmt.Sprintf("Bridged: %s", pkg),
		"version":      "0.1.0",
		"description":  fmt.Sprintf("Plugin %s (bridged via plugin-bridge)", pkg),
		"type":         "tool",
		"command":      fmt.Sprintf("npx tsx %s %s", proxyJS, entryFile),
		"capabilities": []string{"tool"},
		"config":       map[string]any{},
	}
	data, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(filepath.Join(destDir, "plugin.json"), data, 0o644); err != nil {
		return nil, err
	}

	return &InstallResult{ID: pluginID, Name: pluginID, Path: destDir, NeedsRestart: true}, nil

}
