package main

import (
	"os"
	"path/filepath"
	"testing"
)

// withTempConfigHome points XDG_CONFIG_HOME at a fresh temp dir for the test.
func withTempConfigHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestGetConfigDir_CanonicalSearchEngine(t *testing.T) {
	dir := withTempConfigHome(t)
	got := getConfigDir()
	want := filepath.Join(dir, "search-engine")
	if got != want {
		t.Errorf("getConfigDir() = %q, want %q", got, want)
	}
}

func TestGetLegacyConfigDir_IsSx(t *testing.T) {
	dir := withTempConfigHome(t)
	got := getLegacyConfigDir()
	want := filepath.Join(dir, "sx")
	if got != want {
		t.Errorf("getLegacyConfigDir() = %q, want %q", got, want)
	}
}

// Canonical dir present -> use it (and NOT the legacy file).
func TestLoadConfig_PrefersCanonical(t *testing.T) {
	dir := withTempConfigHome(t)
	writeFile(t, filepath.Join(dir, "search-engine", "config.toml"), `searxng_url = "https://new.example.com"`+"\n")
	writeFile(t, filepath.Join(dir, "sx", "config.toml"), `searxng_url = "https://old.example.com"`+"\n")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.SearxngURL != "https://new.example.com" {
		t.Errorf("expected canonical config to win, got %q", cfg.SearxngURL)
	}
}

// Canonical absent, legacy present -> read legacy as a read-only bridge and do
// NOT modify the legacy file.
func TestLoadConfig_LegacyReadOnlyBridge(t *testing.T) {
	dir := withTempConfigHome(t)
	legacyPath := filepath.Join(dir, "sx", "config.toml")
	const legacyContent = `searxng_url = "https://legacy.example.com"` + "\n"
	writeFile(t, legacyPath, legacyContent)

	before, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatalf("read legacy: %v", err)
	}

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.SearxngURL != "https://legacy.example.com" {
		t.Errorf("expected legacy config to be read, got %q", cfg.SearxngURL)
	}

	// Legacy file must be untouched.
	after, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatalf("re-read legacy: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("legacy config file was modified; before=%q after=%q", before, after)
	}

	// And no canonical file should have been created by loadConfig.
	canonical := filepath.Join(dir, "search-engine", "config.toml")
	if _, err := os.Stat(canonical); err == nil {
		t.Errorf("loadConfig must not create a canonical file when bridging legacy")
	}
}

// ensureConfig must NOT shadow the legacy config by creating an empty canonical
// file when only the legacy file exists.
func TestEnsureConfig_DoesNotShadowLegacy(t *testing.T) {
	dir := withTempConfigHome(t)
	writeFile(t, filepath.Join(dir, "sx", "config.toml"), `searxng_url = "https://legacy.example.com"`+"\n")

	if err := ensureConfig(); err != nil {
		t.Fatalf("ensureConfig: %v", err)
	}
	canonical := filepath.Join(dir, "search-engine", "config.toml")
	if _, err := os.Stat(canonical); err == nil {
		t.Errorf("ensureConfig must not create canonical file while legacy exists (would shadow it)")
	}
}

// No config anywhere, non-interactive (searchOpts.JSON true) -> ensureConfig
// creates a canonical file WITHOUT blocking on stdin, seeding an empty URL so
// the downstream "no SearXNG configured" check fires.
func TestEnsureConfig_NonInteractiveCreatesEmptyURL(t *testing.T) {
	dir := withTempConfigHome(t)

	// Force the non-interactive path so createConfigFile never calls Scanln.
	oldJSON := searchOpts.JSON
	searchOpts.JSON = true
	defer func() { searchOpts.JSON = oldJSON }()

	if err := ensureConfig(); err != nil {
		t.Fatalf("ensureConfig: %v", err)
	}
	canonical := filepath.Join(dir, "search-engine", "config.toml")
	if _, err := os.Stat(canonical); err != nil {
		t.Fatalf("expected canonical config to be created, got %v", err)
	}

	// Reload and confirm searxng URL is empty (not the interactive default),
	// so hasSearxngConfigured is false and the structured config error path runs.
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if hasSearxngConfigured(cfg) {
		t.Errorf("expected no SearXNG configured after non-interactive create, got url=%q", cfg.SearxngURL)
	}
}

func TestLoadConfig_AllowPaidFallbackDefaultsFalse(t *testing.T) {
	withTempConfigHome(t)
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.AllowPaidFallback {
		t.Errorf("AllowPaidFallback should default to false")
	}
	if len(cfg.FallbackEngines) != 0 {
		t.Errorf("FallbackEngines should default to empty, got %v", cfg.FallbackEngines)
	}
}

func TestLoadConfig_ParsesBackendBaseURLs(t *testing.T) {
	dir := withTempConfigHome(t)
	writeFile(t, filepath.Join(dir, "search-engine", "config.toml"), `
searxng_url = "https://searx.example.com"

[engines_brave]
base_url = "https://brave.mock"

[engines_tavily]
base_url = "https://tavily.mock"

[engines_exa]
base_url = "https://exa.mock"

[engines_jina]
base_url = "https://jina.mock"
`)
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.EnginesBrave.BaseURL != "https://brave.mock" {
		t.Errorf("brave base_url = %q", cfg.EnginesBrave.BaseURL)
	}
	if cfg.EnginesTavily.BaseURL != "https://tavily.mock" {
		t.Errorf("tavily base_url = %q", cfg.EnginesTavily.BaseURL)
	}
	if cfg.EnginesExa.BaseURL != "https://exa.mock" {
		t.Errorf("exa base_url = %q", cfg.EnginesExa.BaseURL)
	}
	if cfg.EnginesJina.BaseURL != "https://jina.mock" {
		t.Errorf("jina base_url = %q", cfg.EnginesJina.BaseURL)
	}
}
