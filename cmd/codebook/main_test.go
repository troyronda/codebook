package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/troyronda/codebook"
	"github.com/troyronda/codebook/internal/fakeollama"
	"github.com/troyronda/codebook/internal/testvault"
)

// Phrases from the fixture notes. None may appear in any output.
var noteText = []string{"Pemberly", "Runner beans", "Powdery mildew", "cardboard", "yellow ones", "flat file"}

func checkNoText(t *testing.T, what, s string) {
	t.Helper()
	for _, p := range noteText {
		if strings.Contains(s, p) {
			t.Errorf("%s contains note text %q", what, p)
		}
	}
}

func fake(t *testing.T) *fakeollama.Server {
	t.Helper()
	s := fakeollama.New(map[string]string{"sprout:latest": "sha256:aaa", "oak:latest": "sha256:bbb", "fern:cloud": "sha256:ccc"})
	t.Cleanup(s.Close)
	return s
}

func cmd(t *testing.T, stdin string, args ...string) (string, string, error) {
	t.Helper()
	home := t.TempDir() // the default modelguard log goes in the user cache folder
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	var out, errOut bytes.Buffer
	err := run(context.Background(), args, strings.NewReader(stdin), &out, &errOut)
	return out.String(), errOut.String(), err
}

func TestEvalThenAgree(t *testing.T) {
	s := fake(t)
	root := testvault.Copy(t, "../../testdata/notes")
	csvPath := filepath.Join(t.TempDir(), "eval", "run.csv")
	stdout, stderr, err := cmd(t, "", "eval", "-codebook", "../../testdata/codebook.yaml", "-model", "sprout", "-allow", "sprout,oak",
		"-endpoint", s.URL, "-max-chars", "40", "-out", csvPath, root)
	if err != nil {
		t.Fatalf("eval: %v\n%s", err, stderr)
	}
	if got := len(s.Requests()); got != 7 {
		t.Errorf("%d model calls, want one per file (7)", got)
	}
	b, err := os.ReadFile(csvPath)
	if err != nil {
		t.Fatal(err)
	}
	recs, err := csv.NewReader(bytes.NewReader(b)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1+7*4 {
		t.Fatalf("%d CSV records, want header + 28", len(recs))
	}
	truncated := 0
	for _, r := range recs[1:] {
		if r[5] == "true" {
			truncated++
		}
		if r[6] != "" {
			t.Errorf("correct column filled: %v", r)
		}
	}
	if truncated == 0 || truncated == 28 {
		t.Errorf("%d truncated rows; want some, not all", truncated)
	}
	meta, err := os.ReadFile(csvPath + ".meta.json")
	if err != nil {
		t.Fatal(err)
	}
	var m evalMeta
	if err := json.Unmarshal(meta, &m); err != nil || m.Digest != "sha256:aaa" || m.ServerVersion != fakeollama.Version || m.SkippedCopies != 1 || m.Files != 7 {
		t.Errorf("meta %s (%v)", meta, err)
	}
	for what, s := range map[string]string{"stdout": stdout, "stderr": stderr, "csv": string(b), "meta": string(meta)} {
		checkNoText(t, "eval "+what, s)
	}

	// The same run again refuses to overwrite.
	if _, _, err := cmd(t, "", "eval", "-codebook", "../../testdata/codebook.yaml", "-model", "sprout", "-allow", "sprout",
		"-endpoint", s.URL, "-out", csvPath, root); err == nil {
		t.Error("eval overwrote an existing CSV")
	}

	// Label every answer correct except the critical ones, then agree.
	for i, r := range recs[1:] {
		r[6] = "y"
		if r[1] == "private" && i%2 == 0 {
			r[6] = "n"
		}
	}
	var lab bytes.Buffer
	_ = csv.NewWriter(&lab).WriteAll(recs)
	labelled := filepath.Join(t.TempDir(), "labelled.csv")
	if err := os.WriteFile(labelled, lab.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err = cmd(t, "", "agree", "-codebook", "../../testdata/codebook.yaml", "-group", "journal/,reading/,chats/", labelled)
	if err != nil {
		t.Fatalf("agree: %v\n%s", err, stderr)
	}
	for _, want := range []string{"labelled 28", "journal/", "other", "private", "proposed gates"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("agree output lacks %q:\n%s", want, stdout)
		}
	}
	checkNoText(t, "agree", stdout+stderr)
}

func TestRun(t *testing.T) {
	s := fake(t)
	root := testvault.Copy(t, "../../testdata/notes")
	a, b := filepath.Join(root, "journal", "walk.md"), filepath.Join(root, "chats", "squash.md")
	logPath := filepath.Join(t.TempDir(), "disagreements.jsonl")
	base := []string{"run", "-codebook", "../../testdata/codebook.yaml", "-model", "sprout", "-allow", "sprout,oak", "-endpoint", s.URL, "-log", logPath}

	stdout, stderr, err := cmd(t, "", append(base, a, b)...)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, stderr)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 2 {
		t.Fatalf("%d results, want 2", len(lines))
	}
	var r codebook.Result
	if err := json.Unmarshal([]byte(lines[0]), &r); err != nil || r.Path != a || len(r.Answers) != 4 || r.Digest != "sha256:aaa" {
		t.Errorf("result %+v (%v)", r, err)
	}
	// Paths on stdin give the same results.
	stdin, _, err := cmd(t, a+"\n\n"+b+"\n", base...)
	if err != nil || stdin != stdout {
		t.Errorf("stdin run differs (%v)", err)
	}
	logged, _ := os.ReadFile(logPath)
	for what, s := range map[string]string{"stdout": stdout, "stderr": stderr, "log": string(logged)} {
		checkNoText(t, "run "+what, s)
	}

	// With a larger model, escalated answers are settled in a second pass.
	before := len(s.Requests())
	if _, stderr, err := cmd(t, "", append(base, "-escalate-model", "oak", a, b)...); err != nil {
		t.Fatalf("run with escalation: %v\n%s", err, stderr)
	}
	reqs := s.Requests()[before:]
	if len(reqs) < 2 || reqs[0].Model != "sprout" || reqs[1].Model != "sprout" {
		t.Errorf("the first pass didn't finish before escalating: %d requests", len(reqs))
	}
	for _, r := range reqs[2:] {
		if r.Model != "oak" {
			t.Errorf("second pass used %q", r.Model)
		}
	}
}

func TestRunRefusesModels(t *testing.T) {
	s := fake(t)
	for _, tc := range [][]string{
		{"-model", "fern:cloud", "-allow", "fern:cloud"},
		{"-model", "oak", "-allow", "sprout"},
		{"-model", "sprout", "-allow", "sprout", "-escalate-model", "fern:cloud"},
	} {
		args := append([]string{"run", "-codebook", "../../testdata/codebook.yaml", "-endpoint", s.URL}, tc...)
		if _, _, err := cmd(t, "", append(args, "../../testdata/notes/inbox-note.md")...); err == nil {
			t.Errorf("%v ran", tc)
		}
	}
	for _, r := range s.Requests() {
		if strings.Contains(r.Model, "cloud") {
			t.Errorf("a request reached the server for %q", r.Model)
		}
	}
	if _, _, err := cmd(t, "", "run", "-codebook", "../../testdata/codebook.yaml", "-model", "sprout", "-allow", "sprout", "-endpoint", "http://192.0.2.1:11434", "x.md"); err == nil {
		t.Error("ran against a non-loopback host")
	}
}
