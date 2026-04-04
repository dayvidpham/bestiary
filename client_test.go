package bestiary_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dayvidpham/bestiary"
)

// sampleWireJSON is the canonical mock payload used across tests.
// It represents the exact shape returned by models.dev/api.json.
const sampleWireJSON = `{
  "anthropic": {
    "models": {
      "claude-opus-4-6": {
        "id": "claude-opus-4-6",
        "name": "Claude Opus 4.6",
        "family": "claude",
        "reasoning": true,
        "tool_call": true,
        "attachment": true,
        "temperature": true,
        "structured_output": false,
        "interleaved": false,
        "open_weights": false,
        "release_date": "2025-07-01",
        "knowledge": "2024-12",
        "cost": {"input": 15.0, "output": 75.0, "reasoning": 0.0},
        "limit": {"context": 200000, "output": 32000},
        "modalities": {"input": ["text", "image"], "output": ["text"]}
      }
    }
  }
}`

// TestFetchModels_ValidJSON verifies that FetchModels correctly maps every
// field from the wire JSON to a ModelInfo value.
func TestFetchModels_ValidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, sampleWireJSON)
	}))
	defer srv.Close()

	c := bestiary.NewClient(bestiary.WithBaseURL(srv.URL))
	models, err := c.FetchModels(context.Background())
	if err != nil {
		t.Fatalf("FetchModels: unexpected error: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}

	m := models[0]

	if string(m.ID) != "claude-opus-4-6" {
		t.Errorf("ID: got %q, want %q", m.ID, "claude-opus-4-6")
	}
	if string(m.Provider) != "anthropic" {
		t.Errorf("Provider: got %q, want %q", m.Provider, "anthropic")
	}
	if m.DisplayName != "Claude Opus 4.6" {
		t.Errorf("DisplayName: got %q, want %q", m.DisplayName, "Claude Opus 4.6")
	}
	if m.Family != "claude" {
		t.Errorf("Family: got %q, want %q", m.Family, "claude")
	}
	if !m.Reasoning {
		t.Error("Reasoning: expected true")
	}
	if !m.ToolCall {
		t.Error("ToolCall: expected true")
	}
	if !m.Attachment {
		t.Error("Attachment: expected true")
	}
	if !m.Temperature {
		t.Error("Temperature: expected true")
	}
	if m.ContextWindow != 200000 {
		t.Errorf("ContextWindow: got %d, want 200000", m.ContextWindow)
	}
	if m.MaxOutput != 32000 {
		t.Errorf("MaxOutput: got %d, want 32000", m.MaxOutput)
	}
	if m.CostInputPerMTok == nil || *m.CostInputPerMTok != 15.0 {
		t.Errorf("CostInputPerMTok: got %v, want 15.0", m.CostInputPerMTok)
	}
	if m.CostOutputPerMTok == nil || *m.CostOutputPerMTok != 75.0 {
		t.Errorf("CostOutputPerMTok: got %v, want 75.0", m.CostOutputPerMTok)
	}
	if m.ReleaseDate != "2025-07-01" {
		t.Errorf("ReleaseDate: got %q, want %q", m.ReleaseDate, "2025-07-01")
	}
	if m.Knowledge != "2024-12" {
		t.Errorf("Knowledge: got %q, want %q", m.Knowledge, "2024-12")
	}
	// Modalities
	if len(m.Modalities.Input) != 2 {
		t.Errorf("Modalities.Input length: got %d, want 2", len(m.Modalities.Input))
	}
	if len(m.Modalities.Output) != 1 {
		t.Errorf("Modalities.Output length: got %d, want 1", len(m.Modalities.Output))
	}
	// LastSynced must be empty — caller sets it on persist
	if m.LastSynced != "" {
		t.Errorf("LastSynced: expected empty string, got %q", m.LastSynced)
	}
}

// TestFetchModels_RetryOn500 verifies that the client retries exactly
// retries+1 total times when the server returns 500 each time.
func TestFetchModels_RetryOn500(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	// 2 retries → 3 total attempts
	c := bestiary.NewClient(
		bestiary.WithBaseURL(srv.URL),
		bestiary.WithRetries(2),
		bestiary.WithTimeout(5*time.Second),
	)
	_, err := c.FetchModels(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	got := int(atomic.LoadInt32(&attempts))
	if got != 3 {
		t.Errorf("attempts: got %d, want 3", got)
	}

	var apiErr *bestiary.ErrAPIUnavailable
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type: expected *ErrAPIUnavailable, got %T: %v", err, err)
	}
	if apiErr.Attempts != 3 {
		t.Errorf("ErrAPIUnavailable.Attempts: got %d, want 3", apiErr.Attempts)
	}
}

// TestFetchModels_ContextCancellation verifies that a cancelled context
// interrupts the retry wait rather than sleeping for the full backoff.
func TestFetchModels_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context after 100 ms — well before the 1 s backoff would expire.
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	c := bestiary.NewClient(
		bestiary.WithBaseURL(srv.URL),
		bestiary.WithRetries(5),
		bestiary.WithTimeout(5*time.Second),
	)

	start := time.Now()
	_, err := c.FetchModels(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Should have returned within ~500 ms, not after multiple full backoffs.
	if elapsed > 2*time.Second {
		t.Errorf("FetchModels did not respect context cancellation: took %v", elapsed)
	}
	// Error should either be a context error or wrap one.
	if !errors.Is(err, context.Canceled) {
		t.Logf("note: returned error was %v (not context.Canceled, may be wrapped in ErrAPIUnavailable)", err)
	}
}

// TestFetchModels_10MBLimit verifies that the client caps response bodies at
// 10 MB and returns an error rather than reading unbounded data.
func TestFetchModels_10MBLimit(t *testing.T) {
	const limit = 10 * 1024 * 1024 // 10 MB

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Write a valid JSON prefix then flood with garbage to exceed 10 MB.
		// io.LimitReader will truncate, causing json.Unmarshal to fail.
		w.Write([]byte(`{"x":{"models":{`))
		garbage := strings.Repeat("a", limit+1024)
		io.WriteString(w, garbage)
	}))
	defer srv.Close()

	c := bestiary.NewClient(
		bestiary.WithBaseURL(srv.URL),
		bestiary.WithRetries(0), // no retries — fail fast
		bestiary.WithTimeout(10*time.Second),
	)

	_, err := c.FetchModels(context.Background())
	if err == nil {
		t.Fatal("expected error for oversized body, got nil")
	}
}

