// Package infrastructure stores chat sessions in Postgres and calls the AI service.
package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"shenzen/backend/internal/features/advisory/domain"
)

type Store struct{ pool *pgxpool.Pool }

func NewStore(p *pgxpool.Pool) Store { return Store{p} }

func (s Store) CreateSession(ctx context.Context, id, userID, title string, now time.Time) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO chat_sessions (id, user_id, title, created_at, updated_at) VALUES ($1, $2, $3, $4, $4)`, id, userID, title, now)
	return err
}

func (s Store) Owns(ctx context.Context, userID, id string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM chat_sessions WHERE id = $1 AND user_id = $2)`, id, userID).Scan(&ok)
	return ok, err
}

func (s Store) Title(ctx context.Context, userID, id string) (string, error) {
	var t string
	err := s.pool.QueryRow(ctx, `SELECT title FROM chat_sessions WHERE id = $1 AND user_id = $2`, id, userID).Scan(&t)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	return t, err
}

func (s Store) AddMessage(ctx context.Context, sessionID, role, content string, cites []domain.Citation, trace json.RawMessage, now time.Time) (domain.Message, error) {
	var cj, tj []byte
	if cites != nil {
		cj, _ = json.Marshal(cites)
	}
	if len(trace) > 0 {
		tj = trace
	}
	m := domain.Message{Role: role, Content: content, Citations: cites}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return m, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tx.QueryRow(ctx, `INSERT INTO chat_messages (session_id, role, content, citations, trace, created_at) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		sessionID, role, content, cj, tj, now).Scan(&m.ID, &m.CreatedAt); err != nil {
		return m, err
	}
	if _, err := tx.Exec(ctx, `UPDATE chat_sessions SET updated_at = $2 WHERE id = $1`, sessionID, now); err != nil {
		return m, err
	}
	return m, tx.Commit(ctx)
}

func (s Store) scan(rows pgx.Rows) ([]domain.Message, error) {
	defer rows.Close()
	var out []domain.Message
	for rows.Next() {
		var m domain.Message
		var cj, tj []byte
		if err := rows.Scan(&m.ID, &m.Role, &m.Content, &cj, &tj, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.Citations = []domain.Citation{}
		if len(cj) > 0 {
			_ = json.Unmarshal(cj, &m.Citations)
		}
		m.Turns = []domain.TurnEvent{}
		if len(tj) > 0 {
			_ = json.Unmarshal(tj, &m.Turns)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s Store) Recent(ctx context.Context, sessionID string, n int) ([]domain.Message, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, role, content, citations, turns, created_at FROM (
		SELECT id, role, content, citations, trace->'turns' AS turns, created_at FROM chat_messages WHERE session_id = $1 ORDER BY id DESC LIMIT $2) t ORDER BY id`, sessionID, n)
	if err != nil {
		return nil, err
	}
	return s.scan(rows)
}

func (s Store) Messages(ctx context.Context, sessionID string) ([]domain.Message, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, role, content, citations, trace->'turns', created_at FROM chat_messages WHERE session_id = $1 ORDER BY id`, sessionID)
	if err != nil {
		return nil, err
	}
	return s.scan(rows)
}

func (s Store) Sessions(ctx context.Context, userID string, limit int) ([]domain.Session, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, title, updated_at FROM chat_sessions WHERE user_id = $1 ORDER BY updated_at DESC, id LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Session
	for rows.Next() {
		var x domain.Session
		if err := rows.Scan(&x.ID, &x.Title, &x.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s Store) Delete(ctx context.Context, userID, id string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM chat_sessions WHERE id = $1 AND user_id = $2`, id, userID)
	return tag.RowsAffected() > 0, err
}
