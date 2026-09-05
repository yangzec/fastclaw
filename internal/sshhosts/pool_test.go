package sshhosts

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/fastclaw-ai/fastclaw/internal/store"
)

func TestPoolReusesConnection(t *testing.T) {
	t.Cleanup(ResetPool)
	handshakes, rec, creds := startPooledSSH(t)

	ctx := context.Background()
	if _, err := Run(ctx, rec, creds, "one", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, rec, creds, "two", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if n := handshakes.Load(); n != 1 {
		t.Fatalf("handshakes=%d want 1", n)
	}
	if !DefaultPool.Info(rec.ID).Connected {
		t.Fatal("expected live pooled connection")
	}

	DefaultPool.Drop(rec.ID)
	if DefaultPool.Info(rec.ID).Connected {
		t.Fatal("drop should disconnect")
	}
	if _, err := Run(ctx, rec, creds, "three", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if n := handshakes.Load(); n != 2 {
		t.Fatalf("after drop handshakes=%d want 2", n)
	}
}

func TestPoolIdleDisconnect(t *testing.T) {
	t.Cleanup(ResetPool)
	handshakes, rec, creds := startPooledSSH(t)
	rec.IdleTimeoutSec = 1

	if _, err := Run(context.Background(), rec, creds, "one", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for DefaultPool.Info(rec.ID).Connected && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if DefaultPool.Info(rec.ID).Connected {
		t.Fatal("idle client should have dropped")
	}
	if _, err := Run(context.Background(), rec, creds, "two", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if n := handshakes.Load(); n != 2 {
		t.Fatalf("handshakes=%d want 2 after idle reconnect", n)
	}
}

func TestTmuxSessionName(t *testing.T) {
	if got := TmuxSessionName("gpu-box"); got != "fastclaw-gpu-box" {
		t.Fatalf("got %q", got)
	}
	if got := TmuxSessionName(""); got != "fastclaw" {
		t.Fatalf("empty alias: %q", got)
	}
}

func startPooledSSH(t *testing.T) (*atomic.Int32, store.SSHHostRecord, Creds) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == "deploy" && string(pass) == "s3cret" {
				return nil, nil
			}
			return nil, errors.New("denied")
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	var handshakes atomic.Int32
	go serveSSHCounting(t, ln, cfg, &handshakes)

	host, port, _ := net.SplitHostPort(ln.Addr().String())
	rec := store.SSHHostRecord{
		ID:             "ssh_pool_test",
		Host:           host,
		Port:           mustPort(port),
		Username:       "deploy",
		AuthType:       store.SSHAuthPassword,
		IdleTimeoutSec: 60,
	}
	return &handshakes, rec, Creds{Password: "s3cret"}
}

func serveSSHCounting(t *testing.T, ln net.Listener, cfg *ssh.ServerConfig, n *atomic.Int32) {
	t.Helper()
	for {
		nConn, err := ln.Accept()
		if err != nil {
			return
		}
		go func(nConn net.Conn) {
			conn, chans, reqs, err := ssh.NewServerConn(nConn, cfg)
			if err != nil {
				_ = nConn.Close()
				return
			}
			n.Add(1)
			defer conn.Close()
			go ssh.DiscardRequests(reqs)
			for newCh := range chans {
				if newCh.ChannelType() != "session" {
					newCh.Reject(ssh.UnknownChannelType, "unknown")
					continue
				}
				ch, reqs, err := newCh.Accept()
				if err != nil {
					return
				}
				go handleSession(ch, reqs)
			}
		}(nConn)
	}
}
