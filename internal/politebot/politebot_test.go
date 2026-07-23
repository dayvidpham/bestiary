package politebot

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// testUA is a stand-in descriptive User-Agent. Production bots pass their own
// versioned string into New; the seam guarantees whatever is passed is set on
// every request (TestClient_SetsUserAgent).
const testUA = "politebot-test/0.0.0 (+https://github.com/dayvidpham/bestiary; offline test)"

// manifestAccept is a sample Accept media type (the registry-v2 manifest type),
// used to assert Get forwards the caller's Accept header.
const manifestAccept = "application/vnd.docker.distribution.manifest.v2+json"

// recordingDoer is a canned transport: it records every request and returns a
// fixed 200 body, so the seam is exercised without opening a socket.
type recordingDoer struct {
	reqs []*http.Request
	body string
}

func (d *recordingDoer) Do(req *http.Request) (*http.Response, error) {
	d.reqs = append(d.reqs, req)
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(d.body)),
		Header:     make(http.Header),
	}, nil
}

// fakeClock is an injectable monotonic clock + sleeper: doSleep advances the
// clock instead of blocking, so rate-limit behavior is asserted with no real
// wall-clock elapsing.
type fakeClock struct {
	t     time.Time
	slept []time.Duration
}

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) doSleep(d time.Duration) { c.slept = append(c.slept, d); c.t = c.t.Add(d) }

// newTestClient builds a Client through the SAME exported injection seam
// (New + With* options) that cmd/bestiary-hf uses in production — not a
// test-only construction path — wired to a canned transport and fake clock.
func newTestClient(body string) (*Client, *recordingDoer, *fakeClock) {
	rd := &recordingDoer{body: body}
	fc := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	c := New(testUA, WithDoer(rd), WithClock(fc.now), WithSleep(fc.doSleep))
	return c, rd, fc
}

func TestClient_SetsUserAgent(t *testing.T) {
	c, rd, _ := newTestClient(`{}`)
	if _, err := c.Get(context.Background(), "https://registry.ollama.ai/v2/library/x/manifests/y", manifestAccept); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(rd.reqs) != 1 {
		t.Fatalf("want 1 request, got %d", len(rd.reqs))
	}
	if got := rd.reqs[0].Header.Get("User-Agent"); got != testUA {
		t.Fatalf("User-Agent = %q, want %q (a descriptive UA is mandatory)", got, testUA)
	}
	if got := rd.reqs[0].Header.Get("Accept"); got != manifestAccept {
		t.Fatalf("Accept = %q, want %q", got, manifestAccept)
	}
}

func TestClient_SleepsBetweenRequests(t *testing.T) {
	c, _, fc := newTestClient(`{}`)
	ctx := context.Background()
	if _, err := c.Get(ctx, "https://x/1", ""); err != nil {
		t.Fatalf("Get 1: %v", err)
	}
	if len(fc.slept) != 0 {
		t.Fatalf("first request must not sleep, slept=%v", fc.slept)
	}
	if _, err := c.Get(ctx, "https://x/2", ""); err != nil {
		t.Fatalf("Get 2: %v", err)
	}
	if len(fc.slept) != 1 {
		t.Fatalf("second request must sleep exactly once, slept=%v", fc.slept)
	}
	if fc.slept[0] < DefaultMinRequestInterval {
		t.Fatalf("rate-limit sleep = %v, want >= %v (>= 1 second between requests)", fc.slept[0], DefaultMinRequestInterval)
	}
}

// A non-2xx status is rejected with an actionable error (never returned as a
// successful body). This guards the seam's non-2xx reject discipline.
func TestClient_RejectsNon2xx(t *testing.T) {
	rd := &errDoer{status: 404, body: "nope"}
	fc := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	c := New(testUA, WithDoer(rd), WithClock(fc.now), WithSleep(fc.doSleep))
	if _, err := c.Get(context.Background(), "https://x/missing", ""); err == nil {
		t.Fatalf("want error for a 404 status")
	}
}

type errDoer struct {
	status int
	body   string
}

func (d *errDoer) Do(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: d.status,
		Body:       io.NopCloser(strings.NewReader(d.body)),
		Header:     make(http.Header),
	}, nil
}
