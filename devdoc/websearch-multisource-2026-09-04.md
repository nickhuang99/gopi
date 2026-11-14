<!-- doc-class: historical -->
# web_search multi-source + author noise fix (search-quality epic follow-up, t-1788478571)

Date: 2026-09-04 · Branch: `feat/websearch-multisource` · Follow-up to commit eaf8544 / devdoc 61a8b0c.

## Symptom (verified 2026-09-04)

`gopi web search "shunyu yao paper"` returned:

```
inputs: [bing empty 0, google timeout 0, arxiv ok 5]
```

The merged method was arxiv-only in practice: bing always empty, google always
timeout. And the arxiv results were 2/5 noise (quantum-chaos + topological-
insulator papers — *not* Shunyu Yao the agent researcher).

## Root causes (read the source, not the docs)

### 1. bing "always empty" — wrong egress, not a broken parser

`bing_search.py` sent English queries **through the search-bot VPS**
(`ssh remotebox → curl www.bing.com`). Live test showed that VPS egress gets a
bing **bot soft-block decoy SERP** for "rare name + topic word" queries
(shunyu yao paper → cowboy cocktail shakers; shunyu yao research → HATE
antonyms; per-request random). The eaf8544 junk gate caught the decoys and
reported `status=empty` — correct gate behavior, but the **source was
poisoned**: the same queries from the **local residential egress** returned
REAL results (ReAct paper first, ~0.6s, stable across 3 runs) with
`setlang=en&mkt=en-US&cc=US` on www.bing.com.

Fix: English queries now try **local www.bing.com → cn.bing.com → search-bot
fallback**, each host with a 2-attempt retry (bing has per-request 200-empty
jitter), junk gate still applied per source. Real fix = local residential
egress is NOT bot-flagged; search-bot is. Google path doc (61a8b0c) said the
same about google.com needing residential egress — bing was just wired to the
wrong one.

### 2. google "always timeout" — the 10s doomed fallback

`google_search.py` CDP gateway (search-bot :9232) reports a cached
`reCAPTCHA /sorry challenge` for the VPS egress IP (verified live: health says
`challenged: true`). The old code then burned ~10s on the curl SERP/News-RSS
fallback **from the same blocked IP** (news.google.com also /sorry-walled),
ending in `status=timeout`.

Fix: when the CDP gateway reports a challenge, **fail fast**
(`status=unavailable`, ~2.3s instead of ~10.5s) with a message naming the
block + fix (residential IP / VPN route). The CDP path stays the correct
architecture and auto-activates if Google's risk posture changes or the
egress IP changes. Do NOT paper over google with fake results.

### 3. arxiv noise — homonyms, not bad fielding

`au:shunyu_yao` is a *name-only* match — arXiv has no author ID. Live probe
of the full author record (93 papers): the name "Shunyu Yao" is shared by
**~5 distinct people** — the Princeton agent researcher (~52 cs papers), a
cond-mat non-Hermitian physicist, a hep-th wormhole theorist, a particle
experimentalist, etc. eaf8544's au: rewrite correctly found the person's
papers; the noise was other people with the same name.

Fix: **co-authorship anchor-cluster disambiguation** (`disambiguate_homonyms`):
- fetch a wider `au:` window (HOMONYM_WINDOW=15) relevance-sorted;
- anchor = #1 hit (query paper-context biases relevance to the intended
  person — "shunyu yao paper" ranks ReAct first);
- keep only entries sharing ≥1 co-author with the anchor's growing set
  (transitive within the window); orphaned other-person papers drop.
Verified: "shunyu yao paper" → ReAct/WebShop/InterCode/KeepCALM/ToT (5/5 on
the agent cluster); yann_lecun (single person) unaffected; au:zhong_wang
ambiguous cond-mat name clusters to its top group.

## Tests (machine-checkable)

1. `gopi web search "shunyu yao paper"` → bing ok (source diversity) + arxiv
   ok; NO quantum/topological/wormhole noise in top results.
2. `gopi web search "hermes agent"` (non-arxiv blog/project) → real results
   (GitHub NousResearch/hermes-agent etc.), bing primary.
