// Package infrastructure stores bookmarks and reads bookmarked articles.
package infrastructure

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"shenzen/backend/internal/features/news/domain"
)

type Repo struct{ pool *pgxpool.Pool }

func NewRepo(p *pgxpool.Pool) Repo { return Repo{p} }

func (r Repo) Add(ctx context.Context, userID, articleID string) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO news_bookmarks (user_id, article_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, userID, articleID)
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23503" { // foreign key: the article does not exist
		return domain.ErrArticleNotFound
	}
	return err
}

func (r Repo) Remove(ctx context.Context, userID, articleID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM news_bookmarks WHERE user_id = $1 AND article_id = $2`, userID, articleID)
	return err
}

func (r Repo) Marked(ctx context.Context, userID string, ids []string) (map[string]bool, error) {
	rows, err := r.pool.Query(ctx, `SELECT article_id FROM news_bookmarks WHERE user_id = $1 AND article_id = ANY($2)`, userID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

func (r Repo) List(ctx context.Context, userID string, limit int) ([]domain.Article, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT a.id, a.source, a.category, a.title, a.summary, a.url, a.published_at, a.tickers, a.sentiment
		FROM news_bookmarks b JOIN news_articles a ON a.id = b.article_id
		WHERE b.user_id = $1 ORDER BY b.created_at DESC, a.id LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Article
	for rows.Next() {
		var a domain.Article
		var at time.Time
		var s *float64
		if err := rows.Scan(&a.ID, &a.Source, &a.Category, &a.Title, &a.Summary, &a.URL, &at, &a.Tickers, &s); err != nil {
			return nil, err
		}
		a.PublishedAt, a.Sentiment = at.UTC().Format(time.RFC3339), domain.SentimentLabel(s)
		if a.Tickers == nil {
			a.Tickers = []string{}
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
