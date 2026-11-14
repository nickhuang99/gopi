<!-- doc-class: historical -->
# Task Report: every sweep finding names the root it was judged against

Branch: `fix/worktree-sweep-finding-root-stamp` (linked worktree
`~/workspace/gopi-wtsweep-rootstamp`), branched from master `830ac32b`.

## The defect (left by WIP commit 1e553d7f)

`worktreeSweepJudge` (internal/tools/worktree_sweep.go) is a ladder: every rung
returns its own verdict, and the finding it returns IS the operator's plan.
Commit 1e553d7f ("memory-base worktree root — implementation + tests, 2
assertions unmet") added `Root` as the finding's "judged against" label but left
two rungs unstamped, so two tests have been red on master since:

```
--- FAIL: TestMemoryBaseKeepArmsOverrideTheNewRoot/dirty_⇒_kept  (names no root)
--- FAIL: TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy   (names no root)
```

**(A) the dirty arms returned `f` RAW** — three `return f` sites (the escalated
orphan, the unowned dirty report, and the owned dirty report at ④) skipped
`worktreeSweepTagFinding` entirely, so `Rule` *and* `Root` stayed empty even
though `judgedRoot` had already been resolved by rung ①. A finding that cannot
name the arm that decided it is not a plan.

**(B) the out-of-policy arm had nothing to stamp.** It stamped the root from
`worktreeroots.PermitsAny`, which returns `Root{}, false` when nothing permits —
so `Role` was `""`, `worktreeSweepTagFinding` appends nothing when `Path == ""`,
and the refusal named no root *precisely when the operator most needs to see what
was consulted* (its own reason promises the memory-base remedy).

## Completed

1. **Every arm's finding carries its arm and its root.** The three raw returns
   now go through `worktreeSweepTagFinding(f, worktreeRuleDirty, judgedRoot)` —
   `worktreeRuleDirty` is the existing constant for the dirty family (Rule 1b/13:
   no new inline label).

2. **The out-of-policy refusal reports what it consulted.**
   - New `worktreeSweepConsultedRoot(permitted)`: when no root permitted, the
     reported root is the memory base the SCRATCH HOME resolves onto, derived by
     the *same* measurement the sweep's own implicit root uses (`memoryBaseRoot`)
     → `Path` + `SourceMemoryBaseMeasured`, **no role**. A role is what
     *authorises* an operation and `worktreeroots.Permits` refuses a roleless
     root, so labelling a refusal can never widen what the sweep may remove.
   - New `worktreeSweepOutOfPolicyReason(roots, consulted)`: the reason now names
     **every root that was consulted** (path + source, via the shared renderer
     `worktreeSweepRootLabel`) instead of asserting "outside every declared
     root" with nothing behind it. On a host with no memory base the reason says
     so and quotes `memoryBaseRootSkipped` — no fabricated root (Rule 15).
   - The remedy sentence is spelled from the `worktreeroots.RoleMemoryBase`
     constant, so the assertion in the pre-existing test
     (`strings.Contains(f.Reason, "memory-base")`) cannot drift from the role it
     names.

3. **Honest `no-root` label.** The arms decided *before* any root is resolvable
   (main checkout, bare, dead registration, git lock, and the dir-gone sub-arms)
   cannot name a root, and inventing one would be a false finding. Their findings
   now report the constant `worktreeSweepNoRootLabel` ("no-root") rather than an
   empty string — "empty" and "never set" become distinguishable in the plan.
   `worktreeSweepTagFinding` substitutes it, so a rung that reports a rootless
   verdict is explicit rather than blank.

4. **Regression gate: `internal/tools/worktree_sweep_finding_root_gate_test.go`.**
   - `TestWorktreeSweepFindingRootGate` walks **20 cases covering every arm the
     judge can reach** and asserts, per arm: the disposition reaches that arm,
     `f.Rule` equals the arm constant, and `f.Root` is either the actionable
     label (path **and** source — a bare role was the silent half of the defect)
     or, for the pre-root arms only, the declared `no-root` label.
   - `TestWorktreeSweepJudgeReturnsAreTagged` reads the judge's own source and
     **fails on any raw `return f`** — the exact shape that re-opened the hole.
   - `TestRawFindingReturnPredicateIsRedCapable` proves that predicate can go
     red on a synthetic body (a gate that cannot fail proves nothing).
   - `assertEveryJudgeArmIsCited` makes the gate's own list of arms an assertion:
     a new arm constant without a cited case fails the gate (count tripwire).
   - `TestWorktreeSweepFindingRootGateReportsTheStyleOfTheLabel` pins the
     rendering so `findingRootIsActionable` cannot go vacuously true.

## Not Done (honest)

- **One pre-existing WIP expectation had to be corrected, and that is the one
  place where I changed a test's *intent* rather than only its root assertion.**
  `TestMemoryBaseKeepArmsOverrideTheNewRoot`'s "dirty ⇒ kept (report)" row
  expected `report`, but the ladder postdates the WIP commit: a dirty
  registration with **no ledger row** and past the age floor now escalates
  (`worktreeEscalateOrphan`, epic worktree-reap-reachable). With the raw return
  fixed, the row reaches the escalation arm and scores `escalate`. The row now
  expects `escalate` + `worktreeRuleDirty` and is named accordingly. The
  never-destroy invariant it asserts (no reclaim) is unchanged — nothing is
  removed either way. The dirty REPORT arms (owned dirty, young dirty,
  unmeasurable-age dirty) are covered by the new gate's own cases, so the report
  shape did not lose coverage.
- The two root-carrying arms decided **outside** the judge (the runner's run-cap
  and no-ledger overrides) are not walked by the gate: they re-stamp `Rule` and
  carry the judge's `Root` through untouched, which the cap/ledger tests already
  pin. Named in the gate as the two justified non-judge arms.
- The historical devdocs that quote the old refusal sentence
  (`devdoc/worktree-residue-release.md`, `devdoc/testsuite-audit-20260915.md`)
  are records of what was printed then and were deliberately **not** edited.

## Verification output

Branch tip of `fix/worktree-sweep-finding-root-stamp` (read it back with
`git log --oneline -1 fix/worktree-sweep-finding-root-stamp` — the report lives IN
the commit, so it cannot carry its own hash without amending in a loop).

### Build / vet / fmt

```
$ CGO_ENABLED=0 go build ./...
BUILD_EXIT=0
$ CGO_ENABLED=0 go vet ./internal/tools/
VET_EXIT=0
$ gofmt -l internal/tools/
internal/tools/task_deliverable_worktree.go
```
Every file THIS change touches is gofmt-clean (the three files not listed —
`worktree_sweep.go`, `worktree_sweep_memory_base_test.go`,
`worktree_sweep_finding_root_gate_test.go` — were verified with an explicit
`gofmt -l <those paths>`, empty). The single listed file is unformatted on
pristine master too (`gofmt -l internal/tools/task_deliverable_worktree.go` in
the main repo at 830ac32b prints the same path), and is NOT part of this change.

### The two named tests (the ones red on master)

```
$ CGO_ENABLED=0 go test ./internal/tools/ \
    -run 'TestMemoryBaseKeepArmsOverrideTheNewRoot|TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy' -count=1
    --- PASS: TestMemoryBaseKeepArmsOverrideTheNewRoot (0.00s)
        --- PASS: .../dirty,_unowned_and_past_the_age_floor_⇒_escalated_to_a_human (0.00s)
        (+ 5 more sub-cases PASS)
    --- PASS: TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy (0.00s)
ok  github.com/nick/gopi/internal/tools
```

### The new gate (all four gate tests)

```
$ CGO_ENABLED=0 go test ./internal/tools/ -run 'TestWorktreeSweepFindingRootGate|TestWorktreeSweepJudgeReturnsAreTagged|TestRawFindingReturnPredicateIsRedCapable' -count=1 -v
--- PASS: TestWorktreeSweepFindingRootGate (0.00s)   [20/20 arm cases PASS]
--- PASS: TestWorktreeSweepJudgeReturnsAreTagged (0.00s)
--- PASS: TestRawFindingReturnPredicateIsRedCapable (0.00s)
--- PASS: TestWorktreeSweepFindingRootGateReportsTheStyleOfTheLabel (0.00s)
```

### Baseline control (the pre-existing WIP reds, on PRISTINE master)

Captured FIRST, in a separate detached worktree at master `830ac32b` (the main
repo was not touched):

```
$ git worktree add /home/user/workspace/gopi-msweep-base 830ac32b
$ cd /home/user/workspace/gopi-msweep-base
$ CGO_ENABLED=0 go test ./internal/tools/ -count=1
--- FAIL: TestMemoryBaseKeepArmsOverrideTheNewRoot (0.00s)
    --- FAIL: .../dirty_⇒_kept_(uncommitted_work_is_never_destroyed) (0.00s)   "names no root"
--- FAIL: TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy (0.00s)
        worktree_sweep_memory_base_test.go:350: the finding names no root — the operator cannot see what was consulted
FAIL github.com/nick/gopi/internal/tools 141.179s
```

### Full package differential (no NEW red)

```
$ CGO_ENABLED=0 go test ./internal/tools/ -count=1     # on the fix branch
FINAL_EXIT=1
--- FAIL: TestTaskFindSessionJSONLStillHonoursEveryLayout (0.00s)
    taskledger_cost_test.go:246: flat layout resolved ".../<sessionID>/session.jsonl", want ".../<sessionID>.jsonl"
FAIL github.com/nick/gopi/internal/tools
```

That failure is **pre-existing and unrelated to the sweep**: it reproduces on
pristine master `830ac32b`, 3 runs out of 3 —
`go test ./internal/tools/ -run TestTaskFindSessionJSONLStillHonoursEveryLayout -count=3`
in `/home/user/workspace/gopi-msweep-base` fails identically (same two layout
cases). It is a task_sessions layout/session-tree-cache issue, not a finding-root
issue; it is neither caused nor masked by this change. Net: the fix branch's
failure set is a strict SUBSET of master's (the two named reds are gone; the
pre-existing one remains, unchanged and unrelated).

