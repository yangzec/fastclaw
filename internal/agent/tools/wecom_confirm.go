package tools

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

const wecomConfirmTTL = 15 * time.Minute

type wecomPending struct {
	AgentID   string
	Kind      string // "cancel_schedule" | "append_doc"
	Preview   string
	Expires   time.Time
	SchedID   string
	DocID     string
	DocAppend string
}

var (
	wecomPendingMu sync.Mutex
	wecomPendings  = map[string]wecomPending{}
)

func wecomNewConfirmToken() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func wecomStorePending(p wecomPending) string {
	wecomPendingMu.Lock()
	defer wecomPendingMu.Unlock()
	now := time.Now()
	for k, v := range wecomPendings {
		if now.After(v.Expires) {
			delete(wecomPendings, k)
		}
	}
	tok := wecomNewConfirmToken()
	p.Expires = now.Add(wecomConfirmTTL)
	wecomPendings[tok] = p
	return tok
}

func wecomTakePending(agentID, token string) (wecomPending, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return wecomPending{}, fmt.Errorf("confirm_token required — call this tool once without it, show the preview, and only retry after the user agrees")
	}
	wecomPendingMu.Lock()
	defer wecomPendingMu.Unlock()
	p, ok := wecomPendings[token]
	if !ok {
		return wecomPending{}, fmt.Errorf("confirm_token is invalid or already used")
	}
	if time.Now().After(p.Expires) {
		delete(wecomPendings, token)
		return wecomPending{}, fmt.Errorf("confirm_token expired — call the tool again without a token to get a new preview")
	}
	if p.AgentID != "" && agentID != "" && p.AgentID != agentID {
		return wecomPending{}, fmt.Errorf("confirm_token does not belong to this agent")
	}
	delete(wecomPendings, token)
	return p, nil
}

func wecomConfirmPrompt(preview, token string) string {
	return preview + "\n\nNOT APPLIED. Show this preview to the user and ask them to confirm. " +
		"Do not call this tool again until they explicitly agree. " +
		"When they do, call again with confirm_token=" + token + " (same other arguments). " +
		"Do not invent a token."
}
