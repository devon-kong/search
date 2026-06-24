package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

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
	searxngCmd.AddCommand(newSearxngEnginesCmd())
	return searxngCmd
}

// newSearxngEnginesCmd returns the `sx searxng engines` subcommand. It lists the
// upstream engine inventory from SearXNG's /config endpoint. By default it issues
// exactly one GET /config (no /search), reports config-layer enabled engines, and
// never triggers fallback or paid backends.
func newSearxngEnginesCmd() *cobra.Command {
	var jsonOut bool
	var category string
	var filter string
	var engines []string
	var enabledOnly bool
	var all bool
	var live bool

	enginesCmd := &cobra.Command{
		Use:                   "engines",
		Short:                 "List SearXNG upstream engines from /config",
		Long:                  "engines lists the upstream engines from the configured SearXNG instance's /config endpoint. By default it issues a single GET /config (no /search) and reports config-layer enabled engines. enabled=true is a config-layer flag, not a live-search guarantee. Use --live to probe specific engines. It never uses the sx JSON envelope, --clean, or paid fallback.",
		Args:                  cobra.NoArgs,
		DisableFlagsInUseLine: true,
		Run: func(cmd *cobra.Command, args []string) {
			runSearxngEngines(jsonOut, category, filter, engines, all, live)
		},
	}
	enginesCmd.Flags().BoolVar(&jsonOut, "json", false, "output engine inventory as a JSON envelope")
	enginesCmd.Flags().StringVar(&category, "category", "", fmt.Sprintf("filter by ONE category: %s", strings.Join(searxngCategories, ", ")))
	enginesCmd.Flags().StringVar(&filter, "filter", "", "case-insensitive substring match on engine name or shortcut")
	enginesCmd.Flags().StringSliceVar(&engines, "engines", nil, "case-insensitive exact match on engine name (comma-separated)")
	enginesCmd.Flags().BoolVar(&enabledOnly, "enabled", false, "show only config-enabled engines (the default)")
	enginesCmd.Flags().BoolVar(&all, "all", false, "include config-disabled engines")
	enginesCmd.Flags().BoolVar(&live, "live", false, "live-probe the named engines (requires --engines, max 5)")
	enginesCmd.MarkFlagsMutuallyExclusive("enabled", "all")

	return enginesCmd
}

type enginesFilters struct {
	Category    string   // normalized; "" if unset
	Filter      string   // raw; "" if unset
	Engines     []string // exact-name match list; nil/empty = no name filter
	EnabledOnly bool     // true unless --all
	Live        bool
}

// filterSearxngEngines applies all filters with AND semantics and returns the
// result sorted by Name ascending (stable). Pure; no network. Never returns nil
// (return an empty, non-nil slice when nothing matches).
func filterSearxngEngines(engines []backends.SearxngEngineConfig, f enginesFilters) []backends.SearxngEngineConfig {
	lowerFilter := strings.ToLower(f.Filter)

	var nameSet map[string]struct{}
	if len(f.Engines) > 0 {
		nameSet = make(map[string]struct{}, len(f.Engines))
		for _, name := range f.Engines {
			nameSet[strings.ToLower(name)] = struct{}{}
		}
	}

	out := make([]backends.SearxngEngineConfig, 0, len(engines))
	for _, e := range engines {
		if f.EnabledOnly && !e.Enabled {
			continue
		}

		if f.Category != "" {
			match := false
			for _, c := range e.Categories {
				if c == f.Category {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}

		if lowerFilter != "" {
			if !strings.Contains(strings.ToLower(e.Name), lowerFilter) &&
				!strings.Contains(strings.ToLower(e.Shortcut), lowerFilter) {
				continue
			}
		}

		if nameSet != nil {
			if _, ok := nameSet[strings.ToLower(e.Name)]; !ok {
				continue
			}
		}

		out = append(out, e)
	}

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})

	return out
}

// validateEnginesLiveOptions enforces --live preconditions. Pure; no network.
//
//	live && len(engines)==0 -> error "--live requires --engines"
//	live && len(engines)>5  -> error "--live supports at most 5 engines (got N)"
//	otherwise nil
func validateEnginesLiveOptions(live bool, engines []string) error {
	if !live {
		return nil
	}
	if len(engines) == 0 {
		return fmt.Errorf("--live requires --engines")
	}
	if len(engines) > 5 {
		return fmt.Errorf("--live supports at most 5 engines (got %d)", len(engines))
	}
	return nil
}

type searxngEnginesEnvelope struct {
	OK          bool                  `json:"ok"`
	Source      string                `json:"source"`  // always "searxng_config"
	Backend     string                `json:"backend"` // always "searxng"
	Filters     searxngEnginesFilters `json:"filters"`
	EngineCount int                   `json:"engine_count"`
	Engines     []searxngEngineJSON   `json:"engines"`  // ALWAYS [] never null
	Warnings    []string              `json:"warnings"` // ALWAYS [] never null
	Error       *searxngEnginesError  `json:"error"`    // null on success
}

type searxngEnginesFilters struct {
	Category    string   `json:"category"`
	Filter      string   `json:"filter"`
	Engines     []string `json:"engines"` // ALWAYS [] never null
	EnabledOnly bool     `json:"enabled_only"`
	Live        bool     `json:"live"`
}

