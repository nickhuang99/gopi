<!-- doc-class: historical -->
# The `task run` wrapper's success path never reads the ledger — diagnosis, live reproduction, and why the brief's proposed fix is wrong

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Task**: `01a0f3ef-bc74-7af3-811e-ba843b3ce317`
**Briefing**: `briefings/brief-worker-trusts-exit-code-not-ledger.md`
**Master at the time of measurement**: `80553b35`
**Worktree**: `~/workspace/gopi-01a0f3efbc747af3811eba843b3ce317`
**Status**: DIAGNOSIS ONLY — nothing in `cmd/agent/cli_tasks.go` or `internal/` was modified. The only added file is the reproduction test.

---

## 1. What the defect is (the brief's premise — CONFIRMED)

`runTaskRunOnce` (`cmd/agent/cli_tasks.go`) decides a run's outcome from the child's
exit code plus `writebackOutcome`. The ledger read the brief asks about **exists** —
but only on the failure arm:

```go
2589    runErr := cmd.Wait()
2600    if runErr != nil {
...
2607        closedByChild, rowStatus, statusErr := tools.TaskRowTerminalAt(target)
2608        writebackOutcome(target, tools.TaskStatusFailed, ...)
2618        taskRunReleaseOwnWorktrees(target, runCloseoutReapScope(target))
2620        taskRunAbnormalChildRetry(target, closedByChild, rowStatus, statusErr)
2621        return fmt.Errorf("gopi task run: task %s FAILED (child: %w)", target, runErr)
2622    }
2626    writebackOutcome(target, tools.TaskStatusCompleted, "gopi run completed"+stats, ...)
...
2649    if rep, err := tools.TaskFinishCleanupSessionID(target); err != nil { ... }
2673    taskRunReleaseOwnWorktrees(target, runCloseoutReapScope(target))
2674    fmt.Printf("task %s completed (elapsed %s)\n", target, elapsed)
2675    return nil
```

Between `2626` and `2675` — the whole success arm — there is **no read of the row.**

## 2. Absence proof (deliverable ③)

**The only production call site** of the ledger read, repo-wide:

```
$ grep -rn 'TaskRowTerminalAt(' cmd internal --include=*.go | grep -v '_test.go'
cmd/agent/cli_tasks.go:2607:		closedByChild, rowStatus, statusErr := tools.TaskRowTerminalAt(target)
internal/tools/task_run_retry.go:110:func TaskRowTerminalAt(taskID string) (terminal bool, status string, err error) {
```

One definition, **one** call site — and it is inside `if runErr != nil` (line 2600).

**Zero occurrences inside the success arm's span** (line 2626 → the `return nil` at 2675):

```
$ awk 'NR==2626{f=1} f{print NR": "$0} f&&/return nil/{exit}' cmd/agent/cli_tasks.go \
    | grep 'TaskRowTerminalAt'
$ echo "exit=$?"
exit=1
```

Empty output, exit 1 — nothing matches. The success arm consults the ledger nowhere.

## 3. Live reproduction (deliverable ① — RED)

