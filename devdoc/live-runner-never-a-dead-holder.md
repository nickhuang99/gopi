<!-- doc-class: historical -->
# Devdoc: a LIVE recorded runner is never a "proven dead holder" (sweep rung ③ veto)

Task: `01a0a996-dde5-77a4-bbb2-de6087f155e6` — branch `fix/live-runner-not-dead-holder2`,
base `master` = `900f648c`.

## The defect (measured, not inferred)

`task sweep-worktrees --apply` deleted a **live** session's worktree.

Chain, with the exact code sites:

1. `worktreeSweepRowHoldsGround` rung ③ keeps a registration only when the row
   *holds ground*: live status **AND** `!HolderProvenDead` **AND** `!lifeEnded`.
2. `HolderProvenDead` is decided by `finishCleanupDeadLocalHolder(status, claimed_by, rec)`
   — whose third argument is the row's **recorded runner**
   (`proc_pid` / `proc_start_ticks` / `proc_cmd`).
3. `finishCleanupProcessHolderLive` — the liveness half — carried a **three-rung**
   ladder: pid present, not zombie, start ticks match, **and then** a *fourth*
   rung comparing `procCmdline(pid)` against the recorded `rec.Cmd`:

   ```go
   if cmd := procCmdline(rec.Pid); rec.Cmd != "" && cmd != "" && cmd != rec.Cmd {
       return false // same pid + start, different program: not this runner
   }
   ```

   pid + start ticks are **unique per process incarnation** — two processes
   cannot share the pair — so once that rung passed, the live process *is* the
   recorded runner. The cmdline comparison could therefore never mean "another
   process owns the number"; it only ever compared two *spellings* of the same
   process's command line.
4. Any spelling difference thus became a **death proof**: the raw `/proc/<pid>/cmdline`
   transcription (trailing NUL rendered as a space), an in-place `exec`, or a
   string truncated at `procIdentityCmdlineMax`.
5. ⇒ `HolderProvenDead = true` ⇒ rung ③ skips the row ⇒ the sweep reaches
   `unclaimed-reclaim` and deletes the **directory** *and* drops the
   `.git/worktrees/<name>` registration — the exact incident shape the
   live-holder veto exists to prevent
   (`task_finish_cleanup.go`'s own comment at the `worktreeSweepRowHoldsGround`
   consumer records the opposite intent).

`taskIdentityVerdict` already documents and implements the correct precedence
(pid layer decides; the string is corroboration). `finishCleanupProcessHolderLive`
had simply never received that ruling — one ladder, two spellings (Rule 14).

Measured on this host (peerbox) through the scratch rig, against the pre-fix
parent: the rig's fixture writes the cmdline verbatim from `/proc`
(`"sleep 300 "`), a **live** pty-holding runner was classified dead, and the
apply pass printed

```
reclaim     [unclaimed-reclaim] age=72h .../positive/wt (no task row owns it, no live holder, clean, ...)
```

with the directory gone and the registration dropped (`RIG_EXIT=1`).

## The fix (Rule 4: surgical)

`internal/tools/task_finish_cleanup.go` — `finishCleanupProcessHolderLive`'s
ladder is now **pid-only**:

