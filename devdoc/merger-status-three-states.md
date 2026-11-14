<!-- doc-class: historical -->
# Task Report: `gopi merger --status` separates the three states it used to collapse (audit gap G3)

Epic: *`gopi merger --status` collapses three distinct states into one misleading
line* — audit gap **G3**, session `01a0b1c5`, measured twice 2026-09-18.
Branch **`fix/merger-status-three-states`**, worktree
`/home/user/workspace/gopi-merger-status-states` — **reclaimed externally
mid-run**; the branch was re-checked-out into `/home/user/workspace/gopi-mst-final`
and every gate below was re-observed there (see "The external reclaim" and
"The re-observation after the reclaim") — base `9ce8e159` (master).

Recovery:
`git show fix/merger-status-three-states:devdoc/merger-status-three-states.md`.

## Completed

1. **The render no longer prints a machine token bare.** `MergerStatus.Render`'s
   headline is now `mergerStateHeadline()` — **one SENTENCE per state**, carrying
   the measured numbers (the pid). The state switch had
   `case st.HolderPID > 0: st.State = StatusNotRunning` (the `(a)`/`(b)` collapse)
   and `Render` printed `merger:      <s.State>`, so the render was the defect,
   not the probe.

2. **A fifth state token, `StatusHolderGone` ("holder gone")** — a stamped pid
   whose process is gone. The three states are now three sentences:

   | state | machine | rendered headline |
   |---|---|---|
   | `StatusNotRunning` | (a) nothing ever started | `no merger is running — no lock file and no lock holder: nothing was ever started in this profile` |
   | `StatusHolderGone` | (b) a stale lock | `a merger is NOT running — the last one exited and left pid N stamped in the lock file; the lock is FREE (the next tick is safe to start)` |
   | `StatusAlive` | (d) a live holder | `a merger IS running (pid N holds the lock)` |

   The token deliberately avoids the word *stale*: "stale" is the **lock
   holder's** vocabulary for a different measurement (`LockHolderNote` renders it
   on the holder line, the progress edge inside the take-over line) — re-using
   one word for two measurements is how this surface got here.

