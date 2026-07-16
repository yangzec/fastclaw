package channels

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/bus"
)

func TestWeChatDispatchInboundImageURLForwardsPhotoURLs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("\x89PNG\r\n\x1a\nPNGDATA"))
	}))
	defer srv.Close()

	mb := bus.New()
	w := &WeChat{bus: mb, accountID: "acct", httpClient: srv.Client(), ctxTokens: make(map[string]string)}
	w.dispatchInbound(wechatMessage{
		MessageID:    123,
		FromUserID:   "user1",
		MessageType:  wechatMsgTypeUser,
		MessageState: wechatMsgStateFinish,
		ItemList: []wechatItem{{
			Type:      wechatItemTypeImage,
			ImageItem: &wechatImageItem{URL: srv.URL + "/img.png"},
		}},
	})

	select {
	case got := <-mb.Inbound:
		if got.Text == "" {
			t.Fatalf("Text is empty; image-only messages need a default vision prompt")
		}
		if len(got.PhotoURLs) != 1 {
			t.Fatalf("PhotoURLs len = %d, want 1", len(got.PhotoURLs))
		}
		if !strings.HasPrefix(got.PhotoURLs[0], "data:image/png;base64,") {
			t.Fatalf("PhotoURLs[0] = %q, want data:image/png base64 URL", got.PhotoURLs[0])
		}
	case <-time.After(time.Second):
		t.Fatal("no inbound message dispatched")
	}
}