| evidence | verdict |
|---|---|
| no `/proc/<pid>` entry | gone (definite) |
| zombie (`pidIsZombie`) | gone (terminated, unreaped — `pidAlive`'s Z rule) |
| start ticks differ | gone (definite — the number was recycled) |
| start ticks agree | **ALIVE** (definite — pid+start is unique per incarnation) |
| no identity recorded (`StartTicks <= 0`) | falls to the caller's older, narrower rule — behaviour unchanged for rows stamped by an older build |

The negative direction is untouched: a recycled pid is still gone, so the orphan
release still works. No new constant was needed (Rule 1/13/14): the surviving
comparisons reuse the existing names.

Why not "compare the cmdline more leniently" (trim / prefix)? Because the
comparison is not an evidence axis at all: after a start-ticks match it cannot
distinguish processes, and it *can* only produce false deaths. A lenient
comparison would have kept the same failure mode for the next spelling.

## Regression tests (red before, green after)

`internal/tools/live_claim_worktree_veto_test.go`:

- `TestProcessHolderLiveIgnoresTheRecordedCmdlineSpelling` — cheap classifier
  pin: pid + start ticks match, recorded cmdline differs, the runner must still
  count as live. Also asserts the refusal direction survives (start ticks +1 ⇒
  gone).
- `TestSweepKeepsTheWorktreeOfALiveRecordedRunner` — the **sweep-level** red
  proof: seeds a live-status row whose claim stamp names a dead pid while its
  recorded runner is live (raw `/proc` spelling), then drives the **real** row
  loader (`worktreeSweepTaskRows`) and the **real** judge
  (`worktreeSweepJudge`) on a real git registration, asserting the row holds
  ground and the registration is KEPT with rule `ledger-ownership` naming the
  owning row.

Pre-fix run (working tree reverted to `master:internal/tools/task_finish_cleanup.go`,
everything else identical):

```
--- FAIL: TestProcessHolderLiveIgnoresTheRecordedCmdlineSpelling
    a live runner (pid 1559966, matching start ticks) was called NOT live because
    its recorded cmdline — "sleep 300 " — differs from procCmdline()'s "sleep 300"
--- FAIL: TestSweepKeepsTheWorktreeOfALiveRecordedRunner
    a LIVE recorded runner was classified HolderProvenDead — rung ③ will skip
    the row and reclaim its worktree
```

Both fail on the pre-fix parent with exactly the intended messages and pass on
the fix.

## The rig

`scripts/repro-live-holder-worktree-veto.sh` — **already on the base, no commit
of its own**: `git diff 900f648c..HEAD -- scripts/` is empty, i.e. the rig (and
the age-gate fix in it) came in with master `62a2b002`, an ancestor of this
branch's base. Deliverable 3 is therefore its *run*, not its text. It drives the
**real CLI** (`gopi task sweep-worktrees --apply --min-age-h 1`) over a scratch
rig under the scratch tree (Rule 22):

- **positive arm** — claim stamp = dead pid on this host, recorded runner = LIVE
  process, its cmdline recorded in the raw `/proc` spelling ⇒ worktree directory
  and `.git/worktrees/<name>` registration must survive, and the sweep must
  print its own keep verdict naming the owning row.
- **negative arm** — a provably dead runner's worktree is still reclaimed (there
  is no non-resident CLI face for the directory-deleting arms, so this direction
  is reached through the package test, invoked from the rig).

**Age gate.** The registration's age is measured by
`worktreeSweepCreatedAt` (`internal/tools/worktree_sweep.go`) — it reads the
backlink `<gitdir>/gitdir` and then `HEAD`, first hit wins, *not* `commondir`
and not the newest mtime. The rig therefore ages **those** files
(`find "$repo/.git/worktrees" -mindepth 2 -maxdepth 2 \( -name gitdir -o -name HEAD \) -exec touch -d '3 days ago' {} +`).
Without it a fresh registration reads `keep [age-threshold] age=0h` and the
positive arm never reaches the ownership rung it exists to prove — a red that
read like a broken assertion while proving nothing. (The epic's text names the
reader `worktreeSweepRegistrationAge`; on this master the function carrying that
logic is `worktreeSweepCreatedAt` — same reader, older spelling.)

Result against the fixed binary: `rig results: PASS=4 FAIL=0`, `RIG_EXIT=0`.
Against the pre-fix binary: `PASS=1 FAIL=3`, `RIG_EXIT=1` (directory deleted,
registration dropped, reclaim line present).

## Deliberately NOT done

