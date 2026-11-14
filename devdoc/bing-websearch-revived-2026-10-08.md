<!-- doc-class: historical -->
# Bing web search revival — the gate judged a page it never saw handled (2026-10-08)

**Task**: `01a1185ea4e9746c9b752c33280089cc` · branch `task/01a1185ea4e9746c9b752c33280089cc`

## Symptom

```
$ ./bin/gopi web search "golang" --method bing --count 3
{"status": "empty", "method": "bing", "elapsed_ms": 3568, "results": [], "count": 0,
 "message": "all bing sources failed or returned decoy (possible bot soft-block); treating as empty"}
```

Every source in the chain — local `www.bing.com`, local `cn.bing.com`, the
search-bot curl fallback — ended on what the script called a decoy, so the epic's
premise ("all three end on a decoy/challenge page") was literally true *as the
script reported it*. It was not true of the pages themselves.

## Diagnosis (raw evidence, source by source)

Probe: fetch each source directly with the script's own fetcher and parse it with
the script's own parser, but *without* the relevance gate.

| source | HTTP | `b_algo` blocks | first result | gate says |
|---|---|---|---|---|
| local `www.bing.com` (`setlang=en&mkt=en-US&cc=US`) | 200, 98 867 B | 10 | `The Go Programming Language` → `https://golang.google.cn/` | **junk** (false positive) |
| local `cn.bing.com` (same params) | 200, 100 747 B | 10 | same real SERP | **junk** (false positive) |
| search-bot `curl … www.bing.com/search` | 200, 130 844 B | 10 | same real SERP | **junk** (false positive) |

Both local endpoints return a **real, relevant SERP** for `golang`; two of the
three sources were healthy. `junk_serp()` was declaring them decoys.

Root cause, and it is a two-line story:

