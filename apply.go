package codebook

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"unicode/utf8"

	"github.com/ollama/ollama/api"
)

// Document is one document to code. Path identifies it in results; Text is never
// copied into a result.
type Document struct {
	Path string
	Text string
}

// Result is a model's answers to every question for one document. It holds hashes and
// labels, never the document's text.
type Result struct {
	Path          string `json:"path"`
	DocHash       string `json:"doc_hash"`      // SHA-256 of the full text, in lowercase hex
	CodebookHash  string `json:"codebook_hash"` // see Codebook.Hash
	Model         string `json:"model"`
	Digest        string `json:"digest"`
	ServerVersion string `json:"server_version"`
	// Chars is how many characters were sent; Truncated is set when that was fewer than
	// the document's length.
	Chars        int      `json:"chars"`
	Truncated    bool     `json:"truncated"`
	InputTokens  int      `json:"input_tokens"`
	OutputTokens int      `json:"output_tokens"`
	Answers      []Answer `json:"answers"`
}

// Answer is one question's answer. Label, Probability and Confidence are filled for
// every type; exactly one of Noul, Choice and Score holds the typed answer.
type Answer struct {
	Question string `json:"question"`
	Type     string `json:"type"`
	Critical bool   `json:"critical,omitempty"`
	// Label is the most likely label: "true" or "false" for Noul, the option for Choice,
	// the most likely level for Score.
	Label string `json:"label"`
	// Probability is the probability of Label.
	Probability float64 `json:"probability"`
	// Confidence is how strongly the model favours one label, from 0 (uniform) to 1:
	// 1 - H(p)/ln(N), as the server computes it for choice and score. It is not
	// calibrated correctness; the agreement report measures that.
	Confidence float64 `json:"confidence"`
	// Route is where the gates send this answer.
	Route Route `json:"route"`

	Noul   *NoulAnswer   `json:"noul,omitempty"`
	Choice *ChoiceAnswer `json:"choice,omitempty"`
	Score  *ScoreAnswer  `json:"score,omitempty"`
}

// NoulAnswer is a yes/no answer.
type NoulAnswer struct {
	// True is the probability of "true", from 0 to 1.
	True float64 `json:"true"`
}

// ChoiceAnswer is a choice among named labels.
type ChoiceAnswer struct {
	Choice        string             `json:"choice"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    float64            `json:"confidence"`
}

// ScoreAnswer is a score over ordered levels.
type ScoreAnswer struct {
	// Score is the probability-weighted average of the zero-based level indices.
	Score float64 `json:"score"`
	// Probabilities are keyed by level name.
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    float64            `json:"confidence"`
}

// wire is the server's answer, before it is typed.
type wire struct {
	Type          string             `json:"type"`
	Noul          *float64           `json:"noul"`
	Choice        string             `json:"choice"`
	Score         *float64           `json:"score"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    *float64           `json:"confidence"`
}

// Apply asks every question about doc in one call and returns typed answers, each
// routed by its gate.
func (c *Codebook) Apply(ctx context.Context, doc Document) (*Result, error) {
	return c.apply(ctx, c.Model, doc)
}

func (c *Codebook) apply(ctx context.Context, m *Model, doc Document) (*Result, error) {
	if m == nil || m.client == nil {
		return nil, errors.New("codebook has no model; open one with OpenModel")
	}
	sum := sha256.Sum256([]byte(doc.Text))
	text, truncated := truncate(doc.Text, m.MaxChars)
	state, err := json.Marshal(text)
	if err != nil {
		return nil, err
	}
	resp, err := m.client.SystemOne(ctx, &api.SystemOneRequest{
		Model:     m.Name,
		State:     state,
		Questions: c.questions(),
	})
	if err != nil {
		// The server's error never includes the state, but the path is all we add.
		return nil, fmt.Errorf("%s: %w", doc.Path, err)
	}
	r := &Result{
		Path:          doc.Path,
		DocHash:       hex.EncodeToString(sum[:]),
		CodebookHash:  c.Hash,
		Model:         m.Name,
		Digest:        m.Digest,
		ServerVersion: m.ServerVersion,
		Chars:         utf8.RuneCountInString(text),
		Truncated:     truncated,
		InputTokens:   resp.Usage.InputTokens,
		OutputTokens:  resp.Usage.OutputTokens,
	}
	for _, q := range c.Questions {
		raw, ok := resp.Answers.Get(q.Name)
		if !ok {
			return nil, fmt.Errorf("%s: no answer to %q", doc.Path, q.Name)
		}
		a, err := decode(q, raw)
		if err != nil {
			return nil, fmt.Errorf("%s: question %q: %w", doc.Path, q.Name, err)
		}
		a.Route = c.gate(q).route(q, a)
		r.Answers = append(r.Answers, a)
	}
	return r, nil
}

