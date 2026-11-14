<!-- doc-class: historical -->
# The probe teardown must not relocate a run that keeps using its cwd

**Where it came from**: `gopi merge`'s run `01a0cb9f` printed two lines to stderr
that named no code path an operator could read:

```
[gopi] no GOPI.md found — searched: /
task_submit: warning: cwd / is not a git work tree — row submitted without a
  deliverable.repo (repo "/" is not a git work tree (git rev-parse failed:
  exit status 128: fatal: not a git repository ...))
```

Both look like configuration complaints. Neither is: they are symptoms of the
merge PROCESS having relocated itself to `/` mid-run.

## Root cause

`gopi merge` measures each non-fast-forwarding branch in a throwaway linked
worktree under the scratch tree, then removes it:

```
runMerge
 └─ mergeSweepRepo
     └─ mergeMeasureConflict            (and mergeTryUnionResolution, the union lane)
         └─ mergeTeardownProbeWorktree
             ├─ mergeReleaseCwd()       ← the defect
             ├─ removalaudit.WorktreeRemove(...)
             └─ git worktree remove --force <probe>
```

`mergeReleaseCwd()` called `worktreerelease.ReleaseReporting`, whose
implementation is `os.Chdir(worktreerelease.ProcessRootPath)` — and
`ProcessRootPath` is `"/"`. The obligation is real and correct in general:
`git worktree remove` deletes the directory it names, and a process standing
inside a deleted directory pins that inode — and every file under it — until the
process exits.

**What makes `gopi merge` different from every other producer of that release**
(`internal/tools/task_finish_cleanup.go`, `internal/tools/main_checkout_restore.go`,
`internal/tools/tasks.go`, `internal/agent/taskregister.go`, the merger pass,
`cli_resume.go`): those are CLOSE-OUTS — the process is finished and may die, so
re-parenting its cwd is free. `gopi merge` is **not finished**. It keeps running
in the SAME process and then starts the hand-over child in-process:

```
mergeHandOver
 ├─ header := mergeRunHeader(...)
 ├─ ag, err := newAgent("")        ← cmd/agent/main.go
 └─ ag.Run(header + mergePrompt)   ← the LLM child runs HERE
```

The child inherits the process cwd, which `mergeReleaseCwd` had just set to `/`.
Two consequences, both measured:

1. **The project context is lost.** `internal/agent/loop.go` resolves `GOPI.md`
   ONCE at session start by walking up from the process cwd
   (`LoadProjectContext("")` → `ResolveProjectContextFile`). From `/` the walk
   has exactly one directory to visit, so it finds nothing and prints the bounded
   list — `searched: /`.
2. **Repo-relative resolution degrades.** The `task_submit` path runs
   `git rev-parse` in the cwd to fill `deliverable.repo`; from `/` that fails
   with exit 128, so the row is filed with no repo and the merge child's work is
   not attributable to a repository.

The two lines are ONE defect seen twice.

## Why the fix is a CONDITION, not a removal

No merge code ever chdir's into a probe checkout: every git call goes through
`mergeGit`, which sets `cmd.Dir` explicitly (the one place
`cmd.Dir = dir` appears in the merge files). So the ordinary probe teardown has
**nothing to release** — the directory is pinned by nobody — and the relocation
was a pure loss. The release is still owed if a future probe ever does stand
inside its checkout, so it is kept and made conditional:

- `mergeStandingInRemovedTree(wt)` — does THIS process stand at or under the
  directory about to be removed? `os.Getwd()` compared against the probe path
  with BOTH sides resolved through `filepath.EvalSymlinks` (the scratch tree is
  reached through symlinked parents on this host, and a textual prefix test
  answers "not there" for a process that is there — the false negative that
  would re-arm the original pin). The comparison uses `dir + os.PathSeparator`
  so a SIBLING whose name extends the probe's (`…/wt` vs `…/wt-other`) is not a
  match.
- `mergeReleaseCwdForTeardown(wt)` — releases only on that arm, and prints the
  relocation on stderr when it does (Rule 15: "released" and "nothing to
  release" are different facts; the close-out faces never print, so an ordinary
  `gopi merge` gains no new output).
