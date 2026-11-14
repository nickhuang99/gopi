<!-- doc-class: historical -->
# Task Report: `gopi stats` + `gopi raw` + log `--http/--raw` moved out (claude implementation)

Branch: `feat/log-stats-raw-claude` (worktree `~/workspace/gopi-logstats-claude`)
Spec: `/home/user/work/gopi-log-redesign/IMPLEMENT-SPEC.md` + `FINAL-USAGE-PROPOSAL.md`

## Completed

### 1. `gopi stats` — numbers, not content
- `cmd/agent/cli_stats.go`: `stats <session-id>` / `stats --last [N]` / `stats --all`,
  view flags `--rounds` (per-round table), `--tools` (call ranking), `--json`
  (machine-readable, includes the per-round rows). `--rounds/--tools` compose
  with `--last`/`--all`.
- Source selection (`buildStats`): the paired raw HTTP log is PRIMARY
  (`ResolveRawLogForSession`), the session JSONL request events are the
  fallback. Every output names its source — `source: raw log <file>` or
  `source: session jsonl <file> (no raw log)`.
- Aggregate box: model / source / rounds / prompt tokens + avg / per-round
  min-max / cache hit% + miss / completion / tools calls + kinds / trim cliffs
  (largest + round) / wall time / raw log size.

### 2. `gopi raw` — bytes, not content
- `cmd/agent/cli_raw.go`: `<session-id> [<round>] [request|response]`,
  `--rounds` (round# → line span → bytes index), `--lines A-B` (also `A` and
  `A-` to EOF), `--sysprompt`, `--file PATH` (EXCLUSIVE with session-id; composes
  with `<round>`/`--lines`), `--last [N]`.
- stdout is pure bytes: no banner, no header. Diagnostics (truncation note, the
  `--last N` session separator) go to stderr only — verified with a 77 MB dump.
- `--json` / `--text` are refused BY NAME ("raw prints verbatim bytes"), never
  silently ignored.
- 64 MiB cap with a rune-safe cut (`capRuneSafe`) — no mid-CJK slicing.
- Bare `gopi raw <id>` is byte-identical to the raw log file (cmp-verified).

### 3. `gopi log` — raw-log views removed
- `--http`, `--http N`, `--http N --request|--response`, `--raw` now print ONE
  stderr line naming the exact equivalent and exit **2**
  (`logMovedPointer` — full migration table in cli_log_test.go
  `TestLogMovedPointerTable`). Not silently reinterpreted, not aliased.
- Every content flag is untouched: the byte-hash of `log <id>` output for
  `--response --thinking --chat --todo --memory --search --job --json --request`
  is identical to a binary built from master.
- `logUsage` documents the move (and `TestLogUsageNamesTheNewCommands` guards it).

### 4. Golden tests on REAL logs (nick's hard requirement)
- `tests/fixtures/log-stats-raw/`: a real session (`fc426fed-e3df-4e30-…`,
  9 rounds) + its paired raw HTTP log, cut exactly on the round-5 boundary so
  round numbering and line numbers stay the real ones. Provenance + sha256 in
  the fixture README.
- `internal/gopilog/stats_golden_test.go`: snapshots for stats
  overview/`--rounds`/`--tools`/`--json` in BOTH sources (raw and jsonl
  fallback) plus the `raw --rounds` index. `GOPI_GOLDEN_UPDATE=1` re-records; a
  drift names the first differing line.
- The verbatim views are asserted against the fixture file ITSELF (not a
  snapshot): a round's request body and its response span must be byte-identical
  to the logged bytes.
- `cmd/agent` re-asserts the same snapshots through `runStatsSession`, proving
  session lookup + raw pairing + rendering add no decoration.

## Design decisions (and the evidence behind them)

**Raw usage → stats mapping is pinned, not guessed.** In the Anthropic-style
stream, `message_start.usage.input_tokens` is the UNCACHED part and
`cache_read_input_tokens` the cached one, so

	prompt = input_tokens + cache_read + cache_creation
	hit    = cache_read          miss = input_tokens (uncached)

`internal/gopilog/stats_test.go::TestBuildStatsFromRoundsMatchesJSONL` asserts
this equals the JSONL `prompt_tokens/cache_hit/cache_miss` semantics, and on the
real fixture both sources produce the identical 90,524 prompt / 1,268
completion / 8 tool calls.

**Trim cliff = two floors.** `Δreq drop > 4,000 tokens AND prompt < 60% of the
previous round` (`statsCliffMinDropTokens`, `statsCliffMaxRatioPct`). The pair
was calibrated on the 463-round reference session `7168c24d-1bad-…` and
reproduces FINAL-USAGE-PROPOSAL's `--rounds` excerpt exactly — Δreq -43.2K /
-74.4K / -92.2K with prompts 14.2K / 13.6K / 14.0K. One floor alone is wrong:
a -10K drop at ratio 0.8 (session tail) is not a cliff, and a 60% drop of a
3K round is noise. A targeted search of the (drop, ratio) plane found **no**
combination that yields the mock's "5 cliffs" — the mock's own excerpt shows 3,
so the count "5" in the proposal was inconsistent with itself.

