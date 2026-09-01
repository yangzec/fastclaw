package provider

import "strings"

// sseDataPayload returns the payload of an SSE `data:` line.
//
// The WHATWG / MDN event-stream format treats the space after the colon
// as optional: "If the value contains a leading space, it is ignored…
// the colon and space are both optional." Many OpenAI-compatible
// gateways (DeepSeek, GLM, self-hosted proxies) emit `data:{...}` with
// no space. Matching the literal prefix `data: ` then skips every
// event and the assembled reply is empty.
func sseDataPayload(line string) (payload string, ok bool) {
	if !strings.HasPrefix(line, "data:") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(line, "data:")), true
}
