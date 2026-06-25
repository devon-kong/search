# Routing and paid policy

Routing decisions, the X / Twitter exclusion policy, the paid/fallback gate and confirmation flow,
and the safety boundaries.

## Table of contents

1. Routing decision tree (search / body-fetch / local grep / no-op)
2. Default-safe posture
3. The four-state gate (fallback / paid)
4. Quota-risk wording template
5. Four-state machine-readable table
6. X / Twitter exclusion policy
7. Safety boundaries (incl. the A1 red line)

---

## 1. Routing decision tree

| Situation | Route |
|---|---|
| Explicit "search / look up", time-sensitive info, news, fact-check | **This skill — search** |
| "Read / fetch the body of this URL" | **A fetch tool — not search.** This skill does not fetch bodies |
| "Find XX in the codebase / local files" | **Local grep — no network** |
| General-knowledge / concept Q&A, translation / writing / arithmetic / editing code | **No trigger** |

Key boundary: `sx` itself has `--text` / `--html` body extraction, but the Phase 2 design rule is
that **this skill does not handle page bodies**. Even so, route "I need the body" to a fetch tool;
do not push `--text` on the normal path. The reason is single responsibility: this skill does one
thing — "search → URL + metadata."

## 2. Default-safe posture

- Default backend: SearXNG only.
- Automatic fallback: **off** by default (`fallback_engines=[]`). Never inject fallback config.
- Tavily / Exa-API (paid) are not used by default.
- Safe-search default is `strict`.

## 3. The four-state gate (the core)

1. **SearXNG only by default**; automatic fallback off. The skill never enables fallback or slips
   fallback config into a command.
2. **No Tavily by default.** Tavily is `paid`, and in auto-fallback it is blocked by
   `allow_paid_fallback=false`.
3. **The paid gate blocks only auto-fallback, never the user's explicit choice** (the most common
   misunderstanding — pin it down):
   - **Explicit `--engine tavily` (or any paid backend) always runs** and consumes quota. Respect
     the user's intent; the gate does not block it. Before running, say in one line "this is a paid
     backend and will use quota" — inform, don't block.
   - **Automatic paid fallback** (SearXNG fails → auto-fall to Tavily) is gated and **requires
     explicit user confirmation** of the quota risk before it proceeds.
4. **Respect the user's existing config; don't mislabel it as skill-initiated.** If the user's
   `config.toml` already has `allow_paid_fallback=true` and `fallback_engines` includes tavily, sx
   may auto-use Tavily after SearXNG fails. Detect this by `backend.fallback_used=true` **and**
   `backend.used="tavily"`, respect the sx result, and explain to the user "this is because your
   config already enabled paid fallback" — **not** "the skill chose Tavily." The skill never adds
   `--engine tavily` on its own and never rewrites fallback config.

## 4. Quota-risk wording template

Before a paid path fires, explain plainly: "Tavily / Exa-API are paid backends billed per call
(see the README for the exact quota model). Want me to proceed?" Then **wait for confirmation**
before allowing automatic fallback. For an explicit `--engine` choice by the user, inform only — do
not block.

## 5. Four-state machine-readable table

| State | Machine signal |
|---|---|
| **Search failure** (backend/network down) | `ok:false` + `error.code ∈ {BACKEND_UNAVAILABLE, NETWORK, RATE_LIMIT, INVALID_RESPONSE}`, exit code 1 |
| **Config error** | `error.code ∈ {CONFIG_ERROR, INVALID_ARGUMENT, INVALID_INPUT}`, exit code 2; `config validate` failure is exit code 3 |
| **Quota risk** (paid) | a paid backend skipped during auto-fallback → an entry in `warnings`; or `backend.cost_tier="paid"` |
| **No results** (not a failure) | `ok:true` + `results:[]`, exit code 0 |

## 6. X / Twitter exclusion policy

General web searches exclude X / Twitter content **by default** as a policy stance. This is "the
default filter applied while searching," not "trigger whenever X is mentioned." A request aimed
**specifically** at searching X / Twitter does **not** trigger this skill — route it elsewhere.

**Mechanism is partially tested — still not confirmed reliable (open item).** `sx` exposes **no
first-class exclusion flag** in its README contract:

- The most likely viable approach is passing the query operators `-site:x.com -site:twitter.com`
  (handled as exclusion by SearXNG / the upstream engine), but the **README does not promise this**.
- `--engines` is **whitelist** semantics — it does not mean "exclude" and is not the right tool.
- Writing `engines` into `config.toml` is the forbidden **A1 dead key**.

**Smoke test observed (2026-06-25, via SSH tunnel to the live SearXNG):**

- **The default result set is unstable.** The same query (`elon musk`) returned 48 results in one
  run and 29 in the next; whether any `x.com` URL shows up varies run to run.
- **The default already surfaces almost no X.** Across 5 queries with exact-host matching, X-domain
  hits were essentially 0 — the lone exception was a single `https://x.com/elonmusk` in one run.
- **The operator looks viable but is unproven.** In that one run, adding `-site:x.com
  -site:twitter.com` dropped the `x.com` hit to 0. But with n=1 and an unstable default that rarely
  surfaces X at all, this is **not** enough to call the mechanism reliable.
- **When you verify, match domains by exact host** (`urllib.parse(url).netloc`), never substring:
  `"x.com" in url` falsely flags `complex.com`, `xx.com`, etc. (this bit us once).

Until a repeated, multi-query test confirms it, **do not hardcode any exclusion command or flag as
established usage.** The default-exclude posture in the description still holds as a *policy stance*
(the default genuinely surfaces almost no X); only the *active-exclusion mechanism* stays open.

Note also (WC-7): whether the default upstream engine set already includes x/twitter depends on the
specific SearXNG instance's `/config`; you can check with `sx searxng engines --json` (the ~7–15s
diagnostic path), which is not part of the default search path.

## 7. Safety boundaries (A1 red line)

1. **Zero sensitive material in examples** — no real API keys / tokens, no credential-bearing URLs,
   no real instance host. Use `searxng.example.com` / `example.com` placeholders.
2. **Rely on sx's redaction; don't reinvent it, and don't undo it.** Keys / basic-auth / tokens /
   credential-bearing URLs are already redacted across all sx outputs.
3. **Core red line: never echo sensitive stderr.** Parse stdout JSON only; do not echo stderr by
   default; do not add `--debug` by default; even when troubleshooting, relay only the structured
   error (`error.code` / `error.hint`), never raw stderr (internal URLs, stacks, headers).
4. **A1 dead-config red line:** never suggest writing `categories` / `language` / `engines` /
   `url_handler` into `config.toml`; make those choices with CLI flags only.
