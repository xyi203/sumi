// Package apiclient is the HTTP client of sumi's own API: the bill, category,
// stats and CSV-import operations, and the MCP server built on them.
//
// Both the CLI and the server's /mcp endpoint use it. Going through the API
// rather than the service layer keeps one implementation of every operation and
// lets the API's middleware stay the single place that authenticates and checks
// scopes; arch_test.go keeps this package off the server's internals.
package apiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Error carries the server's own message so a client never invents its own
// wording for a server-side rejection.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("http %d: %s", e.Status, e.Message)
}

// Client talks to the API with exactly one credential: an API key, or a bearer
// token when APIKey is empty.
type Client struct {
	BaseURL string
	APIKey  string
	Bearer  string
	HTTP    *http.Client
}

// Do performs one request and returns the raw body of a 2xx response.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body any) ([]byte, error) {
	endpoint := c.BaseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		payload = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, payload)
	if err != nil {
		return nil, err
	}
	switch {
	case c.APIKey != "":
		req.Header.Set("X-API-Key", c.APIKey)
	case c.Bearer != "":
		req.Header.Set("Authorization", "Bearer "+c.Bearer)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach sumi api at %s: %w", c.BaseURL, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &Error{Status: resp.StatusCode, Message: extractMessage(raw, resp.Status)}
	}
	return raw, nil
}

func extractMessage(raw []byte, fallback string) string {
	var envelope struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &envelope) == nil && envelope.Message != "" {
		return envelope.Message
	}
	if trimmed := strings.TrimSpace(string(raw)); trimmed != "" {
		return trimmed
	}
	return fallback
}

// ParseBillType maps the human-facing type names onto the API's 1/2 encoding.
func ParseBillType(raw string) (int16, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "expense", "1", "支出":
		return 1, nil
	case "income", "2", "收入":
		return 2, nil
	default:
		return 0, fmt.Errorf("invalid type %q: use expense or income", raw)
	}
}

// orExpense treats an omitted type as expense, the default everywhere.
func orExpense(rawType string) string {
	if strings.TrimSpace(rawType) == "" {
		return "expense"
	}
	return rawType
}

func setIfPresent(query url.Values, key, value string) {
	if value = strings.TrimSpace(value); value != "" {
		query.Set(key, value)
	}
}
