<!-- doc-class: historical -->
# Task Report: Delete the orphaned per-round digest view API (internal/gopilog)

Branch: `chore/gopilog-httpview-removal` (forked from `fix/gopilog-httpview-orphan`, tip a93d18e)
Worktree: `~/workspace/gopi-httpview-removal`

## Background

`gopi log <id> --http …` lost its view in commit e23d506 ("gopi stats + gopi raw;
move log --http/--raw out"): the log surface now prints only a pointer line
(`cmd/agent/cli_log.go`, `logMovedPointer`). The migrated commands render their
own output — `gopilog.FormatStatsRounds` (`gopi stats <id> --rounds`) and
`gopilog.RawRoundIndex` / `RawRoundRequest` / `RawRoundResponseSpan` (`gopi raw
<id> [round] [request|response]`). That left the whole per-round digest view
reachable from tests and nothing else. It was marked Deprecated with an explicit
"follow-up task" note on the predecessor branch; this task is that follow-up.

Base-branch choice: `master` lacks both the deprecation notes and the
`cmd/agent` wiring, so the removal builds on `fix/gopilog-httpview-orphan` and
keeps `ParseHTTPSelector`'s `cmd/agent` call site alive.

## Completed

### Removed (deleted outright — no successor by design for the message digest)

Exported:
- `HTTPBodyOptions` (verbatim-body view tuner)
- `FormatHTTPRounds` (the `--http` list/detail entry point)
- `FormatHTTPRoundRequestBody` (verbatim request body)
- `FormatHTTPRoundResponseBody` (verbatim response span)
- `HTTPDetailIndex` (round → slice index resolver)

Unexported chain they were the only roots of:
- `formatHTTPRoundList`, `formatHTTPRoundDetail`
- `writeRequestDetail`, `writeResponseDetail`
- `describeHTTPMessage`, `shortToolID`
- `httpRequestShape`, `httpRoundTools`

Their display-cap constants (only those formatters used them):
`httpMsgPreviewMax`, `httpMsgListMax`, `httpFieldsMax`, `httpChunkTailMax`,
`httpBodyLineMax`.

`internal/gopilog/httpview_test.go` deleted in full — every test in it
(`TestFormatHTTPRoundList`, `TestFormatHTTPRoundDetail`,
`TestFormatHTTPRoundLastDefault`, `TestFormatHTTPRoundRequestBody`,
`TestFormatHTTPRoundResponseBody`, `TestFormatHTTPRoundResponseBodyMaxLines`,
`TestHTTPDetailIndex`) covered the digest view, and its two helpers
(`rawLogFixture`, `roundVerbatimFixture`) had no other consumer.

### Survivors (kept in `httpview.go`, production-wired)

- `HTTPRoundSelector`, `ParseHTTPSelector`, `HTTPSelectorAll`,
  `HTTPSelectorLast` — the `all|last|N` round grammar, production call site
  `cmd/agent/cli_log.go` (`roundSelectionFor`/`roundArg`), shared with
  `gopi raw <round>`. Kept verbatim.
- `humanBytes` — consumed by `rawview.go`, `stats.go` (4 call sites).
- `orDash` — consumed by `stats.go` (4 call sites).
- `readRawLogLineSpan` — consumed by `rawview.go`.

`headerSep`, `truncateRunes`, `oneLine`, `shortID` are defined in
`internal/gopilog/formatter.go` (not `httpview.go`) and were left untouched.
`newRoundScanner` is in `rounds.go`. Each survivor's consumer was grep-verified
in production (`--include=*.go`, excluding `_test.go`) before keeping it.

The file's leading doc block was rewritten to record WHY the file now holds only
the grammar + shared helpers, so a future reader does not "restore" the digest
view.

### ParseHTTPSelector coverage preserved

`internal/gopilog/httpview_selector_grammar_test.go`
(`TestHTTPSelectorKeywordConstantsAreTheAcceptedSpellings`) is kept — it pins the
keyword spellings to `HTTPSelectorAll`/`HTTPSelectorLast` and asserts near-miss
spellings error. `cmd/agent/cli_log_roundgrammar_test.go` keeps the parser ↔ cmd
matrix cross-check. Master's `internal/gopilog/cov_gopilog_hp_test.go` (absent on
the base branch) was checked for references to removed symbols — it references
`ParseHTTPSelector` only, so the eventual merge into master stays green.

