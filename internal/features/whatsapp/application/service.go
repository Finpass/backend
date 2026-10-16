package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"shenzen/backend/internal/features/whatsapp/domain"
	"shenzen/backend/internal/shared/apperr"
)

// Policy holds the engagement rules the YAML describes but the product cannot measure yet (no open/read signal from GoWA).
// Zero disables a rule.
type Policy struct {
	ReengageAfterDays  int // YAML: 7 days without opening
	AutoPauseAfterDays int // YAML: 30 days without opening
}

type Deps struct {
	Store   domain.Store
	Matcher domain.Matcher
	Sender  domain.Sender
	Names   domain.Names
	Link    string // dashboard base URL, no trailing slash
	Secret  string // webhook secret
	NewID   func() string
	Now     func() time.Time
	Policy  Policy
}

type Service struct{ d Deps }

func NewService(d Deps) *Service { return &Service{d} }

func (s *Service) contact(ctx context.Context, userID string) (domain.Contact, error) {
	c, err := s.d.Store.Contact(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.NewContact(userID), nil
	}
	return c, err
}

type Settings struct {
	Phone         *string    `json:"phone"`
	Verified      bool       `json:"verified"`
	OptIn         bool       `json:"optIn"`
	SendTimeLocal string     `json:"sendTimeLocal"`
	Timezone      string     `json:"timezone"`
	LastSentAt    *time.Time `json:"lastSentAt"`
	Frequency     string     `json:"frequency"`
	Paused        bool       `json:"paused"`
	DeliveryIssue bool       `json:"deliveryIssue"`
}

func view(c domain.Contact) Settings {
	s := Settings{Verified: c.Verified, OptIn: c.OptIn, SendTimeLocal: c.SendTime, Timezone: c.Timezone, LastSentAt: c.LastSentAt, Frequency: c.Frequency,
		Paused: c.PausedAt != nil, DeliveryIssue: c.FailStreak >= 3}
	if c.Phone != "" {
		p := c.Phone
		s.Phone = &p
	}
	return s
}

func (s *Service) Settings(ctx context.Context, userID string) (Settings, error) {
	c, err := s.contact(ctx, userID)
	return view(c), err
}

type SettingsUpdate struct {
	Phone         string
	OptIn         bool
	SendTimeLocal string
	Frequency     string
	Timezone      string
	Locale        string
}

func (s *Service) UpdateSettings(ctx context.Context, userID string, u SettingsUpdate) (Settings, error) {
	c, err := s.contact(ctx, userID)
	if err != nil {
		return Settings{}, err
	}
	var bad []apperr.Detail
	if strings.TrimSpace(u.Phone) == "" {
		if u.OptIn {
			bad = append(bad, apperr.Detail{Field: "phone", Message: "required_for_opt_in"})
		}
		c.Phone, c.Verified, c.VerifiedAt = "", false, nil
		_ = s.d.Store.DeleteVerification(ctx, userID)
	} else {
		p, err := domain.NormalizePhone(u.Phone)
		if err != nil {
			bad = append(bad, apperr.Detail{Field: "phone", Message: "invalid_phone"})
		} else if p != c.Phone {
			c.Phone, c.Verified, c.VerifiedAt = p, false, nil // a new number must be verified again
			_ = s.d.Store.DeleteVerification(ctx, userID)
		}
	}
	if u.SendTimeLocal != "" {
		if !domain.ValidSendTime(u.SendTimeLocal) {
			bad = append(bad, apperr.Detail{Field: "sendTimeLocal", Message: "must_be_HH:MM"})
		} else {
			c.SendTime = u.SendTimeLocal
		}
	}
	if u.Frequency != "" {
		if !domain.ValidFrequency(u.Frequency) {
			bad = append(bad, apperr.Detail{Field: "frequency", Message: "must_be_daily_weekdays_weekly"})
		} else {
			c.Frequency = u.Frequency
		}
	}
	if u.Timezone != "" {
		if _, err := domain.LoadTimezone(u.Timezone); err != nil {
			bad = append(bad, apperr.Detail{Field: "timezone", Message: "unknown_timezone"})
		} else {
			c.Timezone = u.Timezone
		}
	}
	if len(bad) > 0 {
		return Settings{}, apperr.Validation(bad...)
	}
	if domain.ValidLocale(u.Locale) {
		c.Locale = u.Locale
	}
	was := c.OptIn
	c.OptIn = u.OptIn
	if u.OptIn {
		c.PausedAt = nil
	}
	if err := s.d.Store.SaveContact(ctx, c); err != nil {
		if errors.Is(err, domain.ErrPhoneUsed) {
			return Settings{}, apperr.New(apperr.Conflict, "This number is already verified by another account")
		}
		return Settings{}, err
	}
	if was != c.OptIn {
		kind := "opt_out"
		if c.OptIn {
			kind = "opt_in"
		}
		s.event(ctx, userID, kind, "", map[string]any{"via": "settings"})
	}
	return view(c), nil
}

