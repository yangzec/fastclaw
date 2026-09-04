package sshhosts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/fastclaw-ai/fastclaw/internal/store"
)

const (
	// DefaultIdleTimeout closes a reused SSH client after this much
	// quiet time so a forgotten GPU box does not stay logged in forever.
	DefaultIdleTimeout = 2 * time.Hour
	defaultKeepAlive   = 30 * time.Second
)

// Pool reuses *ssh.Client connections so ssh_exec does not handshake
// on every command. Idle clients are dropped after IdleTimeout.
type Pool struct {
	mu        sync.Mutex
	conns     map[string]*pooledConn
	keepAlive time.Duration
}

type pooledConn struct {
	pool     *Pool
	key      string
	client   *ssh.Client
	mu       sync.Mutex
	inUse    int
	lastUsed time.Time
	closed   bool
	tmuxOK   bool
	idle     *time.Timer
	stopKeep chan struct{}
}

// ConnInfo is the in-memory status of a pooled SSH client.
type ConnInfo struct {
	Connected bool
	LastUsed  time.Time
}

// DefaultPool is process-wide so the Settings Test button and the
// agent's ssh_exec share the same live sessions.
var DefaultPool = NewPool()

// NewPool returns an empty connection pool.
func NewPool() *Pool {
	return &Pool{
		conns:     make(map[string]*pooledConn),
		keepAlive: defaultKeepAlive,
	}
}

// ResetPool closes every pooled client. Tests call this so keepalive
// goroutines do not leak across cases.
func ResetPool() {
	old := DefaultPool
	DefaultPool = NewPool()
	old.CloseAll()
}

// ConnKey identifies a pooled client. Host ID is preferred so a rename
// keeps the session; tests without an ID fall back to user@host:port.
func ConnKey(h store.SSHHostRecord) string {
	if id := strings.TrimSpace(h.ID); id != "" {
		return id
	}
	port := h.Port
	if port <= 0 {
		port = 22
	}
	return fmt.Sprintf("%s@%s:%d", h.Username, h.Host, port)
}

func poolKey(h store.SSHHostRecord, creds Creds) string {
	k := ConnKey(h)
	if strings.TrimSpace(h.ID) != "" {
		return k
	}
	sum := sha256.Sum256([]byte(creds.Password + "\x00" + creds.PrivateKey + "\x00" + creds.Passphrase))
	return k + "#" + hex.EncodeToString(sum[:8])
}

// TmuxSessionName is the detached tmux session FastClaw keeps on the
// remote host so a human can `tmux attach` while the agent works.
func TmuxSessionName(alias string) string {
	alias = strings.ToLower(strings.TrimSpace(alias))
	if alias == "" {
		return "fastclaw"
	}
	return "fastclaw-" + alias
}

// IdleTimeout is how long a quiet connection stays up. 0 means the
// 2h default; negative means keep until FastClaw exits.
func IdleTimeout(h store.SSHHostRecord) time.Duration {
	switch {
	case h.IdleTimeoutSec < 0:
		return 0
	case h.IdleTimeoutSec == 0:
		return DefaultIdleTimeout
	default:
		return time.Duration(h.IdleTimeoutSec) * time.Second
	}
}

