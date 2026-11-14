<!-- doc-class: historical -->
# Task Report: cov-gopilog-hp — internal/gopilog function-level coverage 91.4% → 100.0%

**Branch:** `feat/cov-gopilog-hp` · **Worktree:** `~/workspace/gopi-cov-gopilog-hp`
**Charter:** TESTS ONLY — no product code was modified (`git diff --stat` = one new `_test.go`).

## Completed

### 1. Inventory read + target list

The briefing said the 5 uncovered funcs were "concentrated in formatter.go (1173 lines)". **That was wrong** — reading the package showed they live in four other files. Always verify the briefing's attribution against the source before trusting it (memory #1687 instructs exactly this).

| Function | Definition | Prod callers (`file:line`) |
|---|---|---|
| `ParseHTTPSelector` | `internal/gopilog/httpview.go:41` | **NONE** (see §6 bug 1) |
| `ResolveSessionDir` | `internal/gopilog/reader.go:36` | `reader.go:372`, `rawlog.go:223`, `cmd/agent/cli_resume.go:68`, `cli_stats.go:236`, `cli_raw.go:308`, `commands.go:391`, `cli_log.go:456,481,507,531,596` |
| `SessionRawLogPath` | `internal/gopilog/rawlog.go:297` | `rawlog.go:359`, `cmd/agent/cli_resume.go:82` |
| `SessionStarted` | `internal/gopilog/stats.go:543` | `stats.go` (internal), `cmd/agent/cli_stats.go:370` |
| `(*SessionStats).ToolRanking` | `internal/gopilog/stats.go:667` | `stats.go:1086` |

code_search evidence (one call per symbol, structured hits):

```
ParseHTTPSelector   internal/gopilog/httpview.go:41  func  func ParseHTTPSelector(arg string) (HTTPRoundSelector, error)
ResolveSessionDir   internal/gopilog/reader.go:36    func  func ResolveSessionDir() (string, error)
SessionRawLogPath   internal/gopilog/rawlog.go:297   func  func SessionRawLogPath(path string) string
SessionStarted      internal/gopilog/stats.go:543    func  func SessionStarted(path string) (time.Time, bool)
ToolRanking         internal/gopilog/stats.go:666    reference (doc comment)
ToolRanking         internal/gopilog/stats.go:667    func  func (st *SessionStats) ToolRanking() []ToolCount
ToolRanking         internal/gopilog/stats.go:1086   reference (call site)
```

Dependent types read: `RawEvent` (types.go:9, incl. `RawLogPath` at types.go:23), `ToolCount` (stats.go:101), `SessionStats` (stats.go:108), `HTTPRoundSelector` (httpview.go:33).

### 2. New tests — `internal/gopilog/cov_gopilog_hp_test.go` (prefix `covGlog*`, 7 tests / 33 subtests)

| Test | Happy path | Boundary cases | Error path |
|---|---|---|---|
| `TestCovGlogParseHTTPSelector_Matrix` | `""`, `"all"`, `"last"`, `7`, `-3`, `+9` | whitespace-only → All; `"  all  "`; `"\tlast\n"`; explicit `0` | — |
| `TestCovGlogParseHTTPSelector_Malformed` | — | `"-"`, `"--2"`, `"0x10"`, `"٣"` (non-ASCII digit) are **not** `Atoi`-numbers | 9 rejected inputs: non-nil err, zero-value selector (no partial leak), rejected arg echoed in the message |
| `TestCovGlogResolveSessionDir_Happy` | isolated `GOPI_HOME` → `<home>/.gopi/sessions` | parent dir == `config.GopiDir()`; base == `config.SessionsDirName`; **purity**: resolving creates nothing | — |
| `TestCovGlogResolveSessionDir_Error` | — | — | `GOPI_HOME=""` **and** `HOME=""` → err wraps `"cannot resolve gopi dir"`, returns `""` |
| `TestCovGlogSessionRawLogPath` | `session_start.rawLogPath` returned | event on a **later** line after junk/malformed/blank lines; pre-resume session (field absent) → `""`; no `session_start` at all; empty file; blank-only file | missing path → `""`; **directory** arg → `""` |
| `TestCovGlogSessionStarted` | exact timestamp round-trip (RFC3339) | **zero** timestamp → `ok=false`; decoy line whose *content* embeds `"session_start"` followed by the real event; a `session_end` line is not a start; empty file | missing path → `(zero,false)`; directory → `(zero,false)` |
| `TestCovGlogToolRanking_Ordering` | calls desc, then name asc (`read 9 > bash/edit/write 2`) | empty `ToolKinds` → 0 entries; **stability** (repeat call identical); **purity** (receiver slice not reordered) | — |
| `TestCovGlogToolRanking_FromRounds` | real parsed rounds (`statsRawLog` fixture): read×2 > bash×1 | — | — |

