package systemone

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOrderedRoundTrip(t *testing.T) {
	var o Ordered[int]
	if err := json.Unmarshal([]byte(`{"zebra":1,"apple":2,"mango":3}`), &o); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for k := range o.All() {
		keys = append(keys, k)
	}
	if strings.Join(keys, ",") != "zebra,apple,mango" {
		t.Errorf("keys %v lost their order", keys)
	}
	b, _ := json.Marshal(o)
	if string(b) != `{"zebra":1,"apple":2,"mango":3}` {
		t.Errorf("marshal = %s", b)
	}
	o.Set("apple", 9)
	o.Set("kiwi", 4)
	b, _ = json.Marshal(&o)
	if string(b) != `{"zebra":1,"apple":9,"mango":3,"kiwi":4}` {
		t.Errorf("after Set = %s", b)
	}
	if err := json.Unmarshal([]byte(`[1]`), &o); err == nil {
		t.Error("array accepted as object")
	}
}

func TestDuration(t *testing.T) {
	b, _ := json.Marshal(Duration{5 * time.Minute})
	if string(b) != `"5m0s"` {
		t.Errorf("got %s", b)
	}
	b, _ = json.Marshal(Duration{-1})
	if string(b) != `-1` {
		t.Errorf("got %s", b)
	}
	var d Duration
	if err := json.Unmarshal([]byte(`30`), &d); err != nil || d.Duration != 30*time.Second {
		t.Errorf("got %v, %v", d, err)
	}
}

func serve(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return &Client{BaseURL: s.URL, HTTP: s.Client()}
}

func TestSystemOne(t *testing.T) {
	var got SystemOneRequest
	var auth string
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/systemone" {
			http.NotFound(w, r)
			return
		}
		auth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&got)
		io.WriteString(w, `{"model":"m","answers":{"b":{"type":"noul","noul":0.2},"a":{"type":"noul","noul":0.9}},"usage":{"input_tokens":12,"output_tokens":0}}`)
	})
	c.APIKey = "k"
	qs := &SystemOneQuestions{}
	qs.Set("b", SystemOneQuestion{Type: "noul", Instructions: json.RawMessage(`"B?"`), Criteria: json.RawMessage(`{"false":"no","true":"yes"}`)})
	qs.Set("a", SystemOneQuestion{Type: "noul", Instructions: json.RawMessage(`"A?"`)})
	resp, err := c.SystemOne(context.Background(), &SystemOneRequest{Model: "m", State: json.RawMessage(`"text"`), Questions: qs})
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer k" {
		t.Errorf("auth header %q", auth)
	}
	var sent []string
	for k := range got.Questions.All() {
		sent = append(sent, k)
	}
	if strings.Join(sent, ",") != "b,a" {
		t.Errorf("questions sent in order %v", sent)
	}
	var answers []string
	for k := range resp.Answers.All() {
		answers = append(answers, k)
	}
	if strings.Join(answers, ",") != "b,a" || resp.Usage.InputTokens != 12 || resp.Model != "m" {
		t.Errorf("response %+v answers %v", resp, answers)
	}
}

func TestErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
		want   string
	}{
		"status with error body": {404, `{"error":"model \"x\" not found"}`, `404: model "x" not found`},
		"200 with error body":    {200, `{"error":"runner crashed"}`, "200: runner crashed"},
		"status without body":    {500, ``, "500 Internal Server Error"},
		"no answers":             {200, `{"model":"m"}`, "no answers"},
		"not JSON":               {200, `<html>`, "decode response"},
	} {
		c := serve(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			io.WriteString(w, tc.body)
		})
		_, err := c.SystemOne(context.Background(), &SystemOneRequest{Model: "m", Questions: &SystemOneQuestions{}})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
		var se *Error
		if strings.Contains(name, "error body") && !errors.As(err, &se) {
			t.Errorf("%s: not an *Error", name)
		}
	}
}

func TestResponseCap(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"model":"`+strings.Repeat("x", MaxResponse)+`"}`)
	})
	_, err := c.SystemOne(context.Background(), &SystemOneRequest{Model: "m", Questions: &SystemOneQuestions{}})
	if err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("err = %v", err)
	}
}