func hashCode(userID, code string) string {
	sum := sha256.Sum256([]byte(userID + "|whatsapp_verify|" + code))
	return hex.EncodeToString(sum[:])
}

func newCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func (s *Service) requireGateway(ctx context.Context) error {
	ok, err := s.d.Sender.Connected(ctx)
	if err != nil {
		return apperr.Wrap(apperr.Unavailable, "WhatsApp gateway is unreachable", err)
	}
	if !ok {
		return apperr.New(apperr.Unavailable, "WhatsApp gateway is not logged in")
	}
	return nil
}

func (s *Service) VerifyStart(ctx context.Context, userID string) error {
	c, err := s.contact(ctx, userID)
	if err != nil {
		return err
	}
	if c.Phone == "" {
		return apperr.Validation(apperr.Detail{Field: "phone", Message: "save_a_phone_first"})
	}
	now := s.d.Now()
	if v, err := s.d.Store.Verification(ctx, userID); err == nil && v.Phone == c.Phone && now.Before(v.CreatedAt.Add(domain.VerifyCooldown)) {
		return apperr.New(apperr.TooManyRequests, "Wait a minute before requesting another code")
	}
	if err := s.requireGateway(ctx); err != nil {
		return err
	}
	code, err := newCode()
	if err != nil {
		return err
	}
	if err := s.d.Store.SaveVerification(ctx, domain.Verification{UserID: userID, Phone: c.Phone, CodeHash: hashCode(userID, code), ExpiresAt: now.Add(domain.VerifyTTL), CreatedAt: now}); err != nil {
		return err
	}
	text := fmt.Sprintf("Kode verifikasi Finpass Anda: %s. Berlaku 10 menit. Jangan bagikan kode ini kepada siapa pun.", code)
	if c.Locale == "en" {
		text = fmt.Sprintf("Your Finpass verification code: %s. Valid for 10 minutes. Do not share this code with anyone.", code)
	}
	id, err := s.d.Sender.Send(ctx, c.Phone, text)
	s.logMsg(ctx, userID, "out", "verification", "", id, err) // the body is never stored: it holds the code
	if err != nil {
		_ = s.d.Store.DeleteVerification(ctx, userID)
		return apperr.Wrap(apperr.UpstreamFailed, "Could not send the verification code", err)
	}
	return nil
}

func (s *Service) VerifyConfirm(ctx context.Context, userID, code string) (Settings, error) {
	invalid := apperr.Validation(apperr.Detail{Field: "code", Message: "invalid_or_expired"})
	c, err := s.contact(ctx, userID)
	if err != nil {
		return Settings{}, err
	}
	v, err := s.d.Store.Verification(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) || (err == nil && v.Phone != c.Phone) {
		return Settings{}, invalid
	}
	if err != nil {
		return Settings{}, err
	}
	if !s.d.Now().Before(v.ExpiresAt) {
		return Settings{}, invalid
	}
	if v.Attempts >= domain.VerifyMaxAttempts {
		return Settings{}, apperr.New(apperr.TooManyRequests, "Too many attempts, request a new code")
	}
	if subtle.ConstantTimeCompare([]byte(v.CodeHash), []byte(hashCode(userID, strings.TrimSpace(code)))) != 1 {
		if err := s.d.Store.AddVerificationAttempt(ctx, userID); err != nil {
			return Settings{}, err
		}
		return Settings{}, invalid
	}
	now := s.d.Now()
	c.Verified, c.VerifiedAt = true, &now
	if err := s.d.Store.SaveContact(ctx, c); err != nil {
		if errors.Is(err, domain.ErrPhoneUsed) {
			return Settings{}, apperr.New(apperr.Conflict, "This number is already verified by another account")
		}
		return Settings{}, err
	}
	_ = s.d.Store.DeleteVerification(ctx, userID)
	return view(c), nil
}

