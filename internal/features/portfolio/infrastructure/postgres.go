// Package infrastructure is the Postgres adapter of the portfolio repository.
package infrastructure

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"shenzen/backend/internal/features/portfolio/domain"
)

type Repo struct{ pool *pgxpool.Pool }

func NewRepo(p *pgxpool.Pool) Repo { return Repo{p} }

const cols = `id, user_id, symbol, manual_name, manual_class, manual_currency, manual_price::float8, manual_price_updated_at,
	units::float8, avg_cost::float8, acquired_at, created_at`

func scan(row pgx.Row) (domain.Holding, error) {
	var h domain.Holding
	var sym, name, class, ccy *string
	var price *float64
	var upd *time.Time
	err := row.Scan(&h.ID, &h.UserID, &sym, &name, &class, &ccy, &price, &upd, &h.Units, &h.AvgCost, &h.AcquiredAt, &h.CreatedAt)
	if err != nil {
		return h, err
	}
	if sym != nil {
		h.Symbol = *sym
	}
	if name != nil && class != nil && ccy != nil && price != nil && upd != nil {
		h.Manual = &domain.Manual{Name: *name, Class: *class, Currency: *ccy, Price: *price, PriceUpdatedAt: upd.UTC()}
	}
	h.AcquiredAt = h.AcquiredAt.UTC()
	h.CreatedAt = h.CreatedAt.UTC()
	return h, nil
}

func (r Repo) Insert(ctx context.Context, h domain.Holding, limit int) error {
	var sym, name, class, ccy any
	var price, upd any
	if h.Symbol != "" {
		sym = h.Symbol
	}
	if m := h.Manual; m != nil {
		name, class, ccy, price, upd = m.Name, m.Class, m.Currency, m.Price, m.PriceUpdatedAt
	}
	// The count guard and the insert are one statement, so concurrent requests cannot overshoot the limit by much.
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO holdings (id, user_id, symbol, manual_name, manual_class, manual_currency, manual_price, manual_price_updated_at, units, avg_cost, acquired_at, created_at, updated_at)
		SELECT $1::text, $2::text, $3::text, $4::text, $5::text, $6::text, $7::float8, $8::timestamptz, $9::float8, $10::float8, $11::date, $12::timestamptz, $12::timestamptz
		WHERE (SELECT count(*) FROM holdings WHERE user_id = $2) < $13::int`,
		h.ID, h.UserID, sym, name, class, ccy, price, upd, h.Units, h.AvgCost, h.AcquiredAt, h.CreatedAt, limit)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrLimitReached
	}
	return nil
}

func (r Repo) List(ctx context.Context, userID string) ([]domain.Holding, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+cols+` FROM holdings WHERE user_id = $1 ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Holding
	for rows.Next() {
		h, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (r Repo) Get(ctx context.Context, userID, id string) (domain.Holding, error) {
	h, err := scan(r.pool.QueryRow(ctx, `SELECT `+cols+` FROM holdings WHERE id = $1 AND user_id = $2`, id, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return h, domain.ErrNotFound
	}
	return h, err
}

func (r Repo) Update(ctx context.Context, h domain.Holding) error {
	var price, upd any
	if m := h.Manual; m != nil {
		price, upd = m.Price, m.PriceUpdatedAt
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE holdings SET units = $3::float8, avg_cost = $4::float8, acquired_at = $5::date,
			manual_price = COALESCE($6::float8, manual_price), manual_price_updated_at = COALESCE($7::timestamptz, manual_price_updated_at), updated_at = now()
		WHERE id = $1 AND user_id = $2`, h.ID, h.UserID, h.Units, h.AvgCost, h.AcquiredAt, price, upd)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r Repo) Delete(ctx context.Context, userID, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM holdings WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