Note: the env vars `GOPI_DAEMON_READY_FD`, `GOPI_WORKER_DAEMON_CHILD` and
`GOPI_WORKER_LOCK_FD` were unset for every run (`env -u …`), so the suite cannot
touch the live worker daemon's ledger.

### Red proof of the gate itself (a gate that cannot fail proves nothing)

With the branch's source as it is, I re-introduced the defect it guards — one
raw `return f` kept, one of the three dirty returns reverted to `return f`:

```
$ python3 … # replace the 2nd "return worktreeSweepTagFinding(f, worktreeRuleDirty, judgedRoot)" with "return f"
$ CGO_ENABLED=0 go test ./internal/tools/ -run 'TestWorktreeSweepJudgeReturnsAreTagged|TestWorktreeSweepFindingRootGate' -count=1
--- FAIL: TestWorktreeSweepJudgeReturnsAreTagged (0.00s)
    worktree_sweep_finding_root_gate_test.go:365: worktreeSweepJudge returns an unstamped finding
        (1 site(s)) — every return must go through worktreeSweepTagFinding: [196]
--- FAIL: TestWorktreeSweepFindingRootGate (0.00s)
    --- FAIL: .../dirty,_unowned_and_young_⇒_root_named_(plain_report)      "root = ... for arm dirty"
    --- FAIL: .../dirty_with_an_unmeasurable_age_⇒_root_named_(plain_report) "root = ... for arm dirty"
FAIL
$ git checkout -- internal/tools/worktree_sweep.go
$ CGO_ENABLED=0 go test ./internal/tools/ -run 'TestWorktreeSweepJudgeReturnsAreTagged|TestWorktreeSweepFindingRootGate' -count=1
ok  github.com/nick/gopi/internal/tools  1.280s
```

