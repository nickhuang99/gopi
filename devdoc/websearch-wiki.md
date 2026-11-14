<!-- doc-class: historical -->
# Task Report: web_search wiki method (Wikipedia)

## Completed
- New plugin script `plugins/websearch/scripts/wiki_search.py` (mirrored to
  `~/.gopi/plugins/websearch/scripts/` at runtime): 0-token Wikipedia search
  via the MediaWiki API (`action=query&list=search`, `format=json`), emitting
  the unified `{status,method,elapsed_ms,results,count}` JSON contract with
  slim `{title,url,snippet}` results (snippet ≤200 chars, `...` truncation).
- Network strategy: try direct `en.wikipedia.org` first (works abroad, <1s);
  on any failure fall back to the search-bot VPS jump host
  (`ssh remotebox` + base64-wrapped remote curl — same pattern as
  github_search.py: `echo <b64script> | base64 -d | bash -s <b64url>` so
  `$1` is bound under `set -u`; plain `ssh host script arg` concatenates and
  leaves `$1` unbound — the bug I hit and fixed).
- Registered the `wiki` method end-to-end:
  - `internal/tools/websearch.go`: `webSearchScripts["wiki"]`, doc comments,
    unknown-method enum message.
  - `internal/agent/loop.go`: tool Description + InputSchema method enum.
  - `cmd/agent/cli_websearch.go`: `webSearchMethods` map, usage text (added
    `--method wiki` example + Flags enum), parse error enum message.
- Tests: `TestWebSearchWikiMethod` (fake script asserts plain query/--count
  routing, no method-specific flags) in `internal/tools/websearch_test.go`;
  CLI enum-hint assertion updated in `cmd/agent/cli_websearch_test.go`.
- Docs: runtime skill catalog `~/.gopi/plugins/websearch/skills/websearch.md`
  updated — method table row, routing rule (百科/概念/人物/事件 → wiki),
  usage example, cost note.

## Not Done
- `wiki` NOT added to the merged multi-method default (hardcoded
  bing+google+arxiv). Design decision: an encyclopedia source inside general
  web fusion would add noise to every default search; `method=wiki` is the
  explicit, reliable path for concept lookups. Revisit if a wiki-weighted
  fusion is wanted.
- Only English Wikipedia (en.wikipedia.org) — no per-language/zhwiki support
  yet (MediaWiki API makes it trivial to add later: swap the domain).
- Repo `plugins/websearch/scripts/` mirrors only arxiv/github/web_fetch today
  (bing/google/deepseek/merge_results are runtime-only); wiki was added to the
  repo mirror per the newer-scripts convention.

## Verification
- Build: `go build ./...` OK
- Tests: `go test ./internal/tools/ ./cmd/agent/` — tools OK (76s, includes
  slow network hermetic tests), agent OK (4.4s)
- Script live: `python3 wiki_search.py "retrieval augmented generation" --count 3`
  → status=ok, 3 results, source=remotebox; nonexistent query → status=empty
- E2E CLI: `gopi web_search "large language model" --method wiki --count 2`
  → status=ok, results `Large language model` / `List of large language
  models`, count=2, elapsed ~12.7s (GFW: 5s direct timeout + ssh hop)
- Commit: <hash filled at commit time>

## Notes
- Direct fetch in the GFW environment always times out (5s), so CN calls pay
  ~13s for the ssh hop — comparable to google; abroad it is <1s direct.
- The `source` field (direct|remotebox) is included in the payload for
  observability; it is extra to the documented contract and harmless to the
  LLM-facing JSON.