Real contract findings encoded as assertions (not guessed from the doc comments):

- `SessionStarted` pre-filters lines with `sessionStartProbe` — a line *containing* the literal but decoding to another event must be skipped, and the real `session_start` found afterwards. The decoy subtest pins that.
- `SessionRawLogPath` / `SessionStarted` both **swallow** unreadable input into an empty/not-found result (documented contract) — a directory is the free way to test "opens but cannot be scanned".
- `ToolRanking` copies before sorting (`append([]ToolCount(nil), …)`) — asserted, because a refactor dropping the copy would silently reorder every caller's `ToolKinds`.

### 3. Metric honesty check

`func_coverage.py` is a name-appearance metric, so a call-site count was verified per function (a mere mention would be dishonest coverage):

```
ParseHTTPSelector: 5 call sites
ResolveSessionDir: 4 call sites
SessionRawLogPath: 7 call sites
SessionStarted:    8 call sites
ToolRanking:       5 call sites
```

### 4. Constants (Rule 1b/13)

Referenced instead of re-typing: `config.GopiDirEnv`, `config.SessionsDirName`, `config.GopiDir()`, `toolnames.ToolRead/ToolBash/ToolWrite/ToolEdit`, and the package's own `eventTypeSessionEnd` (reader.go:331).
Two values have **no** existing constant and are declared once at the top of the test file: `covGlogEventSessionStart` (`"session_start"` — see §6 bug 2) and `covGlogRawLogPath` (fixture path). `covGlogEventRequest` likewise.

## Not Done

- **No product code touched** (charter). Both findings below are filed as tickets, not fixed.
- `formatter.go` was **not** the gap location the briefing claimed, and needed no test changes: all 5 gaps were in `httpview.go`/`reader.go`/`rawlog.go`/`stats.go`. Nothing in `formatter.go` was modified.
- Pre-existing Rule 1b violations in the package's *other* test files were left alone (out of a tests-only slice's charter, and renaming in sibling files invites merge conflicts).

## Bugs filed

1. **Orphaned `--http` view API in internal/gopilog** — `ParseHTTPSelector` (httpview.go:41) and `FormatHTTPRounds` (httpview.go:83) have **zero production call sites**; so does exported `FormatHTTPRoundRequestBody` (httpview.go:454). Commit `e23d506` ("feat(cli): gopi stats + gopi raw; move log --http/--raw out") removed the `cmd/agent` consumer (`gopi log --http` now only prints a "moved out" pointer via `logMovedPointer`, cli_log.go:402-437) but left the ~470-line view surface exported and undocumented as dead. Contradicts GOPI.md Rule 3's minimalism premise.
   **Filed:** task `01a08c9a-d396-772b-a29b-1f8b67e50444` (tags `bug,gopilog,dead-code`, priority 2).
