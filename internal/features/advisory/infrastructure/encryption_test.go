package infrastructure

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"shenzen/backend/internal/platform/crypto"
)

func key() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

// Needs a database with the auth and advisory_chat migrations: SEC_TEST_DATABASE_URL=postgres://...
func TestChatTextIsStoredEncrypted(t *testing.T) {
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
	st := NewStore(pool, keys)
	_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = 'enc-chat'`)
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, name) VALUES ('enc-chat', 'enc-chat@x.co', 'x')`); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM users WHERE id = 'enc-chat'`)

	now := time.Now()
	if err := st.CreateSession(ctx, "00000000-0000-4000-8000-0000000000e1", "enc-chat", "Gaji saya 20 juta", now); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddMessage(ctx, "00000000-0000-4000-8000-0000000000e1", "user", "Penghasilan saya Rp20.000.000 per bulan", nil, nil, now); err != nil {
		t.Fatal(err)
	}

	var title, content string
	_ = pool.QueryRow(ctx, `SELECT title FROM chat_sessions WHERE id = '00000000-0000-4000-8000-0000000000e1'`).Scan(&title)
	_ = pool.QueryRow(ctx, `SELECT content FROM chat_messages WHERE session_id = '00000000-0000-4000-8000-0000000000e1'`).Scan(&content)
	if !strings.HasPrefix(title, "v1:") || !strings.HasPrefix(content, "v1:") || strings.Contains(content, "20.000.000") {
		t.Fatalf("chat text must be sealed at rest: %q / %q", title, content)
	}
	msgs, err := st.Messages(ctx, "00000000-0000-4000-8000-0000000000e1")
	if err != nil || len(msgs) != 1 || msgs[0].Content != "Penghasilan saya Rp20.000.000 per bulan" {
		t.Fatalf("read back: %+v %v", msgs, err)
	}
	if got, err := st.Title(ctx, "enc-chat", "00000000-0000-4000-8000-0000000000e1"); err != nil || got != "Gaji saya 20 juta" {
		t.Fatalf("title: %q %v", got, err)
	}

	// legacy plaintext is still readable and Backfill seals it
	_, _ = pool.Exec(ctx, `UPDATE chat_messages SET content = 'lama' WHERE session_id = '00000000-0000-4000-8000-0000000000e1'`)
	if m, _ := st.Messages(ctx, "00000000-0000-4000-8000-0000000000e1"); len(m) != 1 || m[0].Content != "lama" {
		t.Fatalf("legacy plaintext must still read: %+v", m)
	}
	if err := st.Backfill(ctx); err != nil {
		t.Fatal(err)
	}
	_ = pool.QueryRow(ctx, `SELECT content FROM chat_messages WHERE session_id = '00000000-0000-4000-8000-0000000000e1'`).Scan(&content)
	if !strings.HasPrefix(content, "v1:") {
		t.Fatalf("Backfill must seal legacy chat text: %q", content)
	}
}
