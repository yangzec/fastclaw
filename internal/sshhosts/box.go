// Package sshhosts encrypts saved SSH credentials and dials remote hosts
// so FastClaw can inject keys/passwords without putting them in chat.
package sshhosts

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/fastclaw-ai/fastclaw/internal/config"
)

// Creds is the plaintext payload sealed into SSHHostRecord.SecretEnc.
type Creds struct {
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"privateKey,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
}

// Box seals and opens Creds with a local AES-256-GCM key stored under
// FASTCLAW_HOME. The key file is created on first use with 0600 perms.
type Box struct {
	gcm cipher.AEAD
}

// OpenBox loads (or creates) the host-secret key in FastClaw's home dir.
func OpenBox() (*Box, error) {
	home, err := config.HomeDir()
	if err != nil {
		return nil, err
	}
	return OpenBoxAt(home)
}

// OpenBoxAt is the testable form of OpenBox.
func OpenBoxAt(home string) (*Box, error) {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, fmt.Errorf("sshhosts: mkdir home: %w", err)
	}
	path := filepath.Join(home, "ssh-hosts.key")
	raw, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("sshhosts: read key: %w", err)
		}
		raw = make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return nil, fmt.Errorf("sshhosts: generate key: %w", err)
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			return nil, fmt.Errorf("sshhosts: write key: %w", err)
		}
	}
	if len(raw) != 32 {
		return nil, errors.New("sshhosts: key file must be 32 bytes")
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{gcm: gcm}, nil
}

// Seal encrypts creds to a hex ciphertext (nonce || sealed).
func (b *Box) Seal(c Creds) (string, error) {
	if b == nil {
		return "", errors.New("sshhosts: box is nil")
	}
	plain, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, b.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := b.gcm.Seal(nonce, nonce, plain, nil)
	return hex.EncodeToString(sealed), nil
}

// Open decrypts a blob produced by Seal.
func (b *Box) Open(enc string) (Creds, error) {
	var zero Creds
	if b == nil {
		return zero, errors.New("sshhosts: box is nil")
	}
	if enc == "" {
		return zero, errors.New("sshhosts: empty secret")
	}
	raw, err := hex.DecodeString(enc)
	if err != nil {
		return zero, fmt.Errorf("sshhosts: decode: %w", err)
	}
	ns := b.gcm.NonceSize()
	if len(raw) < ns {
		return zero, errors.New("sshhosts: ciphertext too short")
	}
	plain, err := b.gcm.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return zero, fmt.Errorf("sshhosts: decrypt: %w", err)
	}
	var c Creds
	if err := json.Unmarshal(plain, &c); err != nil {
		return zero, err
	}
	return c, nil
}
