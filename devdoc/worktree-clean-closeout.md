<!-- doc-class: historical -->
# Task Report: a run leaves its worktree CLEAN — the named rule + the close-out check for BOTH dirty shapes

Branch: `feat/worktree-clean-closeout` (worktree `~/workspace/gopi-wt-clean-closeout`,
fresh from master `ba53da3d`, Rule 6a; Rule 23 — the sibling branches
`feat/worktree-refusal-arms-v2` and the prior enforcement branch were READ
(`git show`/`git log`), never committed on).

Epic: *"A gopi run must leave its worktree CLEAN"* — ten branches permanently
invisible to the merge queue because the producing run left untracked scratch or
never committed real tracked work, while its row still says `completed`.

## What was already true when this run started (measured, not assumed)

Two of the four deliverables were **already on master**, landed by the prior
enforcement run (`135b0f64` feat, `bec3e40f` tests, devdoc
`devdoc/run-untracked-residue-enforce.md`):

- `ReclaimRunResidueCheck` (internal/tools/task_reclaim.go) measures the row's
  OWN recorded worktree at close-out and records an attributed
  `completed-unclean` event + stderr line; the `GOPI_TASK_RUN_RESIDUE_ENFORCE`
  switch refuses the writeback.
- Its tests and the A/B red proof.

Re-measured on master rather than trusted:

```
$ git merge-base --is-ancestor 135b0f64 master && echo IN   → IN
$ git merge-base --is-ancestor bec3e40f master && echo IN   → IN
```

**The gap this run closes** — three things:

1. **No rule.** `grep -n "porcelain" GOPI.md` → nothing. The mechanism existed
   with nothing stating the contract it enforces, so a run could not know
   before violating it.
2. **The worse shape was SILENT.** The close-out check spoke only for
   untracked-only residue; a worktree left dirty with TRACKED content — an
   uncommitted deliverable, the shape that makes `feat/web-local-trust` /
   `fix/ledger-firstopen-budget` / row `01a0aa8b` (`completed`!) invisible —
   produced no close-out record at all.
3. The epic's coverage statement therefore could not be made honestly: the
   mechanism covered one of the two shapes the rule must cover.

## The rule: `GOPI.md` **Rule 30** — placement argument

The epic offers two placements (a new numbered rule, or fold into Rule 6) and
demands the choice be argued.

**Chosen: a new numbered Rule 30.** Reasons, in the order that decided it:

