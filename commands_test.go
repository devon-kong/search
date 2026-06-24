package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

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

func TestNewSearxngCmd_HasRawSubcommandAndFlags(t *testing.T) {
	oldConfig := config
	config = getDefaultConfig()
	defer func() { config = oldConfig }()

	c := newSearxngCmd()
	var raw *cobra.Command
	for _, sub := range c.Commands() {
		if sub.Name() == "raw" {
			raw = sub
			break
		}
	}
	if raw == nil {
		t.Fatalf("searxng command should have a raw subcommand")
	}
	for _, flag := range []string{"categories", "news", "engines", "language", "safe-search", "time-range", "num"} {
		if raw.Flags().Lookup(flag) == nil {
			t.Fatalf("raw subcommand should expose --%s", flag)
		}
	}
	if raw.Flags().Lookup("clean") != nil {
		t.Fatalf("raw subcommand must not expose --clean")
	}
}

// --- engines subcommand assembly --------------------------------------------

// getEnginesSubcommand returns the `engines` subcommand of `sx searxng`, or nil.
func getEnginesSubcommand(t *testing.T) *cobra.Command {
	t.Helper()
	c := newSearxngCmd()
	for _, sub := range c.Commands() {
		if sub.Name() == "engines" {
			return sub
		}
	}
	return nil
}

func TestNewSearxngCmd_HasEnginesSubcommand(t *testing.T) {
	oldConfig := config
	config = getDefaultConfig()
	defer func() { config = oldConfig }()

	c := newSearxngCmd()
	var hasRaw, hasEngines bool
	for _, sub := range c.Commands() {
		switch sub.Name() {
		case "raw":
			hasRaw = true
		case "engines":
			hasEngines = true
		}
	}
	if !hasRaw {
		t.Errorf("searxng command should still have a raw subcommand")
	}
	if !hasEngines {
		t.Errorf("searxng command should have an engines subcommand")
	}
}

func TestNewSearxngEnginesCmd_HasExactlySevenFlags(t *testing.T) {
	oldConfig := config
	config = getDefaultConfig()
	defer func() { config = oldConfig }()

	engines := getEnginesSubcommand(t)
	if engines == nil {
		t.Fatalf("searxng command should have an engines subcommand")
	}

	for _, flag := range []string{"json", "category", "filter", "engines", "enabled", "all", "live"} {
		if engines.Flags().Lookup(flag) == nil {
			t.Errorf("engines subcommand should expose --%s", flag)
		}
	}
	for _, flag := range []string{"clean", "diagnostics"} {
		if engines.Flags().Lookup(flag) != nil {
			t.Errorf("engines subcommand must NOT expose --%s", flag)
		}
	}

	// Exactly 7 flags, nothing else (count only local flags).
	count := 0
	engines.Flags().VisitAll(func(*pflag.Flag) { count++ })
	if count != 7 {
		t.Errorf("engines subcommand should expose exactly 7 flags, got %d", count)
	}
}

// --enabled and --all are mutually exclusive. cobra validates flag groups before
// Run (ValidateFlagGroups runs ahead of cmd.Run in execute()), so dispatching the
// engines subcommand via its parent returns an error without any network access.
func TestNewSearxngEnginesCmd_EnabledAndAllMutuallyExclusive(t *testing.T) {
	oldConfig := config
	config = getDefaultConfig()
	defer func() { config = oldConfig }()

	// Execute via the parent so cobra dispatches to the engines subcommand
	// correctly; running Execute() on the isolated child re-roots to the parent
	// and would mis-dispatch the child flags.
	c := newSearxngCmd()
	c.SilenceErrors = true
	c.SilenceUsage = true
	c.SetArgs([]string{"engines", "--enabled", "--all"})

	err := c.Execute()
	if err == nil {
		t.Fatal("expected error: --enabled and --all are mutually exclusive")
	}
	if !contains(err.Error(), "enabled") || !contains(err.Error(), "all") {
		t.Errorf("expected mutual-exclusion error mentioning enabled/all, got %v", err)
	}
}

