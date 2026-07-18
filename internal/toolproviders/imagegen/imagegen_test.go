package imagegen

import "testing"

func TestStructuredResponsesCopySlices(t *testing.T) {
	urls := []string{"https://example.com/a.png"}
	resp := urlResponse("p", urls)
	urls[0] = "changed"
	out, ok := resp.Raw.(Output)
	if !ok || len(out.URLs) != 1 || out.URLs[0] != "https://example.com/a.png" {
		t.Fatalf("url raw output mismatch: %#v", resp.Raw)
	}
	if resp.Text == "" {
		t.Fatalf("url response text empty")
	}

	b64s := []string{"iVBORw0KGgo="}
	resp = base64Response("p", b64s)
	b64s[0] = "changed"
	out, ok = resp.Raw.(Output)
	if !ok || len(out.Base64) != 1 || out.Base64[0] != "iVBORw0KGgo=" {
		t.Fatalf("base64 raw output mismatch: %#v", resp.Raw)
	}
	if resp.Text == "" {
		t.Fatalf("base64 response text empty")
	}
}