3. Regression: `gopi web search "find all paper links for download by authors
   including name shunyu yao"` → 5 clean Shunyu Yao agent papers.
4. `go test ./cmd/agent/ ./internal/tools/` green.
5. `python3 plugins/websearch/tests/test_search_quality.py` → 16 tests green
   (added: homonym disambiguation 3 cases, bing local-EN url + unescape).

Script sync discipline (from epic eaf8544): repo is source of truth; edit the
repo copy → `cp` to `~/.gopi/plugins/websearch/scripts/` →
testsuite DeployedScriptSync guard.

## Files changed

- `plugins/websearch/scripts/bing_search.py` — local-EN-first source chain +
  retry; plain-URL `&amp;` unescape (scholar links were HTML-entity polluted).
- `plugins/websearch/scripts/arxiv_search.py` — homonym disambiguation on the
  au: path (HOMONYM_WINDOW=15 anchor-cluster).
- `plugins/websearch/scripts/google_search.py` — fail-fast on CDP /sorry
  challenge (skip doomed same-IP fallback).
- `plugins/websearch/tests/test_search_quality.py` — +5 tests (16 total).
- `plugins/websearch/scripts/merge_results.py` + `deepseek_ask.py` — RECOVERED
  from the peerbox (2026-09-05 follow-up) — see "Post-merge gap" below.

## Remaining (documented, not papered over)

- google stays blocked until the egress IP changes / residential-VPN route
  exists (needs the RPi5/search-bot-VPN infra from the epic doc; CDP main
  path will auto-activate).
- bing decoy gate still applies per-source; a bot-flagged network would see
  `empty` again — but now with 3 candidate egresses the merged path survives.

## Post-merge gap found and fixed (2026-09-05, this follow-up)

After 52b31ab (this branch) merged to master, live verification of the merged
default exposed one more silent failure: **`merge_results.py` was still an
unversioned script** (audit B5: bing/deepseek/merge_results lived only in
`~/.gopi/plugins/websearch/scripts/`; only bing_search.py was later mirrored).
This laptop's fresh `~/.gopi` had no merge_results.py, so
`mergeWebSearchResults` fell into the "merge unavailable → best surviving
single method" fallback — **every merged search returned bing-only, with the
arxiv (and any google) results silently dropped** and no `inputs[]` in the
payload to show it. The tool was again effectively single-source, just from
the other side.

Fix (commit 9e33904): recovered the only live copies of `merge_results.py`
(277 lines: URL/domain+title dedup, relevance scoring, per-result
source/sources attribution, elapsed_ms from merge start) and
`deepseek_ask.py` (160 lines) from the peerbox's `~/.gopi` via scp, mirrored
them into `plugins/websearch/scripts/`, py_compile-verified, and re-deployed
to this machine's `~/.gopi`. DeployedScriptSync guard (testsuite) covers the
six versioned scripts; the two recovered ones should be added there (noted
below).

## Final live verification (2026-09-05, HP laptop, post-9e33904)

- `gopi web search "shunyu yao paper"` →
  `{"method":"merged","status":"ok","count":5,
    inputs:[bing ok 5, google unavailable 0, arxiv ok 5]}` — fused bing+arxiv
  results, **all attributable to Shunyu Yao** (ReAct, ToT, WebShop, his Google
  Scholar profile, shunyuyao.github.io); zero quantum-chaos/topological noise.
- Non-arxiv query `hermes agent` → 5 real web results
  (hermes-agent.nousresearch.com, github.com/NousResearch/hermes-agent, …).
- Regression, the original epic query ("find all paper links … name shunyu
  yao") → 5/5 clean agent papers (Keep CALM, ReAct, WebShop, InterCode, ToT).
- `--method google` → honest fail-fast: `unavailable` in ~2.6s naming the
  reCAPTCHA /sorry challenge + the residential-IP fix (not a 10s timeout).
- `python3 plugins/websearch/tests/test_search_quality.py` → 16/16 green.

Residual: google stays `unavailable` until a residential egress exists on the
remotebox path (CDP architecture intact, auto-activates on IP change).
