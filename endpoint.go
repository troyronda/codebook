package codebook

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

// DefaultURL is a local Ollama server.
const DefaultURL = "http://127.0.0.1:11434"

// Endpoint kinds.
const (
	// KindOllama is an Ollama server: codebook records its version and each
	// model's digest.
	KindOllama = "ollama"
	// KindOther is any other System One server: codebook records the endpoint
	// and the model name each response reports.
	KindOther = "other"
)

// Endpoint is where System One requests go. A codebook file may name one; a
// caller may pass one; the zero value is the local Ollama server.
type Endpoint struct {
	// URL is the server's base URL (DefaultURL if empty).
	URL string `yaml:"url" json:"url"`
	// Kind is KindOllama (the default) or KindOther.
	Kind string `yaml:"kind" json:"kind"`
	// APIKeyCommand, if set, is a command (program and arguments, no shell)
	// that prints the API key on stdout. The key itself never appears in a
	// codebook file or a config.
	APIKeyCommand []string `yaml:"api_key_command" json:"api_key_command"`
}

// Or returns e with empty fields taken from fallback, then from the defaults.
func (e Endpoint) Or(fallback Endpoint) Endpoint {
	if e.URL == "" {
		e.URL = fallback.URL
	}
	if e.Kind == "" {
		e.Kind = fallback.Kind
	}
	if len(e.APIKeyCommand) == 0 {
		e.APIKeyCommand = fallback.APIKeyCommand
	}
	if e.URL == "" {
		e.URL = DefaultURL
	}
	if e.Kind == "" {
		e.Kind = KindOllama
	}
	return e
}

func (e Endpoint) validate() error {
	if e.URL == "" {
		return nil
	}
	u, err := url.Parse(e.URL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("endpoint url %q is not an http or https URL", e.URL)
	}
	if e.Kind != "" && e.Kind != KindOllama && e.Kind != KindOther {
		return fmt.Errorf("endpoint kind %q is not %q or %q", e.Kind, KindOllama, KindOther)
	}
	return nil
}

// apiKey runs APIKeyCommand. Errors never include the command's output.
func (e Endpoint) apiKey(ctx context.Context) (string, error) {
	if len(e.APIKeyCommand) == 0 {
		return "", nil
	}
	var out, errOut bytes.Buffer
	cmd := exec.CommandContext(ctx, e.APIKeyCommand[0], e.APIKeyCommand[1:]...)
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("api key command %q failed: %w", e.APIKeyCommand[0], err)
	}
	key := strings.TrimSpace(out.String())
	if key == "" {
		return "", errors.New("api key command printed nothing")
	}
	return key, nil
}
