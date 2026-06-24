package main

import (
	"encoding/json"
	"strings"
	"testing"

	"sx/backends"
)

func TestStrictEngineWarnings_UnresponsiveAndMixedEngines(t *testing.T) {
	diagnostics := &backends.SearxngDiagnostics{
		UnresponsiveEngines: json.RawMessage(`[["google","timeout"],["bing","disabled"]]`),
	}
	results := []SearchResult{{
		Title:   "result",
		Engine:  "google",
		Engines: []string{"google", "duckduckgo"},
	}}

	warnings := strictEngineWarnings([]string{"google"}, results, diagnostics)
	if len(warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %#v", warnings)
	}
	if !strings.Contains(warnings[0], `requested SearXNG engine "google"`) {
		t.Fatalf("missing unresponsive warning: %#v", warnings)
	}
	if !strings.Contains(strings.Join(warnings, "\n"), `unrequested SearXNG engine "duckduckgo"`) {
		t.Fatalf("missing mixed-engine warning: %#v", warnings)
	}
}

func TestApplyStrictEngineWarnings_WarningOnly(t *testing.T) {
	outcome := backends.SearchOutcome{
		Results: []backends.SearchResult{{
			Title:  "result",
			Engine: "duckduckgo",
		}},
		Diagnostics: &backends.SearxngDiagnostics{
			UnresponsiveEngines: json.RawMessage(`[]`),
		},
	}

	applyStrictEngineWarnings(&outcome, []string{"google"})
	if len(outcome.Results) != 1 {
		t.Fatalf("strict-engines must not filter results, got %#v", outcome.Results)
	}
	if len(outcome.Warnings) != 1 {
		t.Fatalf("expected one warning, got %#v", outcome.Warnings)
	}
	if len(outcome.Diagnostics.StrictEnginesWarnings) != 1 {
		t.Fatalf("diagnostics should carry strict warnings, got %#v", outcome.Diagnostics)
	}
}

func TestStrictEngineWarnings_NoRequestedEnginesNoWarnings(t *testing.T) {
	warnings := strictEngineWarnings(nil, []SearchResult{{Engine: "duckduckgo"}}, nil)
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings without requested engines, got %#v", warnings)
	}
}