type searxngEngineJSON struct {
	Name             string             `json:"name"`
	Shortcut         string             `json:"shortcut"`
	Categories       []string           `json:"categories"` // ALWAYS [] never null
	Enabled          bool               `json:"enabled"`
	Timeout          float64            `json:"timeout"`
	Paging           bool               `json:"paging"`
	SafeSearch       bool               `json:"safesearch"`
	TimeRangeSupport bool               `json:"time_range_support"`
	LanguageSupport  bool               `json:"language_support"`
	Live             *searxngEngineLive `json:"live,omitempty"` // present only with --live
}

type searxngEngineLive struct {
	OK          bool   `json:"ok"`
	ResultCount int    `json:"result_count"`
	Error       string `json:"error"` // redacted; "" when ok
}

type searxngEnginesError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Backend string `json:"backend"`
}

// runSearxngEngines is the entry point for `sx searxng engines`. It builds the
// filter set, fetches /config (one GET, no /search), filters, optionally
// live-probes named engines, and emits human or JSON output.
func runSearxngEngines(jsonOut bool, category, filter string, engines []string, all, live bool) {
	filters := enginesFilters{
		Filter:      filter,
		Engines:     engines,
		EnabledOnly: !all,
		Live:        live,
	}

	// Validate and normalize --category before any network access.
	if category != "" {
		if !validateCategory(category) {
			emitSearxngEnginesError(filters, "INVALID_ARGUMENT",
				fmt.Sprintf("invalid category %q (supported: %s)", category, strings.Join(searxngCategories, ", ")),
				"searxng", exitUsageConfig, jsonOut)
			return
		}
		filters.Category = normalizeCategory(category)
	}

	// Validate --live preconditions; no network on failure.
	if err := validateEnginesLiveOptions(live, engines); err != nil {
		emitSearxngEnginesError(filters, "INVALID_ARGUMENT", err.Error(), "searxng", exitUsageConfig, jsonOut)
		return
	}

	if err := ensureConfig(); err != nil {
		emitSearxngEnginesError(filters, "CONFIG_ERROR",
			fmt.Sprintf("config error: %s", backends.RedactSecrets(err.Error())),
			"searxng", exitUsageConfig, jsonOut)
		return
	}

	if !hasSearxngConfigured(config) {
		emitSearxngEnginesError(filters, "BACKEND_UNAVAILABLE",
			"no SearXNG instance configured", "searxng", exitSearchFail, jsonOut)
		return
	}

	backendMgr = initBackendManager(config)
	backend, ok := backendMgr.GetBackend("searxng")
	if !ok {
		emitSearxngEnginesError(filters, "BACKEND_UNAVAILABLE",
			"SearXNG backend is not registered", "searxng", exitSearchFail, jsonOut)
		return
	}
	cfgFetcher, ok := backend.(backends.SearxngConfigFetcher)
	if !ok {
		emitSearxngEnginesError(filters, "BACKEND_UNAVAILABLE",
			"SearXNG backend does not support /config inventory", "searxng", exitSearchFail, jsonOut)
		return
	}

	resp, err := cfgFetcher.FetchConfig()
	if err != nil {
		code := "BACKEND_UNAVAILABLE"
		message := safeSearxngConfigErrorMessage(err)
		backend := "searxng"
		var be *backends.BackendError
		if errors.As(err, &be) {
			code, _ = mapErrCodeToJSON(be.Code)
			if be.Backend != "" {
				backend = be.Backend
			}
		}
		emitSearxngEnginesError(filters, code, message, backend, exitSearchFail, jsonOut)
		return
	}

	filtered := filterSearxngEngines(resp.Engines, filters)

	var liveResults []*searxngEngineLive
	if live {
		rawBackend, ok := backend.(interface {
			SearchRaw(backends.SearchOptions) (backends.SearxngRawResponse, error)
		})
		if !ok {
			emitSearxngEnginesError(filters, "BACKEND_UNAVAILABLE",
				"SearXNG backend does not support live probing", "searxng", exitSearchFail, jsonOut)
			return
		}
		liveResults = make([]*searxngEngineLive, len(filtered))
		for i, e := range filtered {
			raw, probeErr := rawBackend.SearchRaw(backends.SearchOptions{
				Query:      "test",
				Engines:    []string{e.Name},
				Categories: e.Categories,
				NumResults: 1,
				PageNo:     1,
			})
			lr := &searxngEngineLive{OK: probeErr == nil}
			if probeErr == nil {
				lr.ResultCount = len(raw.Results)
			} else {
				lr.Error = safeSearxngConfigErrorMessage(probeErr)
			}
			liveResults[i] = lr
		}
	}

	if jsonOut {
		emitSearxngEnginesSuccess(filters, filtered, liveResults)
		return
	}

	printSearxngEnginesTable(filtered, liveResults, live)
}

