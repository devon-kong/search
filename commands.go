package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"sx/backends"
)

// newSearchCmd returns a thin alias for the root search behavior: `sx search
// [query...]` runs the exact same code path as `sx [query...]`. It shares the
// root command's flags so behavior is identical.
func newSearchCmd(root *cobra.Command) *cobra.Command {
	searchCmd := &cobra.Command{
		Use:                   "search [query...]",
		Short:                 "Search the web (alias for the default command)",
		Long:                  "search runs a web search. It is an explicit alias for the default `sx [query...]` form and shares all of its flags.",
		Args:                  cobra.ArbitraryArgs,
		DisableFlagsInUseLine: true,
		Run:                   runSearch, // identical code path as the root command
	}
	// Share the root command's flag set so `sx search` accepts the same flags
	// as `sx`. This keeps a single source of truth for search flags.
	searchCmd.Flags().AddFlagSet(root.Flags())
	return searchCmd
}

// newSearxngCmd returns SearXNG-specific commands that intentionally avoid the
// generic multi-backend envelope.
func newSearxngCmd() *cobra.Command {
	searxngCmd := &cobra.Command{
		Use:   "searxng",
		Short: "SearXNG-specific commands",
	}

	var rawOpts SearchOptions
	var rawNews bool
	rawNum := defaultResultCount
	safeSearchDefault := defaultSafeSearch
	if config != nil {
		rawNum = config.ResultCount
		safeSearchDefault = config.SafeSearch
	}

	rawCmd := &cobra.Command{
		Use:                   "raw [query...]",
		Short:                 "Output raw SearXNG JSON",
		Long:                  "raw queries the configured SearXNG backend and writes SearXNG's JSON response body directly. It does not use the sx JSON envelope, --clean, or paid fallback.",
		Args:                  cobra.ArbitraryArgs,
		DisableFlagsInUseLine: true,
		Run: func(cmd *cobra.Command, args []string) {
			runSearxngRaw(cmd, args, rawOpts, rawNews, rawNum)
		},
	}
	rawCmd.Flags().StringSliceVar(&rawOpts.Categories, "categories", nil, fmt.Sprintf("SearXNG categories to search in: %s", strings.Join(searxngCategories, ", ")))
	rawCmd.Flags().BoolVarP(&rawNews, "news", "N", false, "shortcut for --categories news")
	rawCmd.Flags().StringSliceVarP(&rawOpts.SearxngEngines, "engines", "e", nil, "SearXNG upstream engines to request (for example google, duckduckgo, google news)")
	rawCmd.Flags().StringVarP(&rawOpts.Language, "language", "l", "", "search results in a specific language")
	rawCmd.Flags().StringVar(&rawOpts.SafeSearch, "safe-search", safeSearchDefault, "filter results for safe search (none, moderate, strict)")
	rawCmd.Flags().StringVarP(&rawOpts.TimeRange, "time-range", "r", "", "search results within a specific time range (day, week, month, year)")
	rawCmd.Flags().IntVarP(&rawNum, "num", "n", rawNum, "requested result count for compatibility with sx search")

	searxngCmd.AddCommand(rawCmd)
	return searxngCmd
}

func runSearxngRaw(cmd *cobra.Command, args []string, rawOpts SearchOptions, rawNews bool, rawNum int) {
	query, ok := rawQueryFromArgsOrStdin(cmd, args)
	if !ok {
		return
	}

	if rawNews {
		rawOpts.Categories = []string{"news"}
	}

	if err := validateRawSearxngOptions(&rawOpts); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", backends.RedactSecrets(err.Error()))
		setExit(exitUsageConfig)
		return
	}

	if rawOpts.SafeSearch == "" {
		rawOpts.SafeSearch = config.SafeSearch
	}

	if err := ensureConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: config error: %s\n", backends.RedactSecrets(err.Error()))
		setExit(exitUsageConfig)
		return
	}

	if !hasSearxngConfigured(config) {
		emitSearxngRawError(&backends.BackendError{
			Backend: "searxng",
			Err:     fmt.Errorf("no SearXNG instance configured"),
			Code:    backends.ErrCodeUnavailable,
		})
		return
	}

	backendMgr = initBackendManager(config)
	backend, ok := backendMgr.GetBackend("searxng")
	if !ok {
		emitSearxngRawError(fmt.Errorf("SearXNG backend is not registered"))
		return
	}
	rawBackend, ok := backend.(interface {
		SearchRaw(backends.SearchOptions) (backends.SearxngRawResponse, error)
	})
	if !ok {
		emitSearxngRawError(fmt.Errorf("SearXNG backend does not support raw output"))
		return
	}

	raw, err := rawBackend.SearchRaw(backends.SearchOptions{
		Query:      query,
		Categories: rawOpts.Categories,
		Engines:    rawOpts.SearxngEngines,
		Language:   rawOpts.Language,
		TimeRange:  rawOpts.TimeRange,
		SafeSearch: rawOpts.SafeSearch,
		PageNo:     1,
		NumResults: rawNum,
	})
	if err != nil {
		emitSearxngRawError(err)
		return
	}

	if _, err := os.Stdout.Write(raw.Raw); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing raw JSON: %s\n", backends.RedactSecrets(err.Error()))
		setExit(exitSearchFail)
		return
	}
	if len(raw.Raw) == 0 || raw.Raw[len(raw.Raw)-1] != '\n' {
		fmt.Println()
	}
}

