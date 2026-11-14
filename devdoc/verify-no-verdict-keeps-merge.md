<!-- doc-class: historical -->
# verify-no-verdict-keeps-merge — the post-merge gate must not punish a missing judgement

Epic: two defects in the merger land/verify pipeline, dispatched 2026-09-14.
Branch: `fix/verify-no-verdict-keeps-merge` (worktree `/home/user/workspace/gopi-verdict-reclaim`).

## Defect A — a verdict-less child was treated as a judged FAIL

### The measured incident

Row `01a09d2d-06e6-7c5c-976c-7c526e87e75d`: the post-merge verify child ended
**without recording a verdict** (elapsed 40m42s). The gate's verdict flow had
exactly two outcomes:

```go
if verdict == mergeVerifyVerdictPass { …keep… }
return mergerVerifyUnwind(o, rec, verdict, beforeTip, afterTip)   // everything else
```

"Everything else" silently collapsed two OPPOSITE states of knowledge:

| state | what is known | correct fate |
|---|---|---|
| **judged FAIL** | the merged state was MEASURED red, with evidence | unwind (`git revert -m 1`), file a rework |
| **no judgement** | the child died / was killed at the wait cap / never started | **nothing is known** |

The second row of the table took the first row's fate: master was restored, a
whole merge was consumed, and a from-scratch rework row was filed telling a
re-developer to redo work that had never been judged. An absent measurement is
not evidence of breakage.

### The fix

`cmd/agent/merger_verify_child.go` now routes on the judgement axis before it
routes on the verdict:

- `Pass` → keep (unchanged);
- `ChildDead` / `ChildTimeout` → **`mergerVerifyNoJudgement`**: master keeps the
  merge, the event records the absence of a judgement + the keep statement, and
  the **VERIFY** is requeued;
- everything else → `mergerVerifyUnwind` (the FAIL arm, unchanged).

`mergerVerifyNoJudgement` deliberately shares no body with `mergerVerifyUnwind`:
every step of the unwind (preserve the branch, compute the merge commit,
`git revert -m 1`, file the rework) is wrong for an unjudged merge, so sharing a
body would mean sharing a revert.

### The requeued row (`internal/tools/verify_gate.go`)

`MergeVerifyFileVerifyRetry` files a NEW pending row whose contract is the
opposite of the rework row's in three load-bearing places:

1. it says the merge is **KEPT** — a re-developer reading "reverted" would
   rewrite shipped code;
2. it orders a **VERIFY** (`gopi task verify <id>` / the same locate-build-test
   pass), never an edit. The branch ref may already be consumed by the landing
   (`git branch -d`), so any instruction to check the branch out would be
   unexecutable;
