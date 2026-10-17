package application_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"shenzen/backend/internal/features/advisory/application"
	"shenzen/backend/internal/features/advisory/domain"
	"shenzen/backend/internal/shared/apperr"
)

type msg struct {
	domain.Message
	sid string
}
type sess struct{ id, user, title string }

type store struct {
	lastTrace json.RawMessage
	sessions  []sess
	msgs      []msg
	n         int64
}

func (s *store) CreateSession(_ context.Context, id, u, t string, _ time.Time) error {
	s.sessions = append(s.sessions, sess{id, u, t})
	return nil
}
func (s *store) Owns(_ context.Context, u, id string) (bool, error) {
	for _, x := range s.sessions {
		if x.id == id && x.user == u {
			return true, nil
		}
	}
	return false, nil
}
func (s *store) Title(_ context.Context, u, id string) (string, error) {
	for _, x := range s.sessions {
		if x.id == id && x.user == u {
			return x.title, nil
		}
	}
	return "", domain.ErrNotFound
}
func (s *store) AddMessage(_ context.Context, sid, role, c string, cs []domain.Citation, tr json.RawMessage, now time.Time) (domain.Message, error) {
	s.n++
	if len(tr) > 0 {
		s.lastTrace = tr
	}
	m := domain.Message{ID: s.n, Role: role, Content: c, Citations: cs, CreatedAt: now}
	s.msgs = append(s.msgs, msg{m, sid})
	return m, nil
}
func (s *store) Messages(_ context.Context, sid string) ([]domain.Message, error) {
	var out []domain.Message
	for _, m := range s.msgs {
		if m.sid == sid {
			out = append(out, m.Message)
		}
	}
	return out, nil
}
func (s *store) Recent(ctx context.Context, sid string, n int) ([]domain.Message, error) {
	all, _ := s.Messages(ctx, sid)
	if len(all) > n {
		all = all[len(all)-n:]
	}
	return all, nil
}
func (s *store) Sessions(_ context.Context, u string, _ int) ([]domain.Session, error) {
	var out []domain.Session
	for _, x := range s.sessions {
		if x.user == u {
			out = append(out, domain.Session{ID: x.id, Title: x.title})
		}
	}
	return out, nil
}
func (s *store) Delete(_ context.Context, u, id string) (bool, error) {
	for i, x := range s.sessions {
		if x.id == id && x.user == u {
			s.sessions = append(s.sessions[:i], s.sessions[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

type ctxs struct{ err error }

func (c ctxs) Build(context.Context, string, []string) (domain.UserContext, error) {
	return domain.UserContext{Profile: domain.ProfileFacts{RiskProfile: "Moderate"}}, c.err
}

type ai struct {
	err   error
	got   domain.ChatRequest
	turns []domain.TurnEvent
	run   string
}

func (a *ai) Chat(_ context.Context, r domain.ChatRequest) (domain.Answer, error) {
	a.got = r
	return domain.Answer{Answer: "ok [1]", Grounded: true, Citations: []domain.Citation{{N: 1, Title: "t"}}, Turns: a.turns}, a.err
}

func (a *ai) Turns(_ context.Context, run string, _ int) ([]domain.TurnEvent, error) {
	a.run = run
	return a.turns, nil
}

func setup(c ctxs, a *ai) (*application.Service, *store, *time.Time) {
	st := &store{}
	now := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	i := 0
	return application.NewService(application.Deps{Store: st, Contexts: c, AI: a,
		NewID: func() string { i++; return fmt.Sprintf("s%d", i) }, Now: func() time.Time { return now }}), st, &now
}

func code(err error) apperr.Code { e, _ := apperr.As(err); return e.Code }

func TestNewSessionAndTitle(t *testing.T) {
	svc, st, _ := setup(ctxs{}, &ai{})
	long := strings.Repeat("a", 90)
	r, err := svc.Chat(context.Background(), "u1", "", long, "id", "")
	if err != nil || r.SessionID != "s1" || !r.Grounded || r.Message.Role != "assistant" {
		t.Fatalf("%+v %v", r, err)
	}
	if len(st.sessions[0].title) != 60 || len(st.msgs) != 2 {
		t.Fatalf("title %d msgs %d", len(st.sessions[0].title), len(st.msgs))
	}
}

func TestSessionIsolation(t *testing.T) {
	svc, _, _ := setup(ctxs{}, &ai{})
	r, _ := svc.Chat(context.Background(), "u1", "", "hi", "en", "")
	if _, err := svc.Chat(context.Background(), "u2", r.SessionID, "hi", "en", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("chat: %v", err)
	}
	if _, err := svc.Session(context.Background(), "u2", r.SessionID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("get: %v", err)
	}
	if err := svc.Delete(context.Background(), "u2", r.SessionID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("delete: %v", err)
	}
	if l, _ := svc.Sessions(context.Background(), "u2"); len(l) != 0 {
		t.Fatal("list leaks")
	}
	if err := svc.Delete(context.Background(), "u1", r.SessionID); err != nil {
		t.Fatal(err)
	}
}

func TestHistoryWindowExcludesCurrent(t *testing.T) {
	a := &ai{}
	svc, _, _ := setup(ctxs{}, a)
	r, _ := svc.Chat(context.Background(), "u1", "", "m0", "id", "")
	for i := 1; i <= 5; i++ { // 12 stored messages by now
		if _, err := svc.Chat(context.Background(), "u1", r.SessionID, fmt.Sprintf("m%d", i), "id", ""); err != nil {
			t.Fatal(err)
		}
	}
	if len(a.got.History) != 6 || a.got.Message != "m5" || a.got.History[5].Role != "assistant" {
		t.Fatalf("%+v", a.got)
	}
}

func TestAIFailureKeepsUserMessageOnly(t *testing.T) {
	a := &ai{err: errors.New("down")}
	svc, st, _ := setup(ctxs{}, a)
	_, err := svc.Chat(context.Background(), "u1", "", "hello", "id", "")
	if code(err) != apperr.UpstreamFailed || len(st.msgs) != 1 || st.msgs[0].Role != "user" {
		t.Fatalf("%v %d", err, len(st.msgs))
	}
}

func TestNoProfileCreatesNothing(t *testing.T) {
	svc, st, _ := setup(ctxs{err: domain.ErrNoProfile}, &ai{})
	_, err := svc.Chat(context.Background(), "u1", "", "hello", "id", "")
	if !errors.Is(err, domain.ErrNoProfile) || len(st.sessions) != 0 || len(st.msgs) != 0 {
		t.Fatalf("%v", err)
	}
}

func TestValidationAndRateLimit(t *testing.T) {
	svc, _, now := setup(ctxs{}, &ai{})
	for _, c := range []struct{ m, l string }{{"  ", "id"}, {strings.Repeat("x", 1001), "id"}, {"ok", "fr"}} {
		if _, err := svc.Chat(context.Background(), "u1", "", c.m, c.l, ""); code(err) != apperr.ValidationFailed {
			t.Fatalf("%q %q: %v", c.m, c.l, err)
		}
	}
	for i := 0; i < 20; i++ {
		if _, err := svc.Chat(context.Background(), "u1", "", "hi", "id", ""); err != nil {
			t.Fatal(i, err)
		}
	}
	if _, err := svc.Chat(context.Background(), "u1", "", "hi", "id", ""); code(err) != apperr.TooManyRequests {
		t.Fatalf("21st: %v", err)
	}
	if _, err := svc.Chat(context.Background(), "u2", "", "hi", "id", ""); err != nil {
		t.Fatalf("other user: %v", err)
	}
	*now = now.Add(61 * time.Second)
	if _, err := svc.Chat(context.Background(), "u1", "", "hi", "id", ""); err != nil {
		t.Fatalf("after window: %v", err)
	}
}

func TestRunIDIsNamespacedByUserAndTurnsStored(t *testing.T) {
	a := &ai{turns: []domain.TurnEvent{{Sequence: 1, Title: "Mencari...", Type: "processing", Status: "in_progress"}}}
	svc, st, _ := setup(ctxs{}, a)
	run := "11111111-1111-4111-8111-111111111111"
	r, err := svc.Chat(context.Background(), "u1", "", "hi", "id", run)
	if err != nil {
		t.Fatal(err)
	}
	if a.got.RunID != "u1_"+run {
		t.Errorf("run id sent to ai = %q", a.got.RunID)
	}
	if _, err := svc.Chat(context.Background(), "u1", r.SessionID, "again", "id", ""); err != nil || a.got.RunID != "" {
		t.Errorf("empty run id must stay empty: %q %v", a.got.RunID, err)
	}
	if _, err := svc.Turns(context.Background(), "u2", run, 0); err != nil || a.run != "u2_"+run {
		t.Errorf("polling must be scoped to the caller: %q %v", a.run, err)
	}
	if !strings.Contains(string(st.lastTrace), `"turns"`) || !strings.Contains(string(st.lastTrace), "Mencari...") {
		t.Errorf("turns not stored with the message: %s", st.lastTrace)
	}
}

type newsFeed struct{ err error }

func (n newsFeed) Latest(context.Context, string, int) ([]domain.NewsFact, error) {
	return []domain.NewsFact{{Title: "BBCA laba naik", URL: "https://n.example/1"}}, n.err
}

func TestNewsReachesTheAIAndAFailureDoesNotBlockTheAnswer(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want int
	}{{nil, 1}, {errors.New("feed down"), 0}} {
		a := &ai{}
		st := &store{}
		svc := application.NewService(application.Deps{Store: st, Contexts: ctxs{}, AI: a, News: newsFeed{tc.err},
			NewID: func() string { return "s1" }, Now: time.Now})
		if _, err := svc.Chat(context.Background(), "u1", "", "kabar BBCA?", "id", ""); err != nil {
			t.Fatal(err)
		}
		if got := len(a.got.UserContext.News); got != tc.want {
			t.Fatalf("news %d, want %d", got, tc.want)
		}
	}
}

func TestContextHasNoNews(t *testing.T) {
	svc, _, _ := setup(ctxs{}, &ai{})
	uc, err := svc.Context(context.Background(), "u1")
	if err != nil || uc.Profile.RiskProfile != "Moderate" || len(uc.News) != 0 {
		t.Fatalf("%+v %v", uc, err)
	}
}
