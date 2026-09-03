package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/sshhosts"
	"github.com/fastclaw-ai/fastclaw/internal/store"
)

func TestSSHExecOwnerUsesAliasNotSecret(t *testing.T) {
	st := newTestStore(t)
	t.Setenv("FASTCLAW_HOME", t.TempDir())
	box, err := sshhosts.OpenBox()
	if err != nil {
		t.Fatal(err)
	}
	secret, err := box.Seal(sshhosts.Creds{Password: "s3cret"})
	if err != nil {
		t.Fatal(err)
	}
	owner := "owner-1"
	if err := st.SaveSSHHost(context.Background(), &store.SSHHostRecord{
		UserID:    owner,
		Name:      "gpu-box",
		Host:      "10.0.4.21",
		Port:      22,
		Username:  "deploy",
		AuthType:  store.SSHAuthPassword,
		SecretEnc: secret,
		Enabled:   true,
	}); err != nil {
		t.Fatal(err)
	}

	var sawPassword bool
	orig := sshRun
	t.Cleanup(func() { sshRun = orig })
	sshRun = func(ctx context.Context, host store.SSHHostRecord, creds sshhosts.Creds, command string, timeout time.Duration) (sshhosts.Result, error) {
		if creds.Password != "s3cret" {
			t.Errorf("dialer got password %q", creds.Password)
		}
		if strings.Contains(command, "s3cret") {
			sawPassword = true
		}
		if host.Name != "gpu-box" || command != "df -h" {
			t.Errorf("host=%q cmd=%q", host.Name, command)
		}
		return sshhosts.Result{Output: "ok-from-remote"}, nil
	}

	r := NewRegistry(filepath.Join(t.TempDir(), "home"), t.TempDir())
	r.SetCallerIsAdmin(true)
	RegisterSSHExec(r, st, box, owner)

	out, err := r.Execute(context.Background(), "ssh_exec", mustJSON(t, sshExecArgs{
		Host:    "gpu-box",
		Command: "df -h",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if out != "ok-from-remote" {
		t.Fatalf("output = %q", out)
	}
	if sawPassword {
		t.Fatal("password leaked into the remote command")
	}
}

func TestSSHExecGuestRefused(t *testing.T) {
	st := newTestStore(t)
	t.Setenv("FASTCLAW_HOME", t.TempDir())
	box, err := sshhosts.OpenBox()
	if err != nil {
		t.Fatal(err)
	}
	r := NewRegistry(t.TempDir(), t.TempDir())
	r.SetCallerIsAdmin(false)
	RegisterSSHExec(r, st, box, "owner-1")

	out, err := r.Execute(context.Background(), "ssh_exec", mustJSON(t, sshExecArgs{
		Host:    "gpu-box",
		Command: "id",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "refused") {
		t.Fatalf("guest should be refused, got %q", out)
	}
}

func TestSSHExecUnknownAliasListsNames(t *testing.T) {
	st := newTestStore(t)
	t.Setenv("FASTCLAW_HOME", t.TempDir())
	box, err := sshhosts.OpenBox()
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := box.Seal(sshhosts.Creds{Password: "x"})
	_ = st.SaveSSHHost(context.Background(), &store.SSHHostRecord{
		UserID: "owner-1", Name: "gpu-box", Host: "h", Username: "u",
		AuthType: store.SSHAuthPassword, SecretEnc: secret, Enabled: true,
	})
	r := NewRegistry(t.TempDir(), t.TempDir())
	r.SetCallerIsAdmin(true)
	RegisterSSHExec(r, st, box, "owner-1")

	_, err = r.Execute(context.Background(), "ssh_exec", mustJSON(t, sshExecArgs{
		Host: "missing", Command: "id",
	}))
	if err == nil || !strings.Contains(err.Error(), "gpu-box") {
		t.Fatalf("expected available alias in error, got %v", err)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