3. it carries `merge-verify-retry` **and** `branch-rework`, so the merger's lane
   predicate (`repo_sweep`'s branch-rework check) still sees the branch as
   lane-owned and does not spend a resolver on it, while the row ITSELF is a
   third, distinct verb (`merge verify retry: <branch> (judge <id>)`).

Dedup: one OPEN retry row per (repo,branch) — a re-run while the previous retry
is pending reports the existing row instead of minting a duplicate.

### The anti-loop cap

`MergeVerifyRetryCap = 2` (`MergeVerifyRetryAttempts` counts EVERY retry row
ever filed, terminal ones included — an open-only count would reset the moment a
retry was claimed). At the cap the gate stops requeueing and records
`MergeVerifyReasonRetryBudget`, a **human-decision handover** — while still
KEEPING the merge: the cap is about the verify, never about the code.

`MergeVerifyReasonRetryBudget` is deliberately a DIFFERENT string from
`MergeVerifyReasonBudget` (the failed-verdict cap). Two different handovers must
not be spelled the same or an operator cannot tell "the code keeps failing
verification" from "the verifier keeps dying".

## Defect B — test-spawned merger daemons pinned task worktrees

### Reconciliation first (Rule 23: read the old branch's path, then go your own way)

`fix/daemon-cwd-escape` (merged as `a72d6f6b`, in master as `c1117fb9`) already
changed the **producer**: `internal/daemoncwd` gives every long-lived child a cwd
that is (①) provably off the `/` partition and (②) provably outside the worktree
it descends from. `finishCleanupWorktreeInUse` already NAMED a daemon holder
without weakening its refusal.

### Measured, not assumed

```
$ for p in $(pgrep -f 'gopi-merger merger'); do echo "$p $(readlink /proc/$p/cwd)"; done
130432 /home/user/workspace/gopi-worker-status-assert/cmd/agent
… 8 more, each cwd inside a task worktree …
$ tr '\0' '\n' < /proc/4001104/environ | grep GOPI_HOME
GOPI_HOME=/dev/shm/gopi-test-scratch/suite-cmd-agent-run-2060563466/TestMergerRestart_…/001
```

Every leftover is a `gopi-merger merger` daemon whose cwd is a **task worktree**
and whose GOPI_HOME is a **suite scratch root**; their `lstart` (13:26–19:48) all
predate the producer fix landing (19:57) and their binaries are deleted
(`readlink /proc/<pid>/exe` → no such file). Running the merger e2e cases on
**master's current tree** left the same pre-existing pids and no new one
(`go test ./cmd/agent/ -run TestMerger` → `ok … 77.0s`, zero new daemons). So the
producer fix holds; the residue is old-binary, and what remained unfixed is the
**recovery** arm.

### Fix ③ — the reclaim arm must converge on a leftover holder

A process standing in the scratch TEST tree is nobody's legitimate holder (no
production process may occupy `SuiteTempRoot`/`MkdirTemp`), and it is IMMORTAL —
so "refuse until the holder leaves" keeps that branch un-recyclable forever,
while "any live cwd may be out-voted" would delete a directory a live test is
standing in. The arm therefore converges on **AGE**:

- `internal/daemoncwd.IsTestScratchCwd` / `IsTestScratchHolder` classify the
  holder and **exclude the daemon's own escape root** (`scratchHome/runtime/process`):
  a correctly escaped daemon stands there for its immortal life and is a
  LEGITIMATE holder — out-voting it would be the guard weakening the epic forbids.
- below `finishCleanupTestHolderMinAge` (10m) the holder is treated as a test
  still in flight → refuse, and the reason names the class and the floor
  (`holder is a leftover of a TEST run: its cwd is inside the scratch test tree …`);
- at or past it → a leftover → reclaim;
- an **unmeasurable age refuses**: an absent fact is never a permission (Rule 15).

The age seam (`var finishCleanupProcAge = finishCleanupProcessAge`) is the AGE
OBSERVATION only; cwd and argv[0] evidence stay real `/proc` reads.

## Rejected alternatives (and why)

- **Unwinding on no-verdict but re-landing**: consumes a merge for an unknown
  judgement and needs the branch ref (already consumed by `git branch -d`).
- **Filing another rework row with a different reason**: still tells a
  re-developer to redo never-judged work.
- **Treating a missing retry row as success**: a silent pass would hide the
  absence of a judgement — the very defect (Rule 15).
- **Letting any live cwd be out-voted after a timeout**: deletes directories out
  from under live tests.
- **Killing the leaked daemons from the gate**: a daemon holds the merger lock;
  killing it externally is how two mergers run at once.

## The residue class is WIDER than the incident — measured, and it corrects the first reading

The file header of `daemoncwd_env_holder_test.go` still carries my first
reading ("the cwd is NOT in the scratch tree, so cwd-only read every one as an
ordinary holder"). A second measurement, taken **while the fix's own e2e suites
were running** (2026-09-14 20:52), disproves the general form of that sentence:

```
$ pgrep -f 'gopi-merger merger'          # 13 live; the two youngest are from a suite running RIGHT NOW
3748192  lstart 19:18  exe=<worktree>/bin/gopi                        (NOT deleted)
   TMPDIR=/dev/shm/gopi-test-scratch/suite-cmd-agent-run-3774849882
   GOPI_TEST_SCRATCH_HOME=/dev/shm/gopi-test-scratch
   GOPI_HOME=…/TestMergerDaemon_LeavesTheLoginSessionScope674712688/001
44361    lstart 20:07  exe=…/worker-daemon-bin/gopi-worker-daemon
   GOPI_TEST_SCRATCH_HOME=/dev/shm/gopi-test-scratch  TMPDIR=…suite-cmd-agent-run-2972946727
130432   lstart 20:16  exe=…(deleted)   GOPI_TEST_SCRATCH_HOME=…  TMPDIR=…suite-cmd-agent-run-2837926703
```

Every live merger daemon carries the scratch roots in its **exec-time
environment**; two of them (`lstart` 20:07 and 20:16) were spawned by
`TestMerger*` suites running on this host at that moment, and their cwd is a
**task worktree**, not the scratch tree — i.e. the cwd channel alone cannot see
this class at all, and daemons of the residue shape are still being produced by
*someone's* suite. (Confounder, stated because it matters: several gopi sessions
run these same suites concurrently on this host, each from its own worktree, so
this read does not attribute a spawn to the CURRENT tree — that attribution is
made separately by the controlled before/after measurement below.)

