package codebook

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
)

// AgreeOptions configures the agreement report.
type AgreeOptions struct {
	// Groups are path prefixes to report agreement for, such as "library/". The longest
	// matching prefix wins; other paths fall in "other". With no groups, paths are
	// grouped by their top-level folder.
	Groups []string
	// AcceptTarget is the agreement an accepted answer should reach (default 0.95).
	AcceptTarget float64
	// EscalateTarget is the agreement worth a second model's opinion rather than a
	// person's (default 0.8).
	EscalateTarget float64
	// MinSupport is the fewest labelled answers a proposed threshold rests on (default 5).
	MinSupport int
}

// Tally counts labelled answers and those the person marked correct.
type Tally struct {
	N     int `json:"n"`
	Agree int `json:"agree"`
}

// Rate is the share of answers marked correct, or NaN with none.
func (t Tally) Rate() float64 {
	if t.N == 0 {
		return math.NaN()
	}
	return float64(t.Agree) / float64(t.N)
}

func (t *Tally) add(ok bool) {
	t.N++
	if ok {
		t.Agree++
	}
}

// Group is a named tally.
type Group struct {
	Name string `json:"name"`
	Tally
}

// CriticalTally counts a critical question's misses. A missed positive is a "false"
// answer the person marked wrong; a false alarm is a "true" answer marked wrong.
type CriticalTally struct {
	Question        string `json:"question"`
	Labelled        int    `json:"labelled"`
	Misses          int    `json:"misses"`
	MissedPositives int    `json:"missed_positives"`
	FalseAlarms     int    `json:"false_alarms"`
}

// Proposal is a proposed gate for one question.
type Proposal struct {
	Question string `json:"question"`
	Gate
	// Support is how many labelled answers sit at or above the Accept threshold.
	Support int `json:"support"`
	// Note says when a threshold couldn't be supported by the sample.
	Note string `json:"note,omitempty"`
}

// Report is the agreement between a model and a person. It holds counts, question
// names and folder names only, never document text.
type Report struct {
	Rows       int             `json:"rows"`
	Labelled   int             `json:"labelled"`
	Unreadable int             `json:"unreadable"` // correct cells that aren't y or n
	Overall    Tally           `json:"overall"`
	Questions  []Group         `json:"questions"`
	Folders    []Group         `json:"folders"`
	Bands      []Group         `json:"bands"`
	Critical   []CriticalTally `json:"critical"`
	Proposals  []Proposal      `json:"proposals"`
}

// bands are the confidence bands, lowest first.
var bands = []float64{0, 0.5, 0.7, 0.8, 0.9, 0.95}

func bandName(i int) string {
	if i == len(bands)-1 {
		return fmt.Sprintf("%.2f-1.00", bands[i])
	}
	return fmt.Sprintf("%.2f-%.2f", bands[i], bands[i+1])
}

func band(c float64) int {
	i := sort.SearchFloat64s(bands, c)
	if i == len(bands) || bands[i] > c {
		i--
	}
	return max(i, 0)
}

// judgement reads a correct cell: y or n, case-insensitively, also yes/no and 1/0.
func judgement(s string) (ok, labelled, readable bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return false, false, true
	case "y", "yes", "1":
		return true, true, true
	case "n", "no", "0":
		return false, true, true
	}
	return false, false, false
}

// Agree compares a model's answers with a person's judgements and proposes thresholds.
func (c *Codebook) Agree(rows []EvalRow, o AgreeOptions) *Report {
	if o.AcceptTarget == 0 {
		o.AcceptTarget = 0.95
	}
	if o.EscalateTarget == 0 {
		o.EscalateTarget = 0.8
	}
	if o.MinSupport == 0 {
		o.MinSupport = 5
	}
	r := &Report{Rows: len(rows)}
	byQ := map[string]*Tally{}
	byF := map[string]*Tally{}
	byB := make([]Tally, len(bands))
	crit := map[string]*CriticalTally{}
	labelled := map[string][]EvalRow{}
	for i := range rows {
		row := &rows[i]
		ok, lab, readable := judgement(row.Correct)
		if !readable {
			r.Unreadable++
			continue
		}
		if !lab {
			continue
		}
		r.Labelled++
		r.Overall.add(ok)
		tally(byQ, row.Question).add(ok)
		tally(byF, folder(row.Path, o.Groups)).add(ok)
		byB[band(row.Confidence)].add(ok)
		labelled[row.Question] = append(labelled[row.Question], *row)
		if q := c.Question(row.Question); q != nil && q.Critical {
			ct := crit[q.Name]
			if ct == nil {
				ct = &CriticalTally{Question: q.Name}
				crit[q.Name] = ct
			}
			ct.Labelled++
			if !ok {
				ct.Misses++
				switch row.Answer {
				case "false":
					ct.MissedPositives++
				case "true":
					ct.FalseAlarms++
				}
			}
		}
	}
	for _, q := range c.Questions {
		if t := byQ[q.Name]; t != nil {
			r.Questions = append(r.Questions, Group{q.Name, *t})
		}
		if ct := crit[q.Name]; ct != nil {
			r.Critical = append(r.Critical, *ct)
		}
		r.Proposals = append(r.Proposals, c.propose(q, labelled[q.Name], o))
	}
	names := make([]string, 0, len(byF))
	for n := range byF {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		r.Folders = append(r.Folders, Group{n, *byF[n]})
	}
	for i, t := range byB {
		r.Bands = append(r.Bands, Group{bandName(i), t})
	}
	return r
}

