package application_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"shenzen/backend/internal/features/profile/application"
	"shenzen/backend/internal/features/profile/domain"
	"shenzen/backend/internal/shared/apperr"
)

type fakeRepo struct {
	consent     map[string]domain.Consent
	profiles    map[string]domain.Profile
	assessments []domain.Assessment
	name        string
}

func newRepo() *fakeRepo {
	return &fakeRepo{consent: map[string]domain.Consent{}, profiles: map[string]domain.Profile{}, name: "Dinda Putri"}
}

func (f *fakeRepo) Consent(_ context.Context, id string) (domain.Consent, error) {
	c, ok := f.consent[id]
	if !ok {
		return c, domain.ErrNotFound
	}
	return c, nil
}

func (f *fakeRepo) SaveConsent(_ context.Context, c domain.Consent, now time.Time) error {
	c.AcceptedAt = &now
	f.consent[c.UserID] = c
	return nil
}

func (f *fakeRepo) Profile(_ context.Context, id string) (domain.Profile, error) {
	p, ok := f.profiles[id]
	if !ok {
		return p, domain.ErrNotFound
	}
	return p, nil
}

func (f *fakeRepo) SaveDraft(_ context.Context, p domain.Profile) error {
	f.profiles[p.UserID] = p
	return nil
}

func (f *fakeRepo) LatestAssessment(_ context.Context, id string) (domain.Assessment, error) {
	for _, a := range slices.Backward(f.assessments) {
		if a.UserID == id {
			return a, nil
		}
	}
	return domain.Assessment{}, domain.ErrNotFound
}

func (f *fakeRepo) AssessmentTimes(_ context.Context, id string, since time.Time) ([]time.Time, error) {
	var out []time.Time
	for _, a := range f.assessments {
		if a.UserID == id && a.CreatedAt.After(since) {
			out = append(out, a.CreatedAt)
		}
	}
	return out, nil
}

func (f *fakeRepo) SaveAssessment(_ context.Context, a domain.Assessment) error {
	p := f.profiles[a.UserID]
	if p.Status != domain.StatusDraft {
		return domain.ErrSubmitted
	}
	p.Status = domain.StatusCompleted
	f.profiles[a.UserID] = p
	f.assessments = append(f.assessments, a)
	return nil
}

func (f *fakeRepo) FirstName(context.Context, string) (string, error) { return f.name, nil }

func (f *fakeRepo) Delete(_ context.Context, id string) error {
	delete(f.consent, id)
	delete(f.profiles, id)
	f.assessments = slices.DeleteFunc(f.assessments, func(a domain.Assessment) bool { return a.UserID == id })
	return nil
}

type fakeAI struct {
	err      error
	calls    int
	last     domain.ExplainRequest
	deadline bool
}

func (f *fakeAI) Explain(ctx context.Context, r domain.ExplainRequest) (*domain.Explanation, error) {
	f.calls++
	f.last = r
	d, ok := ctx.Deadline()
	f.deadline = ok && time.Until(d) <= 15*time.Second
	if f.err != nil {
		return nil, f.err
	}
	return &domain.Explanation{Locale: r.Locale, Source: "template", Summary: "hello " + r.FirstName}, nil
}

type env struct {
	svc  *application.Service
	repo *fakeRepo
	ai   *fakeAI
	now  time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{repo: newRepo(), ai: &fakeAI{}, now: time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)}
	n := 0
	e.svc = application.NewService(e.repo, e.ai, func() string { n++; return fmt.Sprintf("a%d", n) }, func() time.Time { return e.now })
	return e
}

func dinda() domain.Answers {
	return domain.Answers{
		"q1": "25_30", "q5": "0", "q10": "freelancer", "q11": "somewhat_stable",
		"q2": 8_000_000.0, "q3": 5_000_000.0, "q4": 7_500_000.0, "q6": []any{"paylater"}, "q6_installment": 500_000.0,
		"q7": 3_000_000.0, "q8": 1_000_000.0, "q9": "1_3", "q23": []any{"mutual_funds", "gold"},
		"q12": "hold", "q13": "balance", "q14": "cautious", "q15": "chance_5m", "q16": "basic", "q16b": "a",
		"q17": "home_purchase", "q18": 300_000_000.0, "q19": "5_10",
		"q20": "rent", "q21": "yes_not_active", "q22": "prefer",
	}
}