3. **State (c) — a live process whose progress record is stale — is now a
   report-level sentence.** Before: `takeoverProgressStaleEdge` (15m) lived only
   inside a parenthetical on the take-over line, so a report whose headline said
   "a merger IS running" never mentioned that its round had stopped moving. Now
   `MergerTakeover.staleProgress()` requires **all three** conditions (live +
   attached record + age ≥ edge) and `Render` prints
   `mergerTakeoverStaleProgressFormat` under the take-over block, naming the pid,
   the age and the edge — and withholding the verdict ("the process is ALIVE and
   a live round can be legitimately quiet, so this is a suspicion, never a
   verdict … DO NOT kill it on this line alone"). All three conditions is what
   keeps a zombie, a malformed record and a not-yet-written record out of it: a
   dead process's record is stale by definition, and calling that "live but
   quiet" is the same class of lie as calling a zombie running.

4. **Every magic string is a named constant** (Rule 1b/13/14):
   `MergerStateLiveMarker`, `MergerStateNoneNote`, `MergerStateStaleLockMarker`,
   `mergerStateStaleLockStrayMarker`, `mergerStateLiveFormat`,
   `mergerStateStaleLockFormat`, `mergerStateStaleLockOrphanFormat`,
   `mergerStateHolderGoneNoStampNote`, `mergerStatePreambleMarker`,
   `mergerStatePreambleHeaderFormat`, `mergerStateLetterIsTokenFormat`,
   `mergerStateLetterSentenceFormat`, `takeoverStaleProgressMarker`,
   `mergerTakeoverStaleProgressFormat`. The operator-facing contract strings are
   **exported** so the CLI faces and their E2E tests reference one definition
   instead of mirroring a phrase.

5. **A state the render does not know is VISIBLE, not plausible** (Rule 15): the
   `default:` arm prints the letter *plus* "⚠ this line printed a state letter,
   not a sentence: the render has no headline for state %q yet (Rule 15: a new
   state must state what it means)". `StatusTakeoverUnlocked`/`StatusOrphanLocked`
   are already sentences and keep their letter, named explicitly.

6. **`StateHasLiveMerger()` — ONE predicate for "will anything act by itself".**
   `dispositionMergeNote` was keyed on `state == StatusNotRunning`, so splitting
   the dead states in two would have **silently dropped** the "none of the
   remedies below will run by itself" note for every stale-lock profile — the
   shape where an operator most needs it. Unknown states read as *not live*
   (fail-closed, because the sentence it guards tells an operator nothing will
   run on its own).

7. **`TestMergerStatusDoesNotCallAZombieRunning` repaired at its root cause**
   (see "The zombie assertion" below).

## The zombie assertion — decided with evidence

**Question the briefing posed**: the assertion is RED at base; is the render or
the assertion wrong?

**Decision: the ASSERTION was wrong. The render was never wrong.**

- The old assertion pinned an **adjacency**: `strings.Contains(out, "pid 1344 " +
  MergerTakeoverStateExited)`, i.e. the state word must sit *directly after* the
  pid. Epic `merger-takeover-identity-stats` grew the identity phrase to
  `pid 1344+starttime …+cmdline …`, which legitimately inserts text between
  them — so the assertion went red on a **correct** render. The adjacency was
  never the fact under test.
- The fact is: **the ENTRY the render prints for that pid carries its MEASURED
  state and never the running word.** The repaired assertion locates the entry
  line *by its label* (`renderEntryLineFor(out, "pid 1344")`, scanning
  `mergerTakeoverLineLabel` lines) and asserts it contains
  `MergerTakeoverStateExited` and NOT `MergerTakeoverStateRunning` — by
  constants, not re-typed words. The label is part of the lookup on purpose: the
  same pid also appears on the `lock holder:` line, so a plain line search
  returns the wrong line.
- It also gained the **state-(c) negative control**: the report-level
  "live but its progress has not moved" sentence must not fire for a zombie.

Note on "RED at base": `git merge-base --is-ancestor 84df4f02 HEAD` is true —
`84df4f02 test(merger-status): assert the zombie's pid and state separately`
(landed 07:19) is an ancestor of my base `9ce8e159` (landed 12:42), so on **my**
base the test already passes with the adjacency replaced by two separate
`Contains`. What survives of the audit's red is the *shape* the earlier repair
left: a per-render assertion that cannot express "this ENTRY carries this state",
and no negative control on (c). This branch lands the per-entry form with the
red-then-green record; the pre-existing `84df4f02` repair is acknowledged, not
re-claimed.

## Files

- `internal/tools/merger_status.go` — `StatusHolderGone`; the operator-text
  constant block (item 4); `mergerStateHeadline()`; `stateNote()` (the
  leftover-memory paragraph, printed **only** for `StatusHolderGone`); state (c)'s
  `staleProgress()` + its render.
- `internal/tools/merger_status_disposition.go` — `StateHasLiveMerger()` +
  `dispositionMergeNote` routed through it.
- `internal/tools/merger_status_headline_test.go` **(new)** — 8 regression
  guards (a≠b, no bare token incl. an unknown state, (b)+stray-holder, live names
  its pid, (c) is its own sentence, (c)'s 7-case negative-control table, the
  dead-states predicate).
- `internal/tools/merger_takeover_state_test.go` — the zombie assertion +
  `renderEntryLineFor`.
- `internal/tools/merger_status_test.go` — the stamped-dead fixture is (b) now,
  asserted by marker + pid; and (a) must not print (b)'s marker.
- `internal/tools/merger_status_inherited_flock_test.go`,
  `merger_takeover_live_test.go` — assertions moved from the bare token to the
  reading / the exported note.
- `cmd/agent/merger_status_cli_test.go` — the read-only faces assert the (a)
  marker **and** that no `lock holder: pid` line appears in a profile where
  nothing was ever started.
- `cmd/agent/merger_loop_e2e_test.go` — the live loop is asserted **positively**
  (`tools.MergerStateLiveMarker` + the holding pid from the lock file) instead of
  by the absence of the retired word.

## Verification

All commands below were first run in the worktree
`/home/user/workspace/gopi-merger-status-states`; after that worktree was
reclaimed externally (next section), the branch was re-checked-out into
`/home/user/workspace/gopi-mst-final` and the whole gate set was **re-observed**
there — see "The re-observation after the reclaim" for the run that is current.

| gate | command | observed |
|---|---|---|
| build (whole tree) | `go build ./...` | exit 0 |
| vet (whole tree) | `go vet ./...` | exit 0, no output |
| standards gates | `go test ./internal/standards/ -count=1` | `ok github.com/nick/gopi/internal/standards 11.212s` |
| branch-scoped tools tests | `go test ./internal/tools/ -count=1 -run 'Merger\|Status\|Takeover\|Zombie\|Lock'` | `ok github.com/nick/gopi/internal/tools 183.884s` |
| the 8 new guards, named | `go test ./internal/tools/ -run 'TestMergerStatusHeadline…\|…StaleProgress…\|…DeadStates…\|TestMergerStatusDoesNotCallAZombieRunning' -v` | 8/8 `--- PASS`, `ok … 0.624s` |
| zombie assertion | `go test ./internal/tools/ -run TestMergerStatusDoesNotCallAZombieRunning -v` | `--- PASS: TestMergerStatusDoesNotCallAZombieRunning (0.00s)` |
| cmd/agent merger subset | `go test ./cmd/agent/ -count=1 -run 'MergerStatus\|MergerLoop\|MergerStop\|MergerDaemon\|MergerFlock\|MergerRestart'` | **1 FAIL** — `TestMergerRestartDoesNotTouchTheTakeoverInFlight (95.32s)`, RED at base too; every other case in the subset green (`FAIL … 287.591s`). Re-run on the re-added stable worktree `/home/user/workspace/gopi-mst-final` at the branch tip `490690d3` — same single failure |
| the SAME restart test at BASE | `go test ./cmd/agent/ -run TestMergerRestart` in a detached base worktree (`gopi-mst-base`, `9ce8e159`) | **2 FAIL**, the same two tests (`118.747s`) — see "Not Done" |
| worktree clean | `git status --porcelain` | empty after the commit |
| commit | `git commit` | `e2d4f5cc` + `da1234d2` + `490690d3` on `fix/merger-status-three-states` |

### The external reclaim, disclosed (it explains the whole-package numbers)

**This run's worktree was DELETED mid-test-run** by a reclaim pass belonging to
another session, not by this diff — `/home/user/workspace/gopi-merger-status-states`
stopped existing between two commands, and the whole-package `./cmd/agent/` sweep
that was executing at that instant produced a **cascade** that is entirely
`getwd: no such file or directory` / `open …: no such file or directory`:
**80** occurrences, e.g.

```
--- FAIL: TestTaskRunSeamFallsBackToSkeleton (0.00s)
    cli_tasks_report_seam_test.go:177: resolve repo root: getwd: no such file or directory
--- FAIL: TestMergerLoopHoldsTheLockForItsWholeLife (0.00s)
    merger_loop_e2e_test.go:119: repo-context sandbox: read the starting cwd: getwd: no such file or directory
--- FAIL: TestMergerTickSpawnsButNeverWaits (0.00s)
    merger_tick_no_child_gate_test.go:362: read ../../internal/tools/repo_sweep.go: open ../../internal/tools/repo_sweep.go: no such file or directory — the gate cannot check a file it cannot read
```

`TestMergerLoopHoldsTheLockForItsWholeLife` — the test that exercises this
epic's live-loop render — is one of those cascade victims (`0.00s`, cwd gone),
so **that run carries no verdict about this diff**. The branch was intact and
the report recovered from the branch (see Recovery above); the worktree was
re-added and the scoped suite re-run from the stable tree, whose results are the
ones tabulated above.

Note this is the same class of hazard Rule 29/Rule 30 describe from the other
side: the deliverable survived because it was **committed** (the dirty-worktree
refusal exists for exactly this), which is why the report is recoverable at all.

### Evidence (self-filed)

- `go build ./...` → `exit=0` (no output). `go vet ./...` → `exit=0` (`VET_ALL_EXIT=0`).
- `go test ./internal/standards/ -count=1` → `ok github.com/nick/gopi/internal/standards 11.212s`.
- `go test ./internal/tools/ -count=1 -run 'Merger|Status|Takeover|Zombie|Lock'` →
  `ok github.com/nick/gopi/internal/tools 183.884s`.
- The 8 named guards, `-v` → `--- PASS` ×8 (`… 0.624s`), including
  `TestMergerStatusStaleProgressNeedsAllThreeConditions` with its 7 sub-cases.
- `git status --porcelain` after the commit → **empty** (the `(porcelain end)`
  line above it is the whole output).
- Base-vs-fixed first lines: `diff -q` computed by `redgreen.sh` → `IDENTICAL`
  (base) / `DIFFERENTLY` (fixed).
- Live loop: `merger: a merger IS running (pid 1593269 holds the lock)` +
  `interval: 6s (measured from the log's last sleep line)` +
  `next tick: 15:15:17 (estimated: in 5s — the last sleep began at 15:15:11 +
  interval 6s)`; lock file after a status query still `7` bytes `999999`.
- Zombie red-then-green: mutation A → `FAIL … 0.852s` naming the running state on
  the entry line; historical assertion B → `FAIL … 0.854s`; unmutated → `ok`.
- Base restart E2E RED: `--- FAIL: TestMergerRestartReplacesTheLoopWithoutOverlap
  (96.56s)` + `--- FAIL: TestMergerRestartDoesNotTouchTheTakeoverInFlight
  (15.84s)`, `FAIL github.com/nick/gopi/cmd/agent 118.747s`.

### The re-observation after the reclaim (`gopi-mst-final`, branch tip)

Every gate below was **re-run from scratch** on the re-added worktree, because
the first run's numbers died with the reclaimed directory (a result the run did
not observe is not a reportable result, Rule 5):

