package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
)

// secretBox encrypts small config secrets (B2 application key, webhook signing
// secret) before they touch SQLite. The key comes from the RECORDING_CONFIG_KEY
// env var — 32 raw bytes as hex or base64, or any string (hashed to 32 bytes).
// With no key set it degrades to pass-through so local/dev setups still work; a
// warning is logged once.
type secretBox struct {
	aead cipher.AEAD // nil => pass-through
}

type secretEncryptor interface{ Encrypt(plain string) (string, error) }
type secretDecryptor interface{ Decrypt(stored string) (string, error) }

const encPrefix = "enc:v1:"

func newSecretBox(log *slog.Logger) *secretBox {
	raw := strings.TrimSpace(os.Getenv("RECORDING_CONFIG_KEY"))
	if raw == "" {
		log.Warn("RECORDING_CONFIG_KEY not set — recording B2/webhook secrets will be stored in plaintext in wacalls.db")
		return &secretBox{}
	}
	key := decodeKey(raw)
	block, err := aes.NewCipher(key)
	if err != nil {
		log.Error("RECORDING_CONFIG_KEY invalid, falling back to plaintext secret storage", "err", err)
		return &secretBox{}
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		log.Error("cannot init GCM for secret storage, falling back to plaintext", "err", err)
		return &secretBox{}
	}
	return &secretBox{aead: aead}
}

func decodeKey(raw string) []byte {
	if b, err := hex.DecodeString(raw); err == nil && len(b) == 32 {
		return b
	}
	if b, err := base64.StdEncoding.DecodeString(raw); err == nil && len(b) == 32 {
		return b
	}
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

// Encrypt returns "" for "", an enc:v1: token when a key is configured, or the
// plaintext unchanged otherwise.
func (s *secretBox) Encrypt(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	if s.aead == nil {
		return plain, nil
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := s.aead.Seal(nonce, nonce, []byte(plain), nil)
	return encPrefix + base64.StdEncoding.EncodeToString(ct), nil
}

// Decrypt reverses Encrypt. Values without the enc: prefix are returned as-is
// (plaintext written before a key was configured, or manual edits).
func (s *secretBox) Decrypt(stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	if !strings.HasPrefix(stored, encPrefix) {
		return stored, nil
	}
	if s.aead == nil {
		return "", errors.New("secret is encrypted but RECORDING_CONFIG_KEY is not set")
	}
	ct, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, encPrefix))
	if err != nil {
		return "", err
	}
	ns := s.aead.NonceSize()
	if len(ct) < ns {
		return "", errors.New("secret ciphertext too short")
	}
	plain, err := s.aead.Open(nil, ct[:ns], ct[ns:], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