func (s *Service) options(c domain.Contact, locale string, now time.Time) domain.Options {
	if !domain.ValidLocale(locale) {
		locale = c.Locale
	}
	return domain.Options{Locale: locale, Link: s.d.Link + "/dashboard", Now: now.In(c.Location())}
}

func (s *Service) content(ctx context.Context, userID string, c domain.Contact) (domain.Content, map[string]string, error) {
	local := s.d.Now().In(c.Location())
	content, keys, err := s.d.Matcher.Content(ctx, userID, local)
	if err != nil {
		return content, keys, err
	}
	if n, err := s.d.Names.FirstName(ctx, userID); err == nil {
		content.Name = n
	}
	if n, err := s.d.Store.SentCount(ctx, userID); err == nil && n == 0 {
		content.NewUser = true
	}
	return content, keys, nil
}

type Preview struct {
	domain.Briefing
	Trace map[string]any `json:"trace"`
}

// Preview renders exactly what would be sent today. It has no side effects.
func (s *Service) Preview(ctx context.Context, userID, locale string) (Preview, error) {
	b, keys, _, err := s.compose(ctx, userID, locale)
	if err != nil {
		return Preview{}, err
	}
	return Preview{Briefing: b, Trace: map[string]any{"selection": keys, "fallback": b.Fallback, "dropped": b.Dropped}}, nil
}

func (s *Service) compose(ctx context.Context, userID, locale string) (domain.Briefing, map[string]string, domain.Contact, error) {
	c, err := s.contact(ctx, userID)
	if err != nil {
		return domain.Briefing{}, nil, c, err
	}
	content, keys, err := s.content(ctx, userID, c)
	if err != nil {
		if errors.Is(err, domain.ErrNoProfile) {
			return domain.Briefing{}, nil, c, apperr.New(apperr.NotFound, "Complete the financial profile first")
		}
		return domain.Briefing{}, nil, c, err
	}
	b := domain.Compose(content, s.options(c, locale, s.d.Now()))
	return b, keys, c, nil
}

func (s *Service) SendTest(ctx context.Context, userID, locale string) (Preview, error) {
	b, keys, c, err := s.compose(ctx, userID, locale)
	if err != nil {
		return Preview{}, err
	}
	if !c.Verified || c.Phone == "" {
		return Preview{}, apperr.New(apperr.Conflict, "Verify your WhatsApp number first")
	}
	if err := s.requireGateway(ctx); err != nil {
		return Preview{}, err
	}
	id, err := s.d.Sender.Send(ctx, c.Phone, b.Text)
	s.logMsg(ctx, userID, "out", "test", b.Text, id, err)
	if err != nil {
		return Preview{}, apperr.Wrap(apperr.UpstreamFailed, "Could not send the message", err)
	}
	return Preview{Briefing: b, Trace: map[string]any{"selection": keys, "fallback": b.Fallback, "dropped": b.Dropped, "providerMessageId": id}}, nil
}

func (s *Service) logMsg(ctx context.Context, userID, dir, kind, body, providerID string, sendErr error) {
	m := domain.Message{ID: s.d.NewID(), UserID: userID, Direction: dir, Kind: kind, Body: body, Status: "sent", ProviderID: providerID}
	if sendErr != nil {
		m.Status, m.Error = "failed", clipErr(sendErr)
	}
	if err := s.d.Store.LogMessage(ctx, m); err != nil {
		slog.WarnContext(ctx, "whatsapp: log message failed", "err", err)
	}
}

