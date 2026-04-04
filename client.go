package bestiary

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// defaultBaseURL is the canonical models.dev API endpoint.
const defaultBaseURL = "https://models.dev/api.json"

// defaultTimeout is applied to the underlying http.Client when no
// WithTimeout option is provided.
const defaultTimeout = 30 * time.Second

// defaultRetries is the number of retry attempts made after the first failure
// when no WithRetries option is provided.
const defaultRetries = 2

// Client fetches model metadata from the models.dev API.
// Use NewClient to construct a Client with sensible defaults.
type Client struct {
	httpClient *http.Client
	baseURL    string
	retries    int
}

// ClientOption is a functional option for configuring a Client.
type ClientOption func(*Client)

// WithTimeout sets the HTTP request timeout. The default is 30 seconds.
func WithTimeout(d time.Duration) ClientOption {
	return func(c *Client) {
		c.httpClient.Timeout = d
	}
}

// WithRetries sets the number of retry attempts after an initial failure.
// For example, WithRetries(2) means up to 3 total attempts.
// The default is 2 retries.
func WithRetries(n int) ClientOption {
	return func(c *Client) {
		c.retries = n
	}
}

// WithBaseURL overrides the API endpoint. The default is
// "https://models.dev/api.json".
func WithBaseURL(url string) ClientOption {
	return func(c *Client) {
		c.baseURL = url
	}
}

// NewClient creates a Client with the given options applied on top of defaults.
// Defaults: 30 s timeout, 2 retries, "https://models.dev/api.json".
func NewClient(opts ...ClientOption) *Client {
	c := &Client{
		httpClient: &http.Client{Timeout: defaultTimeout},
		baseURL:    defaultBaseURL,
		retries:    defaultRetries,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// FetchModels retrieves all model metadata from the models.dev API.
// It retries on transient failures (non-2xx responses or network errors) up to
// c.retries additional times with exponential backoff, honouring ctx between
// attempts.
//
// On final failure it returns *ErrAPIUnavailable so callers can use errors.As
// to inspect structured fields.
//
// LastSynced on each returned ModelInfo is left empty; the caller must set it
// when persisting the results.
func (c *Client) FetchModels(ctx context.Context) ([]ModelInfo, error) {
	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		// Honour context cancellation between retry waits.
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}

		models, err := c.fetchOnce(ctx)
		if err == nil {
			return models, nil
		}
		lastErr = err
	}
	return nil, &ErrAPIUnavailable{
		URL:      c.baseURL,
		Attempts: c.retries + 1,
		Cause:    lastErr,
	}
}

// fetchOnce performs a single HTTP GET and returns the parsed model list.
// It enforces a 10 MB body limit and returns a descriptive error on any
// non-200 status, read failure, or JSON decode failure.
func (c *Client) fetchOnce(ctx context.Context) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL, nil)
	if err != nil {
		return nil, fmt.Errorf("bestiary: Client.fetchOnce: create request for %s: %w", c.baseURL, err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bestiary: Client.fetchOnce: HTTP GET %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"bestiary: Client.fetchOnce: unexpected HTTP status %d from %s; expected 200 OK",
			resp.StatusCode, c.baseURL,
		)
	}

	const maxBodyBytes = 10 * 1024 * 1024 // 10 MB
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("bestiary: Client.fetchOnce: read response body from %s: %w", c.baseURL, err)
	}

	var apiResp wireResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf(
			"bestiary: Client.fetchOnce: decode JSON from %s: %w"+
				" (body may have been truncated at 10 MB limit)",
			c.baseURL, err,
		)
	}

	var models []ModelInfo
	for providerSlug, prov := range apiResp {
		for _, wm := range prov.Models {
			models = append(models, toModelInfo(providerSlug, wm))
		}
	}
	return models, nil
}

// FetchModelsByProvider fetches all models and returns only those from the
// given provider. It is a convenience wrapper around FetchModels.
func (c *Client) FetchModelsByProvider(ctx context.Context, p Provider) ([]ModelInfo, error) {
	all, err := c.FetchModels(ctx)
	if err != nil {
		return nil, err
	}
	filtered := make([]ModelInfo, 0, len(all))
	for _, m := range all {
		if m.Provider == p {
			filtered = append(filtered, m)
		}
	}
	return filtered, nil
}
