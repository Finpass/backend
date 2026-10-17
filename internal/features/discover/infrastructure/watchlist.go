// Package infrastructure reads the screener table, refreshes it and stores the watchlist.
package infrastructure

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"shenzen/backend/internal/features/discover/domain"
)

type Watchlist struct{ pool *pgxpool.Pool }

func NewWatchlist(p *pgxpool.Pool) Watchlist { return Watchlist{p} }

func (w Watchlist) Add(ctx context.Context, userID, symbol string) error {
	_, err := w.pool.Exec(ctx, `INSERT INTO watchlist (user_id, symbol) VALUES ($1, $2) ON CONFLICT DO NOTHING`, userID, symbol)
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23503" { // foreign key: unknown symbol
		return domain.ErrAssetNotFound
	}
	return err
}

func (w Watchlist) Remove(ctx context.Context, userID, symbol string) error {
	_, err := w.pool.Exec(ctx, `DELETE FROM watchlist WHERE user_id = $1 AND symbol = $2`, userID, symbol)
	return err
}

func (w Watchlist) Marked(ctx context.Context, userID string, symbols []string) (map[string]bool, error) {
	rows, err := w.pool.Query(ctx, `SELECT symbol FROM watchlist WHERE user_id = $1 AND symbol = ANY($2)`, userID, symbols)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out[s] = true
	}
	return out, rows.Err()
}
