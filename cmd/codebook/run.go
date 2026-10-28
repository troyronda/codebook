package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/troyronda/codebook"
)

func runCmd(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var m modelFlags
	m.register(fs, 0, stderr)
	logPath := fs.String("log", "", "append answers the gates didn't accept to this disagreement log `file` (JSON lines)")
	larger := fs.String("escalate-model", "", "ask this larger `model` about escalated answers, in a second pass after the first model finishes")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cb, err := m.open(ctx)
	if err != nil {
		return err
	}
	// Check the larger model before the first pass, so a refused one costs nothing.
	var big *codebook.Model
	if *larger != "" {
		if big, err = m.openModel(ctx, cb, *larger); err != nil {
			return err
		}
	}
	paths := fs.Args()
	if len(paths) == 0 {
		sc := bufio.NewScanner(stdin)
		for sc.Scan() {
			if p := strings.TrimSpace(sc.Text()); p != "" {
				paths = append(paths, p)
			}
		}
		if err := sc.Err(); err != nil {
			return err
		}
	}

	results := make([]*codebook.Result, len(paths))
	var log []codebook.Disagreement
	for i, p := range paths {
		doc, err := readDoc(p)
		if err != nil {
			return err
		}
		if results[i], err = cb.Apply(ctx, doc); err != nil {
			return err
		}
	}
	if big != nil {
		for i, p := range paths {
			doc, err := readDoc(p)
			if err != nil {
				return err
			}
			ds, err := cb.Escalate(ctx, big, doc, results[i], time.Now())
			if err != nil {
				return err
			}
			log = append(log, ds...)
		}
	}
	// Every answer not accepted goes in the log once: those the larger model disagreed
	// with are already there, with its answer.
	logged := map[[2]string]bool{}
	for _, d := range log {
		logged[[2]string{d.Path, d.Question}] = true
	}
	enc := json.NewEncoder(stdout)
	now := time.Now()
	for _, r := range results {
		for _, d := range cb.Disagreements(r, now) {
			if !logged[[2]string{d.Path, d.Question}] {
				log = append(log, d)
			}
		}
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	if *logPath != "" {
		if err := codebook.AppendLog(*logPath, log); err != nil {
			return err
		}
	}
	fmt.Fprintf(stderr, "run: %d documents, %d answers sent on\n", len(results), len(log))
	return nil
}

func readDoc(path string) (codebook.Document, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return codebook.Document{}, err
	}
	return codebook.Document{Path: path, Text: string(b)}, nil
}
