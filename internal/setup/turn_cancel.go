package setup

import (
	"context"
	"sync"
)

// turnCancel is one registered Stop handle for an in-flight web turn.
// Pointer identity lets unregister ignore a newer turn that replaced it.
type turnCancel struct {
	cancel context.CancelFunc
}

// turnCancelRegistry maps (user, agent, session) to the cancel func of
// the currently running web/team chat turn. Refresh detaches the HTTP
// handler but leaves this entry so Stop still works after reload.
type turnCancelRegistry struct {
	mu sync.Mutex
	m  map[string]*turnCancel
}

func turnCancelKey(userID, agentID, sessionID string) string {
	return userID + "\x00" + agentID + "\x00" + sessionID
}

func (r *turnCancelRegistry) register(key string, h *turnCancel) {
	if r == nil || h == nil || key == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.m == nil {
		r.m = make(map[string]*turnCancel)
	}
	r.m[key] = h
}

func (r *turnCancelRegistry) unregister(key string, h *turnCancel) {
	if r == nil || h == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.m[key] == h {
		delete(r.m, key)
	}
}

func (r *turnCancelRegistry) cancel(key string) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	h := r.m[key]
	r.mu.Unlock()
	if h == nil || h.cancel == nil {
		return false
	}
	h.cancel()
	return true
}
