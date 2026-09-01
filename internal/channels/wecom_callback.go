package channels

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/fastclaw-ai/fastclaw/internal/config"
	"github.com/fastclaw-ai/fastclaw/internal/store"
)

// Official 自建应用 receive-message URL verification (doc 10514 / path 90238).
// Used to unlock 企业可信IP when the enterprise has no 备案域名 for 可信域名.
// Chat still uses the AI-bot long-conn; this endpoint only answers WeCom's
// GET echostr challenge (and ACKs later POSTs with "success").
const wecomCallbackPadBlock = 32

// WeComCallbackCreds is Token + EncodingAESKey + CorpID from the 自建应用.
type WeComCallbackCreds struct {
	Token          string
	EncodingAESKey string
	CorpID         string
}

// WeComCallbackFromChannel reads receive-message creds off a wecom row.
func WeComCallbackFromChannel(ch *store.ChannelRecord) (WeComCallbackCreds, error) {
	if ch == nil {
		return WeComCallbackCreds{}, fmt.Errorf("wecom callback: channel missing")
	}
	cc := config.ChannelConfigFromData(ch.Data)
	acct, ok := cc.Accounts[ch.AccountID]
	if !ok {
		return WeComCallbackCreds{}, fmt.Errorf("wecom callback: account missing")
	}
	creds := WeComCallbackCreds{
		Token:          strings.TrimSpace(acct.CorpCallbackToken),
		EncodingAESKey: strings.TrimSpace(acct.CorpCallbackAESKey),
		CorpID:         strings.TrimSpace(acct.CorpID),
	}
	if creds.Token == "" || creds.EncodingAESKey == "" {
		return WeComCallbackCreds{}, fmt.Errorf("wecom callback: save Token and EncodingAESKey on the WeCom channel first")
	}
	if creds.CorpID == "" {
		return WeComCallbackCreds{}, fmt.Errorf("wecom callback: enable official calendar (CorpID) first")
	}
	return creds, nil
}

// WeComVerifyCallbackURL handles the official GET challenge.
// Returns the plaintext echostr that the HTTP handler must write as the body.
func WeComVerifyCallbackURL(creds WeComCallbackCreds, signature, timestamp, nonce, echostr string) (string, error) {
	if err := wecomCheckSignature(creds.Token, timestamp, nonce, echostr, signature); err != nil {
		return "", err
	}
	plain, receiveID, err := wecomDecrypt(creds.EncodingAESKey, echostr)
	if err != nil {
		return "", err
	}
	if receiveID != "" && creds.CorpID != "" && receiveID != creds.CorpID {
		return "", fmt.Errorf("wecom callback: receiveid %q does not match corpId", receiveID)
	}
	return string(plain), nil
}

func wecomCheckSignature(token, timestamp, nonce, echostr, want string) error {
	want = strings.TrimSpace(want)
	parts := []string{token, timestamp, nonce, echostr}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	got := fmt.Sprintf("%x", sum)
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("wecom callback: bad msg_signature")
	}
	return nil
}

func wecomAESKey(encodingAESKey string) ([]byte, error) {
	raw := strings.TrimSpace(encodingAESKey)
	if raw == "" {
		return nil, fmt.Errorf("wecom callback: EncodingAESKey required")
	}
	if !strings.HasSuffix(raw, "=") {
		raw += "="
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("wecom callback: EncodingAESKey: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("wecom callback: EncodingAESKey must decode to 32 bytes")
	}
	return key, nil
}

func wecomDecrypt(encodingAESKey, echostr string) (plain []byte, receiveID string, err error) {
	key, err := wecomAESKey(encodingAESKey)
	if err != nil {
		return nil, "", err
	}
	cipherText, err := base64.StdEncoding.DecodeString(echostr)
	if err != nil {
		return nil, "", fmt.Errorf("wecom callback: echostr: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, "", err
	}
	if len(cipherText) < aes.BlockSize || len(cipherText)%aes.BlockSize != 0 {
		return nil, "", fmt.Errorf("wecom callback: cipher length")
	}
	iv := key[:aes.BlockSize]
	buf := make([]byte, len(cipherText))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(buf, cipherText)
	buf, err = wecomPKCS7Unpad(buf)
	if err != nil {
		return nil, "", err
	}
	if len(buf) < 20 {
		return nil, "", fmt.Errorf("wecom callback: plaintext too short")
	}
	msgLen := binary.BigEndian.Uint32(buf[16:20])
	if int(msgLen) < 0 || 20+int(msgLen) > len(buf) {
		return nil, "", fmt.Errorf("wecom callback: bad msg length")
	}
	msg := buf[20 : 20+msgLen]
	receiveID = string(buf[20+msgLen:])
	return msg, receiveID, nil
}

// wecomEncrypt is used by tests to mint a valid echostr.
func wecomEncrypt(encodingAESKey, corpID, plaintext string) (string, error) {
	key, err := wecomAESKey(encodingAESKey)
	if err != nil {
		return "", err
	}
	rand16 := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, rand16); err != nil {
		return "", err
	}
	raw := make([]byte, 0, 20+len(plaintext)+len(corpID))
	raw = append(raw, rand16...)
	var ln [4]byte
	binary.BigEndian.PutUint32(ln[:], uint32(len(plaintext)))
	raw = append(raw, ln[:]...)
	raw = append(raw, plaintext...)
	raw = append(raw, corpID...)
	raw = wecomPKCS7Pad(raw)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	iv := key[:aes.BlockSize]
	out := make([]byte, len(raw))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, raw)
	return base64.StdEncoding.EncodeToString(out), nil
}

func wecomSign(token, timestamp, nonce, echostr string) string {
	parts := []string{token, timestamp, nonce, echostr}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	return fmt.Sprintf("%x", sum)
}

func wecomPKCS7Pad(b []byte) []byte {
	n := wecomCallbackPadBlock - (len(b) % wecomCallbackPadBlock)
	if n == 0 {
		n = wecomCallbackPadBlock
	}
	return append(b, bytes.Repeat([]byte{byte(n)}, n)...)
}

func wecomPKCS7Unpad(b []byte) ([]byte, error) {
	if len(b) == 0 {
		return nil, fmt.Errorf("wecom callback: empty pad")
	}
	n := int(b[len(b)-1])
	if n < 1 || n > wecomCallbackPadBlock || n > len(b) {
		return nil, fmt.Errorf("wecom callback: bad pkcs7")
	}
	return b[:len(b)-n], nil
}