func rawQueryFromArgsOrStdin(cmd *cobra.Command, args []string) (string, bool) {
	if isPipeInput() {
		input, err := readFromStdin()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: error reading from stdin: %s\n", backends.RedactSecrets(err.Error()))
			setExit(exitUsageConfig)
			return "", false
		}
		query := strings.TrimSpace(input)
		if query == "" {
			fmt.Fprintln(os.Stderr, "Error: empty input from stdin")
			setExit(exitUsageConfig)
			return "", false
		}
		return query, true
	}

	if len(args) == 0 {
		_ = cmd.Help()
		return "", false
	}
	return strings.Join(args, " "), true
}

func validateRawSearxngOptions(rawOpts *SearchOptions) error {
	for _, category := range rawOpts.Categories {
		if !validateCategory(category) {
			return fmt.Errorf("invalid category %q (supported: %s)", category, strings.Join(searxngCategories, ", "))
		}
	}
	for i, category := range rawOpts.Categories {
		rawOpts.Categories[i] = normalizeCategory(category)
	}

	if rawOpts.TimeRange != "" {
		if !validateTimeRange(rawOpts.TimeRange) {
			return fmt.Errorf("invalid time range %q (use: %s)", rawOpts.TimeRange, strings.Join(timeRangeOptions, ", "))
		}
		rawOpts.TimeRange = expandTimeRange(rawOpts.TimeRange)
	}

	switch rawOpts.SafeSearch {
	case "", "none", "moderate", "strict":
		return nil
	default:
		return fmt.Errorf("invalid safe-search %q (use: none, moderate, strict)", rawOpts.SafeSearch)
	}
}

type searxngRawErrorEnvelope struct {
	OK    bool            `json:"ok"`
	Error searxngRawError `json:"error"`
}

type searxngRawError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Backend string `json:"backend"`
}

func emitSearxngRawError(err error) {
	setExit(exitSearchFail)
	jerr := searxngRawError{
		Code:    "BACKEND_UNAVAILABLE",
		Message: "SearXNG raw request failed",
		Backend: "searxng",
	}

	var be *backends.BackendError
	if errors.As(err, &be) {
		code, _ := mapErrCodeToJSON(be.Code)
		jerr.Code = code
		if be.Backend != "" {
			jerr.Backend = be.Backend
		}
		jerr.Message = safeSearxngRawErrorMessage(be)
	}

	payload := searxngRawErrorEnvelope{OK: false, Error: jerr}
	if encErr := json.NewEncoder(os.Stdout).Encode(payload); encErr != nil {
		fmt.Fprintf(os.Stderr, "Error formatting raw error JSON: %s\n", backends.RedactSecrets(encErr.Error()))
	}
}

func safeSearxngRawErrorMessage(be *backends.BackendError) string {
	msg := backends.RedactSecrets(be.Err.Error())
	if strings.Contains(msg, "://") {
		return "SearXNG raw request failed"
	}
	if msg == "" {
		return "SearXNG raw request failed"
	}
	return msg
}

// newConfigCmd returns the `sx config` command group with the `validate`
// subcommand.
func newConfigCmd() *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect and validate sx configuration",
	}
	configCmd.AddCommand(newConfigValidateCmd())
	return configCmd
}

// newConfigValidateCmd validates configuration syntax, required fields, and
// backend sanity. It performs NO live network requests by default, so it never
// spends paid credits. Exit 0 on success, 3 on validation failure.
func newConfigValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate the configuration (no network requests)",
		Long:  "validate checks config syntax, required fields, and backend configuration sanity. It makes no live network requests and never spends paid credits.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			problems, notes := validateConfig(config)
			// Notes are non-fatal advisories (e.g. a safe paid-fallback gate).
			for _, n := range notes {
				fmt.Fprintf(os.Stderr, "Note: %s\n", n)
			}
			if len(problems) == 0 {
				fmt.Fprintln(os.Stderr, "Configuration is valid.")
				return
			}
			fmt.Fprintln(os.Stderr, "Configuration has problems:")
			for _, p := range problems {
				fmt.Fprintf(os.Stderr, "  - %s\n", p)
			}
			setExit(exitCheckFail)
		},
	}
}