- the `worker-daemon-bin/` layout is a **suite-internal** layer: a scratch tree
  contains only the suite's own sandbox, so a cwd under
  `<scratch home>/suite-*/worker-daemon-bin` is inside the scratch tree BY
  CONSTRUCTION (`scratch.HomeDir()` is derived from `GOPI_TEST_SCRATCH_HOME`, so
  both live under it);
- `GOPI_TEST_SCRATCH_HOME` present at all is the strictest channel and is set on
  every one of them.

So the fix is *broader* than the incident it was built for, and the reasoning
above should be read with that correction: the cwd channel is belt-and-braces
(and the one that covers a **deleted** binary under `<t>/cmd/agent`), while the
environment channel is the primary one. Both channels stay — a classification
whose two evidence sources can only ADD positives is not weakened by one of them
being redundant on today's host, and the cwd channel is the one that survives a
process whose `/proc/<pid>/environ` a sandboxed caller cannot read.

### The classification is ALSO read from the kernel (measured, this host)

The predicate is exercised against the REAL live leftovers, not only fixtures —
scanning `/proc` directly (a `pgrep -f` scan self-matches, Rule 24):

```
pid=130432    daemon=true  holder=true   cwd=/home/user/workspace/gopi-worker-status-assert/cmd/agent
pid=190573    daemon=true  holder=true   cwd=/home/user/workspace/gopi-adopt-integrated/cmd/agent
pid=2528490   daemon=true  holder=false  cwd=<unreadable>
pid=2995143   daemon=true  holder=true   cwd=/home/user/workspace/gopi-tmp-repo-msg/cmd/agent
pid=3748192   daemon=true  holder=true   cwd=/home/user/workspace/gopi-peak-cost/cmd/agent
pid=3755281   daemon=true  holder=true   cwd=/dev/shm/gopi-vf-master/cmd/agent
pid=3816672   daemon=true  holder=true   cwd=/home/user/workspace/gopi-voice-client-e2e/cmd/agent
pid=4001104   daemon=true  holder=true   cwd=/home/user/workspace/gopi-listperf/cmd/agent
pid=44361     daemon=true  holder=true   cwd=/home/user/workspace/gopi-worker-help/cmd/agent
→ 9 of 10 live merger daemons classified as test-artifact holders; holder=false exactly for the one whose cwd is unreadable (2528490 — a deleted-and-reparented process): an unmeasurable fact is never a classification (Rule 15).
```

(This was the FIRST kernel read, taken on the branch tree alone. The controlled
A/B on the same live population — red tree vs fix tree — is the next section;
the pid set moves between reads because daemons keep being spawned, which is why
the A/B reports counts and controls rather than an identical pid list.)

Note `scratchCwd=false` on every row: this is the environment channel doing the
work, on the live process, which is why the cwd-only predicate had read all of
them as ordinary holders. The probe asserts it saw at least one daemon, so a
broken scan cannot read green.

### Red proof — measured, both halves

**(a) Against the predicate's own tests, on a detached worktree with ONLY the
predicate swapped back to the cwd-only form** (everything else identical — same
commit `10adcf8d` + a 9-line replacement of the function body):

```
$ CGO_ENABLED=0 go test ./internal/daemoncwd/     # red tree
--- FAIL: TestIsTestScratchHolderClassifiesADaemonByItsEnvironment
--- FAIL: TestIsTestScratchHolderSeesTheSuiteHomeVariable
FAIL github.com/nick/gopi/internal/daemoncwd
```

