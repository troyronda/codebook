package codebook

// This is the only file that knows which System One client codebook uses. To
// switch to another client, replace this file: everything else talks to the
// asker interface.

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/troyronda/codebook/internal/systemone"
)

// wireQuestion is one question as sent to the model.
type wireQuestion struct {
	Name         string
	Type         string
	Instructions json.RawMessage
	Criteria     json.RawMessage
}

// reply is the model's answers, keyed by question name.
type reply struct {
	Model        string // the model name the response reports
	Answers      map[string]json.RawMessage
	InputTokens  int
	OutputTokens int
}

// asker asks every question about one state in one call.
type asker interface {
	ask(ctx context.Context, model string, state json.RawMessage, qs []wireQuestion) (*reply, error)
}

func newAsker(baseURL string, hc *http.Client, apiKey string) asker {
	return &systemOneAsker{c: &systemone.Client{BaseURL: baseURL, HTTP: hc, APIKey: apiKey}}
}

type systemOneAsker struct{ c *systemone.Client }

func (a *systemOneAsker) ask(ctx context.Context, model string, state json.RawMessage, qs []wireQuestion) (*reply, error) {
	req := &systemone.SystemOneRequest{Model: model, State: state, Questions: &systemone.SystemOneQuestions{}}
	for _, q := range qs {
		req.Questions.Set(q.Name, systemone.SystemOneQuestion{Type: q.Type, Instructions: q.Instructions, Criteria: q.Criteria})
	}
	resp, err := a.c.SystemOne(ctx, req)
	if err != nil {
		return nil, err
	}
	r := &reply{Model: resp.Model, Answers: map[string]json.RawMessage{},
		InputTokens: resp.Usage.InputTokens, OutputTokens: resp.Usage.OutputTokens}
	for k, v := range resp.Answers.All() {
		r.Answers[k] = v
	}
	return r, nil
}
