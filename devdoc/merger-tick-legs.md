<!-- doc-class: historical -->
# Task Report: the merger's serial face legs — `--once`-only legs restored to the launching face

Branch: `fix/merger-tick-legs` (worktree `/home/user/workspace/gopi-merger-tick-legs`,
based on master `9ce8e159`; the briefing named `8b0011a3`, which master had
already advanced past — see "Not Done (honest)" for why that did not invalidate
the diagnosis).

## Completed

- **A shared definition of the pass's NON-TICK legs** —
  `mergerSerialFaceLegs(opts)` (`cmd/agent/cli_tasks.go`): the durable-mark
  drain (`mergerPassCloseLoop(nil)`), the dead-holder ladder
  (`mergerResumeDeadHolderLeg` then `mergeDeadHolderReclaimLeg`, in that order),
  and the two cross-node sweeps (`serveAdoptionSweep`, `serveGitSyncSweep`).
  One definition, reached by BOTH faces (Rule 14) — `mergerPassOnce` calls it,
  so the `--once` face is byte-for-byte the same sequence it had before.
- **The launching face runs them** — `mergerLaunchSurvivingLegs(opts)`
  (`cmd/agent/merger_tick.go`), called from the tick's cron/`--every` branch
  ABOVE the pre-spawn gate (a tick that skips its launch still repairs — and
  that window is exactly when a dead holder's worktree is parked). Bounded by
  `mergerTickNarrowTimeout` (2m, named constant, Rule 1) and wrapped in a
  `recover()` so a defect in a leg can never cost the take-over (Rule 15b: the
  panic is recorded, not swallowed).
- **No merge judgement was added to Go**: the per-repo SWEEP stays off both the
  helper and the launching branch (asserted), so "which branch lands" is still
  the take-over prompt's call. The helper is judgement-free by construction:
  a drain (rebuilds what a landing already did), a ladder (judges a HOLDER from
  the ledger + `/proc`), and two operator-declared peer moves.
- **The take-over prompt was NOT changed**, so
  `cmd/agent/merger_takeover_prompt_gate_test.go` needed no extension and still
  passes (no prompt edit was the point: the legs run, the obligation is not
  merely stated).
- **Pinning tests** (`cmd/agent/merger_tick_legs_test.go`):
  - `TestMergerTickRunsTheDeadHolderLadder` — the ARRIVAL half, on the
    PRODUCTION face: a real bare `gopi merger` as an isolated process over a
    dead-holder fixture. Asserts the serial legs' own arrival (the adoption
    sweep's report line, reachable only from the helper), the reclamation LINE
    (which names the freed branch) and the EFFECT (the worktree is gone), and
    that a take-over is still started.
  - `TestMergerLaunchingFaceRunsTheSerialLegsButStillDoesNotSweep` — the WIRING
    half: the launching branch calls the legs, calls them BEFORE the launch,
    calls them ABOVE the pre-spawn gate, and still reaches neither
    `mergerPassOnce` nor `mergerSweepRepo`.
  - `TestMergerSerialLegsAreOneDefinitionReachedByBothFaces` — the SHAPE half:
    one definition, both faces, the ladder order stated exactly once, the
    bound + recover on the launching face.
- **devdoc** `devdoc/merger-tick-legs.md` (intent evidence per leg, the fix, the
  measurements, and the ordering finding below).
- **A self-inflicted regression found by the branch-scoped smoke, and fixed**
  (`cmd/agent/cli_tasks_rebuild_*_test.go`): the two pass-START ordering gates
  (`TestCloseLoopWiringIsComplete`, `TestPassOnceDrainsMarksAtPassStart`) read
  `mergerPassOnce`'s BODY for the literal `mergerPassCloseLoop(nil)`. Once the
  drain's text moved into the shared `mergerSerialFaceLegs` definition (Rule 14)
  and the pass reached it by CALL, both gates read the drain as MISSING and went
  red — the shape change they were blind to, not a lost obligation. Fixed with
  `passStartDrain` (see the Verification section): the gates now ask for the
  pass's OFFSET at the drain — the call site directly, or the call into the
  shared definition whose body carries it — so the requirement stays stated once
  and the gates stay red on a genuine removal.

## The intent finding (deliverable 1), with citations

The real deployment form, quoted from the record: the merger ships a resident
face — "`gopi merger` with no arguments … the cron form … each tick acquires the
single-instance lock, runs ONE tick, and exits" plus "`--every D` … : a merger
that sleeps D between ticks" (`devdoc/merger-pure-cronjob.md`, the pure-cronjob
epic). `runTaskMergerWithLock`'s LAUNCH branch (`cmd/agent/merger_tick.go`) is
where both of those faces end, and `mergerPassOnce` was described as reachable
from `--once` only. Per leg:

