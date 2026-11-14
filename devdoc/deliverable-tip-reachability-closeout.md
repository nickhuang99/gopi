<!-- doc-class: historical -->
# The deliverable-tip reachability criterion at close-out (epic 01a13bbb)

A `completed` ledger row whose deliverable branch resolves to no ref used to be
indistinguishable from a delivered one. This document records the mechanism
measured on a REAL row, the criterion added at the close-out seam, its boundary
(what it refuses to do), and the two false-positive classes it is built to
avoid.

## 1. The incident, measured

Row `01a13ae1-2477-7490-b79b-24b8b8b2b705` (`status: completed`,
`deliverable.branch: task/01a13ae124777490b79b24b8b8b2b705`) on host
`hostb-Laptop`, repo `/home/user/work/GoPi`:

```
$ git cat-file -t 6446222744bc6442b71ec1348e6806563bfea54e
commit                                  # the OBJECT is alive
$ git for-each-ref --contains 6446222744bc6442b71ec1348e6806563bfea54e --format='%(refname)'
                                        # (empty) — no ref holds it
$ git rev-parse --verify --quiet refs/heads/task/01a13ae124777490b79b24b8b8b2b705
rc=1                                    # the ref is gone
$ git merge-base --is-ancestor 6446222744bc6442b71ec1348e6806563bfea54e master
rc=1                                    # master does not contain the work
$ ssh peerbox 'cd ~/work/GoPi && git for-each-ref --contains 6446222744… --format="%(refname)"'
                                        # (empty) on the delivery point too
```

The removal-audit sink (`~/.gopi/logs/removal-audit.log`) carries FOUR records
naming that branch, and the two branch-delete ones are the close-out chain's own:

```
18:24:42  worktree-removal  finish-cleanup-worktree-force
18:24:43  branch-delete     finish-cleanup-branch        sha=6446222744…  delivery_point=peerbox:~/work/GoPi
18:24:50  branch-delete     finish-cleanup-branch-force  sha=6446222744…  delivery_point=peerbox:~/work/GoPi
```

So the chain force-deleted the ref on the strength of a push landing the
DELIVERY POINT does not hold, and the row was written back `completed` later
with nothing carrying its work. The loss is silent: `gopi task branch-refs
--peer peerbox` renders that row exactly like a delivered one (`orphaned` is the
census reader's verdict — 449 of 1447 rows — but no close-out path ever consults
it, and it runs after the fact by hand).

## 2. The criterion

`internal/tools/task_deliverable_tip.go`, called from the ONE choke point every
harness close-out passes through: the delivery state gate in
`taskUpdateStampedCore` (`internal/tools/tasks.go`, beside
`reclaimRunResidueRefusal`), with its RECORD half on the writeback seam beside
`reclaimRunResidueAtWriteback`.

For a row whose deliverable records a branch, five reads, in order:

