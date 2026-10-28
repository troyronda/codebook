// The request and response types in this file are copied from Ollama's
// api/types.go (github.com/ollama/ollama v0.40.11), with the ordered maps
// replaced by Ordered. Ollama's licence:
//
// MIT License
//
// Copyright (c) Ollama
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package systemone

import (
	"encoding/json"
	"math"
	"time"
)

// ImageData is raw image bytes, sent base64-encoded.
type ImageData []byte

// Duration is a keep-alive duration. Negative values keep a model loaded
// indefinitely.
type Duration struct {
	time.Duration
}

// MarshalJSON writes -1 for a negative duration and a Go duration string
// otherwise, as Ollama does.
func (d Duration) MarshalJSON() ([]byte, error) {
	if d.Duration < 0 {
		return []byte("-1"), nil
	}
	return []byte(`"` + d.Duration.String() + `"`), nil
}

// UnmarshalJSON accepts seconds as a number or a Go duration string.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch t := v.(type) {
	case float64:
		if t < 0 {
			d.Duration = time.Duration(math.MaxInt64)
		} else {
			d.Duration = time.Duration(t * float64(time.Second))
		}
	case string:
		dur, err := time.ParseDuration(t)
		if err != nil {
			return err
		}
		if dur < 0 {
			dur = time.Duration(math.MaxInt64)
		}
		d.Duration = dur
	}
	return nil
}

// SystemOneRequest asks a decision model typed questions about a state.
type SystemOneRequest struct {
	Model     string              `json:"model"`
	Runner    string              `json:"runner,omitempty"`
	State     json.RawMessage     `json:"state"`
	Images    []ImageData         `json:"images,omitempty"`
	Videos    []json.RawMessage   `json:"videos,omitempty"`
	Questions *SystemOneQuestions `json:"questions"`
	KeepAlive *Duration           `json:"keep_alive,omitempty"`
}

// SystemOneQuestions keeps questions in request order, which is the order of
// the answers.
type SystemOneQuestions = Ordered[SystemOneQuestion]

// SystemOneQuestion is a noul (yes/no), choice, or score question. Criteria is
// an object for noul and choice questions and an ordered array for score.
type SystemOneQuestion struct {
	Type         string          `json:"type"`
	Instructions json.RawMessage `json:"instructions"`
	Criteria     json.RawMessage `json:"criteria,omitempty"`
}

// SystemOneResponse holds one answer per question. Each answer's type field
// says which answer fields it has.
type SystemOneResponse struct {
	Model                 string            `json:"model"`
	Answers               *SystemOneAnswers `json:"answers"`
	Usage                 SystemOneUsage    `json:"usage"`
	PromptEvalCachedCount *int              `json:"prompt_eval_cached_count,omitempty"`
}

// SystemOneAnswers keeps answers in request order.
type SystemOneAnswers = Ordered[json.RawMessage]

// SystemOneUsage counts the tokens evaluated and generated to answer.
type SystemOneUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}
