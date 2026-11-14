<!-- doc-class: historical -->
# Repairing master's five red test families in ./internal/tools (2026-09-28)

Task `01a0e693-6996-7b8b-a550-b1a80380c46a`, branch
`task/01a0e69369967b8ba550b1a80380c46a` off master `4c3aebb8`.

**Post-sync note (2026-09-28 14:53:52).** While this run was in flight, master
moved to `9fb9f8e8`, which **merged the prior attempt**
(`task/01a0e6834bf07d85ae558b6c32ea7382`, `c41a49ab`). The branch was therefore
re-synced onto it — `67625ee0` — and six conflicts were resolved in favour of
this branch's side (the fix landed here first, re-derived), with this run's
duplicate push-fence table dropped in favour of master's
`task_finish_cleanup_localside_test.go`. All nine targets were re-observed green
after that resolution, and the acceptance budget run was re-made on the synced
tree. Everything below describes the re-derived fix, which is what the branch
carries.

**Goal**: `make test-budget PKGS='./internal/tools'` exits 0 on a fresh branch.
Master was red with nine failing targets in five families, each a gate/production
seam drift rather than a broken behaviour.

**Method (Rule 23 + the triage criterion).** A prior repair attempt exists as
branch `task/01a0e6834bf07d85ae558b6c32ea7382` (`c41a49ab`, `08322a50`); it has
never been merged and is not on master. It was read as *information*
(`git diff master...task/01a0e683…` → an evidence file in the session dir), and
every fix was then **re-derived on this branch from the failing run's own
output**. Each family was diagnosed with three instruments — the gate's own
failure text, the anchored symbol's `git log`/blame history, and the production
caller's real control flow — and the side the evidence convicted was the only
side changed. Per the measured precedent (memory #6553/#6559, the same failure
family on this same package), the four shapes are: SEAM-MIGRATION (production
re-typed a value), GATE-ANCHOR-DRIFT (gate pinned a byte shape), and
FIXTURE-ENV (fixture asked a policy question about a path the policy cannot see).

## Family 1 — two negative-totality assertions went UNCLASSIFIED

`TestAssertionTotalityAuditGate` reported two sites and demanded a
classification; the remedy is the justified row, **never a relaxed predicate**.

| site | why the bundled claim is correct |
|---|---|
| `cmd/agent/memory_sync_tick_test.go:TestMemorySyncTickWithoutDeclaredPeerSpawnsNothing` (:248) | the set is a **row/set count at the input**. `memorySyncTickRun` (`cmd/agent/memory_sync_tick.go:266`) reads the topology and **returns at :286** when `len(peers)==0`, *before* the loop whose only act is `memorySyncTickSpawn(argv)` (:294) — so the spawn site is unreachable on this path and zero calls is the arm's own work. The refusal is reported and names `config.MemoryPeersSettingsKey`. Positive polarity on a live fixture in the same file: `TestMemorySyncTickSpawnsOneChildPerDeclaredPeer` (`len(*calls)==2` for 2 peers) and `TestMemorySyncTickThrottleAndProcessStart` (1 then 2). |
| `internal/tools/chat_ground_test.go:TestChatGroundDegradesInANonRepoDirectory` (:222) | the set is the **entry list of a temp directory that was empty before the call** — evidence of ABSENCE, not a step list. `ChatGroundFor` returns `chatGroundDegrade(...)` at `internal/tools/chat_ground.go:142`, and `chatGroundDegrade` only fills a `ChatGroundResolution{Degraded:true,Detail:…}`: it creates no directory, links no worktree, writes no file. Positive carriers: `TestChatGroundCreatesTheSessionWorktree` / `TestChatGroundReusesTheSameSessionGround` (`chat_ground_test.go:62/:122`). |

Both rows name what the proof still **cannot** separate (the residual), as the
map's format requires.

## Family 2 — `taskIDNormalize` was re-inlined (SEAM-MIGRATION, production)

`TestTaskIDNormalizeIsTheSingleSeam` failed with
`tasks.go still trims task_id inline ("taskID = strings.TrimSpace(taskID)")`.
`taskFindSessionJSONLByTask` (`internal/tools/tasks.go:2465`) re-typed the raw
trim where the epic's contract is that every `task_id` entry point reaches the
one rule (`Rules 13/14`). **Fix: the call site, not the gate** —
`taskID = taskIDNormalize(taskID)`.