func TestWeChatDispatchInboundImageUploadsWhenConfigured(t *testing.T) {
	imageBytes := []byte("\x89PNG\r\n\x1a\nPNGDATA")
	imageSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(imageBytes)
	}))
	defer imageSrv.Close()

	uploadURL := "https://cdn.example/images/wechat.png"
	uploadSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("upload method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "image/png" {
			t.Fatalf("upload Content-Type = %q, want image/png", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(body, imageBytes) {
			t.Fatalf("upload body = %q, want original image bytes", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"url":"` + uploadURL + `"}`))
	}))
	defer uploadSrv.Close()
	t.Setenv(wechatImageUploadURLEnv, uploadSrv.URL)

	mb := bus.New()
	w := &WeChat{bus: mb, accountID: "acct", httpClient: imageSrv.Client(), ctxTokens: make(map[string]string)}
	w.dispatchInbound(wechatMessage{
		MessageID:    126,
		FromUserID:   "user1",
		MessageType:  wechatMsgTypeUser,
		MessageState: wechatMsgStateFinish,
		ItemList: []wechatItem{{
			Type:      wechatItemTypeImage,
			ImageItem: &wechatImageItem{URL: imageSrv.URL + "/img.png"},
		}},
	})

	select {
	case got := <-mb.Inbound:
		if len(got.PhotoURLs) != 1 || got.PhotoURLs[0] != uploadURL {
			t.Fatalf("PhotoURLs = %#v, want upload URL", got.PhotoURLs)
		}
	case <-time.After(time.Second):
		t.Fatal("no inbound message dispatched")
	}
}

func TestWeChatDispatchInboundEncryptedCDNImageForwardsPhotoURLs(t *testing.T) {
	plain := []byte("\x89PNG\r\n\x1a\nPNGDATA")
	key := []byte("1234567890abcdef")
	encrypted, err := wechatAESECBEncrypt(plain, key)
	if err != nil {
		t.Fatal(err)
	}

	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "novac2c.cdn.weixin.qq.com" || r.URL.Path != "/c2c/download" {
			t.Fatalf("unexpected CDN request URL: %s", r.URL.String())
		}
		if r.URL.Query().Get("encrypted_query_param") != "opaque-param" {
			t.Fatalf("encrypted_query_param = %q", r.URL.Query().Get("encrypted_query_param"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(string(encrypted))),
		}, nil
	})}

	mb := bus.New()
	w := &WeChat{bus: mb, accountID: "acct", httpClient: client, ctxTokens: make(map[string]string)}
	w.dispatchInbound(wechatMessage{
		MessageID:    124,
		FromUserID:   "user1",
		MessageType:  wechatMsgTypeUser,
		MessageState: wechatMsgStateFinish,
		ItemList: []wechatItem{{
			Type: wechatItemTypeImage,
			ImageItem: &wechatImageItem{Media: &wechatMediaInfo{
				EncryptQueryParam: "opaque-param",
				AESKey:            base64.StdEncoding.EncodeToString([]byte(hex.EncodeToString(key))),
				EncryptType:       wechatCDNEncryptType,
			}},
		}},
	})

	select {
	case got := <-mb.Inbound:
		if got.Text == "" {
			t.Fatalf("Text is empty; image-only messages need a default vision prompt")
		}
		if len(got.PhotoURLs) != 1 {
			t.Fatalf("PhotoURLs len = %d, want 1", len(got.PhotoURLs))
		}
		if !strings.HasPrefix(got.PhotoURLs[0], "data:image/png;base64,") {
			t.Fatalf("PhotoURLs[0] = %q, want data:image/png base64 URL", got.PhotoURLs[0])
		}
	case <-time.After(time.Second):
		t.Fatal("no inbound message dispatched")
	}
}

func TestWeChatDispatchInboundImageWithCaption(t *testing.T) {
	mb := bus.New()
	w := &WeChat{bus: mb, accountID: "acct", httpClient: http.DefaultClient, ctxTokens: make(map[string]string)}
	dataURL := "data:image/png;base64,QUJD"
	w.dispatchInbound(wechatMessage{
		MessageID:    125,
		FromUserID:   "user1",
		MessageType:  wechatMsgTypeUser,
		MessageState: wechatMsgStateFinish,
		ItemList: []wechatItem{
			{Type: wechatItemTypeText, TextItem: &wechatTextItem{Text: "这是什么？"}},
			{Type: wechatItemTypeImage, ImageItem: &wechatImageItem{URL: dataURL}},
		},
	})

	select {
	case got := <-mb.Inbound:
		if got.Text != "这是什么？" {
			t.Fatalf("Text = %q", got.Text)
		}
		if len(got.PhotoURLs) != 1 || got.PhotoURLs[0] != dataURL {
			t.Fatalf("PhotoURLs = %#v", got.PhotoURLs)
		}
	case <-time.After(time.Second):
		t.Fatal("no inbound message dispatched")
	}
}

func TestWeChatImageDataURLFromBytesCompressesLargeImages(t *testing.T) {
	buf := largePNG(t)

	dataURL, err := wechatImageDataURLFromBytes(buf.Bytes(), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(dataURL, "data:image/jpeg;base64,") {
		t.Fatalf("dataURL prefix = %.32q, want compressed jpeg", dataURL)
	}
	if len(dataURL) > 1_500_000 {
		t.Fatalf("dataURL len = %d, want <= 1.5MB to avoid API 413", len(dataURL))
	}
}

func TestWeChatOutboundMediaCompressesLargeGeneratedImages(t *testing.T) {
	buf := largePNG(t)
	item := bus.MediaItem{
		Filename:    "openai_codex_gpt-image-2-medium.png",
		ContentType: "image/png",
		Bytes:       buf.Bytes(),
	}

	got := prepareWeChatOutboundMedia(item)
	if got.ContentType != "image/jpeg" {
		t.Fatalf("ContentType = %q, want image/jpeg", got.ContentType)
	}
	if !strings.HasSuffix(got.Filename, ".jpg") {
		t.Fatalf("Filename = %q, want .jpg", got.Filename)
	}
	if len(got.Bytes) == 0 || len(got.Bytes) >= len(item.Bytes) {
		t.Fatalf("compressed size = %d, original = %d", len(got.Bytes), len(item.Bytes))
	}
	if cdnType, itemType := classifyWeChatMedia(got); cdnType != wechatCDNMediaTypeImage || itemType != wechatItemTypeImage {
		t.Fatalf("classify = (%d,%d), want image/image", cdnType, itemType)
	}
}

func largePNG(t *testing.T) bytes.Buffer {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2400, 1800))
	for y := 0; y < 1800; y++ {
		for x := 0; x < 2400; x++ {
			v := uint32(x*1103515245) ^ uint32(y*12345) ^ uint32((x+y)*2654435761)
			img.Set(x, y, color.RGBA{R: uint8(v), G: uint8(v >> 8), B: uint8(v >> 16), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
