package application_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"shenzen/backend/internal/features/auth/application"
	"shenzen/backend/internal/features/auth/domain"
	"shenzen/backend/internal/shared/apperr"
)

type memStore struct {
	users    map[string]domain.User
	otps     map[string]domain.OTP
	sessions map[string]domain.Session
}

func newStore() *memStore {
	return &memStore{users: map[string]domain.User{}, otps: map[string]domain.OTP{}, sessions: map[string]domain.Session{}}
}

func (m *memStore) Create(_ context.Context, u domain.User) error {
	m.users[u.ID] = u
	return nil
}

func (m *memStore) find(match func(domain.User) bool) (domain.User, error) {
	for _, u := range m.users {
		if match(u) {
			return u, nil
		}
	}
	return domain.User{}, domain.ErrNotFound
}

func (m *memStore) FindByEmail(_ context.Context, e string) (domain.User, error) {
	return m.find(func(u domain.User) bool { return u.Email == e })
}
func (m *memStore) FindByID(_ context.Context, id string) (domain.User, error) {
	return m.find(func(u domain.User) bool { return u.ID == id })
}
func (m *memStore) FindByGoogleSub(_ context.Context, s string) (domain.User, error) {
	return m.find(func(u domain.User) bool { return u.GoogleSub == s })
}
func (m *memStore) update(id string, f func(*domain.User)) error {
	u := m.users[id]
	f(&u)
	m.users[id] = u
	return nil
}
func (m *memStore) UpdateCredentials(_ context.Context, id, n, h string) error {
	return m.update(id, func(u *domain.User) { u.Name, u.PasswordHash = n, h })
}
func (m *memStore) SetPassword(_ context.Context, id, h string) error {
	return m.update(id, func(u *domain.User) { u.PasswordHash = h })
}
func (m *memStore) MarkVerified(_ context.Context, id string) error {
	return m.update(id, func(u *domain.User) { u.EmailVerified = true })
}
func (m *memStore) LinkGoogle(_ context.Context, id, s string) error {
	return m.update(id, func(u *domain.User) { u.GoogleSub, u.EmailVerified = s, true })
}

func (m *memStore) Delete(_ context.Context, id string) error {
	delete(m.users, id)
	return nil
}

type otpRepo struct{ *memStore }

func (r otpRepo) Replace(_ context.Context, o domain.OTP) error {
	r.otps[o.UserID+string(o.Purpose)] = o
	return nil
}
func (r otpRepo) Latest(_ context.Context, uid string, p domain.Purpose) (domain.OTP, error) {
	o, ok := r.otps[uid+string(p)]
	if !ok {
		return domain.OTP{}, domain.ErrNotFound
	}
	return o, nil
}
func (r otpRepo) mutate(id string, f func(*domain.OTP)) error {
	for k, o := range r.otps {
		if o.ID == id {
			f(&o)
			r.otps[k] = o
		}
	}
	return nil
}
func (r otpRepo) AddAttempt(_ context.Context, id string) error {
	return r.mutate(id, func(o *domain.OTP) { o.Attempts++ })
}
func (r otpRepo) Consume(_ context.Context, id string) error {
	return r.mutate(id, func(o *domain.OTP) { o.Consumed = true })
}

type sessionRepo struct{ *memStore }

func (r sessionRepo) Create(_ context.Context, s domain.Session) error {
	r.sessions[s.TokenHash] = s
	return nil
}
func (r sessionRepo) FindByTokenHash(_ context.Context, h string) (domain.Session, error) {
	s, ok := r.sessions[h]
	if !ok {
		return domain.Session{}, domain.ErrNotFound
	}
	return s, nil
}
func (r sessionRepo) Revoke(_ context.Context, id string, at time.Time) error {
	for k, s := range r.sessions {
		if s.ID == id {
			s.RevokedAt = at
			r.sessions[k] = s
		}
	}
	return nil
}
func (r sessionRepo) RevokeAll(_ context.Context, uid string, at time.Time) error {
	for k, s := range r.sessions {
		if s.UserID == uid {
			s.RevokedAt = at
			r.sessions[k] = s
		}
	}
	return nil
}

type plainHasher struct{}