| leg | intended carrier | evidence |
|---|---|---|
| declared-rebuild drain (`mergerPassCloseLoop(nil)`) | any face — the mark is "the pass's memory across process lifetimes"; "a rebuild interrupted with its lander is recovered by the successor" (`cli_tasks_rebuild.go`) | `internal/tools/rebuild_pending.go` header; the drain is the pass's FIRST act, placed there so "a pass that dies a second later has already healed a stale artifact" (`cli_tasks.go`); `repo_sweep.go:1107` — "the next pass's START drains it" |
| dead-holder RESUME | "a RESIDENT process must perform [the release], because the branch whose dead holder still occupies a worktree is exactly the branch the sweep skips as PROTECTED" (2026-09-13 fix, `cli_tasks.go`) — the resident process is the merger | `devdoc/merge-resolve-nested-report-path-clobber.md:39` names `TestMergerPassCallsResumeArmBeforeReclaim` (a test that was never landed — see Not Done); `cli_tasks.go:2689` cites it as the order's assertion |
| dead-holder RECLAIM | same citation — "the merger is the component a task host actually runs" | `mergeDeadHolderReclaimLeg`'s own doc: "a named seam … the regression it guards is precisely 'the merger never invoked the arm'" |
| adoption + git-sync sweeps | not a deployment question: they are already called on EVERY face (measured: the eager `serveAdoptionSweep(repo,opts.adoptionEvery)` call in the tick prologue runs on both faces) | `cli_tasks_adopt.go`/`cli_tasks_sync_git.go`; the run log of the fixture (scanned=1 on a bare tick) |

**Measured correction to the epic's premise (one point).** The epic listed five
legs as `--once`-only. The evidence shows the two cross-node sweeps are NOT
`--once`-only (the tick prologue already runs the eager adoption sweep). The
genuinely `--once`-only duties were the drain and the dead-holder ladder, and
those are what this change restores.

## Not Done (honest)

