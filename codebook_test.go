package codebook_test

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ollama/ollama/api"
	"github.com/troyronda/codebook"
	"github.com/troyronda/codebook/internal/fakeollama"
)

const (
	small = "sprout:latest"
	large = "oak:latest"
)

var allow = []string{"sprout", "oak"}

func newServer(t *testing.T) *fakeollama.Server {
	t.Helper()
	s := fakeollama.New(map[string]string{small: "sha256:aaa", large: "sha256:bbb", "fern-cloud:latest": "sha256:ccc"})
	t.Cleanup(s.Close)
	return s
}

func open(t *testing.T, s *fakeollama.Server, name string) *codebook.Model {
	t.Helper()
	c, err := codebook.NewClient(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	m, err := codebook.OpenModel(context.Background(), c, name, allow)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func load(t *testing.T) *codebook.Codebook {
	t.Helper()
	cb, err := codebook.Load("testdata/codebook.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return cb
}

func TestLoad(t *testing.T) {
	cb := load(t)
	if len(cb.Questions) != 4 || !cb.Question("private").Critical || len(cb.Hash) != 64 {
		t.Fatalf("unexpected codebook: %d questions, hash %q", len(cb.Questions), cb.Hash)
	}
}

func TestParseRejects(t *testing.T) {
	q := func(body string) string { return "questions:\n  - name: q\n" + body }
	for name, src := range map[string]string{
		"no questions":         "name: x\n",
		"todo definition":      q("    type: noul\n    ask: Is it?\n    labels:\n      - {name: \"false\", definition: \"TODO(someone): write\"}\n      - {name: \"true\", definition: Yes.}\n"),
		"todo ask":             q("    type: noul\n    ask: TODO\n    labels:\n      - {name: \"false\", definition: No.}\n      - {name: \"true\", definition: Yes.}\n"),
		"missing definition":   q("    type: noul\n    ask: Is it?\n    labels:\n      - {name: \"false\"}\n      - {name: \"true\", definition: Yes.}\n"),
		"noul wrong labels":    q("    type: noul\n    ask: Is it?\n    labels:\n      - {name: no, definition: No.}\n      - {name: yes, definition: Yes.}\n"),
		"one choice":           q("    type: choice\n    ask: Which?\n    labels:\n      - {name: a, definition: A.}\n"),
		"unknown type":         q("    type: rank\n    ask: Which?\n    labels:\n      - {name: a, definition: A.}\n      - {name: b, definition: B.}\n"),
		"unknown field":        q("    type: noul\n    ask: Is it?\n    colour: red\n    labels:\n      - {name: \"false\", definition: No.}\n      - {name: \"true\", definition: Yes.}\n"),
		"bad name":             "questions:\n  - name: has space\n    type: noul\n    ask: Is it?\n    labels:\n      - {name: \"false\", definition: No.}\n      - {name: \"true\", definition: Yes.}\n",
		"escalate over accept": "gate: {accept: 0.5, escalate: 0.9}\n" + q("    type: noul\n    ask: Is it?\n    labels:\n      - {name: \"false\", definition: No.}\n      - {name: \"true\", definition: Yes.}\n"),
	} {
		if _, err := codebook.Parse([]byte(src)); err == nil {
			t.Errorf("%s: parsed, want an error", name)
		}
	}
}

func TestCheckAllowed(t *testing.T) {
	for _, tc := range []struct {
		name  string
		allow []string
		ok    bool
	}{
		{"sprout", allow, true},
		{"sprout:latest", allow, true},
		{"oak:7b", allow, false},
		{"oak:7b", []string{"oak:7b"}, true},
		{"fern:cloud", []string{"fern:cloud"}, false},
		{"fern-cloud", []string{"fern-cloud"}, false},
		{"CLOUDY", []string{"CLOUDY"}, false},
		{"sprout", nil, false},
	} {
		err := codebook.CheckAllowed(tc.name, tc.allow)
		if (err == nil) != tc.ok {
			t.Errorf("CheckAllowed(%q, %v) = %v, want ok=%v", tc.name, tc.allow, err, tc.ok)
		}
		if err != nil && !errors.Is(err, codebook.ErrNotAllowed) {
			t.Errorf("CheckAllowed(%q): error %v is not ErrNotAllowed", tc.name, err)
		}
	}
}

func TestCloudModelNeverRuns(t *testing.T) {
	s := newServer(t)
	c, _ := codebook.NewClient(s.URL)
	_, err := codebook.OpenModel(context.Background(), c, "fern-cloud", []string{"fern-cloud"})
	if !errors.Is(err, codebook.ErrNotAllowed) {
		t.Fatalf("OpenModel(cloud) = %v, want ErrNotAllowed", err)
	}
	if n := len(s.Requests()); n != 0 {
		t.Fatalf("%d requests reached the server", n)
	}
}

func TestRemoteModelRefused(t *testing.T) {
	s := newServer(t)
	s.Remote[large] = true
	c, _ := codebook.NewClient(s.URL)
	if _, err := codebook.OpenModel(context.Background(), c, "oak", allow); !errors.Is(err, codebook.ErrNotAllowed) {
		t.Fatalf("OpenModel(remote) = %v, want ErrNotAllowed", err)
	}
}

func TestNewClientLoopbackOnly(t *testing.T) {
	for host, ok := range map[string]bool{
		"http://127.0.0.1:11434": true,
		"http://localhost:11434": true,
		"http://[::1]:11434":     true,
		"http://10.0.0.5:11434":  false,
		"https://ollama.example": false,
		"http://0.0.0.0:11434":   false,
	} {
		if _, err := codebook.NewClient(host); (err == nil) != ok {
			t.Errorf("NewClient(%q) = %v, want ok=%v", host, err, ok)
		}
	}
}

func TestOpenModelRecordsVersionAndDigest(t *testing.T) {
	m := open(t, newServer(t), "sprout")
	if m.ServerVersion != fakeollama.Version || m.Digest != "sha256:aaa" {
		t.Fatalf("got version %q digest %q", m.ServerVersion, m.Digest)
	}
	c, _ := codebook.NewClient(newServer(t).URL)
	if _, err := codebook.OpenModel(context.Background(), c, "sprout:big", []string{"sprout:big"}); err == nil {
		t.Fatal("opened a model the server doesn't have")
	}
}

// fixed answers by question, for checking decoding and gates.
func fixed(answers map[string]any) fakeollama.AnswerFunc {
	return func(_, _, name string, _ api.SystemOneQuestion) any { return answers[name] }
}

func TestApply(t *testing.T) {
	s := newServer(t)
	s.Answer = fixed(map[string]any{
		"topic":   fakeollama.Choice([]string{"vegetables", "soil", "tools"}, 1, 0.97),
		"private": fakeollama.Noul(0.3),                         // under 0.5, but above the critical flag
		"example": fakeollama.Noul(0.85),                        // confidence about 0.39: to a person
		"finish":  fakeollama.Score([]float64{0.05, 0.9, 0.05}), // confidence about 0.64: escalate
	})
	cb := load(t)
	cb.Model = open(t, s, "sprout")
	cb.Model.MaxChars = 10
	text := "Runner beans climbed the fence — fast."
	r, err := cb.Apply(context.Background(), codebook.Document{Path: "journal/beans.md", Text: text})
	if err != nil {
		t.Fatal(err)
	}

	reqs := s.Requests()
	if len(reqs) != 1 {
		t.Fatalf("%d requests, want one call for every question", len(reqs))
	}
	if reqs[0].Questions.Len() != 4 {
		t.Fatalf("%d questions sent, want 4", reqs[0].Questions.Len())
	}
	var order []string
	for name := range reqs[0].Questions.All() {
		order = append(order, name)
	}
	if strings.Join(order, ",") != "topic,private,example,finish" {
		t.Errorf("question order %v", order)
	}
	topic, _ := reqs[0].Questions.Get("topic")
	if !strings.HasPrefix(string(topic.Criteria), `{"vegetables":`) {
		t.Errorf("choice criteria lost label order: %s", topic.Criteria)
	}
	finish, _ := reqs[0].Questions.Get("finish")
	if !strings.HasPrefix(string(finish.Criteria), `["scrap: A fragment`) {
		t.Errorf("score criteria: %s", finish.Criteria)
	}
	var sent string
	_ = json.Unmarshal(reqs[0].State, &sent)
	if sent != "Runner bea" || !r.Truncated || r.Chars != 10 {
		t.Errorf("sent %q, truncated %v, chars %d", sent, r.Truncated, r.Chars)
	}

	if r.Model != "sprout" || r.Digest != "sha256:aaa" || r.ServerVersion != fakeollama.Version || r.CodebookHash != cb.Hash || len(r.DocHash) != 64 {
		t.Errorf("result provenance %+v", r)
	}
	want := map[string]struct {
		label string
		route codebook.Route
	}{
		"topic":   {"soil", codebook.Accept},
		"private": {"false", codebook.Human},
		"example": {"true", codebook.Human},
		"finish":  {"draft", codebook.Escalate},
	}
	for _, a := range r.Answers {
		w := want[a.Question]
		if a.Label != w.label || a.Route != w.route {
			t.Errorf("%s: label %q route %q, want %q %q (confidence %.3f)", a.Question, a.Label, a.Route, w.label, w.route, a.Confidence)
		}
	}
	if a := r.Answers[1]; a.Noul == nil || a.Noul.True != 0.3 || math.Abs(a.Probability-0.7) > 1e-9 {
		t.Errorf("noul decode: %+v", a)
	}
	if a := r.Answers[3]; a.Score == nil || a.Score.Probabilities["draft"] != 0.9 || math.Abs(a.Score.Score-1) > 1e-9 {
		t.Errorf("score decode: %+v", a.Score)
	}
	if a := r.Answers[0]; a.Choice == nil || a.Choice.Probabilities["soil"] != 0.97 {
		t.Errorf("choice decode: %+v", a.Choice)
	}

	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "Runner") {
		t.Errorf("result holds document text: %s", b)
	}
}

func TestApplyRejectsWrongAnswer(t *testing.T) {
	s := newServer(t)
	s.Answer = func(_, _, name string, q api.SystemOneQuestion) any {
		if name == "topic" {
			return fakeollama.Choice([]string{"weeds", "soil"}, 0, 0.9)
		}
		return fakeollama.Hashed("", "", name, q)
	}
	cb := load(t)
	cb.Model = open(t, s, "sprout")
	if _, err := cb.Apply(context.Background(), codebook.Document{Path: "a.md", Text: "x"}); err == nil {
		t.Fatal("accepted a choice that isn't a label")
	}
}

func TestEscalateAndLog(t *testing.T) {
	s := newServer(t)
	s.Answer = func(model, _, name string, _ api.SystemOneQuestion) any {
		switch name {
		case "topic": // escalated; the larger model agrees
			return fakeollama.Choice([]string{"vegetables", "soil", "tools"}, 0, 0.9)
		case "finish": // escalated; the larger model picks another level
			if strings.HasPrefix(model, "oak") {
				return fakeollama.Score([]float64{0.9, 0.05, 0.05})
			}
			return fakeollama.Score([]float64{0.05, 0.9, 0.05})
		}
		return fakeollama.Noul(0.001)
	}
	cb := load(t)
	cb.Model = open(t, s, "sprout")
	doc := codebook.Document{Path: "journal/beans.md", Text: "Runner beans."}
	r, err := cb.Apply(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if r.Answers[0].Route != codebook.Escalate || r.Answers[3].Route != codebook.Escalate {
		t.Fatalf("routes before escalation: %v %v", r.Answers[0].Route, r.Answers[3].Route)
	}
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	ds, err := cb.Escalate(context.Background(), open(t, s, "oak"), doc, r, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.Answers[0].Route != codebook.Accept || r.Answers[3].Route != codebook.Human {
		t.Errorf("routes after escalation: %v %v", r.Answers[0].Route, r.Answers[3].Route)
	}
	if len(ds) != 1 || ds[0].Reason != "models-disagree" || ds[0].Other == nil || ds[0].Other.Label != "scrap" || ds[0].Other.Digest != "sha256:bbb" {
		t.Fatalf("disagreements %+v", ds)
	}

	path := filepath.Join(t.TempDir(), "disagreements.jsonl")
	if err := codebook.AppendLog(path, ds); err != nil {
		t.Fatal(err)
	}
	if err := codebook.AppendLog(path, ds); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if n := strings.Count(string(b), "\n"); n != 2 {
		t.Errorf("log has %d lines, want 2", n)
	}
	if strings.Contains(string(b), "Runner") {
		t.Error("log holds document text")
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("log mode %v", info.Mode().Perm())
	}
}

func TestDisagreementsCriticalFlag(t *testing.T) {
	s := newServer(t)
	s.Answer = fixed(map[string]any{
		"topic":   fakeollama.Choice([]string{"vegetables", "soil", "tools"}, 0, 0.99),
		"private": fakeollama.Noul(0.25),
		"example": fakeollama.Noul(0.999),
		"finish":  fakeollama.Score([]float64{0.005, 0.005, 0.99}),
	})
	cb := load(t)
	cb.Model = open(t, s, "sprout")
	r, err := cb.Apply(context.Background(), codebook.Document{Path: "a.md", Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	ds := cb.Disagreements(r, time.Now())
	if len(ds) != 1 || ds[0].Question != "private" || ds[0].Reason != "critical-flag" || !ds[0].Critical {
		t.Fatalf("disagreements %+v", ds)
	}
}