func TestValidateRawSearxngOptions_NormalizesAndExpands(t *testing.T) {
	opts := &SearchOptions{
		Categories: []string{"social-media"},
		SafeSearch: "strict",
		TimeRange:  "w",
	}
	if err := validateRawSearxngOptions(opts); err != nil {
		t.Fatalf("validateRawSearxngOptions failed: %v", err)
	}
	if opts.Categories[0] != "social media" {
		t.Fatalf("category = %q, want social media", opts.Categories[0])
	}
	if opts.TimeRange != "week" {
		t.Fatalf("time range = %q, want week", opts.TimeRange)
	}
}

func TestValidateRawSearxngOptions_InvalidSafeSearch(t *testing.T) {
	opts := &SearchOptions{SafeSearch: "maybe"}
	if err := validateRawSearxngOptions(opts); err == nil {
		t.Fatalf("expected invalid safe-search error")
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

// --- filterSearxngEngines: pure, network-free -------------------------------

// sampleEngines is a fixed in-memory slice mixing enabled/disabled, varied
// categories, names, and shortcuts.
func sampleEngines() []backends.SearxngEngineConfig {
	return []backends.SearxngEngineConfig{
		{Name: "Google", Shortcut: "go", Categories: []string{"general", "web"}, Enabled: true},
		{Name: "google news", Shortcut: "gon", Categories: []string{"news"}, Enabled: true},
		{Name: "bing", Shortcut: "bi", Categories: []string{"general"}, Enabled: false},
		{Name: "duckduckgo", Shortcut: "ddg", Categories: []string{"general", "web"}, Enabled: true},
		{Name: "wikipedia", Shortcut: "wp", Categories: []string{"general"}, Enabled: false},
	}
}

func names(engines []backends.SearxngEngineConfig) []string {
	out := make([]string, len(engines))
	for i, e := range engines {
		out[i] = e.Name
	}
	return out
}

func TestFilterSearxngEngines_EnabledOnlyDropsDisabled(t *testing.T) {
	got := filterSearxngEngines(sampleEngines(), enginesFilters{EnabledOnly: true})
	for _, e := range got {
		if !e.Enabled {
			t.Errorf("EnabledOnly should drop disabled engine %q", e.Name)
		}
	}
	// enabled: Google, google news, duckduckgo -> 3
	if len(got) != 3 {
		t.Fatalf("expected 3 enabled engines, got %d (%v)", len(got), names(got))
	}
}

func TestFilterSearxngEngines_AllKeepsDisabled(t *testing.T) {
	got := filterSearxngEngines(sampleEngines(), enginesFilters{EnabledOnly: false})
	if len(got) != 5 {
		t.Fatalf("expected all 5 engines with EnabledOnly=false, got %d (%v)", len(got), names(got))
	}
}

func TestFilterSearxngEngines_CategoryFilter(t *testing.T) {
	got := filterSearxngEngines(sampleEngines(), enginesFilters{EnabledOnly: false, Category: "news"})
	if len(got) != 1 || got[0].Name != "google news" {
		t.Fatalf("expected only [google news] for category news, got %v", names(got))
	}
}

func TestFilterSearxngEngines_FilterMatchesNameAndShortcutCaseInsensitive(t *testing.T) {
	// Uppercase substring of name "Google" should match case-insensitively.
	gotName := filterSearxngEngines(sampleEngines(), enginesFilters{EnabledOnly: false, Filter: "GOOG"})
	if len(gotName) != 2 {
		t.Fatalf("expected name-substring match to return Google + google news, got %v", names(gotName))
	}
	// Shortcut "ddg" (uppercased input) should match duckduckgo by shortcut.
	gotShortcut := filterSearxngEngines(sampleEngines(), enginesFilters{EnabledOnly: false, Filter: "DDG"})
	if len(gotShortcut) != 1 || gotShortcut[0].Name != "duckduckgo" {
		t.Fatalf("expected shortcut match to return duckduckgo, got %v", names(gotShortcut))
	}
}

func TestFilterSearxngEngines_EnginesExactNameCaseInsensitive(t *testing.T) {
	got := filterSearxngEngines(sampleEngines(), enginesFilters{EnabledOnly: false, Engines: []string{"GOOGLE"}})
	if len(got) != 1 || got[0].Name != "Google" {
		t.Fatalf("expected exact case-insensitive name match [Google], got %v", names(got))
	}
}

func TestFilterSearxngEngines_CombinedFiltersAND(t *testing.T) {
	// Category general AND filter "goog" -> only "Google" (google news is news,
	// not general; bing/wikipedia are general but do not match "goog").
	got := filterSearxngEngines(sampleEngines(), enginesFilters{
		EnabledOnly: false,
		Category:    "general",
		Filter:      "goog",
	})
	if len(got) != 1 || got[0].Name != "Google" {
		t.Fatalf("expected combined filters to AND to [Google], got %v", names(got))
	}
}

func TestFilterSearxngEngines_SortedByNameAscending(t *testing.T) {
	got := filterSearxngEngines(sampleEngines(), enginesFilters{EnabledOnly: false})
	for i := 1; i < len(got); i++ {
		if got[i-1].Name > got[i].Name {
			t.Fatalf("expected sorted-by-name ascending, got %v", names(got))
		}
	}
}

func TestFilterSearxngEngines_EmptyMatchReturnsNonNilEmpty(t *testing.T) {
	got := filterSearxngEngines(sampleEngines(), enginesFilters{EnabledOnly: false, Filter: "no-such-engine"})
	if got == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %v", names(got))
	}
}

// --- validateEnginesLiveOptions: pure, network-free -------------------------

func TestValidateEnginesLiveOptions(t *testing.T) {
	tests := []struct {
		name    string
		live    bool
		engines []string
		wantErr bool
	}{
		{"not live, no engines", false, nil, false},
		{"live, no engines", true, nil, true},
		{"live, one engine", true, []string{"a"}, false},
		{"live, six engines", true, []string{"a", "b", "c", "d", "e", "f"}, true},
		{"live, five engines", true, []string{"a", "b", "c", "d", "e"}, false},
	}
	for _, tt := range tests {
		err := validateEnginesLiveOptions(tt.live, tt.engines)
		if tt.wantErr && err == nil {
			t.Errorf("%s: expected error, got nil", tt.name)
		}
		if !tt.wantErr && err != nil {
			t.Errorf("%s: expected nil, got %v", tt.name, err)
		}
	}
}

// --- emitSearxngEnginesSuccess JSON array invariants ------------------------

// captureStdout redirects os.Stdout while fn runs and returns what was written.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("copy: %v", err)
	}
	return buf.String()
}