| gate | command | observed |
|---|---|---|
| build | `go build ./...` | `BUILD OK` |
| vet | `go vet ./...` | `VET OK` |
| merger-scoped tools | `go test ./internal/tools/ -count=1 -run 'MergerStatus\|MergerTakeover\|Zombie\|Headline\|StaleLock\|HolderGone'` | `ok github.com/nick/gopi/internal/tools 33.489s` |
| the 8 guards, `-v` | same selection, verbose | 8/8 `--- PASS` (incl. all 7 sub-cases of the all-three-conditions table), `ok … 0.581s` |
| **operator-level e2e, re-run at the branch tip** | `bash e2e.sh` against a binary built from `e48f6f76` | three states, three sentences — quoted below |
| **red-then-green, re-run at the branch tip** | `bash zombie-redgreen2.sh` | baseline `ok … 0.542s` → mutation A `FAIL … 1.555s` → B `FAIL … 0.399s` → unmutated `ok … 0.806s` |

The live-loop e2e re-observed at the tip, whole run quoted in
`~/workspace/_tmp/mst-e2e/e2e-resume.log`:

```
### STEP 1 — status with NOTHING started (state a: no lock file at all)
merger:      no merger is running — no lock file and no lock holder: nothing was ever started in this profile
...
### STEP 2 — state (b): a stale lock (stamped pid, that process is gone)
merger:      a merger is NOT running — the last one exited and left pid 999999 stamped in the lock file; the lock is FREE (the next tick is safe to start)
lock holder: pid 999999  (stale: the pid named here is gone; the lock is FREE)

(headline reading: a merger ran in this profile and its process is gone (the lock file still carries pid 999999); everything below is the state it LEFT BEHIND —)
--- the lock file after a status query (must be UNTOUCHED: read-only, no new pid stamped):
-rw-rw-r-- 1 nick nick 7 Sep 18 16:10 …/serve.lock
999999
### STEP 3 — a LIVE loop: gopi merger --every 6s
--- status while the loop is LIVE (state d):
merger:      a merger IS running (pid 4094965 holds the lock)
lock holder: pid 4094965
interval:    6s (from the live holder's --every)
last tick:   16:10:01 (running now — 2s so far; the tick has not logged its sleep line, so its end is not measurable)
next tick:   unknown (the tick the log's last boundary introduced is still running — its end is not logged yet)
--- a SECOND merger is refused:
gopi merger: serve lock held by a live daemon: … (lock … held by pid 4094965) — refusing double-serve…
### STEP 4 — SIGTERM the loop, then read the state it leaves (b)
merger:      a merger is NOT running — the last one exited and left pid 4094965 stamped in the lock file; …
```

