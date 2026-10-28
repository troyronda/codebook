// Package systemone is a small client for the System One API (POST
// /v1/systemone), which asks a decision model typed questions about a state.
// It uses the standard library only; the caller supplies the *http.Client.
package systemone

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// MaxResponse is the largest response body read; larger responses are an error.
const MaxResponse = 8 << 20

// Client talks to one System One endpoint.
type Client struct {
	// BaseURL is the server's base URL, such as "http://127.0.0.1:11434".
	BaseURL string
	// HTTP sends the requests. It must not be nil.
	HTTP *http.Client
	// APIKey, if set, is sent as a bearer token.
	APIKey string
}

// Error is an error the server reported.
type Error struct {
	StatusCode int
	Message    string
}

func (e *Error) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("systemone: server returned %d %s", e.StatusCode, http.StatusText(e.StatusCode))
	}
	return fmt.Sprintf("systemone: server returned %d: %s", e.StatusCode, e.Message)
}

// SystemOne asks the questions in req.
func (c *Client) SystemOne(ctx context.Context, req *SystemOneRequest) (*SystemOneResponse, error) {
	var resp SystemOneResponse
	if err := c.Do(ctx, http.MethodPost, "/v1/systemone", req, &resp); err != nil {
		return nil, err
	}
	if resp.Answers == nil {
		return nil, errors.New("systemone: response has no answers")
	}
	return &resp, nil
}

// Do sends one JSON request (in is nil for none) and decodes the JSON
// response into out. A non-2xx status, or a body holding an "error" field, is
// returned as *Error.
func (c *Client) Do(ctx context.Context, method, path string, in, out any) error {
	if c.HTTP == nil {
		return errors.New("systemone: no HTTP client")
	}
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimSuffix(c.BaseURL, "/")+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponse+1))
	if err != nil {
		return err
	}
	if len(b) > MaxResponse {
		return fmt.Errorf("systemone: response larger than %d bytes", MaxResponse)
	}
	var e struct {
		Error *string `json:"error"`
	}
	_ = json.Unmarshal(b, &e)
	if resp.StatusCode < 200 || resp.StatusCode > 299 || e.Error != nil {
		msg := ""
		if e.Error != nil {
			msg = *e.Error
		}
		return &Error{StatusCode: resp.StatusCode, Message: msg}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("systemone: decode response: %w", err)
	}
	return nil
}
