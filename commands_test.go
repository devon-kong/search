package main

import (
	"testing"

	"github.com/spf13/cobra"

	"sx/backends"
)

func TestExitCodeConstants(t *testing.T) {
	// Documented contract for machine callers.
	if exitSuccess != 0 {
		t.Errorf("exitSuccess = %d, want 0", exitSuccess)
	}
	if exitSearchFail != 1 {
		t.Errorf("exitSearchFail = %d, want 1", exitSearchFail)
	}
	if exitUsageConfig != 2 {
		t.Errorf("exitUsageConfig = %d, want 2", exitUsageConfig)
	}
	if exitCheckFail != 3 {
		t.Errorf("exitCheckFail = %d, want 3", exitCheckFail)
	}
}

func TestSetExit(t *testing.T) {
	old := exitCode
	defer func() { exitCode = old }()
	setExit(exitSearchFail)
	if exitCode != exitSearchFail {
		t.Errorf("setExit did not record code, got %d", exitCode)
	}
}

// --- validateConfig: zero-network config validation -------------------------

func TestValidateConfig_ValidSearxng(t *testing.T) {
	cfg := &Config{Engine: "searxng", SearxngURL: "https://searx.example.com"}
	problems, _ := validateConfig(cfg)
	if len(problems) != 0 {
		t.Errorf("expected valid config, got problems: %v", problems)
	}
}

func TestValidateConfig_SearxngMissingURL(t *testing.T) {
	cfg := &Config{Engine: "searxng"}
	problems, _ := validateConfig(cfg)
	if len(problems) == 0 {
		t.Errorf("expected a problem for searxng without URL")
	}
}

func TestValidateConfig_UnknownEngine(t *testing.T) {
	cfg := &Config{Engine: "bogus"}
	problems, _ := validateConfig(cfg)
	found := false
	for _, p := range problems {
		if contains(p, "unknown engine") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected unknown engine problem, got %v", problems)
	}
}

func TestValidateConfig_UnknownFallback(t *testing.T) {
	cfg := &Config{Engine: "searxng", SearxngURL: "https://x", FallbackEngines: []string{"nope"}}
	problems, _ := validateConfig(cfg)
	found := false
	for _, p := range problems {
		if contains(p, "unknown fallback") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected unknown fallback problem, got %v", problems)
	}
}

// Paid backend in fallback while gate closed is a NOTE, not a problem (must not
// fail validation): it is a safe, intended configuration.
func TestValidateConfig_PaidFallbackIsNoteNotProblem(t *testing.T) {
	cfg := &Config{
		Engine:            "searxng",
		SearxngURL:        "https://x",
		FallbackEngines:   []string{"tavily"},
		AllowPaidFallback: false,
	}
	problems, notes := validateConfig(cfg)
	for _, p := range problems {
		if contains(p, "tavily") {
			t.Errorf("paid fallback should not be a hard problem, got %v", problems)
		}
	}
	found := false
	for _, n := range notes {
		if contains(n, "tavily") && contains(n, "paid") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a note about the paid fallback, got notes=%v", notes)
	}
}

func TestValidateConfig_InvalidHTTPMethod(t *testing.T) {
	cfg := &Config{Engine: "searxng", SearxngURL: "https://x", HTTPMethod: "DELETE"}
	problems, _ := validateConfig(cfg)
	found := false
	for _, p := range problems {
		if contains(p, "http_method") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected http_method problem, got %v", problems)
	}
}

func TestValidateConfig_InvalidSafeSearch(t *testing.T) {
	cfg := &Config{Engine: "searxng", SearxngURL: "https://x", SafeSearch: "maybe"}
	problems, _ := validateConfig(cfg)
	found := false
	for _, p := range problems {
		if contains(p, "safe_search") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected safe_search problem, got %v", problems)
	}
}

// --- search alias command assembly ------------------------------------------

func TestNewSearchCmd_IsAliasOfRoot(t *testing.T) {
	root := &cobra.Command{Use: "sx"}
	root.Flags().Bool("json", false, "")
	root.Flags().String("engine", "", "")

	sc := newSearchCmd(root)
	if sc.Use != "search [query...]" {
		t.Errorf("unexpected Use: %q", sc.Use)
	}
	// Same code path (runSearch) as the root command.
	if sc.Run == nil {
		t.Errorf("search command must have a Run handler")
	}
	// Shares the root flag set, so it accepts the same flags as `sx`.
	if sc.Flags().Lookup("json") == nil {
		t.Errorf("search alias should expose root's --json flag")
	}
	if sc.Flags().Lookup("engine") == nil {
		t.Errorf("search alias should expose root's --engine flag")
	}
}

func TestNewConfigCmd_HasValidate(t *testing.T) {
	c := newConfigCmd()
	var hasValidate bool
	for _, sub := range c.Commands() {
		if sub.Name() == "validate" {
			hasValidate = true
		}
	}
	if !hasValidate {
		t.Errorf("config command should have a validate subcommand")
	}
}

func TestNewHealthCmd_HasLiveFlagDefaultFalse(t *testing.T) {
	c := newHealthCmd()
	f := c.Flags().Lookup("live")
	if f == nil {
		t.Fatalf("health command should have a --live flag")
	}
	if f.DefValue != "false" {
		t.Errorf("--live should default to false, got %q", f.DefValue)
	}
}

// --- initBackendManager wires config base_url into backends -----------------

func TestInitBackendManager_WiresBaseURLs(t *testing.T) {
	cfg := getDefaultConfig()
	cfg.SearxngURL = "https://searx.example.com"
	cfg.EnginesBrave.BaseURL = "https://brave.mock"
	cfg.EnginesTavily.BaseURL = "https://tavily.mock"
	cfg.EnginesExa.BaseURL = "https://exa.mock"

	mgr := initBackendManager(cfg)

	if b, ok := mgr.GetBackend("brave"); ok {
		if bb, ok := b.(*backends.BraveBackend); ok && bb.BaseURL != "https://brave.mock" {
			t.Errorf("brave BaseURL = %q, want https://brave.mock", bb.BaseURL)
		}
	} else {
		t.Errorf("brave backend not registered")
	}

	if b, ok := mgr.GetBackend("tavily"); ok {
		if tb, ok := b.(*backends.TavilyBackend); ok && tb.BaseURL != "https://tavily.mock" {
			t.Errorf("tavily BaseURL = %q, want https://tavily.mock", tb.BaseURL)
		}
	} else {
		t.Errorf("tavily backend not registered")
	}

	if b, ok := mgr.GetBackend("exa"); ok {
		if eb, ok := b.(*backends.ExaBackend); ok && eb.BaseURL != "https://exa.mock" {
			t.Errorf("exa BaseURL = %q, want https://exa.mock", eb.BaseURL)
		}
	} else {
		t.Errorf("exa backend not registered")
	}
}

// initBackendManager must honor AllowPaidFallback by gating the paid fallback in
// the resulting manager (closed gate -> paid backend never called).
func TestInitBackendManager_PaidGateClosedByConfig(t *testing.T) {
	cfg := getDefaultConfig()
	cfg.SearxngURL = "" // make searxng primary fail (unconfigured)
	cfg.EnginesTavily.APIKey = "fake"
	cfg.FallbackEngines = []string{"tavily"}
	cfg.AllowPaidFallback = false

	mgr := initBackendManager(cfg)
	_, err := mgr.Search(backends.SearchOptions{Query: "x"})
	if err == nil {
		t.Fatalf("expected failure: searxng unconfigured and paid fallback gated")
	}
}
