package infrastructure

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"

	"shenzen/backend/internal/features/matching/domain"
)

// History reads the selection keys the WhatsApp slice stores in briefing_runs.content.
type History struct{ pool *pgxpool.Pool }

func NewHistory(p *pgxpool.Pool) History { return History{p} }

func (h History) History(ctx context.Context, userID string, days int) ([]domain.DayRecord, error) {
	rows, err := h.pool.Query(ctx, `SELECT run_date::text, content FROM briefing_runs WHERE user_id = $1 AND status = 'sent' ORDER BY run_date DESC LIMIT $2`, userID, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.DayRecord
	for rows.Next() {
		var d domain.DayRecord
		var raw []byte
		if err := rows.Scan(&d.Date, &raw); err != nil {
			return nil, err
		}
		var c struct{ AlertKey, NewsTopic, NewsID, NudgeKey string }
		_ = json.Unmarshal(raw, &c)
		d.AlertKey, d.NewsTopic, d.NewsID, d.NudgeKey = c.AlertKey, c.NewsTopic, c.NewsID, c.NudgeKey
		out = append(out, d)
	}
	return out, rows.Err()
}