const uid = "u1"

func (e *env) ready(t *testing.T) {
	t.Helper()
	if _, err := e.svc.Consent(context.Background(), uid, true); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.SaveAnswers(context.Background(), uid, application.SaveCommand{Answers: dinda()}); err != nil {
		t.Fatal(err)
	}
}

func code(err error) apperr.Code {
	if ae, ok := apperr.As(err); ok {
		return ae.Code
	}
	return ""
}

func TestAnswersRequireConsent(t *testing.T) {
	e := newEnv(t)
	_, err := e.svc.SaveAnswers(context.Background(), uid, application.SaveCommand{Answers: domain.Answers{"q1": "25_30"}})
	if code(err) != apperr.Forbidden {
		t.Fatalf("got %v", err)
	}
	if _, err := e.svc.Submit(context.Background(), uid, "id"); code(err) != apperr.Forbidden {
		t.Fatalf("submit without consent: %v", err)
	}
}

func TestConsentIsReturnedInView(t *testing.T) {
	e := newEnv(t)
	v, _ := e.svc.Get(context.Background(), uid)
	if v.Status != "none" || v.Consent != nil || v.CurrentSection != "section_1" || v.Result != nil || !v.CanRetake.Allowed {
		t.Fatalf("empty view: %+v", v)
	}
	c, err := e.svc.Consent(context.Background(), uid, true)
	if err != nil || !c.Accepted || !c.WhatsappOptIn || c.AcceptedAt == nil {
		t.Fatalf("%+v %v", c, err)
	}
	v, _ = e.svc.Get(context.Background(), uid)
	if v.Consent == nil || !v.Consent.WhatsappOptIn {
		t.Fatalf("%+v", v.Consent)
	}
}

func TestAutosavePartialMergeAndMissing(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_, _ = e.svc.Consent(ctx, uid, false)

	saved, err := e.svc.SaveAnswers(ctx, uid, application.SaveCommand{Section: "section_1", Answers: domain.Answers{"q1": "25_30", "q5": "0"}})
	if err != nil {
		t.Fatal(err)
	}
	if saved.CurrentSection != "section_1" || len(saved.Missing) != 21 || saved.Missing[0] != "q10" {
		t.Fatalf("%+v", saved)
	}
	saved, err = e.svc.SaveAnswers(ctx, uid, application.SaveCommand{Section: "section_2", Answers: domain.Answers{"q2": 8e6}})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Answers["q1"] != "25_30" || saved.Answers["q2"] != 8e6 || saved.CurrentSection != "section_2" {
		t.Fatalf("merge lost data: %+v", saved)
	}
	v, _ := e.svc.Get(ctx, uid)
	if v.Status != "draft" || v.Answers["q5"] != "0" || v.UpdatedAt == nil {
		t.Fatalf("%+v", v)
	}
}