**Round numbering is never cross-indexed, and the mock had it shifted by one.**
The proposal's table labels the cliffs 39/171/450; those are JSONL *iterations*
(0-based). In raw ordinals they are 40/172/451 — which is what my `--rounds`
prints, because `gopi raw <id> N` resolves raw ordinals. Both tables state their
basis (`raw round ordinal, 1-based` / `jsonl iteration, 0-based`), and the
cross-source agreement of the *numbers* (only the labels differ) is the proof.

**Guard probes are rows but never aggregates.** A probe round keeps its table
row (so numbering stays gap-free and `gopi raw <id> N` still resolves) with a
`p` marker, but stays out of the Δreq chain and the token sums — its small body
would otherwise read as a fake trim cliff (this is exactly the fake cliff the
463-round session would have produced 3 of). Reference session: `rounds 463
(3 probe)`.

**`--all` sums the JSONL, not the raw corpus.** Aggregating raw logs means
reading ~23 GB on this machine (measured: 116 MB in 4 s ≈ 13 minutes for the
whole corpus). The JSONL request events carry the same usage numbers, so `--all`
takes the JSONL fast path, says so in its source line
(`984 sessions (jsonl totals)`) and in `statsUsage`. Cost: 984 sessions in 11 s.
`--last N` and single-session views stay raw-primary.

**Wall time always comes from the JSONL.** The raw log has no timestamps, so
`SessionWallSeconds` reads `session_end.elapsed_sec`; when absent the box says
`n/a (no session_end)` rather than inventing a number.

**Overview box uses the repo's formatter style.** The proposal's mock draws a
fixed-width ┌─┐ box; I kept the box glyphs but let the border follow the widest
row (`statsBox`), so a long value can never spill. Keys are padded to a named
constant (`statsKeyWidth`).

## Not Done / deferred (honest list)
- Per-round **wall time** and per-round **Δbytes** are absent — the raw log has
  no timestamps, so only the session-level wall time is real.
- Request/response **body-level** dimensions (per-message sizes, tool-argument
  stats) are not in `--json`; the spec lists them as a later bits-by-bits
  extension.
- `stats` has no `--tools --json` split — `--json` always carries
  `tool_kinds`, so a script gets everything in one document.
- No `gopi raw --grep`/`--filter`: that is content selection, not bytes; it
  belongs to `gopi log`.
- `internal/gopilog/rawlog.go` and 3 pre-existing cmd/agent test files fail
  `gofmt -l` — untouched by me, left alone to keep the diff surgical.

## Verification
- Build: `go build ./...` OK (also `go vet ./cmd/agent/ ./internal/gopilog/` clean).
- Tests: `go test ./... -count=1` — **all packages ok**, incl.
  `cmd/agent` (103 s), `internal/gopilog` (1.5 s), `internal/tools` (348 s).
  New tests: 12 in `internal/gopilog` (stats + golden), 17 in `cmd/agent`
  (stats/raw parsing, CLI-over-fixture, log migration table).
- Regression: pre-existing `cli_log_test.go` passes unmodified (26 tests) —
  content views are byte-identical.
- Real-session runs:
  - `stats 7168c24d-…` → rounds 463 (3 probe), prompt 32,728,702, min 13,170 /
    max 106,139, completion 210,239, tools 504 calls / 14 kinds, 3 cliffs
    (largest -92.2K @ round 451), wall 44m41s, 116.1 MB raw log, 4.1 s.
  - `stats --all` → 984 sessions, 67,936 rounds, 5.29 B prompt tokens, 98.56%
    cache hit, 75,672 tool calls / 25 kinds, 11.4 s.
  - `raw <id> | cmp - <rawlog>` → byte-identical; `raw <id> 3 request` →
    byte-identical to the file slice (46,414 B incl. newline).
  - `raw --last 2` → 77,199,722 B on stdout, 2 lines on stderr.
- Commit: 5 commits on `feat/log-stats-raw-claude`
  (`8b1442e` stats core, `7b8e19f` CLI, `369d27b` goldens, `1d9e726` raw
  whole-log fix, + this doc).

## Incident note (worktree was deleted mid-task)
At 13:08 the worktree directory `~/workspace/gopi-logstats-claude` and its
branch were removed from outside this session (only the two files I had just
written survived as orphans; the branch `feat/log-stats-raw-claude` no longer
existed in `git worktree list`). Recovered by re-running
`git worktree add -b feat/log-stats-raw-claude ~/workspace/gopi-logstats-claude master`
and restoring the two files. Nothing was lost, but anything reading worktrees
(PA sweep, another agent) should not delete a worktree whose branch is mid-task
— GOPI.md 6d's declaration-first rule exists for this.