- **Different obligation, different moment.** Rule 6 ("Isolated Work Directory
  + Git Push Semantics") governs **where** work happens — worktree vs clone,
  and how the branch gets back to the main repo. Rule 30 governs **the state
  the worktree is left in at close-out**. Rule 6's own text ends at "the commit
  is already on the branch … that IS the deliverable" — it has no close-out
  step to hang this contract on, and grafting one in would make a *setup*
  paragraph silently own a *finish* contract.
- **Different audience.** Rule 6 is read once, when a run starts and picks its
  ground. Rule 30 must be read by every run before it reports ready — it is the
  last thing a run does, and it is the one rule whose violation invalidates
  everything else the run did (an unmergeable branch).
- **The repo's own precedent.** Rules 25–29 are each one named, checkable
  discipline appended at the end; Rule 30 is that shape. The rule's identity is
  greppable (`grep "^## Rule 30" GOPI.md`) and the code cites it by number
  (the enforcement arm's header names "GOPI.md Rule 30"), which a fold into
  Rule 6 could not do.
- **It is machine-checked, and the check points at the rule.** Rule 16–21
  established that a rule worth having is worth a check; Rule 30 is one of the
  rules whose check exists in code from the day it lands.

**Coverage statement (explicit, as the acceptance requires): BOTH shapes.**

| shape | porcelain | the rule's remedy | close-out event |
|---|---|---|---|
| untracked litter | `?? …` | delete it, or keep it **outside** the worktree (session dir) | `completed-unclean` |
| uncommitted tracked work | `M`/`A`/`D`/`R`/`C` … | **commit** it (deleting destroys the work) | `completed-unclean-tracked-work` |
| clean | (nothing) | — | (no record) |

The rule states the self-check verbatim — `cd <your worktree> && git status
--porcelain   # must print nothing` — and states the two remedies as OPPOSITE,
because "clean up the worktree" is exactly the wrong instruction for the tracked
shape. It also carries the epic's prohibition in the rule text itself: the rule
is *not* a licence to build a tolerating machine.

The worker-ownership pointer gate (`internal/testutil`) stays green: Rule 30
mentions no worker-launch/task-run command shape, so GOPI.md needs no pointer
line (verified — see Verification; the gate's one red is a pre-existing
`briefings/brief-worker-run-entry-rework.md`, measured identically on baseline).

## The check: both shapes at close-out

`internal/tools/task_reclaim.go` — the existing enforcement arm, extended (not
rewritten, Rule 4). The prior arm's classifier-level split was reused as the
predicate; nothing about the reclaim/removal guards changed.

- **`reclaimResidueClass(porcelain)` → `reclaimResidueClean` /
  `reclaimResidueUntracked` / `reclaimResidueTracked`.** A PREDICATE over the raw
  measurement (delegating to the existing `reclaimUntrackedOnly`), deliberately
  **not** a comparison of rendered sentences: this epic's own history shows a
  wording comparison silently silencing the arm
  (`devdoc/run-untracked-residue-enforce.md` §Addendum — 4 tests went red when
  the sibling arm reworded a marker). Named constants, one definition (Rules
  1b/13/14).
- **`reclaimRunResidueNote(taskID, worktree, porcelain)` is the ONE facade** every
  caller uses; it dispatches to `reclaimRunResidueUntrackedNote` (the prior
  sentence, unchanged) or `reclaimRunResidueTrackedNote` (new), each quoting the
  classifier's **own wording by reference** — `reclaimUntrackedResidueMarker` /
  `reclaimSkipDirtyReason` — never re-spelled (Rule 14), and each carrying its
  own remedy.
- **Events are distinct but one family** (Rule 14): the shared stem
  `reclaimRunResidueUncleanEvent` (`completed-unclean`) with the two shapes
  suffixed from it — `reclaimRunResidueEvent` (untracked) and
  `reclaimRunResidueDirtyEvent` (`…-tracked-work`). The idempotence guard
  (`reclaimRunResidueFiled`) probes the STEM, so adding the second shape could
  not make a row record twice. `reclaimRunResidueKind` is the one place a shape
  becomes an event name.
- **The tracked shape is always RECORDED, never REFUSED.**
  `reclaimRunResidueRefusal` now requires the class to be untracked-only before
  the operator's switch may stop a writeback. Reason: a tree holding tracked work
  may hold another session's in-progress edits — the epic's ten worktrees are
  exactly that — and the epic forbids strengthening or redirecting the
  "uncommitted work is work" guards. This is a deliberate, tested asymmetry.
- **Nothing is deleted, no branch is touched.** The arm still contains no
  `--force`, no `os.Remove`/`os.RemoveAll` and no `git worktree remove` — the
  pre-existing Rule-13 source gate (`TestReclaimRunResidueArmNeverForcesAnd…`)
  keeps asserting that over the extended region.

### Where it is wired

Unchanged from the prior run and deliberately not duplicated: the record rides
`taskUpdate` — the one state gate every harness door passes through
(`TaskRunWriteback`, the auto-register end hook's `TaskRunCloseoutStaleRun`, the
merger's executor, the CLI's `task update`). The record runs **after** the
terminal write commits (a failed measurement can never roll back an outcome);
the refusal sits **before** the UPDATE. `internal/tools/run_untracked_residue_test.go`
already proves both doors carry it.

## Warn vs fail: the design choice, and why

**Warn + ledger is the default; the hard stop is an operator switch — and the
switch is scoped to the untracked shape only.**

1. **The status is the writeback's authoritative terminal fact.** The
   neighbouring `deliverable.branch` backfill is documented as a side-channel
   that "never rolls back the outcome". Default-denying the `completed` write
   would deadlock a finished run at its last write — trading one defect for a
   worse one, and putting the merge queue's problem into the run's exit path.
2. **The record is what makes the violation VISIBLE, and visibility is the
   acceptance.** The epic's complaint is not "runs are allowed to be dirty" — it
   is "the dirt silently poisoned the branch while the row said `completed`".
   One attributed `task_events` row + one stderr line, naming the run, the
   worktree and the entries, ends the silence. Measured below.
3. **Deleting is data loss, so a "fail and clean" default was never an option**
   — the classifier cannot tell the run's `build.out` from a human's scratch
   note, and the epic explicitly forbids a heuristic "probably scratch" class.
4. **The hard stop exists for an operator who wants it**, and where it is safe:
   untracked-only dirt in the run's OWN worktree is nobody's work, so
   `GOPI_TASK_RUN_RESIDUE_ENFORCE=1` leaves the row OPEN (with the refusal
   itself as a ledger fact) and the run can clean up and write back. The tracked
   shape is excluded because refusal there means acting on work that may belong
   to a live sibling session.
5. **Nothing was weakened to make this pass.** `reclaimSkipDirtyReason`,
   `reclaimDirtyVerdict`, `reclaimDirtyHeld` and every removal gate are
   byte-identical; the refusal semantics of the sweep are untouched. The new
   code only *speaks* about a state that was previously unspoken.

## Interaction with the ten existing dirty worktrees (honest)

**This change makes them visible going forward; it does not retroactively
report them, and it does not clean them.**

- The check measures the row's worktree at **close-out of a `completed`
  writeback**. The ten rows are already terminal, so no door re-measures them:
  they stay exactly as they are today. That is by design — the epic assigns
  those trees to the sweep (`01a0aa9e`) and park (`01a0aaa6`) lanes and forbids
  hand-cleaning them here.
- What *does* change for them: the next close-out **inside** one of those trees
  (a resumed run writing back) will now record the shape, so the dirt stops
  being invisible at the moment it is touched again.
- The sweep's rendering of those trees is **unchanged**: it still refuses them
  with `reclaimSkipDirtyReason` (tracked) / `reclaimUntrackedResidueReason`
  (untracked) and removes nothing. The prior run's per-row
  `reclaimUntrackedResidueMarker` report remains the sweep-side handle.
- The ten are therefore *not* silently recyclable — no guard was loosened. The
  only new capability is that a NEW violation announces itself.

## Verification

Raw command evidence (worktree `~/workspace/gopi-wt-clean-closeout`,
master `ba53da3d`; scratch root `/dev/shm/gopi-test-scratch`, dev id 28 vs `/`
2051 — Rule 22).

### RED → GREEN (the new shape is invisible before, visible after)

The red proof is written against the **pre-fix API only** so the SAME file
compiles on both trees and the two runs differ by the fix alone
(`internal/tools/zz_redproof3_dirty_closeout_tracked_ab_test.go`):

```
# baseline — a DETACHED worktree at pristine master, ONE file copied in:
$ git worktree add --detach /dev/shm/gopi-test-scratch/wtclean-red-baseline master
$ cp internal/tools/zz_redproof3_dirty_closeout_tracked_ab_test.go <baseline>/internal/tools/
$ cd <baseline> && go test ./internal/tools/ -count=1 -v -run TestZZRedProof3_TrackedDirtyCloseoutIsVisible
--- FAIL: TestZZRedProof3_TrackedDirtyCloseoutIsVisible (0.45s)
    zz_redproof3_dirty_closeout_tracked_ab_test.go:67: UNCOMMITTED DELIVERABLE: the run closed out
    `completed` while its OWN worktree …/wt still held a TRACKED change (work.txt) and NOTHING on
    the row's trail says so — the branch is then invisible to every merge path while the row reads
    completed
FAIL	github.com/nick/gopi/internal/tools	5.574s

# this branch — the same file, no copy:
$ go test ./internal/tools/ -count=1 -v -run TestZZRedProof3_TrackedDirtyCloseoutIsVisible
--- PASS: TestZZRedProof3_TrackedDirtyCloseoutIsVisible (0.29s)
PASS
ok  	github.com/nick/gopi/internal/tools	1.792s
```

The passing run prints the operator line it files:

```
[task-notify] run t-dirty-ab closed out with UNCOMMITTED TRACKED WORK in …/wt:
worktree is DIRTY (uncommitted work) — left for a human — entries: M work.txt;
the deliverable was never committed (commit it before reporting ready — a dirty
close-out makes the branch invisible to the merge queue; the outcome stands,
nothing deleted)
```

### Live end-to-end: a real run that ends dirty, with the visible signal

A real repo + a real linked worktree on its own branch, driven by the built
binary (`bin/gopi` from this branch), scratch-root only
(script: `/dev/shm/gopi-test-scratch/wtclean-evidence.sh`):

```
### the run's worktree right before close-out (Rule 30 self-check) ###
 M work.txt
?? build.out
### close-out: gopi task update --status completed ###
[task-notify] run t-e2e-dirty closed out with UNCOMMITTED TRACKED WORK in
/dev/shm/gopi-test-scratch/wtclean-e2e/wt: … entries: M work.txt | ?? build.out; …
Task updated: t-e2e-dirty (status=completed, reason=task finished)
### the row's audit trail (the visible signal) ###
in_progress ::
completed :: run finished; all green
completed-unclean-tracked-work :: run t-e2e-dirty closed out with UNCOMMITTED TRACKED WORK in
  /dev/shm/gopi-test-scratch/wtclean-e2e/wt: worktree is DIRTY (uncommitted work) — left for a
  human — entries: M work.txt | ?? build.out; the deliverable was never committed (commit it
  before reporting ready — a dirty close-out makes the branch invisible to the merge queue; the
  outcome stands, nothing deleted)
### the row's status (the outcome stands — the record is a side-channel) ###
completed
```

**False-positive guard in the same run** — a CLEAN worktree close-out files
nothing and says nothing:

```
### clean worktree self-check (must print nothing) ###                       <- no output
### close-out of the clean run (expect NO new event) ###
Task updated: t-e2e-clean (status=completed, reason=task finished)
in_progress ::
completed :: clean run                                                      <- no residue event
```

### New tests (green here)

`internal/tools/run_dirty_closeout_test.go` — 4 new tests:

- `TestReclaimRunResidueTrackedWorkIsRecordedAtCloseout` (record + wording +
  entries + worktree, via the real writeback),
- `TestReclaimRunResidueNoteCoversBothShapes` (the facade: clean silent,
  untracked → litter sentence, tracked → tracked sentence, mixed → tracked;
  neither sentence may carry the other shape's wording),
- `TestReclaimRunResidueEnforceSwitchNeverRefusesTrackedWork` (the switch is
  scoped; the outcome stands; the shape is still recorded),
- `TestReclaimRunResidueTrackedShapeHasItsOwnEvent` (distinct events, one stem,
  right event per shape).

`internal/tools/run_untracked_residue_test.go` — 1 updated:
`TestReclaimRunResidueCheckCleanAndTrackedAreSilent` now asserts only the CLEAN
case (the tracked case is no longer silent by design; it has its own tests).
The sibling `zz_redproof2_…TrackedChangeStaysTodays` explicitly asserted the
OLD tracked silence; it is superseded by
`zz_redproof3_…TrackedDirtyCloseoutIsVisible` — **see Not Done**.

Targeted run of the whole family:

```
$ go test ./internal/tools/ -count=1 -run 'ReclaimRunResidue|RunResidueTracked|RunOwnUntrackedResidue|ZZRedProof'
ok  	github.com/nick/gopi/internal/tools	12.542s
```

### Gates

```
$ make build            → CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/     (exit 0)
$ go vet ./...          → VET_DONE                                            (exit 0, no output)
$ gofmt -l .            → 7 paths, IDENTICAL to pristine master's gofmt -l set
                          (diff of the two sorted lists: IDENTICAL_GOFMT_SETS —
                          4 cmd/agent test files, 2 devdoc/voice-zerodep-poc files,
                          internal/testutil/worker_ownership_gate_test.go,
                          internal/tools/task_deliverable_worktree.go; all pre-existing,
                          none touched here. `make fmt` deliberately NOT run — Rule.)
$ go test ./internal/standards/ → ok github.com/nick/gopi/internal/standards 29.929s
                          (Rules 16–21 green, baseline unchanged — nothing added, no permit)
$ ANTHROPIC_AUTH_TOKEN=… go test ./cmd/agent/ -count=1 -run 'ReclaimRunResidue|Residue|WorkerRunRow|Dirty'
                        → ok github.com/nick/gopi/cmd/agent 24.273s
```

### Merge readiness (measured against the master of the day, not the branch base)

The branch was created from `ba53da3d`; master moved to `17cec86b` while this
run was in flight. Measured in a throwaway detached worktree at `17cec86b`:

```
$ git merge --no-commit --no-ff feat/worktree-clean-closeout    → exit 0, NO conflicts
$ CGO_ENABLED=0 go build ./...                                  → BUILD_OK
$ go vet ./internal/tools/                                      → VET_OK
$ go test ./internal/tools/ -run 'Residue|Dirty|Closeout|Reclaim|ZZRedProof' → ok 32.509s
$ go test ./internal/standards/                                 → ok 17.385s (baseline unchanged)
```

So the branch lands on the current master cleanly and green.

### GOPI.md pointer gate

```
$ go test ./internal/testutil/ -count=1 -run 'TestWorkerOwnership'
--- FAIL: TestWorkerOwnershipPointerPresent
      1 uncovered file(s): briefings/brief-worker-run-entry-rework.md
--- FAIL: TestWorkerOwnershipGateStaysGreenAcrossSuccessiveMergeRounds
```

**Pre-existing, not this change** — measured on the pristine baseline tree in
the same session: byte-identical failure (same single file, same two tests).
The offender is a briefing this run never touched, and GOPI.md (which Rule 30
edits) is **not** in the uncovered list: Rule 30 mentions no worker-launch / task-run
command shape, so the gate has nothing to require there. The briefing names the
gate as a risk when editing GOPI.md; that risk is measured, not assumed.

## Files

- `GOPI.md` — the new Rule 30 (rule text, self-check command, coverage of both
  shapes, the two opposite remedies, the checked-at-close-out mechanism, and the
  explicit prohibition on a tolerating machine).
- `internal/tools/task_reclaim.go` — `reclaimResidueClass`, the two note
  builders + the facade, the event family (`…UncleanEvent` stem /
  `…Event` / `…DirtyEvent`), `reclaimRunResidueKind`, the extended outcome core,
  and the refusal scoped to the untracked shape.
- `internal/tools/run_dirty_closeout_test.go` — the 4 new tests (the tracked
  shape's regression half).
- `internal/tools/run_untracked_residue_test.go` — the clean/tracked test
  narrowed to the clean case.
- `internal/tools/zz_redproof3_dirty_closeout_tracked_ab_test.go` — the A/B red
  proof (FAIL on pristine master, PASS here), pre-fix API only.
- `devdoc/worktree-clean-closeout.md` — this report.

## Not Done (honest)

- **The tracked shape's pre-existing A/B red proof still passes, unchanged —
  and that is the evidence the two shapes were separated, not conflated.**
  `zz_redproof2_run_residue_tracked_ab_test.go` asserts "a TRACKED change must
  keep today's refusal — the run's own *untracked-residue* wording
  (`reclaimUntrackedResidueMarker`) may never be filed against somebody's
  uncommitted work". Re-run on this branch over the whole `ZZRedProof` family:

  ```
  --- PASS: TestZZRedProof2_RunOwnUntrackedResidue_TrackedChangeStaysTodays (0.54s)
  --- PASS: TestZZRedProof3_TrackedDirtyCloseoutIsVisible (0.64s)
  --- PASS: TestZZRedProof_DirtyDeadHolderWorkIsOrphaned (4.37s)
  --- PASS: TestZZRedProofFalseReclaimPromise (3.94s)
  --- PASS: TestZZRedProof_RunOwnUntrackedResidueIsRecordedAndAttributed (0.64s)
  ok  	github.com/nick/gopi/internal/tools	12.652s
  ```

  Both hold at once because the tracked shape is recorded with the *classifier's
  tracked* wording (`reclaimSkipDirtyReason`) and never with the untracked
  marker: the two contracts are orthogonal, and a future edit that "simplifies"
  the two sentences into one would redden redproof2 immediately. Nothing was
  edited to keep it green — it was re-run and it is green on its own terms.
- **One test count assertion**: `TestReclaimRunResidueWritebackRecordsOnceAnd…`
  counts `event LIKE 'completed-unclean%'` rows — deliberately still green, and
  it is the guard that the tracked-shape addition did not double-record.
- **No producer-side (static) gate was added.** The epic asks to consider a
  `internal/testutil`-style gate that fails when a test/script writes into the
  repo tree. Not built, with reason: the `.evid/` families are produced by
  **shell commands at run time inside a worktree** (log redirections, an
  evidence directory), invisible to a gate that scans *sources*; and a runtime
  gate over "the tree must be clean after the suite" would red on any unrelated
  worktree the suite is invoked from. The close-out check is the mechanism that
  can actually see this fact, and it is the one the epic chose. Recorded as a
  candidate, not silently dropped.
- **The ten existing dirty worktrees are untouched** — by instruction (sweep
  `01a0aa9e` / park `01a0aaa6` own them). This change neither cleans them nor
  retro-reports them; see the interaction section above.
- **The default is still warn, not fail.** If the operator wants the hard stop
  as the default for untracked litter, it is a one-line flip of
  `reclaimRunResidueEnforced` — deliberately left as the operator's switch, per
  the warning above about the writeback's authoritative terminal fact.
- **The WHOLE `./internal/tools/` package cannot be run to completion on this
  host — and that is pre-existing, measured on pristine master.** `go test
  ./internal/tools/ -timeout 240s` panics `test timed out after 4m0s` while
  running `TestMergeBuildGate_SharedModCacheResolvesWithProxyOff`; the SAME
  test is named in the panic on a detached member of **pristine master
  `0f2cc45c`** (master: `FAIL … 247.053s`; branch: `FAIL … 242.654s`). The
  affected-package evidence in this report is therefore the TARGETED family
  run, which passes on both trees:

  ```
  branch  cae557e0: go test ./internal/tools/ -count=1 -timeout 300s \
                      -run 'Residue|Dirty|Closeout|Reclaim|ZZRedProof'   → ok 51.076s
  master  0f2cc45c: the same command on a detached pristine worktree    → ok 59.939s
  ```

  Not caused by this change (both trees fail identically on the same
  unrelated test) and not silently skipped: it is recorded here because a
  future reader running the whole package will otherwise think this change
  broke it.
- **Rule 11 (LLM adoption proof) does not apply**: no tool was added or modified
  and no prompt routing changed (Rule 3 respected — no new top-level tool, no
  new CLI verb).
