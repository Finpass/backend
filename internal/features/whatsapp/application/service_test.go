package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"shenzen/backend/internal/features/whatsapp/domain"
	"shenzen/backend/internal/shared/apperr"
)

type run struct {
	status   string
	attempts int
	next     *time.Time
	updated  time.Time
	result   domain.RunResult
}

type fakeStore struct {
	mu       sync.Mutex
	contacts map[string]domain.Contact
	verifs   map[string]domain.Verification
	runs     map[string]*run
	msgs     []domain.Message
	events   []domain.Event
	erased   []string
}

func newStore() *fakeStore {
	return &fakeStore{contacts: map[string]domain.Contact{}, verifs: map[string]domain.Verification{}, runs: map[string]*run{}}
}

func (f *fakeStore) Contact(_ context.Context, id string) (domain.Contact, error) {
	if c, ok := f.contacts[id]; ok {
		return c, nil
	}
	return domain.Contact{}, domain.ErrNotFound
}
func (f *fakeStore) SaveContact(_ context.Context, c domain.Contact) error {
	if c.Verified {
		for id, o := range f.contacts {
			if id != c.UserID && o.Verified && o.Phone == c.Phone {
				return domain.ErrPhoneUsed
			}
		}
	}
	if old, ok := f.contacts[c.UserID]; ok { // the real upsert leaves these columns alone
		c.LastSentAt, c.LastInboundAt, c.FailStreak = old.LastSentAt, old.LastInboundAt, old.FailStreak
	}
	f.contacts[c.UserID] = c
	return nil
}
func (f *fakeStore) ContactByPhone(_ context.Context, p string) (domain.Contact, error) {
	for _, c := range f.contacts {
		if c.Phone == p && c.Verified {
			return c, nil
		}
	}
	return domain.Contact{}, domain.ErrNotFound
}
func (f *fakeStore) ListActive(context.Context) ([]domain.Contact, error) {
	var out []domain.Contact
	for _, c := range f.contacts {
		if c.Active() {
			out = append(out, c)
		}
	}
	return out, nil
}
func (f *fakeStore) TouchInbound(_ context.Context, id string, at time.Time) error {
	c := f.contacts[id]
	c.LastInboundAt = &at
	f.contacts[id] = c
	return nil
}
func (f *fakeStore) SetSent(_ context.Context, id string, at time.Time, streak int) error {
	c := f.contacts[id]
	if !at.IsZero() {
		c.LastSentAt = &at
	}
	c.FailStreak = streak
	f.contacts[id] = c
	return nil
}
func (f *fakeStore) SaveVerification(_ context.Context, v domain.Verification) error {
	f.verifs[v.UserID] = v
	return nil
}
func (f *fakeStore) Verification(_ context.Context, id string) (domain.Verification, error) {
	if v, ok := f.verifs[id]; ok {
		return v, nil
	}
	return domain.Verification{}, domain.ErrNotFound
}
func (f *fakeStore) AddVerificationAttempt(_ context.Context, id string) error {
	v := f.verifs[id]
	v.Attempts++
	f.verifs[id] = v
	return nil
}
func (f *fakeStore) DeleteVerification(_ context.Context, id string) error {
	delete(f.verifs, id)
	return nil
}
func (f *fakeStore) ClaimRun(_ context.Context, id, date string, now time.Time) (int, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := id + "|" + date
	r, ok := f.runs[k]
	if !ok {
		f.runs[k] = &run{status: "sending", attempts: 1, updated: now}
		return 1, true, nil
	}
	if (r.status == "retry" && r.next != nil && !r.next.After(now)) || (r.status == "sending" && r.updated.Before(now.Add(-15*time.Minute))) {
		r.status, r.attempts, r.updated = "sending", r.attempts+1, now
		return r.attempts, true, nil
	}
	return 0, false, nil
}
func (f *fakeStore) FinishRun(_ context.Context, id, date string, res domain.RunResult) error {
	r := f.runs[id+"|"+date]
	r.status, r.next, r.result = res.Status, res.NextAttempt, res
	return nil
}
func (f *fakeStore) SentCount(_ context.Context, id string) (int, error) {
	n := 0
	for k, r := range f.runs {
		if strings.HasPrefix(k, id+"|") && r.status == "sent" {
			n++
		}
	}
	return n, nil
}
func (f *fakeStore) LogMessage(_ context.Context, m domain.Message) error {
	for _, o := range f.msgs {
		if o.ID == m.ID {
			return domain.ErrDuplicate
		}
	}
	f.msgs = append(f.msgs, m)
	return nil
}
func (f *fakeStore) LogEvent(_ context.Context, e domain.Event) error {
	f.events = append(f.events, e)
	return nil
}
func (f *fakeStore) Erase(_ context.Context, id string) error {
	f.erased = append(f.erased, id)
	delete(f.contacts, id)
	return nil
}
func (f *fakeStore) kinds() string {
	var k []string
	for _, e := range f.events {
		k = append(k, e.Kind)
	}
	return strings.Join(k, ",")
}

