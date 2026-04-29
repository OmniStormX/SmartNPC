// Package bridge contains the minimal HTTP transport used by the M2 mail
// experiment to talk to the SMAPI mod.
//
// This is intentionally small and disposable: M3 will replace it with a real
// WebSocket bridge. Keep this file < 100 lines so the rewrite is painless.
package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultModURL is the address the SMAPI mod listens on by default.
const DefaultModURL = "http://127.0.0.1:18745"

// Client is a tiny JSON-over-HTTP client for the experimental mod endpoint.
type Client struct {
	baseURL string
	hc      *http.Client
}

// NewClient returns a Client targeting the given mod base URL (e.g.
// "http://127.0.0.1:18745"). Pass an empty string to use DefaultModURL.
func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = DefaultModURL
	}
	return &Client{
		baseURL: baseURL,
		hc:      &http.Client{Timeout: 5 * time.Second},
	}
}

// PostJSON sends `in` as JSON to baseURL + path, decodes the JSON response into
// `out`. Returns an error for any non-2xx status, including a snippet of the
// response body to aid debugging.
func (c *Client) PostJSON(ctx context.Context, path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("post %s: %w", path, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("mod returned %d: %s", resp.StatusCode, string(respBody))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode response: %w (body=%s)", err, string(respBody))
	}
	return nil
}