- An unreadable cwd, or an unresolvable path, answers "not standing there" →
  skip. That is the safe direction for THIS caller precisely because the
  close-out faces still release unconditionally: a wrong skip costs only an
  inode-pin this caller never held.

Nothing was weakened at the close-out faces, and
`internal/standards/cwd_release_gate_test.go` still passes unchanged (it counts
producers that remove a worktree; `gopi merge` still does, and now also decides
whether it holds the ground it removes).

## Evidence

**RED → GREEN, unit level** (master's production code + a harness test built
from pre-existing symbols, since the predicate itself is new to this branch):

```
RED   (master merge_run.go):
  cwd releases performed by the probe teardown while this process stood in
  <fixture repo> (NOT the probe): 2
  --- FAIL: TestRedProofMergeRunIsNotRelocatedByTheProbeTeardown
GREEN (fixed merge_run.go): the same fixture performs 0
```

**A/B on real binaries, sampling the LIVE process's cwd from the kernel**
(`readlink /proc/<pid>/cwd` every 50 ms for the whole run; the rig is
`scripts/probe-scope-rig.sh`, a conflicting branch over a real repo, endpoint
pointed at a closed port so no model call is made):

| binary | samples | samples at `/` | `GOPI.md` resolution |
|---|---|---|---|
| master (953a92c2) | 327 | **300** | `[gopi] no GOPI.md found — searched: /` |
| fixed | 340 | 12 | `[gopi] loaded GOPI.md (85083 bytes)` |

Re-measured the same A/B at the end of the run (fresh binaries, same rig) —
the split is stable, only the sample counts move with rig timing:

| binary | samples | samples at `/` | log |
|---|---|---|---|
| master (`953a92c2`) | 365 | **342** | both operator lines verbatim |
| fixed (`37d59acc`) | 416 | 23 (all after `loaded GOPI.md`) | context resolved, no warning |

Master reproduced the operator's two lines **verbatim**:

```
[gopi] no GOPI.md found — searched: /
task_submit: warning: cwd / is not a git work tree — row submitted without a
  deliverable.repo (repo "/" is not a git work tree (git rev-parse failed:
  exit status 128: ...))
```

With the fix both are gone and the child loads the project context.

**The residual relocation is a CLOSE-OUT, not a mid-run relocation.** It is
measured rather than argued: with the fix, the first sample at `/` lands at
t+1.39 s and every remaining sample is at `/` until the process exits — a
one-way transition, no bounce back. And the run's own log puts the child's
context resolution (line 7, `loaded GOPI.md (85083 bytes)`) BEFORE anything that
could observe the relocation, with no cwd-dependent step after it — no
`task_submit`, no `git rev-parse`, no second context read. So the relocation
happens after the run's last cwd-dependent act.

That is consistent with the merge child being an `internal/agent` registration
whose close-out legitimately releases the cwd (`internal/agent/taskregister.go`,
`defer worktreerelease.ReleaseReporting`), and the child is the run's LAST act,
so the release is correct for it — the same class as every other producer.
Note the two values are not chained through an anchor: the census of the rig's
child session directory came back empty (`$WORK/home/sessions` is never created;
the child registers from the session dir the run already made), so the ORDER
here comes from the run log, not from a timestamp. Recorded rather than silently
folded into the count: the two symptoms the operator reported are gone, and the
remaining relocation is a close-out, not the mid-run relocation this branch
fixes.

## Files

- `cmd/agent/merge_run.go` — `mergeStandingInRemovedTree`, `pathWithin`,
  `mergeReleaseCwdForTeardown` (replaces the unconditional `mergeReleaseCwd`);
  `mergeTeardownProbeWorktree` calls the conditional form; comments on both
  `mergeMeasureConflict` and the teardown state the obligation and its scope.
- `cmd/agent/merge_run_test.go` — scenario 8 rewritten into the two run-level
  arms (does NOT relocate when not in the probe; DOES release when in it).
- `cmd/agent/merge_probe_cwd_scope_test.go` — the predicate's own arms, including
  the sibling-separator case and the symlink case.
- `scripts/probe-scope-rig.sh` — the real-binary rig behind the table above.
- `devdoc/DOC-INDEX.tsv` — the truth-class row for this document (the docclass
  gate refuses an unclassified file under `devdoc/`).