Three readings, three distinct sentences, and the `(a)`/`(b)` first lines are
**not** byte-identical (that byte-identity was the G3 defect). The read-only
contract is re-observed on the same run: after the status query the lock file is
still `7` bytes holding `999999` — untouched, no pid re-stamped.

At the branch tip the red-then-green re-ran too: mutation A plants the defect in
production (`Line()` prints `MergerTakeoverStateRunning` for every take-over) and
the repaired assertion goes **RED** naming the entry line —

```
    merger_takeover_state_test.go:141: the entry for pid 1344 carries the running state "running" — a terminated take-over must never be rendered as a merge round in flight:
        takeover:    pid 1344+starttime UNREADABLE …+cmdline UNREADABLE … running for 4h41m0s; …
FAIL	github.com/nick/gopi/internal/tools	1.555s
```

— i.e. the assertion is **not vacuous**: it still catches the defect it exists
for, on the wording the identity-phrase epic left behind.

### The red-then-green records (two mutations, both run)

`zombie-redgreen.sh` (kept in the session scratch, `~/workspace/_tmp/mst-e2e/`),
three phases — **not** a test, a by-hand observation:

- **0. baseline, unmutated** → `ok github.com/nick/gopi/internal/tools 0.767s`.
- **A. the DEFECT planted in production** (`merger_takeover_live.go`'s `Line()`
  prints `MergerTakeoverStateRunning` for every take-over) → the repaired
  assertion goes **RED**:
  `merger_takeover_state_test.go:141: the entry for pid 1344 carries the running
  state "running" — a terminated take-over must never be rendered as a merge
  round in flight` → `FAIL`. **The assertion still catches the defect** (it is
  not vacuous).
