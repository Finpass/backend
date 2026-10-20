// Package application runs the advisory chat: session ownership, history window, AI call, storage.
package application

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"shenzen/backend/internal/features/advisory/domain"
	"shenzen/backend/internal/shared/apperr"
)

const (
	rateLimit  = 20
	rateWindow = time.Minute
	dayLimit   = 200 // chat messages per user per day
	newsLimit  = 5   // articles offered to the advisor per question
)

type Deps struct {
	Store    domain.Store
	Contexts domain.Contexts
	AI       domain.AI
	News     domain.News   // optional: without it the advisor answers from documents only
	NewID    func() string // uuid
	Now      func() time.Time
}

type Service struct {
	d  Deps
	mu sync.Mutex
	// shortcut: per-instance fixed window kept in memory, move to a shared limiter when the API scales out.
	seen map[string]*window
	day  map[string]*window
}

type window struct {
	n       int
	resetAt time.Time
}

func NewService(d Deps) *Service {
	return &Service{d: d, seen: map[string]*window{}, day: map[string]*window{}}
}

type Reply struct {
	SessionID string         `json:"sessionId"`
	Message   domain.Message `json:"message"`
	Grounded  bool           `json:"grounded"`
}

func (s *Service) allow(userID string) bool { return s.take(s.seen, userID, rateLimit, rateWindow) }

// allowDay caps what one account can spend on model calls in a day, however slowly it asks.
func (s *Service) allowDay(userID string) bool { return s.take(s.day, userID, dayLimit, 24*time.Hour) }

func (s *Service) take(m map[string]*window, userID string, limit int, span time.Duration) bool {
	now := s.d.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(m) > 10_000 {
		for k, w := range m {
			if now.After(w.resetAt) {
				delete(m, k)
			}
		}
	}
	w := m[userID]
	if w == nil || now.After(w.resetAt) {
		w = &window{resetAt: now.Add(span)}
		m[userID] = w
	}
	w.n++
	return w.n <= limit
}

// runKey namespaces a client run id by user, so one user can never poll another's progress.
func runKey(userID, runID string) string {
	if runID == "" {
		return ""
	}
	return userID + "_" + runID
}

func (s *Service) Turns(ctx context.Context, userID, runID string, since int) ([]domain.TurnEvent, error) {
	out, err := s.d.AI.Turns(ctx, runKey(userID, runID), since)
	if out == nil {
		out = []domain.TurnEvent{}
	}
	return out, err
}

// Context is everything the advisor knows about the user, shown to them as is. News is not part of it.
func (s *Service) Context(ctx context.Context, userID string) (domain.UserContext, error) {
	return s.d.Contexts.Build(ctx, userID, nil)
}

func (s *Service) Chat(ctx context.Context, userID, sessionID, message, lang, runID string) (Reply, error) {
	return s.ChatAbout(ctx, userID, sessionID, message, lang, runID, nil)
}

// ChatAbout is Chat with products the user wants compared; their rule results join the advisor's context.
func (s *Service) ChatAbout(ctx context.Context, userID, sessionID, message, lang, runID string, compare []string) (Reply, error) {
	if len(compare) == 1 || len(compare) > domain.MaxCompare {
		return Reply{}, apperr.Validation(apperr.Detail{Field: "compare", Message: "pick 2 to 4 products"})
	}
	message = strings.TrimSpace(message)
	if n := len([]rune(message)); n < 1 || n > domain.MaxMessage {
		return Reply{}, apperr.Validation(apperr.Detail{Field: "message", Message: "must be 1 to 1000 characters"})
	}
	if lang != "id" && lang != "en" {
		return Reply{}, apperr.Validation(apperr.Detail{Field: "lang", Message: "must be id or en"})
	}
	if !s.allow(userID) {
		return Reply{}, apperr.New(apperr.TooManyRequests, "Too many messages, try again in a minute")
	}
	if !s.allowDay(userID) {
		return Reply{}, apperr.New(apperr.TooManyRequests, "Daily message limit reached, try again tomorrow")
	}
	uc, err := s.d.Contexts.Build(ctx, userID, compare) // before any write: no profile must not leave an empty session behind
	if err != nil {
		return Reply{}, err
	}
	if s.d.News != nil {
		if items, err := s.d.News.Latest(ctx, userID, newsLimit); err == nil {
			uc.News = items
		} else {
			slog.WarnContext(ctx, "advisory: news unavailable", "err", err) // answered from documents instead
		}
	}
	now := s.d.Now()
	if sessionID == "" {
		sessionID = s.d.NewID()
		title := message
		if r := []rune(title); len(r) > domain.TitleLen {
			title = string(r[:domain.TitleLen])
		}
		if err := s.d.Store.CreateSession(ctx, sessionID, userID, title, now); err != nil {
			return Reply{}, err
		}
	} else if ok, err := s.d.Store.Owns(ctx, userID, sessionID); err != nil {
		return Reply{}, err
	} else if !ok {
		return Reply{}, domain.ErrNotFound
	}
	prior, err := s.d.Store.Recent(ctx, sessionID, domain.HistoryWindow)
	if err != nil {
		return Reply{}, err
	}
	if _, err := s.d.Store.AddMessage(ctx, sessionID, "user", message, nil, nil, now); err != nil {
		return Reply{}, err
	}
	history := make([]domain.Turn, len(prior))
	for i, m := range prior {
		history[i] = domain.Turn{Role: m.Role, Content: m.Content}
	}
	ans, err := s.d.AI.Chat(ctx, domain.ChatRequest{Message: message, Lang: lang, History: history, UserContext: uc, RunID: runKey(userID, runID)})
	if err != nil {
		slog.WarnContext(ctx, "advisory: ai service failed", "err", err)
		return Reply{}, apperr.Wrap(apperr.UpstreamFailed, "The advisory assistant is unavailable right now, please try again", err)
	}
	if ans.Citations == nil {
		ans.Citations = []domain.Citation{}
	}
	if ans.Turns == nil {
		ans.Turns = []domain.TurnEvent{}
	}
	stored, _ := json.Marshal(map[string]any{"trace": ans.Trace, "turns": ans.Turns})
	saved, err := s.d.Store.AddMessage(ctx, sessionID, "assistant", ans.Answer, ans.Citations, stored, s.d.Now())
	if err != nil {
		return Reply{}, err
	}
	saved.Turns = ans.Turns
	return Reply{SessionID: sessionID, Message: saved, Grounded: ans.Grounded}, nil
}

func (s *Service) Sessions(ctx context.Context, userID string) ([]domain.Session, error) {
	out, err := s.d.Store.Sessions(ctx, userID, domain.SessionLimit)
	if out == nil {
		out = []domain.Session{}
	}
	return out, err
}

type Detail struct {
	ID       string           `json:"id"`
	Title    string           `json:"title"`
	Messages []domain.Message `json:"messages"`
}

func (s *Service) Session(ctx context.Context, userID, id string) (Detail, error) {
	title, err := s.d.Store.Title(ctx, userID, id)
	if err != nil {
		return Detail{}, err
	}
	ms, err := s.d.Store.Messages(ctx, id)
	if ms == nil {
		ms = []domain.Message{}
	}
	return Detail{ID: id, Title: title, Messages: ms}, err
}

func (s *Service) Delete(ctx context.Context, userID, id string) error {
	ok, err := s.d.Store.Delete(ctx, userID, id)
	if err == nil && !ok {
		return domain.ErrNotFound
	}
	return err
}