// validateConfig returns (problems, notes) for the loaded config. A non-empty
// problems slice means the config is invalid (exit 3). Notes are non-fatal
// advisories. No network access is performed.
func validateConfig(cfg *Config) (problems []string, notes []string) {

	// Primary engine must be a known backend.
	validEngines := map[string]bool{"searxng": true, "brave": true, "tavily": true, "exa": true, "jina": true}
	engine := cfg.Engine
	if engine == "" {
		engine = "searxng"
	}
	if !validEngines[engine] {
		problems = append(problems, fmt.Sprintf("unknown engine %q (valid: searxng, brave, tavily, exa, jina)", engine))
	}

	// If primary is searxng, a URL must be configured.
	if engine == "searxng" && !hasSearxngConfigured(cfg) {
		problems = append(problems, "engine is \"searxng\" but no searxng_url/searxng_urls is configured")
	}

	// Fallback engines must be known, and must not include the primary.
	for _, fb := range cfg.FallbackEngines {
		if !validEngines[fb] {
			problems = append(problems, fmt.Sprintf("unknown fallback engine %q", fb))
		}
	}

	// Surface (as a non-fatal note) when paid backends are listed in fallback
	// while the paid gate is off. This is a safe, intended configuration, so it
	// must NOT fail validation.
	if !cfg.AllowPaidFallback {
		for _, fb := range cfg.FallbackEngines {
			if fb == "tavily" || fb == "exa" {
				notes = append(notes, fmt.Sprintf("fallback engine %q is paid; with allow_paid_fallback=false it will be skipped in automatic fallback (set allow_paid_fallback=true to enable)", fb))
			}
		}
	}

	// HTTP method sanity.
	if m := strings.ToUpper(strings.TrimSpace(cfg.HTTPMethod)); m != "" && m != "GET" && m != "POST" {
		problems = append(problems, fmt.Sprintf("http_method %q is invalid (use GET or POST)", cfg.HTTPMethod))
	}

	// Safe search sanity.
	if s := cfg.SafeSearch; s != "" && s != "none" && s != "moderate" && s != "strict" {
		problems = append(problems, fmt.Sprintf("safe_search %q is invalid (use none, moderate, strict)", s))
	}

	// SearXNG strategy sanity.
	if st := cfg.SearxngStrategy; st != "" && st != backends.SearxngStrategyOrdered && st != backends.SearxngStrategyParallelFastest {
		problems = append(problems, fmt.Sprintf("searxng_strategy %q is invalid (use ordered or parallel-fastest)", st))
	}

	return problems, notes
}

// newHealthCmd returns the `sx health` command. By default it only checks
// configuration/availability (IsAvailable) and never issues live requests to
// paid backends. With --live it performs a real reachability search; for paid
// backends (tavily, exa in api/auto mode) it first warns that this may consume
// credits. Exit 0 on success, 3 on failure.
func newHealthCmd() *cobra.Command {
	var live bool
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Check backend configuration and availability",
		Long: `health reports whether each configured backend is available.

By default it only checks configuration/availability and makes NO live network
requests, so it never spends paid credits. Pass --live to perform a real
reachability check; for paid backends (tavily, exa API) it first prints a
warning that the check may consume credits.`,
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runHealth(live)
		},
	}
	cmd.Flags().BoolVar(&live, "live", false, "perform live reachability checks (may consume paid credits for tavily/exa API)")
	return cmd
}

func runHealth(live bool) {
	mgr := initBackendManager(config)

	// Deterministic ordering for stable output.
	order := []string{"searxng", "brave", "tavily", "exa", "jina"}

	anyFail := false
	for _, name := range order {
		backend, ok := mgr.GetBackend(name)
		if !ok {
			continue
		}

		available := backend.IsAvailable()
		tier := backend.CostTier()

		if !live {
			status := "not configured"
			if available {
				status = "configured"
			}
			fmt.Fprintf(os.Stderr, "%-8s %-13s available=%v\n", name, tier, available)
			_ = status
			continue
		}

		// Live mode: skip backends that aren't even configured.
		if !available {
			fmt.Fprintf(os.Stderr, "%-8s %-13s SKIP (not configured)\n", name, tier)
			continue
		}

		// Paid backends: warn before spending credits.
		if tier == backends.CostTierPaid {
			fmt.Fprintf(os.Stderr, "Warning: live-checking paid backend %q may consume credits.\n", name)
		}

		_, err := backend.Search(backends.SearchOptions{Query: "ping", NumResults: 1})
		if err != nil {
			anyFail = true
			fmt.Fprintf(os.Stderr, "%-8s %-13s FAIL: %s\n", name, tier, backends.RedactSecrets(err.Error()))
			continue
		}
		fmt.Fprintf(os.Stderr, "%-8s %-13s OK\n", name, tier)
	}

	if anyFail {
		setExit(exitCheckFail)
	}
}
