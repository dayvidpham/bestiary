package bestiary

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

// defaultBaseURL is the canonical models.dev api.json endpoint. The sibling
// models.json and catalog.json artifact URLs are derived from it (see
// artifactURL), so overriding it with WithBaseURL redirects all three fetches.
const defaultBaseURL = "https://models.dev/api.json"

// artifactModels and artifactCatalog are the sibling artifact basenames used to
// derive the models.json / catalog.json URLs from the configured base URL.
const (
	artifactModels  = "models.json"
	artifactCatalog = "catalog.json"
)

// defaultTimeout is applied to the underlying http.Client when no
// WithTimeout option is provided.
const defaultTimeout = 30 * time.Second

// defaultRetries is the number of retry attempts made after the first failure
// when no WithRetries option is provided.
const defaultRetries = 2

// maxBodyBytes caps the size of any single artifact response body read from the
// API, guarding against unbounded reads.
const maxBodyBytes = 10 * 1024 * 1024 // 10 MB

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
// "https://models.dev/api.json". The models.json and catalog.json artifact URLs
// are derived from it as siblings, so a test server pointed here via WithBaseURL
// serves all three artifacts by routing on the request path.
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

// ParseAPIJSON decodes an api.json artifact (a models.dev provider map) into the
// public ModelInfo slice. It is the SAME decode path Client.FetchModels uses, so
// codegen and tests can parse a committed api.json snapshot offline with
// identical results. Model order follows API map iteration and is not sorted;
// callers that need determinism sort the result themselves.
func ParseAPIJSON(data []byte) ([]ModelInfo, error) {
	var apiResp wireResponse
	if err := json.Unmarshal(data, &apiResp); err != nil {
		return nil, fmt.Errorf(
			"bestiary: ParseAPIJSON: decode api.json: %w;"+
				" why: the payload is not a valid models.dev provider map"+
				" (map of provider slug → {models});"+
				" where: ParseAPIJSON;"+
				" how to fix: verify the bytes are the api.json artifact"+
				" (or the catalog.json \"providers\" value)",
			err,
		)
	}
	return modelsFromProviderMap(apiResp), nil
}

// ParseModelsJSON decodes a models.json artifact (provider-agnostic model facts,
// keyed by canonical <lab>/<model> id) into the public EntityMetadata slice. It
// is the SAME decode path Client.FetchModelMetadata uses. A malformed benchmark
// score (a JSON string) is captured per row on ScoreRaw and never fails the
// parse; no row is dropped. Source and LastSynced are left zero — the caller
// assigns ingest provenance.
func ParseModelsJSON(data []byte) ([]EntityMetadata, error) {
	var m wireModelMetadataMap
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf(
			"bestiary: ParseModelsJSON: decode models.json: %w;"+
				" why: the payload is not a valid models.dev metadata map"+
				" (map of canonical model id → model facts);"+
				" where: ParseModelsJSON;"+
				" how to fix: verify the bytes are the models.json artifact"+
				" (or the catalog.json \"models\" value)",
			err,
		)
	}
	return metadataFromMap(m), nil
}

// ParseCatalogJSON decodes a catalog.json artifact ({models, providers}) into a
// Catalog. It is the SAME decode path Client.FetchCatalog uses, and is exactly
// the composition of ParseAPIJSON over the providers value and ParseModelsJSON
// over the models value — both views come from a single upstream deploy.
func ParseCatalogJSON(data []byte) (Catalog, error) {
	var wc wireCatalog
	if err := json.Unmarshal(data, &wc); err != nil {
		return Catalog{}, fmt.Errorf(
			"bestiary: ParseCatalogJSON: decode catalog.json: %w;"+
				" why: the payload is not a valid models.dev catalog"+
				" (an object with \"models\" and \"providers\" keys);"+
				" where: ParseCatalogJSON;"+
				" how to fix: verify the bytes are the catalog.json artifact",
			err,
		)
	}
	return Catalog{
		Models:   modelsFromProviderMap(wc.Providers),
		Metadata: metadataFromMap(wc.Models),
	}, nil
}

// modelsFromProviderMap flattens a decoded provider map into ModelInfo values.
// It is the single conversion shared by ParseAPIJSON and ParseCatalogJSON so the
// two produce identical results for the same providers view.
func modelsFromProviderMap(apiResp wireResponse) []ModelInfo {
	var models []ModelInfo
	for providerSlug, prov := range apiResp {
		for _, wm := range prov.Models {
			models = append(models, toModelInfo(providerSlug, wm))
		}
	}
	return models
}