- **B. the HISTORICAL adjacency assertion run against the current, correct
  render** → **RED** (`FAIL github.com/nick/gopi/internal/tools 0.854s`), i.e.
  the audit's red reproduced: the old assertion was about **word order**, not
  about the fact.
- **1. final, unmutated again** → `ok … 0.833s`.

`redgreen.sh` — the **operator-level** base-vs-fixed binary record, two separate
hermetic `GOPI_HOME`s, the same two queries against each binary:

```
######## base binary
---- (a) nothing ever started:
merger:      not running
---- (b) stale lock (pid 999999, its process is gone):
merger:      not running
==== VERDICT: (a) and (b) render IDENTICALLY — the G3 collapse
######## fixed binary
---- (a) nothing ever started:
merger:      no merger is running — no lock file and no lock holder: nothing was ever started in this profile
---- (b) stale lock:
merger:      a merger is NOT running — the last one exited and left pid 999999 stamped in the lock file; the lock is FREE (the next tick is safe to start)
==== VERDICT: (a) and (b) render DIFFERENTLY
```

`diff -q` on the two `(a)`/`(b)` headline files is what the base verdict is
computed from — the base binary's two first lines are byte-identical.

### The live-loop proof (state (d), a real `gopi merger --every 6s`)

Hermetic home (private `GOPI_HOME` **and** `GOPI_SETTINGS` — `GOPI_HOME` alone
does not isolate the repo sweep, because `ResolveMergerRepos` falls back to the
real `~/.gopi/settings.json`; the fixture's settings carry an empty `repos`
table). No signal ever sent to the operator's live merger or workers.

```
### STEP 3 — a LIVE loop: gopi merger --every 6s
loop pid 1584106
--- loop startup output:
[merger] gopi merger: running the tick every 6s until signalled (Ctrl-C or SIGTERM); the single-instance lock is held for this loop's whole life
[merger] tick start 15:14:55
--- status while the loop is LIVE (state d):
merger:      a merger IS running (pid 1584106 holds the lock)
lock file:   …/run-DkJY7f/home/.gopi/serve.lock
lock holder: pid 1584106
interval:    6s (from the live holder's --every)
last tick:   15:14:55 (running now — 2s so far; …)
--- a SECOND merger is refused:
gopi merger: serve lock held by a live daemon: another merge process is already running (lock … held by pid 1584106) — refusing double-serve…
--- lock file after the refusal (belongs to the loop, not the refused one):
1584106
### STEP 4 — SIGTERM the loop, then read the state it leaves (b)
merger:      a merger is NOT running — the last one exited and left pid 1584106 stamped in the lock file; …
```

