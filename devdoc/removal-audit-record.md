<!-- doc-class: historical -->
# Removal Audit Record — every removal names its arm (2026-09-18)

## The incident this closes

Two live-worktree-removal incidents (2026-09-17, 2026-09-18) destroyed the
working directories of task rows that were **still in progress**. In both
post-mortems the operator went to `~/.gopi/logs/` first —
`merger.log`, `worker.log`, `merger-takeover.log` — and came away with nothing
attributable: no arm, no path, no branch, no row id, no deciding evidence. The
destruction was recorded nowhere; the removal was, mechanically,
**unattributable**.

The sibling branch `fix/live-row-session-veto` (task `01a0af77`) closes the
removal CLASS: it adds the guards (`worktreeSweepSessionLiveEvidence`,
`reclaimLiveSessionRefusal`, `branchDeleteOrphanRefusal`) that decide *whether* a
removal may happen. This branch does the other half — it makes the removals that
**do** happen *readable afterwards*. One record per mutation, emitted BEFORE the
mutation runs.

## The record

One line of JSON, appended to `<gopi data root>/logs/removal-audit.log`:

```json
{"version":1,"ts":"2026-09-18T16:42:07.123456789Z","kind":"worktree-removal",
 "arm":"worktree-sweep-reclaim","op":"worktree:remove","pid":4711,
 "repo":"/home/user/work/GoPi","target":"/home/user/workspace/gopi-abc",
 "branch":"feat/abc","task_id":"01a0af50",
 "evidence":"unowned, unheld, clean, past the age floor"}
```

- **`arm`** is the field the incidents lacked: a named constant
  (`removalaudit.ArmSweepReclaim`, …, 16 arms) identifying the code path that
  decided to delete. Never a literal (Rules 1b/13/14).
- **`evidence`** carries the deciding fact when the arm has one (the veto
  channel a guard did not trip, the ancestry probe that succeeded, the
  registration that was dead). The channel names are constants
  (`EvidenceChannelLiveSession`, `EvidenceChannelLiveHolder`,
  `EvidenceChannelLedgerClaim`).
- **The attributing core is never omitted** (`version`, `ts`, `kind`, `arm`,
  `op`, `pid`, `target`); only the optional fields carry `omitempty` (Rule 15a).

### Why the sink is `logs/removal-audit.log`

That directory is where the post-mortem went first. One file, but every line
carries the five attributing facts the three component logs could not supply.
`GOPI_REMOVALAUDIT_LOG` overrides the path (that is how the tests pin a sink of
their own). With no override and no resolvable gopi data root the record is
**REFUSED and printed to stderr** naming the reason — never silently dropped,
never written to a guessed path (Rule 15).

### Why it is emitted BEFORE the mutation

An attempt that git then refuses still left the attempt recorded; a process
killed mid-removal has already written the line that explains the half-removed
state. A record written *after* the mutation explains nothing about a crash
inside it — which is the shape that loses the arm. The gate enforces the
ordering (see below).

Recording is **best-effort by contract**: `Emit` returns nothing, and a failure
to write the evidence never fails the removal the evidence exists to explain.
It never panics (Rule 19).

## What changed

