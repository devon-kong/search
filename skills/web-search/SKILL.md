---
name: web-search
description: >-
  Search the live web and return ranked results as a structured JSON envelope (title, URL,
  summary, source) — links and metadata, not page bodies. Use whenever the user asks to search
  or look something up ("搜一下", "帮我查", "查一下", "search for", "look up"), asks about anything
  time-sensitive ("最新", "现在", "今天", "近期", latest prices, recent progress, what's happening
  now), wants news or recent coverage, or needs a fact checked online when your knowledge may be
  stale. Reach for it even without the word "search" — if a correct answer depends on current,
  external, verifiable info, search. General web searches exclude X / Twitter by default. Do NOT
  use it to fetch or read the body of a URL the user hands you (page extraction — use a fetch
  tool), to search local code or files (use grep), to answer general-knowledge questions you
  already know, for tasks needing no lookup (translation, writing, arithmetic, editing code), or
  for searches aimed at X / Twitter (use a different tool).
---

# web-search

This skill drives the local `sx` CLI to search the live web and hand back a structured JSON
envelope. Everything below is about **how to call it and the discipline of calling it** — when to
trigger lives entirely in the description above, not here.

## What this returns

A search-results JSON envelope: each result carries a title, URL, summary (the `content` field), and source metadata.
You get **links and metadata, not page bodies**. If the user actually needs the full text of a
specific page, that is page extraction, not search — route it to a fetch tool, not this skill.
Treat the JSON on stdout as the single source of truth and report it faithfully.

## Search workflow

One pass, in order — each step's detail lives in the named section below:

1. **Build** the command from the user's intent → default shape `sx --json --num 5 "<query>" </dev/null`
   (non-default → *Command recipes*). Input: user query · Output: one runnable command, stdin closed.
2. **Run once.** Exactly one `sx` call, no retry-on-success. Output: a JSON envelope on stdout + exit code `0/1/2/3`.
3. **Gate on `ok` + exit code.** `ok:true` & exit `0` → step 4; anything else → *Failure modes*.
4. **Read** the envelope — `results[]`, `warnings[]`, `backend.*` (see *Reading the result*). Output: the fields to report.
5. **Report faithfully** — results + every `warnings[]` + paid `cost_tier`; never fabricate a missing field
   (an empty `content`, say). Output: an answer grounded only in stdout.

## Default command

```
sx --json --num 5 "<query>" </dev/null
```

**Always close stdin with `</dev/null`.** When `sx` runs with stdin attached to a non-TTY (any
agent / Bash-tool invocation), it blocks waiting for stdin EOF and **hangs indefinitely** — even
when the query is passed as an argument. Appending `</dev/null` makes it return immediately. This
is confirmed by smoke testing; treat it as mandatory for every `sx` call this skill issues.

`--json` is mandatory — it gives you a single machine-readable envelope on stdout. `--num 5` is a
**skill-policy default** chosen to save tokens; the user can override it (the `sx` CLI's own
default is 10). The default backend is the self-hosted SearXNG instance with no automatic
fallback. Pass options as **CLI flags only** — never write `categories`/`language`/`engines`/
`url_handler` into `config.toml`; those keys are silently ignored there (this is the A1 dead-config
trap). For anything beyond the default shape, read `references/command-recipes.md`.

## Reading the result

A real (trimmed) envelope from `sx --json` looks like this:

```json
{
  "ok": true,
  "results": [
    {"title": "What Is Ownership?", "url": "https://doc.rust-lang.org/book/ch04-01.html", "content": "Ownership is a set of rules that govern how a Rust program manages memory…", "source": "searxng"}
  ],
  "warnings": [],
  "backend": {"used": "searxng", "cost_tier": "self_hosted", "fallback_used": false}
}
```

