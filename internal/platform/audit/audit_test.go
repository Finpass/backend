package audit

import (
	"testing"
	"time"
)

func TestChainCommitsToEveryField(t *testing.T) {
	at := time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC)
	base := Chain("p", at, "u1", "auth.login.ok", "", "r1", []byte("{}"))
	for name, other := range map[string]string{
		"prev":     Chain("q", at, "u1", "auth.login.ok", "", "r1", []byte("{}")),
		"actor":    Chain("p", at, "u2", "auth.login.ok", "", "r1", []byte("{}")),
		"action":   Chain("p", at, "u1", "auth.login.bad", "", "r1", []byte("{}")),
		"time":     Chain("p", at.Add(time.Second), "u1", "auth.login.ok", "", "r1", []byte("{}")),
		"meta":     Chain("p", at, "u1", "auth.login.ok", "", "r1", []byte(`{"a":1}`)),
		"boundary": Chain("p", at, "u1a", "uth.login.ok", "", "r1", []byte("{}")),
	} {
		if other == base {
			t.Errorf("changing %s must change the hash", name)
		}
	}
	if base != Chain("p", at, "u1", "auth.login.ok", "", "r1", []byte("{}")) {
		t.Fatal("hash must be deterministic")
	}
}