func (plainHasher) Hash(p string) (string, error) { return "h:" + p, nil }
func (plainHasher) Compare(h, p string) bool      { return h == "h:"+p }

type fakeTokens struct{}

func (fakeTokens) Issue(id string, now time.Time) (string, time.Time, error) {
	return "at:" + id, now.Add(time.Minute), nil
}
func (fakeTokens) Parse(t string, _ time.Time) (string, error) { return t[3:], nil }

type captureMailer struct{ code string }

func (c *captureMailer) SendOTP(_ context.Context, _, _ string, _ domain.Purpose, code string) error {
	c.code = code
	return nil
}

type fakeGoogle struct{ p domain.GoogleProfile }

func (f fakeGoogle) Verify(context.Context, string) (domain.GoogleProfile, error) { return f.p, nil }

func newService(t *testing.T, now *time.Time, google domain.GoogleVerifier) (*application.Service, *captureMailer) {
	t.Helper()
	st, mail, n := newStore(), &captureMailer{}, 0
	svc, err := application.NewService(application.Deps{
		Users: st, OTPs: otpRepo{st}, Sessions: sessionRepo{st}, Hasher: plainHasher{}, Tokens: fakeTokens{}, Mailer: mail, Google: google,
		NewID:  func() string { n++; return fmt.Sprint("id", n) },
		Now:    func() time.Time { return *now },
		Config: application.Config{RefreshTTL: time.Hour, RememberTTL: 24 * time.Hour},
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc, mail
}

func code(t *testing.T, err error) apperr.Code {
	t.Helper()
	ae, ok := apperr.As(err)
	if !ok {
		t.Fatalf("expected apperr, got %v", err)
	}
	return ae.Code
}

func TestRegisterVerifyLoginRefreshLogout(t *testing.T) {
	ctx, now := context.Background(), time.Now()
	svc, mail := newService(t, &now, nil)
	reg := application.RegisterCommand{Name: "Ana", Email: "Ana@Example.com", Password: "secret123"}

	if _, err := svc.Register(ctx, reg); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(ctx, application.LoginCommand{Email: reg.Email, Password: reg.Password}); code(t, err) != apperr.Forbidden {
		t.Fatal("unverified login must be forbidden")
	}
	if _, err := svc.VerifyEmail(ctx, application.VerifyCommand{Email: reg.Email, Code: "000000"}); code(t, err) != apperr.Unprocessable {
		t.Fatal("wrong code must be rejected")
	}
	res, err := svc.VerifyEmail(ctx, application.VerifyCommand{Email: reg.Email, Code: mail.code})
	if err != nil || res.User.Email != "ana@example.com" {
		t.Fatalf("verify: %+v %v", res, err)
	}
	if _, err := svc.VerifyEmail(ctx, application.VerifyCommand{Email: reg.Email, Code: mail.code}); err == nil {
		t.Fatal("a code must be single use")
	}
	if _, err := svc.Register(ctx, reg); code(t, err) != apperr.Conflict {
		t.Fatal("verified email must conflict")
	}
	if _, err := svc.Login(ctx, application.LoginCommand{Email: reg.Email, Password: "wrong-pass1"}); code(t, err) != apperr.Unauthorized {
		t.Fatal("bad password must be unauthorized")
	}
	login, err := svc.Login(ctx, application.LoginCommand{Email: reg.Email, Password: reg.Password, Remember: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := login.Tokens.RefreshExpiresAt.Sub(now); got != 24*time.Hour {
		t.Fatalf("remember TTL = %v", got)
	}

	next, err := svc.Refresh(ctx, login.Tokens.Refresh, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Refresh(ctx, login.Tokens.Refresh, ""); code(t, err) != apperr.Unauthorized {
		t.Fatal("rotated token replay must fail")
	}
	if _, err := svc.Refresh(ctx, next.Tokens.Refresh, ""); err != nil {
		t.Fatal("a replay inside the grace window must not end the family")
	}
	now = now.Add(domain.RefreshGrace + time.Second)
	if _, err := svc.Refresh(ctx, login.Tokens.Refresh, ""); code(t, err) != apperr.Unauthorized {
		t.Fatal("late replay must fail")
	}
	if _, err := svc.Refresh(ctx, next.Tokens.Refresh, ""); err == nil {
		t.Fatal("late replay must revoke the whole family")
	}
}

func TestOTPAttemptsAreLimited(t *testing.T) {
	ctx, now := context.Background(), time.Now()
	svc, mail := newService(t, &now, nil)
	if _, err := svc.Register(ctx, application.RegisterCommand{Name: "Bo", Email: "bo@example.com", Password: "secret123"}); err != nil {
		t.Fatal(err)
	}
	for range domain.OTPMaxAttempts {
		_, _ = svc.VerifyEmail(ctx, application.VerifyCommand{Email: "bo@example.com", Code: "000000"})
	}
	_, err := svc.VerifyEmail(ctx, application.VerifyCommand{Email: "bo@example.com", Code: mail.code})
	if code(t, err) != apperr.TooManyRequests {
		t.Fatalf("exhausted code must lock, got %v", err)
	}
}

func TestPasswordReset(t *testing.T) {
	ctx, now := context.Background(), time.Now()
	svc, mail := newService(t, &now, nil)
	if err := svc.ForgotPassword(ctx, "ghost@example.com"); err != nil {
		t.Fatal("unknown email must not error")
	}
	if mail.code != "" {
		t.Fatal("unknown email must not send mail")
	}
	_, _ = svc.Register(ctx, application.RegisterCommand{Name: "Cy", Email: "cy@example.com", Password: "secret123"})
	res, _ := svc.VerifyEmail(ctx, application.VerifyCommand{Email: "cy@example.com", Code: mail.code})

	if err := svc.ForgotPassword(ctx, "cy@example.com"); err != nil {
		t.Fatal(err)
	}
	reset := application.ResetCommand{Email: "cy@example.com", Code: mail.code, NewPassword: "newsecret9"}
	if err := svc.ResetPassword(ctx, reset); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Refresh(ctx, res.Tokens.Refresh, ""); err == nil {
		t.Fatal("reset must end existing sessions")
	}
	if _, err := svc.Login(ctx, application.LoginCommand{Email: "cy@example.com", Password: "newsecret9"}); err != nil {
		t.Fatal(err)
	}
}

func TestGoogleLoginCreatesAndLinks(t *testing.T) {
	ctx, now := context.Background(), time.Now()
	g := fakeGoogle{domain.GoogleProfile{Sub: "g1", Email: "di@example.com", Name: "Di", EmailVerified: true}}
	svc, _ := newService(t, &now, g)
	first, err := svc.LoginWithGoogle(ctx, application.GoogleCommand{AccessToken: "x"})
	if err != nil || first.User.Name != "Di" {
		t.Fatalf("%+v %v", first, err)
	}
	again, err := svc.LoginWithGoogle(ctx, application.GoogleCommand{AccessToken: "x"})
	if err != nil || again.User.ID != first.User.ID {
		t.Fatal("same Google account must map to the same user")
	}
	disabled, _ := newService(t, &now, nil)
	if _, err := disabled.LoginWithGoogle(ctx, application.GoogleCommand{AccessToken: "x"}); !errors.Is(err, domain.ErrGoogleDisabled) {
		t.Fatalf("got %v", err)
	}
}

func TestDeleteAccountNeedsThePassword(t *testing.T) {
	ctx, now := context.Background(), time.Now()
	svc, mail := newService(t, &now, nil)
	_, _ = svc.Register(ctx, application.RegisterCommand{Name: "Di", Email: "di@example.com", Password: "secret123"})
	res, _ := svc.VerifyEmail(ctx, application.VerifyCommand{Email: "di@example.com", Code: mail.code})

	if err := svc.DeleteAccount(ctx, res.User.ID, "wrong-pass1"); code(t, err) != apperr.Unauthorized {
		t.Fatal("a wrong password must not delete the account")
	}
	if _, err := svc.Me(ctx, res.User.ID); err != nil {
		t.Fatal("the account must survive a refused deletion")
	}
	if err := svc.DeleteAccount(ctx, res.User.ID, "secret123"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Me(ctx, res.User.ID); err == nil {
		t.Fatal("the account must be gone")
	}
	if _, err := svc.Refresh(ctx, res.Tokens.Refresh, ""); err == nil {
		t.Fatal("sessions must end with the account")
	}
}