func tally(m map[string]*Tally, k string) *Tally {
	if m[k] == nil {
		m[k] = &Tally{}
	}
	return m[k]
}

func folder(path string, groups []string) string {
	if len(groups) == 0 {
		if i := strings.IndexByte(path, '/'); i >= 0 {
			return path[:i+1]
		}
		return "./"
	}
	best := ""
	for _, g := range groups {
		if strings.HasPrefix(path, g) && len(g) > len(best) {
			best = g
		}
	}
	if best == "" {
		return "other"
	}
	return best
}

// propose finds the lowest confidence at which the answers at or above it reach each
// target, using only thresholds with at least MinSupport answers above them. For a
// critical yes/no question it also proposes Flag: the lowest probability of "true" the
// model gave any document the person judged to be a true positive, rounded down, so
// none in the sample would have been accepted unseen.
func (c *Codebook) propose(q Question, rows []EvalRow, o AgreeOptions) Proposal {
	p := Proposal{Question: q.Name, Gate: c.gate(q)}
	if len(rows) == 0 {
		p.Note = "no labelled answers; keeping the current gate"
		return p
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Confidence > rows[j].Confidence })
	accept, escalate := math.NaN(), math.NaN()
	support := 0
	agree := 0
	for i, row := range rows {
		if ok, _, _ := judgement(row.Correct); ok {
			agree++
		}
		n := i + 1
		// Only a threshold between distinct confidences can separate rows.
		if i+1 < len(rows) && rows[i+1].Confidence == row.Confidence {
			continue
		}
		if n < o.MinSupport {
			continue
		}
		rate := float64(agree) / float64(n)
		if rate >= o.AcceptTarget {
			accept, support = row.Confidence, n
		}
		if rate >= o.EscalateTarget {
			escalate = row.Confidence
		}
	}
	switch {
	case math.IsNaN(accept):
		p.Accept, p.Support = 1, 0
		p.Note = fmt.Sprintf("no threshold reaches %.0f%% agreement on %d or more answers; send every answer on", o.AcceptTarget*100, o.MinSupport)
	default:
		p.Accept, p.Support = floor2(accept), support
	}
	if math.IsNaN(escalate) {
		p.Escalate = p.Accept
	} else {
		p.Escalate = min(floor2(escalate), p.Accept)
	}
	if q.Critical && q.Type == Noul {
		lowest := math.NaN()
		for _, row := range rows {
			ok, _, _ := judgement(row.Correct)
			pTrue := row.Probability
			if row.Answer == "false" {
				pTrue = 1 - row.Probability
			}
			if positive := (row.Answer == "true") == ok; positive && (math.IsNaN(lowest) || pTrue < lowest) {
				lowest = pTrue
			}
		}
		if !math.IsNaN(lowest) {
			p.Flag = floor2(lowest)
		} else {
			p.Note = strings.TrimSpace(p.Note + " no labelled positives; keeping the current flag")
		}
	}
	return p
}

// floor2 rounds down to two decimals, ignoring float error just below a boundary.
func floor2(v float64) float64 { return math.Floor(v*100+1e-9) / 100 }

// WriteText writes the report as plain text: totals, question names and folder names.
func (r *Report) WriteText(w io.Writer) error {
	var b strings.Builder
	fmt.Fprintf(&b, "rows %d, labelled %d", r.Rows, r.Labelled)
	if r.Unreadable > 0 {
		fmt.Fprintf(&b, ", unreadable %d (correct must be y or n)", r.Unreadable)
	}
	fmt.Fprintf(&b, "\noverall agreement %s\n", pct(r.Overall))
	section := func(title string, gs []Group) {
		fmt.Fprintf(&b, "\n%s\n", title)
		for _, g := range gs {
			fmt.Fprintf(&b, "  %-24s %s\n", g.Name, pct(g.Tally))
		}
	}
	section("by question", r.Questions)
	section("by folder", r.Folders)
	section("by confidence band", r.Bands)
	fmt.Fprintf(&b, "\ncritical questions\n")
	if len(r.Critical) == 0 {
		fmt.Fprintf(&b, "  none labelled\n")
	}
	for _, c := range r.Critical {
		fmt.Fprintf(&b, "  %-24s labelled %d, misses %d (missed positives %d, false alarms %d)\n",
			c.Question, c.Labelled, c.Misses, c.MissedPositives, c.FalseAlarms)
	}
	fmt.Fprintf(&b, "\nproposed gates (accept / escalate / flag)\n")
	for _, p := range r.Proposals {
		fmt.Fprintf(&b, "  %-24s %.2f / %.2f / %.2f  support %d", p.Question, p.Accept, p.Escalate, p.Flag, p.Support)
		if p.Note != "" {
			fmt.Fprintf(&b, "  (%s)", p.Note)
		}
		b.WriteByte('\n')
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func pct(t Tally) string {
	if t.N == 0 {
		return "-"
	}
	return fmt.Sprintf("%5.1f%%  (%d/%d)", 100*t.Rate(), t.Agree, t.N)
}
