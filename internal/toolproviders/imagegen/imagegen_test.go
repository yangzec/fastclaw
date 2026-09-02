package imagegen

import "testing"

func TestURLResponseKeepsStructuredOutput(t *testing.T) {
	resp := urlResponse("a cat", []string{"https://cdn.example/a.png"})
	out, ok := resp.Raw.(Output)
	if !ok {
		t.Fatalf("Raw should be Output, got %T", resp.Raw)
	}
	if len(out.URLs) != 1 || out.URLs[0] != "https://cdn.example/a.png" {
		t.Fatalf("urls = %#v", out.URLs)
	}
	if out.URLs[0] == "" || resp.Text == "" {
		t.Fatal("text and urls must both be populated")
	}
}

func TestBase64ResponseKeepsStructuredOutput(t *testing.T) {
	resp := base64Response("a cat", []string{"aaaa"})
	out, ok := resp.Raw.(Output)
	if !ok {
		t.Fatalf("Raw should be Output, got %T", resp.Raw)
	}
	if len(out.Base64) != 1 || out.Base64[0] != "aaaa" {
		t.Fatalf("base64 = %#v", out.Base64)
	}
}