2. **`"session_start"` is a bare literal in 5 product sites while only `session_end` has a constant** — `reader.go:514`, `rawlog.go:251,315,342`, `formatter.go:801` spell `"session_start"` inline, while `eventTypeSessionEnd` (reader.go:331) exists for the sibling event. Rule 1b/13 defect: a rename/misspelling in one site silently stops matching the other event's writers (and the value also exists twice more as probe vars, stats.go:571/578).
   **Filed:** task `01a08c9a-d702-727c-ae99-7486744cf7b6` (tags `bug,gopilog,hardcoding`, priority 2).

## Verification

```
$ python3 scripts/func_coverage.py --pkg internal/gopilog --uncovered      # BEFORE
package                                     funcs    cov    pct  tests
--------------------------------------------------------------------------
internal/gopilog                               58     53  91.4%
--------------------------------------------------------------------------
TOTAL                                          58     53  91.4%

Uncovered exported funcs (by package):

  internal/gopilog (5):
    - ParseHTTPSelector
    - ResolveSessionDir
    - SessionRawLogPath
    - SessionStarted
    - ToolRanking
```

```
$ python3 scripts/func_coverage.py --pkg internal/gopilog --uncovered      # AFTER
package                                     funcs    cov    pct  tests
--------------------------------------------------------------------------
internal/gopilog                               58     58 100.0%
--------------------------------------------------------------------------
TOTAL                                          58     58 100.0%

Uncovered exported funcs (by package):

NOTE: conservative metric — a func counts as covered only if its name
appears in a *_test.go file of the same package. Target: 100%.
```

```
$ go test -count=1 ./internal/gopilog/          # RUN 1
ok  	github.com/nick/gopi/internal/gopilog	2.018s

$ go test -count=1 ./internal/gopilog/          # RUN 2 (non-flaky)
ok  	github.com/nick/gopi/internal/gopilog	1.584s

$ go test -count=1 -run TestCovGlog -v ./internal/gopilog/ | tail -1
ok  	github.com/nick/gopi/internal/gopilog	0.016s

$ gofmt -l internal/gopilog/       # (empty)
gofmt clean
$ go vet ./internal/gopilog/
vet OK
$ CGO_ENABLED=0 go build ./...
build OK
```

New tests, all PASS (33 subtests): `TestCovGlogParseHTTPSelector_Matrix`, `TestCovGlogParseHTTPSelector_Malformed`, `TestCovGlogResolveSessionDir_Happy`, `TestCovGlogResolveSessionDir_Error`, `TestCovGlogSessionRawLogPath`, `TestCovGlogSessionStarted`, `TestCovGlogToolRanking_Ordering`, `TestCovGlogToolRanking_FromRounds`.

**Commit:** `5166840 test(gopilog): close function-level coverage 91.4% -> 100% (5 exported funcs)` (branch `feat/cov-gopilog-hp`)

## memory_store entries

- `#1727` — gopilog coverage-slice recipe (target-list verification, per-file gap locations, session-JSONL fixture shape, covGlog* prefix, name-appearance honesty check, toolnames/config constants, bug findings).

## Workflow feedback

- **The briefing's file attribution was wrong** ("5 uncovered funcs, concentrated in formatter.go"): all 5 live in httpview.go/reader.go/rawlog.go/stats.go. Cost: ~2 tool rounds. Cheap fix upstream — generate the target table from `func_coverage.py` output joined with `grep -n '^func '` per file before writing the briefing.
- **The memory-first checkpoint paid off**: hits #1687/#1694/#1724 supplied the entire recipe (worktree first, read the metric script, verify the target list, task-unique helper prefix, run the suite twice). Near-zero exploration cost this run.
- **Rule 11 note**: this is a tests-only slice — no tool/prompt change, so the LLM-adoption gate does not apply; the deliverable is the metric + contract assertions above.
- **Reusable cost fact**: `go test -count=1 ./internal/gopilog/` is ~1.6-2.0s (unlike internal/tools' 250s+) — a plain bash call suffices, no background job needed. ANTHROPIC_AUTH_TOKEN was not required for this package.
