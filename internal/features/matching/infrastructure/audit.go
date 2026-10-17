package infrastructure

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"

	"shenzen/backend/internal/features/matching/domain"
)

// Audit keeps suitability records in suitability_records.
type Audit struct{ pool *pgxpool.Pool }

func NewAudit(p *pgxpool.Pool) *Audit { return &Audit{p} }

// Record inserts unless the user's latest record of that kind has the same content, so reloading a page adds nothing.
func (a *Audit) Record(ctx context.Context, userID, kind string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(b)
	_, err = a.pool.Exec(ctx, `
		INSERT INTO suitability_records (user_id, kind, digest, payload)
		SELECT $1, $2, $3, $4::jsonb
		WHERE NOT EXISTS (
		  SELECT 1 FROM (SELECT digest FROM suitability_records WHERE user_id = $1 AND kind = $2 ORDER BY id DESC LIMIT 1) l WHERE l.digest = $3)`,
		userID, kind, hex.EncodeToString(sum[:]), string(b))
	return err
}

func (a *Audit) List(ctx context.Context, userID string, limit int) ([]domain.SuitabilityRecord, error) {
	rows, err := a.pool.Query(ctx, `SELECT id, kind, payload, created_at FROM suitability_records WHERE user_id = $1 ORDER BY id DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.SuitabilityRecord{}
	for rows.Next() {
		var r domain.SuitabilityRecord
		if err := rows.Scan(&r.ID, &r.Kind, &r.Payload, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
