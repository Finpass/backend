package application

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"shenzen/backend/internal/features/whatsapp/domain"
)

// Tick processes every contact that is due now. It is idempotent: briefing_runs has one row per user per local date and a
// row is claimed with a single INSERT ... ON CONFLICT, so two ticks or two instances cannot send the same briefing twice.
func (s *Service) Tick(ctx context.Context) (sent, failed int) {
	contacts, err := s.d.Store.ListActive(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "whatsapp: list contacts failed", "err", err)
		return 0, 0
	}
	now := s.d.Now()
	for _, c := range contacts {
		if ctx.Err() != nil {
			return
		}
		date, due := domain.Due(c, now)
		if !due {
			continue
		}
		attempt, ok, err := s.d.Store.ClaimRun(ctx, c.UserID, date, now)
		if err != nil {
			slog.ErrorContext(ctx, "whatsapp: claim run failed", "err", err)
			continue
		}
		if !ok {
			continue
		}
		if s.deliver(ctx, c, date, attempt) {
			sent++
		} else {
			failed++
		}
	}
	return
}

func (s *Service) deliver(ctx context.Context, c domain.Contact, date string, attempt int) bool {
	now := s.d.Now()
	finish := func(r domain.RunResult) {
		if err := s.d.Store.FinishRun(ctx, c.UserID, date, r); err != nil {
			slog.ErrorContext(ctx, "whatsapp: finish run failed", "err", err)
		}
	}
	content, keys, err := s.content(ctx, c.UserID, c)
	if err != nil {
		finish(domain.RunResult{Status: "failed", Error: clipErr(err)})
		if !errors.Is(err, domain.ErrNoProfile) {
			slog.ErrorContext(ctx, "whatsapp: compose failed", "err", err)
		}
		return false
	}
	text, fallback, slots, chars := "", "", []domain.Slot(nil), 0
	if s.d.Policy.AutoPauseAfterDays > 0 && s.inactiveDays(c, now) >= s.d.Policy.AutoPauseAfterDays {
		c.OptIn, c.PausedAt = false, &now
		_ = s.d.Store.SaveContact(ctx, c)
		s.event(ctx, c.UserID, "opt_out", "", map[string]any{"via": "auto_pause_inactive"})
		finish(domain.RunResult{Status: "failed", Error: "auto-paused: inactive"})
		return false
	}
	if d := s.inactiveDays(c, now); s.d.Policy.ReengageAfterDays > 0 && d >= s.d.Policy.ReengageAfterDays && d%s.d.Policy.ReengageAfterDays == 0 {
		text, fallback = domain.ComposeReengage(content.Name, d, c.Locale), "user_inactive"
		chars = len([]rune(text))
	} else {
		b := domain.Compose(content, s.options(c, "", now))
		text, fallback, slots, chars = b.Text, b.Fallback, b.Slots, b.CharCount
	}
	id, sendErr := s.d.Sender.Send(ctx, c.Phone, text)
	s.logMsg(ctx, c.UserID, "out", "briefing", text, id, sendErr)
	if sendErr != nil {
		slog.WarnContext(ctx, "whatsapp: briefing delivery failed", "phone", domain.MaskPhone(c.Phone), "attempt", attempt, "err", sendErr)
		s.event(ctx, c.UserID, "briefing_failed", "", map[string]any{"attempt": attempt})
		if attempt < domain.MaxRetries {
			next := now.Add(domain.RetryInterval)
			finish(domain.RunResult{Status: "retry", Error: clipErr(sendErr), NextAttempt: &next})
			return false
		}
		finish(domain.RunResult{Status: "failed", Error: clipErr(sendErr)})
		streak := c.FailStreak + 1
		_ = s.d.Store.SetSent(ctx, c.UserID, time.Time{}, streak)
		if streak == 3 { // three days in a row: tell the web app (Settings shows deliveryIssue)
			s.event(ctx, c.UserID, "delivery_issue", "", map[string]any{"days": streak})
		}
		return false
	}
	finish(domain.RunResult{Status: "sent", Fallback: fallback, Keys: keys, CharCount: chars})
	_ = s.d.Store.SetSent(ctx, c.UserID, now, 0)
	s.event(ctx, c.UserID, "briefing_sent", "", map[string]any{"chars": chars, "fallback": fallback})
	for _, sl := range slots {
		s.event(ctx, c.UserID, "slot_included", sl.Key, nil)
	}
	return true
}

// inactiveDays counts days since the last inbound reply (or verification). GoWA gives no read receipt in this integration,
// so this is an approximation of "has not opened"; both rules default to off.
func (s *Service) inactiveDays(c domain.Contact, now time.Time) int {
	ref := c.LastInboundAt
	if ref == nil {
		ref = c.VerifiedAt
	}
	if ref == nil {
		return 0
	}
	return int(now.Sub(*ref).Hours() / 24)
}

// RunScheduler ticks every interval until ctx ends.
func (s *Service) RunScheduler(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	slog.Info("whatsapp scheduler started", "interval", every.String())
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if sent, failed := s.Tick(ctx); sent+failed > 0 {
				slog.Info("whatsapp tick", "sent", sent, "failed", failed)
			}
		}
	}
}
