package bestiary

import (
	"context"
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
	return nil, nil // implemented in L3
}

// FetchModelsByProvider fetches all models and returns only those from the
// given provider. It is a convenience wrapper around FetchModels.
func (c *Client) FetchModelsByProvider(ctx context.Context, p Provider) ([]ModelInfo, error) {
	return nil, nil // implemented in L3
}
