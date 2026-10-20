package infrastructure

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"

	"shenzen/backend/internal/features/auth/domain"
)

func TestOTPPageEscapesName(t *testing.T) {
	var b bytes.Buffer
	if err := otpPage.Execute(&b, map[string]any{"Heading": "H", "Name": "<script>x</script>", "Lead": "l", "Code": "123456", "Minutes": 10}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if strings.Contains(out, "<script>") || !strings.Contains(out, "123456") {
		t.Fatalf("unexpected page: %s", out)
	}
}

// a server that never offers STARTTLS: the message must not be sent in plain text when TLS is required
func TestSMTPRefusesPlainTextWhenTLSIsRequired(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan string, 16)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		fmt.Fprint(conn, "220 test ready\r\n")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			got <- strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "EHLO"):
				fmt.Fprint(conn, "250 test\r\n")
			case strings.HasPrefix(line, "QUIT"):
				fmt.Fprint(conn, "221 bye\r\n")
				return
			default:
				fmt.Fprint(conn, "250 ok\r\n")
			}
		}
	}()
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	m := NewSMTPMailer(SMTPConfig{Host: host, Port: port, From: "a@x.co", RequireTLS: true})
	err = m.SendOTP(context.Background(), "b@x.co", "B", domain.VerifyEmail, "123456")
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("want a STARTTLS refusal, got %v", err)
	}
	close(got)
	for line := range got {
		if strings.HasPrefix(line, "MAIL") || strings.HasPrefix(line, "RCPT") || strings.HasPrefix(line, "DATA") {
			t.Fatalf("nothing may be sent before TLS: %q", line)
		}
	}
}