// metadataFromMap flattens a decoded metadata map into EntityMetadata values. It
// is the single conversion shared by ParseModelsJSON and ParseCatalogJSON.
func metadataFromMap(m wireModelMetadataMap) []EntityMetadata {
	out := make([]EntityMetadata, 0, len(m))
	for key, wm := range m {
		out = append(out, toEntityMetadata(key, wm))
	}
	return out
}

// FetchModels retrieves all model metadata from the models.dev api.json artifact.
// It retries on transient failures (non-2xx responses, network errors, or a
// malformed body) up to c.retries additional times with linear backoff, honouring
// ctx between attempts.
//
// On final failure it returns *ErrAPIUnavailable so callers can use errors.As to
// inspect structured fields.
//
// LastSynced on each returned ModelInfo is left empty; the caller must set it
// when persisting the results.
func (c *Client) FetchModels(ctx context.Context) ([]ModelInfo, error) {
	return fetchAndParse(ctx, c, c.baseURL, ParseAPIJSON)
}

// FetchModelMetadata retrieves the provider-agnostic model facts from the
// models.dev models.json artifact (URL derived as a sibling of the base URL). Its
// retry, body-limit, and error semantics match FetchModels. Source and LastSynced
// on each returned EntityMetadata are left zero; the caller assigns them on persist.
func (c *Client) FetchModelMetadata(ctx context.Context) ([]EntityMetadata, error) {
	u, err := c.artifactURL(artifactModels)
	if err != nil {
		return nil, err
	}
	return fetchAndParse(ctx, c, u, ParseModelsJSON)
}

// FetchCatalog retrieves both catalog views in a single request from the
// models.dev catalog.json artifact (URL derived as a sibling of the base URL). Its
// retry, body-limit, and error semantics match FetchModels.
func (c *Client) FetchCatalog(ctx context.Context) (Catalog, error) {
	u, err := c.artifactURL(artifactCatalog)
	if err != nil {
		return Catalog{}, err
	}
	return fetchAndParse(ctx, c, u, ParseCatalogJSON)
}

// fetchAndParse is the shared fetch+parse retry loop for all three artifacts. It
// GETs url (with body-limit enforcement) and applies parse to the body, retrying
// on any transient error — a non-2xx status, a network error, or a parse failure
// — up to c.retries additional times with linear backoff, honouring ctx between
// attempts. On final failure it returns *ErrAPIUnavailable naming url. It is a
// free function rather than a method because Go methods cannot be generic.
func fetchAndParse[T any](ctx context.Context, c *Client, url string, parse func([]byte) (T, error)) (T, error) {
	var zero T
	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		// Honour context cancellation between retry waits.
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return zero, ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}

		body, err := c.getBytes(ctx, url)
		if err == nil {
			var result T
			result, err = parse(body)
			if err == nil {
				return result, nil
			}
		}
		lastErr = err
	}
	return zero, &ErrAPIUnavailable{
		URL:      url,
		Attempts: c.retries + 1,
		Cause:    lastErr,
	}
}

// getBytes performs a single HTTP GET of url and returns the raw response body.
// It enforces a 10 MB body limit and returns a descriptive error on any non-200
// status, request-construction failure, network error, or read failure.
func (c *Client) getBytes(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("bestiary: Client.getBytes: create request for %s: %w", url, err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bestiary: Client.getBytes: HTTP GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"bestiary: Client.getBytes: unexpected HTTP status %d from %s; expected 200 OK",
			resp.StatusCode, url,
		)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("bestiary: Client.getBytes: read response body from %s: %w", url, err)
	}
	return body, nil
}

// artifactURL derives the URL of a sibling catalog artifact (models.json,
// catalog.json) from the configured base URL by replacing the base URL's final
// path segment with name. For the default base "https://models.dev/api.json" this
// yields "https://models.dev/<name>"; for a bare test-server base
// "http://127.0.0.1:port" it yields "http://127.0.0.1:port/<name>". This keeps
// all three fetches pointed at the same host and lets an httptest server route
// on the request path.
func (c *Client) artifactURL(name string) (string, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf(
			"bestiary: Client.artifactURL: parse base URL %q: %w;"+
				" why: the configured base URL is not a valid URL;"+
				" where: Client.artifactURL (deriving the %s artifact URL);"+
				" how to fix: pass a valid URL via WithBaseURL"+
				" (e.g. https://models.dev/api.json)",
			c.baseURL, err, name,
		)
	}
	dir := path.Dir(u.Path)
	if dir == "" || dir == "." {
		dir = "/"
	}
	u.Path = path.Join(dir, name)
	if !strings.HasPrefix(u.Path, "/") {
		u.Path = "/" + u.Path
	}
	return u.String(), nil
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