The red arm is master's **pristine export** (`d1f885a8`), measured by the filing
run: `grep -a '"Action":"fail"' run-1790574262-3243144.json` lists
`TestAssertionTotalityAuditGate`, `TestDefaultGitPushback…IntegratedPushesUnmerged`,
`TestDefaultGitPushback…NothingUnmergedPushesNothing`,
`TestTaskIDNormalizeIsTheSingleSeam`, `TestClaimOrderHasOneDefinition` and the
four `TestWorktreeSweepRefusesEvictionOfAPeerCheckout` entries — nine. This
run's own recon (`recon-gates.log:85-86`) shows it on the pre-fix tree:

```
--- FAIL: TestTaskIDNormalizeIsTheSingleSeam (0.01s)
    taskid_whitespace_normalize_test.go:223: tasks.go still trims task_id inline ("taskID = strings.TrimSpace(taskID)") — call taskIDNormalize instead (one seam)
```

## Family 3 — the claim-order gate anchored on a byte shape (GATE-ANCHOR-DRIFT)

`TestClaimOrderHasOneDefinition` failed with
`TaskList's pending view must reference taskClaimOrderBy`. The gate asserted
`strings.Contains(src, "query += taskClaimOrderBy")` — an **assignment
operator**, i.e. a spelling. The pending view's order became newest-first
(`4973fd52`), so the view's remaining reader of the queue order is
`taskNextClaimID` (`tasks.go:6010`) — the function that decides which row wears
`[next]`; its concatenation is hoisted onto one line, so the byte shape moved
while the seam did not.

**Fix: re-anchor the gate on the SITES, not the file.**

```go
if !strings.Contains(srcAtFunction(t, src, "taskNextClaimID"), "taskClaimOrderBy") { … }
if !strings.Contains(srcAtFunction(t, src, "TaskList"), "taskNextClaimID(") { … }
```

`srcAtFunction` slices one top-level function, so "the file contains this
string" — satisfied by any *other* function — is no longer enough. The
one-spelling count (`ORDER BY COALESCE(priority` == 1) is kept unchanged.

## Family 4 — the sweep fixture asked about a path the policy cannot see (FIXTURE-ENV)

`TestWorktreeSweepRefusesEvictionOfAPeerCheckout` failed in all three subtests
(`gone=false, want gone=true` ×2, plus the refusal-arm assertion). The
diagnosis is in the subtest's **own printed findings** — no speculation needed:

```
FINDING report declared-root-scope /…/001/self/wt | outside every declared root
  (consulted: worktree /home/user/workspace …, scratch /home/user/workspace/gopi-test-scratch,
   fixture /dev/shm/suite-tools-run-4049419583 …, session /…/001/self/repo)
```

The fixture's repo lives under the **parent** test's `t.TempDir()`, but the
child helper **process** re-enters this package's `TestMain`, which pins `TMPDIR`
to its own per-run suite root — a **sibling**. So in the child the registration
lay outside every host-resolved root and rung ① fired before the rungs the test
asserts. Structural, not a policy defect.

**Fix**: make the root list injectable — `worktreeSweepDeps.Roots`
(`internal/tools/worktree_sweep.go`), `nil` = resolve from the host
(`worktreeSweepRoots(repo)`), which is the production path and stays byte-identical
in behaviour; the fixture declares its own throwaway area. This is the same
manoeuvre as the ledger rows and holder list already injected beside it, and it
edits **no** rung, role or authorisation.

## Family 5 — the push fence mis-parsed a local colon target (production)

`TestDefaultGitPushbackSkipsIntegratedPushesUnmerged` and
`TestDefaultGitPushbackNothingUnmergedPushesNothing` failed with the fence's own
refusal:

```
test fence: a test process must not push a deliverable to a real node
(target=/dev/shm/suite-tools-run-4125617200/TestDefaultGitPushbackNothingUnmergedPushesNothing…/001/peer.git:~/work/GoPi)
```

`finishCleanupTargetInTestSpace` split every target on its **first colon**, so
the fixture's own bare repo had its local side read as the literal `~/work/GoPi`
— the **operator's real checkout** — and the fence refused a push whose bytes
would land entirely inside the suite's own temp root. It contradicted the
fence's own documented contract ("a fixture pushing to its OWN bare repo under
the test temp root is inside test space and unaffected", `gitPushbackPlan`).

