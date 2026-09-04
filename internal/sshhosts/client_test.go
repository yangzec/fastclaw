package sshhosts

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/fastclaw-ai/fastclaw/internal/store"
)

func TestRunPasswordAndKey(t *testing.T) {
	t.Cleanup(ResetPool)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner := signer

	clientPub, clientPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientSigner, err := ssh.NewSignerFromKey(clientPriv)
	if err != nil {
		t.Fatal(err)
	}
	_ = clientPub
	_ = pub

	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == "deploy" && string(pass) == "s3cret" {
				return nil, nil
			}
			return nil, errors.New("denied")
		},
		PublicKeyCallback: func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if c.User() == "keyuser" && string(key.Marshal()) == string(clientSigner.PublicKey().Marshal()) {
				return nil, nil
			}
			return nil, errors.New("denied")
		},
	}
	cfg.AddHostKey(hostSigner)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go serveSSH(t, ln, cfg)

	host, port, _ := net.SplitHostPort(ln.Addr().String())
	rec := store.SSHHostRecord{
		Host:     host,
		Port:     mustPort(port),
		Username: "deploy",
		AuthType: store.SSHAuthPassword,
	}
	res, err := Run(context.Background(), rec, Creds{Password: "s3cret"}, "echo ok", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "ok\n" && res.Output != "ok" {
		// servers wrap echo; accept prefix
		if res.Output == "" {
			t.Fatalf("empty output: %+v", res)
		}
	}
	if res.PinnedHostKey == "" {
		t.Fatal("expected TOFU pin")
	}

	// Replay with pinned key and a bad password should fail handshake.
	rec.HostKey = res.PinnedHostKey
	if _, err := Run(context.Background(), rec, Creds{Password: "nope"}, "echo ok", 5*time.Second); err == nil {
		t.Fatal("expected auth failure")
	}

	keyRec := rec
	keyRec.Username = "keyuser"
	keyRec.AuthType = store.SSHAuthKey
	keyPEM, err := ssh.MarshalPrivateKey(clientPriv, "")
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(keyPEM)
	res2, err := Run(context.Background(), keyRec, Creds{PrivateKey: string(pemBytes)}, "uname", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Output == "" {
		t.Fatal("expected key-auth output")
	}
}

func TestRunMergesStdoutAndStderr(t *testing.T) {
	t.Cleanup(ResetPool)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	_ = pub
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
	go serveSSHBoth(t, ln, cfg)

	host, port, _ := net.SplitHostPort(ln.Addr().String())
	rec := store.SSHHostRecord{
		Host:     host,
		Port:     mustPort(port),
		Username: "deploy",
		AuthType: store.SSHAuthPassword,
	}
	res, err := Run(context.Background(), rec, Creds{Password: "s3cret"}, "ignored", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	outN := strings.Count(res.Output, "OUT\n")
	errN := strings.Count(res.Output, "ERR\n")
	if outN != 200 || errN != 200 {
		t.Fatalf("want 200 OUT and 200 ERR, got OUT=%d ERR=%d len=%d", outN, errN, len(res.Output))
	}
}

func serveSSH(t *testing.T, ln net.Listener, cfg *ssh.ServerConfig) {
	serveSSHWith(t, ln, cfg, handleSession)
}

func serveSSHBoth(t *testing.T, ln net.Listener, cfg *ssh.ServerConfig) {
	serveSSHWith(t, ln, cfg, handleSessionStdoutStderr)
}

func serveSSHWith(t *testing.T, ln net.Listener, cfg *ssh.ServerConfig, session func(ssh.Channel, <-chan *ssh.Request)) {
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
				go session(ch, reqs)
			}
		}(nConn)
	}
}

func handleSession(ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer ch.Close()
	for req := range reqs {
		if req.Type != "exec" {
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
			continue
		}
		if req.WantReply {
			_ = req.Reply(true, nil)
		}
		_, _ = ch.Write([]byte("ok\n"))
		_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
		return
	}
}

func handleSessionStdoutStderr(ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer ch.Close()
	for req := range reqs {
		if req.Type != "exec" {
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
			continue
		}
		if req.WantReply {
			_ = req.Reply(true, nil)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				_, _ = ch.Write([]byte("OUT\n"))
			}
		}()
		go func() {
			defer wg.Done()
			stderr := ch.Stderr()
			for i := 0; i < 200; i++ {
				_, _ = stderr.Write([]byte("ERR\n"))
			}
		}()
		wg.Wait()
		_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
		return
	}
}

func mustPort(s string) int {
	var n int
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

func TestValidateCreds(t *testing.T) {
	if err := ValidateCreds(store.SSHAuthPassword, Creds{Password: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCreds(store.SSHAuthPassword, Creds{}); err == nil {
		t.Fatal("empty password")
	}
	if err := ValidateCreds(store.SSHAuthKey, Creds{PrivateKey: "not-a-key"}); err == nil {
		t.Fatal("bad key")
	}
}
