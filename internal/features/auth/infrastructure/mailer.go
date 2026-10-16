package infrastructure

import (
	"context"
	"fmt"
	"log/slog"
	"net/smtp"
	"strings"

	"shenzen/backend/internal/features/auth/domain"
)

type SMTPConfig struct {
	Host, Port, User, Password, From string
}

type SMTPMailer struct{ cfg SMTPConfig }

func NewSMTPMailer(cfg SMTPConfig) SMTPMailer { return SMTPMailer{cfg: cfg} }

func subjectAndLead(p domain.Purpose) (string, string) {
	if p == domain.ResetPassword {
		return "Reset your password", "Use this code to reset your password"
	}
	return "Verify your email", "Use this code to verify your email"
}

func (m SMTPMailer) SendOTP(_ context.Context, to, name string, p domain.Purpose, code string) error {
	subject, lead := subjectAndLead(p)
	body := fmt.Sprintf("Hi %s,\r\n\r\n%s:\r\n\r\n    %s\r\n\r\nIt expires in %d minutes. If you did not request it, ignore this email.\r\n",
		sanitizeHeader(name), lead, code, int(domain.OTPTTL.Minutes()))
	msg := strings.Join([]string{
		"From: " + m.cfg.From, "To: " + to, "Subject: " + subject,
		"MIME-Version: 1.0", "Content-Type: text/plain; charset=UTF-8", "", body,
	}, "\r\n")
	var auth smtp.Auth
	if m.cfg.User != "" {
		auth = smtp.PlainAuth("", m.cfg.User, m.cfg.Password, m.cfg.Host)
	}
	return smtp.SendMail(m.cfg.Host+":"+m.cfg.Port, auth, m.cfg.From, []string{to}, []byte(msg))
}

func sanitizeHeader(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
}

// LogMailer prints the code instead of sending; development only.
type LogMailer struct{}

func (LogMailer) SendOTP(ctx context.Context, to, _ string, p domain.Purpose, code string) error {
	slog.InfoContext(ctx, "otp (dev mailer, SMTP not configured)", "to", to, "purpose", p, "code", code)
	return nil
}
