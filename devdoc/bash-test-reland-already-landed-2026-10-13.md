<!-- doc-class: historical -->
# The bash_test.go "re-land" was already landed — the inherited merge is an ancestor of the branch cut

**This document is a HISTORICAL record of one run (task
`01a12f282f127186a661d375f6e1826a`, 2026-10-13). It is not a standing claim about
current master: the figures below are measured at the revisions it names.**

## What the briefing asserted, and what the repository says

The epic asked for a **re-land**: take the handed-over branch
`task/01a12efb410d70db88de664ec7cc61e7` (tip `f69a6ed10`, ref deleted, object
surviving), re-apply its two commits onto the *current* master tip in this run's
own branch (`task/01a12f282f127186a661d375f6e1826a`, cut at `e7b5ead`), and
resolve the expected conflict additively — master's `bashExitPrefixZero/One` +
`bashExitField` block against the branch's diagnostic-window block.

The premise does not hold. **The branch had already been merged to master before
this run's branch was even cut:**

```
870187918  merge(task/01a12efb410d70db88de664ec7cc61e7): inherit the length-bounded diagnostics + guard
           author=user <user@example.com>  date=Mon Oct 12 15:57:03 2026 +0800
           parents=799779dcf f69a6ed10
```

`f69a6ed10` is the merge's **second parent**; `git merge-base --is-ancestor
f69a6ed10 e7b5ead` → rc=0, i.e. the inherited tip is an ancestor of this run's
own branch cut. Two follow-ups widened the same work (`1487ce143` at 15:59:55 and
`8c31f0e51` at 16:01:32), so what reached master is a **superset**, not a copy.

The measurements, all on the run's own branch (`gopi-01a12f28…` worktree):

| check | measured |
|---|---|
| `git merge-base --is-ancestor f69a6ed10 <branch cut e7b5ead>` | rc=0 |
| `grep -c '^<<<<<<<' internal/tools/bash_test.go` at `e7b5ead` | `0` |
| `bashExitPrefixZero` / `bashExitPrefixOne` at `e7b5ead` | present (line 21) |
| `func bashExitField` | present (line 32) |
| `bashDiagWindowBytes` / `bashDiagHeadProbeBytes` / `bashDiagScannedFiles` | present (52 / 59 / 64) |
| branch's escape-hatch arm rewrite (`exec.LookPath("sudo")` guard) | present (line 381) |
| `func TestBashMaskingTailHidesTheTestedStatus` (masking-tail repro) | present (line 401) |
| `git diff e7b5ead master -- internal/tools/bash_test.go` | **empty** (byte-identical) |

**Both helper blocks coexist with zero conflict markers because they were merged
on 2026-10-12, not because a conflict was resolved here.** The conflict diff
carried into this run as an attachment (`01a12efb-conflict.diff`) is a photograph
of a resolution that had already happened.

## Why the briefing could say otherwise — a false premise with an observable cause

This is the shape Rule 41 exists for: the briefing's inherited hand-over
("ref already deleted; tip object survives") was written from the *branch*, where
the ref genuinely is gone, without asking whether the **commits** had instead
survived through a merge. A deleted branch ref and an unmerged branch are not the
same fact. The delivery-point sync then shipped the whole thing again — that is
`git merge master` below.

## What the run actually did

1. Established the measurements above (evidence under
   `~/.gopi/sessions/01a12f28-2f12-7186-a661-d375f6e1826a/evidence/`).
2. Ran the delivery-point sync — `git merge master -m "delivery-point sync: …"` —
   which **fast-forwarded** `e7b5eadd6..edd69493c` (no commit created, `-m`
   ignored), i.e. master had no divergence from the branch at all. Both files
   carried by master (`internal/tools/task_finish_cleanup.go`,
   `internal/docclass/prose_path_line_baseline_table.go`, the new doc's row) came
   in without one hunk to reconcile.
3. Wrote this document as the run's deliverable, and ran
   `gopi tool doccheck --fix` so the file's `DOC-INDEX.tsv` row exists in the
   **same commit** that lands the doc (Rule 36).

**No conflict resolution, no re-application, and no code change was needed —
`git status --porcelain` was empty before the sync and printed nothing after it.**
The branch tip equals the master tip it was synced to; the branch therefore has
nothing of its own to merge, and merging it is a no-op (fast-forward).

## The one thing worth carrying forward

`internal/tools/bash_test.go` on master is the **superset**: the inherited
fixes plus the guard-widening follow-ups. Anyone re-dispatching a "re-land
`01a12efb`" ticket should first run

```bash
git merge-base --is-ancestor f69a6ed10 <their own base> && echo already landed
```

because the object surviving without its ref is precisely what made this ticket
look necessary.
