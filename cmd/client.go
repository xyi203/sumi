package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"sumi/internal/apiclient"
)

const (
	envAPIURL      = "SUMI_API_URL"
	envAPIKey      = "SUMI_API_KEY"
	envAccessToken = "SUMI_ACCESS_TOKEN"
	defaultAPIURL  = "http://localhost:3000"
	requestTimeout = 15 * time.Second
)

// apiClient is the client for the account commands under `sumi auth`, which use
// a bearer token: API keys cannot manage sessions or other keys. The data
// commands use a plain *apiclient.Client from newAPIClient.
type apiClient struct {
	*apiclient.Client

	// Set for bearer clients so an expired access token can be refreshed and the
	// refreshed pair written back to disk.
	cfg     *localConfig
	cfgPath string
}

func resolveBaseURL(cfg *localConfig) string {
	if fromEnv := strings.TrimSpace(os.Getenv(envAPIURL)); fromEnv != "" {
		return strings.TrimRight(fromEnv, "/")
	}
	if cfg != nil && strings.TrimSpace(cfg.APIURL) != "" {
		return strings.TrimRight(strings.TrimSpace(cfg.APIURL), "/")
	}
	return defaultAPIURL
}

// newAPIClient builds the API-key client used by the data commands and `sumi mcp`.
// The environment wins over the stored config so a container can inject credentials.
func newAPIClient() (*apiclient.Client, error) {
	cfg, _, err := loadConfig()
	if err != nil {
		return nil, err
	}

	apiKey := strings.TrimSpace(os.Getenv(envAPIKey))
	if apiKey == "" {
		apiKey = strings.TrimSpace(cfg.APIKey)
	}
	if apiKey == "" {
		return nil, fmt.Errorf("no API key: set %s, or run `sumi auth login` then `sumi auth key create`", envAPIKey)
	}

	return &apiclient.Client{
		BaseURL: resolveBaseURL(cfg),
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: requestTimeout},
	}, nil
}

// newAuthClient builds the bearer client used by `sumi auth`; API keys cannot
// manage sessions or other keys.
func newAuthClient() (*apiClient, error) {
	cfg, path, err := loadConfig()
	if err != nil {
		return nil, err
	}

	token := strings.TrimSpace(os.Getenv(envAccessToken))
	if token == "" {
		token = strings.TrimSpace(cfg.AccessToken)
	}
	if token == "" {
		return nil, fmt.Errorf("not logged in: run `sumi auth login --email <email> --password-stdin`")
	}

	return &apiClient{
		Client: &apiclient.Client{
			BaseURL: resolveBaseURL(cfg),
			Bearer:  token,
			HTTP:    &http.Client{Timeout: requestTimeout},
		},
		cfg:     cfg,
		cfgPath: path,
	}, nil
}

// newPublicClient builds a client for the endpoints that take no credential
// (register, login, refresh).
func newPublicClient() (*apiClient, error) {
	cfg, _, err := loadConfig()
	if err != nil {
		return nil, err
	}
	return &apiClient{Client: &apiclient.Client{
		BaseURL: resolveBaseURL(cfg),
		HTTP:    &http.Client{Timeout: requestTimeout},
	}}, nil
}

// do performs one request, transparently refreshing an expired access token once.
func (c *apiClient) do(method, path string, query url.Values, body any) ([]byte, error) {
	ctx := context.Background()
	raw, err := c.Do(ctx, method, path, query, body)
	if err == nil {
		return raw, nil
	}

	failure, ok := err.(*apiclient.Error)
	if !ok || failure.Status != http.StatusUnauthorized || c.Bearer == "" {
		return nil, err
	}
	if c.cfg == nil || strings.TrimSpace(c.cfg.RefreshToken) == "" {
		return nil, err
	}
	if refreshErr := c.refreshSession(); refreshErr != nil {
		// Report the original 401: the refresh failing means the session is gone.
		return nil, fmt.Errorf("%w (session refresh also failed: %v)", err, refreshErr)
	}
	return c.Do(ctx, method, path, query, body)
}

// refreshSession exchanges the stored refresh token for a new pair and persists
// it, so short-lived access tokens do not force a re-login between commands.
func (c *apiClient) refreshSession() error {
	payload := map[string]string{"refresh_token": c.cfg.RefreshToken}
	fresh := &apiclient.Client{BaseURL: c.BaseURL, HTTP: c.HTTP}

	raw, err := fresh.Do(context.Background(), "POST", "/api/auth/refresh", nil, payload)
	if err != nil {
		return err
	}

	var session authResponse
	if err := json.Unmarshal(raw, &session); err != nil {
		return err
	}
	if strings.TrimSpace(session.AccessToken) == "" {
		return fmt.Errorf("refresh returned no access token")
	}

	c.Bearer = session.AccessToken
	c.cfg.AccessToken = session.AccessToken
	if strings.TrimSpace(session.RefreshToken) != "" {
		c.cfg.RefreshToken = session.RefreshToken
	}
	if c.cfgPath != "" {
		return saveConfig(c.cfg, c.cfgPath)
	}
	return nil
}

// emitJSON writes a machine-readable result to stdout. Every successful command
// prints exactly one JSON document so callers can pipe straight into a parser.
func emitJSON(raw []byte) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		trimmed = []byte("{}")
	}
	if _, err := os.Stdout.Write(trimmed); err != nil {
		return err
	}
	_, err := os.Stdout.Write([]byte("\n"))
	return err
}

func emitValue(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return emitJSON(encoded)
}