func clipErr(err error) string {
	s := err.Error()
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

func (s *Service) event(ctx context.Context, userID, kind, slot string, meta map[string]any) {
	if err := s.d.Store.LogEvent(ctx, domain.Event{UserID: userID, Kind: kind, Slot: slot, Meta: meta}); err != nil {
		slog.WarnContext(ctx, "whatsapp: log event failed", "kind", kind, "err", err)
	}
}

// Erase removes every WhatsApp record of the user (UU PDP deletion).
func (s *Service) Erase(ctx context.Context, userID string) error {
	return s.d.Store.Erase(ctx, userID)
}

// ---- webhook ----

type inbound struct {
	Event    string `json:"event"`
	DeviceID string `json:"device_id"`
	Payload  struct {
		ID       string `json:"id"`
		ChatID   string `json:"chat_id"`
		From     string `json:"from"`
		IsFromMe bool   `json:"is_from_me"`
		Body     string `json:"body"`
	} `json:"payload"`
}

var ErrBadSignature = apperr.New(apperr.Unauthorized, "Invalid webhook signature")

// HandleWebhook verifies the HMAC, then runs the reply command. Messages that are not 1:1 text from a verified user are ignored (nil error).
func (s *Service) HandleWebhook(ctx context.Context, body []byte, signature string) error {
	if !domain.VerifySignature(s.d.Secret, body, signature) {
		return ErrBadSignature
	}
	var in inbound
	if err := json.Unmarshal(body, &in); err != nil {
		return apperr.Wrap(apperr.BadRequest, "Invalid webhook body", err)
	}
	if in.Event != "message" || in.Payload.IsFromMe || strings.HasSuffix(in.Payload.ChatID, "@g.us") || strings.TrimSpace(in.Payload.Body) == "" {
		return nil
	}
	phone, ok := domain.PhoneFromJID(in.Payload.From)
	if !ok {
		return nil
	}
	c, err := s.d.Store.ContactByPhone(ctx, phone)
	if errors.Is(err, domain.ErrNotFound) {
		slog.Info("whatsapp: message from unknown number ignored", "phone", domain.MaskPhone(phone))
		return nil
	}
	if err != nil {
		return err
	}
	text := in.Payload.Body
	if len(text) > 500 {
		text = text[:500]
	}
	if err := s.d.Store.LogMessage(ctx, domain.Message{ID: "in:" + in.Payload.ID, UserID: c.UserID, Direction: "in", Kind: "reply", Body: text, Status: "received", ProviderID: in.Payload.ID}); err != nil {
		if errors.Is(err, domain.ErrDuplicate) { // GoWA retries deliveries: handle each message once
			return nil
		}
		return err
	}
	now := s.d.Now()
	_ = s.d.Store.TouchInbound(ctx, c.UserID, now)
	cmd := domain.ParseCommand(text)
	s.event(ctx, c.UserID, "reply", "", map[string]any{"command": string(cmd)})

	switch cmd {
	case domain.CmdStop:
		c.OptIn, c.PausedAt = false, &now
		if err := s.d.Store.SaveContact(ctx, c); err != nil {
			return err
		}
		s.event(ctx, c.UserID, "opt_out", "", map[string]any{"via": "STOP"})
	case domain.CmdResume:
		c.OptIn, c.PausedAt = true, nil
		if err := s.d.Store.SaveContact(ctx, c); err != nil {
			return err
		}
		s.event(ctx, c.UserID, "opt_in", "", map[string]any{"via": "RESUME"})
	}

	rc := domain.ReplyContext{Locale: c.Locale, Link: s.d.Link + "/dashboard"}
	switch cmd {
	case domain.CmdMore, domain.CmdProfile:
		content, _, err := s.content(ctx, c.UserID, c)
		if err != nil && !errors.Is(err, domain.ErrNoProfile) {
			slog.Warn("whatsapp: reply content failed", "err", err)
		}
		rc.Content = content
	case domain.CmdUpdate:
		rc.Link = s.d.Link + "/onboarding"
	}
	reply := domain.Reply(cmd, rc)
	id, sendErr := s.d.Sender.Send(ctx, c.Phone, reply)
	s.logMsg(ctx, c.UserID, "out", "command", reply, id, sendErr)
	if sendErr != nil {
		slog.Warn("whatsapp: reply not delivered", "phone", domain.MaskPhone(c.Phone), "err", sendErr)
	}
	return nil
}
