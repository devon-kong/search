package main

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"sx/backends"
)

const version = "2.4.0"

// Exit codes (documented contract for machine callers):
//
//	0 = success
//	1 = backend or search failure (e.g. all backends failed, no results)
//	2 = usage / argument / configuration error
//	3 = validation or health check failure
const (
	exitSuccess     = 0
	exitSearchFail  = 1
	exitUsageConfig = 2
	exitCheckFail   = 3
)

var (
	config     *Config
	searchOpts SearchOptions
	backendMgr *backends.Manager
	// exitCode is the process exit status; commands set it and main() exits
	// with it at the end so deferred cleanup and cobra teardown still run.
	exitCode = exitSuccess
)

// setExit records the highest-severity exit code seen. Higher numeric codes do
// not strictly mean more severe, so we simply set it (last writer wins within a
// single command path, which is sufficient here).
func setExit(code int) {
	exitCode = code
}

// isTerminal checks if the given file is connected to a terminal
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func main() {
	var err error
	config, err = loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", backends.RedactSecrets(err.Error()))
		os.Exit(exitUsageConfig)
	}

	var rootCmd = &cobra.Command{
		Use:   "sx [query...]",
		Short: "Multi-engine web search from the command line",
		Long: `sx is a command-line web search tool. It defaults to self-hosted SearXNG;
Brave, Tavily, Exa, and Jina are also supported. Automatic fallback is opt-in
and paid backends (Tavily, Exa API) are never used without explicit consent.

Use --json for a stable machine-readable envelope. Exit codes: 0 success,
1 search/backend failure, 2 usage/config error, 3 validation/health failure.`,
		Version:               version,
		Run:                   runSearch,
		Args:                  cobra.ArbitraryArgs,
		DisableFlagsInUseLine: true,
	}

	// Add flags
	rootCmd.Flags().StringVar(&config.SearxngURL, "searxng-url", config.SearxngURL, "Primary SearXNG instance URL")
	rootCmd.Flags().StringSliceVar(&config.SearxngURLs, "searxng-urls", config.SearxngURLs, "Additional SearXNG instance URLs for failover")
	rootCmd.Flags().StringVar(&config.SearxngStrategy, "searxng-strategy", config.SearxngStrategy, "SearXNG instance strategy (ordered, parallel-fastest)")
	rootCmd.Flags().StringSliceVar(&searchOpts.Categories, "categories", nil, fmt.Sprintf("list of categories to search in: %s", strings.Join(searxngCategories, ", ")))
	rootCmd.Flags().BoolVar(&searchOpts.JSON, "json", false, "output search results in JSON format")
	rootCmd.Flags().BoolVarP(&searchOpts.Clean, "clean", "c", false, "omit empty and null values in JSON output")
	rootCmd.Flags().StringSliceVarP(&searchOpts.SearxngEngines, "engines", "e", nil, "list of SearXNG engines to use for search")
	rootCmd.Flags().StringVar(&searchOpts.ExplicitEngine, "engine", "", fmt.Sprintf("search backend to use (%s)", validEngineNames()))
	rootCmd.Flags().BoolVarP(&searchOpts.Expand, "expand", "x", config.Expand, "show complete URL in search results (URLs are shown by default)")
	rootCmd.Flags().BoolVarP(&searchOpts.First, "first", "j", false, "open the first result in web browser and exit")
	rootCmd.Flags().StringVar(&config.HTTPMethod, "http-method", config.HTTPMethod, "HTTP method to use for search requests (GET or POST)")
	rootCmd.Flags().Float64Var(&config.Timeout, "timeout", config.Timeout, "HTTP request timeout in seconds")
	rootCmd.Flags().StringVarP(&searchOpts.Language, "language", "l", "", "search results in a specific language")
	rootCmd.Flags().BoolVar(&searchOpts.Lucky, "lucky", false, "opens a random result in web browser and exit")
	rootCmd.Flags().BoolVar(&config.NoVerifySSL, "no-verify-ssl", config.NoVerifySSL, "do not verify SSL certificates")
	rootCmd.Flags().BoolVar(&config.NoColor, "nocolor", config.NoColor, "disable colored output")
	rootCmd.Flags().BoolVar(&config.NoUserAgent, "noua", config.NoUserAgent, "disable user agent")
	rootCmd.Flags().IntVarP(&config.ResultCount, "num", "n", config.ResultCount, "show N results per page")
	rootCmd.Flags().StringVar(&searchOpts.SafeSearch, "safe-search", config.SafeSearch, "filter results for safe search (none, moderate, strict)")
	rootCmd.Flags().StringVarP(&searchOpts.Site, "site", "w", "", "search sites using site: operator")
	rootCmd.Flags().StringVarP(&searchOpts.TimeRange, "time-range", "r", "", "search results within a specific time range (day, week, month, year)")
	rootCmd.Flags().BoolVar(&searchOpts.Unsafe, "unsafe", false, "allow unsafe search results")
	rootCmd.Flags().BoolVar(&config.Debug, "debug", config.Debug, "show debug output")
	rootCmd.Flags().BoolVarP(&searchOpts.HTMLOnly, "html", "H", false, "fetch and output raw HTML with anti-bot detection")
	rootCmd.Flags().BoolVarP(&searchOpts.LinksOnly, "links-only", "L", false, "output only URLs, one per line")
	rootCmd.Flags().BoolVarP(&searchOpts.TextOnly, "text", "T", false, "fetch pages and convert to clean markdown (uses readability)")
	rootCmd.Flags().StringVarP(&searchOpts.OutputFile, "output", "o", "", "save output to file")
	rootCmd.Flags().BoolVar(&searchOpts.Top, "top", false, "show only the top result")

	// Interactive mode (non-interactive is now the default)
	rootCmd.Flags().BoolVarP(&searchOpts.Interactive, "interactive", "i", false, "enter interactive mode after displaying results")
	// Keep -p/--np as hidden deprecated alias for backward compatibility
	rootCmd.Flags().BoolVarP(&searchOpts.NoPrompt, "np", "p", false, "just search and exit, do not prompt (deprecated: now the default)")
	rootCmd.Flags().MarkDeprecated("np", "non-interactive is now the default; use -i/--interactive for interactive mode")
	rootCmd.Flags().MarkShorthandDeprecated("np", "non-interactive is now the default; use -i/--interactive for interactive mode")

	// Category shortcuts
	var files, music, news, social, videos bool
	rootCmd.Flags().BoolVarP(&files, "files", "F", false, "show results from files section")
	rootCmd.Flags().BoolVarP(&music, "music", "M", false, "show results from music section")
	rootCmd.Flags().BoolVarP(&news, "news", "N", false, "show results from news section")
	rootCmd.Flags().BoolVarP(&social, "social", "S", false, "show results from social media section")
	rootCmd.Flags().BoolVarP(&videos, "videos", "V", false, "show results from videos section")

	// History subcommand
	historyCmd := &cobra.Command{
		Use:   "history",
		Short: "Show search history",
		Run: func(cmd *cobra.Command, args []string) {
			limit, _ := cmd.Flags().GetInt("limit")
			if err := printHistory(limit); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				setExit(exitSearchFail)
			}
		},
	}
	historyCmd.Flags().IntP("limit", "n", 20, "number of history entries to show")

	historyClearCmd := &cobra.Command{
		Use:   "clear",
		Short: "Clear search history",
		Run: func(cmd *cobra.Command, args []string) {
			if err := clearHistory(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				setExit(exitSearchFail)
			}
		},
	}
	historyCmd.AddCommand(historyClearCmd)

	// Completion subcommand
	completionCmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for sx.

