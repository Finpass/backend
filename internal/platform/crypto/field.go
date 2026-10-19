// Package crypto encrypts sensitive columns in the application (AES-256-GCM) so a database dump alone reveals nothing.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/hkdf"
)

const keyLen = 32

var ErrCiphertext = errors.New("crypto: invalid ciphertext")

// Keyring holds the active key-encryption key (KEK), retired ones that can still decrypt, and the blind-index key.
type Keyring struct {
	activeID string
	keks     map[string][]byte
	index    []byte
}

// NewKeyring decodes base64 keys of 32 bytes. old maps key ids to retired KEKs kept only to read older rows.
func NewKeyring(activeID, kek, index string, old map[string]string) (*Keyring, error) {
	k := &Keyring{activeID: activeID, keks: map[string][]byte{}}
	var err error
	if k.keks[activeID], err = decode32(kek); err != nil {
		return nil, fmt.Errorf("DATA_KEK: %w", err)
	}
	if k.index, err = decode32(index); err != nil {
		return nil, fmt.Errorf("DATA_INDEX_KEY: %w", err)
	}
	for id, v := range old {
		if k.keks[id], err = decode32(v); err != nil {
			return nil, fmt.Errorf("old key %s: %w", id, err)
		}
	}
	return k, nil
}

func decode32(s string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, err
	}
	if len(b) != keyLen {
		return nil, fmt.Errorf("must be %d bytes, got %d", keyLen, len(b))
	}
	return b, nil
}

// aead derives a per-column data key from the KEK, so a leaked column key exposes nothing else.
func (k *Keyring) aead(keyID, label string) (cipher.AEAD, error) {
	kek, ok := k.keks[keyID]
	if !ok {
		return nil, fmt.Errorf("crypto: unknown key id %q", keyID)
	}
	dek := make([]byte, keyLen)
	if _, err := io.ReadFull(hkdf.New(sha256.New, kek, nil, []byte("finpass/"+label)), dek); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Seal encrypts plain as "v1:<keyID>:<base64(nonce|ciphertext)>". label ("table.column") and rowID are authenticated,
// so a ciphertext copied to another column or row fails to open.
func (k *Keyring) Seal(label, rowID, plain string) (string, error) {
	g, err := k.aead(k.activeID, label)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ct := g.Seal(nonce, nonce, []byte(plain), []byte(label+"|"+rowID))
	return "v1:" + k.activeID + ":" + base64.RawStdEncoding.EncodeToString(ct), nil
}

func (k *Keyring) Open(label, rowID, sealed string) (string, error) {
	parts := strings.SplitN(sealed, ":", 3)
	if len(parts) != 3 || parts[0] != "v1" {
		return "", ErrCiphertext
	}
	raw, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return "", ErrCiphertext
	}
	g, err := k.aead(parts[1], label)
	if err != nil {
		return "", err
	}
	if len(raw) < g.NonceSize() {
		return "", ErrCiphertext
	}
	pt, err := g.Open(nil, raw[:g.NonceSize()], raw[g.NonceSize():], []byte(label+"|"+rowID))
	if err != nil {
		return "", ErrCiphertext
	}
	return string(pt), nil
}

// IsSealed tells encrypted values from legacy plaintext while a column is being migrated.
func IsSealed(v string) bool { return strings.HasPrefix(v, "v1:") }

// NeedsRotation reports whether sealed was written with a retired key.
func (k *Keyring) NeedsRotation(sealed string) bool {
	parts := strings.SplitN(sealed, ":", 3)
	return len(parts) == 3 && parts[1] != k.activeID
}

// BlindIndex is a keyed hash for equality lookups and uniqueness on encrypted columns.
func (k *Keyring) BlindIndex(label, normalized string) string {
	m := hmac.New(sha256.New, k.index)
	m.Write([]byte(label + "|" + normalized))
	return base64.RawStdEncoding.EncodeToString(m.Sum(nil))
}

// A nil *Keyring means encryption is off (development without DATA_KEK): values pass through unchanged. Production
// refuses to start without keys, so the pass-through never ships.

// Protect seals plain, or returns it as-is when encryption is off.
func (k *Keyring) Protect(label, rowID, plain string) (string, error) {
	if k == nil {
		return plain, nil
	}
	return k.Seal(label, rowID, plain)
}

// Reveal opens a sealed value; plaintext written before encryption was enabled is returned unchanged.
func (k *Keyring) Reveal(label, rowID, v string) (string, error) {
	if k == nil || !IsSealed(v) {
		return v, nil
	}
	return k.Open(label, rowID, v)
}

// Index is the blind index of normalized, or normalized itself when encryption is off.
func (k *Keyring) Index(label, normalized string) string {
	if k == nil {
		return normalized
	}
	return k.BlindIndex(label, normalized)
}
