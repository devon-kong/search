# Contracts and errors

How to parse the `sx --json` output and handle failures. The exit code is the primary machine
signal; the JSON `ok` / `error.code` provide the detail. The contract guarantees they agree.

## Table of contents

1. Output split (iron rule)
2. Success envelope, field by field
3. Failure envelope, field by field
4. The 8 `error.code` values + handling by code
5. Exit codes + "exit code is the primary signal"
6. `cost_tier` and the paid prompt
7. Relaying `warnings[]`
8. Redaction

---

## 1. Output split (iron rule)

Under `--json`, **stdout carries exactly one legal JSON envelope**. All logs / warnings / debug /
quota notices go to **stderr**. Parse stdout only. Never treat stderr as JSON, and never echo
sensitive content from stderr back to the user. (sx already redacts keys / basic-auth / tokens /
credential-bearing URLs, but the skill still must not echo raw stderr.)

## 2. Success envelope (field by field)

```json
{
  "ok": true,
  "query": "<query>",
  "backend": { "requested": "searxng", "used": "searxng", "fallback_used": false, "fallback_reason": "", "cost_tier": "self_hosted" },
  "timing": { "total_ms": 42 },
  "results": [ /* result objects */ ],
  "warnings": [],
  "error": null
}
```

- `results` — **never `null`**; empty results are `[]`. Zero results is still success (exit `0`).
- `warnings` — array, never `null`. A paid backend skipped during auto-fallback (and similar) shows
  up here; **report it** to the user.
- `backend.requested` / `used` — the backend requested vs the one actually used (on failure `used`
  may be `null`).
- `backend.fallback_used` — `true` only when a real fallback fired.
- `backend.fallback_reason` — on success with no fallback the README example is `""` (empty
  string). (Phase 1 prompt wrote `null`; the README is authoritative. Parse tolerantly: treat both
  `""` and `null` as "no fallback reason." — open item WC-5.)
- `backend.cost_tier` — one of `self_hosted` / `free_external` / `paid` / `unknown`; if `paid`,
  prompt the user.
- `diagnostics` — present **only** with `--diagnostics`; otherwise the key is omitted entirely
  (there is no `diagnostics: null`). Contains `answers[]` / `suggestions[]` / `infoboxes[]` /
  `unresponsive_engines[]` / `number_of_results`, passed through conservatively by SearXNG version.
  Parse defensively; do not assume a strong schema (WC-8).

## 3. Failure envelope (field by field)

```json
{
  "ok": false,
  "query": "<query>",
  "backend": { "requested": "searxng", "used": null, "fallback_used": false, "fallback_reason": "", "cost_tier": "self_hosted" },
  "timing": { "total_ms": 12 },
  "results": [],
  "warnings": [],
  "error": { "code": "NETWORK", "message": "all backends failed: ...", "backend": "searxng", "retryable": true, "retry_after_seconds": null, "hint": "check network connectivity and the backend URL" }
}
```

- `error.code` — see §4. **Branch on the code** (retryable vs config error vs rate limit); don't
  rely on `message` text alone.
- `error.message` — human-readable; for multi-backend failure it is a multi-line string listing each
  backend tried. Already redacted.
- `error.retry_after_seconds` — **always `null`** in this version; do not use it for back-off.
- `error.hint` — a user-facing next step.

## 4. `error.code` enumeration (exactly 8) + handling

| code | class | typical case | exit-code tendency | handling |
|---|---|---|---|---|
| `BACKEND_UNAVAILABLE` | backend/search failure | backend unreachable / no backend available | 1 | report as a transient backend issue; retry is reasonable |
| `NETWORK` | backend/search failure | network error, "all backends failed" | 1 | check connectivity; retryable |
| `AUTH` | backend/search failure | invalid key/credential | 1 | tell the user the backend's key is missing/invalid; do not retry blindly |
| `RATE_LIMIT` | backend/search failure | 429 throttling | 1 | back off / try later; `retry_after_seconds` is `null`, so don't depend on it |
| `INVALID_RESPONSE` | backend/search failure | backend returned something unparseable | 1 | report; possibly retry with another backend on the user's request |
| `CONFIG_ERROR` | usage class | configuration error | 2 | a config problem; surface `error.hint` |
| `INVALID_ARGUMENT` | usage class | illegal flag/argument | 2 | usually the skill mis-assembled the command — fix the template |
| `INVALID_INPUT` | usage class | illegal input | 2 | fix the input |

(`code → exit code` is a **tendency** map; the actual exit code is authoritative — WC-6.)

## 5. Exit codes (primary machine signal)

| exit code | meaning | skill response |
|---|---|---|
| `0` | success (**including 0 results**) | parse `results[]`, which may be empty |
| `1` | backend or search failure | read `error.code`, branch on retryable / rate-limit / auth |
| `2` | usage, argument, or config error | fix the command template (usually a skill arg-assembly bug) |
| `3` | validation (`config validate`) / health (`health`) failure | only from diagnostic subcommands; tell the user to check config/backend |

Use the **exit code as the primary signal**; JSON `ok` / `error.code` are the breakdown. The
contract guarantees they agree.

## 6. `cost_tier` and the paid prompt

`backend.cost_tier ∈ {self_hosted, free_external, paid, unknown}`. On `paid`, prompt the user that
the result came from (or would come from) a paid backend. See `routing-and-paid-policy.md` for the
gate and wording.

## 7. Relaying `warnings[]`

`warnings[]` is never `null`. Entries such as "paid backend skipped during auto-fallback" must be
relayed to the user, not swallowed — they explain why a result set is what it is.

## 8. Redaction

sx already redacts API keys / basic-auth / tokens / credential-bearing URLs across **all** outputs,
including the failure envelope's `error.message` and `warnings`. Trust this layer and **do not undo
it**: never echo raw stderr (instance addresses, internal diagnostics, stack traces). When relaying
an error, restate the structured fields (`error.code`, `error.hint`) only.
