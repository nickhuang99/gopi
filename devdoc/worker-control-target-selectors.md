<!-- doc-class: historical -->
# The worker control face's TARGET SELECTORS — `--pid` and `--branch`

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Task**: `01a13274-9e41-7d92-bbf5-9669edfbe3b4`
**Epic**: the foreground worker had no safe stop path (two defects); the
file-delivery half of the same epic is `devdoc/worker-foreground-file-stop.md`.
**Cut point**: `cf15c989d`

## The defect (measured 2026-10-13)

`gopi worker --stop --branch <B>` was **accepted and ignored**.

`--branch` is the RESUME family's ground selector (`cli_worker_resume.go`:
`scanWorkerResumeFlags`). The CONTROL face (`cli_worker_control.go`) parsed the
actions (`--stop` / `--restart` / `--start` / `--status`), `--force` and `--wait`
— and **no target selector existed anywhere on the stop path** (grep for
`--branch` in the control file: zero hits). So:

1. the word was not the control face's, and
2. `workerControlRequested` did not route it either (its vocabulary was actions +
   `--status`), so the argv fell to `scanWorkerArgs`, which read `--branch` as
   **the resume mode's ground** — and the `--stop` was discarded;
3. when the invocation *did* reach the stop arm (an action present), the arm's own
   route was the **enumeration fallback**: `foregroundWorkerPIDs()` →
   every foreground worker of the profile, or none.

Both halves are the same disease: a word the operator used to name ONE worker
changed nothing, and the command then acted on a set nobody named — printing
nothing about the substitution. Reproduced (before the fix), verbatim:

```
$ gopi worker --stop --branch some-branch
gopi worker: no DETACHED worker daemon is running (…)
gopi worker: 2 FOREGROUND `gopi worker` process(es) found (they hold no daemon lock): 209851, 449811
gopi worker: asking FOREGROUND pid 209851 to finish the in-flight round and exit …
gopi worker: asking FOREGROUND pid 449811 to finish the in-flight round and exit …
```

Neither worker had anything to do with `some-branch`.

## The change

### 1. A selector is a first-class part of the control argv

`worker_control_selector.go` owns the vocabulary, the parse and the resolution:

| selector | meaning | how it is resolved |
|---|---|---|
| `--pid N` | that process, as `--status` lists it | its OWN shape is measured (`workerControlWorkerShape`) |
| `--branch B` | the worker holding branch B | **the ledger**: `tools.TaskForBranch` → the row that owns B → that row's claim stamp (`claimed_by`) → the holder pid |

`--branch` is resolved through the ledger and **not** by scanning `/proc`,
because a worker's argv does not carry the branch it works on while the ledger
does (`deliverable.branch` is stamped at claim time, and the row's claim stamp
names the executing process). Reconstructing "which branch is this worker
holding" from `/proc` would be a second, weaker definition of one fact —
the duplicate-source defect Rules 13/14 forbid. The stamp is read through
`tools.ClaimHolderPID` (a new one-line EXPORTED face of `claimHolder`), so the
`host:pid` parse that every liveness judgement in the tree already trusts stays
in exactly ONE place.

### 2. Routing: the word belongs to this face now