A second run, waited to the **sleep edge** so `next tick` is actually projected:

```
--- loop output at the sleep edge:
[merger] sleeping 6s from 15:15:11
### --status in the sleep window:
merger:      a merger IS running (pid 1593269 holds the lock)
lock holder: pid 1593269
interval:    6s (measured from the log's last sleep line)
last tick:   15:15:07 (5s ago — the last tick's work took 4s before it slept)
next tick:   15:15:17 (estimated: in 5s — the last sleep began at 15:15:11 + interval 6s)
```

Read-only contract re-observed: the lock file after a status query was
`-rw-rw-r-- … 7 bytes, 999999` — **untouched**, no pid re-stamped, `serve.lock`
still absent in the never-started profile (asserted in
`TestMergerStatus_FacesAreReadOnly`).

## Not Done (honest)

- **The restart E2E is RED at base — proven, and therefore excluded per the
  briefing's clause.** `TestMergerRestart*` fails on a **clean detached worktree
  of the branch point `9ce8e159`**, with this diff nowhere near it
  (`git diff 9ce8e159 -- cmd/agent/merger_restart_e2e_test.go internal/tools/merger_restart*`
  is empty — my diff touches neither the test nor any restart source):

  ```
  # base 9ce8e159 (worktree /home/user/workspace/gopi-mst-base), `go test ./cmd/agent/ -run TestMergerRestart`:
  --- FAIL: TestMergerRestartReplacesTheLoopWithoutOverlap (96.56s)
      merger_restart_e2e_test.go:464: the restart never installed a fresh holder (holder 1941747, first 1941747)
  --- FAIL: TestMergerRestartDoesNotTouchTheTakeoverInFlight (15.84s)
      merger_restart_e2e_test.go:731: the in-flight take-over (pid 2015557) was KILLED by the restart …
  FAIL	github.com/nick/gopi/cmd/agent	118.747s
  ```

  Both failure texts are the **stop face's own "did not reach a tick boundary in
  1m0s" timeout** (`pid N still holds …/serve.lock after 1m0s — the loop has not
  reached a tick boundary yet`), i.e. a 30s-interval loop on a loaded host — the
  same root cause the briefing names, measured on the base, not inferred.
  On this branch the subset run fails **fewer** of them
  (`TestMergerRestartDoesNotTouchTheTakeoverInFlight` alone), so the difference is
  timing, not the diff.

  **Root cause is the same class as the known `01a0aba7` budget row**: every
  fixture's first ledger open pays the host's device journal commits
  (`testutil: … the ledger sandbox … is on block-backed filesystem type 0xef53
  (device-backed)` — printed by the runner on every suite entry), so a 30s-tick
  loop plus a 60s stop budget does not fit under contention. Not fixed here — it
  is a test-fixture cost, not a merger-status render defect, and the briefing
  allows the exclusion when the root cause is stated with evidence.
- **The `internal/tools` whole-package sweep exceeds Go's 600s package budget at
  BASE too** (measured earlier in this session: a goroutine dump inside an
  unrelated test, `TestTaskRunWritebackTagsExplicitAndEmpty`) — a loaded-host
  wall-clock issue, not a merge-status defect, and consistent with the finding
  already recorded in `devdoc/merger-status-operator-text.md`.
- **The pre-existing zombie repair `84df4f02`** is an ancestor of this base; see
  "The zombie assertion" for what this branch adds beyond it.

## Why (the reasoning a future agent would otherwise re-derive)

The surface had **one word for two machines** and **no sentence for a third**.
A state token is the *machine* vocabulary — callers, scripts and log greps
compare it — while the headline is what an operator *reads*; separating them is
the fix, and it makes the render mechanically assertable: every state's first
line now differs byte-wise from every other's. The second half is the one that
bites later: any sibling that compared the old token (`state ==
StatusNotRunning`) silently stops covering half its cases when a state is split,
so the derived readings were routed through one predicate (`StateHasLiveMerger`)
rather than re-spelling the live-state set at each call site.