## Not Done

- Nothing deferred. The verbatim-body view's `--json`/`--raw` successor surface
  was NOT re-created (explicitly out of scope per the deprecation note: "do not
  add one to revive them"); `gopi raw` already prints bytes.
- `fix/gopilog-httpview-orphan` is left unmerged; merging it is the PA's call,
  not this task's.

## Verification

- `gofmt -l internal/gopilog/ cmd/agent/` → empty (clean)
- `go vet ./internal/gopilog/ ./cmd/agent/` → OK (no output)
- `CGO_ENABLED=0 go build ./...` → OK
- `go test -count=1 ./internal/gopilog/` → `ok github.com/nick/gopi/internal/gopilog 0.959s`
- `go test -count=1 ./cmd/agent/` → **the full-package run is load-flaky on this
  host, not broken by this change.** Evidence chain (all runs with an isolated
  `HOME` + reused `GOMODCACHE`/`GOCACHE`, so no module re-download and no
  contention on the shared `~/.gopi/memory.db`):
  - The hang always lands in `cmd/agent.TestAgentBranchScan_*` →
    `internal/tools.taskUpdate` → `database/sql.(*DB).Close` → `syscall`
    (SQLite WAL checkpoint on close). `git diff HEAD master --
    cmd/agent/cli_tasks_branchscan_test.go internal/tools/tasks.go` is
    **EMPTY**: those files are byte-identical between this commit and master,
    so that path carries no delta from this change (which touches only
    `internal/gopilog`).
  - Apples-to-apples subset, same command, same host, same session:
    base commit a93d18e → `ok … 77.655s`; **this commit → `ok … 94.691s`**.
  - Targeted run of the wiring this change could affect:
    `go test -count=1 -run 'RoundGrammar|RoundArg|HTTPSelector' ./cmd/agent/`
    → `ok github.com/nick/gopi/cmd/agent 0.011s` (all four tests, incl.
    `TestRoundGrammarIsParsedByGopilog`, `TestRoundSelectionIsNotTautological`,
    `TestRoundArgGrammarTable`, `TestLogMovedPointerForMalformedRound`).
  - `go test -count=1 ./internal/gopilog/` → `ok … 0.959s`.
  - Pre-existing, documented: `devdoc/checkpoint-list-hang.md` ("daemons
    hammering the shared memory DB — known pre-existing flake"),
    `devdoc/bidir2-fix.md` (SQLite concurrency flake in full runs), and master's
    own `devdoc/archive-namespace-single-source.md` recording `./cmd/agent/`
    ok 140.1s in a quiet run while flaking under load.
  - The timeout panic's "running tests:" list named a DIFFERENT test on each
    attempt (`TestSweepZombiesCLI_KillMarksFailed`, then
    `TestAgentBranchScan_TwoBranchesTwoRows`, then
    `TestAgentBranchScan_ClosedRowStaysMasked`) — the signature of a broadly
    slow package under host load (this box was concurrently running a 6-way
    `dd … conv=fsync` I/O load test, repeated `go test ./internal/agent/`, and
    an 80%-CPU wine process), not of a single test this change could break.
- Stale-reference sweep: `grep -rn '<removed symbols>' --include=*.go .` → only
  doc-comment name mentions inside `httpview.go`'s status block (no `(` call
  sites, so the acceptance grep `FormatHTTPRounds(\|FormatHTTPRoundRequestBody(\|HTTPDetailIndex(`
  is empty)
- `ParseHTTPSelector` production call site preserved:
  `cmd/agent/cli_log.go` (`roundSelectionFor`/`roundArg`)

## Commit

`chore(gopilog): delete the orphaned per-round digest view API` — see branch
`chore/gopilog-httpview-removal`.