**Fix**: `finishCleanupLocalSide` applies **git's own rule** (the `clone(1)`
scp-like-syntax rule): the `host:path` spelling is recognised *only when no
slash precedes the first colon*. So a target carrying a path spelling before
its first colon is ONE local directory name — and the fence then measures that
name against the temp roots. The refusal direction is unchanged: `alias:~/work/GoPi`
still has no local spelling before its colon, so it still leaves test space.

**Why this is not "widening the allow"**: the fence's contract is about WHERE
THE BYTES LAND; the pre-fix code was answering a different question (it measured
a string it had mis-parsed). Both directions are now pinned by tests, and the
fence's pre-existing positive control
(`TestPushbackTestFenceAllowsAFixturesOwnTempRepo`) is the third observer.

## Red proofs shipped

| test | pins |
|---|---|
| `TestFinishCleanupLocalSideReadsAGitTarget` | the parse rule, 7 shapes: fixture local target (whole string), tilde/dot spellings, colon-in-a-name, no colon, and `alias:~/work/GoPi` still non-local |
| `TestFinishCleanupTargetConfinementLocalColonTarget` | the same at the production entry: inside temp root ⇒ allowed, real node ⇒ refused |
| `TestWorktreeSweepInjectedRootsBringARegistrationIntoPolicy` | an injected root brings the registration into policy ⇒ directory gone |
| `TestWorktreeSweepWithoutARootIsOutOfPolicyAndNotSwept` | the refusal direction: a root that does not reach it ⇒ directory survives, verdict names `declared-root-scope` |
| `TestWorktreeSweepProductionRootsPathIsUnchanged` | the zero value is `nil` ⇒ the production resolver runs (an empty non-nil list would silently disarm every rung) |
| `assertion_totality_allow_test.go` rows ×2 | the two classifications, each with its evidence and its residual |
| `tasklist_order_test.go` (`srcAtFunction`) | the re-anchored gate reads SITES, not the file |

Pre-existing red proofs already in the tree that now go green — i.e. they are the
independent observers of two of these fixes: `TestPushbackTestFenceAllowsAFixturesOwnTempRepo`
(family 5) and the three `TestWorktreeSweepRefusesEvictionOfAPeerCheckout` subtests
(family 4).

## Observations

* **The acceptance run was made twice, on both trees** (the post-sync one and the
  final branch tip), because the devdoc itself is part of the tree:
  `run-1790578646-3756576.json` (`67625ee0`, `elapsed=421s exit=0 outcome=pass`)
  and `run-1790579221-3847390.json` (`df0b689c` branch tip,
  `elapsed=367s load_start=9.29 10.90 11.00 exit=0 outcome=pass`), the second
  with **zero `fail` records in the whole stream** and all nine targets `pass`.
* `make test-budget` exports `GOPI_BIN` when `<repo>/bin/gopi` exists
  (`scripts/test-budget.sh:219`), which **un-skips** tests that a bare `go test`
  skips. A green `go test -run …` is therefore not automatically a green
  budget run — the budget run is the acceptance, and the two were run separately.
* **A budget run is not a baseline just because it started before your edit.**
  The package takes ~6 minutes, and some gates read the tree **at run time**
  (`taskid_whitespace_normalize_test.go:252` does
  `verifySrc, err := os.ReadFile("tasks.go")`), so a stream whose `"start"`
  record precedes an edit can still report the POST-edit verdict for that test.
  Measured here: a stream started 14:34:00, its first test record was 14:42:42,
  and F2's `tasks.go` edit landed 14:34:36 — its `pass
  TestTaskIDNormalizeIsTheSingleSeam` was post-fix and must not be read as
  evidence the target was green on master. Red arms come from a **pristine
  export** or from an isolated `-run '^Name$'` captured before the fix.
* Read a budget stream's per-test verdicts with the ACTION FIELD FIRST
  (`"Action":"pass","Package":…,"Test":…`): grepping
  `"Test":"X"…"Action"` matches nothing and yields a false "0 pass / 0 fail"
  that looks like the test never ran.
* `internal/tools`' child-process tests inherit a `TMPDIR` pinned by `TestMain`;
  any fixture whose paths must be visible to a child **must** pass them
  explicitly (family 4 is exactly that lesson, made a rule by the `Roots` seam).
