<!-- doc-class: normative -->
# Task Report: A truncated run is not a completed run

> **Provenance.** `TestDocAnchorsResolveInNormativeDocs` was retired on 2026-10-22 in `5a35f61` (deleted with `internal/docclass`), so a `file.go:NNN` reference in this page is a landing-time record, not an anchor a live gate enforces — nothing resolves one today. The line numbers stay useful to a reader; they are not checked by the suite. Citations below are quoted as they were written — where one of them reads as present tense ("is RED", "resolves", "fails"), read it as the state of THAT run, never as a gate this tree still has.

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

## Completed

- **`internal/runend` (new package)** — the single vocabulary for *why the
  engine stopped the loop* (`Finished` / `WallclockCap` / `StuckLoop` /
  `StuckToolLoop` / `MaxIterations`), the one truncation predicate
  (`Reason.Truncated()`), the one ledger spelling (`TruncationMarker`,
  `TruncationExplain`, `Detail()`, `ReasonText()`), the one reader
  (`TokenIn`) and the union helper (`Or`). Rule 14 (one home for a shared
  value) — the writer (agent end hook), the reader (`task show`), the CLI
  writeback and the tests all reference these constants, never a literal.
- **The loop records the reason** (`internal/agent/loop.go`): `endReason`
  is declared beside the wallclock deadline and assigned at every
  engine-forced exit — both cap sites, both stuck nets (no-tool `StuckLoop`,
  tool-streak `StuckToolLoop`), and the max-iterations bail. It reaches the
  close-out through the deferred stats closure, so the reason is read after
  the loop returns and is correct on every exit path.
- **The close-out decides from the reason, not only the exit code**
  (`internal/agent/taskregister.go`): `finishSessionTaskRecorded` — the ONE end
  hook — maps `exitCode != 0 || truncation.Truncated()` → `failed`, and records
  the WHY in the row's `reason` column plus the `task_events` result digest.
  The end reason is a parameter of that one function, and the function keeps
  the name `finishSessionTaskRecorded` so the name every doc-reference,
  comment and the structural guard
  (`cmd/agent/task_run_release_coverage_test.go`) uses is the function that
  actually runs the close-out — including its deferred cwd release.
  (572100d5 briefly split the body into a second function
  `finishSessionTaskRecordedWithReason` and left this name a forwarder with no
  production caller; the two shapes were re-merged 2026-09-24 —
  `devdoc/endhook-release-defer-anchor.md`.)
- **The wrapper-child hole is closed** — the path that made the first cut of
  this fix incomplete. A `task run` child registers no auto row, so its END
  hook is a no-op, and the child's stale close-out is *refused* while the
  wrapper is alive (by design: the wrapper's write is the richer one). The
  child therefore records its reason as an append-only **`run-end` trail
  note** (`tools.RecordRunEnd`; event name `runend.EventName`), and the
  wrapper's post-Wait writeback reads it back (`tools.RunEndReasonFor`, which
  walks the reason column then the trail) and refuses `completed`. This is
  the defect's actual production shape: `gopi worker <id>` → exit 0 → a
  half-finished row closed `completed`.
- **`gopi task show` renders truncation** (`internal/tools/tasks.go`): a
  `truncated:  TRUNCATED run (<token>) — the engine stopped the loop … this
  row is NOT a completed result` line, resolved from either slot the close-out
  writes. `internal/tools/task_show_truncated_test.go` pins that an ordinary
  row gets no such line (a false truncation is as bad as a missing one).
- **`resume` still works on a truncated row**: the truncation arm reuses the
  existing `failed`+`recoverable: gopi resume` vocabulary verbatim (no new
  status — the status set is shared by sync/verify/the claim gate), and the
  live run below shows `gopi resume <id>` executing on the truncated row,
  leaving it `failed` with `run_method: resume` and a `resumed:<session>` tag.
