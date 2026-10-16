package infrastructure

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"shenzen/backend/internal/features/auth/domain"
)

type GoogleVerifier struct {
	clientID string
	client   *http.Client
}

func NewGoogleVerifier(clientID string) GoogleVerifier {
	return GoogleVerifier{clientID: clientID, client: &http.Client{Timeout: 8 * time.Second}}
}

type tokenInfo struct {
	Aud           string `json:"aud"`
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified string `json:"email_verified"`
}

type userInfo struct {
	Name string `json:"name"`
}

// Verify checks that the access token was issued to our client, then reads the profile.
func (g GoogleVerifier) Verify(ctx context.Context, accessToken string) (domain.GoogleProfile, error) {
	var ti tokenInfo
	if err := g.get(ctx, "https://oauth2.googleapis.com/tokeninfo?access_token="+url.QueryEscape(accessToken), "", &ti); err != nil {
		return domain.GoogleProfile{}, domain.ErrGoogleInvalid
	}
	if ti.Aud != g.clientID || ti.Sub == "" || ti.Email == "" || ti.EmailVerified != "true" {
		return domain.GoogleProfile{}, domain.ErrGoogleInvalid
	}
	var ui userInfo
	_ = g.get(ctx, "https://www.googleapis.com/oauth2/v3/userinfo", accessToken, &ui) // name is optional
	return domain.GoogleProfile{Sub: ti.Sub, Email: ti.Email, Name: ui.Name, EmailVerified: true}, nil
}

func (g GoogleVerifier) get(ctx context.Context, u, bearer string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	res, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return domain.ErrGoogleInvalid
	}
	return json.NewDecoder(res.Body).Decode(out)
}
