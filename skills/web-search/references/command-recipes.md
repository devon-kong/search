# Command recipes

Full command template matrix for the `web-search` skill. **CLI flags only** — never write
`categories`/`language`/`engines`/`url_handler` into `config.toml` (the A1 dead-config trap:
silently ignored). All `<query>` are placeholders; any instance host is shown as
`searxng.example.com`, never a real URL.

**Stdin rule (critical):** append `</dev/null` to every `sx` invocation in non-interactive / agent
contexts. With stdin attached to a non-TTY, `sx` blocks on stdin EOF and **hangs** even when the
query is an argument (confirmed by smoke). For brevity the templates below omit it — add it when you
run them, e.g. `sx --json --num 5 "<query>" </dev/null`.

## Table of contents

1. Default search quick reference (and the `--num 5` policy default)
2. `--engine` vs `--engines` (and `google` vs `"google news"`)
3. Recipe list (intent → command → expected exit code → parse focus)
4. Diagnostics & inventory (non-default paths)
5. Quoting & escaping
6. Not in this skill's path

---

## 1. Default search quick reference

```
sx --json --num 5 "<query>"
```

- `--json` is mandatory: one legal JSON envelope on stdout, logs on stderr.
- `--num 5` is a **skill-policy default** to save tokens, and is **overridable**. The `sx` CLI's
  own default is `10`. To get more results, raise it: `sx --json --num <N> "<query>"`. `--num 0`
  returns the full single page.
- Default backend = SearXNG (self-hosted); **no automatic fallback**.

## 2. `--engine` vs `--engines` (keep these straight)

| Flag | Selects | Form | Values |
|---|---|---|---|
| `--engine <string>` | the **sx backend** | singular | `searxng` / `brave` / `tavily` / `exa` / `jina` |
| `-e` / `--engines <strings>` | **SearXNG upstream engines** (passed through to SearXNG) | plural, comma-separated | `google` / `duckduckgo` / `"google news"` … (only meaningful when the sx backend is SearXNG) |

- `google` and `"google news"` are **different upstream engines**. `"google news"` contains a space
  and **must be quoted**.
- `--news` is the `news`-category shortcut (= `--categories news`). It is **not** the same as
  requesting the `google news` upstream engine. To use that engine, pass `--engines "google news"`
  (can be combined with `--news`).
- When using `--engines`, pinning `--engine searxng` explicitly is the safer form, since `--engines`
  only takes effect when the sx backend is SearXNG.

## 3. Recipe list

| Intent | Command | Expected exit code | Parse focus |
|---|---|---|---|
| Default search | `sx --json --num 5 "<query>"` | `0` (incl. 0 results) | `ok`, `results[]`, `backend.used`, `timing.total_ms` |
| More results | `sx --json --num <N> "<query>"` (or `--num 0` for full page) | `0` | `results[]` length |
| News | `sx --json --num 5 --news "<query>"` | `0` | `results[]` (news-type) |
| Pick sx backend (free external) | `sx --json --num 5 --engine brave "<query>"` (likewise `exa` / `jina`) | `0` / `1` | `backend.requested` vs `used`, `cost_tier` |
| Pick sx backend (Tavily, **paid**) | `sx --json --num 5 --engine tavily "<query>"` | `0` / `1` | `cost_tier="paid"` — explicit choice always runs and consumes quota; inform the user first |
| Pick SearXNG upstream engines | `sx --json --num 5 --engine searxng --engines google,duckduckgo "<query>"` | `0` / `1` | `results[]` source/engine metadata |
| Upstream engines + news | `sx --json --num 5 --engines "google news" --news "<query>"` | `0` / `1` | quote `"google news"`; stacks with `--news` |
| Time-range filter | `sx --json -r <day\|week\|month\|year> "<query>"` (also accepts `d/w/m/y`) | `0` | `results[]` |
| Single-site **include** | `sx --json -w example.com "<query>"` | `0` | `-w`/`--site` is **include-only**, no exclusion semantics |
| Safe-search | `sx --json --safe-search <none\|moderate\|strict> "<query>"` | `0` | default `strict`; only SearXNG/Brave honor it |

## 4. Diagnostics & inventory (non-default paths)

These are **not** part of the normal search path. Use only when investigating upstream config,
before pinning `--engines`, or while troubleshooting.

| Intent | Command | Notes |
|---|---|---|
| Engine inventory | `sx searxng engines --json` | reads `/config` (~7–15s). `source="searxng_config"`, `engine_count`, `engines[]`, `warnings[]`, `error`. Exit `0/1/2`. |
| Inventory (filter) | `sx searxng engines --json --category news --enabled` / `--filter google` | `--enabled` (default) and `--all` are mutually exclusive; `--filter` is substring match; `--category` is one at a time |
| Inventory (live probe) | `sx searxng engines --json --live --engines google,"google news"` | `--live` **requires** `--engines`, **max 5**; adds a `live{}` diagnostic only, does not change `enabled`, does not trigger fallback/paid |
| Diagnostics passthrough | `sx --json --diagnostics "<query>"` | on success appends a `diagnostics{}` block; troubleshooting only |
| Diagnostics + strict engines | `sx --json --diagnostics --engines google --strict-engines "<query>"` | `--strict-engines` only takes effect when `--engines` is also set; adds warnings only, does not filter or change the exit code |
| Raw SearXNG JSON | `sx searxng raw "<query>"` | diagnostics only. **No sx envelope**, does not support `--clean`, does not trigger paid fallback |

## 5. Quoting & escaping

- Always wrap `<query>` in double quotes.
- Upstream engine names containing spaces must be quoted: `--engines "google news"`.
- Comma-separated engine lists have no spaces around commas: `google,duckduckgo`.

## 6. Not in this skill's path

- **Body extraction** via `--text` / `--html` — this skill does **search only**; route body
  extraction to a fetch tool. Do not push `--text` on the normal path.
- **Writing `categories`/`language`/`engines`/`url_handler` to `config.toml`** — A1 dead key,
  silently ignored. **Forbidden.** Use the equivalent CLI flag instead.