// emitSearxngEnginesSuccess builds and prints the success JSON envelope. All
// array fields are guaranteed non-nil.
func emitSearxngEnginesSuccess(filters enginesFilters, filtered []backends.SearxngEngineConfig, liveResults []*searxngEngineLive) {
	enginesJSON := make([]searxngEngineJSON, len(filtered))
	for i, e := range filtered {
		categories := e.Categories
		if categories == nil {
			categories = []string{}
		}
		ej := searxngEngineJSON{
			Name:             e.Name,
			Shortcut:         e.Shortcut,
			Categories:       categories,
			Enabled:          e.Enabled,
			Timeout:          e.Timeout,
			Paging:           e.Paging,
			SafeSearch:       e.SafeSearch,
			TimeRangeSupport: e.TimeRangeSupport,
			LanguageSupport:  e.LanguageSupport,
		}
		if filters.Live && i < len(liveResults) {
			ej.Live = liveResults[i]
		}
		enginesJSON[i] = ej
	}

	env := searxngEnginesEnvelope{
		OK:          true,
		Source:      "searxng_config",
		Backend:     "searxng",
		Filters:     buildSearxngEnginesFilters(filters),
		EngineCount: len(enginesJSON),
		Engines:     enginesJSON,
		Warnings:    []string{},
		Error:       nil,
	}

	jsonData, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error formatting engines JSON: %s\n", backends.RedactSecrets(err.Error()))
		setExit(exitSearchFail)
		return
	}
	fmt.Println(string(jsonData))
}

// buildSearxngEnginesFilters mirrors the requested filters into the JSON
// envelope, guaranteeing the Engines field is a non-nil array.
func buildSearxngEnginesFilters(filters enginesFilters) searxngEnginesFilters {
	enginesList := filters.Engines
	if enginesList == nil {
		enginesList = []string{}
	}
	return searxngEnginesFilters{
		Category:    filters.Category,
		Filter:      filters.Filter,
		Engines:     enginesList,
		EnabledOnly: filters.EnabledOnly,
		Live:        filters.Live,
	}
}

// printSearxngEnginesTable prints a human-readable header and table to stdout.
func printSearxngEnginesTable(filtered []backends.SearxngEngineConfig, liveResults []*searxngEngineLive, live bool) {
	fmt.Printf("SearXNG upstream engines (from /config): %d\n\n", len(filtered))

	header := "NAME\tSHORTCUT\tCATEGORIES\tENABLED\tTIMEOUT\tPAGING\tSAFESEARCH\tTIME_RANGE\tLANGUAGE"
	if live {
		header += "\tLIVE"
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, header)
	for i, e := range filtered {
		row := fmt.Sprintf("%s\t%s\t%s\t%v\t%g\t%v\t%v\t%v\t%v",
			e.Name,
			e.Shortcut,
			strings.Join(e.Categories, ","),
			e.Enabled,
			e.Timeout,
			e.Paging,
			e.SafeSearch,
			e.TimeRangeSupport,
			e.LanguageSupport,
		)
		if live {
			liveCol := "fail"
			if i < len(liveResults) && liveResults[i] != nil && liveResults[i].OK {
				liveCol = fmt.Sprintf("ok(%d)", liveResults[i].ResultCount)
			}
			row += "\t" + liveCol
		}
		fmt.Fprintln(w, row)
	}
	w.Flush()
}

// emitSearxngEnginesError prints a fully-populated error envelope (JSON mode) or
// a stderr message (human mode) and sets the exit code. The message is always
// redacted and URL-scrubbed.
func emitSearxngEnginesError(filters enginesFilters, code, message, backend string, exitCode int, jsonOut bool) {
	setExit(exitCode)

	safeMessage := safeSearxngConfigMessage(message)

	if !jsonOut {
		fmt.Fprintf(os.Stderr, "Error: %s\n", safeMessage)
		return
	}

	env := searxngEnginesEnvelope{
		OK:          false,
		Source:      "searxng_config",
		Backend:     "searxng",
		Filters:     buildSearxngEnginesFilters(filters),
		EngineCount: 0,
		Engines:     []searxngEngineJSON{},
		Warnings:    []string{},
		Error: &searxngEnginesError{
			Code:    code,
			Message: safeMessage,
			Backend: backend,
		},
	}

	jsonData, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error formatting engines error JSON: %s\n", backends.RedactSecrets(err.Error()))
		return
	}
	fmt.Println(string(jsonData))
}

// safeSearxngConfigErrorMessage redacts and URL-scrubs an error from
// FetchConfig/SearchRaw, mirroring safeSearxngRawErrorMessage.
func safeSearxngConfigErrorMessage(err error) string {
	var be *backends.BackendError
	if errors.As(err, &be) {
		return safeSearxngConfigMessage(backends.RedactSecrets(be.Err.Error()))
	}
	return safeSearxngConfigMessage(backends.RedactSecrets(err.Error()))
}

// safeSearxngConfigMessage redacts and URL-scrubs a message string. If a URL
// survives redaction, it is replaced with a generic message so instance URLs
// never leak.
func safeSearxngConfigMessage(message string) string {
	msg := backends.RedactSecrets(message)
	if strings.Contains(msg, "://") {
		return "SearXNG config request failed"
	}
	if msg == "" {
		return "SearXNG config request failed"
	}
	return msg
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
