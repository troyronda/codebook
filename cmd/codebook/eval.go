package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/troyronda/codebook"
	"github.com/troyronda/codebook/internal/sample"
)

// evalMeta is written beside the CSV, so the run can be traced: which model, which
// server, which codebook. It holds no document text.
type evalMeta struct {
	Time          time.Time `json:"time"`
	Codebook      string    `json:"codebook_hash"`
	Model         string    `json:"model"`
	Digest        string    `json:"digest"`
	ServerVersion string    `json:"server_version"`
	Seed          uint64    `json:"seed"`
	Files         int       `json:"files"`
	Truncated     int       `json:"truncated"`
	MaxChars      int       `json:"max_chars"`
	SkippedCopies int       `json:"skipped_conflict_copies"`
	SkippedRecent int       `json:"skipped_recent"`
}

func evalCmd(ctx context.Context, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var m modelFlags
	m.register(fs, 20000)
	n := fs.Int("n", 50, "number of files to sample")
	seed := fs.Uint64("seed", 1, "sample `seed`; the same seed and files give the same sample")
	skip := fs.String("skip", "attachments", "comma-separated folder `names` to leave out (\".\" leaves out files at the top level); hidden folders are always left out")
	out := fs.String("out", "", "write the CSV to this `file`; it must not exist")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 || *out == "" {
		return errors.New("eval needs -out FILE.csv and one folder")
	}
	root := fs.Arg(0)
	cb, err := m.open(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	files, sk, err := sample.Files(root, sample.Options{N: *n, Seed: *seed, Skip: splitList(*skip), Now: now})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	w, err := codebook.NewEvalWriter(f)
	if err != nil {
		return err
	}
	meta := evalMeta{Time: now.UTC(), Codebook: cb.Hash, Model: cb.Model.Name, Digest: cb.Model.Digest,
		ServerVersion: cb.Model.ServerVersion, Seed: *seed, Files: len(files), MaxChars: m.maxChars,
		SkippedCopies: sk.Conflict, SkippedRecent: sk.Recent}
	for i, rel := range files {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		r, err := cb.Apply(ctx, codebook.Document{Path: rel, Text: string(b)})
		if err != nil {
			return err
		}
		if r.Truncated {
			meta.Truncated++
		}
		if err := w.Write(codebook.EvalRows(r)); err != nil {
			return err
		}
		fmt.Fprintf(stderr, "eval: %d/%d\n", i+1, len(files))
	}
	if err := f.Close(); err != nil {
		return err
	}
	mb, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out+".meta.json", append(mb, '\n'), 0o600); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "eval: %d files, %d truncated, skipped %d conflict copies and %d recently changed; wrote %s\n",
		len(files), meta.Truncated, sk.Conflict, sk.Recent, *out)
	return nil
}
