package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// secretFile is the hub secret in the data directory. It is not in the
// database on purpose: a copy of the database alone does not reveal the TOTP
// secrets of the users.
const secretFile = "secret.key"

const secretLen = 32

// LoadOrCreateSecret returns the hub secret kept in dir, creating it (readable
// by the owner only) at the first start.
func LoadOrCreateSecret(dir string) ([]byte, error) {
	path := filepath.Join(dir, secretFile)
	secret, err := os.ReadFile(path)
	switch {
	case err == nil:
		if len(secret) < secretLen {
			return nil, fmt.Errorf("%s is too short (%d bytes): restore it from a backup, or delete it to start over (two-factor enrollments are then lost)", path, len(secret))
		}
		return secret, nil
	case !errors.Is(err, os.ErrNotExist):
		return nil, err
	}
	secret = make([]byte, secretLen)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generate secret: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) { // another process won the race
			return LoadOrCreateSecret(dir)
		}
		return nil, err
	}
	if _, err := f.Write(secret); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return nil, err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return secret, nil
}

// sealer encrypts small secrets (AES-256-GCM) with a key derived from the hub
// secret.
type sealer struct{ aead cipher.AEAD }

func newSealer(secret []byte) (*sealer, error) {
	key, err := hkdf.Key(sha256.New, secret, nil, "regulus totp secrets v1", 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &sealer{aead}, nil
}

// seal encrypts plain. aad ties the ciphertext to its owner, so a value copied
// to another user's row does not decrypt.
func (b *sealer) seal(plain, aad []byte) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(b.aead.Seal(nonce, nonce, plain, aad)), nil
}

func (b *sealer) open(sealed string, aad []byte) ([]byte, error) {
	raw, err := base64.RawStdEncoding.DecodeString(sealed)
	if err != nil || len(raw) < b.aead.NonceSize() {
		return nil, errors.New("sealed value is malformed")
	}
	nonce, ct := raw[:b.aead.NonceSize()], raw[b.aead.NonceSize():]
	return b.aead.Open(nil, nonce, ct, aad)
}
