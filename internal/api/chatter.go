package api

import (
	"fmt"
	"strings"
)

// ChatterHeader is the per-request chatter for USER.md / MEMORY.md /
// Auto-remember routing. Unlike X-Fastclaw-End-User, it does not switch
// the request UserSpace or billing bucket.
const ChatterHeader = "X-Fastclaw-Chatter"

const (
	defaultAPIChatter = "api-user"
	chatterParamKey   = "user_id"
)

// resolveAPIChatter picks InboundMessage.UserID for /v1/chat/completions.
//
//	both absent          → "api-user" (legacy API callers)
//	only header          → trimmed header
//	only params.user_id  → trimmed string
//	both, same           → that value
//	both, different      → error (do not silently pick one)
//	params.user_id set but not a string → error
//
// OpenAI body `user` / X-Fastclaw-End-User are not consulted.
func resolveAPIChatter(header string, params map[string]any) (string, error) {
	h := strings.TrimSpace(header)
	p, pSet, err := chatterFromParams(params)
	if err != nil {
		return "", err
	}
	hSet := h != ""
	switch {
	case hSet && pSet && h != p:
		return "", fmt.Errorf("%s and params.%s do not match", ChatterHeader, chatterParamKey)
	case hSet:
		return h, nil
	case pSet:
		return p, nil
	default:
		return defaultAPIChatter, nil
	}
}

func chatterFromParams(params map[string]any) (id string, set bool, err error) {
	if len(params) == 0 {
		return "", false, nil
	}
	raw, ok := params[chatterParamKey]
	if !ok || raw == nil {
		return "", false, nil
	}
	s, ok := raw.(string)
	if !ok {
		return "", true, fmt.Errorf("params.%s must be a string", chatterParamKey)
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false, nil
	}
	return s, true, nil
}
