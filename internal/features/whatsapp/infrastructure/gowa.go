package infrastructure

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"shenzen/backend/internal/features/whatsapp/domain"
)

// GoWA talks to github.com/aldinokemal/go-whatsapp-web-multidevice (REST): POST /send/message with
// {"phone": "<digits>@s.whatsapp.net", "message": "..."} and GET /app/status; HTTP basic auth when configured.
type GoWA struct {
	base, user, pass, device string
	http                     *http.Client
}

// GoWA v9 scopes every call to a device slot, sent as X-Device-Id (create it with POST /devices).
func NewGoWA(baseURL, user, pass, device string) *GoWA {
	return &GoWA{base: strings.TrimRight(baseURL, "/"), user: user, pass: pass, device: device, http: &http.Client{Timeout: 20 * time.Second}}
}

func (g *GoWA) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.base+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if g.user != "" {
		req.SetBasicAuth(g.user, g.pass)
	}
	if g.device != "" {
		req.Header.Set("X-Device-Id", g.device)
	}
	resp, err := g.http.Do(req)
	if err != nil {
		return fmt.Errorf("gowa unreachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var e struct {
			Code    any    `json:"code"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &e)
		return fmt.Errorf("gowa status %d: %s", resp.StatusCode, clip(e.Message, 200)) // never echo the request (phone, text)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("gowa returned unreadable body: %w", err)
		}
	}
	return nil
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func (g *GoWA) Send(ctx context.Context, e164, text string) (string, error) {
	var out struct {
		Results struct {
			MessageID string `json:"message_id"`
		} `json:"results"`
	}
	if err := g.do(ctx, http.MethodPost, "/send/message", map[string]string{"phone": domain.JID(e164), "message": text}, &out); err != nil {
		return "", err
	}
	return out.Results.MessageID, nil
}

func (g *GoWA) Connected(ctx context.Context) (bool, error) {
	var out struct {
		Results struct {
			Connected bool `json:"is_connected"`
			LoggedIn  bool `json:"is_logged_in"`
		} `json:"results"`
	}
	if err := g.do(ctx, http.MethodGet, "/app/status", nil, &out); err != nil {
		return false, err
	}
	return out.Results.Connected && out.Results.LoggedIn, nil
}
