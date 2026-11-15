<!-- doc-class: historical -->
# Task Report: internal/peakhours — NextOffPeak names the EXACT off-peak boundary

**Branch**: `fix/peakhours-exact-boundary` (fresh branch — Rule 23: never commit onto
someone else's branch).
**Worktree**: `/home/user/workspace/gopi-peak-exact`.

**Stacking note (the feature landed mid-task).** The epic was briefed against the
unmerged feature tip `feat/peak-hour-guard` @ `ec6ba9f1`, and the fix was first
written and committed there (`55e4ed62`). While it was being verified, the peak-hour
feature was **merged into master** (`86467b49`, via `ffbf6eba`), so the epic's
premise changed underneath: the defect is now reachable from master, and the branch
base became a fork point rather than the tip of the line. `internal/peakhours/` is
byte-identical between `ec6ba9f1` and master `86467b49` (`git diff --stat ec6ba9f1
master -- internal/peakhours` is empty), so the fix was **rebased onto master**
(`git rebase --onto master ec6ba9f1`, clean, no conflict) and the branch now sits
directly on master's tip. The pre-rebase commits are preserved as
`backup/peakhours-exact-on-ec6ba9f1` so the earlier path stays readable (Rule 23:
the old path is evidence, not garbage). Nothing was committed onto the feature
branch itself.
**Epic**: fix the semantic defect filed by the merge-resolution report
(`devdoc/merge-resolve-peak-hour-guard.md`, bug task `01a09a69-…`): `NextOffPeak`
walked candidates as `at + i hours` **from the current minute**, so for a mid-hour
`now` the returned "money stops being doubled" instant was up to 59 minutes late —
and named a moment inside no window at all.

## Completed

- `internal/peakhours/peakhours.go`
  - `hourStart(local)` — new helper: the whole hour at or before `local`, rebuilt
    from the civil fields (`time.Date`) rather than `time.Truncate`, so it is a civil
    hour boundary whatever the zone offset is. It is the lattice the exact walk
    advances on, and the only instant a peak boundary can be (windows are whole
    hours by construction — `parseClock` rejects minute components).
  - `NextOffPeak` — exact semantics, two cases and no others:
    - `at` already off-peak → returns **`at` itself** (nothing stops being doubled
      later; naming a later hour was the same over-statement in reverse).
    - `at` in peak → returns the **end of the peak run** `at` is inside: the next
      whole hour after `at`, then the following ones while still peak.
    - `ok=false` (all 24 hours declared peak) still returns `at + 24h`.
  - `WaitUntilOffPeak` — doc updated; behaviour unchanged in shape (it is the
    distance to the returned instant), so it now reports the exact remaining time
    (18:56 in 18:00-19:00 → 4m) and 0 when off-peak.
  - No new magic numbers/literals: `hoursPerDay` and `time.Hour` only (Rule 1).
- `internal/peakhours/peakhours_test.go`
  - `atClock(hour, minute)` helper added (mid-hour fixtures).
  - `TestNextOffPeakWalksFromTheCurrentMoment` → **REPLACED** by
    `TestNextOffPeakNamesTheBoundaryNotTheNextHourFromNow` (table: mid-hour in a
    one-hour window, mid-hour in a three-hour window, closing boundary 12:00,
    off-peak mid-hour 13:30, opening boundary 14:00, midday gap 12:45) plus the
    invariants "never inside a peak window" and "when the walk advanced, a whole
    hour".
  - **`TestNextOffPeakMidHourRegression`** — the defect's minimal repro:
    `NextOffPeak(18:56, 18:00-19:00) == 19:00` and `WaitUntilOffPeak == 4m`.
  - `TestWaitUntilOffPeak` — rewritten from "whole hours / 2h for 10:30" to the
    exact durations (90m for 10:30 in 09:00-12:00, 4m at 18:56, 1h at the opening
    boundary, 0 off-peak), plus a cross-check that the duration equals the
    distance to `NextOffPeak`'s instant (the two are rendered in one line) and the
    all-day-peak no-boundary case.
- `internal/config/peakhours_decision_test.go` (new) — the REFUSAL-level proof, at
  the layer the operator actually reads:
  - `TestPeakDecisionNamesTheExactWindowEnd`: `GOPI_PEAK_HOURS=18:00-19:00` at
    18:56 → `Peak` armed, window 18:00-19:00, `WakesAt == 2026-09-13T19:00+08:00`,
    `Wait == 4m`.
  - `TestPeakRefusalStatesTheExactBoundary`: the rendered `PeakRefusal` contains
    `2026-09-13T19:00:00+08:00` and `4m0s away`, names the window and the refused
    verb, and **does not** contain `19:56` (the mid-hour-walk artefact).
  - `TestPeakDecisionOffPeakInsideHourIsNotRefused` (13:30 → not refused, no
    Wait/WakesAt) and `TestNextOffPeakAtClosingBoundaryIsTheBoundaryItself`
    (12:00 → 12:00, wait 0 — End is exclusive).
- `devdoc/peakhours-exact-boundary.md` (this file).

## Not Done

- **Weekday semantics are NOT modelled** (pre-existing, untouched): DeepSeek's peak
  windows are *Mon-Fri*, and the package classifies by hour only, so a Saturday
  10:30 is treated as peak. The epic is scoped to the boundary instant, and
  broadening it here would silently change `InPeak` for every consumer
  (`internal/pricing`, the start guard, the worker loop). Worth its own task.
- The conservative walk's **other caller-visible consequence** — the resident
  worker sleeping `d.Wait` — is fixed for free (it reads the same decision), but no
  new `cmd/agent` worker test was added: the existing worker tests exercise the
  deferral with a real clock, and `internal/config`'s decision test is the
  deterministic pin. Stated plainly rather than claimed.
- No changes to `internal/pricing` / `cmd/agent` sources — the fix is a leaf-package
  semantic change and those consumers read it through the same one definition
  (Rule 14). Confirmed by the green runs below.

## The deliberate semantic reversal (why the old tests were replaced)

The removed `TestNextOffPeakWalksFromTheCurrentMoment` pinned the conservative walk
on purpose, with this reasoning: *"overshooting an off-peak boundary can only cost a
little idle time — undershooting it would run inside the window the guard exists to
avoid."* The exact boundary does not undershoot: the answer is the window's End,
which is off-peak by the half-open contract (`Start` inclusive, `End` exclusive)
that `TestInPeakBoundaries` already pins. So the safety argument that justified
conservatism does not apply to the exact instant, while the cost of the old
behaviour was real: the refusal line — *"every token is billed at 2x until X (Y
away)"* — is a factual claim about the provider's clock, and from 18:56 it said
19:56 (1h0m0s), a moment inside no window. The replacement is the tested, documented
change the old comment asked for ("see the devdoc note: the refusal line can
therefore overstate the wait by up to one hour").

**Residual asymmetry, deliberately kept**: an off-peak `at` returns `at` rather than
a boundary. The guard's product for that case is "no refusal at all" (it returns
before `WakesAt` is read), so there is no message to over-state; and `Wait == 0` is
the truthful answer for "money is not being doubled right now". A caller that wants
"the next hour boundary" should ask for it — that is a different question from
"when does doubling stop".

