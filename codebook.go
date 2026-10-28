// Package codebook applies a codebook to documents with local decision models and
// measures how far to trust the answers.
//
// A codebook is a YAML file of questions. Each question names its labels and defines
// every label in plain language, because decision models answer accurately only once
// each label has a definition. A model applies every question to a document in one
// call; gates then decide whether to accept each answer, send it to a larger model or
// send it to a person; and the agreement report compares the model's answers with a
// person's to propose those gates' thresholds.
package codebook

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Question types, as Ollama's System One API names them.
const (
	Noul   = "noul"   // yes or no; labels are exactly "false" and "true"
	Choice = "choice" // one of 2 to 26 named labels
	Score  = "score"  // 2 to 10 levels, ordered from lowest to highest
)

// Limits from Ollama's System One API.
const (
	maxQuestions = 64
	maxChoice    = 26
	maxScore     = 10
)

var nameRE = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)

// Codebook is a set of questions applied together to each document.
type Codebook struct {
	// Name is a short name for the codebook, for reports.
	Name string `yaml:"name"`
	// Gate holds the default thresholds for every question.
	Gate Gate `yaml:"gate"`
	// Questions are asked in this order.
	Questions []Question `yaml:"questions"`

	// Hash is the SHA-256 of the codebook file, in lowercase hex. Results record it, so
	// an answer can always be traced to the exact definitions the model was given.
	Hash string `yaml:"-"`

	// Model applies the codebook. Set it with [OpenModel] before calling Apply.
	Model *Model `yaml:"-"`
}

// Question is one question with a plain-language definition for every label.
type Question struct {
	// Name identifies the question in answers and reports: up to 100 ASCII letters,
	// digits, '_', '.' or '-'.
	Name string `yaml:"name"`
	// Type is Noul, Choice or Score.
	Type string `yaml:"type"`
	// Ask is the question put to the model.
	Ask string `yaml:"ask"`
	// Labels are the possible answers, each with its definition. For Noul they are
	// exactly "false" and "true"; for Score they run from the lowest level to the highest.
	Labels []Label `yaml:"labels"`
	// Critical marks a question whose misses matter most. Its positive answers go to a
	// person at a lower threshold, and the agreement report counts its misses separately.
	Critical bool `yaml:"critical"`
	// Gate overrides the codebook's thresholds for this question. Zero fields inherit.
	Gate Gate `yaml:"gate"`
}

// Label is one possible answer and its plain-language definition.
type Label struct {
	Name       string `yaml:"name"`
	Definition string `yaml:"definition"`
}

// Load reads and validates a codebook file. It refuses a codebook with a missing
// definition or one still marked TODO, since a label without a definition is guessed at.
func Load(path string) (*Codebook, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// Parse decodes and validates a codebook from YAML.
func Parse(b []byte) (*Codebook, error) {
	var c Codebook
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("parse codebook: %w", err)
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(b)
	c.Hash = hex.EncodeToString(sum[:])
	return &c, nil
}

func (c *Codebook) validate() error {
	if len(c.Questions) == 0 {
		return errors.New("codebook has no questions")
	}
	if len(c.Questions) > maxQuestions {
		return fmt.Errorf("codebook has %d questions; the limit is %d", len(c.Questions), maxQuestions)
	}
	if err := c.Gate.validate(); err != nil {
		return fmt.Errorf("codebook gate: %w", err)
	}
	seen := map[string]bool{}
	for i, q := range c.Questions {
		if !nameRE.MatchString(q.Name) {
			return fmt.Errorf("question %d: name %q must be 1-100 ASCII letters, digits, '_', '.' or '-'", i+1, q.Name)
		}
		if seen[q.Name] {
			return fmt.Errorf("question %q appears twice", q.Name)
		}
		seen[q.Name] = true
		if err := q.validate(); err != nil {
			return fmt.Errorf("question %q: %w", q.Name, err)
		}
	}
	return nil
}

func (q Question) validate() error {
	if strings.TrimSpace(q.Ask) == "" {
		return errors.New("ask is empty")
	}
	if isTODO(q.Ask) {
		return errors.New("ask is still TODO")
	}
	switch q.Type {
	case Noul:
		if len(q.Labels) != 2 || q.label("false") == nil || q.label("true") == nil {
			return errors.New(`a noul question has exactly two labels, "false" and "true"`)
		}
	case Choice:
		if len(q.Labels) < 2 || len(q.Labels) > maxChoice {
			return fmt.Errorf("a choice question has 2 to %d labels, not %d", maxChoice, len(q.Labels))
		}
	case Score:
		if len(q.Labels) < 2 || len(q.Labels) > maxScore {
			return fmt.Errorf("a score question has 2 to %d levels, not %d", maxScore, len(q.Labels))
		}
	default:
		return fmt.Errorf("type %q is not %s, %s or %s", q.Type, Noul, Choice, Score)
	}
	names := map[string]bool{}
	for _, l := range q.Labels {
		if !nameRE.MatchString(l.Name) {
			return fmt.Errorf("label %q must be 1-100 ASCII letters, digits, '_', '.' or '-'", l.Name)
		}
		if names[l.Name] {
			return fmt.Errorf("label %q appears twice", l.Name)
		}
		names[l.Name] = true
		if strings.TrimSpace(l.Definition) == "" {
			return fmt.Errorf("label %q has no definition", l.Name)
		}
		if isTODO(l.Definition) {
			return fmt.Errorf("label %q: definition is still TODO", l.Name)
		}
	}
	if err := q.Gate.validate(); err != nil {
		return fmt.Errorf("gate: %w", err)
	}
	return nil
}

func (q Question) label(name string) *Label {
	for i := range q.Labels {
		if q.Labels[i].Name == name {
			return &q.Labels[i]
		}
	}
	return nil
}

func isTODO(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), "TODO")
}

// Question returns the question with the given name, or nil.
func (c *Codebook) Question(name string) *Question {
	for i := range c.Questions {
		if c.Questions[i].Name == name {
			return &c.Questions[i]
		}
	}
	return nil
}
