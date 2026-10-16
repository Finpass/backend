package transport

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"shenzen/backend/internal/platform/httpx"
)

func TestLocale(t *testing.T) {
	cases := map[string]string{"": "id", "id-ID,id;q=0.9": "id", "en": "en", "en-US,en;q=0.9": "en", "EN-gb": "en", "fr": "id", "id,en;q=0.5": "id", "english": "id"}
	for header, want := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if header != "" {
			r.Header.Set("Accept-Language", header)
		}
		if got := locale(r); got != want {
			t.Errorf("%q -> %s, want %s", header, got, want)
		}
	}
}

func TestConsentMustBeAccepted(t *testing.T) {
	for body, wantErr := range map[string]bool{
		`{"accepted":true,"whatsappOptIn":true}`: false,
		`{"accepted":true}`:                      false,
		`{"accepted":false}`:                     true,
		`{}`:                                     true,
		`{"accepted":true,"extra":1}`:            true,
	} {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		if _, err := httpx.Decode[consentRequest](httptest.NewRecorder(), r); (err != nil) != wantErr {
			t.Errorf("%s: err=%v", body, err)
		}
	}
}
