<!-- doc-class: historical -->
# Task Report: orphaned `--http` view API in internal/gopilog (grammar single-sourced + deprecation)

Branch: `fix/gopilog-httpview-orphan` (worktree `~/workspace/gopi-gopilog-httpview`)
Recovery (branch is shared with the main repo — no push needed):
`cd /home/user/work/GoPi && git show fix/gopilog-httpview-orphan:devdoc/cov-gopilog-httpview-orphan.md`
Origin: bug filed while closing function-level coverage of internal/gopilog
(branch `feat/cov-gopilog-hp`, commit 5166840 — devdoc/cov-gopilog-hp.md §Bugs filed).
Follow-up removal task: `01a08ca1-35d8-7a37-a687-f836abbd94c9` (tags=[cleanup]).

## Problem

Commit `e23d506` ("feat(cli): gopi stats + gopi raw; move log --http/--raw out")
removed the only consumer of the `--http` per-round view. `gopi log <id> --http …`
now only prints a "moved out" pointer (`cmd/agent/cli_log.go` `logMovedPointer`),
and the gopilog-side parser + formatter were left exported with no caller:

| symbol | prod callers (before) |
|---|---|
| `ParseHTTPSelector` (httpview.go:41) | 0 |
| `FormatHTTPRounds` (httpview.go:83) | 0 |
| `FormatHTTPRoundRequestBody` (httpview.go:454) | 0 |
| `FormatHTTPRoundResponseBody` (httpview.go:482) | 0 |

Two costs: (1) dead exported surface (GOPI.md Rule 3); (2) the `all|last|N`
round grammar was implemented TWICE — `ParseHTTPSelector` (only tests exercised
it) and `cmd`'s own `roundArg` — with nobody comparing them, so either side
could drift unnoticed.

## Decision (owner question in the bug: reuse vs delete)

Split the two halves, because the two halves have different successors:

- **The grammar stays and gets one owner: `gopilog.ParseHTTPSelector`.**
  `cmd/agent`'s moved-pointer path now PARSES `--http` through it
  (`roundSelectionFor` → `roundArg` in `cmd/agent/cli_log.go`). The grammar is
  shared with `gopi raw <round>` / `gopi stats --rounds`, so a single parser is
  the point — this is now a **production call site**.
- **The digest view is dead and gets deprecated, not revived.** A review of
  `devdoc/log-stats-raw-impl-claude.md` (the FINAL-USAGE-PROPOSAL migration
  record that `logMovedPointer` quotes) shows the migration deliberately folded
  the old views into `gopi stats --rounds` (numbers) and `gopi raw <id> [round]
  [request|response]` (bytes). The digest view (request fingerprint, per-message
  digest, response digest) has **no successor** — reviving it via a new flag on
  `gopi raw` would contradict that proposal AND `gopi raw`'s documented rule
  ("raw prints BYTES … no --json/--text/--format flags, passing one is an error").
  So the three roots and their ~9-member unexported chain carry `// Deprecated:`
  notes naming the successor commands, and removal is filed as a follow-up task
  (deleting ~470 lines in the fix commit would have mixed a grammar fix with a
  large deletion — the reviewable unit is smaller this way).

`humanBytes` / `orDash` / `headerSep` / `readRawLogLineSpan` were checked
individually: they have live callers (`rawview.go`, `stats.go`) and are NOT part
of the orphan.

## What changed

1. `cmd/agent/cli_log.go`
   - new `roundSelectionFor(httpArg)` — delegates to `gopilog.ParseHTTPSelector`;
     it defines NO grammar of its own.
   - `roundArg` rewritten to decide from the PARSED selection
     (`Detail && Round > 0` + a canonical re-parse), so the two-grammar split is
     gone while the pointer text stays byte-identical.
