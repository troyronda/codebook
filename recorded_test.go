package codebook_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/troyronda/codebook"
)

// Recordings are real responses from a local Ollama server to the made-up
// notes in testdata/notes, asked with testdata/codebook.yaml. Re-record with:
//
//	go test -run TestRecorded -record -record-model <model>
//
// against a server on 127.0.0.1:11434. Add the server's version to
// TestedVersions when you do.
var (
	record      = flag.Bool("record", false, "record responses from the local Ollama server")
	recordModel = flag.String("record-model", "", "model to record with")
)

const liveURL = "http://127.0.0.1:11434"

// recording is one exchange.
type recording struct {
	Version  string          `json:"version"`
	Model    string          `json:"model"`
	Digest   string          `json:"digest"`
	Request  json.RawMessage `json:"request"`
	Response json.RawMessage `json:"response"`
}

var recordedNotes = []string{"inbox-note.md", "journal/beans.md", "journal/compost.md", "reading/tools-article.md", "chats/squash.md"}

func bodyKey(b []byte) string {
	var v any
	_ = json.Unmarshal(b, &v)
	canon, _ := json.Marshal(v) // map keys sorted, so formatting doesn't matter
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:])
}

func TestRecorded(t *testing.T) {
	dir := filepath.Join("testdata", "recorded")
	if *record {
		recordAll(t, dir)
		return
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*", "*.json"))
	if len(files) == 0 {
		t.Fatal("no recordings")
	}
	byKey := map[string]recording{}
	var version, model, digest string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var r recording
		if err := json.Unmarshal(b, &r); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		byKey[bodyKey(r.Request)] = r
		version, model, digest = r.Version, r.Model, r.Digest
	}

	// A replay server that answers only requests it has a recording for.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"version": version})
	})
	mux.HandleFunc("GET /api/tags", func(w http.ResponseWriter, r *http.Request) {
		name := model
		if !strings.Contains(name, ":") {
			name += ":latest" // as Ollama lists it
		}
		json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": name, "model": name, "digest": digest}}})
	})
	mux.HandleFunc("POST /v1/systemone", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rec, ok := byKey[bodyKey(b)]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"error":"no recording for this request"}`)
			return
		}
		w.Write(rec.Response)
	})
	s := httptest.NewServer(mux)
	defer s.Close()

	m := openAt(t, s.URL, model)
	if len(m.Warnings) != 0 {
		t.Errorf("recorded version %s is not in TestedVersions: %v", version, m.Warnings)
	}
	cb := load(t).WithModel(m)
	for _, note := range recordedNotes {
		r, err := cb.Apply(context.Background(), doc(t, note))
		if err != nil {
			t.Fatalf("%s: %v", note, err)
		}
		if len(r.Answers) != len(cb.Questions) || r.Digest != digest || r.ServerVersion != version || r.ReportedModel == "" {
			t.Errorf("%s: result %+v", note, r)
		}
		for _, a := range r.Answers {
			if a.Probability <= 0 || a.Probability > 1 || a.Confidence < 0 || a.Confidence > 1 || a.Label == "" {
				t.Errorf("%s: %s: implausible answer %+v", note, a.Question, a)
			}
			if (a.Noul != nil) != (a.Type == codebook.Noul) || (a.Choice != nil) != (a.Type == codebook.Choice) || (a.Score != nil) != (a.Type == codebook.Score) {
				t.Errorf("%s: %s: typed answer doesn't match type %s", note, a.Question, a.Type)
			}
		}
	}
}

// TestLive runs one note against the local server when it is reachable and
// CODEBOOK_LIVE_MODEL names a model on it.
func TestLive(t *testing.T) {
	model := os.Getenv("CODEBOOK_LIVE_MODEL")
	if testing.Short() || model == "" || !reachable() {
		t.Skip("set CODEBOOK_LIVE_MODEL and run an Ollama server on 127.0.0.1:11434")
	}
	m := openAt(t, liveURL, model)
	for _, w := range m.Warnings {
		t.Log("warning:", w)
	}
	r, err := load(t).WithModel(m).Apply(context.Background(), doc(t, "journal/beans.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Answers) != 4 {
		t.Errorf("got %d answers", len(r.Answers))
	}
}

func reachable() bool {
	c, err := net.DialTimeout("tcp", "127.0.0.1:11434", 300*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func openAt(t *testing.T, base, model string) *codebook.Model {
	t.Helper()
	m, err := codebook.Open(context.Background(), codebook.Endpoint{URL: base}, model)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func doc(t *testing.T, rel string) codebook.Document {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "notes", rel))
	if err != nil {
		t.Fatal(err)
	}
	return codebook.Document{Path: rel, Text: string(b)}
}

// recordAll proxies the live server and saves each System One exchange.
func recordAll(t *testing.T, dir string) {
	if *recordModel == "" || !reachable() {
		t.Fatal("-record needs -record-model and a server on 127.0.0.1:11434")
	}
	target, _ := url.Parse(liveURL)
	var version, digest string
	var exchanges []recording
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		in, _ := io.ReadAll(r.Body)
		out, err := http.NewRequestWithContext(r.Context(), r.Method, target.String()+r.URL.Path, bytes.NewReader(in))
		if err != nil {
			t.Error(err)
			return
		}
		out.Header = r.Header.Clone()
		resp, err := http.DefaultClient.Do(out) // loopback only: liveURL is fixed
		if err != nil {
			t.Error(err)
			return
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if r.URL.Path == "/v1/systemone" {
			exchanges = append(exchanges, recording{Request: in, Response: body})
		}
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
	}))
	defer proxy.Close()

	m := openAt(t, proxy.URL, *recordModel)
	version, digest = m.ServerVersion, m.Digest
	cb := load(t).WithModel(m)
	for _, note := range recordedNotes {
		if _, err := cb.Apply(context.Background(), doc(t, note)); err != nil {
			t.Fatalf("%s: %v", note, err)
		}
	}
	out := filepath.Join(dir, version)
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for i, ex := range exchanges {
		ex.Version, ex.Model, ex.Digest = version, *recordModel, digest
		b, _ := json.MarshalIndent(ex, "", "  ")
		name := filepath.Join(out, strconv.Itoa(i+1)+".json")
		if err := os.WriteFile(name, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("recorded %d exchanges with Ollama %s in %s", len(exchanges), version, out)
}
