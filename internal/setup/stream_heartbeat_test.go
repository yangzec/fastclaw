package setup

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestForwardHeartbeatWritesDataEventNotSSEComment(t *testing.T) {
	rr := httptest.NewRecorder()
	forwardHeartbeatEvent(rr, rr)

	body := rr.Body.String()
	if strings.Contains(body, ": ping") {
		t.Fatalf("heartbeat body contains SSE comment ping: %q", body)
	}
	if !strings.HasPrefix(body, "data: ") {
		t.Fatalf("heartbeat body = %q, want data event", body)
	}
	payload := strings.TrimPrefix(strings.TrimSpace(body), "data: ")
	var evt map[string]any
	if err := json.Unmarshal([]byte(payload), &evt); err != nil {
		t.Fatalf("heartbeat payload is not JSON: %v body=%q", err, body)
	}
	if evt["type"] != "heartbeat" {
		t.Fatalf("heartbeat type = %v, want heartbeat", evt["type"])
	}
}
