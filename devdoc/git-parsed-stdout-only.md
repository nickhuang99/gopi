<!-- doc-class: normative -->
# Parsed git reads take STDOUT only — stderr is diagnostics

**The defect, measured 2026-10-26.** The dirty guard read `git status
--porcelain` through `finishCleanupGitRaw`, which returned
`strings.TrimSpace(string(cmd.CombinedOutput()))`. `CombinedOutput` merges git's
two streams, so any stderr text becomes the "porcelain" body. With `GIT_TRACE=1`
in force, git writes

```
06:54:17.718401 git.c:463               trace: built-in: git status --porcelain
```

to stderr — so a PROVABLY CLEAN worktree was read as dirty and
`reclaimDirtyVerdict` (`internal/tools/task_reclaim.go`) answered
`reclaimSkipDirtyReason` ("worktree is DIRTY (uncommitted work) — left for a
human"), which refuses the removal. Measured before the fix:

```
$ GIT_TRACE=1 go test ./internal/tools/ -run 'TestSelfDelivery' -count=1  # selector-ok: the output below is a 2026-10-26 RECORD of a run against the tests that then existed; `test_reclaim_self_delivery_test.go` was deleted 2026-10-31 (task 01a19052, alone-deletion probe: DEAD-ALONE + zero production references), so the selector can never match again. The live guard for this rule is `TestParsedGitReadsIgnoreStderrPollution`.
--- FAIL: TestSelfDeliveryPushesRemovesAndForceDeletes       (0.07s)
--- FAIL: TestSelfDeliveryFailureKeepsTheRefAndNamesTheTarget (0.06s)
--- FAIL: TestSelfDeliveryDirtyGroundIsRefusedAndKept         (0.07s)
--- PASS: TestSelfDeliveryReportsAChainThatCouldNotRun        (0.00s)
--- FAIL: TestSelfDeliveryIsIdempotentForTheWorkerPath        (0.06s)
FAIL
```

(4 failures, not 2 — the same pollution also reached the branch probe, the
`rev-parse` tip probe and the test helper; see "the second half" below.)

## The rule

**A helper whose RETURN VALUE IS PARSED must never see git's stderr.** stdout is
the verdict; stderr is diagnostics. A helper whose value is only PRINTED (git's
own refusal sentence in a report) may keep the merged stream — that text IS the
deliverable there.

`internal/tools/git_parsed_output.go` is the ONE implementation (Rule 14):

| reader | returns | for |
|---|---|---|
| `finishCleanupGitParsedStreams(ctx, dir, timeout, args…)` | `gitParsedOutputStreams{Stdout, Stderr}` | a caller that needs git's diagnostics SEPARATELY from the value |
| `finishCleanupGitParsedStdout(ctx, dir, timeout, args…)` | the trimmed stdout | the ordinary parsed read |

Both keep git's own sentence available — it rides the returned **error**
(`git <args>: <exit>: <first non-empty stderr line>`), so a refusal that must
quote git still can, without a parser ever seeing it (Rule 15: stderr is not
swallowed).

**No `combined bool` parameter.** A flag on one function is the same latent
coupling in a new spelling — a caller opts back into the merged stream by
accident of its argument list. The two behaviours are two FUNCTIONS.

## Scope: parsed vs printed

| parsed (moved to the new reader) | printed (kept `CombinedOutput`) |
|---|---|
| `reclaimGitStatus` — the dirty guard's porcelain (`task_reclaim.go`) | `finishCleanupGitRaw` legs whose value goes into a report: `git checkout <branch>`, `git worktree remove`, `git worktree prune` |
| `worktreeSweepDirtyReason`, `mergedCleanupDirtyReason` — the two sweep dirty readers | `mergedCleanupGit` — its three callers are `worktree remove` / `branch -d` statements |
| `git worktree list --porcelain` × 7 (`finishCleanupBranchWorktree`, `finishCleanupOwningWorktree`, `reclaimWorktreePathLister`, `reclaimWorktreeRegisteredIn`, `taskWorktreeOwningDir`, `taskRepoLinkedWorktrees`, `reclaimSelfOwnRepoOf`, `worktreeSweepRegistrations`, `mergedCleanupWorktrees`) | `worktreeSweepClearLockFile` / `worktreeSweepUnlock` (a file removal and `git worktree unlock`) |
| `git rev-parse` family: `--abbrev-ref HEAD`, `--verify --quiet HEAD`, `HEAD`, a branch tip, `--show-toplevel`, `--git-common-dir`, `--path-format=absolute --git-common-dir` | `worktree remove` / `worktree prune` / `branch -D` in the close-out chain |
| `symbolic-ref --short HEAD` (the main checkout's branch) | the push legs (git's own refusal sentence is the whole point) |
| `git status --porcelain` in the finish-cleanup chain (5 reads) and the salvage arm (`--ignored=matching`) | |
| `git branch --merged <integration>` (one branch name per line) | |
| `git reflog show --format=%H <branch>` (entry count) | |
| `git ls-remote <target> <ref>` (remote tip) | |
| `git diff HEAD --binary`, `git ls-files --others --exclude-standard` (the salvage payload — a stderr line spliced into a patch is corruption) | |

`git merge-base --is-ancestor` is neither: it prints nothing and the answer IS
the exit status, so there is nothing to parse and nothing to separate.

## The second half: the FIXTURE parsed git too

After the production readers were separated, the acceptance run STILL failed —
for a different reason, and the failure text said so:

```
task_reclaim_self_delivery_test.go:161: the peer must hold the delivered tip 06:58:04.162679 git.c:463
    trace: built-in: git rev-parse feat/self-release
```

The arms compare tips through `gitOut` (`internal/tools/task_reclaim_self_test.go`),
which also returned `CombinedOutput` — so `gitOut(...)` handed a trace line to a
string comparison. Two sides were parsing: the code and its instrument. **A test
helper whose value is compared is a PARSED read too**, and it was fixed the same
way (two streams, stderr only in the failure message).

## The regression guard

`internal/tools/git_parsed_output_test.go` — `TestParsedGitReadsIgnoreStderrPollution`.
Two-sided on purpose, so neither half can pass by measuring nothing:

* **RED arm** — the PRE-FIX shape (`finishCleanupGitRaw`'s merged stream) must
  SEE a non-empty body on a CLEAN tree under `GIT_TRACE=1`. If git ever stops
  writing trace to stderr, this arm fails and says the guard proved nothing.
* **GREEN arms** — `finishCleanupGitParsedStreams` keeps the streams apart,
  `reclaimGitStatus`+`reclaimDirtyVerdict` read the same tree CLEAN,
  `worktreeSweepDirtyReason` / `mergedCleanupDirtyReason` / `repoRootOf` agree,
  AND an untracked file is still read as residue (no false negative, and the
  parsed body carries no `trace:` text).

Red proof, on this ground, by reverting the one line in `reclaimGitStatus`:

```
--- FAIL: TestParsedGitReadsIgnoreStderrPollution (0.01s)
    git_parsed_output_test.go:118: the dirty guard read a CLEAN tree as
      "worktree is DIRTY (uncommitted work) — left for a human\n06:56:48.771819 git.c:463
       trace: built-in: git status --porcelain" … under GIT_TRACE=1 — stderr pollution
       is still reaching the parser
```

Restored, then green. `GIT_TRACE` (git's own documented tracing knob) is the
injection channel: the test does not need a second mechanism to manufacture
stderr.

## What this does NOT change

Behaviour without `GIT_TRACE` is identical: stdout of every one of these
commands was already the whole body when git said nothing on stderr, and an
unrunnable git still fails closed (`worktreeSweepDirtyReason` still reports
"git status failed: …" rather than "clean"). The fix depends on no env var.
