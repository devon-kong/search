---
name: searxng-direct-search
description: Use when web search quality, language control, or diagnostics matter; query the local SearXNG JSON API directly instead of relying on Hermes built-in web_search.
version: 1.0.0
author: Hermes Agent
license: MIT
metadata:
  hermes:
    tags: [searxng, search, research, diagnostics, hermes]
    related_skills: [hermes-agent]
---

# SearXNG Direct Search

## Overview

Use this workflow when search results need higher control or diagnosis than Hermes built-in `web_search` exposes. The built-in SearXNG provider returns normalized `results`, but hides useful SearXNG fields such as `unresponsive_engines`, `answers`, `suggestions`, and `infoboxes`.

The SearXNG endpoint is not hardcoded. It is resolved at runtime (see "Endpoint Resolution"), defaulting to `http://127.0.0.1:13001/search` on this machine.

Principle: prefer direct SearXNG JSON API calls for precision and diagnostics; do not modify Hermes search provider source code unless the user explicitly approves.

## Endpoint Resolution

Do not hardcode the SearXNG URL. Resolve the base URL in this order (first match wins):

1. Environment variable `SEARXNG_URL` (ad-hoc override).
2. Config file `~/.config/searXNG/searxng.env`, line `SEARXNG_URL=...`.
3. Fallback default `http://127.0.0.1:13001`.

The search endpoint is `<resolved_url>/search` (trailing slash trimmed).

Config file format (`~/.config/searXNG/searxng.env`, stdlib-parseable, also shell-sourceable):

```plain
SEARXNG_URL=http://127.0.0.1:13001
```

To point the agents at a different instance, edit that one file (or export `SEARXNG_URL`); no skill edit needed. The Python templates below embed a `searxng_base()` helper that implements this resolution.

## When to Use

Use this skill when:

- The user asks to search, research, compare sources, or diagnose search quality and precision matters.
- Hermes `web_search` returns empty, sparse, obviously biased, or low-quality results.
- You need to inspect why search quality is poor, especially upstream engine failures.
- You need explicit control over `language`, `categories`, `safesearch`, or pagination.
- The user asks about Chinese vs English result behavior.
- The user is concerned about Hermes updates overwriting local code changes.
- You need to see SearXNG diagnostic fields: `unresponsive_engines`, `answers`, `suggestions`, `infoboxes`.

Do not use this skill when:

- A simple web search is enough and diagnostics do not matter.
- The task is URL extraction or page reading; use `web_extract` / browser tools instead.
- The task requires interactive browser behavior.
- The user explicitly asks to use Hermes built-in `web_search`.

## Boundaries

By default, this workflow is read-only:

- Do not edit Hermes source code.
- Do not edit SearXNG configuration.
- Do not restart Docker containers.
- Do not hardcode `language=zh-CN` globally.
- Do not set SearXNG `default_lang: zh-CN` globally without explicit approval.
- If configuration changes are needed, present a plan, side effects, and rollback path first.

## Local Facts (this machine)

Local SearXNG on this Mac mini, shared by Hermes and CLI agents (Codex / Claude Code):

```plain
endpoint     : resolved via config (see Endpoint Resolution), default http://127.0.0.1:13001
instance     : Devon SearXNG
json api     : GET /search?format=json -> 200 application/json
safe_search  : 1 (instance default)
autocomplete : duckduckgo
default_lang : unset (no global language) -> language must be a per-query choice, never a global default
engines      : ~88 enabled (of 245 available)
```

Upstream engine flakiness (CAPTCHA / rate limits on engines such as brave, duckduckgo, startpage) is common and intermittent. Do not assume which engines are down; read `unresponsive_engines` from each JSON response to see the actual per-query state. This prevents misdiagnosing upstream engine blocks as a SearXNG total failure.

## Default Query Strategy

Default API parameters:

```plain
q=<query>
format=json
pageno=1
```

For ordinary web search, add:

```plain
categories=general
```

Do not add `language` by default. SearXNG can choose broader results when no language is specified.

## Language Strategy

Do not globally set `language=zh-CN`.

Rules:

- Default: omit `language`.
- User explicitly wants Chinese sources: use `language=zh-CN`.
- User explicitly wants English/international sources: use `language=en-US`.
- Query is clearly Chinese and Chinese sources are preferred: consider `zh-CN`.
- Query is clearly English and global/technical sources are preferred: consider `en-US`.
- Mixed Chinese/English technical query: usually omit `language` first, then compare if results are poor.

Known effect from local tests: adding `language=zh-CN` can change the selected engines and bias English technical queries toward Chinese results. It should be a per-query choice, not a default.

## Category Strategy

Recommended starting point:

```plain
categories=general
```

Use other categories only when the user intent is clear:

- `news` for news searches.
- `it` only for targeted technical vertical searches. It may return overly narrow sources such as Docker Hub or MDN and can be worse for broad technical research.
- Avoid image/video categories unless the user asks for those media.

## Safesearch Strategy

The local instance currently has `safe_search: 1`.

Default: rely on instance config.

For diagnosis, compare with:

```plain
safesearch=0
```

Do not force `safesearch=0` as a default unless the user accepts broader/less filtered results.

## Engine Strategy

Do not pass `engines` by default.

Reason: hardcoding engines bypasses the instance's configured search mix and can make results worse when one engine is blocked by CAPTCHA or rate limits.

For diagnosis only, temporary comparisons may use:

```plain
engines=google
engines=duckduckgo
```

Treat those as probes, not permanent defaults.

## Recommended Python Template

Use Python stdlib so no `jq` or extra package is required:

