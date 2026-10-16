package infrastructure

import (
	"context"
	"net/http"

	"shenzen/backend/internal/features/profile/domain"
	"shenzen/backend/internal/platform/aiclient"
)

type Explainer struct{ client *aiclient.Client }

func NewExplainer(c *aiclient.Client) Explainer { return Explainer{c} }

type resultBody struct {
	ID                   string `json:"id"`
	CreatedAt            string `json:"createdAt"`
	QuestionnaireVersion string `json:"questionnaireVersion"`
	RuleVersion          string `json:"ruleVersion"`
	domain.Output
}

type explainBody struct {
	Locale  string            `json:"locale"`
	Persona map[string]string `json:"persona"`
	Result  resultBody        `json:"result"`
	Answers domain.Answers    `json:"answers"`
}

func (e Explainer) Explain(ctx context.Context, req domain.ExplainRequest) (*domain.Explanation, error) {
	r := req.Result
	body := explainBody{
		Locale: req.Locale, Persona: map[string]string{"firstName": req.FirstName}, Answers: req.Answers,
		Result: resultBody{ID: r.ID, CreatedAt: r.CreatedAt, QuestionnaireVersion: r.QuestionnaireVersion, RuleVersion: r.RuleVersion, Output: r.Output},
	}
	var out domain.Explanation
	if err := e.client.Do(ctx, http.MethodPost, "/ml/profile/explain", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
