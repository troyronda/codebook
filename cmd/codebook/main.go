// Command codebook applies a codebook to documents with local models and measures how
// far to trust the answers.
//
//	codebook run   -codebook FILE [FILE...]
//	codebook eval  -codebook FILE -out FILE.csv DIR
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
  codebook run   -codebook FILE [FILE...]
  codebook eval  -codebook FILE -out FILE.csv DIR
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
	codebook                   string
	endpoint, kind, keyCommand string
	maxChars                   int
	stderr                     io.Writer
}

func (m *modelFlags) register(fs *flag.FlagSet, maxChars int, stderr io.Writer) {
	m.stderr = stderr
	fs.StringVar(&m.codebook, "codebook", "", "codebook `file` (YAML); its model field names the decision model")
	fs.StringVar(&m.endpoint, "endpoint", "", "System One server base `URL` (default: the codebook's endpoint, else OLLAMA_HOST, else "+codebook.DefaultURL+")")
	fs.StringVar(&m.kind, "endpoint-kind", "", "`kind` of server: ollama (records version and digest) or other")
	fs.StringVar(&m.keyCommand, "api-key-command", "", "`command` that prints the endpoint's API key (split on spaces; no shell)")
	fs.IntVar(&m.maxChars, "max-chars", maxChars, "send at most `n` characters of each document (0: all)")
}

// open loads the codebook and opens the model it names.
func (m *modelFlags) open(ctx context.Context) (*codebook.Codebook, error) {
	if m.codebook == "" {
		return nil, errors.New("-codebook is required")
	}
	cb, err := codebook.Load(m.codebook)
	if err != nil {
		return nil, err
	}
	if cb.ModelName == "" {
		return nil, fmt.Errorf("%s names no model; add a model: line", m.codebook)
	}
	model, err := m.openModel(ctx, cb, cb.ModelName)
	if err != nil {
		return nil, err
	}
	cb.Model = model
	return cb, nil
}

func (m *modelFlags) openModel(ctx context.Context, cb *codebook.Codebook, name string) (*codebook.Model, error) {
	ep := codebook.Endpoint{URL: m.endpoint, Kind: m.kind, APIKeyCommand: strings.Fields(m.keyCommand)}.Or(cb.Endpoint)
	model, err := codebook.Open(ctx, ep, name)
	if err != nil {
		return nil, err
	}
	for _, w := range model.Warnings {
		fmt.Fprintln(m.stderr, "codebook: warning:", w)
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
