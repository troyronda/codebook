package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"

	"github.com/troyronda/codebook"
)

func agreeCmd(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("agree", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cbPath := fs.String("codebook", "", "codebook `file` the eval used (for critical questions and current gates)")
	groups := fs.String("group", "", "comma-separated path `prefixes` to report agreement for; others count as \"other\" (default: top-level folders)")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	accept := fs.Float64("accept-target", 0.95, "agreement an accepted answer should reach")
	escalate := fs.Float64("escalate-target", 0.8, "agreement worth a larger model's opinion rather than a person's")
	support := fs.Int("min-support", 5, "fewest labelled answers a proposed threshold rests on")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 || *cbPath == "" {
		return errors.New("agree needs -codebook FILE and one labelled CSV")
	}
	cb, err := codebook.Load(*cbPath)
	if err != nil {
		return err
	}
	f, err := os.Open(fs.Arg(0))
	if err != nil {
		return err
	}
	defer f.Close()
	rows, err := codebook.ReadEval(f)
	if err != nil {
		return err
	}
	r := cb.Agree(rows, codebook.AgreeOptions{Groups: splitList(*groups), AcceptTarget: *accept, EscalateTarget: *escalate, MinSupport: *support})
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	return r.WriteText(stdout)
}
