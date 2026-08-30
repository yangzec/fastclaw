package api

import (
	"net/http"
	"strings"

	"github.com/fastclaw-ai/fastclaw/internal/bus"
)

// HandleV2ListSessionMessages returns durable V2 conversation history using
// the public session ID. Local-only artifact references are removed from
// assistant text; clients obtain files from the sibling V2 file endpoints.
func (s *Server) HandleV2ListSessionMessages(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("agentId")
	ag, err := s.v2AgentForRequest(r, agentID)
	if err != nil {
		writeV2AgentError(w, err)
		return
	}
	sessionID := r.PathValue("sessionId")
	if !v2SessionIDPattern.MatchString(sessionID) {
		writeV2Error(w, http.StatusBadRequest, "invalid sessionId")
		return
	}
	userID, ok := v2RequestUserID(r)
	if !ok {
		writeV2Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	nativeSessionID := ag.NativeSessionKey(bus.InboundMessage{
		Channel:  "api-v2",
		ChatID:   v2WorkspaceSessionID(userID, sessionID),
		UserID:   userID,
		PeerKind: "dm",
		AgentID:  ag.Name(),
	})
	if nativeSessionID == "" {
		writeV2Error(w, http.StatusInternalServerError, "failed to resolve session")
		return
	}
	messages := v2ConversationMessages(ag.WebChatHistory(nativeSessionID))
	writeJSON(w, http.StatusOK, map[string]any{
		"agentId": agentID, "sessionId": sessionID, "messages": messages,
	})
}

func v2ConversationMessages(history []map[string]any) []map[string]any {
	messages := make([]map[string]any, 0, len(history))
	for _, message := range history {
		role, _ := message["role"].(string)
		content, _ := message["content"].(string)
		switch role {
		case "user":
			entry := map[string]any{"role": "user", "content": content}
			if imageURLs, exists := message["imageUrls"]; exists {
				entry["imageUrls"] = imageURLs
			}
			if strings.TrimSpace(content) != "" || entry["imageUrls"] != nil {
				messages = append(messages, entry)
			}
		case "assistant":
			content = sanitizeV2Message(content)
			if content != "" {
				messages = append(messages, map[string]any{
					"role": "assistant", "content": content,
				})
			}
		}
	}
	return messages
}
