package crypto

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func key() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

func ring(t *testing.T) *Keyring {
	t.Helper()
	k, err := NewKeyring("k1", key(), key(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestRoundTrip(t *testing.T) {
	k := ring(t)
	s, err := k.Seal("whatsapp_contacts.phone", "u1", "+628123456789")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s, "628123") || !IsSealed(s) {
		t.Fatalf("not sealed: %s", s)
	}
	got, err := k.Open("whatsapp_contacts.phone", "u1", s)
	if err != nil || got != "+628123456789" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestBindingAndTamper(t *testing.T) {
	k := ring(t)
	s, _ := k.Seal("a.b", "u1", "secret")
	if _, err := k.Open("a.b", "u2", s); !errors.Is(err, ErrCiphertext) {
		t.Fatal("other row must not open")
	}
	if _, err := k.Open("a.c", "u1", s); !errors.Is(err, ErrCiphertext) {
		t.Fatal("other column must not open")
	}
	if _, err := k.Open("a.b", "u1", s[:len(s)-2]+"AA"); !errors.Is(err, ErrCiphertext) {
		t.Fatal("tampered ciphertext must not open")
	}
}

func TestNonceIsFresh(t *testing.T) {
	k := ring(t)
	a, _ := k.Seal("a.b", "u1", "same")
	b, _ := k.Seal("a.b", "u1", "same")
	if a == b {
		t.Fatal("same plaintext must give different ciphertext")
	}
}

func TestRotation(t *testing.T) {
	oldKEK := key()
	idx := key()
	k1, _ := NewKeyring("k1", oldKEK, idx, nil)
	sealed, _ := k1.Seal("a.b", "u1", "v")
	k2, err := NewKeyring("k2", key(), idx, map[string]string{"k1": oldKEK})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := k2.Open("a.b", "u1", sealed); err != nil || got != "v" {
		t.Fatalf("old row must still open: %q %v", got, err)
	}
	if !k2.NeedsRotation(sealed) {
		t.Fatal("old key id must be flagged")
	}
}

func TestBlindIndexStable(t *testing.T) {
	k := ring(t)
	a, again, other := k.BlindIndex("p", "+62812"), k.BlindIndex("p", "+62812"), k.BlindIndex("p", "+62813")
	if a != again || a == other {
		t.Fatal("blind index must be deterministic and distinct")
	}
}

func TestBadKeys(t *testing.T) {
	if _, err := NewKeyring("k1", "short", key(), nil); err == nil {
		t.Fatal("short KEK must be rejected")
	}
}
