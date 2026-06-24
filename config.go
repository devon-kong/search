package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Schema          string   `toml:"$schema,omitempty"`
	SearxngURL      string   `toml:"searxng_url"`
	SearxngURLs     []string `toml:"searxng_urls,omitempty"`
	SearxngStrategy string   `toml:"searxng_strategy,omitempty"`
	SearxngUsername string   `toml:"searxng_username,omitempty"`
	SearxngPassword string   `toml:"searxng_password,omitempty"`
	ResultCount     int      `toml:"result_count"`
	Categories      []string `toml:"categories,omitempty"`
	SafeSearch      string   `toml:"safe_search"`
	Engines         []string `toml:"engines,omitempty"`
	Expand          bool     `toml:"expand"`
	Language        string   `toml:"language,omitempty"`
	HTTPMethod      string   `toml:"http_method"`
	Timeout         float64  `toml:"timeout"`
	NoVerifySSL     bool     `toml:"no_verify_ssl"`
	NoUserAgent     bool     `toml:"no_user_agent"`
	NoColor         bool     `toml:"no_color"`
	URLHandler      string   `toml:"url_handler,omitempty"`
	Debug           bool     `toml:"debug"`
	DefaultOutput   string   `toml:"default_output,omitempty"`
	HistoryEnabled  bool     `toml:"history_enabled"`
	MaxHistory      int      `toml:"max_history"`

	// Multi-engine support
	Engine            string       `toml:"engine"`
	FallbackEngines   []string     `toml:"fallback_engines,omitempty"`
	AllowPaidFallback bool         `toml:"allow_paid_fallback"`
	EnginesBrave      BraveConfig  `toml:"engines_brave"`
	EnginesTavily     TavilyConfig `toml:"engines_tavily"`
	EnginesExa        ExaConfig    `toml:"engines_exa"`
	EnginesJina       JinaConfig   `toml:"engines_jina"`
}

// BraveConfig holds Brave Search API configuration
type BraveConfig struct {
	APIKey  string `toml:"api_key,omitempty"`
	BaseURL string `toml:"base_url,omitempty"`
}

// TavilyConfig holds Tavily Search API configuration
type TavilyConfig struct {
	APIKey            string `toml:"api_key,omitempty"`
	BaseURL           string `toml:"base_url,omitempty"`
	SearchDepth       string `toml:"search_depth,omitempty"`
	IncludeRawContent bool   `toml:"include_raw_content,omitempty"`
	IncludeAnswer     bool   `toml:"include_answer,omitempty"`
}

// ExaConfig holds Exa backend config for API and MCP modes.
type ExaConfig struct {
	Mode       string `toml:"mode,omitempty"` // auto | api | mcp
	APIKey     string `toml:"api_key,omitempty"`
	BaseURL    string `toml:"base_url,omitempty"`
	MCPURL     string `toml:"mcp_url,omitempty"`
	MCPTool    string `toml:"mcp_tool,omitempty"`
	NumResults int    `toml:"num_results,omitempty"`
}

// JinaConfig holds Jina backend config.
type JinaConfig struct {
	APIKey       string `toml:"api_key,omitempty"`
	AllowKeyless bool   `toml:"allow_keyless"`
	BaseURL      string `toml:"base_url,omitempty"`
}

const (
	defaultSearxngURL      = "https://searxng.example.com"
	defaultSearxngStrategy = "ordered"
	defaultResultCount     = 10
	defaultSafeSearch      = "strict"
	defaultHTTPMethod      = "GET"
	defaultTimeout         = 30.0
	defaultExpand          = false
	defaultNoVerifySSL     = false
	defaultNoUserAgent     = false
	defaultNoColor         = false
	defaultDebug           = false
	defaultDefaultOutput   = ""
	defaultHistoryEnabled  = true
	defaultMaxHistory      = 100
)

var defaultURLHandlers = map[string]string{
	"darwin":  "open",
	"linux":   "xdg-open",
	"windows": "explorer",
}

// configHomeDir returns the base XDG config directory (honors XDG_CONFIG_HOME).
func configHomeDir() string {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		configHome = filepath.Join(homeDir, ".config")
	}
	return configHome
}

// getConfigDir returns the canonical config directory: ~/.config/search-engine
// (or $XDG_CONFIG_HOME/search-engine).
func getConfigDir() string {
	base := configHomeDir()
	if base == "" {
		return ""
	}
	return filepath.Join(base, "search-engine")
}

// getLegacyConfigDir returns the previous config directory (~/.config/sx). Used
// only as a read-only migration source; sx never writes here.
func getLegacyConfigDir() string {
	base := configHomeDir()
	if base == "" {
		return ""
	}
	return filepath.Join(base, "sx")
}

func getDefaultConfig() *Config {
	return &Config{
		SearxngURL:      "",
		SearxngStrategy: defaultSearxngStrategy,
		ResultCount:     defaultResultCount,
		SafeSearch:      defaultSafeSearch,
		Expand:          defaultExpand,
		HTTPMethod:      defaultHTTPMethod,
		Timeout:         defaultTimeout,
		NoVerifySSL:     defaultNoVerifySSL,
		NoUserAgent:     defaultNoUserAgent,
		NoColor:         defaultNoColor,
		Debug:           defaultDebug,
		DefaultOutput:   defaultDefaultOutput,
		HistoryEnabled:  defaultHistoryEnabled,
		MaxHistory:      defaultMaxHistory,
		Engine:          "searxng",
		EnginesTavily: TavilyConfig{
			SearchDepth: "basic",
		},
		EnginesExa: ExaConfig{
			Mode:       "auto",
			MCPTool:    "exa-web-search",
			NumResults: 10,
		},
		EnginesJina: JinaConfig{
			AllowKeyless: true,
			BaseURL:      "https://s.jina.ai",
		},
	}
}