2. `internal/gopilog/httpview.go`
   - `HTTPSelectorAll` / `HTTPSelectorLast` keyword constants, referenced by
     `ParseHTTPSelector` (Rule 1b/13 — the compared keywords had no definition).
   - fixed the parser's mangled error text (`all|last|last|N` → `all|last|N`).
   - a "Round-view API status" note at the top of the file: who is dead, who is
     not, and which task removes the dead ones.
   - `// Deprecated:` notes on `FormatHTTPRounds`,
     `FormatHTTPRoundRequestBody`, `FormatHTTPRoundResponseBody`, `HTTPDetailIndex`.
3. Tests
   - `cmd/agent/cli_log_roundgrammar_test.go` (new) — the cross-package grammar
     contract and the non-tautology (delegation) check.
   - `internal/gopilog/httpview_selector_grammar_test.go` (new) — the keyword
     constants are exactly the accepted spellings.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK.
- Tests: `go test -count=1 ./internal/gopilog/` → ok (1.5s / 2.2s, run twice);
  `go test -count=1 -timeout 900s ./cmd/agent/` → **ok, 432.492s, exit 0**
  (full package; the package is slow on a loaded box — 5+ concurrent worktrees
  run their own `agent.test` children — but it passes).
  - Note (honest): an earlier unattended full-package run died with
    `signal: terminated … FAIL github.com/nick/gopi/cmd/agent 332.124s`, i.e.
    killed under machine contention, not an assertion failure. Re-run after the
    box quietened: green at 432s. The grammar tests this fix touches are
    self-contained any way (`-run 'Grammar|Keyword|Selector'` → ok 0.014s).
- `gofmt -l internal/gopilog/ cmd/agent/` → empty; `go vet` on both → clean.
- Production call site (acceptance (a)):
  `grep -rn 'ParseHTTPSelector(' --include=*.go .` → definition +
  `cmd/agent/cli_log.go:23` (`roundSelectionFor`).
- Grammar-contract evidence — the test is NOT vacuous. Two independent mutations
  each failed it:
  1. cmd re-derives the grammar
     (`ParseHTTPSelector(strings.ToLower(httpArg))` inside `roundSelectionFor`)
     → `TestRoundGrammarIsParsedByGopilog` FAILs on `"All"` / `"LAST"`.
  2. the parser gains an unlisted spelling
     (`arg == HTTPSelectorAll || strings.EqualFold(arg, "ALL")`) → the SAME
     shared table fails on `"All"`, AND the gopilog-side
     `TestHTTPSelectorKeywordConstantsAreTheAcceptedSpellings` fails on
     `"All"`/`"ALL"` — one drift, two independent detectors.
  Both restored; suite re-verified green.
- Backward compatibility: the pre-existing `TestLogMovedPointerTable` (9 rows)
  still passes unchanged, and the new table cross-checks `roundArg` against an
  independent statement of its old behavior across 16 inputs (incl. `"07"`,
  `"+3"`, `"0x10"`, non-ASCII digits).
- End-to-end byte-identity of the pointer line (the refactor's real risk):
  built the pre-fix binary (`4d9b779~1` in a throwaway worktree) and the
  post-fix binary, ran `gopi log <sid> --http <arg>` for 12 arguments
  (`""`, `all`, `last`, `3`, `0`, `-2`, `07`, `bogus`, `last2`, `" 3 "`, `"3 "`,
  `ALL`) and md5-compared stdout+stderr: **12/12 SAME**. Observed mapping is the
  historical one — numeric → `gopi raw <id> N`, everything else (incl. `last`,
  `all`, `0`, `-2`, `bogus`) → `gopi stats <id> --rounds`.

## Not Done (honest)

- The dead digest view is DEPRECATED, not deleted — removal is follow-up task
  `01a08ca1-35d8-7a37-a687-f836abbd94c9` (deliberate: one reviewable unit per
  commit; the removal also has to decide where `HTTPRoundSelector` lives once
  httpview.go goes).
- No new CLI surface was added to expose the digest view: that would reverse the
  documented `gopi log --http` → `gopi stats`/`gopi raw` migration.

## Commit

`fix(gopilog): single-source the round grammar in cmd, deprecate the orphaned --http view`