| # | question | surface (all EXISTING — Rule 35) |
|---|---|---|
| ① | does the branch still resolve to a ref? | `branchDeleteTipOf` (`branch_delete_guard.go`) |
| ② | what tip did the delete record? | `removalaudit.BranchDeleteRecords` (the sink's own reader) |
| ③ | does a DURABLE local ref contain the tip? | `branchDeleteListRefs` + `branchDeleteContainingRefs` + `branchDeleteDurableHolders` |
| ④ | does the recorded delivery point hold it? | `branchDeleteDeliveryPointHoldsTip` (`ls-remote`, the push's own channel) |
| ⑤ | does reachable history name the row? | `taskRefNamedTaskIDs` (`git log --all`, full-id match) |

Verdicts: `no-branch` (control case — never judged), `reachable` (①③④),
`settled` (⑤), `unreachable` (the defect), `unchecked` (a read failed).

On `unreachable`:

- an ATTRIBUTED `task_events` record (`deliverable-tip-unreachable`) naming the
  row, the branch, the tip, the delivery point consulted and the sink path — the
  same record-then-report ladder `reclaimRunResidueRecord` uses, and the same
  idempotence guard (`deliverableTipFiled`);
- the same sentence on stderr, and through `taskNotifyLog`;
- with `GOPI_TASK_DELIVERABLE_TIP_ENFORCE` set (default OFF), the `completed`
  writeback is REFUSED, the row stays OPEN and its trail already carries both
  the record and `deliverable-tip-unreachable-refused`.

## 3. Why these reads and not others (the two false-positive classes)

**False positive 1 — "the ref is gone" is not "the work is lost."** The
overwhelming majority of unresolved rows landed long ago and had their ref
consumed (`task_ref_reachability.go` measured 1184 of 1214 accounted for by
reachable history). Hence ③④⑤: a merge commit on master, a tag, another
remote's tracking ref, or the merger's own `Merge … (re-lands <id>)` subject all
answer *reachable*. The measured fixture arm
(`TestDeliverableTipReachableShapesAreSilent`) pins the three shapes.

**False positive 2 — a scratch holder is not a holder.** `branchDeleteDurableHolders`
drops the pushback lane's per-pass mirror (`gitPushbackCheckRootRef`), which its
own writer DELETES when the pass ends — the epic 01a12f48 state, where a
commit's only company was scratch one `git fetch` from gone. ③ is decided on
durable holders only.

**Direction of every failure is fail-safe.** An unreadable repo, unreadable
refs or unreadable history is `unchecked` and NEVER rendered as "no ref holds
it" (Rule 15) — the one reading that would make this arm dangerous. The note
names what could not be read.

## 4. What this arm deliberately does NOT do

- **It never deletes, moves, restores or pushes anything.** The tip is not
  recovered, no ref is re-created, no archive tag is minted: the criterion is a
  MEASUREMENT plus a trail. Recovery stays the operator's decision (Rule 6d).
- **It does not touch the removal-audit sink** — read-only, through its own
  reader.
- **It adds no persistent dependency** (Rule 35): no sweep, no daemon, no state
  file, no new scheduled pass. It rides the existing close-out gate and is a
  read-only git + one ledger read.
- **It does not judge a row with no deliverable branch.** The criterion is a
  statement about a claim that names a ref. (`no-branch` is silent by
  construction, and the fixture pins it.)
- **It does not widen any existing gate.** The run-residue arm, the tracked-dirt
  binding gate and the merge-backlog terminal guard are untouched; the new
  event name deliberately does NOT share the `completed-unclean` stem, which is a
  PREFIX MATCH for the residue arm's own idempotence guard.

## 5. Verification

- Red proof: `internal/tools/zz_redproof_deliverable_tip_test.go` — master
  `c3a3f4eae` FAILs (`SILENT LOSS: … nothing on the row's trail says so`), this
  branch PASSes; the control arm passes on BOTH trees (that is what a control
  group is).
- Regression: `internal/tools/task_deliverable_tip_test.go` — the defect, the
  control case, the three silent-reachable shapes, the unreadable-repo
  `unchecked` verdict, the enforce switch's refusal, and a mirror gate pinning
  the marker phrase to one definition.
- Live measurement, read half: the real row `01a13ae1-…`, judged through the
  exported judge with the real ledger and the real repository, reads
  `unreachable` with `tip 6446222744bc6442b71ec1348e6806563bfea54e` (taken from
  the removal-audit record of the deletion), `holders []`, and the delivery point
  `peerbox:~/work/GoPi` named as not holding it; a live row of this run's own reads
  `reachable` (its ref still resolves) with no note. Raw output:
  `~/.gopi/sessions/01a13bbb-…/evidence/real-row-01a13ae1.txt`.
- Live measurement, write half: the REAL CLI door, run against a COPY of the
  operator's ledger (the live row is untouched), re-stamped the incident row
  `--status completed` and the criterion printed its sentence twice (`[task]` and
  `[task-notify]`) and filed
  `deliverable-tip-unreachable` as the LAST row of that row's trail, while the
  status still stands. Raw output:
  `~/.gopi/sessions/01a13bbb-…/evidence/real-writeback-01a13ae1.txt`.

## 6. Known residual

- The criterion is asked at CLOSE-OUT. It cannot catch a tip that becomes
  unreachable AFTER the row was written back — that is what the census reader
  (`gopi task branch-refs`) is for, and the two are deliberately separate: a
  gate that re-judged terminal rows would be a sweep, and a sweep is a
  persistent dependency this epic does not add.
- ⑤ rests on the merger writing the row id into a reachable commit SUBJECT. A
  row landed by a path that does not name it reads as unaccounted for if no ref
  holds its tip either — the strict direction, recorded here rather than
  softened (Rule 41).
