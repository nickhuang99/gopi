<!-- doc-class: historical -->
# Two cmd/agent reds reported against an OLD cut: both already green at master tip

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task `01a144145ffc7afda3b961e685c7ee3d`, ground `cc846ee` (=
`refs/heads/master` = `refs/remotes/gitPushcheck/master`, 2026-10-17 08:26).

## The claim this doc records

A briefing reported two `cmd/agent` tests RED on master and asked for repairs.
Both were measured at OLD revisions — briefing base `bc8fc75` (167 commits
behind), row authored at `9abec608` (162 behind). **Re-measured at the cut point
`cc846ee`, both are GREEN and need no source change at all.** The reportable
result of this task is the re-measurement, so the two claims are recorded here
with their instruments rather than being "fixed" into a change that does not
exist.

| test | reported | at `cc846ee` | why |
|---|---|---|---|
| `TestMergeEveryLoopWritesNoStateFile` | FAIL: profile holds `.gopi/logs/removal-audit.log` | **PASS** | `c466c05c1` (2026-10-16 22:49) + `61c9a86c4` are ancestors of the cut |
| `TestSpawnersPinTheRepoContext` | FAIL: tree carries 4 markers, scan counted 2 escaped sites | **PASS** | `4d9e7fd96` (2026-10-17 00:21) is an ancestor of the cut |

Verify the ancestry claim in one line each (a `task/*` branch's fix reaches
master by merge, so the commit sha stays in the shared object store):

```bash
git merge-base --is-ancestor c466c05c1 HEAD && git merge-base --is-ancestor 61c9a86c4 HEAD \
  && git merge-base --is-ancestor 4d9e7fd96 HEAD && echo "all three are ancestors of the cut"
```

## The BEFORE half is real, and it is on the record

A re-measurement that only reports "green" is worth nothing on its own — it
cannot tell "already fixed" from "the test did not run". So the same two test
functions were run in an UNTOUCHED detached worktree at the row-authoring
revision (`git worktree add --detach … 9abec608`, never `git checkout <rev> --
<file>` inside the branch, which would measure AFTER), with `bin/gopi` built
there first:

```
--- FAIL: TestMergeEveryLoopWritesNoStateFile (2.34s)
    merge_every_test.go:573: the profile holds [.gopi/logs/removal-audit.log] — the command's contract is no state file, no pid file, no progress record.
--- FAIL: TestSpawnersPinTheRepoContext (0.34s)
    repo_context_sandbox_guard_test.go:524: the tree carries 4 "repo-context-ok:" marker(s) but this scan COUNTED only 2 escaped spawn site(s): …
FAIL	github.com/nick/gopi/cmd/agent	4.073s
```

That is the briefing's two sentences, verbatim, reproduced independently. The
fixes therefore are not "not needed" in the abstract — they were needed, landed
in between, and the briefing was carried over a stale base.

## Per-arm roll-forward: which commit carries which half

Pointing at a commit is a claim; the arms were measured at the fix's own parent
and at the fix, so each name is backed by an observed red→green transition
rather than by reading the log.

- **Arm 1 — the tolerated audit sink.** `TestMergeEveryLoopWritesNoStateFile`
  FAIL at `c466c05c1^` (naming `merge_every_test.go:573`, the sink), PASS at
  `c466c05c1` (`fix(cmd/agent): the merge-loop residue walk tolerates the audit
  sink it writes itself`). `61c9a86c4` (2026-10-17) then backed that tolerance
  with an observation — the tolerance is read a second time through the
  emitter's own reader, and every record in the tolerated file must be this
  run's own (`mergeAssertSinkHoldsTheLoopsOwnRecords`). Artifacts:
  `arm1-parent.txt`, `arm1-fix.txt`.
- **Arm 2 — the marker census.** `TestSpawnersPinTheRepoContext` FAIL at
  `4d9e7fd96^` with the exact "4 marker(s) … counted only 2" message, PASS at
  `4d9e7fd96` (`fix(cmd/agent): reconcile the repo-context gate's marker census
  with its escaped sites`). Artifacts: `arm2-parent.txt`, `arm2-fix.txt`.

## The mechanism, in case it recurs (the reusable half)

Both reds had the same shape: **an instrument that was right and an account that
was stale**.

`TestSpawnersPinTheRepoContext`'s census used `strings.Count(src, marker)`,
which counts **mentions**: a comment explaining that an escape was removed, or a
doc sentence spelling the marker out, counted as a live permit. The count then
read 4 while three real directives existed, and the verdict blamed the scan
("the tree carries 4 … but this scan COUNTED only 2 escaped spawn site(s)").
`rcsMarkerDirectives` (introduced by `4d9e7fd96`) counts only the lines that
**ARE** the directive — trim, strip the `//` prefix, `HasPrefix(marker)` — and
the mismatch message now names both sides (which files ×N, which
`file:line: fn()`). Do not write the marker's full spelling with its trailing
colon in prose beside a scanned file; refer to it without the colon.

`TestMergeEveryLoopWritesNoStateFile` bit for the mirror reason: the residue walk
refused `.gopi/logs/removal-audit.log` while the SHARED reason text named
`internal/signalrec` — the instrument (the walk) was right, the explanation was
wrong. The tolerated set grew by exactly one artifact and the walk's rule grew
with it, in the file that owns the rule.

## State of the gate: what was NOT touched

- `rcsScanFloor` is **80** at `9abec608`, at `4d9e7fd96` and at `cc846ee`
  (unchanged across the whole window).
- This task's diff against `cc846ee` touches **no Go source and no gate file** —
  it is this document plus the report. The acceptance run at the cut point was:
  `go test -count=1 -run 'TestMergeEveryLoopWritesNoStateFile|TestSpawnersPinTheRepoContext' ./cmd/agent/`
  → `ok … 4.060s`, and the verbose run logs the premise the gate actually read:
  `110 lifecycle spawn site(s) scanned, 89 pinned, 2 escaped, 19 daemon-start
  (own-dir) site(s), 2 escape marker(s)` — **2 markers == 2 escaped sites**, and
  `110 ≥ 80` so the scan is not vacuous.

## Planning note for the next briefing

A red measured at a revision 160+ commits behind master is not a red on master.
Carry the *revision* into the briefing and require a re-measurement at the
recipient's own cut before any repair is commissioned — otherwise the executor
either "fixes" an already-green test (churn, and a risk of weakening something
real) or, worse, repairs it at the wrong revision and reports a fixed red that
was never red. Both happened in this epic's neighbourhood: a sibling task
(`01a143a7`) was briefed against `1e6de82` and found its Red 1 already green too.
