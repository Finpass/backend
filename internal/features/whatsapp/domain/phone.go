// Package domain holds the WhatsApp delivery rules: phone handling, slot templates, reply commands, scheduling. No I/O.
package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
)

var (
	ErrInvalidPhone = errors.New("whatsapp: invalid phone number")
	e164Re          = regexp.MustCompile(`^\+[1-9]\d{7,14}$`)
	idPhoneRe       = regexp.MustCompile(`^\+628\d{7,11}$`)
)

// NormalizePhone returns E.164. Indonesian local numbers (08..., 8..., 62...) become +62...; other countries need an explicit + or 00.
func NormalizePhone(raw string) (string, error) {
	var b strings.Builder
	for i, r := range strings.TrimSpace(raw) {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+' && i == 0:
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '(' || r == ')' || r == '.':
		default:
			return "", ErrInvalidPhone
		}
	}
	s := b.String()
	switch {
	case strings.HasPrefix(s, "+"):
	case strings.HasPrefix(s, "00"):
		s = "+" + s[2:]
	case strings.HasPrefix(s, "62"):
		s = "+" + s
	case strings.HasPrefix(s, "0"):
		s = "+62" + s[1:]
	case strings.HasPrefix(s, "8"):
		s = "+62" + s
	default:
		return "", ErrInvalidPhone
	}
	if strings.HasPrefix(s, "+620") { // +62 0812... typed with a stray trunk zero
		s = "+62" + s[4:]
	}
	if !e164Re.MatchString(s) || (strings.HasPrefix(s, "+62") && !idPhoneRe.MatchString(s)) {
		return "", ErrInvalidPhone
	}
	return s, nil
}

// MaskPhone keeps the country code and the last three digits; phone numbers are personal data and never logged in full.
func MaskPhone(p string) string {
	if len(p) < 8 {
		return "***"
	}
	return p[:3] + strings.Repeat("*", len(p)-6) + p[len(p)-3:]
}

const jidSuffix = "@s.whatsapp.net"

// JID is the recipient format GoWA expects: digits plus @s.whatsapp.net.
func JID(e164 string) string { return strings.TrimPrefix(e164, "+") + jidSuffix }

// PhoneFromJID returns E.164 for a user JID; group and LID JIDs return false.
func PhoneFromJID(jid string) (string, bool) {
	n, ok := strings.CutSuffix(jid, jidSuffix)
	if !ok {
		return "", false
	}
	if i := strings.Index(n, ":"); i > 0 { // device suffix 628...:12
		n = n[:i]
	}
	p := "+" + n
	return p, e164Re.MatchString(p)
}

// VerifySignature checks GoWA's X-Hub-Signature-256 ("sha256=<hex>") over the raw body in constant time.
func VerifySignature(secret string, body []byte, header string) bool {
	if secret == "" {
		return false
	}
	sig, ok := strings.CutPrefix(strings.TrimSpace(header), "sha256=")
	if !ok {
		return false
	}
	got, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return subtle.ConstantTimeCompare(got, m.Sum(nil)) == 1
}

// Sign produces the header value for a body (used by tests and local simulation).
func Sign(secret string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}
