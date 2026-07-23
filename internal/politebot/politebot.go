// Package politebot is the shared, compiler-private polite-HTTP plumbing for the
// offline registry-ingest tools (cmd/bestiary-ollama, cmd/bestiary-hf).
//
// It exists so the project's politeness policy — a descriptive, versioned
// User-Agent and at least one second between outbound requests (a user-stated
// hard constraint, GH#12) — has ONE implementation, tested once, rather than
// being copied into each bot. Every fetch a bot makes funnels through the single
// Client.Get seam, which is what makes the guarantee structurally enforceable
// (and unit-testable without real time or real sockets).
//
// The seam is injectable by design: the outbound transport (Doer), the clock
// (now), and the sleeper (sleep) are all swappable via functional options, so an
// offline test drives Get with a canned transport and a fake clock and never
// opens a socket or waits real wall-clock. Production callers use New with no
// options and get a real *http.Client, time.Now, and time.Sleep.
//
// This package is under internal/ (unimportable outside the module) and carries
// ZERO public-API impact: it is cmd-side tool plumbing, not part of the root
// bestiary library surface.
package politebot

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	// DefaultMinRequestInterval is the minimum wall-clock gap enforced between two
	// outbound requests: at least one second, a user-stated hard constraint
	// (GH#12). New applies it unless WithMinInterval overrides it.
	DefaultMinRequestInterval = 1 * time.Second

	// MaxResponseBytes caps any single response body (defensive; registry
	// manifests and config blobs are tiny, tag pages are modest). It is
	// deliberately the value the shipped Ollama tool used (8 MiB); a bot needing a
	// different cap should request a dedicated option rather than harmonizing this.
	MaxResponseBytes = 8 << 20 // 8 MiB
)

// Doer is the minimal HTTP surface the polite Client needs. *http.Client
// satisfies it; tests inject a canned transport so no socket is opened.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Client is the SINGLE outbound-request seam a polite bot uses. Every fetch
// funnels through Get, which (a) enforces the minimum request interval since the
// previous request via an injectable clock+sleeper, and (b) sets the descriptive
// User-Agent. Routing all traffic through one seam is what makes the politeness
// guarantee structurally enforceable (and unit-testable without real time or
// sockets).
type Client struct {
	doer        Doer
	ua          string
	minInterval time.Duration
	now         func() time.Time
	sleep       func(time.Duration)

	started bool
	last    time.Time
}

// Option customizes a Client at construction. The zero-option New yields a
// production client; the With* options are the injection seam offline tests and
// alternate bots use — one code path for both, no test-only export.
type Option func(*Client)

// WithDoer injects the outbound HTTP transport (default a real *http.Client with
// a 30s timeout). Tests pass a canned transport so no socket is opened.
func WithDoer(d Doer) Option { return func(c *Client) { c.doer = d } }

// WithClock injects the monotonic clock used to measure the inter-request gap
// (default time.Now). Tests pass a fake clock so no real time elapses.
func WithClock(now func() time.Time) Option { return func(c *Client) { c.now = now } }

// WithSleep injects the sleeper used to honor the request interval (default
// time.Sleep). Tests pass a fake sleeper that advances the fake clock instead of
// blocking.
func WithSleep(sleep func(time.Duration)) Option { return func(c *Client) { c.sleep = sleep } }

// WithMinInterval overrides the minimum inter-request gap (default
// DefaultMinRequestInterval). The floor exists to honor a project hard
// constraint; lower it only with a deliberate reason.
func WithMinInterval(d time.Duration) Option { return func(c *Client) { c.minInterval = d } }

// New builds a polite Client that identifies itself with userAgent on every
// request. With no options it is a production client backed by a real
// *http.Client, the real monotonic clock, and a real time.Sleep. The userAgent
// is per-bot (each tool passes its own descriptive, versioned string) so the
// politeness identity is caller-owned while the cadence policy is shared.
func New(userAgent string, opts ...Option) *Client {
	c := &Client{
		doer:        &http.Client{Timeout: 30 * time.Second},
		ua:          userAgent,
		minInterval: DefaultMinRequestInterval,
		now:         time.Now,
		sleep:       time.Sleep,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Get performs one polite GET. It sleeps to honor the minimum request interval
// (skipped on the very first request), sets the User-Agent (and optional Accept)
// headers, and returns the response body (capped at MaxResponseBytes) for a 2xx
// status.
func (c *Client) Get(ctx context.Context, url, accept string) ([]byte, error) {
	if c.started {
		elapsed := c.now().Sub(c.last)
		if elapsed < c.minInterval {
			c.sleep(c.minInterval - elapsed)
		}
	}
	c.started = true
	c.last = c.now()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf(
			"politebot: build request for %q failed: %w\n"+
				"  What: net/http rejected the request URL\n"+
				"  Where: Client.Get\n"+
				"  How to fix: verify the URL is well-formed", url, err)
	}
	req.Header.Set("User-Agent", c.ua)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}

	resp, err := c.doer.Do(req)
	if err != nil {
		return nil, fmt.Errorf(
			"politebot: GET %q failed: %w\n"+
				"  What: the HTTP request did not complete\n"+
				"  Where: Client.Get\n"+
				"  When: during a live polite-bot fetch\n"+
				"  How to fix: check network connectivity to %s", url, err, url)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf(
			"politebot: read body of %q failed: %w\n"+
				"  Where: Client.Get\n"+
				"  How to fix: retry; the response stream was truncated or reset", url, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf(
			"politebot: GET %q returned HTTP %d\n"+
				"  What: a non-2xx status\n"+
				"  Where: Client.Get\n"+
				"  Why: the model/tag may not exist or the registry rejected the request\n"+
				"  How to fix: verify the URL/tag and the Accept header", url, resp.StatusCode)
	}
	c.last = c.now()
	return body, nil
}
