package aiclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"shenzen/backend/internal/platform/aiclient"
	"shenzen/backend/internal/shared/apperr"
)

func serve(status int, body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Internal-Api-Key") != "k" {
			status, body = 401, `{"success":false,"error":{"code":"UNAUTHORIZED","message":"bad key","details":[]}}`
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func TestDoDecodesData(t *testing.T) {
	srv := serve(200, `{"success":true,"message":"ok","data":{"n":3}}`)
	defer srv.Close()
	var out struct{ N int }
	if err := aiclient.New(srv.URL, "k", time.Second).Do(context.Background(), "GET", "/x", nil, &out); err != nil || out.N != 3 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func TestClientFaultKeepsCode(t *testing.T) {
	srv := serve(422, `{"success":false,"error":{"code":"VALIDATION_FAILED","message":"bad","details":[{"field":"a","message":"required"}]}}`)
	defer srv.Close()
	err := aiclient.New(srv.URL, "k", time.Second).Do(context.Background(), "POST", "/x", map[string]int{}, nil)
	if ae, ok := apperr.As(err); !ok || ae.Code != apperr.ValidationFailed || len(ae.Details) != 1 {
		t.Fatalf("got %v", err)
	}
}

func TestServerFaultBecomesUpstreamFailed(t *testing.T) {
	srv := serve(500, `{"success":false,"error":{"code":"INTERNAL","message":"boom","details":[]}}`)
	defer srv.Close()
	err := aiclient.New(srv.URL, "k", time.Second).Do(context.Background(), "GET", "/x", nil, nil)
	if ae, ok := apperr.As(err); !ok || ae.Code != apperr.UpstreamFailed {
		t.Fatalf("got %v", err)
	}
}

func TestUnreachable(t *testing.T) {
	err := aiclient.New("http://127.0.0.1:1", "k", time.Second).Do(context.Background(), "GET", "/x", nil, nil)
	if ae, ok := apperr.As(err); !ok || ae.Code != apperr.UpstreamFailed {
		t.Fatalf("got %v", err)
	}
}
