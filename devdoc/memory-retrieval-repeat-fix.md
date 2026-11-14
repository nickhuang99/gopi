<!-- doc-class: historical -->
# Task Report: memory_search repeat-run retrieval failure + CLI help

## Problem

Two runs of the SAME VCMI task were issued (sessions `82f5bc4c-…` at
2026-08-24 20:49 and `1cbb8fef-…` at 2026-08-25 01:15). The first run stored
six distilled lessons into `~/.gopi/memory.db` (ids 200–204), yet the second
run's opening `memory_search` returned the "No distilled memory found" hint —
the repeat-run experience-retrieval loop failed. Separately, the
`memory_search` / `memory_store` / `web_search` CLI subcommands had no help in
`gopi -h`, so users couldn't discover them.

## Investigation (fully, from the two session logs)

1. Pulled both sessions via `gopi-log <id> --memory --text`.
2. Confirmed the store side worked: session 82f5bc4c stored ids 200–204
   (`Memory stored (id=…)`), and the DB (`~/.gopi/memory.db`) holds all of
   200–215.
3. Reproduced the failure with the exact failing query against the live DB:
   `memory_search "vcmi LLM AI roadmap tool calls game agent study"` → nothing.
4. Isolated the term: `study` appears in **0 rows** of the DB. Isolated the
   mechanism: FTS5 unicode61 tokenizer runs `study` as one token, and
   `escapeQuery` (internal/tools/memory.go) wraps **every ASCII token in
   double quotes and joins them with AND** into a single MATCH expression.
   FTS5 AND requires all terms present **in one row** — one out-of-vocabulary
   word zeroes the entire match (`"vcmi" "LLM" … "study"` → 0 rows, whereas
   dropping `"study"` → 3 rows).

**Root cause:** the exact-token FTS5 route is the ONLY recall path for pure
ASCII queries. The trigram / 2-char-CJK LIKE fallback routes only fire for
CJK content. So an LLM-generated n-gram query carrying one word never present
in the store returns nothing — a noisy-but-relevant query is fatal to recall.
This is why the second run "always returns nothing": the query is noisy, not
empty.

## Fix (1) — ASCII partial-recall OR fallback

In `MemorySearch`, when the exact-token (AND) route returns **zero hits** AND
the query has ≥ 2 distinct ASCII tokens of ≥ `asciiTokenMinLen`(2) runes, an
OR-route re-runs the FTS5 match with all tokens joined by `OR` (each still
quoted → injection-safe). ANY one shared token recalls a candidate, capped at
`trigramRecallK`. This mirrors the CJK trigram/LIKE partial-route philosophy.

- Strong queries (all tokens present) keep full AND precision — OR-route only
  fires on a zero-hit exact match.
- Noisy queries are rescued from returning nothing.
- `escapeQueryOR` helper: neutralizes quotes, drops CJK tokens (the trigram /
  LIKE routes own them) and sub-`asciiTokenMinLen` tokens, de-dupes, requires
  ≥ 2 tokens before downgrading (a single-token OR is identical to AND, so no
  point).

Verification against the real DB: the previously-failing query
`"vcmi LLM AI roadmap tool calls game agent study"` now recalls `#203` etc.

## Fix (2) — CLI help discoverability

- `gopi -h` now has a **Subcommands** section listing `memory_search`,
  `memory_store`, `web_search` and pointing at each per-subcommand `-h`.
- Guard test `TestTopLevelUsageListsSubcommands` asserts the usage advertises
  all three; it is meant to catch a help regression.

## Files changed

- `internal/tools/memory.go` — `escapeQueryOR` helper + Route 1.5 OR-fallback
  in `MemorySearch`; new const `asciiTokenMinLen`.
- `internal/tools/memory_test.go` — `TestMemorySearchASCIIPartialRecall`
  (reproduces the real failing query), `TestEscapeQueryOR`; re-cast
  `TestMemorySearchOperatorQueryEscaped` to assert the improved behavior
  (operators still neutralized, partial recall no longer returns nothing).
- `cmd/agent/main.go` — `Subcommands` section in top-level `usage`.
- `cmd/agent/usage_test.go` — new: `TestTopLevelUsageListsSubcommands`.

## Verification

- Build: `go build ./...` — OK (in the isolated worktree).
- Tests: `go test ./...` — all packages PASS (cmd/agent, internal/agent,
  internal/deepseek, internal/tools).
- Manual: `GOPI_MEMORY_DB=~/.gopi/memory.db gopi memory_search
  "vcmi LLM AI roadmap tool calls game agent study"` now returns VCMI records
  (was "No distilled memory found"). `gopi -h` lists the subcommands;
  `gopi memory_search -h` still shows the memory usage.
- Commits on `feat/memory-retrieval-fix`:
  - `fab492e` fix(memory): ASCII partial-recall OR-fallback…
  - `397b440` docs(cli): advertise memory_search/memory_store/web_search…

## Not Done / Notes

- Testing via the CLI was exercised manually against a throwaway build; a
  persisted automated end-to-end CLI test is not added (the tool-level tests
  cover the engine, and `cli_memory_test.go` covers the CLI dispatch path).
- No export/import tooling added — out of scope.
- `GOPI.md` is the coding-rules file GoPi reads at boot; user-facing command
  help lives in `gopi -h` / per-subcommand `-h`, so no change was made there.
