package infrastructure

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"shenzen/backend/internal/features/advisory/domain"
	"shenzen/backend/internal/platform/aiclient"
)

// AI calls POST /ml/rag/chat through the shared AI client (internal key header, request id, no retry).
type AI struct{ c *aiclient.Client }

func NewAI(c *aiclient.Client) AI { return AI{c} }

func (a AI) Chat(ctx context.Context, req domain.ChatRequest) (domain.Answer, error) {
	var out domain.Answer
	err := a.c.Do(ctx, http.MethodPost, "/ml/rag/chat", req, &out)
	return out, err
}

// Turns reads the progress steps the AI service has emitted so far for one run.
func (a AI) Turns(ctx context.Context, runID string, since int) ([]domain.TurnEvent, error) {
	var out []domain.TurnEvent
	err := a.c.Do(ctx, http.MethodGet, "/ml/rag/runs/"+url.PathEscape(runID)+"/turns?since="+strconv.Itoa(since), nil, &out)
	return out, err
}
