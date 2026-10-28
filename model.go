package codebook

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/ollama/ollama/api"
)

// DefaultHost is the local Ollama server.
const DefaultHost = "http://127.0.0.1:11434"

// Model is one decision model on a local Ollama server, checked against an allowlist,
// with the server version and model digest recorded when it was opened.
type Model struct {
	// Name is the model name as given, for example "clef" or "clef:latest".
	Name string
	// Digest is the model's digest on the server when it was opened.
	Digest string
	// ServerVersion is the Ollama server's version when the model was opened.
	ServerVersion string
	// MaxChars caps how much of each document is sent; 0 sends it all. A document cut
	// short is marked Truncated in its result.
	MaxChars int

	client *api.Client
}

// NewClient returns an Ollama client for host. It refuses any host that isn't a
// loopback address, so documents never leave the machine.
func NewClient(host string) (*api.Client, error) {
	u, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("ollama host: %w", err)
	}
	h := u.Hostname()
	if ip := net.ParseIP(h); h != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("ollama host %q is not a loopback address; refusing to send documents off this machine", h)
	}
	return api.NewClient(u, http.DefaultClient), nil
}

// ErrNotAllowed reports a model refused by the allowlist or the cloud rule.
var ErrNotAllowed = errors.New("model not allowed")

// CheckAllowed reports whether name may run. A name must appear on allow, and any name
// containing "cloud" is refused even if it is listed.
func CheckAllowed(name string, allow []string) error {
	if strings.Contains(strings.ToLower(name), "cloud") {
		return fmt.Errorf("%w: %q names a cloud model", ErrNotAllowed, name)
	}
	if !slices.Contains(allow, name) && !slices.Contains(allow, strings.TrimSuffix(name, ":latest")) {
		return fmt.Errorf("%w: %q is not on the allowlist", ErrNotAllowed, name)
	}
	return nil
}

// OpenModel checks name against allow, then records the server version and the model's
// digest. It refuses a model the server would forward to a remote host.
func OpenModel(ctx context.Context, client *api.Client, name string, allow []string) (*Model, error) {
	if err := CheckAllowed(name, allow); err != nil {
		return nil, err
	}
	version, err := client.Version(ctx)
	if err != nil {
		return nil, fmt.Errorf("ollama version: %w", err)
	}
	list, err := client.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("ollama list: %w", err)
	}
	want := name
	if !strings.Contains(want, ":") {
		want += ":latest"
	}
	for _, m := range list.Models {
		if m.Name != want && m.Model != want {
			continue
		}
		if m.RemoteHost != "" || m.RemoteModel != "" {
			return nil, fmt.Errorf("%w: %q runs on a remote host", ErrNotAllowed, name)
		}
		if m.Digest == "" {
			return nil, fmt.Errorf("model %q has no digest", name)
		}
		return &Model{Name: name, Digest: m.Digest, ServerVersion: version, client: client}, nil
	}
	return nil, fmt.Errorf("model %q is not on the Ollama server; pull it first", name)
}
