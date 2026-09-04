package sshhosts

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/fastclaw-ai/fastclaw/internal/store"
)

const maxOutputBytes = 64 * 1024

// Result is one remote command execution.
type Result struct {
	Output        string
	ExitCode      int
	PinnedHostKey string // set when TOFU pins a new host key
}

// Run executes command on host using the saved credential. The local
// process never puts the password on a command line.
func Run(ctx context.Context, host store.SSHHostRecord, creds Creds, command string, timeout time.Duration) (Result, error) {
	var zero Result
	if strings.TrimSpace(command) == "" {
		return zero, errors.New("command is required")
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	auth, err := authMethods(host.AuthType, creds)
	if err != nil {
		return zero, err
	}

	var pinned string
	cfg := &ssh.ClientConfig{
		User:            host.Username,
		Auth:            auth,
		HostKeyCallback: hostKeyCallback(host.HostKey, &pinned),
		Timeout:         15 * time.Second,
	}
	addr := net.JoinHostPort(host.Host, fmt.Sprintf("%d", host.Port))
	if host.Port <= 0 {
		addr = net.JoinHostPort(host.Host, "22")
	}

	dialer := &net.Dialer{Timeout: 15 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return zero, fmt.Errorf("dial %s: %w", addr, err)
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return zero, fmt.Errorf("ssh handshake: %w", err)
	}
	client := ssh.NewClient(c, chans, reqs)
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return zero, fmt.Errorf("ssh session: %w", err)
	}
	defer session.Close()

	if cwd := strings.TrimSpace(host.DefaultCWD); cwd != "" {
		command = "cd " + shellQuote(cwd) + " && " + command
	}

	// crypto/ssh copies stdout and stderr from different goroutines.
	// bytes.Buffer is not safe for concurrent writes, so a mutex is
	// required or remote output is silently dropped.
	var out combinedStream
	session.Stdout = &out
	session.Stderr = &out

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- session.Run(command) }()

	var runErr error
	select {
	case <-runCtx.Done():
		_ = session.Close()
		runErr = fmt.Errorf("command timed out after %s", timeout)
	case runErr = <-errCh:
	}

	outStr := out.String()
	if len(outStr) > maxOutputBytes {
		outStr = outStr[:maxOutputBytes] + "\n…(output truncated)"
	}
	res := Result{Output: outStr, PinnedHostKey: pinned}
	if runErr == nil {
		return res, nil
	}
	var exitErr *ssh.ExitError
	if errors.As(runErr, &exitErr) {
		res.ExitCode = exitErr.ExitStatus()
		return res, nil
	}
	if res.Output != "" {
		return res, fmt.Errorf("%s\nError: %s", res.Output, runErr.Error())
	}
	return res, runErr
}

func authMethods(authType string, creds Creds) ([]ssh.AuthMethod, error) {
	switch authType {
	case store.SSHAuthPassword:
		if creds.Password == "" {
			return nil, errors.New("password is empty")
		}
		return []ssh.AuthMethod{
			ssh.Password(creds.Password),
			ssh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range questions {
					answers[i] = creds.Password
				}
				return answers, nil
			}),
		}, nil
	case store.SSHAuthKey:
		if creds.PrivateKey == "" {
			return nil, errors.New("private key is empty")
		}
		var signer ssh.Signer
		var err error
		if creds.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(creds.PrivateKey), []byte(creds.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(creds.PrivateKey))
		}
		if err != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	default:
		return nil, fmt.Errorf("unknown auth type %q", authType)
	}
}

func hostKeyCallback(pinned string, captured *string) ssh.HostKeyCallback {
	pinned = strings.TrimSpace(pinned)
	if pinned != "" {
		want, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pinned))
		if err == nil {
			return ssh.FixedHostKey(want)
		}
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if captured != nil {
			*captured = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
		}
		return nil
	}
}

// ValidateCreds parses the credential so a bad key is rejected at save time.
func ValidateCreds(authType string, creds Creds) error {
	_, err := authMethods(authType, creds)
	return err
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// combinedStream merges stdout and stderr. crypto/ssh copies those
// streams from different goroutines, so writes must be serialized.
type combinedStream struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *combinedStream) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(p)
}

func (c *combinedStream) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}
