package infrastructure

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeGoWA mimics the parts of GoWA's REST API this client uses.
func fakeGoWA(t *testing.T, loggedIn bool, sendStatus int) (*httptest.Server, *[]map[string]string) {
	var got []map[string]string
	mux := http.NewServeMux()
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if u, p, ok := r.BasicAuth(); !ok || u != "admin" || p != "pw" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("POST /send/message", auth(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		_ = json.NewDecoder(r.Body).Decode(&b)
		got = append(got, b)
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("content type")
		}
		w.WriteHeader(sendStatus)
		if sendStatus != 200 {
			_, _ = w.Write([]byte(`{"code":"400","message":"phone is not registered 6281234567890"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":"SUCCESS","message":"Success","results":{"message_id":"3EB0ABC","status":"ok"}}`))
	}))
	mux.HandleFunc("GET /app/status", auth(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":200,"code":"SUCCESS","results":{"is_connected":true,"is_logged_in":` + map[bool]string{true: "true", false: "false"}[loggedIn] + `}}`))
	}))
	return httptest.NewServer(mux), &got
}

func TestGoWASend(t *testing.T) {
	srv, got := fakeGoWA(t, true, 200)
	defer srv.Close()
	g := NewGoWA(srv.URL+"/", "admin", "pw")
	id, err := g.Send(context.Background(), "+6281234567890", "halo")
	if err != nil || id != "3EB0ABC" {
		t.Fatalf("%q %v", id, err)
	}
	if len(*got) != 1 || (*got)[0]["phone"] != "6281234567890@s.whatsapp.net" || (*got)[0]["message"] != "halo" {
		t.Errorf("request body %v", *got)
	}
}

func TestGoWAErrorsAndAuth(t *testing.T) {
	srv, _ := fakeGoWA(t, true, 400)
	defer srv.Close()
	_, err := NewGoWA(srv.URL, "admin", "pw").Send(context.Background(), "+6281234567890", "halo")
	if err == nil || !strings.Contains(err.Error(), "status 400") {
		t.Fatalf("%v", err)
	}
	if _, err = NewGoWA(srv.URL, "admin", "wrong").Send(context.Background(), "+6281234567890", "x"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("wrong credentials: %v", err)
	}
	if _, err = NewGoWA("http://127.0.0.1:1", "", "").Send(context.Background(), "+6281234567890", "x"); err == nil || strings.Contains(err.Error(), "6281234567890") {
		t.Errorf("unreachable gateway error must not carry the phone: %v", err)
	}
}

func TestGoWAConnected(t *testing.T) {
	for _, logged := range []bool{true, false} {
		srv, _ := fakeGoWA(t, logged, 200)
		ok, err := NewGoWA(srv.URL, "admin", "pw").Connected(context.Background())
		srv.Close()
		if err != nil || ok != logged {
			t.Errorf("logged=%v got %v %v", logged, ok, err)
		}
	}
	if _, err := NewGoWA("http://127.0.0.1:1", "", "").Connected(context.Background()); err == nil {
		t.Error("unreachable")
	}
}