- **`terminateStuckRun` disposition — it does not exist.** `code_search
  terminateStuckRun` returns exactly one hit: a *comment* in `internal/agent/loop.go:247`
  ("// (terminateStuckRun → GOPI_TASK_NOTIFY event=loop-stuck + error …"),
  and `git log -S "terminateStuckRun"` attributes that comment to the commit
  that introduced the stuck net. There is no function by that name anywhere in
  the tree — the sibling path the briefing named is the inlined stuck-net exit
  in `Run`, which **did share the defect for the no-tool detector** only in
  the sense that its reason was unrecorded (its exit code is non-zero, so its
  status was already `failed`; the fix adds the reason so `task show` can tell
  an engine stop from a model failure). Both detectors are covered.

## Not Done

- **Rule 11 (LLM adoption proof) is not applicable, stated so it is not read
  as skipped**: this change touches no tool description, no tool name and no
  prompt routing — `internal/runend` is internal vocabulary and the
  `truncated:` line is rendered by the `gopi task show` CLI surface, which no
  LLM tool call reaches. There is therefore no adoption to demonstrate.
- **Pre-existing red gates, NOT caused by this branch, deliberately left
  alone** (each re-run on master `5233f7ba` in `/home/user/work/GoPi` and
  observed there identically): `TestStandardGates/TestGate1ErrorWrapRedProof`
  (a NEW `%v` violation at `internal/tools/memory_sync_claim.go:1026` plus a
  stale baseline at `:938`), `TestDocManifestCoversEveryDevdocFile` (3
  duplicate DOC-INDEX rows + rows owed for `claim-revert-pending-noop.md`,
  `project-context-walkup.md`), `TestDocClassBannerMatchesManifest`,
  `TestAssertionTotalityAuditGate` (1 stale `auditTotalityAllow` key —
  `task_finish_cleanup_test.go:TestServerRecycleDirtyWorktreeSkipped` — the
  second bug the re-issued brief names), `internal/agent`
  `TestDocAnchorsResolveInNormativeDocs` + `TestDocAnchorScopeComesFromTheManifest`,
  and `internal/stylegate` (247 pre-existing sites on master, 246 here; the
  single line-number delta is a shifted site in `cmd/agent/merge_run.go`, not
  a new violation — none of the files this branch touched appears in the set; a
  brand-new literal hardcode gate would be red on day one, which is Rule 16's
  own rejection reason). All are outside this defect's blast radius and fixing
  them here would mix unrelated changes into this branch.
- **The reason does not survive a *later* successful resume.** A truncated row
  is `failed`; `gopi resume` re-runs against that row but writes lineage only
  (the pre-existing behaviour pinned by
  `cmd/agent/cli_resume_failed_row_test.go`), so the row keeps the truncation
  marker even after a resume succeeds. That is consistent with the existing
  resume-on-terminal-row posture (devdoc/resume-on-failed.md) and was left
  alone deliberately: changing it is a product decision about resume outcomes,
  not part of this defect.
- **No live `task run` surface exists to run** — `gopi task run` was removed
  by the worker-run-entry epic. The wrapper path was exercised through its
  successor, `gopi worker <task-id>` (see Verified / Ran).
- **The 8-hour production default** (`defaultSessionWallclock = 8 * time.Hour`)
  was not changed, per the briefing's constraint; the live runs drive the cap
  through `GOPI_SESSION_MAX_WALLCLOCK`.

## The doc-anchor regression this branch caused, and its repair (`8fed6165`)

The manifest-scoped doc-anchor gate caught a defect the branch itself
introduced, and it is worth recording because the mechanism is the ordinary one:
**this branch added lines** to `internal/agent/loop.go`, `taskregister.go`,
`internal/tools/tasks.go` and `cmd/agent/cli_tasks.go`, and every anchor BELOW an
insertion shifted. Fourteen anchors in normative devdocs stopped resolving to
their symbol and started landing on a BLANK line — a hard `t.Errorf`, not a
logged permit.

