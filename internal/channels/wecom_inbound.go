package channels

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/fastclaw-ai/fastclaw/internal/bus"
)

func wecomInboundPayload(body wecomCallbackBody) (string, []wecomEncryptedAsset) {
	switch body.MsgType {
	case "text":
		return strings.TrimSpace(body.Text.Content), nil
	case "voice":
		return strings.TrimSpace(body.Voice.Content), nil
	case "image":
		return "", wecomAssets(body.Image)
	case "file":
		return "", wecomAssets(body.File)
	case "video":
		return "", wecomAssets(body.Video)
	case "mixed":
		return wecomParseMixed(body.Mixed)
	default:
		return "", nil
	}
}

func wecomAssets(a wecomEncryptedAsset) []wecomEncryptedAsset {
	if strings.TrimSpace(a.URL) == "" {
		return nil
	}
	return []wecomEncryptedAsset{a}
}

func wecomParseMixed(raw json.RawMessage) (string, []wecomEncryptedAsset) {
	if len(raw) == 0 {
		return "", nil
	}
	var parsed struct {
		MsgItem []wecomMixedItem `json:"msg_item"`
		Item    []wecomMixedItem `json:"item"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", nil
	}
	items := parsed.MsgItem
	if len(items) == 0 {
		items = parsed.Item
	}
	var parts []string
	var assets []wecomEncryptedAsset
	for _, it := range items {
		kind := it.MsgType
		if kind == "" {
			kind = it.Type
		}
		if s := strings.TrimSpace(it.Text.Content); s != "" {
			parts = append(parts, s)
		}
		if kind == "image" || it.Image.URL != "" {
			assets = append(assets, wecomAssets(it.Image)...)
		}
		if kind == "file" || it.File.URL != "" {
			assets = append(assets, wecomAssets(it.File)...)
		}
		if kind == "video" || it.Video.URL != "" {
			assets = append(assets, wecomAssets(it.Video)...)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n")), assets
}

type wecomMixedItem struct {
	MsgType string `json:"msgtype"`
	Type    string `json:"type"`
	Text    struct {
		Content string `json:"content"`
	} `json:"text"`
	Image wecomEncryptedAsset `json:"image"`
	File  wecomEncryptedAsset `json:"file"`
	Video wecomEncryptedAsset `json:"video"`
}

func downloadWeComAssets(assets []wecomEncryptedAsset) []bus.MediaItem {
	var out []bus.MediaItem
	for i, a := range assets {
		item, err := downloadWeComAsset(a, i)
		if err != nil {
			slog.Warn("wecom inbound media download failed", "url", truncate(a.URL, 80), "error", err)
			continue
		}
		out = append(out, item)
	}
	return out
}

func downloadWeComAsset(a wecomEncryptedAsset, idx int) (bus.MediaItem, error) {
	enc, ctype, err := fetchInboundBytes(a.URL, nil)
	if err != nil {
		return bus.MediaItem{}, err
	}
	data := enc
	if strings.TrimSpace(a.AESKey) != "" {
		plain, derr := wecomDecryptMedia(a.AESKey, enc)
		if derr != nil {
			return bus.MediaItem{}, derr
		}
		data = plain
	}
	name := strings.TrimSpace(a.Name)
	if name == "" {
		name = wecomInboundFilename(idx, ctype, data)
	}
	if ctype == "" {
		ctype = http.DetectContentType(data)
	}
	return bus.MediaItem{Filename: name, ContentType: ctype, Bytes: data}, nil
}

func wecomInboundFilename(idx int, ctype string, data []byte) string {
	if ctype == "" {
		ctype = http.DetectContentType(data)
	}
	return fmt.Sprintf("wecom-%d%s", idx, mimeExtFromContentType(ctype))
}

func wecomDecryptMedia(aesKey string, ciphertext []byte) ([]byte, error) {
	key, err := wecomDecodeAESKey(aesKey)
	if err != nil {
		return nil, err
	}
	if len(key) < 16 {
		return nil, fmt.Errorf("wecom aeskey too short")
	}
	if len(key) < 32 {
		key = append(key, bytes.Repeat([]byte{0}, 32-len(key))...)
	}
	key = key[:32]
	if len(ciphertext) < aes.BlockSize || len(ciphertext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("wecom ciphertext length %d", len(ciphertext))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	iv := key[:aes.BlockSize]
	plain := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ciphertext)
	return wecomPKCS7Unpad(plain)
}

func wecomDecodeAESKey(raw string) ([]byte, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, fmt.Errorf("empty aeskey")
	}
	if b, err := hex.DecodeString(s); err == nil && (len(b) == 16 || len(b) == 32) {
		return b, nil
	}
	padded := s
	if m := len(padded) % 4; m != 0 {
		padded += strings.Repeat("=", 4-m)
	}
	if b, err := base64.StdEncoding.DecodeString(padded); err == nil && len(b) >= 16 {
		return b, nil
	}
	if len(s) == 16 || len(s) == 32 {
		return []byte(s), nil
	}
	return nil, fmt.Errorf("unrecognized aeskey encoding")
}