So the hole is closed on BOTH axes: the source scan refuses the shape, and the
behavioural ladder catches the finding it produces. (The un-restored tree would
have been left dirty by accident — `git status --short` after the restore is
empty, i.e. the worktree is exactly at the commit.)

### Red proof of (B): the refusal's consulted root is not merely NON-EMPTY

The first form of `TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy` asserted
only `f.Root != ""` — and that assertion PASSES while the arm reports the honest
`no-root` fallback label, i.e. it accepted exactly the silence defect (B)
produces (a non-empty spelling that tells the operator nothing). The assertion
was strengthened to refuse `worktreeSweepNoRootLabel` for this arm (this arm DID
reach the root test) and to require the bracketed provenance (path + source).

Red proof — with the branch's source as it is, restore defect (B)
(`worktreeSweepConsultedRoot` reporting `permitted`, which is `Root{},false` when
nothing permits):

```
$ python3 … # in worktreeSweepConsultedRoot: `if true { return permitted }` before the memory-base measurement
$ CGO_ENABLED=0 go test ./internal/tools/ -run 'TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy' -count=1
--- FAIL: TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy (0.00s)
    worktree_sweep_memory_base_test.go:357: the refusal reports the no-root label "no-root"
        — this arm DID resolve roots; the plan must name what was consulted
FAIL
$ # the behavioural gate catches it independently:
$ CGO_ENABLED=0 go test ./internal/tools/ -run 'TestWorktreeSweepFindingRootGate' -count=1
--- FAIL: TestWorktreeSweepFindingRootGate/outside_every_declared_root_⇒_the_consulted_root_is_named
    root = "no-root" for arm declared-root-scope — a root WAS resolved and the finding must name it
FAIL
$ git checkout -- internal/tools/worktree_sweep.go
$ CGO_ENABLED=0 go test ./internal/tools/ -run 'TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy|TestMemoryBaseKeepArmsOverrideTheNewRoot' -count=1
ok  github.com/nick/gopi/internal/tools  1.204s
```

