<!-- doc-class: normative -->
# Websearch Plugin: arxiv_search.py (W3)

**Deliverable**: `~/.gopi/plugins/websearch/scripts/arxiv_search.py` (live path,
also versioned in this repo at `plugins/websearch/scripts/arxiv_search.py`)
**Design ref**: `~/work/hermes-agent/devdoc/gopi-websearch-plugin-design.md`
**API ref**: `~/work/hermes-agent/skills/research/arxiv/scripts/search_arxiv.py`
(arxiv export API, stdlib-only — no pip deps, 0-token plugin layer)

## What it does

`arxiv_search.py <query> [--count N]` queries the arXiv export API
(`https://export.arxiv.org/api/query`, directly reachable from China, no VPN)
and returns **one slim JSON object** on stdout. All filtering/truncation is
done inside the script so results entering agent context cost 0 extra tokens.

```json
{"status": "ok|empty|timeout|unavailable", "method": "arxiv", "elapsed_ms": N,
 "results": [{"title": "...", "url": "...", "abstract_snippet": "...",
              "published": "YYYY-MM-DD"}], "count": N}
```

- `abstract_snippet`: summary truncated to 300 chars with `...` when cut.
- `url`: canonical abstract page, version suffix stripped (`2506.06962v3` -> `/abs/2506.06962`).
- `published`: first 10 chars of the Atom `published` date.
- `--count` clamped to `[1, 20]` (default 5) — keeps payload slim.

## status semantics (per design doc)

| status | when |
|---|---|
| `ok` | >= 1 result |
| `empty` | query succeeded, zero results |
| `timeout` | API not answered within 15s (`socket.timeout`/`TimeoutError`) |
| `unavailable` | HTTP error, DNS failure, unparseable XML, any other exception |

The script **always** emits valid JSON (single line, `ensure_ascii=False`);
status lets the agent decide "switch method" vs "results are enough".

## Design decisions

- **stdlib only** (`urllib` + `xml.etree.ElementTree`): the RPi5 `arxiv_fetch.py`
  uses the `arxiv` pip lib, but the plugin layer must run anywhere with
  python3 — no `pip install` on the VPS/RPi. Same API, no dependency.
- **Named constants** at top of file: `DEFAULT_COUNT`, `MAX_COUNT`,
  `SNIPPET_CHARS`, `API_TIMEOUT_SEC`, `API_BASE_URL`, `USER_AGENT`.
- **Relevance sort** (`sortBy=relevance&sortOrder=descending`), matching the
  reference script — best default for paper discovery.
- Timeout is caught explicitly before generic `URLError` (py3 wraps
  `socket.timeout` in `URLError.reason` in some paths) so `timeout` is not
  misreported as `unavailable`.

## Verification

- `python3 arxiv_search.py "retrieval augmented generation"` -> `status=ok`,
  5 real papers (AR-RAG, etc.), valid JSON.
- Empty -> quoted phrase that cannot match any token
  (`arxiv_search.py '"zzzqqqxxyyww nonexistent 987654321"'`) -> `status=empty`,
  `count=0`. **Note**: arxiv `all:` search OR-matches tokens, so bare
  gibberish queries DO return hits (e.g. "nonexistence" papers) — a quoted
  phrase is the reliable live trigger for `empty`.
- Simulated `socket.timeout` (monkeypatched fetch) -> `status=timeout`.
- Simulated HTTP 503 -> `status=unavailable`.
- `--count 99` -> clamped to 20 results; `--count 3` -> 3 results.
- URL version suffix stripped in all results.
- Live install verified at `~/.gopi/plugins/websearch/scripts/arxiv_search.py`
  (deployed 2026-08-21, `chmod +x`): `status=ok`, 2 results, valid JSON.
- **Re-deployed 2026-08-21**: the live file at `~/.gopi/plugins/websearch/scripts/`
  was found missing (only the repo-versioned copy existed). Re-installed from
  `plugins/websearch/scripts/arxiv_search.py` (`chmod +x`) and re-verified from
  the live path: `status=ok`, 2 real papers, valid JSON.

Not done (out of scope for W3): `--sort` option, paging (`--start`).

## Author-query support (2026-09 search-quality epic)

**Problem**: `gopi web search "find all paper links for download by authors
including name shunyu yao"` returned nothing useful — the script sent every
query as `all:` full-text search, where a rare author name OR-matches noise
and the person's own papers never surface.

**Fix (in-script, 0-token)**:
1. **Fielded passthrough** — a query already starting with an arXiv field
   prefix (`au:`/`ti:`/`abs:`/`all:`/`cat:`/`co:`/...) is sent VERBATIM, not
   wrapped in `all:`. `au:"Shunyu Yao"` and the `author:` alias now work.
2. **Author-intent detection** — plain queries naming an author next to a
   paper-context word ("shunyu yao papers", "papers by shunyu yao", verbose
   NL "find all paper links ... name shunyu yao") are rewritten to
   `au:shunyu_yao` (arXiv's first_last author index). Detection is
   conservative (stopword-filtered token walk past name|by|of|from|
   including); topic queries ("retrieval augmented generation", "ReAct
   paper") are left on `all:`.
3. **Self-correction** — if the au: rewrite returns zero hits (a title-like
   false positive, e.g. "tree of thoughts paper"), the script falls back to
   the original query's `all:` search and drops the `query_rewritten` field.
4. Payload gains `query_rewritten` (the original query) when a rewrite fires,
   so the agent knows the search was author-fielded.

**Verified live** (2026-09-04):
- `arxiv_search.py "shunyu yao papers"` → ReAct/WebShop/InterCode (Shunyu
  Yao's actual papers, `query_rewritten` set).
- `arxiv_search.py 'au:"Shunyu Yao"'` → same, verbatim fielded.
- Topic queries unchanged (ReAct paper, tree of thoughts, RAG all fine).
- "tree of thoughts paper" → au: empty → all: fallback (no false author).
