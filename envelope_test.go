package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"sx/backends"
)

// decodeEnvelope marshals an envelope and re-decodes it into a generic map so we
// can assert the exact JSON contract (field presence, null semantics, types).
func decodeEnvelope(t *testing.T, env *JSONEnvelope) map[string]interface{} {
	t.Helper()
	if env.Warnings == nil {
		env.Warnings = []string{}
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

func requireKeys(t *testing.T, m map[string]interface{}, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			t.Errorf("missing key %q in %v", k, m)
		}
	}
}

func TestJSONEnvelope_SuccessSchema(t *testing.T) {
	used := "searxng"
	env := &JSONEnvelope{
		OK:    true,
		Query: "golang",
		Backend: jsonBackendMeta{
			Requested:    "searxng",
			Used:         &used,
			FallbackUsed: false,
			CostTier:     backends.CostTierSelfHosted,
		},
		Timing:   jsonTiming{TotalMs: 12},
		Results:  []SearchResult{{Title: "T", URL: "https://x"}},
		Warnings: []string{},
		Error:    nil,
	}
	m := decodeEnvelope(t, env)

	requireKeys(t, m, "ok", "query", "backend", "timing", "results", "warnings", "error")

	if m["ok"] != true {
		t.Errorf("ok = %v, want true", m["ok"])
	}
	if m["error"] != nil {
		t.Errorf("error must be null on success, got %v", m["error"])
	}

	be := m["backend"].(map[string]interface{})
	requireKeys(t, be, "requested", "used", "fallback_used", "fallback_reason", "cost_tier")
	if be["used"] == nil {
		t.Errorf("backend.used must be non-null on success")
	}
	if be["cost_tier"] != backends.CostTierSelfHosted {
		t.Errorf("cost_tier = %v", be["cost_tier"])
	}

	tm := m["timing"].(map[string]interface{})
	requireKeys(t, tm, "total_ms")

	// warnings must be an array (never null)
	if _, ok := m["warnings"].([]interface{}); !ok {
		t.Errorf("warnings should be a JSON array, got %T", m["warnings"])
	}
}

func TestJSONEnvelope_DiagnosticsOmittedByDefault(t *testing.T) {
	used := "searxng"
	env := &JSONEnvelope{
		OK:    true,
		Query: "golang",
		Backend: jsonBackendMeta{
			Requested: "searxng",
			Used:      &used,
		},
		Timing:   jsonTiming{TotalMs: 1},
		Results:  []SearchResult{},
		Warnings: []string{},
		Error:    nil,
	}
	m := decodeEnvelope(t, env)
	if _, ok := m["diagnostics"]; ok {
		t.Fatalf("diagnostics should be omitted by default, got %v", m["diagnostics"])
	}
}

func TestJSONEnvelope_DiagnosticsIncludedWhenSet(t *testing.T) {
	used := "searxng"
	env := &JSONEnvelope{
		OK:    true,
		Query: "golang",
		Backend: jsonBackendMeta{
			Requested: "searxng",
			Used:      &used,
		},
		Timing:   jsonTiming{TotalMs: 1},
		Results:  []SearchResult{},
		Warnings: []string{},
		Diagnostics: &backends.SearxngDiagnostics{
			Answers:             json.RawMessage(`[]`),
			Suggestions:         json.RawMessage(`["go"]`),
			Infoboxes:           json.RawMessage(`[]`),
			UnresponsiveEngines: json.RawMessage(`[]`),
			NumberOfResults:     0,
		},
		Error: nil,
	}
	m := decodeEnvelope(t, env)
	diagnostics, ok := m["diagnostics"].(map[string]interface{})
	if !ok {
		t.Fatalf("diagnostics should be an object, got %T", m["diagnostics"])
	}
	requireKeys(t, diagnostics, "answers", "suggestions", "infoboxes", "unresponsive_engines", "number_of_results")
}

func TestJSONEnvelope_FailureSchema(t *testing.T) {
	env := &JSONEnvelope{
		OK:    false,
		Query: "golang",
		Backend: jsonBackendMeta{
			Requested:    "searxng",
			Used:         nil,
			FallbackUsed: false,
			CostTier:     backends.CostTierSelfHosted,
		},
		Timing:  jsonTiming{TotalMs: 5},
		Results: []SearchResult{},
		Error: &jsonError{
			Code:              "BACKEND_UNAVAILABLE",
			Message:           "all backends failed",
			Backend:           "searxng",
			Retryable:         false,
			RetryAfterSeconds: nil,
		},
	}
	m := decodeEnvelope(t, env)

	requireKeys(t, m, "ok", "query", "backend", "results", "warnings", "error")
	if m["ok"] != false {
		t.Errorf("ok = %v, want false", m["ok"])
	}

	be := m["backend"].(map[string]interface{})
	if be["used"] != nil {
		t.Errorf("backend.used must be null on failure, got %v", be["used"])
	}

	results, ok := m["results"].([]interface{})
	if !ok || len(results) != 0 {
		t.Errorf("results must be an empty array on failure, got %v", m["results"])
	}

	errObj, ok := m["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error must be an object on failure, got %T", m["error"])
	}
	requireKeys(t, errObj, "code", "message", "backend", "retryable", "retry_after_seconds")
	if errObj["retry_after_seconds"] != nil {
		t.Errorf("retry_after_seconds should be null in Phase 1, got %v", errObj["retry_after_seconds"])
	}
	if errObj["retryable"] != false {
		t.Errorf("retryable = %v, want false", errObj["retryable"])
	}
}

