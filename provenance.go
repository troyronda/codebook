package codebook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
)

// TestedVersions are the Ollama server versions codebook's recorded test
// responses came from. Opening a model on another version adds a warning.
var TestedVersions = []string{"0.40.11"}

// ollamaProvenance returns the server version and the model's digest.
func ollamaProvenance(ctx context.Context, hc *http.Client, base, name string) (version, digest string, err error) {
	var v struct {
		Version string `json:"version"`
	}
	if err := getJSON(ctx, hc, base+"/api/version", &v); err != nil {
		return "", "", fmt.Errorf("ollama version: %w", err)
	}
	var tags struct {
		Models []struct {
			Name   string `json:"name"`
			Model  string `json:"model"`
			Digest string `json:"digest"`
		} `json:"models"`
	}
	if err := getJSON(ctx, hc, base+"/api/tags", &tags); err != nil {
		return "", "", fmt.Errorf("ollama models: %w", err)
	}
	want := name
	if !strings.Contains(want, ":") {
		want += ":latest"
	}
	for _, m := range tags.Models {
		if m.Name != want && m.Model != want {
			continue
		}
		if m.Digest == "" {
			return "", "", fmt.Errorf("model %q has no digest", name)
		}
		return v.Version, m.Digest, nil
	}
	return "", "", fmt.Errorf("model %q is not on the Ollama server; pull it first", name)
}

func versionWarning(version string) string {
	if slices.Contains(TestedVersions, version) {
		return ""
	}
	return fmt.Sprintf("Ollama %s is not a version codebook's tests were recorded against (%s); check the answers decode as expected",
		version, strings.Join(TestedVersions, ", "))
}

func getJSON(ctx context.Context, hc *http.Client, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}
	return json.Unmarshal(b, out)
}
