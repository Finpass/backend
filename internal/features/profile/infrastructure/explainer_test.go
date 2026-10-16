package infrastructure_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"shenzen/backend/internal/features/profile/domain"
	"shenzen/backend/internal/features/profile/infrastructure"
	"shenzen/backend/internal/platform/aiclient"
)

func TestExplainerSendsContractAndParsesEnvelope(t *testing.T) {
	var got map[string]any
	var key, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, path = r.Header.Get("X-Internal-Api-Key"), r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"success":true,"message":"ok","data":{"locale":"id","source":"llm","model":"m","summary":"s","risk":"r","ladder":"l","health":"h","nextStep":"n","disclaimer":"d"}}`))
	}))
	defer srv.Close()

	e := infrastructure.NewExplainer(aiclient.New(srv.URL, "k", time.Second))
	out, err := e.Explain(context.Background(), domain.ExplainRequest{
		Locale: "id", FirstName: "Dinda", Answers: domain.Answers{"q1": "25_30"},
		Result: domain.Result{ID: "r1", Output: domain.Output{FinalProfile: "Moderate"}},
	})
	if err != nil || out.Source != "llm" || out.Model == nil || *out.Model != "m" || out.NextStep != "n" {
		t.Fatalf("%+v %v", out, err)
	}
	if key != "k" || path != "/ml/profile/explain" {
		t.Errorf("key=%q path=%q", key, path)
	}
	res, _ := got["result"].(map[string]any)
	persona, _ := got["persona"].(map[string]any)
	if got["locale"] != "id" || persona["firstName"] != "Dinda" || res["finalProfile"] != "Moderate" || res["id"] != "r1" {
		t.Errorf("body %v", got)
	}
	if _, has := res["explanation"]; has {
		t.Error("result must be sent without explanation")
	}
}

func TestExplainerPropagatesFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"success":false,"error":{"code":"INTERNAL","message":"boom","details":[]}}`))
	}))
	defer srv.Close()
	e := infrastructure.NewExplainer(aiclient.New(srv.URL, "k", time.Second))
	if out, err := e.Explain(context.Background(), domain.ExplainRequest{}); err == nil || out != nil {
		t.Fatalf("%+v %v", out, err)
	}
}
