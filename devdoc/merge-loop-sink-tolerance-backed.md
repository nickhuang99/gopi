<!-- doc-class: historical -->
# The merge loop's tolerated audit sink is now backed by the writer's own record

Task `01a143b4-6876-76a8-a003-ce1393f1a0ba`, 2026-10-17, host `hosta`,
ground `task/01a143b4687676a8a003ce1393f1a0ba` @ `347579b26`.

## The report that started it, and what was actually true here

The task arrived as "`TestMergeEveryLoopWritesNoStateFile` is RED:
`.gopi/logs/removal-audit.log` appears in the profile `gopi merge --every` was
given". Measured on THIS ground first, before anything else:

```
$ go test ./cmd/agent/ -run 'TestMergeEveryLoopWritesNoStateFile' -count=1
ok  	github.com/nick/gopi/cmd/agent	2.593s      (3 runs, all exit 0)
```

So: **not a regression on this ground, and not even red.** The report's baseline
(`3db1fd211`) is an ancestor of this ground's cut point (`347579b`, 139 commits
ahead), and the tolerance landed in between:

```
$ git merge-base --is-ancestor c466c05c1 HEAD && echo "c466c05c1 IS ancestor of HEAD"
c466c05c1 IS ancestor of HEAD
$ git merge-base --is-ancestor c466c05c1 3db1fd211 || echo "c466c05c1 NOT ancestor"
c466c05c1 NOT ancestor
```

`c466c05c1 feat(cmd/agent): the merge-loop residue walk tolerates the audit sink
it writes itself` (2026-10-16 22:49, task `01a14369`) is the commit that turned
that scenario green by admitting a second artifact. The detached-worktree control
run reproduces the reported red verbatim, one commit before it:

```
$ git worktree add --detach /home/user/workspace/gopi-redproof-3db1fd211 3db1fd211
$ cd /home/user/workspace/gopi-redproof-3db1fd211
$ go test ./cmd/agent/ -run 'TestMergeEveryLoopWritesNoStateFile' -count=1
--- FAIL: TestMergeEveryLoopWritesNoStateFile (2.34s)
    merge_every_test.go:573: the profile holds [.gopi/logs/removal-audit.log] — the command's contract is no state file, no pid file, no progress record.
        WHY THIS CAN HAPPEN: the ONLY writer that can create a directory here is the signal-provenance recorder …
FAIL	github.com/nick/gopi/cmd/agent	4.157s
```

**Verdict: pre-existing at the reported baseline, fixed before this ground.**
Nothing was red here to fix.

## Who creates the sink — named by the writer's own record, not by its path

The first dispatch of this bug named the writer from the assertion's own WHY text,
which pointed at `internal/signalrec`. That text was its author's hypothesis.
This run named it from the emitter's own output instead, with the sink redirected
by the emitter's own override so a record could be read without the loop's profile
in the way:

```
$ GOPI_REMOVAL_AUDIT_LOG=$S/artifacts/probe-audit-1.log \
    go test ./cmd/agent/ -run 'TestMergeEveryLoopWritesNoStateFile' -count=1
--- PASS: TestMergeEveryLoopWritesNoStateFile (0.76s)
$ cat probe-audit-1.log
{"version":1,"ts":"2026-10-16T16:44:33Z","kind":"branch-delete","arm":"merge-run-spent-branch",
 "op":"branch:delete","pid":2853260,"repo":"…/001","target":"feat/loop-…","branch":"feat/loop-…",
 "evidence":"merge --ff-only landed the ref in master (a fast-forward cannot conflict, nothing is left to hand over)"}
```

The same record, reproduced against the **built binary** on a hand-made fixture
(`gopi merge --every 300ms --fast`, one fast-forwardable branch), so the claim does
not rest on the test fixture:

```
$ env GOPI_HOME=$P/home GOPI_SETTINGS=$P/settings.json GOPI_MEMORY_DB=$P/memory.db \
    $S/artifacts/gopi merge --every 300ms --fast     # SIGINT after 2.5s, exit 0
$ find $P/home -mindepth 1 | sed "s|$P/home/||"
.gopi / .gopi/sessions / .gopi/sessions/process-2853315 / …/signal-provenance.jsonl
.gopi/merge / .gopi/logs / .gopi/logs/removal-audit.log / .gopi/serve.lock
$ git -C $P/repo branch          # the prune the record describes, observed
* master
```