`cmd/agent/task_run_success_path_ledger_read_test.go` drives the **real wrapper**
(`gopi worker <id>` — the explicit-id door onto `runTaskRunOnce`), with a mock LLM
whose child does one legitimate write and leaves untracked scratch behind. The arm
is reached **by configuration, not by a race**: `GOPI_TASK_RUN_RESIDUE_ENFORCE=1`
(Rule 30's own enforcement switch) plus the run's own litter makes
`tools.TaskUpdateWithResult` **refuse** the `completed` writeback
(`internal/tools/tasks.go:4400`, `reclaimRunResidueRefusal`).

Raw output, verbatim:

```
=== RUNTIME EVIDENCE (red arm: residue enforcement armed) ===
wrapper exit code        : 0
wrapper stdout tail      : "worker: own-worktree-release: own registered worktree kept: ... — younger than the 5m0s floor (1s) — a concurrent round may be creating it"
wrapper says 'completed' : true
wrapper stderr has "writeback failed (continuing)" : true
ledger row status after  : "in_progress"
row terminal?            : false
=== END RUNTIME EVIDENCE (red arm: residue enforcement armed) ===
    task_run_success_path_ledger_read_test.go:199: DEFECT REPRODUCED: wrapper exit 0 + stdout says 'completed' + row left "in_progress" (non-terminal)
--- PASS: TestWrapperSilentFalseSuccessOnRefusedWriteback_RED (1.13s)
```

and the two lines that produce it, from the same run:

```
        task t-wrapper-false-success-red completed (elapsed 0s)
        gopi worker: writeback failed (continuing): task: t-wrapper-false-success-red REFUSED the completed writeback — … worktree is DIRTY (untracked-only residue …)
```

**The wrapper announced completion in the same run in which the ledger refused to
record it.** `writebackOutcome` returns nothing (it is `func(...)` with no results),
so a refused write is a stderr line and nothing else: the exit code, the stdout
line, and the `return nil` are all unchanged.

## 4. The green arm (deliverable ② — and the measurement that INVERTS the brief's fix)

Same fixture, switch **off**. Raw output:

```
=== RUNTIME EVIDENCE (green arm: enforcement off) ===
wrapper exit code        : 0
wrapper stdout tail      : "worker: own-worktree-release: own registered worktree kept: ..."
wrapper says 'completed' : true
wrapper stderr has "writeback failed (continuing)" : false
ledger row status after  : "completed"
row terminal?            : true
=== END RUNTIME EVIDENCE (green arm: enforcement off) ===
--- PASS: TestWrapperSuccessIsSoleWriterAndHonest_GREEN (0.77s)
```

The wrapper's behaviour is unchanged (exit 0, says completed) — but the child's own
output carries the fact that breaks the brief's proposed rule:

```
[task-notify] task run closeout t-wrapper-honest-success-green: no live claim evidence
   (claimed_by="peerbox:2771416" session_id="df000fd8-…") — left open for the stale sweep
```

### The inversion

The child's END-hook close-out is refused by `runCloseoutClaimEvidenceHolds`
(`internal/agent/taskregister.go` → `tools.TaskRunCloseoutStaleRun`) **while the
wrapper's claim is alive** — and the wrapper is alive by construction, being the
parent inside `cmd.Wait()`. Therefore, on a *perfectly healthy* run, at the instant
the wrapper looks at the ledger (its post-`Wait` moment), **the row is still
`in_progress`.**

| arm | row at the wrapper's post-Wait look | who made it terminal |
|---|---|---|
| green (healthy) | `in_progress` | the **wrapper's** writeback at line 2626 |
| red (refused writeback) | `in_progress` | **nobody** — it stays open |

**The brief's proposed rule — "non-terminal ⇒ the child died without closing itself" —
is false as written: it would reclassify every healthy run as abnormally dead.**
The row's terminality is not the discriminator. The discriminator is whether the
close-out **landed** (i.e. whether the writeback was *accepted*), which is the
value `writebackOutcome` currently throws away. Asserted in the test at
`task_run_success_path_ledger_read_test.go:248-258`.

This also answers the brief's "corroborating fact": yes, the wrapper's writeback is
the **sole** writer of a successful run's row. There is no second opinion to appeal to.

## 5. Ordering analysis (deliverable ④)

**Two orderings are load-bearing, and they are different questions.**

**(a) The read must precede the writeback (line 2626).** `writebackOutcome` **writes**
the terminal status. A read taken after it would always observe `completed` and
would therefore always conclude "someone else closed this row" — the exact error the
failure arm's own comment names (`cli_tasks.go:2601-2605`): *"'already terminal when
I looked' is the OPPOSITE fact from 'I am about to close it'"*. The failure arm
already gets this right; the successor must be placed **between `cmd.Wait()` (2589)
and `writebackOutcome` (2626)**.

**(b) The read must precede the release tail (line 2673).** In call order the
success arm is:

```
writebackOutcome(completed)            2626   ← writes the row
TaskFinishCleanupSessionID(target)     2649   ← push / worktree remove / branch -d
taskRunReleaseOwnWorktrees(target)     2673   ← reclaim the ground + move cwd out
fmt.Printf("task … completed")         2674   ← the operator is told "success"
```

The release tail runs **before** the completion line is printed. So on the arm this
defect is really about — a child killed with its end hook never run, leaving **no
litter** — the sequence is: the wrapper writes `completed` (nothing refuses it: the
row was open and the ground is clean), finish-cleanup **removes the ground**, and
only then does the console say "completed". The worktree and the branch are gone,
the row is terminal, and no arm of the machine recorded that the child died. **That
is the silent false success with its evidence destroyed**, and it is precisely the
hazard the operator raised.

**What I measured in the red arm is the benign half of that**: because the fixture's
residue makes the ground dirty, both `TaskFinishCleanupSessionID` and
`taskRunReleaseOwnWorktrees` **refused** —

```
task t-wrapper-false-success-red: finish-cleanup skipped: worktree dirty (live bond protected) — carries ONLY UNTRACKED files …: ?? .wprepro-litter.txt
task t-wrapper-false-success-red: own-worktree-release: own worktree release refused: … worktree is DIRTY (untracked-only residue …)
```

— so in *this* arm the ground survived. **That is a coincidence of the fixture, not
a protection**: the litter that triggers the refusal is the same litter that vetoes
the removal. A clean ground releases the hazard's full form. I did not build a
third arm for the clean-ground case: the refuse-trigger *is* the litter, so the two
are coupled in one fixture and separating them needs its own construction; the
ordering conclusion above rests on the code order (2626 → 2649 → 2673 → 2674),
which is unambiguous. **Filed as an open question for the implementer** — see §7.

## 6. Candidate actions (deliverable ⑤ — NOT implemented, not chosen)

| # | action | mechanism available | consequence | risk |
|---|---|---|---|---|
| **A** | Make `writebackOutcome` **return the write result**, and refuse the `completed` claim in the wrapper when the writeback was refused | `tools.TaskUpdateWithResult` already returns the error; the wrapper currently discards it (`cli_tasks.go:3125-3127` logs and drops) | the smallest change; the defect stops being silent *for the refused-writeback arm*; exit code and stdout can then be made to agree with the ledger | does **not** cover the killed-child arm (there the writeback *succeeds* — the wrapper writes `completed` onto an open row, and nothing refuses it) |
| **B** | Mark the row `failed` with a reason naming the abnormal death | `writebackOutcome(target, TaskStatusFailed, …)` | row stops being silently open; the merger and the operator both see it | needs a discriminator that survives §4's inversion — row terminality alone is **not** it; the honest signal ("the close-out was refused / the end hook never ran") must be plumbed out of the child |
| **C** | **Resume** the run | `internal/tools/task_holder_resume.go`; `TaskResumeTarget` (`internal/tools/taskproc.go:888`) | recovers the work automatically — the operator's stated goal | **ordering hazard**: by the time the wrapper knows, `taskRunReleaseOwnWorktrees` (2673) may already have freed the ground. Resuming is unsafe after the release tail unless the read is moved ahead of it. Also: the automated form already exists for the abnormal-exit arm (`taskRunAbnormalChildRetry`, a bounded, detached, never-babysitting retry) — a second auto-resume route on the success arm risks re-running a child that merely *looked* dead |
| **D** | Refuse the close-out entirely and surface it | — | nothing is destroyed, a human decides | stalls a whole worker round on any transient; needs a bounded, named condition to refuse on |

**Resume vs interrupt** (the brief's distinction): *resume* is right when the work is
incomplete and the ground still exists — it needs (b) above. *Interrupt* (hand over
for an operator decision) is the honest outcome when the ground is gone, or when
the row's state cannot distinguish "died" from "did not close out" — which, per §4,
is the **default** state today. Candidate C must not be selected before the
discriminator exists, or it will re-run healthy children.

**No action was implemented. `cmd/agent/cli_tasks.go` is unmodified.**

## 7. Not Done (honest)

- **No fix.** Per the briefing's scope, this task diagnoses. §6 is a table of
  options with consequences; nothing in it was applied.
- **The clean-ground arm of the ordering hazard (§5b) was not built as its own
  fixture.** In the fixture used, the litter that triggers the refused writeback is
  also what vetoes the ground's removal, so the two cannot be separated without a
  second construction. §5b rests on the code's call order, not on a runtime
  observation of a removed ground. **This is the single most valuable follow-up
  measurement**: a child that leaves a *clean* ground and never reaches its end
  hook, with the wrapper's `completed` write succeeding — does the ground actually
  disappear before the "completed" line?
- **The child-side signal was not plumbed.** §4 shows the discriminator must come
  from whether the close-out landed. Where that flag should live (a row tag, the
  child's exit path, `RunEndReasonFor`'s vocabulary) is an implementation decision
  left open.
- **`reconcileChildMirror` (2637) was not analysed** — under `task-single-row-exec`
  the child mints no mirror row, so it is expected to be a no-op, but I did not
  verify that on a legacy fixture.

## 8. Verification

- **Build**: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → `BUILD_OK`
- **Tests**: `GOPI_BIN=$PWD/bin/gopi go test ./cmd/agent/ -run 'TestWrapperSilentFalseSuccessOnRefusedWriteback_RED|TestWrapperSuccessIsSoleWriterAndHonest_GREEN' -v`
  → `--- PASS: …_RED (1.13s)` / `--- PASS: …_GREEN (0.77s)` / `ok github.com/nick/gopi/cmd/agent 1.930s`
- **Ran**: the test drives the real surface — `testutil.RunGopi(t, []string{"worker", row}, …)`
  against the built `bin/gopi`, with a mock LLM child, reading the row back through
  the production reader (`tools.TaskShow`). The `Ran:` surface is the `gopi worker <id>`
  verb exercised end to end; the arms' raw evidence blocks are quoted verbatim in §3 and §4.
- **Result**: red arm — wrapper exit 0, stdout `task t-wrapper-false-success-red completed (elapsed 0s)`,
  ledger row `in_progress`; green arm — wrapper exit 0, ledger row `completed`, child's
  close-out refused with `no live claim evidence`.

## 9. Evidence (self-filed)

- Absence proof — `grep -rn 'TaskRowTerminalAt(' cmd internal --include=*.go | grep -v '_test.go'`
  → exactly two lines (one definition, one call site at `cli_tasks.go:2607`, inside `if runErr != nil`).
  The success-arm span search printed nothing and `grep` exited **1**.
- Red arm — the evidence block and `DEFECT REPRODUCED: wrapper exit 0 + stdout says 'completed' + row left "in_progress" (non-terminal)`.
- Green arm — the evidence block plus the child's line `no live claim evidence … left open for the stale sweep`, machine-asserted in the test, which is what makes §4's inversion a measurement and not an inference.
- Ordering — line anchors `2589 / 2600 / 2607 / 2618 / 2626 / 2649 / 2673 / 2674`, read from the file.

## 10. Reuse

The reproduction is **deterministic by configuration** (`GOPI_TASK_RUN_RESIDUE_ENFORCE=1`
+ the run's own untracked litter), not by race or timing — a fix's regression guard can
reuse this fixture as-is. Two properties the test pins for free: the child's END-hook
close-out is refused while the wrapper lives (the sole-writer fact), and the wrapper's
own writeback is not refused on a healthy run (which is what lets the inversion be
asserted rather than argued).
