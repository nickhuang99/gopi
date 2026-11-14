<!-- doc-class: historical -->
# The /proc/<pid>/stat parse has ONE home: internal/procname

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Branch** `feat/procstat-single-parse` (fresh off master, Rule 23 — the archive
branch `archive/fix/cwd-and-lifetime` was READ, never committed on).

## Why this task exists

Three production readers each carried their own copy of "starttime is field 22 of
/proc/<pid>/stat, counted from after the LAST ')'", plus a FOURTH copy of the
same rule hidden in a sibling column reader (the parent pid). The rule they all
re-implement is the *comm trap*: field 2 is the process's own name, may contain
spaces and parentheses, and a whitespace split of the whole line therefore reads
a NEIGHBOURING NUMBER — silently, because the wrong column is still a plausible
tick count. Four copies means the next fix lands in one of them.

The archive branch `fix/cwd-and-lifetime` had started this consolidation; its
`internal/procname/liveness.go` (a three-verdict `ParseStartTime`) was since
SUPERSEDED by master's newer three-factor liveness API (task `01a0b1ab`,
`StatField`/`StartTicks`/`Verdict`). So only the parse + constant consolidation
was recovered, never the retired liveness file.

## The single home, and why it is internal/procname

`internal/procname` is the leaf package: it imports **nothing project-level**
(`fmt`, `os`, `strconv`, `strings` — verified), so every consumer may import it
without a cycle, and it already owned the process FACT vocabulary
(`ProcRootDefault`, `StatName`, `StatFieldState`, `StatFieldStartTime`,
`StatFieldStartTime`'s derivation from `statFieldStartOffset`).

Import direction (verified before editing, Rule 14):

| consumer | direction | status |
|---|---|---|
| `internal/scratch` | scratch → procname | added by this change |
| `internal/tools` | tools → procname | already existed |
| `internal/config` | config → procname | already existed |
| `internal/testutil` | testutil → procname | already existed |
| `internal/procname` | → (nothing project-level) | verified, no cycle |

## What the single implementation is

`procname.StatField(statLine, field)` — the ONE parse. `false` means **"cannot
answer"** (no `)`, or the line is shorter than the requested field), never a
fabricated value.

Two new entry points expose it in the two shapes the old copies had:

- `procname.StatFieldString(path, field) (string, bool)` — read a stat FILE and
  take one field. (The tools copies held a *path*, not a text line.)
- `procname.StatStartTime(statLine) (uint64, error)` — field 22 as a tick count,
  the `(0, error)` shape `scratch.ParseProcStartTime` had. An unparseable line
  or a non-numeric column is an **error**; the caller never reads a 0 as a value
  (Rule 15).

New constants (Rule 1a — field numbers are literals with meaning, so they are
named once): `StatFieldParent` (4), `StatFieldSession` (6), `CommName`,
`CwdName`, `ProcRootDefault`.

## The sites rewritten as delegations

| # | site | before | after |
|---|---|---|---|
| 1 | `scratch.ParseProcStartTime` (`internal/scratch/holder.go`) | inline last-`)` + hardcoded index 19 + `ParseUint` | `procname.StatStartTime(raw)` |
| 2 | `tools.procStartTicks` (`internal/tools/taskproc.go`) | inline `bytes.LastIndexByte` + `Fields` + index `22-3` | `procname.StatFieldString(...)`; **the caller-side `n <= 0` guard is kept here** |
| 3 | `tools.finishCleanupProcParent` (`internal/tools/task_finish_cleanup.go`) | inline last-`)` + `fields[procStatParentField]` | `procname.StatFieldString(..., procname.StatFieldParent)` |

Plus **five more copies** that were NOT named in the briefing — two found by the
first version of the gate, three by the corrected detector once it recognised the
`")"` string spelling of the search key:

| # | site | before | after |
|---|---|---|---|
| 4 | `config.parentIsWorkerGopi` (`internal/config/agentguard.go`) | inline `LastIndexByte` + `Fields[1]` | `procname.StatFieldString(..., StatFieldParent)` |
| 5 | `testutil.parseProcStat` (`internal/testutil/procident.go`) | inline last-`)` + `fields[n-3]` for starttime/ppid/session/state | `procname.StatField(raw, field)` per column |
| 6 | `daemoncwd.ParentOf` (`internal/daemoncwd/daemoncwd.go`) | inline `LastIndex` + `Fields[statPPIDField]` | `procname.StatFieldString(..., StatFieldParent)` |
| 7 | `signalrec.sidOf` (`internal/signalrec/signalrec.go`) | inline last-`)` + `fields[3]` (a bare literal) | `procname.StatField(raw, StatFieldSession)` |
| 8 | `cmd/agent.workerDetached` (`cmd/agent/cli_worker_status.go`) | inline last-`)` + `fields[3]` (a bare literal) | `procname.StatFieldString(..., StatFieldSession)` |

All five keep their own error/best-effort shape (`(0,false)`, `(0, err)`, or
"unreadable ⇒ not detached") — the delegation changes which code computes the
field, never what the caller does with a failure.

### Constants aliased, never re-typed (Rule 13)

- `tools`: `procCmdlineName = procname.CmdlineName`,
  `procStatFieldOffset = procname.StatFieldState`,
  `procStatStartTimeField = procname.StatFieldStartTime`,
  `procStatParentField = procname.StatFieldParent - procname.StatFieldState`.
- `tools` (`task_finish_cleanup.go`): `procRootName = procname.ProcRootDefault`,
  `procCwdName = procname.CwdName`, `procCommName = procname.CommName`,
  `procStatName = procname.StatName`.
- `scratch`: `ProcRootDir = procname.ProcRootDefault`,
  `ProcCwdName = procname.CwdName`, `ProcStatName = procname.StatName`.
- `testutil` (`procident.go`): `procDir`, `procStatName`, `procCmdlineName`,
  `procCwdName` and the four field numbers.

`procStatParentField` moved into the same const block as `procStatFieldOffset`
so it can be expressed as field − offset instead of a bare `1`.

## Behaviour preserved (the load-bearing part)

- **Trap untouched**: every path still counts fields after the LAST `)`. Nothing
  was "simplified" to a whitespace split.
- **Refusals unchanged**: unreadable/short/malformed → `(0,false)`, `("",false)`
  or an error naming the column. No entry point fabricates a 0.
- **The `n <= 0` guard stayed on the caller side** (`procStartTicks`), exactly as
  before — the parser reports field 22 as the kernel wrote it.
- **`parseProcStat`'s error SHAPE changed** (the only intentional, documented
  difference): it now reports the specific missing column ("has no field 22
  (starttime)") instead of the old aggregate "has N fields after comm, need 21".
  It is still an error in every failing case, still names the pid; both shapes
  are unexported to an internal test. Nothing asserts the old text (checked).

## Proof

1. `internal/procname/procname_test.go`
   - `TestStatFieldTrapCommShapes` — `bash (deleted)`, `gopi worker`, `(bash)`,
     `a ((((b`, `cuda-gdb`, each through field 22, the parent column and the
     state column.
   - `TestStatFieldRefusesWhatItCannotAnswer` — no comm / empty / ends at comm /
     too short / non-numeric ⇒ refusal, never a value.
   - `TestNaiveWhitespaceSplitMisreadsTheTrapLine` — the **red proof kept as a
     test**: the retired whole-line split returns `70016` where the line's field
     22 is `987654321` (a wrong number, not an error), while the real parse
     accepts the same line.
2. `internal/tools/procstat_parse_delegation_test.go` — **the delegation proof on
   a REAL kernel line**: a child is exec'd from a scratch copy named
   `bash (deleted)`, so /proc carries the trap comm genuinely (waited for, not
   assumed, with an ETXTBSY-retrying fixture write). Measured line (observed
   log): naive whole-line split reads `0` where field 22 is `563094242`. All
   entry points — `procname.StatField` / `StatStartTime` / `StatFieldString` /
   `StartTicks`, `scratch.ParseProcStartTime` / `ProcStartTime`,
   `tools.procStartTicks` / `finishCleanupProcParent` — must return the tick
   count an independent oracle (the test's own last-`)` parse) reads.
   `TestProcStatParseDelegationKeepsTheRefusal` pins the failure half through
   every entry point (`pid = 1<<30` cannot exist here).
3. `internal/tools/procstat_parse_single_source_test.go` — the **enforcing gate
   (Rules 13/14)**: (a) the single home really holds exactly one `StatField` with
   the last-`)` parse; (b) each of the EIGHT delegating sites still carries its
   delegation call and NOT the retired body; (c) a walk of every production `.go`
   under `internal/` + `cmd/` fails on the *shape* "a `LastIndex` of `')'`
   feeding a `Fields(` split within 6 lines". Read floor 200 files, so a broken
   walk fails instead of passing green-blind.
4. `cmd/agent/procstat_parse_single_home_test.go` — the repo-wide PRODUCTION
   detector (a report, not a blocker, per the repo's gate convention: a detector
   that cannot tell a fix from a regression must not hold a merge). Measured
   output: **366 production files scanned, 0 re-inlined sites**. Its predicate is
   pinned BOTH ways on synthetic sources
   (`TestProcStatParseDetectorPredicate`): it fires on the two retired spellings
   (`bytes.LastIndexByte(x, ')')` and `strings.LastIndex(x, ")")`) and stays
   silent on a comment, a diagnostic string, the delegation itself, and a
   single-character probe after the last `)`.
   `TestProcStatParseDetectorReadsTheRealTree` is the read floor.

   **The detector is what found sites 6-8.** Its first version stripped ALL
   string literals and therefore missed the `")"` spelling — the report said 0
   while the predicate test said "you would let a FIFTH copy in". Fixing the
   strip (elide only literals longer than the parse's own one-byte search key)
   turned the report into 3 findings, all real.

### Red-then-green, observed

| injection | red | green after restore |
|---|---|---|
| the retired whole-line split into `StatField` | `--- FAIL: TestStatFieldTrapCommShapes` (all 5 comms) and `--- FAIL: TestProcStatParseDelegationAgreesOnRealTrapComm` | `ok internal/procname`, `ok internal/tools` |
| the retired copy re-inlined into `tools.procStartTicks` | `--- FAIL: TestProcStatParseHasOneImplementation` — "carries its own stat-parse body (`LastIndexByte(b, ')')`)" + "internal/tools/taskproc.go:182 re-inlines the parse" | `ok internal/tools` |

Both files were restored byte-identically (`git diff` shows only the intended
48-line procname change).

## Verification

```
CGO_ENABLED=0 go build ./...                                  # OK
go vet ./internal/procname ./internal/scratch ./internal/tools
       ./internal/config ./internal/testutil                  # OK
go test ./internal/procname ./internal/scratch ./internal/config   # ok
go test ./internal/tools                                     # ok
```

## Not Done (honest)

- **Three other stat readers still index the line directly and were NOT
  consolidated**: `tools.takeoverProcessState` (`merger_takeover_live.go`,
  state letter), `tools.pidIsZombie` (`taskzombie.go`, state letter),
  `service_control.isLive` (`internal/tools/service_control.go`, state letter,
  hardcodes `"stat"`). They are in scope for "the single parse" in spirit but
  each reads a SINGLE character at a fixed offset after the last `)`, and that
  offset can never be shifted by a comm that CONTAINS parentheses: content after
  the last `)` is `" S <ppid> …"` either way. Folding them into `StatField` is a
  further change with no behaviour gain, so it is left as a separate task rather
  than smuggled in. Their one pre-existing corner (a comm whose *content* begins
  with `(` and ends with `)`, e.g. `a ((((b`) is unchanged by this work — the
  double-paren shape was already mis-read before and after; only the delegated
  sites now read it correctly. The gate does not flag them (no `Fields` split),
  deliberately: the gate's predicate is the parse *shape* it is precise about,
  and a broader regex would fire on harmless line scans.
- `tools.procIdentityCmdlineMax` (512) was NOT aliased: master's `procname` has
  no such constant (the archive's `identity.go` would have added one). It is
  this package's own presentation bound for the task-row schema, so it stays.
- `internal/procname` still has no `TestMain`/scratch pin: the new test file
  creates no temp directory (fixtures are strings and a real child process), so
  the Rule 22 pin gate has nothing to demand and `t.TempDir()` is used only in
  the tools test whose suite TestMain already pins the root.
