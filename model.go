package codebook

import (
	"context"
	"net/http"
	"strings"
)

// Model is one decision model at one endpoint, with its provenance recorded
// when it was opened.
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

// Open connects to the endpoint (the zero Endpoint is the Ollama server named
// by OLLAMA_HOST, else the local default), fetches the API key if the
// endpoint names a command, and records provenance: the server version and
// model digest for an Ollama endpoint.
func Open(ctx context.Context, ep Endpoint, name string) (*Model, error) {
	ep = ep.Or(Endpoint{})
	if err := ep.validate(); err != nil {
		return nil, err
	}
	key, err := ep.apiKey(ctx)
	if err != nil {
		return nil, err
	}
	hc := &http.Client{}
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