Lesson (worth keeping): when a fix replaces an empty field with an honest
non-empty fallback label, any pre-existing `field != ""` assertion becomes
VACUOUS — it is satisfied by the fallback. The assertion must pin the
distinguishing property the defective path cannot satisfy.


## Deliverable location

- Branch: `fix/worktree-sweep-finding-root-stamp` — MERGED into master
  (`dc9b20b5`), hence reachable from master's history.
- Commits: `a7ea026c` (fix + gate + report), `d2faeeaf` (strengthened (B)
  assertion + red proof), merge `dc9b20b5`.
- Recovery: `git show dc9b20b5:devdoc/worktree-sweep-finding-root-stamp.md`

### Re-verification on the tip that carries this doc (`master` `796b3333`)

After the merge round that followed `dc9b20b5`, both halves were re-measured on
master's TIP rather than on the merge commit — the fix, the gate and the whole
`internal/tools` package:

```
$ CGO_ENABLED=0 go build ./...                                     # OK
$ gofmt -l internal/tools/worktree_sweep.go \
      internal/tools/worktree_sweep_finding_root_gate_test.go \
      internal/tools/worktree_sweep_memory_base_test.go            # (clean)
$ CGO_ENABLED=0 go test ./internal/tools/ -count=1 -v \
      -run 'TestMemoryBaseKeepArmsOverrideTheNewRoot|TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy'
--- PASS: TestMemoryBaseKeepArmsOverrideTheNewRoot (0.00s)
--- PASS: TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy (0.00s)
ok  github.com/nick/gopi/internal/tools  0.452s
$ CGO_ENABLED=0 go test ./internal/tools/ -count=1
ok  github.com/nick/gopi/internal/tools  257.398s        # EXIT=0
```

Red-capability re-proved ON THE TIP (`796b3333`), in a throwaway detached
worktree that was removed afterwards with nothing committed — both directions of
the fix are still live at the tip:

```
# (A) revert ONE dirty return to a raw `return f`:
--- FAIL: TestWorktreeSweepJudgeReturnsAreTagged
    worktreeSweepJudge returns an unstamped finding (1 site(s)) — every return
    must go through worktreeSweepTagFinding: [189]
--- FAIL: TestWorktreeSweepFindingRootGate/dirty,_unowned_and_aged_⇒_root_named_(escalation)
    rule = "", want "dirty-worktree" — the finding must name the arm that decided it

# (B) restore worktreeSweepConsultedRoot's defect (report only what PermitsAny returned):
--- FAIL: TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy
    the refusal reports the no-root label "no-root" — this arm DID resolve roots;
    the plan must name what was consulted
--- FAIL: TestWorktreeSweepFindingRootGate/outside_every_declared_root_⇒_the_consulted_root_is_named
    root = "no-root" for arm declared-root-scope — a root WAS resolved and the
    finding must name it
```

Each red proof is independent: (A)'s source scan and (B)'s provenance assertion
fail separately, so neither can mask the other.