**(b) Against the LIVE processes on this host** (a `/proc` scan, no `pgrep`
self-match), same two trees:

```
red  tree (cwd-only):   RESULT seen=11 holders=0     ← the incident shape, invisibly "ordinary"
fix  tree (branch):     RESULT seen=11 holders=8
```

and the 3 non-holders are the CONTROL negatives, identical on both sides — a
`bash` process that merely carries the pattern in its cmdline, a pid whose cwd
is unreadable, and a process outside the scratch environment. So the change adds
exactly the positives and no false ones.

**Honest scope note on the tests' home.** The two arms that live in
`internal/tools` (`…NamesAFreshTestLeftover`, `…ReclaimsAfterTheTestLeftoverAges`,
`…KeepsAnUnmeasurableTestHolder`) hold under BOTH predicates on this host: the
suite's own scratch root is *inside* the caller's `TMPDIR`, so the daemon fixture
passes the cwd channel too. They therefore pin the AGE CONVERGENCE (the recovery
half this epic dispatched) rather than the classification change; the
classification change is what (a) and (b) pin. Stated rather than glossed: the
`internal/tools` tests would not have gone red on the pre-fix master — the e2e
test would have.

### The operator-facing consequence (deliberately NOT "fixed" here)

The reclaim arm converges on AGE; it does not kill anything. A leaked
test-spawned daemon therefore survives as a **lock-holding process**: measured
evidence that this is real, not theoretical —

```
$ tr '\0' '\n' < /proc/3748192/environ | grep -E 'GOPI_TEST_SCRATCH_HOME|TMPDIR'
GOPI_TEST_SCRATCH_HOME=/dev/shm/gopi-test-scratch
TMPDIR=/dev/shm/gopi-test-scratch/suite-cmd-agent-run-3774849882
$ ps -o lstart= -p 3748192
Mon Sep 14 19:18:55 2026            # ~95 min before this read, still alive, still holding its lock
```

That residue is *identifiable* by one command and no longer *indistinguishable
from a legitimate holder* by the reclaim predicate — which is the whole of
defect B's recovery half. Killing a merger daemon is a live-system action owned
by the merger lock, so it stays an operator decision (Rule 25: no privilege, no
self-appointed reaper).


## Found on the way out: a NEW suite that did not pin its scratch root

The holder tests added here gave `internal/daemoncwd` its first `t.TempDir()`
call sites — and the repo's own `internal/testutil` gate
(`TestEveryTempUsingSuiteIsPinned`, `TestPinCoverageTable`) failed the package:

```
internal/daemoncwd creates scratch (t.TempDir()) but no TestMain routes the pin
through internal/testutil — its scratch lands on the ambient temp root (the boot device)
```

That is Rule 22's criterion catching itself: a suite that creates temp data must
not inherit the ambient default. Fixed with
`internal/daemoncwd/testmain_test.go` → `testutil.RunSuiteMain(m, "daemoncwd")`.

**The fix moved the ground under two fixtures, and that is worth recording:**
pinning puts `t.TempDir()` INSIDE the scratch home, so the two cases whose whole
premise is *"the cwd channel must be silent — this child stands OUTSIDE the test
tree"* became self-defeating (the fixture directory was now inside the tree).
They build a real outside directory instead (`testWorktreeDir`, under the
scratch home's parent), and the positive case keeps its explicit negative
control (`t.Fatalf` if the cwd is inside the tree), so the case cannot pass
vacuously. `childBaseEnv` likewise no longer takes a `t.TempDir()`-derived HOME:
HOME is now a real outside directory (the package's own working dir) — HOME is
not one of the two classified channels, so the baseline stays clean.

## Not done

- The pre-existing leaked daemons (pre-fix binaries) are left RUNNING: killing
  them is a live-system operator action (a merger daemon owns the merge lock),
  not something a test or a code change should do. They are identifiable by
  `pgrep -f 'gopi-merger merger'` + a deleted `/proc/<pi>/exe` link.
- `internal/daemoncwd` has no test that a *spawned* child's cwd is outside the
  scratch test tree on this host beyond the existing daemon-cwd e2e from
  `fix/daemon-cwd-escape`; my part-B work is the RECOVERY arm + its tests.
