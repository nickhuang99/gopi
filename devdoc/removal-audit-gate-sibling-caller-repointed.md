<!-- doc-class: historical -->
# The removal-audit gate's sibling caller file: retired mechanism, re-pointed probe

task `01a18eb2-af3f-78f3-9d8d-bd9481f12fdd` · branch `task/01a18eb2af3f78f39d8dbd9481f12fdd`
· host hosta · ground cut at `0cc0d262`

**Measured at `0cc0d262`** (the ground's own tip — Rule 38: this file's figures are
readings of THIS tree, not of the revision the briefing named, which was five
commits behind).

## What broke, and how it was measured

`internal/standards/removal_audit_gate_test.go` held one constant naming a
production file by literal path, and two named gates read it:

```
removalAuditSiblingCallerFile = "internal/tools/task_finish_cleanup.go"
```

Commit `58b5618b` — *"refactor(task): retire the finish-cleanup mechanism
(slice 3/4)"* — deleted that file. Both arms therefore died before measuring
anything, and the failure was a READ, not a verdict:

```
$ go test -count=1 -run 'TestRemovalAuditGateShapeList|TestRemovalAuditGateSameKindSibling' ./internal/standards/
--- FAIL: TestRemovalAuditGateSameKindSiblingIsLoadBearing (0.00s)
    removal_audit_gate_test.go:1818: read internal/tools/task_finish_cleanup.go: open /home/user/workspace/01a18eb2-af3f-78f3-9d8d-bd9481f12fdd/internal/tools/task_finish_cleanup.go: no such file or directory
--- FAIL: TestRemovalAuditGateShapeListIsLoadBearing (0.00s)
    removal_audit_gate_test.go:2034: read internal/tools/task_finish_cleanup.go: open /home/user/workspace/01a18eb2-af3f-78f3-9d8d-bd9481f12fdd/internal/tools/task_finish_cleanup.go: no such file or directory
FAIL
```

`git ls-tree HEAD internal/tools/` holds no `task_finish_cleanup.go`, so the
constant named a path absent from the tree it claims to measure.

## The Rule-43 judgement: the PAIRING moved, so the probes are re-pointed — not deleted

Rule 43 says the judgement is made from the reference distribution of the
symbols a mechanism held, not from the mechanism's file name. The applied form
here: **the property these two arms assert is still produced by the tree.** The
property is *one emit statement per branch-delete site, read in source order*,
in a file the two `removalaudit.BranchDeleteAt` calls and their emits both live
in.

That was measured, not assumed. Every production file calling
`removalaudit.BranchDeleteAt(` was walked (the gate's own `goFilesUnder(prodRoots)`
predicate) and each one's call count, emit count, branch-site count and
declared-matcher verdict were read off the real source:

| file | calls | emits | branch sites | other sites | declared unrecorded |
|---|---|---|---|---|---|
| `cmd/agent/merge_run.go` | 2 | 2 | 2 | 1 | `[]` |
| `internal/tools/archive_ref_reclaim.go` | 1 | 1 | 1 | 0 | `[]` |
| `internal/tools/task_merged_cleanup.go` | **2** | **2** | **2** | 1 | `[]` |
| `internal/tools/worktree_sweep.go` | 1 | 1 | 2 | 1 | `[]` |

Only `task_merged_cleanup.go` holds the retired file's topology: **two sibling
calls, two emit statements, two branch-delete sites**, and neither arm constant
is a prefix of the other (`ArmMergedCleanupBranch` / `ArmMergedCleanupOrphanBranch`),
which is what lets a needle pick one record out of the source. `worktree_sweep.go`
is the file that could not substitute for it: its emit passes an `arm`
VARIABLE, so no needle can select one of its records, and its two branch sites
share one emit.

So the constant was re-pointed, not floored to zero and not skipped:

```go
removalAuditSiblingCallerFile = "internal/tools/task_merged_cleanup.go"
removalAuditSiblingArm        = "ArmMergedCleanupBranch,"
removalAuditSiblingArmForce   = "ArmMergedCleanupOrphanBranch,"
```

Verified with the acceptance commands (the path exists in HEAD, and HEAD's own
tree carries the emits):

```
$ git ls-tree HEAD internal/tools/task_merged_cleanup.go
100644 blob d44131ba510d8c42ada5a9f557bc87c7183e615c	internal/tools/task_merged_cleanup.go

$ git grep -n "removalaudit\.BranchDeleteAt(removalaudit\.ArmMergedCleanup" HEAD -- internal/tools/task_merged_cleanup.go
HEAD:internal/tools/task_merged_cleanup.go:638:		removalaudit.BranchDeleteAt(removalaudit.ArmMergedCleanupBranch, mainDir, branch, "",
HEAD:internal/tools/task_merged_cleanup.go:751:	removalaudit.BranchDeleteAt(removalaudit.ArmMergedCleanupOrphanBranch, mainDir, branch, "",
```

