# Smoke checks and evals

The 4 smoke checks (commands + expected results, **to be verified on a real machine during the
scaffold stage**), how to read them, the `evals/evals.json` schema, and the 10 candidate eval
prompts. Commands use **CLI flags only**, no config rewrites; `<query>` are placeholders; the
SearXNG instance comes from the user's existing config — examples never show a real URL.

## Table of contents

1. The 4 smoke checks (command + expected + "verify on real machine")
2. Smoke read rules (exit code / key fields / stdout purity)
3. `evals/evals.json` minimal schema + how to fill it
4. The 10 candidate evals
5. Open items handed forward
6. Pre-conditions for real-machine verification

---

## 1. The 4 smoke checks

Each is marked **[to be verified on a real machine during the scaffold stage]**. None are run now.

1. **Default search → legal envelope** — *[to be verified on a real machine during the scaffold stage]*
   - Command: `sx --json --num 5 "<query>"`
   - Expected: exit 0; one legal JSON object on stdout; `ok:true`; `backend.requested="searxng"`,
     `used="searxng"`, `fallback_used=false`, `cost_tier="self_hosted"`; `results` is an array (zero
     results = `[]`); `error:null`.
2. **Explicit backend routes as expected** — *[to be verified on a real machine during the scaffold stage]*
   - Command: `sx --json --num 5 --engine brave "<query>"` (brave chosen so smoke does not touch
     paid by default)
   - Expected: `backend.requested="brave"`; `cost_tier="free_external"`; with a Brave key configured,
     `used="brave"`, `ok:true`; **without a key (verified 2026-06-24), `error.code=BACKEND_UNAVAILABLE`,
     `used=null`, exit 1** — a clean structured failure, not `AUTH` and not a crash.
3. **Structured failure (SearXNG unavailable)** — *[to be verified on a real machine during the scaffold stage]*
   - Command: `sx --json --searxng-url https://searxng.invalid.example "<query>"` (unreachable
     placeholder URL, flag only)
   - Expected: no crash, no swallowed error; exit 1; stdout is still legal JSON; `ok:false`;
     `error.code ∈ {NETWORK, BACKEND_UNAVAILABLE}`; `error` carries `message` / `hint` /
     `retryable`; `results:[]`; no automatic fallback.
4. **stdout not polluted under `--json`** — *[to be verified on a real machine during the scaffold stage]*
   - Command: `sx --json --num 5 "<query>" 2>/dev/null | <JSON parser>`; compared against
     `sx --json --debug …`
   - Expected: after discarding stderr, stdout parses in one pass as a single object with no noise;
     with `--debug`, the extra logs go to stderr only and stdout stays a single clean JSON object.

## 2. Smoke read rules

- **Exit code first**: `0` success (incl. 0 results), `1` backend/search failure, `2` usage/arg/
  config, `3` validation/health.
- **Key fields**: `ok`, `backend.{requested,used,fallback_used,cost_tier}`, `results` (array, never
  null), `error` (null on success; `code`/`message`/`hint`/`retryable` on failure).
- **stdout purity**: under `--json`, stdout must parse as exactly one JSON object; all logs go to
  stderr.

## 3. `evals/evals.json` minimal schema

Per `skill-creator/references/schemas.md`. At this stage write **prompt + expected_output only**;
do **not** write `expectations` (assertions). They are added later.

```json
{
  "skill_name": "web-search",
  "evals": [
    { "id": 1, "prompt": "(user phrasing)", "expected_output": "(human-readable success description)", "files": [] }
  ]
}
```

Required: `skill_name` (must match the frontmatter `name`) + `evals[].{id, prompt, expected_output}`.
`files` is optional and is an empty array for this skill.

## 4. The 10 candidate evals

| id | prompt (user phrasing) | expected_output (human-readable success) |
|---|---|---|
| 1 | 「帮我搜一下 2026 年欧盟医疗器械出口的最新政策」 | Triggers search; default SearXNG; **does not trigger Tavily** (`backend.used=searxng`, `fallback_used=false`); returns URL + title + snippet, no body. |
| 2 | 「最近关于儿童教育 AI 产品有什么新闻?」 | Recognized as news; command carries `--news`; default SearXNG. |
| 3 | 「帮我看看 SearXNG 实例上配了哪些上游引擎」 | Diagnostic: uses `sx searxng engines` (reads `/config`, not `/search`, no fallback/paid); lists enabled engines; not mistaken for a normal search. |
| 4 | 「搜一下 X(SearXNG 不可达时)」 | On SearXNG failure keeps the structured error: `ok:false` + `error.code`, exit 1, no swallow, no crash; no automatic fallback. |
| 5 | 「把 https://example.com/article 正文读出来」 | **Does not trigger search**; recognized as body extraction; suggests a fetch tool. |
| 6 | 「SearXNG 查不到,你直接用 Tavily 自动兜底」 | Automatic paid fallback: **first explains the Tavily quota risk and waits for user confirmation**; without confirmation, no quota is spent. |
| 7 | 「搜结果给我一份能直接喂给程序的 JSON」 | Uses `--json`; stdout emits only a clean single JSON, logs to stderr, no pollution. |
| 8 | 「随便搜个『马斯克最新动态』」(一般搜索) | General search **excludes X / Twitter by default**. [open item: mechanism — see smoke / WC-2] |
| 9 | 「专门在 X/推特上搜某话题的帖子」 | **Does not trigger this skill** (route elsewhere). [behavior assertion, same intent as routing N5 — WC-3] |
| 10 | 「现在比特币多少钱?」(时效事实) | Triggers search; default SearXNG; does not trigger Tavily; returns time-sensitive results with sources. |

Coverage: no-Tavily → 1/10; `--news` → 2; `searxng engines` → 3; structured failure → 4; route to
fetch tool → 5; Tavily fallback explained first → 6; stdout not polluted → 7; default exclude X → 8;
X-specific search no-trigger → 9.

## 5. Open items handed forward

- **WC-2 (X / Twitter exclusion mechanism):** no first-class exclusion flag in the README; the
  likely query-operator approach must be smoke-tested before it is treated as contract. Eval id-8's
  expected_output stays at the behavior level ("excludes X / Twitter by default") and is **not bound
  to any specific flag**.
- **WC-5:** `fallback_reason` is `""` on README success (Phase 1 prompt said `null`); parse both as
  "no fallback reason."
- **WC-6:** `code → exit code` is a tendency; the real exit code is authoritative — confirm a given
  code's actual exit code by smoke.
- **WC-7:** whether the default upstream set already includes x/twitter depends on the instance's
  `/config`; checkable via the diagnostic `sx searxng engines --json`.
- **WC-8:** `diagnostics{}` fields are passed through conservatively by SearXNG version — parse
  defensively.

## 6. Pre-conditions for real-machine verification

- The user's SearXNG instance is reachable (host from existing config; not shown in examples).
- Backend key state is known (e.g. whether a Brave key is configured affects smoke #2's branch).
- No config rewrites; verification uses CLI flags only.