To load completions:

Bash:
  $ source <(sx completion bash)
  # To load completions for each session, execute once:
  # Linux:
  $ sx completion bash > /etc/bash_completion.d/sx
  # macOS:
  $ sx completion bash > $(brew --prefix)/etc/bash_completion.d/sx

Zsh:
  $ source <(sx completion zsh)
  # To load completions for each session, execute once:
  $ sx completion zsh > "${fpath[1]}/_sx"

Fish:
  $ sx completion fish | source
  # To load completions for each session, execute once:
  $ sx completion fish > ~/.config/fish/completions/sx.fish

PowerShell:
  PS> sx completion powershell | Out-String | Invoke-Expression
`,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		Run: func(cmd *cobra.Command, args []string) {
			switch args[0] {
			case "bash":
				rootCmd.GenBashCompletion(os.Stdout)
			case "zsh":
				rootCmd.GenZshCompletion(os.Stdout)
			case "fish":
				rootCmd.GenFishCompletion(os.Stdout, true)
			case "powershell":
				rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
			}
		},
	}

	rootCmd.AddCommand(historyCmd)
	rootCmd.AddCommand(completionCmd)
	rootCmd.AddCommand(newSearchCmd(rootCmd))
	rootCmd.AddCommand(newConfigCmd())
	rootCmd.AddCommand(newHealthCmd())

	if err := rootCmd.Execute(); err != nil {
		// cobra reports flag/arg/usage errors here.
		setExit(exitUsageConfig)
	}

	os.Exit(exitCode)
}

func runSearch(cmd *cobra.Command, args []string) {
	startTime := time.Now()
	var query string

	// requestedEngine is what the user asked for (explicit --engine, else config
	// engine, else searxng). Used for the JSON envelope's backend.requested.
	requestedEngine := searchOpts.ExplicitEngine
	if requestedEngine == "" {
		requestedEngine = config.Engine
	}
	if requestedEngine == "" {
		requestedEngine = "searxng"
	}

	// Check for piped input
	if isPipeInput() {
		input, err := readFromStdin()
		if err != nil {
			emitUsageFailure(query, requestedEngine, "INVALID_INPUT",
				fmt.Sprintf("error reading from stdin: %v", err), startTime)
			return
		}
		query = strings.TrimSpace(input)
		if query == "" {
			emitUsageFailure(query, requestedEngine, "INVALID_INPUT",
				"empty input from stdin", startTime)
			return
		}
	} else if len(args) == 0 {
		cmd.Help()
		return
	} else {
		query = strings.Join(args, " ")
	}

	// Ensure config file exists for actual searches. In non-interactive or
	// --json contexts this must never block on stdin (see ensureConfig).
	if err := ensureConfig(); err != nil {
		emitUsageFailure(query, requestedEngine, "CONFIG_ERROR",
			fmt.Sprintf("config error: %v", backends.RedactSecrets(err.Error())), startTime)
		return
	}

	// Initialize backend manager
	backendMgr = initBackendManager(config)

	// Determine interactive mode:
	// 1. Explicit -i/--interactive flag wins
	// 2. Config default_output = "interactive" enables it
	// 3. Auto-detect: interactive when stdout is TTY and no special output flags
	// 4. Default: non-interactive
	interactive := searchOpts.Interactive
	if !interactive && config.DefaultOutput == "interactive" {
		interactive = true
	}
	// Piped output is never interactive
	if !isTerminal(os.Stdout) || isPipeInput() {
		interactive = false
	}
	// Special output formats are never interactive
	if searchOpts.JSON || searchOpts.LinksOnly || searchOpts.HTMLOnly || searchOpts.TextOnly || searchOpts.Top {
		interactive = false
	}

	// Handle category shortcuts
	if files, _ := cmd.Flags().GetBool("files"); files {
		searchOpts.Categories = []string{"files"}
	}
	if music, _ := cmd.Flags().GetBool("music"); music {
		searchOpts.Categories = []string{"music"}
	}
	if news, _ := cmd.Flags().GetBool("news"); news {
		searchOpts.Categories = []string{"news"}
	}
	if social, _ := cmd.Flags().GetBool("social"); social {
		searchOpts.Categories = []string{"social media"}
	}
	if videos, _ := cmd.Flags().GetBool("videos"); videos {
		searchOpts.Categories = []string{"videos"}
	}

	// Handle unsafe flag
	if searchOpts.Unsafe {
		searchOpts.SafeSearch = "none"
	}

	// Handle top flag - show only first result
	if searchOpts.Top {
		config.ResultCount = 1
	}

	// Validate config: require at least one SearXNG instance when using searxng engine
	engineToUse := requestedEngine
	if engineToUse == "searxng" && !hasSearxngConfigured(config) {
		emitUsageFailure(query, requestedEngine, "CONFIG_ERROR",
			"no SearXNG instance configured (set searxng_url or searxng_urls, or use --engine brave/tavily/exa/jina)",
			startTime)
		return
	}

	// Validate categories
	for _, category := range searchOpts.Categories {
		if !validateCategory(category) {
			emitUsageFailure(query, requestedEngine, "INVALID_ARGUMENT",
				fmt.Sprintf("invalid category '%s' (supported: %s)", category, strings.Join(searxngCategories, ", ")),
				startTime)
			return
		}
	}

	// Validate time range
	if searchOpts.TimeRange != "" {
		if !validateTimeRange(searchOpts.TimeRange) {
			emitUsageFailure(query, requestedEngine, "INVALID_ARGUMENT",
				fmt.Sprintf("invalid time range '%s' (use: %s)", searchOpts.TimeRange, strings.Join(timeRangeOptions, ", ")),
				startTime)
			return
		}
		searchOpts.TimeRange = expandTimeRange(searchOpts.TimeRange)
	}

	// Set defaults from config
	if searchOpts.SafeSearch == "" {
		searchOpts.SafeSearch = config.SafeSearch
	}

	// Record query in history
	_ = appendHistory(query)

	searchOpts.PageNo = 1
	startAt := 0
	var allResults []SearchResult
	var usedEngine string
	var outcome backends.SearchOutcome

	for {
		// Fetch results until we have enough
		for len(allResults) < startAt+config.ResultCount {
			oc, err := performSearch(query, config, &searchOpts, backendMgr, searchOpts.ExplicitEngine)
			if err != nil {
				// Surface any warnings (e.g. paid fallback skipped) to stderr.
				for _, w := range oc.Warnings {
					fmt.Fprintf(os.Stderr, "Warning: %s\n", backends.RedactSecrets(w))
				}
				emitSearchFailure(query, requestedEngine, err, oc.Warnings, startTime)
				return
			}
			// Capture metadata from the first successful page.
			if usedEngine == "" {
				usedEngine = oc.Backend
				outcome = oc
			}

			if len(oc.Results) == 0 {
				break
			}

			allResults = append(allResults, oc.Results...)
			if config.ResultCount == 0 {
				break
			}
			searchOpts.PageNo++
		}

		if len(allResults) == 0 {
			// No results is not an error; emit an empty success envelope in
			// --json mode, otherwise the human message. Exit code stays 0.
			if searchOpts.JSON {
				emitSearchSuccess(query, requestedEngine, outcome, nil, startTime)
			} else {
				fmt.Println("No results found or an error occurred during the search.")
			}
			return
		}

		// Handle special output formats
		if searchOpts.JSON {
			emitSearchSuccess(query, requestedEngine, outcome, allResults, startTime)
			return
		}

		if searchOpts.LinksOnly {
			count := config.ResultCount
			if count == 0 {
				count = len(allResults)
			}
			end := startAt + count
			if end > len(allResults) {
				end = len(allResults)
			}
			linksResults := allResults[startAt:end]
			if err := printLinksOnly(linksResults, searchOpts.OutputFile); err != nil {
				fmt.Fprintf(os.Stderr, "Error outputting links: %v\n", err)
			}
			return
		}

		if searchOpts.HTMLOnly {
			count := config.ResultCount
			if count == 0 {
				count = len(allResults)
			}
			end := startAt + count
			if end > len(allResults) {
				end = len(allResults)
			}
			htmlResults := allResults[startAt:end]
			if err := printHTMLOnly(htmlResults, searchOpts.OutputFile, config); err != nil {
				fmt.Fprintf(os.Stderr, "Error outputting HTML: %v\n", err)
			}
			return
		}

		if searchOpts.TextOnly {
			count := config.ResultCount
			if count == 0 {
				count = len(allResults)
			}
			end := startAt + count
			if end > len(allResults) {
				end = len(allResults)
			}
			textResults := allResults[startAt:end]
			if err := printTextOnly(textResults, searchOpts.OutputFile, config); err != nil {
				fmt.Fprintf(os.Stderr, "Error outputting text: %v\n", err)
			}
			return
		}

		// Handle first/lucky options
		if searchOpts.First && len(allResults) > 0 {
			if err := openURL(allResults[0].URL); err != nil {
				fmt.Fprintf(os.Stderr, "Error opening URL: %v\n", err)
			}
			return
		}

		if searchOpts.Lucky && len(allResults) > 0 {
			randomResult := allResults[rand.Intn(len(allResults))]
			if err := openURL(randomResult.URL); err != nil {
				fmt.Fprintf(os.Stderr, "Error opening URL: %v\n", err)
			}
			return
		}

		// Display results
		count := config.ResultCount
		if count == 0 {
			count = len(allResults)
		}

		if searchOpts.OutputFile != "" {
			if err := printResultsToFile(allResults, count, startAt, searchOpts.Expand, config.NoColor, query, searchOpts.OutputFile); err != nil {
				fmt.Fprintf(os.Stderr, "Error writing results to file: %v\n", err)
			}
		} else {
			printResults(allResults, count, startAt, searchOpts.Expand, config.NoColor, query)
		}

		// Exit if not interactive
		if !interactive {
			return
		}

		// Interactive prompt
		if !handleInteractiveSession(&query, &allResults, &startAt, &searchOpts) {
			return
		}
	}
}

// costTierForBackend resolves a backend's cost tier from the manager registry,
// returning "" when unknown (e.g. before the manager is built).
func costTierForBackend(name string) string {
	if backendMgr == nil || name == "" {
		return ""
	}
	if b, ok := backendMgr.GetBackend(name); ok {
		return b.CostTier()
	}
	return ""
}

// emitSearchSuccess writes a success JSON envelope (stdout or --output file).
func emitSearchSuccess(query, requested string, outcome backends.SearchOutcome, results []SearchResult, startTime time.Time) {
	used := outcome.Backend
	if used == "" {
		used = requested
	}
	env := &JSONEnvelope{
		OK:    true,
		Query: query,
		Backend: jsonBackendMeta{
			Requested:      requested,
			Used:           &used,
			FallbackUsed:   outcome.FallbackUsed,
			FallbackReason: backends.RedactSecrets(outcome.FallbackReason),
			CostTier:       costTierForBackend(used),
		},
		Timing:   jsonTiming{TotalMs: time.Since(startTime).Milliseconds()},
		Results:  buildResultsForJSON(results, searchOpts.Clean),
		Warnings: redactWarnings(outcome.Warnings),
		Error:    nil,
	}
	if searchOpts.OutputFile != "" {
		if err := writeJSONEnvelopeToFile(env, searchOpts.OutputFile); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing JSON to file: %v\n", err)
			setExit(exitSearchFail)
		}
		return
	}
	if err := writeJSONEnvelope(env); err != nil {
		fmt.Fprintf(os.Stderr, "Error formatting JSON: %v\n", err)
		setExit(exitSearchFail)
	}
}

// emitSearchFailure writes a backend/search failure: structured JSON envelope in
// --json mode (exit 1), otherwise the original stderr message (exit 1).
func emitSearchFailure(query, requested string, searchErr error, warnings []string, startTime time.Time) {
	setExit(exitSearchFail)
	if !searchOpts.JSON {
		fmt.Fprintf(os.Stderr, "Search error: %v\n", backends.RedactSecrets(searchErr.Error()))
		return
	}
	jerr := buildJSONError(searchErr, requested)
	env := &JSONEnvelope{
		OK:    false,
		Query: query,
		Backend: jsonBackendMeta{
			Requested:    requested,
			Used:         nil, // null: no backend succeeded
			FallbackUsed: false,
			CostTier:     costTierForBackend(requested),
		},
		Timing:   jsonTiming{TotalMs: time.Since(startTime).Milliseconds()},
		Results:  []SearchResult{},
		Warnings: redactWarnings(warnings),
		Error:    jerr,
	}
	if err := writeJSONEnvelope(env); err != nil {
		fmt.Fprintf(os.Stderr, "Error formatting JSON: %v\n", err)
	}
}

// emitUsageFailure writes a usage/argument/config failure: structured JSON
// envelope in --json mode, otherwise stderr text. Exit code 2.
func emitUsageFailure(query, requested, code, message string, startTime time.Time) {
	setExit(exitUsageConfig)
	if !searchOpts.JSON {
		fmt.Fprintf(os.Stderr, "Error: %s\n", backends.RedactSecrets(message))
		return
	}
	env := &JSONEnvelope{
		OK:    false,
		Query: query,
		Backend: jsonBackendMeta{
			Requested: requested,
			Used:      nil,
			CostTier:  costTierForBackend(requested),
		},
		Timing:  jsonTiming{TotalMs: time.Since(startTime).Milliseconds()},
		Results: []SearchResult{},
		Error: &jsonError{
			Code:              code,
			Message:           backends.RedactSecrets(message),
			Backend:           requested,
			Retryable:         false,
			RetryAfterSeconds: nil,
		},
	}
	if err := writeJSONEnvelope(env); err != nil {
		fmt.Fprintf(os.Stderr, "Error formatting JSON: %v\n", err)
	}
}

// redactWarnings applies secret redaction to each warning string.
func redactWarnings(warnings []string) []string {
	if len(warnings) == 0 {
		return []string{}
	}
	out := make([]string, len(warnings))
	for i, w := range warnings {
		out[i] = backends.RedactSecrets(w)
	}
	return out
}

func handleInteractiveSession(query *string, allResults *[]SearchResult, startAt *int, opts *SearchOptions) bool {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("sx (? for help): ")
		input, err := reader.ReadString('\n')
		if err != nil {
			return false
		}

		input = strings.TrimSpace(input)

		switch {
		case input == "q" || input == "quit" || input == "exit":
			return false

		case input == "?":
			printHelp()
			continue

		case input == "n": // Next page
			*startAt += config.ResultCount
			if *startAt >= len(*allResults) {
				opts.PageNo++
				return true // Need to fetch more results
			}
			printResults(*allResults, config.ResultCount, *startAt, opts.Expand, config.NoColor, *query)
			continue

		case input == "p": // Previous page
			*startAt -= config.ResultCount
			if *startAt < 0 {
				*startAt = 0
			}
			printResults(*allResults, config.ResultCount, *startAt, opts.Expand, config.NoColor, *query)
			continue

		case input == "f": // First page
			*startAt = 0
			printResults(*allResults, config.ResultCount, *startAt, opts.Expand, config.NoColor, *query)
			continue

		case input == "x": // Toggle expand URLs
			opts.Expand = !opts.Expand
			printResults(*allResults, config.ResultCount, *startAt, opts.Expand, config.NoColor, *query)
			continue

		case input == "d": // Toggle debug
			config.Debug = !config.Debug
			fmt.Printf("Debug mode %s\n", map[bool]string{true: "enabled", false: "disabled"}[config.Debug])
			continue

		case strings.HasPrefix(input, "r "): // Change time range
			timeRange := strings.TrimSpace(input[2:])
			if validateTimeRange(timeRange) {
				opts.TimeRange = expandTimeRange(timeRange)
				*startAt = 0
				opts.PageNo = 1
				*allResults = []SearchResult{}
				return true
			} else {
				fmt.Printf("Invalid time range '%s'. Use: %s\n", timeRange, strings.Join(timeRangeOptions, ", "))
			}
			continue

		case strings.HasPrefix(input, "site:"): // Change site filter
			site := strings.TrimSpace(input[5:])
			opts.Site = site
			*startAt = 0
			opts.PageNo = 1
			*allResults = []SearchResult{}
			return true

		case strings.HasPrefix(input, "c "): // Copy URL
			indexStr := strings.TrimSpace(input[2:])
			if index, err := strconv.Atoi(indexStr); err == nil && index > 0 && index <= len(*allResults) {
				url := (*allResults)[index-1].URL
				fmt.Printf("URL: %s\n", url)
			} else {
				fmt.Println("Invalid index specified.")
			}
			continue

		case strings.HasPrefix(input, "j "): // Show JSON for result
			indexStr := strings.TrimSpace(input[2:])
			if index, err := strconv.Atoi(indexStr); err == nil && index > 0 && index <= len(*allResults) {
				result := (*allResults)[index-1]
				if opts.Clean {
					if err := printJSONResultsClean([]SearchResult{result}, *query); err != nil {
						fmt.Fprintf(os.Stderr, "Error formatting JSON: %v\n", err)
					}
				} else {
					if err := printJSONResults([]SearchResult{result}, *query); err != nil {
						fmt.Fprintf(os.Stderr, "Error formatting JSON: %v\n", err)
					}
				}
			}
			continue

		default:
			// Check if it's a number (open result)
			if index, err := strconv.Atoi(input); err == nil && index > 0 && index <= len(*allResults) {
				url := (*allResults)[index-1].URL
				if err := openURL(url); err != nil {
					fmt.Fprintf(os.Stderr, "Error opening URL: %v\n", err)
				}
				continue
			}

			// Treat as new query
			if input != "" {
				*query = input
				*startAt = 0
				opts.PageNo = 1
				*allResults = []SearchResult{}
				// Record new query in history
				_ = appendHistory(input)
				return true
			}
		}
	}
}

func printHelp() {
	help := `
- Enter a search query to perform a new search.
- Type 'n', 'p', and 'f' to navigate to the next, previous and first page of results.
- Type the index (1, 2, 3, etc) to open the search result in a browser.
- Type 'c' plus the index ('c 1', 'c 2') to show the result URL.
- Type 'r timerange' to change the search time range (e.g. 'r week').
- Type 'site:example.com' to filter results by a specific site.
- Type 'x' to toggle showing result URLs.
- Type 'd' to toggle debug output.
- Type 'j' plus the index ('j 1', 'j 2') to show the JSON result for the specified index.
- Type 'q', 'quit', or 'exit' to exit the program.
- Type '?' for this help message.
`
	fmt.Print(help)
}

func openURL(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("explorer", url)
	default:
		return fmt.Errorf("unsupported platform")
	}

	return cmd.Start()
}

func isPipeInput() bool {
	fileInfo, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fileInfo.Mode()&os.ModeCharDevice == 0
}

func readFromStdin() (string, error) {
	var input strings.Builder
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		if input.Len() > 0 {
			input.WriteString(" ")
		}
		input.WriteString(scanner.Text())
	}
	return input.String(), scanner.Err()
}
