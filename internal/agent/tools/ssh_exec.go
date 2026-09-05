package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/sshhosts"
	"github.com/fastclaw-ai/fastclaw/internal/store"
)

type sshExecArgs struct {
	Host    string `json:"host"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

const sshExecGuestRefusal = "[refused: saved SSH hosts are only available to the agent owner. Do not ask the chatter for a password or private key. Politely explain that remote SSH is an owner-only capability.]"

// sshRun is swapped in tests so ssh_exec does not need a live network.
var sshRun = sshhosts.Run

// RegisterSSHExec lets the owner run commands on saved SSH hosts.
// Credentials stay in the encrypted store; the model only ever sees
// the host alias and command output.
func RegisterSSHExec(r *Registry, st store.Store, box *sshhosts.Box, ownerUserID string) {
	if r == nil || st == nil || box == nil || ownerUserID == "" {
		return
	}
	r.Register("ssh_exec",
		"Run a shell command on a saved SSH host. Use the host alias from Settings → SSH Hosts (not a hostname or IP). FastClaw reuses the SSH connection (default 2h idle) and keeps a tmux session named fastclaw-<alias> on the server when tmux is installed. Injects the saved public key or password — never ask the user for credentials, and never put a password in exec() or in chat. Only the agent owner can use this tool.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"host": map[string]interface{}{
					"type":        "string",
					"description": "Saved host alias, e.g. gpu-box. Not a hostname.",
				},
				"command": map[string]interface{}{
					"type":        "string",
					"description": "Remote command to run, e.g. df -h or nvidia-smi.",
				},
				"timeout": map[string]interface{}{
					"type":        "integer",
					"description": "Timeout in seconds (default 60).",
				},
			},
			"required": []string{"host", "command"},
		},
		func(ctx context.Context, raw json.RawMessage) (string, error) {
			if !r.callerIsAdmin {
				return sshExecGuestRefusal, nil
			}
			var args sshExecArgs
			if err := json.Unmarshal(raw, &args); err != nil {
				return "", fmt.Errorf("parse args: %w", err)
			}
			alias := strings.TrimSpace(args.Host)
			command := strings.TrimSpace(args.Command)
			if alias == "" || command == "" {
				return "", fmt.Errorf("host and command are required")
			}
			if len(command) > 8*1024 {
				return "", fmt.Errorf("command is too long")
			}

			rec, err := st.GetSSHHostByName(ctx, ownerUserID, alias)
			if err != nil {
				names := sshHostAliases(ctx, st, ownerUserID)
				if len(names) == 0 {
					return "", fmt.Errorf("no saved SSH host named %q. Add one in Settings → SSH Hosts", alias)
				}
				return "", fmt.Errorf("no saved SSH host named %q. Available: %s", alias, strings.Join(names, ", "))
			}
			if !rec.Enabled {
				return "", fmt.Errorf("SSH host %q is disabled", alias)
			}
			creds, err := box.Open(rec.SecretEnc)
			if err != nil {
				return "", fmt.Errorf("unlock saved credential: %w", err)
			}

			timeout := time.Duration(args.Timeout) * time.Second
			res, err := sshRun(ctx, *rec, creds, command, timeout)
			if err != nil {
				return "", err
			}
			if res.PinnedHostKey != "" && rec.HostKey == "" {
				rec.HostKey = res.PinnedHostKey
				_ = st.SaveSSHHost(ctx, rec)
			}

			out := res.Output
			if out == "" {
				out = "(no output)"
			}
			if res.ExitCode != 0 {
				return fmt.Sprintf("%s\nExit code: %d", out, res.ExitCode), nil
			}
			return out, nil
		},
	)
}

func sshHostAliases(ctx context.Context, st store.Store, userID string) []string {
	rows, err := st.ListSSHHosts(ctx, userID)
	if err != nil {
		return nil
	}
	var names []string
	for _, h := range rows {
		if h.Enabled {
			names = append(names, h.Name)
		}
	}
	return names
}