func TestEmitSearxngEnginesSuccess_EmptyArraysNeverNull(t *testing.T) {
	out := captureStdout(t, func() {
		emitSearxngEnginesSuccess(enginesFilters{EnabledOnly: true}, []backends.SearxngEngineConfig{}, nil)
	})

	var env searxngEnginesEnvelope
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v\noutput:\n%s", err, out)
	}
	if !env.OK {
		t.Errorf("ok = %v, want true", env.OK)
	}
	if env.Source != "searxng_config" {
		t.Errorf("source = %q, want searxng_config", env.Source)
	}
	if env.Error != nil {
		t.Errorf("error = %#v, want nil", env.Error)
	}

	// Re-check raw JSON so we catch null vs [] (typed struct would hide it).
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if string(raw["engines"]) != "[]" {
		t.Errorf("engines should be [] even when empty, got %s", raw["engines"])
	}
	if string(raw["warnings"]) != "[]" {
		t.Errorf("warnings should be [] even when empty, got %s", raw["warnings"])
	}
	if string(raw["error"]) != "null" {
		t.Errorf("error should be null on success, got %s", raw["error"])
	}
}

func TestEmitSearxngEnginesSuccess_NonEmptyCounts(t *testing.T) {
	filtered := []backends.SearxngEngineConfig{
		{Name: "google", Shortcut: "go", Categories: []string{"general"}, Enabled: true, Timeout: 3.0},
		{Name: "bing", Shortcut: "bi", Categories: []string{"general"}, Enabled: true, Timeout: 4.0},
	}
	out := captureStdout(t, func() {
		emitSearxngEnginesSuccess(enginesFilters{EnabledOnly: true}, filtered, nil)
	})

	var env searxngEnginesEnvelope
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v\noutput:\n%s", err, out)
	}
	if env.EngineCount != len(filtered) {
		t.Errorf("engine_count = %d, want %d", env.EngineCount, len(filtered))
	}
	if len(env.Engines) != len(filtered) {
		t.Errorf("engines length = %d, want %d", len(env.Engines), len(filtered))
	}
	if env.Warnings == nil {
		t.Errorf("warnings should be a non-nil array")
	}
}