type sent struct{ phone, text string }

type fakeSender struct {
	out       []sent
	failTimes int
	connected bool
	statusErr error
}

func (s *fakeSender) Send(_ context.Context, p, t string) (string, error) {
	if s.failTimes > 0 {
		s.failTimes--
		return "", errors.New("gowa status 500: boom")
	}
	s.out = append(s.out, sent{p, t})
	return fmt.Sprintf("MSG%d", len(s.out)), nil
}
func (s *fakeSender) Connected(context.Context) (bool, error) { return s.connected, s.statusErr }

type fakeMatcher struct {
	content domain.Content
	keys    map[string]string
	err     error
	calls   int
}

func (m *fakeMatcher) Content(context.Context, string, time.Time) (domain.Content, map[string]string, error) {
	m.calls++
	return m.content, m.keys, m.err
}

type names struct{}

func (names) FirstName(context.Context, string) (string, error) { return "Surya", nil }

type env struct {
	svc *Service
	st  *fakeStore
	snd *fakeSender
	mat *fakeMatcher
	now time.Time
}

const secret = "whsec"

func setup() *env {
	jkt, _ := time.LoadLocation("Asia/Jakarta")
	e := &env{st: newStore(), snd: &fakeSender{connected: true}, now: time.Date(2026, 10, 16, 7, 1, 0, 0, jkt)}
	e.mat = &fakeMatcher{
		content: domain.Content{Name: "x", Risk: "Moderate", Ladder: 3, HealthTotal: 70, HealthCat: "Good", News: &domain.News{Title: "Berita satu"}, MoreNews: []domain.News{{Title: "Berita dua"}},
			Nudge: domain.Nudge{Key: "emergency_fund", Action: domain.Text{ID: "Sisihkan Rp 500.000.", EN: "Set aside Rp 500.000."}, Reason: domain.Text{ID: "Dana darurat 1 bulan.", EN: "1 month."}}},
		keys: map[string]string{"nudgeKey": "emergency_fund", "newsId": "n1", "newsTopic": "macro"},
	}
	e.svc = NewService(Deps{Store: e.st, Matcher: e.mat, Sender: e.snd, Names: names{}, Link: "http://app.test", Secret: secret, NewID: func() string { return fmt.Sprint(len(e.st.msgs)) + "-id" }, Now: func() time.Time { return e.now }})
	return e
}

func (e *env) verified(user, phone string) {
	t := e.now
	c := domain.NewContact(user)
	c.Phone, c.Verified, c.VerifiedAt, c.OptIn = phone, true, &t, true
	e.st.contacts[user] = c
}

