package codebook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// Route is where a gate sends an answer.
type Route string

const (
	Accept   Route = "accept"   // trust the answer
	Escalate Route = "escalate" // ask a larger model
	Human    Route = "human"    // ask a person
)

// Default thresholds, used until the agreement report proposes better ones.
const (
	DefaultAccept   = 0.8
	DefaultEscalate = 0.5
	DefaultFlag     = 0.2
)

// Gate holds the thresholds that route an answer by its confidence. An answer at or
// above Accept is accepted; at or above Escalate it goes to a larger model; below that
// it goes to a person. For a critical yes/no question, any answer whose probability of
// "true" is at or above Flag goes to a person, so a possible positive is never accepted
// on the model's word alone. Zero fields take the codebook's value, then the defaults.
type Gate struct {
	Accept   float64 `yaml:"accept" json:"accept"`
	Escalate float64 `yaml:"escalate" json:"escalate"`
	Flag     float64 `yaml:"flag" json:"flag"`
}

func (g Gate) validate() error {
	for _, v := range []float64{g.Accept, g.Escalate, g.Flag} {
		if !isProb(v) {
			return fmt.Errorf("threshold %v is outside 0 to 1", v)
		}
	}
	if g.Accept != 0 && g.Escalate != 0 && g.Escalate > g.Accept {
		return fmt.Errorf("escalate %v is above accept %v", g.Escalate, g.Accept)
	}
	return nil
}

func (g Gate) or(d Gate) Gate {
	if g.Accept == 0 {
		g.Accept = d.Accept
	}
	if g.Escalate == 0 {
		g.Escalate = d.Escalate
	}
	if g.Flag == 0 {
		g.Flag = d.Flag
	}
	return g
}

// gate returns q's effective thresholds.
func (c *Codebook) gate(q Question) Gate {
	return q.Gate.or(c.Gate).or(Gate{DefaultAccept, DefaultEscalate, DefaultFlag})
}

func (g Gate) route(q Question, a Answer) Route {
	if q.Critical && a.Noul != nil && a.Noul.True >= g.Flag {
		return Human
	}
	switch {
	case a.Confidence >= g.Accept:
		return Accept
	case a.Confidence >= g.Escalate:
		return Escalate
	default:
		return Human
	}
}

// Disagreement is one entry in the disagreement log: an answer that wasn't accepted,
// with why. It holds hashes and labels, never document text.
type Disagreement struct {
	Time         time.Time `json:"time"`
	Path         string    `json:"path"`
	DocHash      string    `json:"doc_hash"`
	CodebookHash string    `json:"codebook_hash"`
	Question     string    `json:"question"`
	Critical     bool      `json:"critical,omitempty"`
	Model        string    `json:"model"`
	Digest       string    `json:"digest"`
	Label        string    `json:"label"`
	Probability  float64   `json:"probability"`
	Confidence   float64   `json:"confidence"`
	Route        Route     `json:"route"`
	// Reason is "low-confidence", "critical-flag" or "models-disagree".
	Reason string `json:"reason"`
	// Other is the larger model's answer, when one was asked.
	Other *OtherAnswer `json:"other,omitempty"`
}

// OtherAnswer is a second coder's answer to the same question.
type OtherAnswer struct {
	Model       string  `json:"model"`
	Digest      string  `json:"digest"`
	Label       string  `json:"label"`
	Probability float64 `json:"probability"`
	Confidence  float64 `json:"confidence"`
}

// Disagreements lists r's answers that a gate didn't accept.
func (c *Codebook) Disagreements(r *Result, now time.Time) []Disagreement {
	var ds []Disagreement
	for _, a := range r.Answers {
		if a.Route == Accept {
			continue
		}
		reason := "low-confidence"
		if q := c.Question(a.Question); q != nil && q.Critical && a.Noul != nil && a.Noul.True >= c.gate(*q).Flag {
			reason = "critical-flag"
		}
		ds = append(ds, disagreement(r, a, reason, now))
	}
	return ds
}

func disagreement(r *Result, a Answer, reason string, now time.Time) Disagreement {
	return Disagreement{
		Time: now.UTC(), Path: r.Path, DocHash: r.DocHash, CodebookHash: r.CodebookHash,
		Question: a.Question, Critical: a.Critical, Model: r.Model, Digest: r.Digest,
		Label: a.Label, Probability: a.Probability, Confidence: a.Confidence,
		Route: a.Route, Reason: reason,
	}
}

// Escalate asks larger every question about doc again and settles r's escalated
// answers: where the larger model gives the same label with confidence at or above the
// question's Escalate threshold, the answer is accepted; otherwise it goes to a person,
// and a "models-disagree" entry is returned for the log. To load one large model at a
// time, escalate in a second pass after the first model has finished every document.
func (c *Codebook) Escalate(ctx context.Context, larger *Model, doc Document, r *Result, now time.Time) ([]Disagreement, error) {
	if larger == nil {
		return nil, errors.New("no larger model")
	}
	needed := false
	for _, a := range r.Answers {
		needed = needed || a.Route == Escalate
	}
	if !needed {
		return nil, nil
	}
	r2, err := c.apply(ctx, larger, doc)
	if err != nil {
		return nil, err
	}
	var ds []Disagreement
	for i, a := range r.Answers {
		if a.Route != Escalate {
			continue
		}
		b := r2.Answers[i]
		q := c.Question(a.Question)
		if b.Label == a.Label && b.Confidence >= c.gate(*q).Escalate && b.Route != Human {
			r.Answers[i].Route = Accept
			continue
		}
		r.Answers[i].Route = Human
		d := disagreement(r, r.Answers[i], "models-disagree", now)
		d.Other = &OtherAnswer{Model: r2.Model, Digest: r2.Digest, Label: b.Label, Probability: b.Probability, Confidence: b.Confidence}
		ds = append(ds, d)
	}
	return ds, nil
}

// AppendLog appends entries to the disagreement log at path, one JSON object per line,
// creating it readable by its owner only.
func AppendLog(path string, ds []Disagreement) error {
	if len(ds) == 0 {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	for _, d := range ds {
		if err := enc.Encode(d); err != nil {
			f.Close()
			return err
		}
	}
	return f.Close()
}
