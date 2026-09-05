package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestSSHHostCRUD(t *testing.T) {
	st, err := NewDBStore("sqlite", "file:"+filepath.Join(t.TempDir(), "t.db")+"?cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	h := &SSHHostRecord{
		UserID:    "u1",
		Name:      "gpu-box",
		Host:      "10.0.4.21",
		Port:      22,
		Username:  "deploy",
		AuthType:  SSHAuthKey,
		SecretEnc: "ciphertext",
		Enabled:   true,
	}
	if err := st.SaveSSHHost(ctx, h); err != nil {
		t.Fatal(err)
	}
	if h.ID == "" {
		t.Fatal("expected generated id")
	}

	got, err := st.GetSSHHostByName(ctx, "u1", "gpu-box")
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "10.0.4.21" || got.SecretEnc != "ciphertext" {
		t.Fatalf("got %+v", got)
	}

	got.IdleTimeoutSec = 7200
	got.PersistTmux = true
	if err := st.SaveSSHHost(ctx, got); err != nil {
		t.Fatal(err)
	}
	again, err := st.GetSSHHost(ctx, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.IdleTimeoutSec != 7200 || !again.PersistTmux {
		t.Fatalf("persist fields: %+v", again)
	}

	now := again.UpdatedAt
	again.LastTestStatus = SSHTestOK
	again.LastTestError = ""
	again.LastTestedAt = &now
	if err := st.SaveSSHHost(ctx, again); err != nil {
		t.Fatal(err)
	}
	probed, err := st.GetSSHHost(ctx, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if probed.LastTestStatus != SSHTestOK || probed.LastTestedAt == nil {
		t.Fatalf("last test fields: %+v", probed)
	}

	dup := *h
	dup.ID = ""
	if err := st.SaveSSHHost(ctx, &dup); !errors.Is(err, ErrSSHHostNameTaken) {
		t.Fatalf("duplicate name: %v", err)
	}

	other := &SSHHostRecord{
		UserID:   "u2",
		Name:     "gpu-box",
		Host:     "10.0.0.2",
		Username: "root",
		AuthType: SSHAuthPassword,
		Enabled:  true,
	}
	if err := st.SaveSSHHost(ctx, other); err != nil {
		t.Fatal(err)
	}

	list, err := st.ListSSHHosts(ctx, "u1")
	if err != nil || len(list) != 1 {
		t.Fatalf("list u1: %v n=%d", err, len(list))
	}

	if err := st.DeleteSSHHost(ctx, h.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetSSHHost(ctx, h.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted host still there: %v", err)
	}
}