## The second defect the re-point SURFACED — a hard-coded single-site expectation

Direction (3) of `TestRemovalAuditGateSameKindSiblingIsLoadBearing` paired *all*
the arm records with *ordinal 0* of the branch sites:

```go
want := removalAuditSiteKeyOrdinal(t, removalAuditSiblingCallerFile, bothRemoved, removalAuditKindBranch, 0)
```

That is correct only for a file holding exactly one branch-delete site — which
the retired file did (its own comment records the 2026-10-14 change to a single
`branch -d`). The successor holds **two**, so a single-site expectation asks the
predicate to report one site while the source hands it two. Measured against the
successor, the old form fails on a CORRECT tree:

```
CAND internal/tools/task_merged_cleanup.go  emits=2 want=internal/tools/task_merged_cleanup.go:651
                                            got=[...task_merged_cleanup.go:651 ...task_merged_cleanup.go:750] MATCH=false
```

The tempting repair — floor the count to 1, or skip the direction — is exactly
what the epic forbids, and it would also have made the direction stop proving
anything. The expectation is now DERIVED from the mutated source's own
enumeration (`removalAuditSiteKeysOrdinal`, the plural of the existing
single-ordinal helper): every branch site the mutated source holds is what the
deleted records must uncover, and nothing else.

## The measured result

```
$ go test -count=1 -v -run 'TestRemovalAuditGateShapeList|TestRemovalAuditGateSameKindSibling' ./internal/standards/
=== RUN   TestRemovalAuditGateSameKindSiblingIsLoadBearing
    removal_audit_gate_test.go:1949: RED (sibling renamed in the definition only): [cmd/agent/merge_run.go:2709 cmd/agent/merge_run.go:2963 internal/tools/archive_ref_reclaim.go:713 internal/tools/task_merged_cleanup.go:653 internal/tools/task_merged_cleanup.go:754 internal/tools/worktree_sweep.go:2665 internal/tools/worktree_sweep.go:2671]
    removal_audit_gate_test.go:2015: RED (all records deleted): [internal/tools/task_merged_cleanup.go:651 internal/tools/task_merged_cleanup.go:750]
--- PASS: TestRemovalAuditGateSameKindSiblingIsLoadBearing (5.75s)
=== RUN   TestRemovalAuditGateShapeListIsLoadBearing
    removal_audit_gate_test.go:2109: declared list → [] unrecorded | legacy single-spelling → 2 unrecorded [internal/tools/task_merged_cleanup.go:653 internal/tools/task_merged_cleanup.go:754]
--- PASS: TestRemovalAuditGateShapeListIsLoadBearing (1.23s)
ok  	github.com/nick/gopi/internal/standards	7.565s
```

Both arms are still **load-bearing**, and the log lines are the evidence rather
than the PASS: the rename direction still uncovers the real sites (7 of them,
across all four caller files), the delete-records direction reports exactly the
two sites it mutated, and the legacy single-spelling matcher still reproduces
the resident red on the very tree the declared union passes on.

Whole family, same tree:

```
$ go test -count=1 -run 'TestRemovalAudit' ./internal/standards/
ok  	github.com/nick/gopi/internal/standards	16.380s
```

## What was NOT done (honest)

- **`ArmFinishCleanupBranch` / `ArmFinishCleanupBranchForce` still exist** in
  `internal/removalaudit/audit.go` with no production emit left — the only
  reader is `finishCleanupOwnRemovalArm` in
  `internal/tools/finish_cleanup_survivors.go:6072`, the reclaim-side predicate
  that still has to RECOGNISE records the retired chain wrote. That is a
  different question from this one (a retired ARM kept for reading a historical
  sink is not a broken gate), and it is recorded here rather than acted on,
  because deleting an arm readers still compare against is not this task's
  call.
- **`PROJECT.md` still cites the deleted path** at lines 479 and 481
  (`finishCleanupRemovalReason`, `internal/tools/task_finish_cleanup.go:1503`
  and `:1041`), and `§4.5`'s push step names `task_finish_cleanup.go`. Those are
  a normative page pointing at a file that no longer exists — the same class of
  defect as the one repaired here, in prose rather than in a gate. Not repaired
  here: re-pointing a rule's own anchors needs the replacement implementation
  chosen by measurement, and it is a separate, dispatchable piece of work.