| File | Change |
|---|---|
| `internal/removalaudit/audit.go` | **new** — the emitter: `Record`, 18 `Arm` constants, 3 `EvidenceChannel` constants, both emit calls, the sink resolution, the read-back `Records`. Zero-dependency leaf (stdlib + `internal/config`). |
| `internal/tools/task_merge.go` | 2 emit sites (already-contained ref; post-landing prune) |
| `internal/tools/repo_sweep.go` | 2 emit sites (ancestor lane; fast-forward lane) |
| `internal/tools/task_merge_executor.go` | 2 emit sites (ancestor; post-commit) |
| `internal/tools/task_adoption.go` | 2 emit sites (`-D` already-integrated; `-d` after the clean merge) |
| `internal/tools/task_finish_cleanup.go` | 4 emit sites (server worktree; `--force` worktree; branch `-d`; branch `-D`) |
| `internal/tools/task_merged_cleanup.go` | 3 emit sites (worktree; branch; orphan-lane branch) |
| `internal/tools/worktree_sweep.go` | 2 emit sites (reclaim; dead-registration branch) |
| `internal/tools/task_reclaim.go` | 1 emit site (row reclaim) + `reclaimVerdict.String()` so the record's disposition is a NAME, not a bare integer |
| `internal/tools/task_reclaim_self.go` | 2 emit sites (own finished row; own-repo unowned registration) |
| `internal/tools/tasks.go` | 1 emit site (the verifier's private ground) |
| `internal/tools/merge_build_gate.go` | 16 call sites funnelled through ONE `mergeBuildGateRemoveWorktree`, which emits — the gate's removal cannot be forgotten at one of its four call sites |
| `cmd/agent/merge_run.go` | **added at the rework** — the foreground `gopi merge` run's 3 sites: the ① spent-ref prune (`ArmMergeRunSpentBranch`), the ② post-fast-forward prune (same arm — one decision, "this ref owes nothing"), and the conflict PROBE's own detached worktree (`ArmMergeRunProbeGround`). It is `package main`, so nothing gated it before: the branch's gate found all three the moment it ran over current master |
| `internal/standards/removal_audit_gate_test.go` | **new** — the structural gate + its RED proofs + the data report |

### The one reviewed exception

`reclaimGitRemove` — the shared low-level runner — carries
`// removal-audit-ok: shared runner — every caller emits its own record …`. One
record per removal, never two: an emit at that depth would have to *guess* the
arm, which is the defect this epic removes. Every caller (sweep-reclaim,
row-reclaim, self-reclaim, self-own-sweep) emits its own.

## The gate (Rule 13's exhaustive pattern)

`internal/standards/removal_audit_gate_test.go` does not read a hand-written
list of arms — a list IS the hole (measured 2026-09-16 in the cwd-release
census: three arms called while listed nowhere). It walks every production file's
AST, resolves the string arguments of every call through the tree's own const
declarations (so `finishCleanupWorktreeRemoveVerb` reads as `"remove"`, one alias
hop to a fixpoint), and classifies any call carrying

- `worktree` + `remove` → a **worktree-removal** site,
- `branch` + `-d`/`-D`/`-r` → a **branch-delete** site.

A site is satisfied only when its enclosing function (or innermost function
literal — closures count) holds the **matching** emit **before** the call.

Three anti-green-blind mechanisms:

1. **Non-vacuity floor** — the walk must classify ≥ `15` sites, else the
   predicate is reading a tree it does not understand and a green verdict is
   meaningless.
2. **Reverse direction** — every `Arm*` constant the emitter declares must be
   referenced by a production emit; a dead arm silently stops covering its path
   while the name still reads like coverage.
3. **The delete-a-producer probe** (`TestRemovalAuditGateSeesTheRealTree`) — it
   takes a REAL production file, removes the real emit's statement, and requires
   the predicate to report the site. Without it, "the tree is clean" could be a
   predicate that reports nothing at all.

`TestRemovalAuditGateRedProof` pins the predicate's precision on synthetic
sources: 11 probes — no-record RED, record-precedes GREEN, record-AFTER RED, a
`WorktreeRemove` record not covering a branch site RED, escape GREEN, a file-head
escape RED (the release window is the CONTIGUOUS comment block the author
attached to the statement, resolved from the parsed comment groups, so a marker
that floats at the top of a file releases nothing), closure GREEN, `worktree
list` GREEN, `checkout -d` GREEN.

Measured census (the data report prints it every run):

```
removal sites: total=23 map[emitting:22 escaped:1] (floor 15)
  kinds=map[branch-delete:15 worktree-removal:8]
declared arms with no production emit: [] (floor 15 arms referenced)
```

## Two true positives the gate found on its first run

This is the part worth keeping: the gate was written over code that had *already*
been instrumented site by site, and it still found two sites the human pass had
missed.

1. `internal/tools/worktree_sweep.go:1858` — `worktreeSweepReapBranch` emitted its
   record on a line *below* the `worktreeSweepObserve` call, so the emitting
   statement was not the function's first bookkeeping statement. Fixed by moving
   the record to the TOP of the function: the record now precedes every
   statement of the deletion, which is the stronger contract.
2. `internal/tools/task_reclaim.go:1801` — the shared runner, the reviewed
   exception above.

Neither was visible to a reader; both are visible to the gate. That is the whole
argument for the gate existing rather than the emitter alone.

### The gate's catch at the rework

The gate found a third class of gap when it was first run over the post-merge
master: `cmd/agent/merge_run.go` — the foreground `gopi merge` command, landed by
a sibling epic *after* this branch was cut — carried 3 removal sites (2×
`branch -d`, 1× `worktree remove --force`) with no record. It is `package main`,
so the emitter's own package and the `internal/tools` instrumentation could not
have covered it, and nothing else in the tree gated it. All three now emit
(`ArmMergeRunSpentBranch` ×2, `ArmMergeRunProbeGround`), which is what took the
census from total=20 to total=23. This is the gate working exactly as designed on
code it had never seen — and the reason the relanded branch is not a
mechanical replay of the archived patch.

## Not Done (honest)

- **`ArmFinishCleanupBranch`, `ArmTaskMergeBranch`, `ArmMergeExecBranch`,
  `ArmRepoSweepBranch`, `ArmAdoptionBranchForce/Clean`, `ArmMergedCleanupOrphanBranch`,
  `ArmRowReclaim`, `ArmSelfReclaim`, `ArmVerifyGround`, `ArmMergeBuildGateGround`,
  `ArmFinishCleanupForce`, and the rework's `ArmMergeRunSpentBranch` /
  `ArmMergeRunProbeGround` have no per-path RED/GREEN test on their own runner.**
  They are covered two ways: (a) the emitter's own tests prove the record shape
  and the RED/GREEN mechanism for both emit calls, and (b) the structural gate
  proves every one of the **21** declared arms is referenced by a production emit
  that sits BEFORE its mutation — i.e. the emit is *there*, in the right place,
  on the right kind. What is NOT proven per-arm is that a specific runner reaches
  its emit at runtime (e.g. `task_adoption.go`'s `-D` lane, `merge_build_gate.go`'s
  four call sites, and the rework's `cmd/agent/merge_run.go` sites — a `gopi
  merge` end-to-end fixture on a real multi-repo settings table is its own task).
  The seven arms with a per-path test (`ArmSweepReclaim`,
  `ArmSweepDeadRegistrationBranch`, `ArmFinishCleanupServer`,
  `ArmMergedCleanupWorktree`, `ArmMergedCleanupBranch`, `ArmTaskMergeBranch`,
  `ArmSelfOwnSweep`) were chosen because their fixtures already
  existed; the remaining arms have fixtures only inside large end-to-end tests
  whose setup is not extractable in this task's budget. The
  gate makes the omission *safe* (a new emit-less site fails CI) but not
  *complete* — a future task can promote the remaining arms to per-path tests.
- **No log rotation / size bound** on `logs/removal-audit.log`. It is an
  append-only file under the gopi data root; a rotation policy belongs with the
  other component logs' policy, not invented here. A record is ~300 bytes and
  removals are rare, so this is an unbounded-but-slow file, not a leak.
- **The record is not surfaced in any operator-facing view** (`gopi task list`,
  the notify events). It is a file an operator greps, which is what the incident
  post-mortem needed; wiring it into a summary view is a UI task.
- **`removalAuditSitesNoRecord` is empty** — no site needed it. The mechanism
  exists because a future AST-blind site may need a reason written down, and an
  empty map is a stronger statement than no mechanism.

## Verification

- Build: `go build ./...` — OK (exit 0); `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` — OK
- Vet: `go vet ./...` — clean (exit 0)
- Emitter unit + RED/GREEN: `go test -count=1 ./internal/removalaudit/` — ok 0.003s
- Gate + its RED proofs: `go test -count=1 -run TestRemovalAudit ./internal/standards/` — ok 2.590s
- Per-path RED/GREEN: `go test -count=1 -run 'EmitsTheAuditRecord|RemovalAudit' ./internal/tools/` — ok 6.978s
- Smoke (branch-scoped): `scripts/test-budget.sh -run 'TestMerge|TestRemovalAudit|TestMergerTakeover' ./cmd/agent/ ./internal/removalaudit/ ./internal/standards/` — see the task report's evidence section (`internal/cmd/agent`'s `merge_run_test.go` is the fixture the `cmd/agent` emits were added against)

## Rework (merge-run take-over)

The original branch merged clean against the PRE-run master but conflicted
against the post-merge master (`52090dc0`) — sibling merges landed first. The
whole series is archived at `~/workspace/merge-run-diffs/fix_removal-audit-record.patch`
(sha `4fcf6ced`); the reported conflict was
`internal/tools/task_adoption.go` + `internal/tools/task_finish_cleanup.go`, and
a third file (`internal/standards/baseline.go`) conflicted as well.

Relanded on a FRESH branch off master (Rule 23 — the failed branch is evidence,
never continued): `fix/removal-audit-record-rework`, produced with
`git apply -3` so the conflict was localized to the three `UU` files. No file
was resolved by picking a side wholesale — each was decided by comparing the two
blobs:

| file | master's side | branch's side | resolution |
|---|---|---|---|
| `task_adoption.go` | the ORPHAN guard (`branchDeleteOrphanRefusal`, 2026-09-18) inside the ②b already-integrated arm | the `ArmAdoptionBranchForce` emit | **both, guard first then emit** |
| `task_finish_cleanup.go` | import `procname` + `branchDeleteOrphanRefusalAt` delivery-point guard | import `removalaudit` + the `ArmFinishCleanupBranchForce` emit | **both imports, guard first then emit** |
| `baseline.go` | the entry is RETIRED — the F6 fix (`01a0b1b3`) replaced the `} else if live {` chain with flat guards | re-key of that same permit `346`→`348` | **master's side** (the re-key's premise is void) |

**Why guard-first-then-emit is the only correct order here, not a stylistic
choice.** The orphan guard is a *refusal* — it `continue`s (`task_adoption.go`)
or `return`s (`task_finish_cleanup.go`) and the delete never runs. The record is
defined as "emitted BEFORE the git mutation, so an attempt git refuses is
recorded too": an emit placed after a refusing guard would not exist at all
(dead code), and the one thing this package exists to make readable — the removal
that *was* attempted — would be the removal of the `-D` that now follows the
guard. Guard first, emit after, delete third is exactly the contract
`removal_audit_gate_test.go` checks (emit must precede the `branch -D` site).

**Why the baseline takes master's side.** The branch's second commit re-keyed
`elseAfterReturnBaseline` `internal/tools/task_adoption.go:346`→`:348` on the
premise that its own emit pushed the `} else if live {` chain two lines down.
That premise no longer holds on this master: the chain is gone (F6 replaced it
with `if live { continue }` flat guards), so the permit is retired, not shifted.
Re-keying a retired site would leave both a stale permit and a phantom one —
Rule 13's stale-permit error, which the gate reports red. On the relanded tree
this file is **byte-identical to master**: the archived series' second commit
has no content of its own — its `baseline.go` re-key is dropped, and the
decision is recorded in the feature commit's message and here rather than as an
empty commit.

### The second relanded commit: the docclass manifest row

`internal/standards`' doc-class gate (`TestDocManifestCoversEveryDevdocFile`)
requires every file under `devdoc/` to have a row in `devdoc/DOC-INDEX.tsv`. That
gate was **already red on master before this branch** — measured on the clean
master checkout: `devdoc/DOC-INDEX.tsv: 2012 rows cover 2041 files under devdoc/`
and the gate names 25 unclassified files (+4 more in its own "and N more" line =
29). This branch's own `devdoc/removal-audit-record.md` is one of them, so without
a row the relanding would add a 30th.

**Scope taken: one row, for this branch's own doc — not the other 28.** The
missing rows are the standing defect of whoever last edited `devdoc/`, and the
generator is whole-corpus: `tools/docclass/classify.py` emits one row per file,
re-anchoring every existing row's evidence. **Measured** — running it in this
tree produced `devdoc/DOC-INDEX.tsv | 139 +++++---` (84 insertions / 55
deletions) for the benefit of 29 rows, and it introduced a NEW red on a file this
branch does not own (`devdoc/web-menu-keyboard-fix.md`: class `historical`, no
banner — `TestDocClassBannerMatchesManifest` passes on master and fails after the
re-run). Folding that into a removal-audit relanding would put a corpus-wide doc
churn inside a feature diff for no reason and make the rework's conflict surface
unbounded. So the second relanded commit
(`docs(docclass)`) adds **exactly one** manifest row plus the required
`historical` banner line in this doc — measured after it: `2013 rows cover 2042
files`, the same 29 unclassified files as master, i.e. **no net change**. The
other 28 stay red and are dispatchable (Rule 26) — a corpus-owning task can
re-run `classify.py`, which restores them with no other delta.

## Recovery

The original branch ref was archived by the merge run and no longer exists in
the main repo; the relanded branch is the live one:

```bash
git show fix/removal-audit-record-rework:devdoc/removal-audit-record.md
git show fix/removal-audit-record-rework:internal/removalaudit/audit.go
git diff --stat master..fix/removal-audit-record-rework   # the 18 files this lands

# the pre-rework series, for comparison (2 commits, 17 files):
git apply --stat ~/workspace/merge-run-diffs/fix_removal-audit-record.patch
```

### The resolution command

```bash
# 1. a FRESH worktree off the current master (Rule 23 — never continue the
#    failed branch):
git worktree add ~/workspace/gopi-removal-audit-rework -b fix/removal-audit-record-rework master

# 2. the archived series, applied 3-way so only the real conflicts stop the
#    apply (17 files, 2 commits):
cd ~/workspace/gopi-removal-audit-rework
git am -3 ~/workspace/merge-run-diffs/fix_removal-audit-record.patch
#    → CONFLICT (content): internal/standards/baseline.go,
#      internal/tools/task_adoption.go, internal/tools/task_finish_cleanup.go
#    → resolve each by the table above (never "ours"/"theirs" wholesale),
#      `git add` the three, `git am --continue`

# 3. what master's side could not have told the patched tree: the gate's own
#    census over CURRENT master — it found cmd/agent/merge_run.go's 3
#    unattributable sites (a sibling's package main file), which are armed here.
go test -run TestRemovalAudit ./internal/standards/

# 4. gates + scoped smoke (see ## Verification), then commit.
```