The per-result summary lives in the **`content`** field (sx's field name) — there is no `snippet`
key. Read `content`. Parse only stdout, and read it honestly. Always check and, where relevant,
surface to the user:

- `ok` — whether the search succeeded.
- `results[]` — the hits. Never `null`; an empty `[]` with exit code `0` means a successful search
  with zero results, not a failure.
- `warnings[]` — never `null`; e.g. a paid backend skipped during auto-fallback shows up here.
  Relay these to the user rather than swallowing them.
- `backend.fallback_used` / `backend.fallback_reason` — whether a fallback actually fired and why.
- `backend.cost_tier` — if it is `paid`, tell the user.

On failure you get `ok:false` plus `error.code`. Branch on the **exit code** (`0/1/2/3`) and on
`error.code` — do not swallow errors and do not guess from the message text alone. Full field-by-
field contract, the 8 `error.code` values, and exit-code handling are in
`references/contracts-and-errors.md`.

## Failure modes

Match the symptom to a row and follow it across. Branch on the **exit code** (`0/1/2/3`) and
`error.code`, never on message text alone. Full field/error contract: `references/contracts-and-errors.md`.

| Trigger (what you observe) | First fix | If it still fails |
|---|---|---|
| `sx: command not found` | Non-login shells skip `~/.local/bin` — call `sx` by its full install path | Report `sx` is not installed; do not silently swap in another tool |
| Call hangs, never returns | You dropped `</dev/null` — re-issue with it appended (mandatory on every call) | Still hanging with it → treat as an environment stall, stop and report |
| `ok:false`, exit `1/2/3` | Read `error.code` (8 values) + the exit code and branch on those | Surface `error.code` + exit code verbatim; never guess from the message text |
| `ok:true`, `results:[]`, exit `0` | A **successful** search with zero hits — say "no results", optionally broaden the query | Never retry on a paid backend to force hits — that trips the paid gate |
| SearXNG backend fails | Default is SearXNG-only, no auto-fallback — report the failure plainly | Escalate to paid only via the 🔴 CHECKPOINT gate below; never auto-escalate |
| Summary looks "missing" | You're reading `snippet`; sx's summary field is `content` — read that instead | If `content` is genuinely empty (some media hits), report title + URL only |
| `warnings[]` non-empty | Relay every warning to the user verbatim; do not swallow | — |

## Command recipes

The default and most common shapes:

- Default search: `sx --json --num 5 "<query>"`
- News: `sx --json --num 5 --news "<query>"` (`--news` = the `news` category shortcut)
- Pick the **sx backend** (singular): `sx --json --num 5 --engine brave "<query>"`
- Pick **SearXNG upstream engines** (plural): `sx --json --num 5 --engine searxng --engines google,duckduckgo "<query>"`
- Upstream engine inventory (non-default, slow): `sx searxng engines --json`
- Diagnostics passthrough (troubleshooting only): `sx --json --diagnostics "<query>"`

Keep `--engine` (chooses which sx backend) and `--engines` (chooses SearXNG upstream engines)
straight — they are different flags. The complete template matrix lives in
`references/command-recipes.md`; read it before using any non-default shape.

## Discipline (do not do by default)

- **Do not enable fallback by default.** The default is SearXNG only; never inject fallback config
  into a command.
- **Do not reach for Tavily (or any paid backend) by default.** Paid auto-fallback is gated.
- **Do not fetch page bodies.** This skill does search only; route body extraction to a fetch tool.
- **Do not auto-run `sx searxng engines`.** It hits `/config` (~7–15s) and is a diagnostic path,
  not part of normal search.
- **`sx searxng raw` is for diagnostics only** — it has no sx envelope and no paid fallback.

The reasoning behind each of these is in `references/routing-and-paid-policy.md`.

## When the user wants paid / fallback / Tavily

The paid gate only blocks **automatic** fallback, never the user's explicit choice:

- If the user explicitly asks for a paid backend, e.g. `sx --json --engine tavily "<query>"`, it
  **will** run and consume quota. Honor it, but say in one line "this is a paid backend and will
  use quota" first — inform, don't block.
🔴 **CHECKPOINT · 🛑 STOP — automatic paid fallback.** If SearXNG fails and the only way forward is
sx falling back to a paid backend (Tavily/Exa), **do not let it proceed automatically.** State the
quota cost in one line, then **wait for the user's explicit confirmation** before re-running with
fallback. No confirmation → stop and report the SearXNG failure instead; never spend quota on the
user's behalf unprompted.
- If the user's own `config.toml` already enables paid fallback, sx may auto-use Tavily on SearXNG
  failure (`backend.fallback_used=true`, `backend.used="tavily"`). Report it as "your config has
  paid fallback enabled," not as the skill choosing Tavily. Never add `--engine tavily` or rewrite
  fallback config on your own.

See `references/routing-and-paid-policy.md` for the gate logic and quota-warning wording.

## Excluding X / Twitter

General web searches exclude X / Twitter content by default as a policy stance.

**Important — mechanism tested once, still not confirmed.** `sx` exposes **no first-class exclusion
flag** in its README contract. The likely approach is the query operators `-site:x.com
-site:twitter.com`, but the README does **not** promise this. A first smoke test (2026-06-25, live
SearXNG) found the default result set **unstable** and **already surfacing almost no X**; the
operator did drop the one `x.com` hit that appeared, but with n=1 that is not proof. **Do not
hardcode any exclusion command or flag as established usage** until a repeated multi-query test
confirms it. Note also that `--engines` is whitelist semantics (it does not mean "exclude"), and
writing `engines` into `config.toml` is the forbidden A1 dead key.

Details and the open question are in `references/routing-and-paid-policy.md`.

## References

Read these on demand (progressive disclosure):

- `references/command-recipes.md` — full command template matrix (default / `--news` / `--engine`
  vs `--engines` / inventory / diagnostics), CLI flags only. Read when you need a non-default shape.
- `references/contracts-and-errors.md` — JSON envelope fields, the 8 `error.code` values, exit
  codes `0/1/2/3`, stdout/stderr split. Read when parsing a result or handling a failure.
- `references/routing-and-paid-policy.md` — routing (search vs body-fetch vs local grep vs no-op),
  the X / Twitter exclusion policy, the paid/fallback gate and confirmation flow, safety
  boundaries. Read when facing a paid / body-fetch / X-exclusion decision.
- `references/smoke-and-evals.md` — the 4 smoke checks, candidate eval prompts, acceptance
  criteria. Read during the scaffold / verification stage.
