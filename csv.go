package codebook

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// EvalHeader is the header of an eval file. A person fills the correct column with
// y or n: is the model's answer right?
var EvalHeader = []string{"path", "question", "answer", "probability", "confidence", "truncated", "correct"}

// EvalRow is one answer in an eval file.
type EvalRow struct {
	Path        string
	Question    string
	Answer      string
	Probability float64
	Confidence  float64
	Truncated   bool
	// Correct is the person's judgement: "" until labelled, then y or n.
	Correct string
}

// EvalRows flattens a result into eval rows, one per question, with Correct empty.
func EvalRows(r *Result) []EvalRow {
	rows := make([]EvalRow, len(r.Answers))
	for i, a := range r.Answers {
		rows[i] = EvalRow{r.Path, a.Question, a.Label, a.Probability, a.Confidence, r.Truncated, ""}
	}
	return rows
}

// EvalWriter writes an eval file.
type EvalWriter struct{ w *csv.Writer }

// NewEvalWriter writes the header and returns a writer for rows.
func NewEvalWriter(w io.Writer) (*EvalWriter, error) {
	cw := csv.NewWriter(w)
	if err := cw.Write(EvalHeader); err != nil {
		return nil, err
	}
	return &EvalWriter{cw}, nil
}

// Write writes rows.
func (e *EvalWriter) Write(rows []EvalRow) error {
	for _, r := range rows {
		rec := []string{r.Path, r.Question, r.Answer,
			strconv.FormatFloat(r.Probability, 'f', 4, 64),
			strconv.FormatFloat(r.Confidence, 'f', 4, 64),
			strconv.FormatBool(r.Truncated), r.Correct}
		if err := e.w.Write(rec); err != nil {
			return err
		}
	}
	e.w.Flush()
	return e.w.Error()
}

// ReadEval reads an eval file.
func ReadEval(r io.Reader) ([]EvalRow, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = len(EvalHeader)
	head, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("eval header: %w", err)
	}
	for i, h := range EvalHeader {
		if head[i] != h {
			return nil, fmt.Errorf("eval header column %d is %q, want %q", i+1, head[i], h)
		}
	}
	var rows []EvalRow
	for line := 2; ; line++ {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return rows, nil
		}
		if err != nil {
			return nil, err
		}
		p, err1 := strconv.ParseFloat(rec[3], 64)
		c, err2 := strconv.ParseFloat(rec[4], 64)
		t, err3 := strconv.ParseBool(rec[5])
		if err1 != nil || err2 != nil || err3 != nil {
			// Name the line, never the cell: a mislabelled file could hold any text.
			return nil, fmt.Errorf("eval line %d: probability, confidence or truncated is malformed", line)
		}
		rows = append(rows, EvalRow{rec[0], rec[1], rec[2], p, c, t, rec[6]})
	}
}
