// Package fakeollama is a fake Ollama server for tests. It answers System One requests
// with fixed answers, so a run replays exactly.
package fakeollama

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"

	"github.com/troyronda/modelguard"

	"github.com/troyronda/codebook/internal/systemone"
)

// Version is the server version the fake reports.
const Version = "0.0.0-fake"

// AnswerFunc answers one question about state. It returns the answer object as the
// server would send it.
type AnswerFunc func(model, state, name string, q systemone.SystemOneQuestion) any

// Server is a fake Ollama server.
type Server struct {
	*httptest.Server
	// Models maps model names (with tag) to digests. Remote marks models the server
	// would forward to another host.
	Models map[string]string
	Remote map[string]bool
	// Answer answers each question; Hashed by default.
	Answer AnswerFunc

	mu       sync.Mutex
	requests []systemone.SystemOneRequest
	auth     string
}

// New starts a fake server with the given models. Close it when done.
func New(models map[string]string) *Server {
	s := &Server{Models: models, Remote: map[string]bool{}, Answer: Hashed}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"version": Version})
	})
	mux.HandleFunc("GET /api/tags", func(w http.ResponseWriter, r *http.Request) {
		type model struct {
			Name       string `json:"name"`
			Model      string `json:"model"`
			Digest     string `json:"digest"`
			RemoteHost string `json:"remote_host,omitempty"`
		}
		var list struct {
			Models []model `json:"models"`
		}
		for name, digest := range s.Models {
			m := model{Name: name, Model: name, Digest: digest}
			if s.Remote[name] {
				m.RemoteHost = "https://remote.example"
			}
			list.Models = append(list.Models, m)
		}
		writeJSON(w, list)
	})
	mux.HandleFunc("POST /v1/systemone", s.systemOne)
	s.Server = httptest.NewServer(mux)
	return s
}

// Guard returns a modelguard config that allows this server's port and the
// given models, logging to log.
func (s *Server) Guard(log string, models ...string) modelguard.Config {
	u, _ := url.Parse(s.URL)
	p, _ := strconv.Atoi(u.Port())
	return modelguard.Config{Ports: []uint16{uint16(p)}, Models: models, Log: log}
}

// Auth returns the Authorization header of the last System One request.
func (s *Server) Auth() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.auth
}

// Requests returns the System One requests received so far.
func (s *Server) Requests() []systemone.SystemOneRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]systemone.SystemOneRequest(nil), s.requests...)
}

func (s *Server) systemOne(w http.ResponseWriter, r *http.Request) {
	var req systemone.SystemOneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.requests = append(s.requests, req)
	s.auth = r.Header.Get("Authorization")
	s.mu.Unlock()
	if _, ok := s.Models[req.Model]; !ok {
		if _, ok := s.Models[req.Model+":latest"]; !ok {
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, map[string]string{"error": "model not found"})
			return
		}
	}
	var state string
	if err := json.Unmarshal(req.State, &state); err != nil {
		state = string(req.State)
	}
	answers := &systemone.SystemOneAnswers{}
	for name, q := range req.Questions.All() {
		b, _ := json.Marshal(s.Answer(req.Model, state, name, q))
		answers.Set(name, b)
	}
	writeJSON(w, systemone.SystemOneResponse{Model: req.Model, Answers: answers,
		Usage: systemone.SystemOneUsage{InputTokens: len(state) / 4, OutputTokens: 1}})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// Hashed answers from a hash of the model, state and question, so every answer is
// fixed but they differ between documents and models.
func Hashed(model, state, name string, q systemone.SystemOneQuestion) any {
	sum := sha256.Sum256([]byte(model + "\x00" + state + "\x00" + name))
	u := float64(binary.BigEndian.Uint64(sum[:8])>>11) / (1 << 53)
	switch q.Type {
	case "noul":
		return Noul(round(u))
	case "choice":
		keys := orderedKeys(q.Criteria)
		return Choice(keys, int(sum[8])%len(keys), 0.4+0.59*u)
	case "score":
		var levels []string
		_ = json.Unmarshal(q.Criteria, &levels)
		probs := make([]float64, len(levels))
		best := int(sum[8]) % len(levels)
		for i := range probs {
			probs[i] = (1 - (0.4 + 0.59*u)) / float64(len(levels)-1)
		}
		probs[best] = 0.4 + 0.59*u
		return Score(probs)
	}
	return nil
}

// Noul is a yes/no answer with probability p of true.
func Noul(p float64) any {
	return map[string]any{"type": "noul", "noul": p}
}

// Choice is a choice of labels[pick] with probability p; the rest share 1-p.
func Choice(labels []string, pick int, p float64) any {
	probs := map[string]float64{}
	ps := make([]float64, len(labels))
	for i, l := range labels {
		ps[i] = (1 - p) / float64(len(labels)-1)
		if i == pick {
			ps[i] = p
		}
		probs[l] = round(ps[i])
	}
	return map[string]any{"type": "choice", "choice": labels[pick], "probabilities": probs, "confidence": round(concentration(ps))}
}

// Score is a score answer from level probabilities, lowest level first.
func Score(probs []float64) any {
	score := 0.0
	byIndex := map[string]float64{}
	legend := map[string]string{}
	for i, p := range probs {
		score += float64(i) * p
		byIndex[strconv.Itoa(i)] = round(p)
		legend[strconv.Itoa(i)] = fmt.Sprintf("level %d", i)
	}
	return map[string]any{"type": "score", "score": round(score), "legend": legend, "probabilities": byIndex, "confidence": round(concentration(probs))}
}

func concentration(ps []float64) float64 {
	h := 0.0
	for _, p := range ps {
		if p > 0 {
			h -= p * math.Log(p)
		}
	}
	return 1 - h/math.Log(float64(len(ps)))
}

func round(v float64) float64 { return math.Round(v*1e4) / 1e4 }

// orderedKeys returns a JSON object's keys in their order in the source.
func orderedKeys(obj json.RawMessage) []string {
	m := &systemone.SystemOneAnswers{} // any ordered map of raw values will do
	if err := json.Unmarshal(obj, m); err != nil {
		return nil
	}
	var keys []string
	for k := range m.All() {
		keys = append(keys, k)
	}
	return keys
}