func loadConfig() (*Config, error) {
	configDir := getConfigDir()
	configFile := filepath.Join(configDir, "config.toml")

	config := getDefaultConfig()

	// Resolve which file to read. Prefer the canonical directory. If it has no
	// config but the legacy ~/.config/sx/config.toml exists, read that as a
	// read-only migration bridge (we never write to the legacy location).
	//
	// To hard-cut and drop legacy support, delete this migration block.
	readFile := ""
	if _, err := os.Stat(configFile); err == nil {
		readFile = configFile
	} else {
		legacyFile := filepath.Join(getLegacyConfigDir(), "config.toml")
		if _, lerr := os.Stat(legacyFile); lerr == nil {
			readFile = legacyFile
			fmt.Fprintf(os.Stderr,
				"Notice: using legacy config %s. The new location is %s; copy your config there to silence this notice (the old file is left untouched).\n",
				legacyFile, configFile)
		}
	}

	if readFile != "" {
		if _, err := toml.DecodeFile(readFile, config); err != nil {
			return nil, fmt.Errorf("failed to load config: %v", err)
		}
	}

	config.SearxngURLs = deduplicateStrings(config.SearxngURLs)
	if config.SearxngStrategy == "" {
		config.SearxngStrategy = defaultSearxngStrategy
	}
	if config.EnginesExa.Mode == "" {
		config.EnginesExa.Mode = "auto"
	}
	if config.EnginesExa.MCPTool == "" {
		config.EnginesExa.MCPTool = "exa-web-search"
	}
	if config.EnginesExa.NumResults <= 0 {
		config.EnginesExa.NumResults = 10
	}
	if config.EnginesJina.BaseURL == "" {
		config.EnginesJina.BaseURL = "https://s.jina.ai"
	}

	return config, nil
}

func ensureConfig() error {
	configDir := getConfigDir()
	configFile := filepath.Join(configDir, "config.toml")

	// Canonical config already exists: nothing to do.
	if _, err := os.Stat(configFile); err == nil {
		return nil
	}

	// Read-only migration bridge: if a legacy ~/.config/sx/config.toml exists,
	// keep using it and do NOT create a new (empty) file in the canonical dir,
	// which would otherwise shadow the legacy config on the next run.
	legacyFile := filepath.Join(getLegacyConfigDir(), "config.toml")
	if _, err := os.Stat(legacyFile); err == nil {
		return nil
	}

	// No config anywhere: create one in the canonical directory.
	return createConfigFile(configDir, configFile)
}

func deduplicateStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func hasSearxngConfigured(config *Config) bool {
	if strings.TrimSpace(config.SearxngURL) != "" {
		return true
	}
	for _, u := range config.SearxngURLs {
		if strings.TrimSpace(u) != "" {
			return true
		}
	}
	return false
}

// interactiveConfigPrompt reports whether it is safe to prompt the user for
// config on stdin: stdout must be a TTY, input must not be piped, and machine
// output (--json) must not be requested.
func interactiveConfigPrompt() bool {
	if searchOpts.JSON {
		return false
	}
	if !isTerminal(os.Stdout) || isPipeInput() {
		return false
	}
	return true
}

func createConfigFile(configDir, configFile string) error {
	// Create config directory if it doesn't exist
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}

	// Determine SearXNG URL. Only prompt when running interactively; in
	// non-TTY / piped / --json contexts we must never block on stdin. In those
	// cases we seed an empty URL so the downstream "no SearXNG configured"
	// check produces a structured config error (exit code 2) instead of hanging.
	searxngURL := ""
	if interactiveConfigPrompt() {
		fmt.Printf("Enter your SearXNG instance URL [%s]: ", defaultSearxngURL)
		fmt.Scanln(&searxngURL)
		if strings.TrimSpace(searxngURL) == "" {
			searxngURL = defaultSearxngURL
		}
	}

	// Create default config
	config := &Config{
		SearxngURL:      searxngURL,
		SearxngStrategy: defaultSearxngStrategy,
		ResultCount:     defaultResultCount,
		SafeSearch:      defaultSafeSearch,
		Expand:          defaultExpand,
		HTTPMethod:      defaultHTTPMethod,
		Timeout:         defaultTimeout,
		NoVerifySSL:     defaultNoVerifySSL,
		NoUserAgent:     defaultNoUserAgent,
		NoColor:         defaultNoColor,
		Debug:           defaultDebug,
	}

	// Write config to file
	file, err := os.Create(configFile)
	if err != nil {
		return err
	}
	defer file.Close()

	// Write schema reference and header
	_, err = file.WriteString(`"$schema" = "https://raw.githubusercontent.com/byteowlz/schemas/refs/heads/main/sx/sx.config.schema.json"

# sx configuration file
`)
	if err != nil {
		return err
	}

	encoder := toml.NewEncoder(file)
	if err := encoder.Encode(config); err != nil {
		return err
	}

	// Status to stderr so stdout stays clean for machine-readable output.
	fmt.Fprintf(os.Stderr, "Created config file: %s\n", configFile)
	return nil
}
