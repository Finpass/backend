// Package infrastructure reads the profile tables (read only) for the dashboard.
package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"shenzen/backend/internal/features/dashboard/domain"
	"shenzen/backend/internal/platform/crypto"
)

type ProfileRepo struct {
	pool *pgxpool.Pool
	keys *crypto.Keyring // nil outside production
}

func NewProfileRepo(p *pgxpool.Pool, keys *crypto.Keyring) ProfileRepo { return ProfileRepo{p, keys} }

func num(a map[string]any, k string) int64 {
	f, _ := a[k].(float64)
	if f < 0 {
		return 0
	}
	return int64(f)
}

func str(a map[string]any, k string) string { s, _ := a[k].(string); return s }

// Read joins users, profiles and the latest assessment. A user without a profile gets Status "none" and zeros.
func (r ProfileRepo) Read(ctx context.Context, userID string) (domain.Profile, error) {
	var name string
	var status *string
	var answers map[string]any
	var rawAnswers, rawOutput json.RawMessage
	var assessmentID *string
	err := r.pool.QueryRow(ctx, `
		SELECT u.name, p.status, p.answers, a.id, a.output
		FROM users u
		LEFT JOIN profiles p ON p.user_id = u.id
		LEFT JOIN LATERAL (SELECT id, output FROM profile_assessments WHERE user_id = u.id ORDER BY created_at DESC LIMIT 1) a ON TRUE
		WHERE u.id = $1`, userID).Scan(&name, &status, &rawAnswers, &assessmentID, &rawOutput)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Profile{Status: "none"}, nil
	}
	if err != nil {
		return domain.Profile{}, err
	}
	if err := r.keys.RevealJSON("profiles.answers", userID, rawAnswers, &answers); err != nil {
		return domain.Profile{}, err
	}
	var risk *string
	if assessmentID != nil {
		var out struct {
			FinalProfile string `json:"finalProfile"`
		}
		if err := r.keys.RevealJSON("profile_assessments.output", *assessmentID, rawOutput, &out); err != nil {
			return domain.Profile{}, err
		}
		if out.FinalProfile != "" {
			risk = &out.FinalProfile
		}
	}
	p := domain.Profile{FirstName: firstName(name), Status: "none"}
	if status != nil {
		p.Status = *status
	}
	if risk != nil {
		p.RiskProfile = *risk
	}
	p.Income, p.Expenses, p.Savings, p.Investments = num(answers, "q2"), num(answers, "q3"), num(answers, "q4"), num(answers, "q7")
	p.Goal, p.Horizon, p.Sharia = str(answers, "q17"), str(answers, "q19"), str(answers, "q22")
	switch q5 := str(answers, "q5"); q5 {
	case "0", "1", "2", "3":
		p.Dependents = int(q5[0] - '0')
	case "4_plus":
		p.Dependents = 4
	}
	return p, nil
}

func firstName(n string) string {
	if f := strings.Fields(n); len(f) > 0 {
		return f[0]
	}
	return ""
}