func TestMapErrCodeToJSON(t *testing.T) {
	cases := []struct {
		code      int
		wantCode  string
		retryable bool
	}{
		{backends.ErrCodeUnavailable, "BACKEND_UNAVAILABLE", false},
		{backends.ErrCodeNetwork, "NETWORK", true},
		{backends.ErrCodeAuth, "AUTH", false},
		{backends.ErrCodeRateLimit, "RATE_LIMIT", true},
		{backends.ErrCodeInvalidResponse, "INVALID_RESPONSE", false},
		{503, "BACKEND_UNAVAILABLE", true}, // 5xx HTTP -> retryable
		{404, "BACKEND_UNAVAILABLE", false},
	}
	for _, c := range cases {
		gotCode, gotRetry := mapErrCodeToJSON(c.code)
		if gotCode != c.wantCode || gotRetry != c.retryable {
			t.Errorf("mapErrCodeToJSON(%d) = (%q,%v), want (%q,%v)", c.code, gotCode, gotRetry, c.wantCode, c.retryable)
		}
	}
}

func TestBuildJSONError_FromBackendError(t *testing.T) {
	be := &backends.BackendError{Backend: "tavily", Err: fmt.Errorf("rate limited"), Code: backends.ErrCodeRateLimit}
	jerr := buildJSONError(be, "searxng")
	if jerr.Code != "RATE_LIMIT" {
		t.Errorf("code = %q, want RATE_LIMIT", jerr.Code)
	}
	if !jerr.Retryable {
		t.Errorf("rate limit should be retryable")
	}
	if jerr.Backend != "tavily" {
		t.Errorf("backend should come from the wrapped BackendError, got %q", jerr.Backend)
	}
	if jerr.RetryAfterSeconds != nil {
		t.Errorf("retry_after_seconds should be nil in Phase 1")
	}
}

func TestBuildJSONError_RedactsSecrets(t *testing.T) {
	be := &backends.BackendError{
		Backend: "tavily",
		Err:     fmt.Errorf("auth failed: Authorization: Bearer tvly-LEAKME-123"),
		Code:    backends.ErrCodeAuth,
	}
	jerr := buildJSONError(be, "tavily")
	if strings.Contains(jerr.Message, "tvly-LEAKME-123") {
		t.Errorf("secret leaked into error message: %q", jerr.Message)
	}
}

func TestBuildResultsForJSON_CleanOmitsEmpty(t *testing.T) {
	results := []SearchResult{{Title: "T", URL: "https://x"}} // many empty fields

	// Non-clean: full struct, empty fields present as zero values.
	full := buildResultsForJSON(results, false)
	if _, ok := full.([]SearchResult); !ok {
		t.Errorf("non-clean should return []SearchResult, got %T", full)
	}

	// Clean: map without empty keys.
	clean := buildResultsForJSON(results, true)
	cleaned, ok := clean.([]map[string]interface{})
	if !ok {
		t.Fatalf("clean should return []map, got %T", clean)
	}
	if len(cleaned) != 1 {
		t.Fatalf("expected 1 cleaned result")
	}
	if _, present := cleaned[0]["content"]; present {
		t.Errorf("clean output should omit empty 'content', got %v", cleaned[0])
	}
	if cleaned[0]["title"] != "T" {
		t.Errorf("title should be present, got %v", cleaned[0])
	}
}

func TestBuildResultsForJSON_EmptyResultsNeverNull(t *testing.T) {
	for _, clean := range []bool{false, true} {
		env := &JSONEnvelope{
			OK:       true,
			Query:    "empty",
			Backend:  jsonBackendMeta{Requested: "searxng"},
			Timing:   jsonTiming{TotalMs: 1},
			Results:  buildResultsForJSON(nil, clean),
			Warnings: []string{},
			Error:    nil,
		}
		m := decodeEnvelope(t, env)
		results, ok := m["results"].([]interface{})
		if !ok {
			t.Fatalf("clean=%v: results should be an array, got %T (%v)", clean, m["results"], m["results"])
		}
		if len(results) != 0 {
			t.Fatalf("clean=%v: expected empty results array, got %v", clean, results)
		}
	}
}

func TestRedactWarnings_NeverNil(t *testing.T) {
	if got := redactWarnings(nil); got == nil || len(got) != 0 {
		t.Errorf("redactWarnings(nil) should be empty non-nil slice, got %v", got)
	}
	got := redactWarnings([]string{"skipped paid fallback \"tavily\""})
	if len(got) != 1 {
		t.Fatalf("expected 1 warning")
	}
}
