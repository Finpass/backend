package aiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"shenzen/backend/internal/platform/httpx"
	"shenzen/backend/internal/shared/apperr"
)

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func New(baseURL, apiKey string, timeout time.Duration) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, http: &http.Client{Timeout: timeout}}
}

type envelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   *struct {
		Code    apperr.Code     `json:"code"`
		Message string          `json:"message"`
		Details []apperr.Detail `json:"details"`
	} `json:"error"`
}

func (c *Client) Do(ctx context.Context, method, path string, in, out any) error {
	var body bytes.Buffer
	if in != nil {
		if err := json.NewEncoder(&body).Encode(in); err != nil {
			return apperr.Wrap(apperr.Internal, "Encode AI request", err)
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, &body)
	if err != nil {
		return apperr.Wrap(apperr.Internal, "Build AI request", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Api-Key", c.apiKey)
	if id := httpx.RequestID(ctx); id != "" {
		req.Header.Set(httpx.RequestIDHeader, id)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return apperr.Wrap(apperr.UpstreamTimeout, "AI service timed out", err)
		}
		return apperr.Wrap(apperr.UpstreamFailed, "AI service unreachable", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var env envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return apperr.Wrap(apperr.UpstreamFailed, fmt.Sprintf("AI service returned status %d with unreadable body", resp.StatusCode), err)
	}
	if !env.Success || env.Error != nil {
		return fromUpstream(resp.StatusCode, env)
	}
	if out != nil {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return apperr.Wrap(apperr.UpstreamFailed, "AI service returned unexpected data", err)
		}
	}
	return nil
}

// Client-side faults pass through with their code; anything else is the AI service's problem, not the caller's.
func fromUpstream(status int, env envelope) error {
	if env.Error == nil {
		return apperr.New(apperr.UpstreamFailed, fmt.Sprintf("AI service failed with status %d", status))
	}
	if status >= 400 && status < 500 {
		return &apperr.Error{Code: env.Error.Code, Message: env.Error.Message, Details: env.Error.Details}
	}
	return apperr.New(apperr.UpstreamFailed, env.Error.Message)
}