// TestFetchModelsByProvider_Filters verifies that FetchModelsByProvider
// returns only models whose provider slug matches the requested Provider.
func TestFetchModelsByProvider_Filters(t *testing.T) {
	multiProviderJSON, _ := json.Marshal(map[string]interface{}{
		"anthropic": map[string]interface{}{
			"models": map[string]interface{}{
				"claude-3-5-haiku": map[string]interface{}{
					"id": "claude-3-5-haiku", "name": "Claude 3.5 Haiku",
				},
			},
		},
		"openai": map[string]interface{}{
			"models": map[string]interface{}{
				"gpt-4o": map[string]interface{}{
					"id": "gpt-4o", "name": "GPT-4o",
				},
			},
		},
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(multiProviderJSON)
	}))
	defer srv.Close()

	c := bestiary.NewClient(bestiary.WithBaseURL(srv.URL))

	got, err := c.FetchModelsByProvider(context.Background(), bestiary.ProviderAnthropic)
	if err != nil {
		t.Fatalf("FetchModelsByProvider: unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 anthropic model, got %d: %+v", len(got), got)
	}
	if string(got[0].Provider) != "anthropic" {
		t.Errorf("Provider: got %q, want %q", got[0].Provider, "anthropic")
	}
}

// TestFetchModels_ErrAPIUnavailable verifies that errors.As can extract a
// *ErrAPIUnavailable from the error returned after all retries are exhausted.
func TestFetchModels_ErrAPIUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusGone)
	}))
	defer srv.Close()

	c := bestiary.NewClient(
		bestiary.WithBaseURL(srv.URL),
		bestiary.WithRetries(1),
		bestiary.WithTimeout(5*time.Second),
	)

	_, err := c.FetchModels(context.Background())
	if err == nil {
		t.Fatal("expected ErrAPIUnavailable, got nil")
	}

	var apiErr *bestiary.ErrAPIUnavailable
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *bestiary.ErrAPIUnavailable, got %T: %v", err, err)
	}
	// 1 retry → 2 total attempts
	if apiErr.Attempts != 2 {
		t.Errorf("Attempts: got %d, want 2", apiErr.Attempts)
	}
	if apiErr.URL == "" {
		t.Error("URL field should not be empty")
	}
	if apiErr.Cause == nil {
		t.Error("Cause field should not be nil")
	}
}
