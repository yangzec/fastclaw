package channels

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/bus"
)

func TestWeComParseMixedUsesOfficialMsgItem(t *testing.T) {
	raw := json.RawMessage(`{
		"msg_item": [
			{"msgtype":"text","text":{"content":"@机器人 看图"}},
			{"msgtype":"image","image":{"url":"https://example.test/img","aeskey":"k"}}
		]
	}`)
	text, assets := wecomParseMixed(raw)
	if text != "@机器人 看图" {
		t.Fatalf("text = %q", text)
	}
	if len(assets) != 1 || assets[0].URL != "https://example.test/img" {
		t.Fatalf("assets = %#v", assets)
	}
}

func TestWeComParseMixedIncludesVideo(t *testing.T) {
	raw := json.RawMessage(`{
		"msg_item": [
			{"msgtype":"video","video":{"url":"https://example.test/vid","aeskey":"k","filename":"clip.mp4"}}
		]
	}`)
	text, assets := wecomParseMixed(raw)
	if text != "" || len(assets) != 1 || assets[0].URL != "https://example.test/vid" || assets[0].Name != "clip.mp4" {
		t.Fatalf("text=%q assets=%#v", text, assets)
	}
}

func TestWeComInboundPayloadFileAndImage(t *testing.T) {
	img := wecomCallbackBody{MsgType: "image"}
	img.Image.URL = "https://example.test/a"
	text, assets := wecomInboundPayload(img)
	if text != "" || len(assets) != 1 {
		t.Fatalf("image payload text=%q assets=%#v", text, assets)
	}

	file := wecomCallbackBody{MsgType: "file"}
	file.File.URL = "https://example.test/b"
	file.File.Name = "report.pdf"
	_, assets = wecomInboundPayload(file)
	if len(assets) != 1 || assets[0].Name != "report.pdf" {
		t.Fatalf("file assets = %#v", assets)
	}
}

func TestWeComDecryptMediaRoundTrip(t *testing.T) {
	key := bytes.Repeat([]byte("k"), 32)
	plain := []byte("hello-wecom-inbound")
	ct := wecomEncryptForTest(t, key, plain)
	got, err := wecomDecryptMedia(hex.EncodeToString(key), ct)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("got %q", got)
	}
}

func TestDownloadWeComAssetDecrypts(t *testing.T) {
	key := bytes.Repeat([]byte("s"), 32)
	plain := []byte("%PDF-wecom")
	ct := wecomEncryptForTest(t, key, plain)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(ct)
	}))
	t.Cleanup(srv.Close)

	item, err := downloadWeComAsset(wecomEncryptedAsset{
		URL:    srv.URL,
		AESKey: hex.EncodeToString(key),
		Name:   "report.pdf",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if item.Filename != "report.pdf" || !bytes.Equal(item.Bytes, plain) {
		t.Fatalf("item = %#v", item)
	}
}

func TestWeComGroupMentionsUsesBotID(t *testing.T) {
	if got := wecomGroupMentions("botA"); len(got) != 1 || got[0] != "botA" {
		t.Fatalf("got %#v", got)
	}
	if wecomGroupMentions("  ") != nil {
		t.Fatal("empty bot id should yield no mentions")
	}
}

func TestWeComHandleMsgCallbackGroupSetsMentions(t *testing.T) {
	mb := bus.New()
	w := &WeCom{bus: mb, accountID: "botA", botID: "botA", lastReq: map[string]string{}, chatType: map[string]int{}, streamID: map[string]string{}}
	body, _ := json.Marshal(map[string]any{
		"msgid":    "m-group",
		"chatid":   "wr_group",
		"chattype": "group",
		"from":     map[string]string{"userid": "u2"},
		"msgtype":  "text",
		"text":     map[string]string{"content": "@机器人 你好"},
	})
	w.handleMsgCallback(wecomFrame{Headers: wecomHeaders{ReqID: "req-g"}, Body: body})
	got := <-mb.Inbound
	if got.PeerKind != "group" || got.ChatID != "wr_group" {
		t.Fatalf("inbound = %+v", got)
	}
	if len(got.Mentions) != 1 || got.Mentions[0] != "botA" {
		t.Fatalf("mentions = %#v, want [botA] so mention-only routing replies", got.Mentions)
	}
}

func TestWeComHandleMsgCallbackForwardsFile(t *testing.T) {
	key := bytes.Repeat([]byte("f"), 32)
	plain := []byte("file-bytes")
	ct := wecomEncryptForTest(t, key, plain)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(ct)
	}))
	t.Cleanup(srv.Close)

	mb := bus.New()
	w := &WeCom{bus: mb, accountID: "bot", lastReq: map[string]string{}, chatType: map[string]int{}, streamID: map[string]string{}}
	body, _ := json.Marshal(map[string]any{
		"msgid":    "m-file",
		"chattype": "single",
		"from":     map[string]string{"userid": "u1"},
		"msgtype":  "file",
		"file": map[string]string{
			"url":      srv.URL,
			"aeskey":   hex.EncodeToString(key),
			"filename": "a.pdf",
		},
	})
	w.handleMsgCallback(wecomFrame{Headers: wecomHeaders{ReqID: "req-1"}, Body: body})
	got := <-mb.Inbound
	if got.Channel != "wecom" || got.ChatID != "u1" || len(got.MediaItems) != 1 {
		t.Fatalf("inbound = %+v", got)
	}
	if string(got.MediaItems[0].Bytes) != "file-bytes" || got.MediaItems[0].Filename != "a.pdf" {
		t.Fatalf("media = %#v", got.MediaItems)
	}
}

func wecomEncryptForTest(t *testing.T, key, plain []byte) []byte {
	t.Helper()
	padded := wecomPKCS7Pad(plain)
	block, err := aes.NewCipher(key[:32])
	if err != nil {
		t.Fatal(err)
	}
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, key[:aes.BlockSize]).CryptBlocks(out, padded)
	return out
}
