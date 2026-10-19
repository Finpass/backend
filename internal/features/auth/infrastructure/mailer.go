package infrastructure

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
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

// otpPage is table-based with inline styles: mail clients ignore most modern CSS.
var otpPage = template.Must(template.New("otp").Parse(`<!doctype html>
<html><body style="margin:0;padding:0;background:#f3f4f9;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#f3f4f9;padding:32px 16px;">
<tr><td align="center">
<table role="presentation" width="480" cellpadding="0" cellspacing="0" style="max-width:480px;width:100%;background:#ffffff;border-radius:16px;overflow:hidden;font-family:-apple-system,'Segoe UI',Helvetica,Arial,sans-serif;">
<tr><td style="background:#0b0f1c;padding:24px 32px;font-size:22px;font-weight:700;letter-spacing:.3px;color:#8ea2ff;">Finpass</td></tr>
<tr><td style="padding:32px;color:#1c2033;">
<p style="margin:0 0 8px;font-size:20px;font-weight:700;">{{.Heading}}</p>
<p style="margin:0 0 24px;font-size:15px;line-height:1.6;color:#4a5068;">Hi {{.Name}}, {{.Lead}}.</p>
<div style="background:#f3f4f9;border:1px solid #dde1f0;border-radius:12px;padding:20px;text-align:center;font-family:'SF Mono',Menlo,Consolas,monospace;font-size:34px;font-weight:700;letter-spacing:10px;color:#1c2033;">{{.Code}}</div>
<p style="margin:24px 0 0;font-size:13px;line-height:1.6;color:#6b7188;">This code expires in {{.Minutes}} minutes. Never share it with anyone, including Finpass staff.</p>
<p style="margin:12px 0 0;font-size:13px;line-height:1.6;color:#6b7188;">If you did not request this, you can safely ignore this email.</p>
</td></tr>
<tr><td style="padding:16px 32px;background:#f8f9fc;border-top:1px solid #e8eaf3;font-size:12px;color:#8a90a6;">Finpass. Your money, explained.</td></tr>
</table>
</td></tr></table>
</body></html>`))

func (m SMTPMailer) SendOTP(_ context.Context, to, name string, p domain.Purpose, code string) error {
	subject, lead := subjectAndLead(p)
	minutes := int(domain.OTPTTL.Minutes())
	var page bytes.Buffer
	err := otpPage.Execute(&page, map[string]any{"Heading": subject, "Name": name, "Lead": strings.ToLower(lead[:1]) + lead[1:], "Code": code, "Minutes": minutes})
	if err != nil {
		return err
	}
	text := fmt.Sprintf("Hi %s,\r\n\r\n%s:\r\n\r\n    %s\r\n\r\nIt expires in %d minutes. If you did not request it, ignore this email.\r\n",
		sanitizeHeader(name), lead, code, minutes)
	const boundary = "finpass-otp-alt"
	msg := strings.Join([]string{
		"From: " + m.cfg.From, "To: " + to, "Subject: " + subject,
		"MIME-Version: 1.0", `Content-Type: multipart/alternative; boundary="` + boundary + `"`, "",
		"--" + boundary, "Content-Type: text/plain; charset=UTF-8", "", text,
		"--" + boundary, "Content-Type: text/html; charset=UTF-8", "", page.String(),
		"--" + boundary + "--", "",
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
