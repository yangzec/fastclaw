package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
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

	tested := time.Now().UTC().Truncate(time.Second)
	h.LastTestStatus = SSHTestOK
	h.LastTestError = ""
	h.LastTestedAt = &tested
	if err := st.SaveSSHHost(ctx, h); err != nil {
		t.Fatal(err)
	}
	got, err = st.GetSSHHost(ctx, h.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastTestStatus != SSHTestOK || got.LastTestedAt == nil {
		t.Fatalf("test status not persisted: %+v", got)
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
