package tools

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/channels"
)

// Mutating Feishu office tools (complete / update / append) require a
// two-step confirm_token so the model cannot apply an edit in the same
// turn it proposed it. First call stores a pending action and returns
// the preview + token; the second call, after the user agrees, applies.

const feishuConfirmTTL = 15 * time.Minute

type feishuPending struct {
	AgentID   string
	Kind      string
	Preview   string
	Expires   time.Time
	TaskGUID  string
	Patch     channels.FeishuTaskPatch
	DocID     string
	DocTitle  string
	DocAppend string
}

var (
	feishuPendingMu sync.Mutex
	feishuPendings  = map[string]feishuPending{}
)

func feishuNewConfirmToken() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func feishuStorePending(p feishuPending) string {
	feishuPendingMu.Lock()
	defer feishuPendingMu.Unlock()
	now := time.Now()
	for k, v := range feishuPendings {
		if now.After(v.Expires) {
			delete(feishuPendings, k)
		}
	}
	tok := feishuNewConfirmToken()
	p.Expires = now.Add(feishuConfirmTTL)
	feishuPendings[tok] = p
	return tok
}

func feishuTakePending(agentID, token string) (feishuPending, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return feishuPending{}, fmt.Errorf("confirm_token required — call this tool once without it, show the preview, and only retry after the user agrees")
	}
	feishuPendingMu.Lock()
	defer feishuPendingMu.Unlock()
	p, ok := feishuPendings[token]
	if !ok {
		return feishuPending{}, fmt.Errorf("confirm_token is invalid or already used")
	}
	if time.Now().After(p.Expires) {
		delete(feishuPendings, token)
		return feishuPending{}, fmt.Errorf("confirm_token expired — call the tool again without a token to get a new preview")
	}
	if p.AgentID != "" && agentID != "" && p.AgentID != agentID {
		return feishuPending{}, fmt.Errorf("confirm_token does not belong to this agent")
	}
	delete(feishuPendings, token)
	return p, nil
}

func feishuConfirmPrompt(preview, token string) string {
	return preview + "\n\nNOT APPLIED. Show this preview to the user and ask them to confirm. " +
		"Do not call this tool again until they explicitly agree. " +
		"When they do, call again with confirm_token=" + token + " (same other arguments). " +
		"Do not invent a token."
}
