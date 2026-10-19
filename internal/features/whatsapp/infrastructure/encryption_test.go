package infrastructure

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"shenzen/backend/internal/features/whatsapp/domain"
	"shenzen/backend/internal/platform/crypto"
)

func key() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

// Needs a database with the auth, whatsapp and encrypt_phone migrations: SEC_TEST_DATABASE_URL=postgres://...
func TestPhoneIsStoredEncryptedAndStillSearchable(t *testing.T) {
	url := os.Getenv("SEC_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SEC_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	keys, _ := crypto.NewKeyring("k1", key(), key(), nil)
	repo := NewRepo(pool, keys)
	for _, id := range []string{"enc-u1", "enc-u2"} {
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
		if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, name) VALUES ($1, $1 || '@x.co', 'x')`, id); err != nil {
			t.Fatal(err)
		}
		defer pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	}
	c := domain.Contact{UserID: "enc-u1", Phone: "+6281234567890", Verified: true, SendTime: "07:00", Timezone: "Asia/Jakarta", Frequency: "daily", Locale: "id"}
	if err := repo.SaveContact(ctx, c); err != nil {
		t.Fatal(err)
	}

	var raw string
	_ = pool.QueryRow(ctx, `SELECT phone FROM whatsapp_contacts WHERE user_id = 'enc-u1'`).Scan(&raw)
	if !strings.HasPrefix(raw, "v1:") || strings.Contains(raw, "81234567890") {
		t.Fatalf("phone must be sealed at rest, got %q", raw)
	}
	if got, err := repo.Contact(ctx, "enc-u1"); err != nil || got.Phone != c.Phone {
		t.Fatalf("read back: %q %v", got.Phone, err)
	}
	if got, err := repo.ContactByPhone(ctx, c.Phone); err != nil || got.UserID != "enc-u1" {
		t.Fatalf("lookup by phone must work through the blind index: %+v %v", got, err)
	}

	other := c
	other.UserID = "enc-u2"
	if err := repo.SaveContact(ctx, other); err != domain.ErrPhoneUsed {
		t.Fatalf("one verified owner per number must hold on encrypted rows, got %v", err)
	}

	// a row written before encryption existed is picked up by Backfill
	_, _ = pool.Exec(ctx, `UPDATE whatsapp_contacts SET phone = '+6281234567890', phone_hash = NULL WHERE user_id = 'enc-u1'`)
	if err := repo.Backfill(ctx); err != nil {
		t.Fatal(err)
	}
	_ = pool.QueryRow(ctx, `SELECT phone FROM whatsapp_contacts WHERE user_id = 'enc-u1'`).Scan(&raw)
	if !strings.HasPrefix(raw, "v1:") {
		t.Fatalf("Backfill must seal legacy plaintext, got %q", raw)
	}
	if got, err := repo.ContactByPhone(ctx, c.Phone); err != nil || got.UserID != "enc-u1" {
		t.Fatalf("lookup after backfill: %+v %v", got, err)
	}
}