Method (the same differential method used for the pre-existing gate list above,
and the one to reuse): run the gate on a **detached worktree at the merge-base**
`fb9097bb` (`git worktree add --detach …`), run it on the branch, and `comm` the
two sorted failure sets. A line in `branch-only` is drift this branch caused;
`base-only` is drift it fixed. Measured:

| | new anchor drifts |
|---|---|
| merge-base `fb9097bb` | **14** |
| branch, before repair | **14** (same 14) |
| branch, after `8fed6165` | **3** — all three present identically on the merge-base |

Each of the 14 was re-keyed **by content-identity, not by arithmetic**: the old
line was read from `git show fb9097bb:<file>` and the new line from the working
tree, and the two strings were compared for equality at the mapped line for
every one (`internal/agent/hippocampus.md` provides the clean example: its
`:42` bare continuation belongs to `internal/agent/heartbeat.go`, which this
branch never touched, so it stays `:42` — the file it inherits is what makes an
anchor immune to a shift in a *different* file). A naive `+N` on every anchor
would have corrupted anchors in files the branch never touched.

The remedy is the gate's own rule — *a line number is an accelerator, the symbol
name in the row is the authority* — so the normative docs were repaired rather
than permitted: the baseline permit list is for drift no symbol in the row could
resolve, and it does **not** grow here. Ten pre-existing drifts were fixed as a
side effect (their anchors happened to move onto their symbols).

## Verification

- Build: `go build ./...` → OK (no output); `go vet ./...` → OK.
- Tests: `go test ./internal/runend/ ./internal/tools/ ./internal/agent/
  ./cmd/agent/ -count=1` (branch-scoped to the touched packages) → all PASS.
  New guards: `TestReasonVocabulary`, `TestTokenInIgnoresUnrelatedDetail`,
  `TestTruncationMarkerHasOneHome` (the literal may appear only at its
  definition — a read-floor keeps it from passing blind),
  `TestAgent_WallclockCap_TruncatedRunIsNotCompleted`,
  `TestAgent_StuckLoop_TruncatedRunIsNotCompleted`,
  `TestFinishSessionTask_EngineReasonDecidesStatus` (all four reasons +
  the finished-run control, whose result digest is byte-identical to the
  pre-fix one), `TestCloseOutRunRowRecordsTruncationForWrapperChild` +
  `…UntruncatedChildLeavesTheRowClean`,
  `TestTaskShowRendersTruncatedRun`, `TestTaskShowOrdinaryRowHasNoTruncationLine`,
  `TestRunEndReasonForReadsTheTrailWhenTheReasonSlotIsClean`,
  `TestWritebackOutcomeRefusesCompletedOnTruncatedRow` + `…CompletesAnOrdinaryRow`.
  **Flipped/added claim:** no pre-existing test asserted "cap → completed" —
  `TestAgent_WallclockCap_GracefulFinish` (`loop_stuck_test.go:255`) asserts
  the *other* half (nil error, non-empty result, notify delivered) and is
  unchanged and still green; the status half simply had no test, which is why
  the defect survived. `truncated_run_test.go` now owns that half.