```bash
python3 - <<'PY'
import urllib.parse, urllib.request, json, time, os, pathlib

def searxng_base():
    url = os.environ.get("SEARXNG_URL")
    if not url:
        cfg = pathlib.Path.home() / ".config" / "searXNG" / "searxng.env"
        if cfg.is_file():
            for line in cfg.read_text().splitlines():
                line = line.strip()
                if line.startswith("SEARXNG_URL=") and not line.startswith("#"):
                    url = line.split("=", 1)[1].strip().strip('"').strip("'")
                    break
    return (url or "http://127.0.0.1:13001").rstrip("/") + "/search"

base = searxng_base()
params = {
    "q": "QUERY_HERE",
    "format": "json",
    "pageno": "1",
    "categories": "general",
}

url = base + "?" + urllib.parse.urlencode(params)
t0 = time.time()

with urllib.request.urlopen(url, timeout=20) as r:
    body = r.read()
    data = json.loads(body)
    status = r.status
    content_type = r.headers.get("content-type")

results = data.get("results") or []
print("http_status=", status)
print("elapsed_s=", round(time.time() - t0, 2))
print("content_type=", content_type)
print("result_count=", len(results))
print("answers_count=", len(data.get("answers") or []))
print("suggestions_count=", len(data.get("suggestions") or []))
print("infoboxes_count=", len(data.get("infoboxes") or []))
print("unresponsive_engines=", data.get("unresponsive_engines") or [])

for i, r in enumerate(results[:10], 1):
    print(f"{i}. {r.get('title', '')}")
    print(f"   {r.get('url', '')}")
    print(f"   engine={r.get('engine')} score={r.get('score')} category={r.get('category')}")
PY
```

## Multi-Query / Language Comparison Template

Use this when comparing default vs language-specific behavior:

```bash
python3 - <<'PY'
import urllib.parse, urllib.request, json, time, os, pathlib

def searxng_base():
    url = os.environ.get("SEARXNG_URL")
    if not url:
        cfg = pathlib.Path.home() / ".config" / "searXNG" / "searxng.env"
        if cfg.is_file():
            for line in cfg.read_text().splitlines():
                line = line.strip()
                if line.startswith("SEARXNG_URL=") and not line.startswith("#"):
                    url = line.split("=", 1)[1].strip().strip('"').strip("'")
                    break
    return (url or "http://127.0.0.1:13001").rstrip("/") + "/search"

base = searxng_base()
query = "QUERY_HERE"
cases = [
    ("default", {"q": query, "format": "json", "pageno": "1", "categories": "general"}),
    ("zh-CN", {"q": query, "format": "json", "pageno": "1", "categories": "general", "language": "zh-CN"}),
    ("en-US", {"q": query, "format": "json", "pageno": "1", "categories": "general", "language": "en-US"}),
]

for name, params in cases:
    url = base + "?" + urllib.parse.urlencode(params)
    t0 = time.time()
    try:
        with urllib.request.urlopen(url, timeout=20) as r:
            data = json.loads(r.read())
    except Exception as exc:
        print("\n" + name, "ERROR", type(exc).__name__, exc)
        continue

    results = data.get("results") or []
    engines = sorted({r.get("engine") for r in results if r.get("engine")})
    print("\n" + name, "elapsed=", round(time.time() - t0, 2), "results=", len(results), "engines=", engines)
    print("unresponsive=", data.get("unresponsive_engines") or [])
    for i, r in enumerate(results[:5], 1):
        print(f"  {i}. [{r.get('engine')} {r.get('score')}] {r.get('title', '')[:100]}")
        print(f"     {r.get('url', '')}")
PY
```

## Output Format for User-Facing Reports

When reporting search results from direct SearXNG, include both results and diagnostics:

```plain
Query: ...
Params: ...
Result count: ...
Unresponsive engines: ...

Top results:
1. title
   url
   engine / score / category
```

If `unresponsive_engines` is non-empty, explicitly call it out. Example:

```plain
Search worked, but several upstream engines failed:
- brave: too many requests
- duckduckgo: CAPTCHA
- startpage: CAPTCHA
```

This prevents misdiagnosing upstream engine blocks as Hermes or SearXNG total failure.

## Diagnosis Checklist

When SearXNG search seems bad:

1. Confirm HTTP status is 200.
2. Confirm JSON parses successfully.
3. Check `result_count`.
4. Check `unresponsive_engines`.
5. Compare default language vs `zh-CN` vs `en-US` if language bias matters.
6. Compare with `categories=general` if no category was used.
7. Compare `safesearch=0` only if safe search may be filtering results.
8. Avoid editing Hermes source code as a first response.
9. If engine failures are persistent, propose SearXNG config changes separately and ask for confirmation.

## Common Pitfalls

1. **Hardcoding `language=zh-CN`.** This can bias English technical queries and change engine selection. Use it only when Chinese results are desired.

2. **Assuming empty results mean SearXNG is down.** Always inspect `unresponsive_engines`; upstream CAPTCHA/rate limits are common.

3. **Using Hermes `web_search` for diagnostics.** It hides fields needed to understand failures. Use direct API calls instead.

4. **Overusing `categories=it`.** It can narrow results too aggressively and return irrelevant vertical sources. Start with `general`.

5. **Editing Hermes provider code locally.** Updates may overwrite local changes or create conflicts. Prefer direct SearXNG calls or upstream PRs.

6. **Changing SearXNG config without approval.** Config changes can affect all future searches and should be planned explicitly.

## Verification Checklist

- [ ] Direct API call used `format=json`.
- [ ] Results and `unresponsive_engines` were inspected.
- [ ] Language was omitted by default or chosen intentionally.
- [ ] No Hermes source code was modified.
- [ ] No SearXNG config was modified unless explicitly approved.
- [ ] User-facing answer includes search diagnostics when relevant.
