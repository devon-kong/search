# sx

Multi-engine web search from the command line

`sx` is a CLI tool for searching the web from your terminal. It supports
multiple search backends -- [SearXNG](https://github.com/searxng/searxng)
(self-hosted, the default), [Exa](https://exa.ai/), [Jina](https://jina.ai/),
[Brave Search](https://api.search.brave.com/) and [Tavily](https://tavily.com/).
By default it uses **only** SearXNG: automatic fallback is **off**, and paid
backends (Tavily, Exa API) are never used silently. Fallback is strictly opt-in
(see [Fallback and paid backends](#fallback-and-paid-backends)).

This is a Go port of the original Python [searxngr](https://github.com/scross01/searxngr) project, extended with multi-engine support.

## Key Features

- **Multiple search backends** - SearXNG (default), Exa, Jina, Brave Search, Tavily; fallback is opt-in and paid backends require explicit consent
- **Safe by default** - no automatic fallback, paid backends never used silently
- **Multi-instance SearXNG failover** - ordered or parallel-fastest strategy
- **Terminal-based interface** with colorized output
- **Non-interactive by default** for scripting; `-i` for interactive mode
- **Backend and upstream engine selection** (`--engine` selects the sx backend; `--engines` selects SearXNG upstream engines)
- Support for **search categories** (general, news, images, videos, science, etc.)
- **Safe search filtering** (none, moderate, strict)
- **Time-range filtering** (day, week, month, year)
- **JSON output** for scripting (stable envelope with backend metadata, timing, warnings, structured errors)
- **Documented exit codes** for reliable machine/agent integration
- **Built-in content extraction** - fetch and convert results to clean markdown
- **Anti-bot detection** - rotating user agents, realistic headers, random delays
- **Query history** - searchable history with `sx history`
- **Shell completions** - bash, zsh, fish, powershell
- **Cross-platform** (macOS, Linux, Windows)

## Installation

```shell
go install github.com/your-repo/sx@latest
```

Or build from source:

```shell
git clone https://github.com/your-repo/sx.git
cd sx
go build -o sx .
```

## Configuration

Config is stored at `$XDG_CONFIG_HOME/search-engine/config.toml`
(typically `~/.config/search-engine/config.toml`), created automatically on
first run.

**Migration from the old location:** if no config exists at the new location but
a legacy `~/.config/sx/config.toml` is present, sx reads the legacy file
(read-only) and prints a one-time notice on stderr. Your old file is never
modified or deleted. To finish migrating, copy it to the new path.

### Example config.toml

```toml
# sx configuration file

# Primary search engine (searxng, brave, tavily, exa, jina)
engine = "searxng"

# Automatic fallback is OFF by default. Empty = never fall back.
fallback_engines = []
# Opt in to free/self-hosted fallback engines:
# fallback_engines = ["jina", "brave"]

# Paid backends (tavily, exa API) are skipped in automatic fallback unless
# this is true. This prevents silently spending credits.
allow_paid_fallback = false

# SearXNG instance settings
searxng_url = "https://searxng.example.com"
searxng_urls = ["https://searxng-backup-1.example.com", "https://searxng-backup-2.example.com"]
searxng_strategy = "ordered" # ordered or parallel-fastest
# searxng_username = ""
# searxng_password = ""

# General settings
result_count = 10
safe_search = "strict"
http_method = "GET"
timeout = 30.0
expand = false
no_verify_ssl = false
no_user_agent = false
no_color = false
debug = false

# Output defaults
# default_output = ""       # "interactive" to default to interactive mode
history_enabled = true
max_history = 100

# Brave Search API (https://api.search.brave.com/)
# Free tier: 2,000 requests/month
[engines_brave]
api_key = ""  # or set BRAVE_API_KEY env var
# base_url = "https://api.search.brave.com/res/v1/web/search"  # override endpoint

# Tavily Search API (https://tavily.com/) -- PAID backend (>= 1 credit/search)
# Free tier: 1,000 credits/month
[engines_tavily]
api_key = ""                  # or set TAVILY_API_KEY env var
# base_url = "https://api.tavily.com/search"  # override endpoint
search_depth = "basic"        # basic (1 credit) or advanced (2 credits)
include_raw_content = false   # return full page content with results
include_answer = false        # return a direct answer

# Exa Search (API + MCP). API mode is paid; MCP mode is free_external.
[engines_exa]
mode = "auto"                # auto, api, mcp
api_key = ""                 # or set EXA_API_KEY env var
# base_url = "https://api.exa.ai/search"  # override API endpoint
mcp_url = ""                 # optional MCP HTTP endpoint
mcp_tool = "exa-web-search"  # MCP tool name
num_results = 10

# Jina Search
[engines_jina]
api_key = ""                 # or set JINA_API_KEY env var
allow_keyless = true
base_url = "https://s.jina.ai"
```

### API Keys via Environment Variables

```shell
export BRAVE_API_KEY="your-brave-key"
export TAVILY_API_KEY="tvly-your-tavily-key"
export EXA_API_KEY="your-exa-key"
export JINA_API_KEY="your-jina-key"
```

## Usage

### Basic Search

```shell
sx "why is the sky blue"
sx "golang tutorials" -n 5
```

### Select Backend and SearXNG Engines

```shell
# Use a specific sx backend
sx "query" --engine exa
sx "query" --engine jina
sx "query" --engine brave
sx "query" --engine tavily

# Use specific SearXNG upstream engines while the sx backend is SearXNG
sx "query" --engine searxng --engines google,duckduckgo
sx "query" --engines "google news" --news

# Default: uses the primary engine only (SearXNG). No automatic fallback
# unless you opt in via fallback_engines (see "Fallback and paid backends").
sx "query"
```

`--engine` selects the sx backend (`searxng`, `brave`, `tavily`, `exa`,
`jina`). `--engines` is passed through to SearXNG and selects SearXNG upstream
engines such as `google`, `duckduckgo`, or `google news`. `google` and
`google news` are different SearXNG upstream engines. `--news` is only a
category shortcut (`--categories news`); it is not the same as requesting the
`google news` engine.

> Note: an explicit `--engine tavily` (or any paid backend) is honored -- the
> paid-fallback gate only applies to *automatic* fallback, not to engines you
> select yourself.

### Output Links for Piping

```shell
# Get URLs only (one per line)
sx "golang testing" -L -n 5

# Pipe to other tools
sx "rust tutorials" -L -n 3 | xargs open
```

### Fetch and Convert Pages to Markdown

```shell
# Top result as markdown
sx "golang channels tutorial" --text --top

# Multiple results saved to file
sx "rust ownership" --text -n 3 -o results.md
```

### Pipelines with scrpr

`sx` pairs with [scrpr](https://github.com/byteowlz/scrpr) for content extraction:

```shell
# Search + extract content
sx "query" -L -n 5 | scrpr --format markdown

# Save to directory
sx "query" -L -n 5 | scrpr --format markdown -o articles/

# Use Jina Reader for JS-heavy sites
sx "query" -L -n 5 | scrpr -B jina --format markdown

# With rate limiting
sx "query" -L -n 10 | scrpr --delay 0.5 --continue-on-error
```

### Other Options

```shell
# Categories
sx "query" -N              # news
sx "query" -V              # videos
sx "query" -S              # social media
sx "query" -F              # files

# Filtering
sx "query" -r week         # time range: day, week, month, year
sx "query" -w example.com  # site-specific search
sx "query" --safe-search none

# Output formats
sx "query" --json          # JSON output
sx "query" --json -c       # Clean JSON (no null fields)
sx "query" --json --diagnostics
sx "query" --json --diagnostics --engines google --strict-engines
sx "query" -H              # Raw HTML with anti-bot headers

# Raw SearXNG JSON: no sx envelope, no --clean, no paid fallback
sx searxng raw "query"
sx searxng raw "query" --news --engines "google news" --language en-US -r day -n 5

# Interactive mode
sx "query" -i

# History
sx history
sx history clear
sx history -n 50

# Subcommands
sx search "query"          # explicit alias for `sx "query"` (same flags)
sx config validate         # check config; no network requests; exit 3 on error
sx health                  # report backend availability (no live requests)
sx health --live           # live reachability check (warns before paid backends)

# Shell completions
sx completion bash
sx completion zsh
```

### All Flags

```
Flags:
      --categories strings   search categories (general, news, videos, images, music, etc.)
      --clean                omit empty/null values in JSON output
      --debug                show debug output
      --diagnostics          include SearXNG diagnostics in --json output
  -e, --engines strings      SearXNG upstream engines to request
      --engine string        sx search backend (searxng, brave, tavily, exa, jina)
  -x, --expand               show full URLs in results (URLs are shown by default)
  -F, --files                files category shortcut
  -j, --first                open first result in browser
  -h, --help                 help for sx
  -H, --html                 fetch raw HTML with anti-bot headers
      --http-method string   GET or POST for SearXNG (default "GET")
  -i, --interactive          enter interactive mode after results
      --json                 JSON output
  -l, --language string      search language
  -L, --links-only           output URLs only, one per line
      --lucky                open random result in browser
  -M, --music                music category shortcut
  -N, --news                 news category shortcut
      --no-verify-ssl        skip SSL verification
      --nocolor              disable colors
      --noua                 disable user agent
  -n, --num int              results per page (default 10)
  -o, --output string        save output to file
      --safe-search string      none, moderate, strict (default "strict")
      --searxng-strategy string SearXNG instance strategy (ordered, parallel-fastest)
      --searxng-url string      Primary SearXNG instance URL
      --searxng-urls strings    Additional SearXNG instance URLs for failover
  -w, --site string             search within a specific site
  -S, --social               social media category shortcut
      --strict-engines       warn if requested SearXNG engines are unresponsive or results include other engines
  -T, --text                 fetch pages and convert to markdown
  -r, --time-range string    day, week, month, year
      --timeout float        request timeout in seconds (default 30)
      --top                  show only top result
      --unsafe               disable safe search
  -v, --version              version
  -V, --videos               videos category shortcut
```

## Search Backend Comparison

| Backend | Auth | Free Tier | Best For |
|---------|------|-----------|----------|
| **SearXNG** | None (self-hosted) | Unlimited | Privacy, full control |
| **Exa** | API key or MCP | Varies by plan/MCP setup | Research-focused search, MCP workflows |
| **Jina** | Optional API key | Keyless best-effort | Fallback without mandatory key |
| **Brave** | API key | 2,000 req/month | Fallback, quick setup |
| **Tavily** | API key | 1,000 credits/month | LLM workflows, rich content |

Cost tiers (used to gate automatic fallback and reported as `backend.cost_tier`
in JSON output):

| Backend | Cost tier |
|---------|-----------|
| SearXNG | `self_hosted` |
| Jina, Brave | `free_external` |
| Exa (MCP mode) | `free_external` |
| Tavily, Exa (API/auto mode) | `paid` |

## Fallback and paid backends

- **Default backend is SearXNG.** With no fallback configured, only SearXNG is
  used; if it fails, sx returns a structured failure (no silent fallback).
- **Automatic fallback is opt-in.** It happens only when `fallback_engines` is
  non-empty. Engines are tried in the listed order after the primary fails.
- **Paid backends are gated.** Tavily and Exa (API/auto mode) are `paid`. They
  are skipped during automatic fallback unless `allow_paid_fallback = true`,
  even if listed in `fallback_engines`. Skips are surfaced in `warnings`.
- **Explicit selection is always honored.** `--engine tavily` (or any paid
  backend) runs that backend directly; the paid gate applies only to automatic
  fallback, not to engines you choose yourself.

```toml
# Safe (default): paid backends never used automatically
fallback_engines = ["jina", "brave", "tavily"]
allow_paid_fallback = false   # tavily is skipped in fallback

# Opt in to paid fallback
fallback_engines = ["jina", "brave", "tavily"]
allow_paid_fallback = true    # tavily may now be used as a fallback
```

## JSON output contract

With `--json`, stdout always contains a single valid JSON envelope; logs,
warnings, and debug go to stderr. Add `-c/--clean` to omit empty fields inside
each result. A successful zero-result search always emits `"results": []`, not
`null`, in both normal and clean JSON modes.

Success:

```json
{
  "ok": true,
  "query": "example",
  "backend": {
    "requested": "searxng",
    "used": "searxng",
    "fallback_used": false,
    "fallback_reason": "",
    "cost_tier": "self_hosted"
  },
  "timing": { "total_ms": 42 },
  "results": [ /* result objects (unchanged structure) */ ],
  "warnings": [],
  "error": null
}
```

With `--diagnostics`, successful SearXNG JSON output also includes a
`diagnostics` object. Without `--diagnostics`, the key is omitted entirely
(there is no `diagnostics: null`).

```json
{
  "diagnostics": {
    "answers": [],
    "suggestions": [],
    "infoboxes": [],
    "unresponsive_engines": [],
    "number_of_results": 0
  }
}
```

`answers`, `infoboxes`, and other SearXNG-version-sensitive arrays are passed
through conservatively instead of being normalized into a strict sx schema.
`--strict-engines` only has an effect when `--engines` is also set. It adds
warnings when requested SearXNG engines are reported under
`unresponsive_engines`, or when result metadata includes engines outside the
requested set. It does not filter results, fail the command, or change the exit
code.

Failure:

```json
{
  "ok": false,
  "query": "example",
  "backend": { "requested": "searxng", "used": null, "fallback_used": false, "fallback_reason": "", "cost_tier": "self_hosted" },
  "timing": { "total_ms": 12 },
  "results": [],
  "warnings": [],
  "error": {
    "code": "NETWORK",
    "message": "all backends failed: ...",
    "backend": "searxng",
    "retryable": true,
    "retry_after_seconds": null,
    "hint": "check network connectivity and the backend URL"
  }
}
```

`error.code` is one of `BACKEND_UNAVAILABLE`, `NETWORK`, `AUTH`, `RATE_LIMIT`,
`INVALID_RESPONSE` (plus `CONFIG_ERROR` / `INVALID_ARGUMENT` / `INVALID_INPUT`
for usage errors). `retry_after_seconds` is always `null` in this release (no
reliable source); `retryable` is best-effort. API keys, basic-auth, tokens, and
credential-bearing URLs are redacted from all output.

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success (including a successful search with zero results) |
| 1 | Backend or search failure (e.g. all backends failed) |
| 2 | Usage, argument, or configuration error |
| 3 | Validation (`config validate`) or health check (`health`) failure |

## Capability matrix and gaps

sx maps the core search parameters of each backend. Some officially supported
parameters are intentionally **not** implemented in this phase (documented gaps,
not silent omissions):

- **SearXNG:** covered -- `q`, `categories`, `engines`, `language`, `pageno`,
  `time_range`, `safesearch`, `num`, `format=json`. Not exposed (UI/instance-level,
  irrelevant to CLI result extraction): `results_on_new_tab`, `image_proxy`,
  `autocomplete`, `theme`, `enabled/disabled_plugins`. Note: `time_range=week`
  is not an official SearXNG enum -- it is passed through and may be ignored by
  some engines.
- **Tavily:** covered -- `query`, `search_depth` (basic/advanced),
  `max_results`, `include_answer`, `include_raw_content`. Gaps (not implemented
  this phase): `topic`, `days`, `time_range`, `start_date`/`end_date`,
  `include_domains`/`exclude_domains`, `chunks_per_source`, `country`,
  `auto_parameters`. Note: the `-r/--time-range` flag currently affects only
  SearXNG, not Tavily.

## Search semantics

sx returns search metadata only -- URL, title, snippet, date, and source. It
does not fetch article bodies for normal search/JSON output. (`--text`/`--html`
are separate, explicit content-extraction modes.)

## Troubleshooting

**Error: all backends failed**
Check your primary engine URL and API keys. Use `--debug` for details.

**Error: HTTP 429 Too Many Requests**
SearXNG rate limiting. Update server limiter settings or use a fallback engine.

**Error: failed to parse JSON response**
Enable JSON format in SearXNG's `settings.yml`:
```yaml
search:
  formats:
    - html
    - json
```

## Dependencies

- [cobra](https://github.com/spf13/cobra) - CLI framework
- [toml](https://github.com/BurntSushi/toml) - Configuration
- [color](https://github.com/fatih/color) - Terminal colors
- [go-readability](https://github.com/go-shiori/go-readability) - Content extraction
- [html-to-markdown](https://github.com/JohannesKaufmann/html-to-markdown) - HTML to Markdown