1. **The gate judged a sample the caller never asked for and the code never
   reported, cut to the caller's `--count`.** `main()` parsed only `count`
   results (`parse_results(raw, count)`) and handed *those* to `junk_serp()`.
   For `golang` the literal token `golang` does not occur in the title or
   snippet of results #0–#2 (they are go.dev pages: "The Go Programming
   Language", "Download and install", "Go 语言教程") — it first appears at #4
   ("GitHub - golang/go …") and in the URL of #0 (`golang.google.cn`).
   `--count 3` therefore produced a 3-item sample with zero term hits, and the
   decoy gate fired on a genuine page. With the default `--count 5` it survived
   by luck (result #4 carries the token) — which is why the epic saw a hard
   failure while a casual `--count 5` looks fine, and why the failure is
   count-dependent rather than source-dependent.
2. **The relevance signal ignored the URL.** A result URL is relevance
   evidence (`golang.google.cn` for query `golang`); the gate looked only at
   title+snippet.

The decoy it was built to catch is real and was re-captured live: for
`shunyu yao arxiv`, `www.bing.com` returns a structurally normal SERP (10
`b_algo` blocks, the query echoed in `#sb_form_q`'s value, `<title>shunyu yao
arxiv - Search</title>`) whose results are unrelated and **change on every
request** (three consecutive fetches: 高德地图/百度经验, then DeepSeek/知乎/掘金,
then an AO3 mirror cluster). That is bing's bot soft-block, and the gate catching
it is correct behaviour, not the defect.

### What it is NOT

The pages carry no challenge at all: `There are no results` = 0, `captcha` = 0,
`sorry` = 0, `unusual traffic` = 0, `b_no` = 1 (the "no results" template holder,
present on every page including the real one), and `X-Cache: CONFIG_NOCACHE` with
a normal `b_results` list. There is nothing to defeat — which is what kills
option (b) below.

## Options evaluated

**(a) Reuse the search-bot CDP gateway (`google-cdp.service` :9232 + headed
Chrome :9222) to fetch bing. — REJECTED as the fix, measured.**
The stack is up (`systemctl is-active google-cdp.service` → `active`; listeners
`127.0.0.1:9232` python3 and `127.0.0.1:9222` chrome; `/health` →
`{"status":"ok","challenged":false}`) and it does answer for bing: a transient
probe that opened **one new tab** on the already-running Chrome and read it over
one CDP websocket (no second Chrome, no gateway change, no new daemon — the
Google tab `www.google.com/webhp?udm=14` was left untouched) returned for
`golang` the genuine SERP (`title: "golang - Search"`, `b_algo` 10, "The Go
Programming Language"). But **it returns the same decoy for the decoy queries**:
`秦朝为什么二世而亡` → eWealthManager/AssetMark account pages; `shunyu yao arxiv` →
`Bing`/`webhp` self-links and Microsoft account pages. The decoy tracks the
**query shape and the egress IP**, not the fetch stack, so the datacenter route
does not cure it while the local route does not need curing. Cost: a CDP
navigate is ~3.5 s + polling (the Google path budgets 40 s) against ~0.5 s for
the direct fetch, and CDP allows one debug connection per target, so every bing
query would contend with the google arm. Not adopted.

**(b) Harden the curl fingerprint (headers/cookies/order, maybe a headless
browser). — REJECTED, evidence-backed.** There is no fingerprint challenge to
defeat: the plain `User-Agent`-only request (the script's own headers, nothing
added) already got the complete real SERP from both local hosts, with
`b_algo` = 10 and the query correctly indexed. A control run adding a full
browser header set (`Accept`, `Accept-Language`, `sec-ch-ua*`, `Sec-Fetch-*`,
`Upgrade-Insecure-Requests`) plus a cookie jar warmed on `https://www.bing.com/`
changed **nothing** — same 10 results, byte-for-byte the same first three URLs.
Spending complexity on a fingerprint that is not the blocker would be exactly
the "tune params without a root cause" anti-pattern.

**(c) Declare bing unavailable and drop it from merged. — REJECTED: it is
false.** Bing returns real results right now from the default route (four
queries verified end to end through the CLI, see the report's `## Verification`),
and `merged` now reports `bing: ok 3` instead of `bing: empty`. Removing a
working source because its *own consistency check* was miscalibrated would have
converted a bug in one gate into the permanent loss of a search method.

**Chosen instead — fix the fetch/verdict, not the report.** The defect was in
the sample the gate was given and the evidence the gate used:
`parse_results()` now returns what the caller asked for and the gate judges
exactly that same list (what you see is what was judged — a wider judged window
would re-open the 716 audit's hole, where decoy items rode through as `ok` while
only the tail was checked), and the URL joined title+snippet as a relevance
field. `count` now also reaches the search-bot curl fallback instead of being
hard-coded to 20 there.

## Files

- `plugins/websearch/scripts/bing_search.py` — gate sample == reported sample;
  URL added to the relevance haystack; `fetch_en(query, count)` /
  `REMOTE_SCRIPT` honour `count`; `junk_serp` docstring states both rules.
- `plugins/websearch/tests/test_bing_scenario.py` (new) — scenario suite over
  **captured real pages** through the real parser + gate + `finalize`.
- `plugins/websearch/tests/testdata/bing_real_golang.html`,
  `…/bing_decoy_serp.html` (new) — the two captured `#b_results > li.b_algo`
  fixtures (8.2 KB / 13.3 KB, trimmed so no CSS/JS bulk is committed).
- `testsuite/bing_scenario_test.go` (new) — runs that suite from the Go tree,
  pinned to the **repo** copy via `BING_SCRIPT`, so a stale deployed script in
  `$HOME` can never be what the gate measures.
- `~/.gopi/plugins/websearch/scripts/bing_search.py` — deployed copy synced
  (Rule: repo is the source of truth; `TestWebFetchContract_DeployedScriptSync`
  guards the drift).

## Why the guard is scenario-level

`plugins/websearch/tests/test_search_quality.py` feeds `junk_serp()`
hand-built dicts, so it cannot see a defect in the part that turns a real page
into those dicts — which is precisely where this regression lived (the unit
tests were green the whole time the CLI was dead). The new suite drives captured
page bytes through `parse_results` → `junk_serp` → `finalize` and asserts the
verdict, and it is red on both old behaviours:

- restore the title+snippet-only haystack (`narrow/` red-proof) →
  `test_real_serp_ok_at_count3_dot_regression` FAILS with
  `'empty' != 'ok' : real SERP reported as empty: … treating as empty`;
- disable the gate (`nogate/` red-proof) → 4 tests FAIL, first
  `test_verdict_is_empty_not_ok: 'ok' != 'empty'` (the decoy rides through).

Both red-proof copies are transient and live outside the repo, under this task's
session directory (`~/.gopi/sessions/bing-evidence/redproof/`).

## Honest limits

- `shunyu yao arxiv` is still `empty` and that is **correct** — live bing serves
  a decoy SERP for it from every route (local www, local cn, search-bot curl,
  and the remotebox CDP tab). Whether that query is answerable at all needs a
  residential egress or a different search method; the report's `## Not Done`
  says so.
- The relevance gate is a heuristic: it can still clip a genuine SERP whose
  first `count` results happen to omit every query token (e.g. `--count 3` on a
  query whose token first appears at #4). That is the price of not leaking decoy
  items and it is now visible in `message` rather than silent.