**The creator, with its emission point:**

| fact | value |
|---|---|
| record | `kind=branch-delete`, `arm=merge-run-spent-branch`, `op=branch:delete`, `pid` = the loop's own pid |
| emitter | `internal/removalaudit/audit.go:543` (`emit`) → opens the sink at `audit.go:574`, `MkdirAll`s `logs/` at `audit.go:569` |
| sink path | `internal/removalaudit/audit.go:599` (`sinkPath`) = `<gopi dir>/logs/removal-audit.log` |
| **caller** | **`cmd/agent/merge_run.go:2504`** (`mergeDecideBranch` arm ②, `removalaudit.BranchDelete(ArmMergeRunSpentBranch, …)` after `merge --ff-only` landed the ref); arms ① and ③ are the same emitter at `merge_run.go:2487` and `merge_run.go:2608` |

So the file is the merge pass's own audit of a ref it pruned — a deletion it really
performed — recorded under the profile it was given. Not a state file, and not
`internal/signalrec`.

**A second, unlisted fact found by the same probe** (Rule 41): the sink's
**directory** is created by any process that runs a merge pass, but the FILE only
appears when a mutation is recorded. A loop whose fixture has nothing to prune
creates `.gopi/logs/` and no file — the 0-byte sink in the earlier report is what a
file created and not yet appended to looks like, and `.gopi/logs/` itself is a
directory the walk skips (as it skips every directory).

## The gap this change closes

The 2026-10-16 fix made the walk admit `<profile>/.gopi/logs/removal-audit.log`
**by name**. Nothing asserted anything about what was inside it: a sink holding a
line written by another process, a sink holding a foreign arm, or an empty sink,
all read as "clean" to an instrument whose subject is *this command leaves no state
behind*. A tolerance granted by a file name is exactly the shape Rule 13 warns
about — the gate got wider without anything measuring the thing it is now allowing.

The fix keeps the walk's two tolerances unchanged and adds the missing half:

- `mergeProfileOffenders` (unchanged) — which PATHS may survive.
- `mergeOwnRecordOffenders` (**new**, `merge_every_child_test.go`) — reads the
  tolerated sink through `removalaudit.Records` and refuses the profile unless every
  record in it is this run's own evidence: `kind == KindBranchDelete`,
  `op == OpBranchDelete`, `arm ∈ {ArmMergeRunSpentBranch, ArmMergeRunProbeGround}`
  (built from the emitter's own constants), `pid` equal to the loop's pid, and a
  non-empty `repo`.
- **An absent or empty sink is refused in the same direction.** The tolerance's
  stated ground is "the pass wrote its own record here"; if no record exists, the
  tolerance is backing nothing and the assertion would be green against a file a
  stranger created.
- `TestMergeResidueWalkHasOneDefinition` now also pins the *call* to the content
  half (producer definition + call-site count + arity, READ FROM THE PARSE TREE),
  so removing the assertion cannot leave the walk tolerating an arbitrary sink with
  every other count still green.

`TestMergeOwnRecordCheckKeepsItsTeeth` is the falsifier, and it asserts BOTH
directions in one comparison: a record written through the **real emitter** at its
default path is admitted, while seven near-misses are refused — absent, empty, a
foreign `kind`, a foreign `op`, another subsystem's arm, another pid, and no repo.
Each refused arm differs from the admitted record in exactly one field.

## Not done / honest limits

- `TestMergeEveryKilledLoopLeavesNoResidueInTheProfile` is classified `heavy` and
  **skips in this run** (`heavytest.Require` refuses an inherited work context, by
  design). Its assertion is the same walk, so this change's effect on it is
  unmeasured; the route is the operator's `GOPI_HUMAN_TERMINAL=1 go test -run …`.
- The `--every` loop's stop path (`merge_stop.go`) publishes a state record under
  `<profile>/.gopi/merge/` and removes it on exit; the walk skips directories, and
  that directory was observed EMPTY after the loop in the probe above. It is not
  covered by the content half, and this change does not add a check for it.
- The scenario's green depends on its fixture pruning a branch (that is what
  produces the record). A future fixture change that leaves nothing to prune would
  fail the new assertion LOUDLY rather than silently — which is the intended
  direction, but it does mean the scenario now needs its fixture to stay
  prune-worthy.