- Ran (reproduction, real binary + scripted LLM, `/dev/shm` off `/`):
  - A slow Anthropic-SSE mock (`mock_llm.py`, replies with a *varying* tool_use
    per round so the run cannot finish and no detector fires) against
    `CGO_ENABLED=0 go build -o gopi-{before,after}` at
    `GOPI_SESSION_MAX_WALLCLOCK=25`.
  - **Before** — `echo 'patch the scheduler timeout bug' | ./gopi-before`
    → `exit_code=0  wall=25s`, ledger:
    `01a0c404-176e-7230-8913-b0024e7324a8 | completed | auto | task finished`,
    trail `completed | — elapsed=25.0s iter=9 tokens=153`, and
    `task show` printed `status: completed`, `reason: task finished` — the
    false success, with the truncation visible only in the session JSONL
    (`session wallclock cap reached (25s) — finished gracefully before the
    model produced a final answer`).
  - **After** — the same command with the rebuilt binary →
    `01a0c404-8b86-75f3-812b-fa2a75b91be2 | failed | auto | TRUNCATED run
    (wallclock-cap) — the engine stopped the loop, not the model; the task was
    NOT reported finished`; trail `failed | — elapsed=25.0s iter=10 tokens=153
    | TRUNCATED run (wallclock-cap) | FAILED — recoverable: gopi resume …`;
    `task show` →
    `status: failed`, `reason: TRUNCATED run (wallclock-cap) …`,
    `result: … | TRUNCATED run (wallclock-cap) | FAILED — recoverable: …`,
    `truncated: TRUNCATED run (wallclock-cap) — the engine stopped the loop
    before the task was reported finished — this row is NOT a completed result`.
  - **Wrapper path** (`gopi task run` successor): `./gopi-before worker
    t-before-worker` → `worker_exit=0`, row `t-before-worker | completed |
    task finished`. `./gopi-after worker t-after-worker` → `worker_exit=0`,
    row `t-after-worker | failed | task failed`, trail
    `failed | … | TRUNCATED run (wallclock-cap) | engine-stopped before
    completion — recoverable: gopi resume t-after-worker …` and
    `run-end | TRUNCATED run (wallclock-cap)`, `task show` `truncated:` line as
    above.
  - **Resume**: `./gopi-after resume 01a0c404-8b86-75f3-812b-fa2a75b91be2` →
    `resume_exit=0`, the run executed (cap fired again at the 8s test cap),
    and afterwards the row still reads
    `failed | TRUNCATED run (wallclock-cap) …` with
    `tags: resumed:9065fe19-8c48-4442-a93b-da3649975819`,
    `run_method: resume`, `truncated: …` — resume is not broken by the fix.
- Result: `go build ./...` OK; `go vet ./...` OK; `gofmt -l` on the touched
  files prints nothing; the branch-scoped suites pass; the live runs above show
  the flipped verdict end-to-end on both the auto-row and the wrapper path.
- Commit: `<see the branch's tip commit>`.

## Evidence (self-filed)

- `go build ./...` → exit 0, no output.
- `go vet ./...` → exit 0, no output.
- `gofmt -l internal/runend internal/agent/loop.go
  internal/agent/taskregister.go internal/agent/truncated_run_test.go
  internal/tools/tasks.go internal/tools/task_show_truncated_test.go
  cmd/agent/cli_tasks.go cmd/agent/cli_truncated_writeback_test.go` → no output.
- Pre-fix live row: `01a0c404-176e-7230-8913-b0024e7324a8 | completed | auto |
  task finished` (sqlite3, read-only, exit 0).
- Post-fix live row: `01a0c404-8b86-75f3-812b-fa2a75b91be2 | failed | auto |
  TRUNCATED run (wallclock-cap) — …` (same query).
- Post-fix wrapper row: `t-after-worker | failed | task failed` with
  `run-end | TRUNCATED run (wallclock-cap)` in its trail.
- `git status --porcelain` in the worktree at close-out: see the final message.

## Reproduce

`/dev/shm/gopi-trunc-repro/{mock_llm.py,run-cap.sh,run-worker.sh}` — start the
mock (`python3 mock_llm.py 8771 3`), then `./run-cap.sh <binary> <label>` for
the auto-row path or `./run-worker.sh <binary> <label>` for the wrapper path.

## Follow-up root-cause verification: the LATCH, not the status rule (2026-09-25)

The status rule fix above was **necessary and not sufficient**. The two tests in
this file went red *again* afterwards — the row parked at `in_progress` — and the
second defect was in a different place: **the end hook's own early return**, which
is reached *after* the status rule and therefore hides it.

### Measured shape

| surface | master `2f1596b6` | tip `d3e94762` |
|---|---|---|
| `TestAgent_WallclockCap_TruncatedRunIsNotCompleted` | PASS | FAIL `status: in_progress` |
| `TestAgent_StuckLoop_TruncatedRunIsNotCompleted` | PASS | FAIL `status: in_progress` |