func code(t *testing.T, e *env) string {
	t.Helper()
	m := regexp.MustCompile(`\b(\d{6})\b`).FindStringSubmatch(e.snd.out[len(e.snd.out)-1].text)
	if m == nil {
		t.Fatalf("no code in %q", e.snd.out[len(e.snd.out)-1].text)
	}
	return m[1]
}

func wantCode(t *testing.T, err error, c apperr.Code) {
	t.Helper()
	if ae, ok := apperr.As(err); !ok || ae.Code != c {
		t.Fatalf("want %s, got %v", c, err)
	}
}

func TestSettingsNormalisationAndReset(t *testing.T) {
	e := setup()
	ctx := context.Background()
	s, err := e.svc.UpdateSettings(ctx, "u1", SettingsUpdate{Phone: "0812-3456-7890", OptIn: true, SendTimeLocal: "06:30", Locale: "en"})
	if err != nil || *s.Phone != "+6281234567890" || s.Verified || s.SendTimeLocal != "06:30" || s.Timezone != "Asia/Jakarta" {
		t.Fatalf("%+v %v", s, err)
	}
	if e.st.contacts["u1"].Locale != "en" {
		t.Error("locale saved")
	}
	// verified, then a different number resets verification
	e.verified("u1", "+6281234567890")
	s, _ = e.svc.UpdateSettings(ctx, "u1", SettingsUpdate{Phone: "081299990000", OptIn: true})
	if s.Verified || *s.Phone != "+6281299990000" {
		t.Errorf("new number must be re-verified: %+v", s)
	}
	// same number keeps verification
	e.verified("u1", "+6281234567890")
	if s, _ = e.svc.UpdateSettings(ctx, "u1", SettingsUpdate{Phone: "+62 812 3456 7890", OptIn: true}); !s.Verified {
		t.Error("same number keeps verified")
	}
	for name, u := range map[string]SettingsUpdate{
		"bad phone":     {Phone: "123"},
		"opt in no tel": {Phone: "", OptIn: true},
		"bad time":      {Phone: "0812345678", SendTimeLocal: "7am"},
		"bad frequency": {Phone: "0812345678", Frequency: "hourly"},
		"bad timezone":  {Phone: "0812345678", Timezone: "Mars/Base"},
	} {
		if _, err := e.svc.UpdateSettings(ctx, "u1", u); err == nil {
			t.Errorf("%s should fail", name)
		} else {
			wantCode(t, err, apperr.ValidationFailed)
		}
	}
	if s, _ := e.svc.UpdateSettings(ctx, "u1", SettingsUpdate{Phone: "", OptIn: false}); s.Phone != nil || s.Verified {
		t.Errorf("clearing the phone: %+v", s)
	}
	if got, _ := e.svc.Settings(ctx, "nobody"); got.OptIn || got.SendTimeLocal != "07:00" || got.Phone != nil {
		t.Errorf("defaults %+v", got)
	}
}