## Verification

- Build: `go build ./...` → exit 0 (`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → exit 0).
- `gofmt -l internal/peakhours internal/config cmd/agent` → no output (clean).
- `go test ./internal/peakhours/` → ok.
- `go test ./internal/config/` → ok (including the four new refusal-level cases).
- `go test ./internal/pricing/ ./internal/standards/` → ok (the other consumers of
  `peakhours`, and the machine-checkable gate suite, unaffected).
- `go test ./cmd/agent/` → see the exact transcript in the section below: the three
  `TestAgentSpawnGuard*` failures are **PRE-EXISTING and environmental**, with the
  root cause identified and both directions reproduced.

### cmd/agent: the three failures are an INHERITED-ENV artefact (root cause, both directions)

The failures are not "worktree hygiene" and not a missing `bin/gopi` — they are this
agent session's own environment leaking into the spawned children:

1. A gopi agent session runs *inside a worker*, so the test process's environment
   carries `GOPI_WORKER_DAEMON_CHILD=1` and `GOPI_DAEMON_READY_FD=3` (here: fd 3 is
   the agent session's own `/proc/<pid>/fd` handle, not a lock file).
2. `guardTestEnv` (`cmd/agent/agent_spawn_guard_test.go`) copies `os.Environ()` and
   strips only the *guard* variables — the two daemon-HANDLE markers are inherited.
3. The test's `gopi worker --once` child therefore takes the daemon-child re-exec
   path instead of the operator command the test asked for, and dies in the
   handshake: `inherited worker lock fd 3 is not a usable file: stat worker.lock: bad
   file descriptor: handshake write failed: write GOPI_DAEMON_READY_FD: bad file
   descriptor`.
4. This is exactly the hazard `zwDropDaemonHandles` documents and fixes in
   `cmd/agent/task_lifecycle_tick_test.go` ("when `go test` itself runs inside a gopi
   worker session … the test process's environment ALREADY carries
   `GOPI_WORKER_DAEMON_CHILD=1` + `GOPI_DAEMON_READY_FD=3`"). The spawn-guard test's
   fixture does not strip them.

Both directions, measured:

| tree | env | `go test ./cmd/agent/` |
| --- | --- | --- |
| master `d760dba1` (untouched, no peakhours semantics change) | inherited (markers present) | **FAIL**: same three `TestAgentSpawnGuard*` |
| feature tip `ec6ba9f1` (untouched) | inherited | **FAIL**: same three |
| master `d760dba1` | markers stripped (`env -u GOPI_DAEMON_READY_FD -u GOPI_WORKER_DAEMON_CHILD`) | **ok** |
| this branch `fix/peakhours-exact-boundary` | markers stripped | **ok** |

Reproduce the determining form:

```bash
env -u GOPI_DAEMON_READY_FD -u GOPI_WORKER_DAEMON_CHILD go test -count=1 ./cmd/agent/
```

The failure is therefore independent of this change in both directions (it exists on
master and disappears on my branch under the same clean environment), and the change
touches no `cmd/agent` file at all (`git diff --stat 55e4ed62^..55e4ed62` below). The
packages the change actually lands in (`internal/peakhours`, `internal/config`) plus
the leaf consumers (`internal/pricing`, `internal/standards`) are green in every
tree.

### Full-package `cmd/agent` run: order-dependent flakiness, and this change's delta

The whole package (not just the spawn-guard subset) was then run on the pristine
control tree **twice, back to back, in the same session** to separate "branch
regression" from "run-order / tree-state artefact". Both runs of the *identical*
untouched tree produced the *identical* two-failure set
(`TestParseTasksSweepZombiesArgs`, `TestEveryHelpConstantFitsTheWrapBudget`), so the
package's verdict is stable for a *warm* tree — and, transitively, the change adds
no `cmd/agent` failure, since it touches no `cmd/agent` file at all
(`git diff --stat master..HEAD` lists only `internal/peakhours/*`,
`internal/config/peakhours_decision_test.go` and `devdoc/*`).

**A fresh worktree is NOT a valid control tree — measured, and it cost several
confounded A/B rounds.** An A/B that ran the *fresh feature* worktree against the
*warm pristine* one produced 38 failures on the fresh side vs 2 on the warm side,
which reads exactly like a catastrophic branch regression. It is not one — the fresh
tree is simply missing two things that are **never tracked by git**:

| prerequisite | how it is produced | why its absence looks like a regression |
| --- | --- | --- |
| `tests/fixtures/log-stats-raw/sessions/*.jsonl`, `.../raw/**` | generated per tree; only `README.md` + `golden/` are tracked (`git ls-files tests/fixtures/log-stats-raw`; the dirs are not in `.gitignore`, they are just untracked build products) | 10 `TestStats*` cases fail with `open …/sessions/….jsonl: no such file or directory` |
| `bin/gopi` | `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` | `TestSweepZombiesCLI_*`, `TestParseTasksSweepZombiesArgs` fail ("needs a gopi binary"), and other E2E tests **SKIP** — the false-green trap |

Every one of the 38 failures is in that "missing prerequisite" family
(`TestStats*`, `TestAcceptance_*` merge-holder fixtures, `TestTaskRun*` seam E2E,
`TestVoice*`, `TestParseTasksArgs*`, `TestDispatchLabels*`, `TestVerbDecisionGate*`
— all of which read a built binary, a generated fixture, or a spawned real daemon).
None of them reads `internal/peakhours`.

**The clean, prerequisite-matched A/B settles it** (both trees warmed identically —
`bin/gopi` built, fixtures copied in — serial, in-session):

| tree | `go test -count=1 ./cmd/agent/` | failing tests |
| --- | --- | --- |
| master `86467b49` (untouched, warmed) | FAIL | `TestParseTasksSweepZombiesArgs`, `TestEveryHelpConstantFitsTheWrapBudget` |
| this branch (warmed) | FAIL | **the identical two** |

`comm -13` of the two failure sets is **empty**: the branch adds no `cmd/agent`
failure. Both residual reds are pre-existing and environmental —
`TestParseTasksSweepZombiesArgs` wants the checked-in `bin/gopi` E2E binary (and
`testutil.GopiBin` SKIPs elsewhere, the false-green trap), and
`TestEveryHelpConstantFitsTheWrapBudget` measures help text against the session's
resolved terminal width. Neither references the peak-hour code; the branch's own
`cmd/agent` peak suite is hermetically green in isolation
(`go test -count=1 -run 'Peak' -v ./cmd/agent/` → `TestPeakGuardVerbNamesTheRefusedVerb`
PASS). The branch touches **no** `cmd/agent` file
(`git diff --stat master..HEAD` lists only `internal/peakhours/*`,
`internal/config/peakhours_decision_test.go`, `devdoc/*`).

### Merge readiness against current master

Master received the peak-hour feature while this task ran: `internal/peakhours/` is
byte-identical between the original stack base `ec6ba9f1` and master's tip
`86467b49` (`git diff --stat ec6ba9f1 master -- internal/peakhours` is empty), so the
fix was rebased onto master's tip and the branch now **descends from master**
(`git merge-base --is-ancestor master HEAD` → true). No `internal/standards/baseline.go`
conflict remains in the picture — that file's merge-time re-key already landed on
master as `86467b49` itself. The branch changes nothing outside
`internal/peakhours/`, the new `internal/config` test and the devdoc.

## Files

| file | change |
| --- | --- |
| `internal/peakhours/peakhours.go` | `hourStart` helper; `NextOffPeak` exact; `WaitUntilOffPeak` doc |
| `internal/peakhours/peakhours_test.go` | conservative test REPLACED; mid-hour regression; exact durations |
| `internal/config/peakhours_decision_test.go` | NEW — decision/refusal-level proof at 18:56 |
| `devdoc/peakhours-exact-boundary.md` | NEW — this report |

## Branch declaration (Rule 6d)

- **Branch**: `fix/peakhours-exact-boundary`, worktree `/dev/shm/gopi-test-scratch/peakhours-exact-wt`
  (a scratch worktree: the `/home/user/workspace` one was reclaimed by the harness's
  worker `finish-cleanup` twice mid-task, once *during* verification — the branch ref
  itself lives in the main repo, so nothing was ever lost),
  based directly on master (rebased twice — off the feature tip, then onto master's
  moving tip). Stacking note above. Final commits: `2aa39e4b` (the fix), `b17033bf` (root cause),
  `50d14267` (restack note), `b8431d51` (the decisive A/B). Prior paths kept in `backup/peakhours-exact-on-ec6ba9f1`
  (= `55e4ed62..c690ecf8`, pre-rebase), `backup/peakhours-exact-preMR`, and the
  master-visible `86467b49`-based series `ecc0d505` (the fix), `4b09f114` (root cause),
  `91a5b240` (restack note) — superseded by the rebase, preserved here for the record.
- **Recovery**: `git -C /home/user/work/GoPi log --oneline fix/peakhours-exact-boundary` /
  `git -C /home/user/work/GoPi show fix/peakhours-exact-boundary:devdoc/peakhours-exact-boundary.md`.
- **Prior path kept**: `backup/peakhours-exact-on-ec6ba9f1` (= the pre-rebase commits
  `55e4ed62..c690ecf8`, stacked on the feature tip).
- The commits are in the **main repo** (a linked worktree shares the object store —
  no push needed, Rule 6a/6c).
- **Not merged** by me; PA merges after acceptance (Rule 6e).