// Info reports whether the pool currently holds a live client for key.
func (p *Pool) Info(key string) ConnInfo {
	if p == nil || key == "" {
		return ConnInfo{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	c := p.conns[key]
	if c == nil || c.closed {
		return ConnInfo{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ConnInfo{}
	}
	return ConnInfo{Connected: true, LastUsed: c.lastUsed}
}

// Drop closes and forgets the pooled client for key.
func (p *Pool) Drop(key string) {
	if p == nil || key == "" {
		return
	}
	p.mu.Lock()
	c := p.conns[key]
	delete(p.conns, key)
	p.mu.Unlock()
	if c != nil {
		c.shutdown()
	}
}

// CloseAll drops every pooled client.
func (p *Pool) CloseAll() {
	if p == nil {
		return
	}
	p.mu.Lock()
	all := p.conns
	p.conns = make(map[string]*pooledConn)
	p.mu.Unlock()
	for _, c := range all {
		c.shutdown()
	}
}

// Run executes command, reusing a live SSH client when one exists.
func (p *Pool) Run(ctx context.Context, host store.SSHHostRecord, creds Creds, command string, timeout time.Duration) (Result, error) {
	var zero Result
	if strings.TrimSpace(command) == "" {
		return zero, errors.New("command is required")
	}
	if p == nil {
		p = DefaultPool
	}
	res, err := p.run(ctx, host, creds, command, timeout, true)
	if err != nil && isDeadConn(err) {
		p.Drop(poolKey(host, creds))
		return p.run(ctx, host, creds, command, timeout, false)
	}
	return res, err
}

func (p *Pool) run(ctx context.Context, host store.SSHHostRecord, creds Creds, command string, timeout time.Duration, retry bool) (Result, error) {
	var zero Result
	key := poolKey(host, creds)
	c, pinned, err := p.acquire(ctx, host, creds, key)
	if err != nil {
		return zero, err
	}
	defer c.release(IdleTimeout(host))

	if host.PersistTmux {
		c.ensureTmux(ctx, host)
	}

	res, err := runOnClient(ctx, c.client, wrapCommandCWD(host, command), timeout)
	if pinned != "" {
		res.PinnedHostKey = pinned
	}
	if err != nil && retry && isDeadConn(err) {
		return zero, err
	}
	return res, err
}

func (p *Pool) acquire(ctx context.Context, host store.SSHHostRecord, creds Creds, key string) (*pooledConn, string, error) {
	p.mu.Lock()
	if c := p.conns[key]; c != nil {
		p.mu.Unlock()
		if c.tryAcquire() {
			return c, "", nil
		}
		p.Drop(key)
	} else {
		p.mu.Unlock()
	}

	client, pinned, err := dialSSH(ctx, host, creds)
	if err != nil {
		return nil, "", err
	}
	c := &pooledConn{
		pool:     p,
		key:      key,
		client:   client,
		lastUsed: time.Now(),
		inUse:    1,
		stopKeep: make(chan struct{}),
	}
	p.mu.Lock()
	if existing := p.conns[key]; existing != nil && existing.tryAcquire() {
		p.mu.Unlock()
		_ = client.Close()
		return existing, "", nil
	}
	p.conns[key] = c
	p.mu.Unlock()
	go c.keepalive(p.keepAlive)
	go c.waitClosed()
	return c, pinned, nil
}

func (c *pooledConn) tryAcquire() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	c.inUse++
	if c.idle != nil {
		c.idle.Stop()
	}
	return true
}

func (c *pooledConn) release(idle time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.inUse > 0 {
		c.inUse--
	}
	c.lastUsed = time.Now()
	if c.closed || c.inUse > 0 {
		return
	}
	if idle <= 0 {
		if c.idle != nil {
			c.idle.Stop()
			c.idle = nil
		}
		return
	}
	key, pool := c.key, c.pool
	if c.idle == nil {
		c.idle = time.AfterFunc(idle, func() { pool.Drop(key) })
		return
	}
	c.idle.Reset(idle)
}

func (c *pooledConn) shutdown() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	if c.idle != nil {
		c.idle.Stop()
	}
	stop := c.stopKeep
	client := c.client
	c.mu.Unlock()
	if stop != nil {
		select {
		case <-stop:
		default:
			close(stop)
		}
	}
	if client != nil {
		_ = client.Close()
	}
}

func (c *pooledConn) keepalive(every time.Duration) {
	if every <= 0 {
		return
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-c.stopKeep:
			return
		case <-ticker.C:
			_, _, err := c.client.SendRequest("keepalive@openssh.com", true, nil)
			if err != nil {
				c.pool.Drop(c.key)
				return
			}
		}
	}
}

func (c *pooledConn) waitClosed() {
	_ = c.client.Wait()
	c.pool.Drop(c.key)
}

func (c *pooledConn) ensureTmux(ctx context.Context, host store.SSHHostRecord) {
	c.mu.Lock()
	if c.tmuxOK || c.closed {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	name := TmuxSessionName(host.Name)
	cmd := "command -v tmux >/dev/null 2>&1 || exit 0; tmux has-session -t " +
		shellQuote(name) + " 2>/dev/null || tmux new-session -d -s " + shellQuote(name)
	if cwd := strings.TrimSpace(host.DefaultCWD); cwd != "" {
		cmd += " -c " + shellQuote(cwd)
	}
	_, _ = runOnClient(ctx, c.client, cmd, 15*time.Second)

	c.mu.Lock()
	c.tmuxOK = true
	c.mu.Unlock()
}

func isDeadConn(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	for _, needle := range []string{
		"closed",
		"reset",
		"EOF",
		"broken pipe",
		"connection lost",
		"ssh: discarded",
	} {
		if strings.Contains(strings.ToLower(s), strings.ToLower(needle)) {
			return true
		}
	}
	return false
}
