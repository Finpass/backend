package application

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"shenzen/backend/internal/features/profile/domain"
	"shenzen/backend/internal/shared/apperr"
)

const explainTimeout = 15 * time.Second

type Service struct {
	repo  domain.Repository
	ai    domain.Explainer // nil disables explanations
	newID func() string
	now   func() time.Time
	// erasers run after the profile data is deleted so other slices (WhatsApp) can erase their personal data too.
	erasers []func(ctx context.Context, userID string) error
}

// AlsoErase registers extra erasure work for DELETE /profile (UU PDP): other slices' personal data goes with the profile.
func (s *Service) AlsoErase(fn func(ctx context.Context, userID string) error) {
	s.erasers = append(s.erasers, fn)
}

func NewService(repo domain.Repository, ai domain.Explainer, newID func() string, now func() time.Time) *Service {
	return &Service{repo: repo, ai: ai, newID: newID, now: now}
}

func (s *Service) Questionnaire() domain.Questionnaire { return domain.Spec }

type View struct {
	Status         string              `json:"status"`
	Consent        *domain.Consent     `json:"consent"`
	Answers        domain.Answers      `json:"answers"`
	OptOut         []string            `json:"optOut"`
	CurrentSection string              `json:"currentSection"`
	UpdatedAt      *time.Time          `json:"updatedAt"`
	Result         *domain.Result      `json:"result"`
	CanRetake      domain.RetakeStatus `json:"canRetake"`
}

func (s *Service) Get(ctx context.Context, userID string) (View, error) {
	v := View{Status: domain.StatusNone, Answers: domain.Answers{}, OptOut: []string{}, CurrentSection: domain.FirstSection()}
	c, err := s.repo.Consent(ctx, userID)
	if err == nil {
		v.Consent = &c
	} else if !errors.Is(err, domain.ErrNotFound) {
		return v, err
	}
	p, err := s.repo.Profile(ctx, userID)
	if err == nil {
		v.Status, v.Answers, v.OptOut, v.CurrentSection, v.UpdatedAt = p.Status, p.Answers, p.OptOut, p.CurrentSection, &p.UpdatedAt
	} else if !errors.Is(err, domain.ErrNotFound) {
		return v, err
	}
	a, err := s.repo.LatestAssessment(ctx, userID)
	if err == nil {
		r := a.Result()
		v.Result = &r
	} else if !errors.Is(err, domain.ErrNotFound) {
		return v, err
	}
	if v.CanRetake, err = s.retakeStatus(ctx, userID); err != nil {
		return v, err
	}
	return v, nil
}

func (s *Service) retakeStatus(ctx context.Context, userID string) (domain.RetakeStatus, error) {
	now := s.now()
	times, err := s.repo.AssessmentTimes(ctx, userID, now.Add(-domain.RetakeWindow))
	if err != nil {
		return domain.RetakeStatus{}, err
	}
	return domain.CanRetake(now, times), nil
}

func (s *Service) Consent(ctx context.Context, userID string, whatsappOptIn bool) (domain.Consent, error) {
	if err := s.repo.SaveConsent(ctx, domain.Consent{UserID: userID, Accepted: true, WhatsappOptIn: whatsappOptIn}, s.now()); err != nil {
		return domain.Consent{}, err
	}
	return s.repo.Consent(ctx, userID)
}

func (s *Service) requireConsent(ctx context.Context, userID string) error {
	c, err := s.repo.Consent(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) || (err == nil && !c.Accepted) {
		return domain.ErrConsentRequired
	}
	return err
}

type SaveCommand struct {
	Section string
	Answers domain.Answers
	OptOut  []string // nil leaves opt-outs untouched
}

type Saved struct {
	Answers        domain.Answers `json:"answers"`
	OptOut         []string       `json:"optOut"`
	CurrentSection string         `json:"currentSection"`
	Missing        []string       `json:"missing"`
}

func (s *Service) SaveAnswers(ctx context.Context, userID string, cmd SaveCommand) (Saved, error) {
	if cmd.Section != "" && !domain.SectionExists(cmd.Section) {
		return Saved{}, apperr.Validation(apperr.Detail{Field: "section", Message: "unknown_section"})
	}
	if err := s.requireConsent(ctx, userID); err != nil {
		return Saved{}, err
	}
	if d := domain.ValidateAnswers(cmd.Answers, cmd.OptOut); len(d) > 0 {
		return Saved{}, apperr.Validation(d...)
	}
	p, err := s.repo.Profile(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) {
		p = domain.Profile{UserID: userID, Status: domain.StatusDraft, CurrentSection: domain.FirstSection(), Answers: domain.Answers{}, OptOut: []string{}}
	} else if err != nil {
		return Saved{}, err
	}
	if p.Status == domain.StatusCompleted {
		return Saved{}, domain.ErrSubmitted
	}
	p.Answers, p.OptOut = domain.Merge(p.Answers, p.OptOut, cmd.Answers, cmd.OptOut, cmd.Section)
	if _, touchesQ8 := cmd.Answers["q8"]; touchesQ8 {
		if d := domain.CheckInvestable(p.Answers, p.OptOut); len(d) > 0 {
			return Saved{}, apperr.Validation(d...)
		}
	}
	if cmd.Section != "" {
		p.CurrentSection = cmd.Section
	}
	p.UpdatedAt = s.now()
	if err := s.repo.SaveDraft(ctx, p); err != nil {
		return Saved{}, err
	}
	return Saved{Answers: p.Answers, OptOut: p.OptOut, CurrentSection: p.CurrentSection, Missing: domain.Missing(p.Answers, p.OptOut)}, nil
}

