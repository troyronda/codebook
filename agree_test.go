package codebook_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/troyronda/codebook"
)

func row(path, q, answer string, p, c float64, correct string) codebook.EvalRow {
	return codebook.EvalRow{Path: path, Question: q, Answer: answer, Probability: p, Confidence: c, Correct: correct}
}

// Hand-computed: see the comments for the expected tallies.
func agreeRows() []codebook.EvalRow {
	rows := []codebook.EvalRow{}
	// example: by confidence, y y y y y n y n y n.
	for i, c := range []float64{0.99, 0.97, 0.95, 0.93, 0.90, 0.85, 0.80, 0.70, 0.60, 0.40} {
		correct := map[bool]string{true: "y", false: "n"}[!(i == 5 || i == 7 || i == 9)]
		path := []string{"journal/a.md", "reading/books/b.md", "reading/c.md", "chats/d.md", "e.md"}[i%5]
		rows = append(rows, row(path, "example", "true", c, c, correct))
	}
	// private (critical): positives are rows 2 (missed, P(true) 0.10) and 3 (P(true) 0.70).
	rows = append(rows,
		row("journal/a.md", "private", "false", 0.95, 0.95, "Y"),
		row("journal/b.md", "private", "false", 0.90, 0.90, "n"),
		row("journal/c.md", "private", "true", 0.70, 0.70, "yes"),
		row("journal/d.md", "private", "true", 0.60, 0.60, "no"),
		row("journal/e.md", "private", "false", 0.99, 0.99, "1"),
		row("journal/f.md", "topic", "soil", 0.5, 0.5, ""),      // unlabelled
		row("journal/g.md", "topic", "soil", 0.5, 0.5, "maybe"), // unreadable
	)
	return rows
}

func TestAgree(t *testing.T) {
	cb := load(t)
	r := cb.Agree(agreeRows(), codebook.AgreeOptions{Groups: []string{"journal/", "reading/", "reading/books/"}})

	if r.Rows != 17 || r.Labelled != 15 || r.Unreadable != 1 || r.Overall != (codebook.Tally{N: 15, Agree: 10}) {
		t.Fatalf("totals: rows %d labelled %d unreadable %d overall %+v", r.Rows, r.Labelled, r.Unreadable, r.Overall)
	}
	groups := func(gs []codebook.Group) map[string]codebook.Tally {
		m := map[string]codebook.Tally{}
		for _, g := range gs {
			m[g.Name] = g.Tally
		}
		return m
	}
	q := groups(r.Questions)
	if q["example"] != (codebook.Tally{N: 10, Agree: 7}) || q["private"] != (codebook.Tally{N: 5, Agree: 3}) {
		t.Errorf("by question %+v", q)
	}
	f := groups(r.Folders)
	// journal: example rows 0,5 (y, n) and five private rows (3 agree); reading/books: rows 1,6;
	// reading/: rows 2,7; other: rows 3,4,8,9.
	want := map[string]codebook.Tally{"journal/": {N: 7, Agree: 4}, "reading/books/": {N: 2, Agree: 2}, "reading/": {N: 2, Agree: 1}, "other": {N: 4, Agree: 3}}
	for k, v := range want {
		if f[k] != v {
			t.Errorf("folder %s = %+v, want %+v", k, f[k], v)
		}
	}
	b := groups(r.Bands)
	wantBands := map[string]codebook.Tally{
		"0.95-1.00": {N: 5, Agree: 5}, // .99 .97 .95 and private .95 .99
		"0.90-0.95": {N: 3, Agree: 2}, // .93 .90 and private .90 (n)
		"0.80-0.90": {N: 2, Agree: 1},
		"0.70-0.80": {N: 2, Agree: 1}, // .70 n and private .70 y
		"0.50-0.70": {N: 2, Agree: 1}, // .60 y and private .60 n
		"0.00-0.50": {N: 1, Agree: 0},
	}
	for k, v := range wantBands {
		if b[k] != v {
			t.Errorf("band %s = %+v, want %+v", k, b[k], v)
		}
	}
	if len(r.Critical) != 1 || r.Critical[0] != (codebook.CriticalTally{Question: "private", Labelled: 5, Misses: 2, MissedPositives: 1, FalseAlarms: 1}) {
		t.Errorf("critical %+v", r.Critical)
	}

	props := map[string]codebook.Proposal{}
	for _, p := range r.Proposals {
		props[p.Question] = p
	}
	if p := props["example"]; p.Accept != 0.90 || p.Escalate != 0.80 || p.Support != 5 {
		t.Errorf("example proposal %+v", p)
	}
	if p := props["private"]; p.Accept != 1 || p.Escalate != 1 || p.Flag != 0.10 || p.Note == "" {
		t.Errorf("private proposal %+v", p)
	}
	if p := props["topic"]; p.Note == "" || p.Accept != codebook.DefaultAccept {
		t.Errorf("topic proposal %+v", p)
	}

	var out bytes.Buffer
	if err := r.WriteText(&out); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"overall agreement", "missed positives 1", "0.90 / 0.80"} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("report lacks %q:\n%s", s, out.String())
		}
	}
}

func TestEvalCSVRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	w, err := codebook.NewEvalWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	in := []codebook.EvalRow{row("a, b.md", "topic", "soil", 0.5, 0.25, ""), {Path: "c.md", Question: "x", Answer: "true", Probability: 1, Confidence: 1, Truncated: true}}
	if err := w.Write(in); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buf.String(), "path,question,answer,probability,confidence,truncated,correct\n") {
		t.Fatalf("header: %q", buf.String())
	}
	out, err := codebook.ReadEval(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0] != in[0] || out[1] != in[1] {
		t.Fatalf("round trip: %+v", out)
	}
	if _, err := codebook.ReadEval(strings.NewReader("path,question,answer,probability,confidence,truncated,correct\na,b,c,secret words,1,false,\n")); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("bad cell: err %v", err)
	}
}