Fixed by `e22d91fd` — *"fix(agent): the end hook stays armed when the start hook
re-enters"* — an ancestor of `2f1596b6` but **not** of `d3e94762`. A run that
measured its reds on `d3e94762` is measuring a tip from before this landed.

### 5-why

1. Why does the auto row stay `in_progress`? The end hook
   (`finishSessionTaskRecorded`) returned before its ledger write.
2. Why did it return? Its guard fired:
   `if !a.autoTaskRegistered || a.SessionLog == nil { return }`.
3. **Which clause?** `!a.autoTaskRegistered` — the `SessionLog` operand was
   **fine**. (A temporary stderr probe on a pristine `d3e94762` worktree, added
   then reverted, printed
   `ENDHOOK guard fired: autoTaskRegistered=false SessionLogNil=false` — and the
   same trace shows the hook *was* reached, so "the hook is never wired up" is
   also falsified: the probe sits inside it, past the deferred cwd release.)
4. Why was the latch false? `maybeRegisterSessionTask` cleared
   `a.autoTaskRegistered = false` on **ENTRY**. A second call arriving with the
   row already latched then took the *adopted-row* return
   (`latchWrapperRow()` → true, "continues an existing row; no auto mirror") —
   having already DISARMED the latch the first, minting call had set.
5. Why does a *start*-hook latch govern the *end* hook? The two hooks share **one
   bit that means two different things**: *"this call minted a row"* (per-call)
   and *"this run has a row to close"* (per-run).

**Norm violated:** a per-call question must not be answered by overwriting a
per-run fact. Two lifetimes, one variable — so the re-entrant call's answer
destroyed the run's.

**Repair:** take the attaching arm FIRST and clear the latch only on the arm that
mints a NEW row. For a wrapper child the gate is a no-op on the latch
(`autoTaskRegistered` is false by design — the wrapper's own writeback owns that
row), so the minting arm loses nothing, and the hook becomes
correct-by-construction instead of correct by the caller's discipline.

### Falsification — the variable is the latch ordering and nothing else

1. On a pristine detached worktree of `d3e94762`, both tests FAIL with the row at
   `status: in_progress` — the observation reproduced.
2. On an otherwise-green tree, moving the latch clear back to ENTRY and widening
   the gate — 9 lines, `internal/agent/taskregister.go` only — re-reds both:

   ```
   --- FAIL: TestAgent_WallclockCap_TruncatedRunIsNotCompleted (1.86s)
   --- FAIL: TestAgent_StuckLoop_TruncatedRunIsNotCompleted (1.13s)
   ```

3. Restore the file (`git status --porcelain` empty) → both PASS.

### Reachability, stated honestly

No production caller reaches the start hook twice: `runEngine` is its only
production call site, and the chat faces (`Agent.Chat` / `ChatOps`) never reach
it. The second call that reproduced the red comes from this file's own fixture,
which calls the start hook to create the row the assertions read. So a fixture
ordering exposed a **real close-out bug** — but this was not a production
incident, and recording it as one would overstate the fix.

### The session-JSONL terminal-write witness

The row's status alone cannot distinguish "the hook returned early" from "the
write landed elsewhere" — which is exactly how this survived a *correct* status
rule. `e22d91fd` closes that hole: the end hook now records its terminal ledger
write as `sessionevent.EventTypeTaskRowClosed` (`task_row_closed`) via
`SessionLogger.LogTaskRowClosed`, on success **and** on failure, and both tests
assert that witness. `assertTerminalRowWriteWitness` requires exactly one event
carrying `task_id` / `status` / `reason` and no error; the negative arm asserts a
wrapper child (which writes no row) carries none. Correspondingly `task_id` was
added to `volatileLogKeys` — it is a per-run identifier like `sessionId`, and the
determinism check in `TestHippocampus_DefaultOffLeavesBehaviourUnchanged` needs
two runs to agree on **status + reason**, which is the part that matters.