1. **The resume arm's arrival is NOT asserted as a live line, and here is the
   measured reason.** The resident task-lifecycle tick — which runs on EVERY
   face, in the tick prologue and before the serial legs — carries the
   finish-cleanup chain, and that chain FREES the worktree of exactly the rows
   the resume arm would continue. Measured on the branch: a bare tick reclaimed
   the fixture's dead holder (its own line: `dead-holder worktree reclaimed
   [dead local holder in_progress …] feat/merger-tick-legs-fixture … freed for
   merge; branch kept`) and the resume arm then reported `holder liveness
   unreadable on this host` — its candidate was already gone. So on any face
   this host runs, the arm's ground is reclaimed before it can act. That is a
   property of two epics (2026-09-13's reclaim, 2026-09-16's resume), NOT of
   this face, so it is recorded in the devdoc and outside this branch's scope;
   the ladder's own arrival is pinned by the helper's arrival plus the static
   half (the arm is in the one definition both faces reach).
2. **The `--first-open` / durable-mark half of the drain is pinned statically
   only.** Producing a pending mark needs a repo whose `.gopi/sync-state.json`
   points at a binary that truthfully reports an older revision; the fixtures
   that do that (`tipBinBuild`) build real Go binaries (tens of seconds, per
   repo), which is not a merge/verify-path cost. The ARRIVAL of the drain on
   the launching face is therefore asserted structurally (it is inside the one
   definition the launching face calls), not by a live mark.
3. **A pre-existing red test remains red, reproduced on pristine master**: with
   the two named base failures, `TestMergerRestartDoesNotTouchTheTakeoverInFlight`
   was reproduced red on a pristine `8b0011a3` worktree BEFORE any branch work
   (output: `--restart exited 1 … pid … still holds … serve.lock after 1m0s`);
   `TestMergerStatusDoesNotCallAZombieRunning` was not reproduced — see the
   Verification section for what was actually observed.
4. **The briefing's `TestMergerPassCallsResumeArmBeforeReclaim` does not exist**
   anywhere in the tree (only two prose references to it: `cli_tasks.go:2689`
   and `devdoc/merge-resolve-nested-report-path-clobber.md:39`). This branch
   supplies the real order assertion instead
   (`TestMergerSerialLegsAreOneDefinitionReachedByBothFaces`), and the stale
   prose references are left alone (a doc correction is its own change).

## Verification

- Build: `go build ./...` → BUILD_OK (branch).
- Vet: `go vet ./...` → clean (pristine `8b0011a3`) and `go vet ./cmd/agent/` →
  clean (branch).
- Pristine baseline (own worktree at `8b0011a3`, no branch work): `go build ./...`
  exit 0, `go vet ./...` exit 0, and
  `go test -count=1 -run 'TestMergerRestartDoesNotTouchTheTakeoverInFlight|TestMergerStatusDoesNotCallAZombieRunning' ./cmd/agent/`
  → `--- FAIL: TestMergerRestartDoesNotTouchTheTakeoverInFlight (65.91s)` with
  `--restart exited 1 … still holds …/.gopi/serve.lock after 1m0s`; the
  `TestMergerStatusDoesNotCallAZombieRunning` case did not print a FAIL line in
  that run (it was not reproduced as failing there — reported as observed, not
  as assumed).
- Red-proof of the new tests on PRISTINE `9ce8e159` (base worktree + only the new
  test file copied in):
  - `TestMergerLaunchingFaceRunsTheSerialLegsButStillDoesNotSweep` →
    `FAIL … the launching branch does not reach "mergerLaunchSurvivingLegs(opts)"`.
  - `TestMergerSerialLegsAreOneDefinitionReachedByBothFaces` → `FAIL … does not
    call/define "mergerLaunchSurvivingLegs(opts)" … func mergerPassOnce( does not
    call "mergerSerialFaceLegs(opts)" … declaration "func mergerSerialFaceLegs("
    not found`.
  - `TestMergerTickRunsTheDeadHolderLadder` → `FAIL … a bare … merger tick never
    ran the cross-node adoption sweep ("[serve] [adopt] scanned=" absent)`.
- Green on the branch:
  `go test -count=1 -run 'TestMergerTickRunsTheDeadHolderLadder|TestMergerLaunchingFace|TestMergerSerialLegs' ./cmd/agent/`
  → `ok  github.com/nick/gopi/cmd/agent  8.213s`.
- Prompt gate: `merger_takeover_prompt_gate_test.go` unchanged and passing (the
  prompt was deliberately not edited — the legs run instead of being promised).
- Branch-scoped smoke (the merge/verify path's own command, Rule 27):
  `GOPI_HEAVY_SKIP_LEDGER=… scripts/test-budget.sh ./cmd/agent/` in the branch
  worktree, budget 25m per package →
  `stamp: 2026-09-18T15:46:41+08:00 host=peerbox pkg=./cmd/agent/ budget=25m
  elapsed=1120s load_start=8.73 9.08 8.99 load_end=8.74 8.13 7.92 cores=4 exit=1
  outcome=fail stream=run-1789717601.json`, with the run's own classification
  line: `--- failures (assertion/build; the budget did NOT trip) ---`. TEN
  failures, EVERY ONE measured as pre-existing on pristine `9ce8e159` — see the
  triage below. The three new tests and the two touched drain gates are green in
  this same run.
- The earlier `go test ./cmd/agent/ ./internal/tools/` run (unbudgeted) is NOT
  cited as evidence: it hit Go's 10m default on BOTH packages while other agents'
  suites ran on this 4-core host, which `scripts/test-budget.sh` exists precisely
  to reclassify (its own header: "the same tree that finishes internal/tools in
  ~189-291 s on a quiet box takes >600 s when other agents' suites are running").
  Its `FAIL … 601.246s` lines name a TIMEOUT, and `internal/tools` ended in a
  `panic: test timed out` goroutine dump rather than an assertion.
- **Failure triage — all ten reproduced on pristine `9ce8e159`**, so none is
  this branch's:
  - `TestServiceHelpIsCapabilityFirst`, `TestSpawnersPinTheRepoContext`,
    `TestTopLevelUsageDelegatesEnvVarsToHelpEnv`, `TestTopLevelUsageShowsTaskLedger`
    → identical FAIL set on the pristine worktree
    (`cd ~/workspace/gopi-base-tick-legs2 && go test -count=1 -run '<the four>'
    ./cmd/agent/` → `--- FAIL` ×4, `FAIL github.com/nick/gopi/cmd/agent 1.147s`);
    the messages name base text/help surfaces this branch never touches
    (`mergerUsageHead: carries process-mechanics vocabulary … (setsid)`;
    `repo-context gate: … merger_restart_e2e_test.go:252 mreStartLoop()`).
  - `TestMergerStopEndsALiveLoop`, `TestMergerRestartReplacesTheLoopWithoutOverlap`,
    `TestMergerRestartDoesNotTouchTheTakeoverInFlight` → run side by side on the
    two worktrees, same selection:
    pristine `FAIL … 61.85s / 90.92s / 63.83s` (`FAIL … 217.133s`), branch
    `FAIL … 64.64s / 90.89s / 63.81s` (`FAIL … 219.950s`).
  - `TestTaskListReadPathE2E`, `TestSweepZombiesCLI_DryRunTouchesNoNotifySink`,
    `TestSweepZombiesCLI_ReportNotifiesOperator` → these three demand a binary the
    worktree does not carry yet by design ("sweep-zombies E2E needs a gopi binary:
    set GOPI_BIN to THIS tree's build … a skip here would report a green
    regression guard that never ran"); identical on the pristine worktree. With
    `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` + `GOPI_BIN=…` they PASS on
    the branch: `ok github.com/nick/gopi/cmd/agent 11.004s`.
- Commit: the code commit is `128019af`, the gate-fix commit `d8c50970`, the
  devdoc commit `0afd9a69` (`fix/merger-tick-legs`, 3 ahead of `9ce8e159`).

## Evidence (self-filed)

- Pristine build/vet: `cd ~/workspace/gopi-base-tick-legs && go build ./...` →
  `BUILD_EXIT=0`; `go vet ./...` → `VET_EXIT=0` (job-1 output file, captured
  before any branch work).
- Baseline red: `--- FAIL: TestMergerRestartDoesNotTouchTheTakeoverInFlight
  (65.91s)` / `FAIL github.com/nick/gopi/cmd/agent 66.884s` — job-1 stdout,
  HEAD printed as `8b0011a37309237e6a6f44a30f34ccce088ac016`.
- Red-proof (pristine `9ce8e159` + new test file only; the copied file is
  byte-identical to the branch's, `diff -q` clean): the three FAIL messages quoted
  above, from `cd ~/workspace/gopi-base-tick-legs2 && go test -count=1 -run
  'TestMergerTickRunsTheDeadHolderLadder' ./cmd/agent/` → `FAIL github.com/nick/gopi/cmd/agent 13.796s`.
- Branch green (the three new tests plus the two touched gates and the neighbouring
  pass-loop gate, one command):
  `go test -count=1 -run
  'TestMergerTickRunsTheDeadHolderLadder|TestMergerLaunchingFaceRunsTheSerialLegsButStillDoesNotSweep|TestMergerSerialLegsAreOneDefinitionReachedByBothFaces|TestCloseLoopWiringIsComplete|TestPassOnceDrainsMarksAtPassStart|TestMergerPassCloseLoopWiredAfterSweep'
  ./cmd/agent/` → `ok github.com/nick/gopi/cmd/agent 9.924s` (an earlier run of the
  same six → `ok … 17.320s`).
- The SELF-INFLICTED regression, observed red then green: with the drain moved into
  `mergerSerialFaceLegs`, `TestCloseLoopWiringIsComplete` printed
  `cli_tasks_rebuild_gate_test.go:233: declaration "func mergerSerialFaceLegs("
  not found` (it was reading the wrong FILE — `rebuildSrc` — for a declaration in
  `cli_tasks.go`) and `TestPassOnceDrainsMarksAtPassStart` printed `do not drain
  the durable marks at pass START`. After `passStartDrain` + reading `passSrcFile`,
  both appear green in the runs above.
- The `TestSweepZombiesCLI_*` binary dependency, measured BOTH ways: without it,
  `--- FAIL … needs a gopi binary: set GOPI_BIN to THIS tree's build` (the test's
  own refusal to report a green guard that never ran); with
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ && GOPI_BIN=… go test …` →
  `ok github.com/nick/gopi/cmd/agent 11.004s`. `bin/` is gitignored
  (`.gitignore:2:bin/`), so the build leaves the tree clean.
- The dirty-tree lesson of this task, observed not assumed: while
  `devdoc/merger-tick-legs.md` was uncommitted, an unrelated e2e read that
  dirtiness as its own fixture's result — `TestTaskRunSeamWritesReportAndShowRendersIt`
  printed `run t-report-seam left UNCOMMITTED TRACKED WORK in its OWN worktree
  /home/user/workspace/gopi-merger-tick-legs: … entries: M devdoc/merger-tick-legs.md
  | ?? .smoke-skipped.tsv; the completed writeback is REFUSED`. Committing the
  devdoc and removing the skip ledger cleared it (Rule 30).
- `git status --porcelain` in the branch worktree: empty, and the command that
  produced it is the one in the close-out (one line, no entries).
- The measured ordering finding: the branch run's own log carried
  `[merger] [finish-cleanup] dead-holder worktree reclaimed … freed for merge;
  branch kept` for `feat/merger-tick-legs-fixture` on a BARE `gopi merger` tick.