// questions builds the request's questions in codebook order.
func (c *Codebook) questions() *api.SystemOneQuestions {
	qs := &api.SystemOneQuestions{}
	for _, q := range c.Questions {
		instr, _ := json.Marshal(q.Ask)
		var crit json.RawMessage
		if q.Type == Score {
			levels := make([]string, len(q.Labels))
			for i, l := range q.Labels {
				levels[i] = l.Name + ": " + l.Definition
			}
			crit, _ = json.Marshal(levels)
		} else {
			// An object in label order; encoding/json would sort a map's keys.
			buf := []byte{'{'}
			for i, l := range q.Labels {
				if i > 0 {
					buf = append(buf, ',')
				}
				k, _ := json.Marshal(l.Name)
				v, _ := json.Marshal(l.Definition)
				buf = append(append(append(buf, k...), ':'), v...)
			}
			crit = append(buf, '}')
		}
		qs.Set(q.Name, api.SystemOneQuestion{Type: q.Type, Instructions: instr, Criteria: crit})
	}
	return qs
}

func decode(q Question, raw json.RawMessage) (Answer, error) {
	var w wire
	if err := json.Unmarshal(raw, &w); err != nil {
		return Answer{}, err
	}
	if w.Type != q.Type {
		return Answer{}, fmt.Errorf("answer type %q, asked %q", w.Type, q.Type)
	}
	a := Answer{Question: q.Name, Type: q.Type, Critical: q.Critical}
	switch q.Type {
	case Noul:
		if w.Noul == nil || !isProb(*w.Noul) {
			return a, errors.New("noul probability missing or out of range")
		}
		p := *w.Noul
		a.Noul = &NoulAnswer{True: p}
		a.Label, a.Probability = "true", p
		if p < 0.5 {
			a.Label, a.Probability = "false", 1-p
		}
		a.Confidence = concentration([]float64{p, 1 - p})
	case Choice:
		if w.Confidence == nil || w.Probabilities == nil {
			return a, errors.New("choice answer incomplete")
		}
		if q.label(w.Choice) == nil {
			return a, fmt.Errorf("choice %q is not a label", w.Choice)
		}
		a.Choice = &ChoiceAnswer{Choice: w.Choice, Probabilities: w.Probabilities, Confidence: *w.Confidence}
		a.Label, a.Probability, a.Confidence = w.Choice, w.Probabilities[w.Choice], *w.Confidence
	case Score:
		if w.Score == nil || w.Confidence == nil {
			return a, errors.New("score answer incomplete")
		}
		probs := map[string]float64{}
		best := -1.0
		for i, l := range q.Labels {
			p := w.Probabilities[strconv.Itoa(i)]
			probs[l.Name] = p
			if p > best {
				best, a.Label = p, l.Name
			}
		}
		a.Score = &ScoreAnswer{Score: *w.Score, Probabilities: probs, Confidence: *w.Confidence}
		a.Probability, a.Confidence = best, *w.Confidence
	}
	return a, nil
}

func isProb(p float64) bool { return p >= 0 && p <= 1 && !math.IsNaN(p) }

// concentration is 1 - H(p)/ln(N), the server's confidence measure.
func concentration(ps []float64) float64 {
	h := 0.0
	for _, p := range ps {
		if p > 0 {
			h -= p * math.Log(p)
		}
	}
	return 1 - h/math.Log(float64(len(ps)))
}

// truncate keeps at most max characters (runes) of s; max <= 0 keeps everything.
func truncate(s string, max int) (string, bool) {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s, false
	}
	n := 0
	for i := range s {
		if n == max {
			return s[:i], true
		}
		n++
	}
	return s, false
}