// UpdateCash changes the liquid savings answer (q4) of a completed profile. The profile stays the single source of the
// cash figure that the dashboard shows; stored assessments are immutable audit records and keep their own copy of the answer.
func (s *Service) UpdateCash(ctx context.Context, userID string, amount int64) error {
	p, err := s.repo.Profile(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ErrNoProfile
	} else if err != nil {
		return err
	}
	if p.Status != domain.StatusCompleted {
		return domain.ErrNoProfile
	}
	a := domain.Answers{"q4": float64(amount)}
	if d := domain.ValidateAnswers(a, nil); len(d) > 0 {
		return apperr.Validation(d...)
	}
	next := make(domain.Answers, len(p.Answers)+1)
	for k, v := range p.Answers {
		next[k] = v
	}
	next["q4"] = float64(amount)
	p.Answers, p.UpdatedAt = next, s.now()
	return s.repo.SaveDraft(ctx, p) // status stays completed: SaveDraft writes the status it is given
}

func (s *Service) Submit(ctx context.Context, userID, locale string) (domain.Result, error) {
	if err := s.requireConsent(ctx, userID); err != nil {
		return domain.Result{}, err
	}
	p, err := s.repo.Profile(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Result{}, domain.ErrNoProfile
	} else if err != nil {
		return domain.Result{}, err
	}
	if p.Status == domain.StatusCompleted {
		return domain.Result{}, domain.ErrSubmitted
	}
	if details := fullValidation(p); len(details) > 0 {
		return domain.Result{}, apperr.Validation(details...)
	}
	st, err := s.retakeStatus(ctx, userID)
	if err != nil {
		return domain.Result{}, err
	}
	if !st.Allowed {
		return domain.Result{}, domain.ErrRetakeNotAllowed(*st.NextAllowedAt)
	}

	var prev *domain.Previous
	if last, err := s.repo.LatestAssessment(ctx, userID); err == nil {
		prev = &domain.Previous{Profile: last.Output.FinalProfile}
	} else if !errors.Is(err, domain.ErrNotFound) {
		return domain.Result{}, err
	}

	out, trace := domain.Evaluate(domain.InputFrom(p.Answers, p.OptOut), prev)
	a := domain.Assessment{
		ID: s.newID(), UserID: userID, QuestionnaireVersion: domain.QuestionnaireVersion, RuleVersion: domain.RuleVersion,
		Input: domain.StoredInput{Answers: p.Answers, OptOut: p.OptOut}, Trace: trace, Output: out, CreatedAt: s.now(),
	}
	a.Explanation = s.explain(ctx, userID, locale, a, p.Answers)
	if err := s.repo.SaveAssessment(ctx, a); err != nil {
		return domain.Result{}, err
	}
	return a.Result(), nil
}

func fullValidation(p domain.Profile) []apperr.Detail {
	details := domain.ValidateAnswers(p.Answers, p.OptOut)
	for _, id := range domain.Missing(p.Answers, p.OptOut) {
		details = append(details, apperr.Detail{Field: id, Message: "required"})
	}
	return append(details, domain.CheckInvestable(p.Answers, p.OptOut)...)
}

// explain never fails the submit: rules decide, the AI only words the result.
func (s *Service) explain(ctx context.Context, userID, locale string, a domain.Assessment, answers domain.Answers) *domain.Explanation {
	if s.ai == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, explainTimeout)
	defer cancel()
	name, _ := s.repo.FirstName(ctx, userID)
	e, err := s.ai.Explain(ctx, domain.ExplainRequest{
		Locale: locale, FirstName: firstWord(name), Result: a.Result(), Answers: answers,
	})
	if err != nil {
		slog.WarnContext(ctx, "profile explanation unavailable, storing none", "err", err)
		return nil
	}
	return e
}

func firstWord(name string) string {
	f, _, _ := strings.Cut(strings.TrimSpace(name), " ")
	return f
}

func (s *Service) Result(ctx context.Context, userID string) (domain.Result, error) {
	a, err := s.repo.LatestAssessment(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Result{}, domain.ErrNoResult
	} else if err != nil {
		return domain.Result{}, err
	}
	return a.Result(), nil
}

func (s *Service) Retake(ctx context.Context, userID string) (View, error) {
	p, err := s.repo.Profile(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return View{}, domain.ErrNoProfile
	} else if err != nil {
		return View{}, err
	}
	if p.Status != domain.StatusDraft {
		st, err := s.retakeStatus(ctx, userID)
		if err != nil {
			return View{}, err
		}
		if !st.Allowed {
			return View{}, domain.ErrRetakeNotAllowed(*st.NextAllowedAt)
		}
		p.Status, p.CurrentSection, p.UpdatedAt = domain.StatusDraft, domain.FirstSection(), s.now()
		if err := s.repo.SaveDraft(ctx, p); err != nil {
			return View{}, err
		}
	}
	return s.Get(ctx, userID)
}

func (s *Service) Delete(ctx context.Context, userID string) error {
	if err := s.repo.Delete(ctx, userID); err != nil {
		return err
	}
	for _, fn := range s.erasers {
		if err := fn(ctx, userID); err != nil {
			return err
		}
	}
	return nil
}