- The **doc / rig-text** question (stale instructions elsewhere about the rig,
  whether the rig's comments are still accurate for other branches) is **not
  folded into this code task**, per the epic constraint.
- The pre-existing `internal/standards` reds at base `900f648c`
  (`TestGateCwdReleaseFindsTheRealTree`, `TestStandardGates/TestGate1ErrorWrapRedProof`,
  `TestStandardGateDataReport` — the `internal/shot/x11.go` get-prefix / `/tmp/.X11-unix`
  escapes and the Rule 16 baseline) are **inherited, not introduced**: the same
  three failures appear on a pristine checkout of the branch's base, and this
  branch's diff touches only `internal/tools/` (2 files) + the devdoc, none of
  them a reported site. Measured fail-set identity (timing stripped, the ONLY
  normalisation): base `900f648c` vs branch `ba40df18` → **IDENTICAL** four
  `--- FAIL` lines including sub-tests. Measured on the **current master tip**
  `fa4aea7a`: `TestGateCwdReleaseFindsTheRealTree` still fails (plus a Rule 16
  `NEW=1` hit) — so the whole red set is a master-level defect set, older than
  and independent of this branch.
  - One caveat stated honestly rather than rounded to "all green": the Rule 16
    row carries **NEW=1**, at **`cmd/agent/main.go:980`**
    (`fmt.Errorf("Error reading @ payload file %q: %w (escape a literal prompt starting with %v as \\%v)", …)`).
    It is byte-identical at base `900f648c` and on master tip `fa4aea7a`
    (whichever `%v` the predicate matches, it is in the *format* text, not a
    dropped error), and it is absent from this branch's diff (zero `cmd/agent`
    files). It is inherited and deserves its own dispatch — either `%w`-wrap the
    intended cause or add `// no-wrap: <reason>` plus the baseline entry.
    **NEW=0 on every other gate row.**
  - Correction of an earlier draft of this devdoc (commits `bf5e93af` … this one):
    it claimed the NEW site was `internal/tools/worktree_sweep.go:1696`. That was
    wrong. Re-measured on both trees: `worktree_sweep.go:1696` is the gate's
    **escape** line (it carries the reviewed `// no-wrap:` annotation — the
    unlock error is deliberately outside the chain), while the **NEW**
    (unannotated) violation is `cmd/agent/main.go:980` — exactly as this report's
    first draft said. Recorded here so a future reader does not repeat the
    inversion: *escape* ≠ *NEW* — both appear in the data report, and only the
    `NEW violation …` line names an unpermitted site.

## Closing re-measurement — what is green, and the tools-suite flake's root cause

Re-measured at `2716429a` (the branch tip). Honest split, because "all green" would be false:

| check | result |
|---|---|
| `CGO_ENABLED=0 go build ./...` | exit 0 |
| the three tests of this fix (`-run 'TestProcessHolderLiveIgnoresTheRecordedCmdlineSpelling\|TestSweepKeepsTheWorktreeOfALiveRecordedRunner\|TestDeadHolderReclaimStillFreesAProvenDeadRunner'`) | 3/3 PASS, `ok 2.365s` |
| `BIN=$PWD/bin/gopi bash scripts/repro-live-holder-worktree-veto.sh` | **RIG_EXIT=0**, `PASS=4 FAIL=0`; apply line `reclaimed=0`; `grep -c unclaimed-reclaim` = **0** |
| `go test -count=1 ./internal/tools/...` | **1 fail line, twice in a row** — `TestTaskFindSessionJSONLStillHonoursEveryLayout` (pre-existing flake, below) |
| `go test -count=1 ./internal/standards/...` | red, **inherited** (identical fail set at base `900f648c` and at master tip) |

**The flake is pre-existing, measured, and root-caused — not a regression from this fix.**

- Rate: `-count=12 -run TestTaskFindSessionJSONLStillHonoursEveryLayout ./internal/tools/` →
  **5/12 FAIL at base `900f648c`**, **3/12 FAIL on this branch**. Neither `internal/tools/tasks.go` nor
  `internal/tools/taskledger_cost_test.go` is in this branch's diff.
- Mechanism (deterministic, both directions): the test builds its fixture from a Go **map**, so write order
  is random. The `bare` layout is a DIRECTORY `<sessionID>/session.jsonl`; the `flat` layout is the FILE
  `<sessionID>.jsonl`. Write `bare` first and the resolver returns the directory form for the flat
  assertion → the assertion fails against a path that is not the one it wrote.
- Proof: a two-test probe copying the body verbatim and pinning the order —
  `probe(bare→flat)` **FAIL** (identical message), `probe(flat→bare)` **PASS**. One hundred percent
  deterministic; the randomness is entirely the map's iteration order.
- Filed separately as task `01a0aa22-fa30-718e-9422-c0eef51f0510` (tags `bug,test-flake`) with the probe and
  the acceptance bar (`-count=20` → 0 failures). Fixing it here would have been scope without value and
  would have widened this fix's diff away from the classifier it is about.

**Rule 16 attribution, finally pinned** (the flip-flop is recorded above on purpose): the data report's Rule 16
row carries one **NEW** violation at **`cmd/agent/main.go:980`** — byte-identical at base `900f648c` and on
master tip `fa4aea7a`, and **zero `cmd/agent` files in this branch's diff** ⇒ inherited, its own dispatch.
`internal/tools/worktree_sweep.go:1696` is the gate's reviewed **escape** line (`// no-wrap:` annotation), not
a NEW violation. Lesson worth keeping: in the data report an `escape:` line and a `NEW violation` line look
alike and mean the opposite — read the label, not the file:line.
