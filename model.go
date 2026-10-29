package codebook

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/troyronda/modelguard"
)

// Model is one decision model at one endpoint, checked against an allowlist,
// with its provenance recorded when it was opened.
type Model struct {
	// Name is the model name as given, for example "sprout" or "sprout:latest".
	Name string
	// Endpoint is where requests go.
	Endpoint Endpoint
	// Digest and ServerVersion are recorded for Ollama endpoints.
	Digest        string
	ServerVersion string
	// Warnings are notes for the caller to show, such as an untested server
	// version.
	Warnings []string
	// MaxChars caps how much of each document is sent; 0 sends it all. A document cut
	// short is marked Truncated in its result.
	MaxChars int

	ask asker
}

// ErrNotAllowed reports a model refused by the allowlist or the cloud rule.
var ErrNotAllowed = errors.New("model not allowed")

// CheckAllowed reports whether name may run. A name must appear on allow, and any name
// containing "cloud" is refused even if it is listed.
func CheckAllowed(name string, allow []string) error {
	if strings.Contains(strings.ToLower(name), "cloud") {
		return fmt.Errorf("%w: %q names a cloud model", ErrNotAllowed, name)
	}
	if !slices.Contains(allow, name) && !slices.Contains(allow, strings.TrimSuffix(name, ":latest")) &&
		!slices.Contains(allow, name+":latest") {
		return fmt.Errorf("%w: %q is not on the allowlist", ErrNotAllowed, name)
	}
	return nil
}

// Open checks name against the modelguard allowlist, builds a modelguard client from
// eg (every request codebook makes goes through it), fetches the API key if
// the endpoint names a command, and records provenance: the server version and
// model digest for an Ollama endpoint.
func Open(ctx context.Context, ep Endpoint, eg modelguard.Config, name string) (*Model, error) {
	ep = ep.Or(Endpoint{})
	if err := ep.validate(); err != nil {
		return nil, err
	}
	if err := CheckAllowed(name, eg.Models); err != nil {
		return nil, err
	}
	hc, err := modelguard.NewHTTPClient(eg)
	if err != nil {
		return nil, err
	}
	key, err := ep.apiKey(ctx)
	if err != nil {
		return nil, err
	}
	base := strings.TrimSuffix(ep.URL, "/")
	m := &Model{Name: name, Endpoint: ep, ask: newAsker(base, hc, key)}
	if ep.Kind == KindOllama {
		if m.ServerVersion, m.Digest, err = ollamaProvenance(ctx, hc, base, name); err != nil {
			return nil, err
		}
		if w := versionWarning(m.ServerVersion); w != "" {
			m.Warnings = append(m.Warnings, w)
		}
	}
	return m, nil
}