`workerControlRequested` gained the selector test, so a `--stop --branch B` argv
is the CONTROL face's even when the action is absent (it then gets the refusal
below). This is what stops `--branch` being read as the resume mode's ground.
`--worktree` (the resume family's other ground selector) is deliberately NOT
claimed: only `--branch` is shared.

A control invocation that ALSO carries `--resume` is now refused by name —
`workerControlConflictParse` appends the resume spelling to the flag list it
reports, so the sentence names the mode that used to swallow the stop.

**`--resume` KEEPS `--branch`, and that exception is load-bearing.** `gopi worker
--resume --branch B` is the RESUME mode's ordinary spelling: the word names the
GROUND that mode starts in, which is a different question from "which worker do I
address". So `workerControlRequested` claims `--branch` only when `--resume` is
ABSENT (`workerResumeModeRequested`) — `--pid` is claimed unconditionally, since no
other face reads it. Measured, and the reason the exception is written down rather
than remembered: claiming the word unconditionally turned a resume into
`--branch … names a TARGET but no ACTION` (exit 2 where the resume arm's own
refusal, exit 1, is the contract) — caught by
`TestToolsMaintenanceHelpSurfaceFormsAreTyped/gopi_worker_--resume_--branch_B`.

### 3. No fallback, ever: every unresolvable selector is a NAMED refusal

| situation | answer (exit) |
|---|---|
| the branch has no owner row | `no ledger row names branch B (ledger L holds N row(s)) — there is no worker for this branch HERE` (1) |
| the ledger was not readable | `the ledger could not be read for branch B: …` — never rendered as "nothing there" (5b) (1) |
| the owner row's holder is on ANOTHER host | `row R is held on host H (pid P there) — that pid means nothing on this host` (1) |
| the owner row records no claim stamp | `row R (status) records NO claim stamp — no process holds this branch` (1) |
| the local holder pid is gone | `row R (status) names holder pid P, which is GONE` (1) |
| the pid is alive but is not a `gopi` worker | `pid P is alive but is NOT a gopi worker process (<cmdline>)` (1) |
| a selector with no action | `--pid 12 names a TARGET but no ACTION — say what to do with it (…)` (2) |
| a BARE `--branch`, and it is the resume family's word | **this face prints nothing** — the invocation leaves the control block and the RESUME face answers: `--branch is the GROUND of a resume — it means nothing without --resume` (2) |
| both selectors at once | `--pid and --branch name ONE target two ways, not a filter pair` (2) |
| a selector whose value is unreadable | `--pid needs a value` / `--pid "abc" is not a positive process id` / `--pid repeated (given twice)` (2) |

`workerControlResolveSelection` has **no** path that returns "unresolved, so do
the old thing": the only nil-target return is the honest "no selector was
given", which is what keeps the unselected routes working.

### 3b. The gate's FOURTH outcome: a deferral, and it is silent

`workerControlSelectionGate` returns `(sentence, refuse, applied, deferred)`. The
fourth is not a refusal and not a selection — it is this face saying "that word is
not mine to answer without an action", and it is
`workerControlDefersToTheResumeFace` = `action == "" && sel.pid == 0 && sel.branch != ""`.

The measured defect it repairs: `gopi worker --branch feat/x` carries no mode switch,
and the RESUME face already refuses exactly that with the sentence that TEACHES the
repair. This face answered first with the aimless-target refusal, whose three remedies
(`--status`/`--stop`/`--restart`) cannot start a resume — two refusals for one typo,
the wrong one first.

**The predicate is a conjunction, and every half is load-bearing.** `sel.branch != ""`
alone also swallows a bare `--pid 12` — a word NO other face reads — and hands it to a
face with no sentence for it; with an action, `--branch B` is this face's own selection
(`--stop --branch B`) and deferring it would give the stop to the face that has none.

**The caller must LEAVE the block, not fall through to its tail.** In `main.go` the
arm is `if refuse {…exit} else if applied {workerControlCommand; return true} else if
!deferred {…status route…}` — a deferral runs neither, so `isWorkerSubcommand` returns
without the control face answering and `scanWorkerArgs` + `workerResumeOnlyFlag`
(`cli_worker_daemon.go`) render the resume sentence. Routing a deferral to the block's
tail `workerControlCommand` instead is the same answer by a longer route: that call
re-parses the selector and takes its OWN applied arm.

`--resume` itself cannot reach the gate: with the mode switch present
`workerControlRequested` does not claim `--branch`, so the invocation was routed to the
resume face before the gate ran.

### 4. A selected target is stopped by the SAME arms as an unselected one

`workerControlStopSelected` reuses `workerControlStopOne` (a daemon record: file
+ version-checked signal) and `workerControlStopForeground` (a process with no
record: file first, signal only on proven binary identity — the arm the sibling
half of this epic built). `--status <selector>` reports the resolved target and
its chain, read-only. `--restart <selector>` stops the target then starts the
replacement through the existing `--daemon` machinery.

## Verification (the full evidence is in the task report)

- Unit: `worker_control_selector_test.go` — routing, the parser (both spellings,
  every refusal), resolution through the ledger (a live holder resolves to its
  pid; the report names the branch, the row, the status and the pid), the
  four-way "cannot resolve" matrix, the dead-pid fixture's own honesty check, and
  the structural red proof.
- Real run (Rule 31), 2026-10-13, two binaries with **different inodes**
  (`p1/gopi` inode 174983683, `p2/gopi` inode 174983684) in a private profile:
  a foreground worker started from `p2` addressed by `p1` —
  `--status --pid` names it; `--stop --pid … --wait 30s` delivers by FILE
  (the worker's own log line names `TERM_<pid>` and the sender's argv), the
  worker exits **0** in 501 ms, the pid is gone, the control directory is EMPTY,
  and the worker's log carries **0** `signal: terminated` lines (nothing was
  signalled). Against the operator's real ledger,
  `--status --branch task/01a13274-…` printed
  `--branch … → row 01a13274-9e41-7d92-bbf5-9669edfbe3b4 (in_progress) → holder pid 209851`
  and exited 0.

## Not done

- `--pid` accepts a live pid that is a `gopi worker` in a session of its own
  with no state record (a detached process whose record was cleared) and treats
  it as a target. That is deliberate (the file channel is read by pid and is
  conservative), but it is NOT pinned by a test — the fixture that would build it
  is a detached child, and the epic's budget went to the branch selector.
