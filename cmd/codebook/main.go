// Command codebook applies a codebook to documents with local models and measures how
// far to trust the answers.
//
//	codebook run   -codebook FILE -model NAME -allow NAMES [FILE...]
//	codebook eval  -codebook FILE -model NAME -allow NAMES -out FILE.csv DIR
//	codebook agree -codebook FILE [-group PREFIXES] FILE.csv
//
// run applies the codebook to the files named as arguments, or to the paths read one
// per line from stdin, and writes one JSON result per line. eval applies it to a spread
// sample of a folder's Markdown files and writes a CSV for a person to label. agree
// reads that labelled CSV and prints agreement totals and proposed gates.
//
// Output names files and reports labels, probabilities and counts. It never includes
// document text.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/troyronda/codebook"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, "codebook:", err)
		}
		os.Exit(1)
	}
}

const usage = `usage:
  codebook run   -codebook FILE -model NAME -allow NAMES [FILE...]
  codebook eval  -codebook FILE -model NAME -allow NAMES -out FILE.csv DIR
  codebook agree -codebook FILE [-group PREFIXES] FILE.csv
Run "codebook <command> -h" for a command's flags.`

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return flag.ErrHelp
	}
	switch args[0] {
	case "run":
		return runCmd(ctx, args[1:], stdin, stdout, stderr)
	case "eval":
		return evalCmd(ctx, args[1:], stderr)
	case "agree":
		return agreeCmd(args[1:], stdout, stderr)
	case "-h", "-help", "--help", "help":
		fmt.Fprintln(stdout, usage)
		return nil
	}
	fmt.Fprintln(stderr, usage)
	return fmt.Errorf("unknown command %q", args[0])
}

// modelFlags are the flags shared by run and eval.
type modelFlags struct {
	codebook, model, allow, host string
	maxChars                     int
}

func (m *modelFlags) register(fs *flag.FlagSet, maxChars int) {
	fs.StringVar(&m.codebook, "codebook", "", "codebook `file` (YAML)")
	fs.StringVar(&m.model, "model", "", "decision model `name` on the local Ollama server")
	fs.StringVar(&m.allow, "allow", "", "comma-separated model `names` allowed to run; names containing \"cloud\" are always refused")
	fs.StringVar(&m.host, "host", codebook.DefaultHost, "Ollama server `URL`; must be a loopback address")
	fs.IntVar(&m.maxChars, "max-chars", maxChars, "send at most `n` characters of each document (0: all)")
}

// open loads the codebook and opens its model.
func (m *modelFlags) open(ctx context.Context) (*codebook.Codebook, error) {
	if m.codebook == "" || m.model == "" || m.allow == "" {
		return nil, errors.New("-codebook, -model and -allow are required")
	}
	cb, err := codebook.Load(m.codebook)
	if err != nil {
		return nil, err
	}
	model, err := m.openModel(ctx, m.model)
	if err != nil {
		return nil, err
	}
	cb.Model = model
	return cb, nil
}

func (m *modelFlags) openModel(ctx context.Context, name string) (*codebook.Model, error) {
	client, err := codebook.NewClient(m.host)
	if err != nil {
		return nil, err
	}
	model, err := codebook.OpenModel(ctx, client, name, splitList(m.allow))
	if err != nil {
		return nil, err
	}
	model.MaxChars = m.maxChars
	return model, nil
}

func splitList(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