func TestAutosaveValidation(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_, _ = e.svc.Consent(ctx, uid, false)
	cases := []struct {
		name string
		cmd  application.SaveCommand
		want string
	}{
		{"bad option", application.SaveCommand{Answers: domain.Answers{"q1": "x"}}, "q1"},
		{"negative number", application.SaveCommand{Answers: domain.Answers{"q3": -1.0}}, "q3"},
		{"bad section", application.SaveCommand{Section: "section_9"}, "section"},
		{"opt out not allowed", application.SaveCommand{OptOut: []string{"q3"}}, "q3"},
		{"opt out and answer", application.SaveCommand{Answers: domain.Answers{"q2": 1.0}, OptOut: []string{"q2"}}, "q2"},
	}
	for _, c := range cases {
		_, err := e.svc.SaveAnswers(ctx, uid, c.cmd)
		ae, ok := apperr.As(err)
		if !ok || ae.Code != apperr.ValidationFailed || ae.Details[0].Field != c.want {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	if v, _ := e.svc.Get(ctx, uid); v.Status != "none" {
		t.Error("an invalid save must not create a draft")
	}
}

func TestInvestableCrossFieldOnSave(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_, _ = e.svc.Consent(ctx, uid, false)
	_, _ = e.svc.SaveAnswers(ctx, uid, application.SaveCommand{Answers: domain.Answers{"q2": 8e6, "q3": 5e6, "q6": []any{"none"}}})
	_, err := e.svc.SaveAnswers(ctx, uid, application.SaveCommand{Answers: domain.Answers{"q8": 3_000_001.0}})
	ae, ok := apperr.As(err)
	if !ok || ae.Details[0].Field != "q8" {
		t.Fatalf("got %v", err)
	}
	if _, err := e.svc.SaveAnswers(ctx, uid, application.SaveCommand{Answers: domain.Answers{"q8": 3e6}}); err != nil {
		t.Fatal(err)
	}
}

func TestOptOutFlow(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_, _ = e.svc.Consent(ctx, uid, false)
	saved, err := e.svc.SaveAnswers(ctx, uid, application.SaveCommand{Section: "section_2", Answers: domain.Answers{"q2": 8e6}})
	if err != nil {
		t.Fatal(err)
	}
	saved, err = e.svc.SaveAnswers(ctx, uid, application.SaveCommand{Section: "section_2", OptOut: []string{"q2"}})
	if err != nil || !slices.Equal(saved.OptOut, []string{"q2"}) {
		t.Fatalf("%+v %v", saved, err)
	}
	if _, has := saved.Answers["q2"]; has || slices.Contains(saved.Missing, "q2") {
		t.Fatalf("opt out must drop the value and not count as missing: %+v", saved)
	}
}

func TestSubmitRejectsIncompleteAnswers(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_, _ = e.svc.Consent(ctx, uid, false)
	if _, err := e.svc.Submit(ctx, uid, "id"); err == nil || code(err) != apperr.NotFound {
		t.Fatalf("no profile: %v", err)
	}
	_, _ = e.svc.SaveAnswers(ctx, uid, application.SaveCommand{Answers: domain.Answers{"q1": "25_30"}})
	_, err := e.svc.Submit(ctx, uid, "id")
	ae, ok := apperr.As(err)
	if !ok || ae.Code != apperr.ValidationFailed || len(ae.Details) != 22 || ae.Details[0].Message != "required" {
		t.Fatalf("got %v", err)
	}
	if e.ai.calls != 0 || len(e.repo.assessments) != 0 {
		t.Error("nothing may run for an incomplete submission")
	}
}

func TestSubmitStoresImmutableAuditRecord(t *testing.T) {
	e := newEnv(t)
	e.ready(t)
	res, err := e.svc.Submit(context.Background(), uid, "en")
	if err != nil {
		t.Fatal(err)
	}
	if res.ID != "a1" || res.QuestionnaireVersion != "4.0" || res.RuleVersion != "4.0" || res.FinalProfile != "Moderate" || res.LadderLevel != 1 {
		t.Fatalf("%+v", res)
	}
	if res.Explanation == nil || res.Explanation.Summary != "hello Dinda" || e.ai.last.Locale != "en" {
		t.Fatalf("explanation %+v request %+v", res.Explanation, e.ai.last)
	}
	if !e.ai.deadline {
		t.Error("the explanation call must carry a deadline of at most 15s")
	}
	if e.ai.last.Result.Explanation != nil || e.ai.last.Answers["q2"] != 8e6 {
		t.Error("the AI gets the result without explanation plus the validated answers")
	}
	a := e.repo.assessments[0]
	if len(a.Trace) < 20 || a.Input.Answers["q4"] != 7.5e6 || a.Output.CapacityScore != res.CapacityScore || a.QuestionnaireVersion != "4.0" {
		t.Errorf("audit record incomplete: trace=%d", len(a.Trace))
	}
	v, _ := e.svc.Get(context.Background(), uid)
	if v.Status != "completed" || v.Result == nil || v.Result.ID != "a1" || v.CanRetake.Allowed || v.CanRetake.NextAllowedAt == nil || v.CanRetake.RemainingThisYear != 2 {
		t.Fatalf("%+v", v)
	}
}

func TestSubmitSurvivesAIFailure(t *testing.T) {
	for _, err := range []error{errors.New("down"), apperr.New(apperr.UpstreamTimeout, "slow"), context.DeadlineExceeded} {
		e := newEnv(t)
		e.ready(t)
		e.ai.err = err
		res, got := e.svc.Submit(context.Background(), uid, "id")
		if got != nil || res.Explanation != nil || len(e.repo.assessments) != 1 || e.repo.assessments[0].Explanation != nil {
			t.Errorf("%v: result=%+v err=%v", err, res, got)
		}
	}
}

func TestSubmitWithoutAIService(t *testing.T) {
	e := newEnv(t)
	e.svc = application.NewService(e.repo, nil, func() string { return "x" }, func() time.Time { return e.now })
	e.ready(t)
	res, err := e.svc.Submit(context.Background(), uid, "id")
	if err != nil || res.Explanation != nil {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestSubmittedProfileIsLocked(t *testing.T) {
	e := newEnv(t)
	e.ready(t)
	ctx := context.Background()
	if _, err := e.svc.Submit(ctx, uid, "id"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.SaveAnswers(ctx, uid, application.SaveCommand{Answers: domain.Answers{"q1": "31_40"}}); code(err) != apperr.Conflict {
		t.Errorf("save after submit: %v", err)
	}
	if _, err := e.svc.Submit(ctx, uid, "id"); code(err) != apperr.Conflict {
		t.Errorf("double submit: %v", err)
	}
}

func TestResult(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.svc.Result(ctx, uid); code(err) != apperr.NotFound {
		t.Fatalf("got %v", err)
	}
	e.ready(t)
	_, _ = e.svc.Submit(ctx, uid, "id")
	r, err := e.svc.Result(ctx, uid)
	if err != nil || r.ID != "a1" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestRetakeCooldownAndYearlyLimit(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.ready(t)
	if _, err := e.svc.Submit(ctx, uid, "id"); err != nil {
		t.Fatal(err)
	}

	_, err := e.svc.Retake(ctx, uid)
	ae, ok := apperr.As(err)
	if !ok || ae.Code != apperr.Conflict || ae.Details[0].Field != "nextAllowedAt" || ae.Details[0].Message != "2026-11-16T09:00:00Z" {
		t.Fatalf("cooldown: %v", err)
	}

	e.now = e.now.Add(31 * 24 * time.Hour)
	v, err := e.svc.Retake(ctx, uid)
	if err != nil || v.Status != "draft" || v.Answers["q2"] != 8e6 || v.CurrentSection != "section_1" || v.Result == nil {
		t.Fatalf("retake should reopen a draft seeded with the last answers and keep the old result: %+v %v", v, err)
	}
	if again, err := e.svc.Retake(ctx, uid); err != nil || again.Status != "draft" {
		t.Fatalf("retake is idempotent while a draft is open: %v", err)
	}
	if _, err := e.svc.SaveAnswers(ctx, uid, application.SaveCommand{Answers: domain.Answers{"q4": 60_000_000.0}}); err != nil {
		t.Fatal(err)
	}
	second, err := e.svc.Submit(ctx, uid, "id")
	if err != nil || second.ID == "a1" || second.LadderLevel != 2 {
		t.Fatalf("%+v %v", second, err)
	}

	e.now = e.now.Add(31 * 24 * time.Hour)
	if _, err := e.svc.Retake(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Submit(ctx, uid, "id"); err != nil {
		t.Fatal(err)
	}
	e.now = e.now.Add(31 * 24 * time.Hour)
	if _, err := e.svc.Retake(ctx, uid); code(err) != apperr.Conflict {
		t.Fatalf("a fourth assessment within a year must be refused: %v", err)
	}
	v, _ = e.svc.Get(ctx, uid)
	if v.CanRetake.Allowed || v.CanRetake.RemainingThisYear != 0 {
		t.Errorf("%+v", v.CanRetake)
	}
}

func TestRetakeUsesPreviousProfileForHysteresis(t *testing.T) {
	e := newEnv(t)
	e.ready(t)
	_, _ = e.svc.Submit(context.Background(), uid, "id")
	if last, _ := e.repo.LatestAssessment(context.Background(), uid); last.Output.FinalProfile != "Moderate" {
		t.Fatal("fixture")
	}
	e.now = e.now.Add(31 * 24 * time.Hour)
	_, _ = e.svc.Retake(context.Background(), uid)
	res, err := e.svc.Submit(context.Background(), uid, "id")
	if err != nil || res.FinalProfile != "Moderate" {
		t.Fatalf("%+v %v", res, err)
	}
	trace := e.repo.assessments[1].Trace
	if len(trace) == 0 {
		t.Fatal("no trace stored")
	}
}

func TestRetakeWithoutProfile(t *testing.T) {
	if _, err := newEnv(t).svc.Retake(context.Background(), uid); code(err) != apperr.NotFound {
		t.Fatalf("got %v", err)
	}
}

func TestDeleteErasesEverything(t *testing.T) {
	e := newEnv(t)
	e.ready(t)
	_, _ = e.svc.Submit(context.Background(), uid, "id")
	if err := e.svc.Delete(context.Background(), uid); err != nil {
		t.Fatal(err)
	}
	v, _ := e.svc.Get(context.Background(), uid)
	if v.Status != "none" || v.Consent != nil || v.Result != nil || len(e.repo.assessments) != 0 || !v.CanRetake.Allowed {
		t.Fatalf("%+v", v)
	}
	if _, err := e.svc.Result(context.Background(), uid); code(err) != apperr.NotFound {
		t.Error("result must be gone")
	}
}

func TestUsersAreIsolated(t *testing.T) {
	e := newEnv(t)
	e.ready(t)
	_, _ = e.svc.Submit(context.Background(), uid, "id")
	v, _ := e.svc.Get(context.Background(), "someone-else")
	if v.Status != "none" || v.Result != nil || len(v.Answers) != 0 {
		t.Fatalf("%+v", v)
	}
}

func TestUpdateCashChangesOnlyQ4OfACompletedProfile(t *testing.T) {
	e := newEnv(t)
	e.ready(t)
	ctx := context.Background()
	if err := e.svc.UpdateCash(ctx, uid, 5_000_000); code(err) != apperr.NotFound {
		t.Fatalf("a draft profile has no cash to edit: %v", err)
	}
	if _, err := e.svc.Submit(ctx, uid, "id"); err != nil {
		t.Fatal(err)
	}
	before := e.repo.profiles[uid]
	if err := e.svc.UpdateCash(ctx, uid, 12_500_000); err != nil {
		t.Fatal(err)
	}
	after := e.repo.profiles[uid]
	if after.Status != domain.StatusCompleted || after.Answers["q4"] != float64(12_500_000) {
		t.Errorf("status %s q4 %v", after.Status, after.Answers["q4"])
	}
	for k, v := range before.Answers {
		if k != "q4" && fmt.Sprint(after.Answers[k]) != fmt.Sprint(v) {
			t.Errorf("answer %s changed: %v -> %v", k, v, after.Answers[k])
		}
	}
	if before.Answers["q4"] == after.Answers["q4"] {
		t.Error("the stored map must be copied, not shared")
	}
	if len(e.repo.assessments) != 1 {
		t.Errorf("assessments are immutable audit records, got %d", len(e.repo.assessments))
	}
	if err := e.svc.UpdateCash(ctx, "someone-else", 1); code(err) != apperr.NotFound {
		t.Errorf("unknown user: %v", err)
	}
	if err := e.svc.UpdateCash(ctx, uid, -1); code(err) != apperr.ValidationFailed {
		t.Errorf("negative amount: %v", err)
	}
}
