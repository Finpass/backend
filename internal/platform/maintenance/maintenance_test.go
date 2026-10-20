package maintenance

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Needs a database with all migrations: SEC_TEST_DATABASE_URL=postgres://...
func TestPurgeRemovesExpiredAndKeepsFresh(t *testing.T) {
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
	exec := func(q string, a ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, q, a...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`DELETE FROM users WHERE id = 'ret-u1'`)
	exec(`INSERT INTO users (id, email, name) VALUES ('ret-u1', 'ret-u1@x.co', 'x')`)
	defer pool.Exec(ctx, `DELETE FROM users WHERE id = 'ret-u1'`)

	now := time.Now()
	old, fresh := now.AddDate(0, 0, -400), now.AddDate(0, 0, -1)
	exec(`INSERT INTO chat_sessions (id, user_id, title, created_at, updated_at) VALUES
		('00000000-0000-4000-8000-0000000000a1','ret-u1','old',$1,$1), ('00000000-0000-4000-8000-0000000000a2','ret-u1','new',$2,$2)`, old, fresh)
	exec(`INSERT INTO chat_messages (session_id, role, content, created_at) VALUES
		('00000000-0000-4000-8000-0000000000a1','user','old',$1), ('00000000-0000-4000-8000-0000000000a2','user','new',$2)`, old, fresh)
	exec(`INSERT INTO auth_sessions (id, user_id, token_hash, expires_at, created_at, revoked_at) VALUES
		('ret-s1','ret-u1','h1',$1,$1,NULL), ('ret-s2','ret-u1','h2',$2,$2,NULL)`, now.AddDate(0, 0, -60), now.Add(time.Hour))

	removed, err := Purge(ctx, pool, Default, now)
	if err != nil {
		t.Fatal(err)
	}
	count := func(q string) (n int) {
		t.Helper()
		if err := pool.QueryRow(ctx, q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return
	}
	if n := count(`SELECT count(*) FROM chat_sessions WHERE user_id = 'ret-u1'`); n != 1 {
		t.Errorf("chat sessions left: %d, want only the fresh one (removed=%v)", n, removed)
	}
	if n := count(`SELECT count(*) FROM chat_messages WHERE session_id = '00000000-0000-4000-8000-0000000000a1'`); n != 0 {
		t.Errorf("messages of the purged session must cascade, left %d", n)
	}
	if n := count(`SELECT count(*) FROM chat_messages WHERE session_id = '00000000-0000-4000-8000-0000000000a2'`); n != 1 {
		t.Errorf("fresh messages must stay, left %d", n)
	}
	if n := count(`SELECT count(*) FROM auth_sessions WHERE user_id = 'ret-u1'`); n != 1 {
		t.Errorf("auth sessions left: %d, want the live one only", n)
	}
	// keeping forever is a choice, not an accident
	if r, err := Purge(ctx, pool, Policy{}, now); err != nil || r["chat_sessions"] != 0 {
		t.Errorf("zero days must keep chats: %v %v", r, err)
	}
}
