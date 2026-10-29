// Command codebook applies a codebook to documents with local models and measures how
// far to trust the answers.
//
//	codebook run   -codebook FILE -model NAME (-allow NAMES | -modelguard FILE) [FILE...]
//	codebook eval  -codebook FILE -model NAME (-allow NAMES | -modelguard FILE) -out FILE.csv DIR
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
	"net/netip"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/troyronda/codebook"
	"github.com/troyronda/modelguard"
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
  codebook run   -codebook FILE -model NAME (-allow NAMES | -modelguard FILE) [FILE...]
  codebook eval  -codebook FILE -model NAME (-allow NAMES | -modelguard FILE) -out FILE.csv DIR
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
	codebook, model, allow     string
	endpoint, kind, keyCommand string
	guardPath, guardLog        string
	maxChars                   int
	stderr                     io.Writer
}

func (m *modelFlags) register(fs *flag.FlagSet, maxChars int, stderr io.Writer) {
	m.stderr = stderr
	fs.StringVar(&m.codebook, "codebook", "", "codebook `file` (YAML)")
	fs.StringVar(&m.model, "model", "", "decision model `name`")
	fs.StringVar(&m.allow, "allow", "", "comma-separated model `names` allowed to run, for the built-in loopback-only modelguard config; names containing \"cloud\" are always refused")
	fs.StringVar(&m.endpoint, "endpoint", "", "System One server base `URL` (default: the codebook's endpoint, else "+codebook.DefaultURL+")")
	fs.StringVar(&m.kind, "endpoint-kind", "", "`kind` of server: ollama (records version and digest) or other")
	fs.StringVar(&m.keyCommand, "api-key-command", "", "`command` that prints the endpoint's API key (split on spaces; no shell)")
	fs.StringVar(&m.guardPath, "modelguard", "", "modelguard config `file` (JSON) naming allowed hosts, address ranges, ports, models and the log; replaces -allow")
	fs.StringVar(&m.guardLog, "modelguard-log", "", "modelguard log `file` for the built-in config (default: codebook/modelguard.jsonl in the user cache folder)")
	fs.IntVar(&m.maxChars, "max-chars", maxChars, "send at most `n` characters of each document (0: all)")
}

// open loads the codebook and opens its model.
func (m *modelFlags) open(ctx context.Context) (*codebook.Codebook, error) {
	if m.codebook == "" || m.model == "" {
		return nil, errors.New("-codebook and -model are required")
	}
	cb, err := codebook.Load(m.codebook)
	if err != nil {
		return nil, err
	}
	model, err := m.openModel(ctx, cb, m.model)
	if err != nil {
		return nil, err
	}
	cb.Model = model
	return cb, nil
}

func (m *modelFlags) openModel(ctx context.Context, cb *codebook.Codebook, name string) (*codebook.Model, error) {
	ep := codebook.Endpoint{URL: m.endpoint, Kind: m.kind, APIKeyCommand: strings.Fields(m.keyCommand)}.Or(cb.Endpoint)
	eg, err := m.guardConfig(ep)
	if err != nil {
		return nil, err
	}
	model, err := codebook.Open(ctx, ep, eg, name)
	if err != nil {
		return nil, err
	}
	for _, w := range model.Warnings {
		fmt.Fprintln(m.stderr, "codebook: warning:", w)
	}
	model.MaxChars = m.maxChars
	return model, nil
}

// guardConfig loads -modelguard, or builds a loopback-only config for the
// endpoint's port and the -allow models.
func (m *modelFlags) guardConfig(ep codebook.Endpoint) (modelguard.Config, error) {
	if m.guardPath != "" {
		if m.allow != "" {
			return modelguard.Config{}, errors.New("use -modelguard or -allow, not both: the modelguard config's models are the allowlist")
		}
		cfg, err := modelguard.Load(m.guardPath)
		if err != nil {
			return cfg, err
		}
		if m.guardLog != "" {
			cfg.Log = m.guardLog
		}
		return cfg, nil
	}
	if m.allow == "" {
		return modelguard.Config{}, errors.New("-allow (or a -modelguard config) is required")
	}
	u, err := url.Parse(ep.URL)
	if err != nil {
		return modelguard.Config{}, err
	}
	if h := u.Hostname(); h != "localhost" {
		if ip, err := netip.ParseAddr(h); err != nil || !ip.Unmap().IsLoopback() {
			return modelguard.Config{}, fmt.Errorf("endpoint %s is not on this machine; pass a -modelguard config that allows it", ep.URL)
		}
	}
	port := u.Port()
	if port == "" {
		port = map[string]string{"http": "80", "https": "443"}[u.Scheme]
	}
	p, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return modelguard.Config{}, fmt.Errorf("endpoint port %q: %w", port, err)
	}
	log := m.guardLog
	if log == "" {
		dir, err := os.UserCacheDir()
		if err != nil {
			return modelguard.Config{}, fmt.Errorf("no -modelguard-log and no user cache folder: %w", err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "codebook"), 0o700); err != nil {
			return modelguard.Config{}, err
		}
		log = filepath.Join(dir, "codebook", "modelguard.jsonl")
	}
	return modelguard.Config{Ports: []uint16{uint16(p)}, Models: splitList(m.allow), Log: log}, nil
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