func TestPhoneUsedByAnotherAccount(t *testing.T) {
	e := setup()
	e.verified("other", "+6281234567890")
	ctx := context.Background()
	_, _ = e.svc.UpdateSettings(ctx, "u1", SettingsUpdate{Phone: "081234567890"})
	if err := e.svc.VerifyStart(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	_, err := e.svc.VerifyConfirm(ctx, "u1", code(t, e))
	wantCode(t, err, apperr.Conflict)
}

func TestVerifyFlow(t *testing.T) {
	e := setup()
	ctx := context.Background()
	wantCode(t, e.svc.VerifyStart(ctx, "u1"), apperr.ValidationFailed) // no phone saved
	_, _ = e.svc.UpdateSettings(ctx, "u1", SettingsUpdate{Phone: "081234567890", OptIn: true})

	e.snd.connected = false
	wantCode(t, e.svc.VerifyStart(ctx, "u1"), apperr.Unavailable)
	e.snd.connected = true

	if err := e.svc.VerifyStart(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	c := code(t, e)
	if e.snd.out[0].phone != "+6281234567890" {
		t.Error("sent to the saved number")
	}
	if v := e.st.verifs["u1"]; v.CodeHash == "" || strings.Contains(v.CodeHash, c) {
		t.Error("code must be stored hashed")
	}
	for _, m := range e.st.msgs {
		if strings.Contains(m.Body, c) {
			t.Error("verification code leaked into the message log")
		}
	}
	wantCode(t, e.svc.VerifyStart(ctx, "u1"), apperr.TooManyRequests) // 60s cooldown
	e.now = e.now.Add(61 * time.Second)

	// wrong code counts attempts, 5 attempts lock
	for i := 0; i < domain.VerifyMaxAttempts; i++ {
		_, err := e.svc.VerifyConfirm(ctx, "u1", "000000")
		if c == "000000" {
			t.Skip("random collision")
		}
		wantCode(t, err, apperr.ValidationFailed)
	}
	_, err := e.svc.VerifyConfirm(ctx, "u1", c)
	wantCode(t, err, apperr.TooManyRequests)

	// a fresh code works and verifies
	if err := e.svc.VerifyStart(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	s, err := e.svc.VerifyConfirm(ctx, "u1", code(t, e))
	if err != nil || !s.Verified {
		t.Fatalf("%+v %v", s, err)
	}
	if _, ok := e.st.verifs["u1"]; ok {
		t.Error("verification consumed")
	}
	_, err = e.svc.VerifyConfirm(ctx, "u1", code(t, e))
	wantCode(t, err, apperr.ValidationFailed) // single use

	// expired
	_ = e.svc.VerifyStart(ctx, "u1")
	cc := code(t, e)
	e.now = e.now.Add(11 * time.Minute)
	_, err = e.svc.VerifyConfirm(ctx, "u1", cc)
	wantCode(t, err, apperr.ValidationFailed)

	// gateway send failure removes the stored code
	e.snd.failTimes = 1
	e.now = e.now.Add(2 * time.Minute)
	wantCode(t, e.svc.VerifyStart(ctx, "u1"), apperr.UpstreamFailed)
	if _, ok := e.st.verifs["u1"]; ok {
		t.Error("failed send must not leave a usable code")
	}
}

func TestPreviewAndSendTest(t *testing.T) {
	e := setup()
	ctx := context.Background()
	_, _ = e.svc.UpdateSettings(ctx, "u1", SettingsUpdate{Phone: "081234567890"})
	p, err := e.svc.Preview(ctx, "u1", "en")
	if err != nil || !strings.Contains(p.Text, "Welcome, Surya!") || len(e.snd.out) != 0 {
		t.Fatalf("%v %q", err, p.Text)
	}
	if p, _ = e.svc.Preview(ctx, "u1", ""); !strings.Contains(p.Text, "Selamat datang") {
		t.Errorf("default locale: %q", p.Text)
	}
	if p, _ = e.svc.Preview(ctx, "u1", "id"); !strings.Contains(p.Text, "Selamat datang, Surya!") { // no briefing sent yet: new user
		t.Errorf("new user welcome: %q", p.Text)
	}
	_, err = e.svc.SendTest(ctx, "u1", "id")
	wantCode(t, err, apperr.Conflict) // not verified
	e.verified("u1", "+6281234567890")
	if _, err = e.svc.SendTest(ctx, "u1", "id"); err != nil || len(e.snd.out) != 1 || e.snd.out[0].phone != "+6281234567890" {
		t.Fatalf("%v %v", err, e.snd.out)
	}
	e.mat.err = domain.ErrNoProfile
	_, err = e.svc.Preview(ctx, "u1", "id")
	wantCode(t, err, apperr.NotFound)
}

func hook(from, body, id string) []byte {
	b, _ := json.Marshal(map[string]any{"event": "message", "device_id": "62899@s.whatsapp.net", "payload": map[string]any{"id": id, "chat_id": from, "from": from, "is_from_me": false, "body": body}})
	return b
}

func TestWebhook(t *testing.T) {
	e := setup()
	ctx := context.Background()
	e.verified("u1", "+6281234567890")
	jid := "6281234567890@s.whatsapp.net"
	post := func(body []byte) error { return e.svc.HandleWebhook(ctx, body, domain.Sign(secret, body)) }

	if err := e.svc.HandleWebhook(ctx, hook(jid, "MORE", "a"), "sha256=00"); err == nil {
		t.Fatal("bad signature must be rejected")
	}
	wantCode(t, e.svc.HandleWebhook(ctx, hook(jid, "MORE", "a"), ""), apperr.Unauthorized)
	if len(e.snd.out) != 0 || len(e.st.msgs) != 0 {
		t.Fatal("nothing happens before the signature is verified")
	}

	if err := post(hook(jid, "MORE", "m1")); err != nil {
		t.Fatal(err)
	}
	if len(e.snd.out) != 1 || !strings.Contains(e.snd.out[0].text, "Berita satu") || !strings.Contains(e.snd.out[0].text, "Berita dua") || e.snd.out[0].phone != "+6281234567890" {
		t.Errorf("MORE reply: %+v", e.snd.out)
	}
	if err := post(hook(jid, "MORE", "m1")); err != nil || len(e.snd.out) != 1 { // GoWA retry of the same message id
		t.Errorf("duplicate delivery must be ignored, sent %d", len(e.snd.out))
	}

	if err := post(hook(jid, "stop", "m2")); err != nil {
		t.Fatal(err)
	}
	c := e.st.contacts["u1"]
	if c.OptIn || c.PausedAt == nil || c.Active() || !strings.Contains(e.snd.out[1].text, "dijeda") {
		t.Errorf("STOP: %+v / %v", c, e.snd.out)
	}
	if c.LastInboundAt == nil {
		t.Error("inbound timestamp")
	}
	if err := post(hook(jid, "RESUME", "m3")); err != nil {
		t.Fatal(err)
	}
	if c = e.st.contacts["u1"]; !c.OptIn || c.PausedAt != nil || !c.Active() {
		t.Errorf("RESUME: %+v", c)
	}
	_ = post(hook(jid, "PROFILE", "m4"))
	_ = post(hook(jid, "UPDATE", "m5"))
	_ = post(hook(jid, "terima kasih", "m6"))
	_ = post(hook(jid, "berapa bunga deposito?", "m7"))
	last := e.snd.out[len(e.snd.out)-4:]
	for i, want := range []string{"Risiko: Moderate", "http://app.test/onboarding", "Command yang tersedia", "belum tersedia"} {
		if !strings.Contains(last[i].text, want) {
			t.Errorf("reply %d lacks %q: %s", i, want, last[i].text)
		}
	}
	for _, want := range []string{"opt_out", "opt_in", "reply"} {
		if !strings.Contains(e.st.kinds(), want) {
			t.Errorf("analytics events lack %s: %s", want, e.st.kinds())
		}
	}
	n := len(e.snd.out)
	_ = post(hook("6289999999999@s.whatsapp.net", "MORE", "x1")) // unknown number
	_ = post(hook("120363@g.us", "MORE", "x2"))                  // group
	own, _ := json.Marshal(map[string]any{"event": "message", "payload": map[string]any{"id": "x3", "from": jid, "chat_id": jid, "is_from_me": true, "body": "MORE"}})
	_ = post(own)
	other, _ := json.Marshal(map[string]any{"event": "message.ack", "payload": map[string]any{"id": "x4", "from": jid, "body": "MORE"}})
	_ = post(other)
	if len(e.snd.out) != n {
		t.Errorf("ignored messages triggered %d replies", len(e.snd.out)-n)
	}
	// a stopped, unverified or foreign contact never receives replies; and the body is stored without phone numbers
	for _, m := range e.st.msgs {
		if strings.Contains(m.Body, "6281234567890") {
			t.Errorf("phone in message log: %q", m.Body)
		}
	}
}

func TestSchedulerIdempotentAndHonoursSettings(t *testing.T) {
	e := setup()
	ctx := context.Background()
	e.verified("u1", "+6281234567890")

	e.now = e.now.Add(-2 * time.Hour) // 05:01 local, before 07:00
	if s, f := e.svc.Tick(ctx); s+f != 0 || len(e.snd.out) != 0 {
		t.Fatal("not due yet")
	}
	e.now = e.now.Add(2 * time.Hour)
	if s, f := e.svc.Tick(ctx); s != 1 || f != 0 {
		t.Fatalf("sent %d failed %d", s, f)
	}
	for i := 0; i < 3; i++ { // repeated ticks, even from a "second instance", never resend
		e.svc.Tick(ctx)
	}
	e.now = e.now.Add(3 * time.Hour)
	e.svc.Tick(ctx)
	if len(e.snd.out) != 1 {
		t.Fatalf("sent %d briefings, want exactly 1", len(e.snd.out))
	}
	r := e.st.runs["u1|2026-10-16"]
	if r.status != "sent" || r.result.Keys["newsId"] != "n1" || r.result.CharCount == 0 {
		t.Errorf("run %+v", r)
	}
	if c := e.st.contacts["u1"]; c.LastSentAt == nil || c.FailStreak != 0 {
		t.Errorf("contact %+v", c)
	}
	if !strings.Contains(e.st.kinds(), "briefing_sent") || !strings.Contains(e.st.kinds(), "slot_included") {
		t.Errorf("analytics %s", e.st.kinds())
	}
	// next local day sends again
	e.now = e.now.Add(24 * time.Hour)
	e.svc.Tick(ctx)
	if len(e.snd.out) != 2 {
		t.Errorf("next day: %d", len(e.snd.out))
	}
	// opt-out, STOP and unverified are honoured
	c := e.st.contacts["u1"]
	c.OptIn = false
	e.st.contacts["u1"] = c
	e.now = e.now.Add(24 * time.Hour)
	e.svc.Tick(ctx)
	c.OptIn, c.Verified = true, false
	e.st.contacts["u1"] = c
	e.svc.Tick(ctx)
	if len(e.snd.out) != 2 {
		t.Errorf("opt-out/unverified still sent: %d", len(e.snd.out))
	}
}

func TestSchedulerRetryAndFailureStreak(t *testing.T) {
	e := setup()
	ctx := context.Background()
	e.verified("u1", "+6281234567890")
	e.snd.failTimes = 100

	e.svc.Tick(ctx) // attempt 1 fails
	r := e.st.runs["u1|2026-10-16"]
	if r.status != "retry" || r.next == nil {
		t.Fatalf("%+v", r)
	}
	e.now = e.now.Add(4 * time.Minute)
	e.svc.Tick(ctx) // too early
	if r.attempts != 1 {
		t.Error("retry before 5 minutes")
	}
	e.now = e.now.Add(2 * time.Minute)
	e.svc.Tick(ctx) // attempt 2
	e.now = e.now.Add(6 * time.Minute)
	e.svc.Tick(ctx) // attempt 3 -> failed
	if r.status != "failed" || r.attempts != 3 {
		t.Fatalf("%+v", r)
	}
	e.now = e.now.Add(6 * time.Minute)
	e.svc.Tick(ctx)
	if r.attempts != 3 {
		t.Error("no more retries after 3")
	}
	if e.st.contacts["u1"].FailStreak != 1 {
		t.Error("streak 1")
	}
	// gateway recovers: a retry that succeeds is delivered once
	e2 := setup()
	e2.verified("u1", "+6281234567890")
	e2.snd.failTimes = 1
	e2.svc.Tick(ctx)
	e2.now = e2.now.Add(6 * time.Minute)
	e2.svc.Tick(ctx)
	e2.svc.Tick(ctx)
	if len(e2.snd.out) != 1 || e2.st.runs["u1|2026-10-16"].status != "sent" {
		t.Errorf("retry delivery: %d %+v", len(e2.snd.out), e2.st.runs)
	}
	// three failed days in a row -> delivery_issue event
	e3 := setup()
	e3.verified("u1", "+6281234567890")
	e3.snd.failTimes = 1000
	for d := 0; d < 3; d++ {
		for i := 0; i < 4; i++ {
			e3.svc.Tick(ctx)
			e3.now = e3.now.Add(6 * time.Minute)
		}
		e3.now = time.Date(2026, 10, 17+d, 7, 1, 0, 0, e3.now.Location())
	}
	if !strings.Contains(e3.st.kinds(), "delivery_issue") {
		t.Errorf("events %s streak %d", e3.st.kinds(), e3.st.contacts["u1"].FailStreak)
	}
	if s, _ := e3.svc.Settings(ctx, "u1"); !s.DeliveryIssue {
		t.Error("settings expose deliveryIssue")
	}
}

func TestSchedulerNoProfileAndStuckRun(t *testing.T) {
	e := setup()
	ctx := context.Background()
	e.verified("u1", "+6281234567890")
	e.mat.err = domain.ErrNoProfile
	e.svc.Tick(ctx)
	if r := e.st.runs["u1|2026-10-16"]; r.status != "failed" || len(e.snd.out) != 0 || e.st.contacts["u1"].FailStreak != 0 {
		t.Errorf("no profile: %+v", r)
	}
	// a run stuck in 'sending' for 15 minutes is re-claimed
	e2 := setup()
	e2.verified("u1", "+6281234567890")
	e2.st.runs["u1|2026-10-16"] = &run{status: "sending", attempts: 1, updated: e2.now.Add(-20 * time.Minute)}
	e2.svc.Tick(ctx)
	if len(e2.snd.out) != 1 {
		t.Error("stuck run should be retried")
	}
	e3 := setup()
	e3.verified("u1", "+6281234567890")
	e3.st.runs["u1|2026-10-16"] = &run{status: "sending", attempts: 1, updated: e3.now.Add(-2 * time.Minute)}
	e3.svc.Tick(ctx)
	if len(e3.snd.out) != 0 {
		t.Error("a fresh in-flight run belongs to another worker")
	}
}

func TestAutoPauseAndReengagePolicy(t *testing.T) {
	e := setup()
	ctx := context.Background()
	e.verified("u1", "+6281234567890")
	old := e.now.Add(-30 * 24 * time.Hour)
	c := e.st.contacts["u1"]
	c.LastInboundAt = &old
	e.st.contacts["u1"] = c
	e.svc.d.Policy = Policy{ReengageAfterDays: 7, AutoPauseAfterDays: 30}
	e.svc.Tick(ctx)
	if c = e.st.contacts["u1"]; c.OptIn || c.PausedAt == nil || len(e.snd.out) != 0 {
		t.Errorf("30 inactive days auto-pauses: %+v", c)
	}
	e2 := setup()
	e2.verified("u1", "+6281234567890")
	week := e2.now.Add(-7 * 24 * time.Hour)
	c = e2.st.contacts["u1"]
	c.LastInboundAt = &week
	e2.st.contacts["u1"] = c
	e2.svc.d.Policy = Policy{ReengageAfterDays: 7}
	e2.svc.Tick(ctx)
	if len(e2.snd.out) != 1 || !strings.Contains(e2.snd.out[0].text, "Kami rindu") {
		t.Errorf("re-engagement: %+v", e2.snd.out)
	}
}

func TestErase(t *testing.T) {
	e := setup()
	e.verified("u1", "+6281234567890")
	if err := e.svc.Erase(context.Background(), "u1"); err != nil || len(e.st.erased) != 1 {
		t.Fatal("erase")
	}
}
