package channels

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/store"
)

func TestWeComVerifyCallbackURLRoundTrip(t *testing.T) {
	raw := []byte("0123456789abcdef0123456789abcdef")
	aesKey := strings.TrimRight(base64.StdEncoding.EncodeToString(raw), "=")
	creds := WeComCallbackCreds{
		Token:          "fastclawtoken",
		EncodingAESKey: aesKey,
		CorpID:         "ww_corp_1",
	}
	echo, err := wecomEncrypt(aesKey, creds.CorpID, "ping-echo")
	if err != nil {
		t.Fatal(err)
	}
	ts, nonce := "1710000000", "n1"
	sig := wecomSign(creds.Token, ts, nonce, echo)
	got, err := WeComVerifyCallbackURL(creds, sig, ts, nonce, echo)
	if err != nil {
		t.Fatal(err)
	}
	if got != "ping-echo" {
		t.Fatalf("got %q", got)
	}
	if _, err := WeComVerifyCallbackURL(creds, "deadbeef", ts, nonce, echo); err == nil {
		t.Fatal("expected bad signature")
	}
}

func TestWeComCallbackFromChannel(t *testing.T) {
	_, err := WeComCallbackFromChannel(&store.ChannelRecord{
		AccountID: "bot_1",
		Data: map[string]any{
			"accounts": map[string]any{
				"bot_1": map[string]any{"corpId": "ww"},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "Token") {
		t.Fatalf("want missing token, got %v", err)
	}
}
