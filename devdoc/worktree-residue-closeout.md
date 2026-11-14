<!-- doc-class: historical -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

# Worktree residue after a finished run — two live leak causes and the release-coverage holes

Epic `01a0a36a-86be-763c-b38c-f2444661439f` · branch `fix/worktree-residue-closeout` ·
worktree `/home/user/workspace/gopi-wt-residue` · 2026-09-15.
Attachment: `work_tree_not_removed.txt` (229 B `git worktree list` — two orphans of
`/home/user/work/GoPi` outliving their runs).

The previous fix targeted the three running methods (`gopi worker`,
`gopi raw-prompt`, `gopi task run`). The machinery was there — all three call
`ReclaimOwnFinishedWorktreesWithin` + `worktreerelease.ReleaseReporting`. The
residue still happened, for reasons those calls could not cover.

## 1. The two orphans, measured

| Orphan | HEAD | State | Blocker |
|---|---|---|---|
| `gopi-bgatefix-blocked` | `45f56317` @ `fix/mergebuildgate-module-download-abstain` | clean, 2 commits on that branch alone | sweep guard ③: "owned by live task `01a0a36a` (in_progress)" — a **false stamp** |
| `gopi-vp-a03d-pre` | detached `79738cb0` → archived `03526655` | 2 staged files present in no branch | dirty tree ⇒ non-force removal refuses |

A whole-`/proc` scan found **no** live cwd/fd/exe holder on either — the
cwd-holder veto is real (guard ③, `scripts/repro-live-holder-worktree-veto.sh`),
but it is not what stranded these two; the false ownership stamp is.

## 2. Root cause 1 — the path-policy guard did not understand `git -C`

`internal/tools/path_policy.go:rejectOutsideDeclaredRoots` scanned
`git … worktree add|remove` token by token and treated the token right after
`git`/the subcommand as the *target*. Git's global `-C <path>` was not consumed,
so

```
git -C /home/user/work/GoPi worktree remove /home/user/workspace/<wt>
```

was read as *removing `/home/user/work/GoPi`* → `target "…" is outside every
declared root`. A repo is never a worktree root, so **every internally generated
removal was refused** — `internal/tools/task_finish_cleanup.go` and
`internal/tools/merge_build_gate.go` both spell it exactly that way. Only an
ad-hoc `cd <repo> && git worktree remove <wt>` escaped the guard.

Live proof: `git -C <repo> worktree remove /home/user/workspace/nonexistent-xyz`
→ refused, naming the repo. The same refusal text appears in session `2a304286`
(the verification run that created `gopi-vp-a03d-pre`): the verifier hit it,
gave up, and left the worktree.

**Fix.** `gitInvocationStart` now consumes git's global options that take a value
(`-C <path>`, `-C<path>`, `-c`, `--git-dir`, `--work-tree`, `--namespace`,
`--exec-path`) and returns the repository as the invocation's base directory.
Removal targets are judged as `worktreeTarget{path, baseDir}` against git's own
base, so the guard sees what git will actually operate on.

## 3. Root cause 2 — the detection seam bound a row by *mentioning* a worktree

`taskBashWorkdirCandidates` (`internal/tools/task_deliverable_worktree.go`) is the
creation-time seam that stamps a run's row with the worktree it is working in.
It accepted **any** `-C <path>` token in a bash round as evidence. A round that
merely *inspected* other worktrees —

```bash
for wt in /home/user/workspace/gopi-bgatefix-blocked /home/user/workspace/gopi-vp-a03d-pre; do
    git -C "$wt" status; …
done
```

— re-stamped the inspecting run's own row with orphan A, 50 s after the row had
been minted repo-only by its `task run` wrapper. The row that exists to *clean*
the orphan became its registered **owner**, and the sweep then protects the
orphan from every later pass. Reproduced live in this session:

```
keep /home/user/workspace/gopi-bgatefix-blocked (owned by live task 01a0a36a-86be-763c-b38c-f2444661439f (in_progress))
```

The function's comment already recorded the hijack as a past incident, and had
been partially repaired (never *rebind* an existing stamp) — a row with no stamp
yet stayed exposed.

**Fix.** Binding requires *acting*, never mentioning: the round's own workdir, a
worktree the command creates or moves (`git worktree add|move <path>`), or the
target of a **mutating** git subcommand (`taskBindMutatingGitSubcommands`).
Read-only subcommands (`status`, `log`, `list`, `diff`, `show`, `rev-parse`)
bind nothing. Tokenizing reuses the one git-option reader from §2 (Rules 13/14).

## 4. Release coverage — two exit paths that skipped the close-out

The obligation is "no wrapper return path leaves the row's ground occupied". Two
paths violated it:

- **`gopi task run` failure returns** (`cmd/agent/cli_tasks.go`). The reclaim +
  cwd release sat only on the success return; both failure returns (the hermetic
  escape and the real failed-child exit) walked past it. A FAILED run is exactly
  the one whose worktree is most likely to survive. Fixed by extracting
  `taskRunReleaseOwnWorktrees(target)` and calling it on all three returns — the
  coverage is now a property of the wrapper, not of each return site.
- **`internal/agent`'s end hook** (`finishSessionTaskRecorded`,
  `internal/agent/taskregister.go`). The cwd release ran *after* early returns
  (`!autoTaskRegistered || SessionLog == nil`, failed ledger close), so a child
  that bailed there stayed standing INSIDE its worktree — which is itself the
  veto that blocks that worktree's removal. Now
  `defer worktreerelease.ReleaseReporting(...)`, idempotent, covering every
  return path. `ReleaseReporting` already skips compiled test binaries, so the
  harness cwd is never hijacked.

The worker leg (`cli_tasks_worker.go` defer → `workerRoundOwnWorktrees`) and the
merger/resolve/verify/resume legs already defer on all paths — verified,
unchanged.

## 5. Verification

- `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK
- `go test -count=1 ./internal/... ./cmd/...` → 31 packages ok, 1 FAIL:
  `TestTaskFindSessionJSONLStillHonoursEveryLayout` (`internal/tools`), which
  **also fails on master** (2/8 isolated runs) — a pre-existing order/timing
  flake, not a regression from this branch. It passes 5/6 in isolation here and
  master's `./internal/tools/` passes when run alone; it only shows up in the
  combined run, which is why master's per-package gate has not caught it.
- Gate suites: `./internal/standards/` ok, `./internal/tools/` ok,
  `./internal/agent/` ok, `./cmd/agent/` ok.
- Red-proofs (production fix stashed ⇒ test FAILS; restored ⇒ PASSES):
  `TestPathPolicyGitChangeDirIsNotTheTarget`,
  `TestTaskDeliverableWorktreeBashReadOnlyProbeDoesNotBind`
  ("a read-only probe BOUND the row to …"),
  `TestTaskRunFailedChildReleasesOwnWorktree`
  ("a FAILED `task run` must still free its own worktree"),
  `TestTaskRunReleaseIsDeferredInTheEndHook`
  ("must DEFER worktreerelease.ReleaseReporting").
- `TestTaskDeliverableWorktreeBashGitCWorktreeCandidate` had pinned the permissive
  rule via a `status` command; rewritten to use a mutation (it was asserting the
  defect).

## 6. Known remainder (submitted, not fixed here)

Task `01a0a38c-f4ec-7572-9066-0883cd0fadc7`:

1. A **persisted** false stamp is not repaired — the fix stops new ones being
   created, but the ledger row above still protects orphan A.
2. Guard ③'s `row.Branch == reg.Branch` arm protects a registration whenever a
   non-terminal row merely *shares the branch name*; with the 24 h age threshold
   (measured on the creation record, so a fresh orphan reports `age=0h`) the
   orphan stays put.
3. The two baseline orphans were made content-safe (B's staged files committed on
   `archive/vp-a03d-probe-20260915` = `03526655`; A's commits live on its branch)
   but their physical removal belongs to the operator/PA merge round.

Also recorded, not fixed: `devdoc/worktree-cwd-release-coverage.md` lists 9
release sites including a `loop.go` "executor exit funnel" — no such call has ever
existed (`git log -S` over `--all`: zero commits). The real count is 7 seams.

## 7. Closure: the attachment's two residue worktrees are GONE (measured)

After §5's A/B and the cleanup of both trees (A held no untracked/modified file;
B's staged files had been committed to its archive tag), both registrations and
both directories were removed in the operator's repo:

```
git worktree remove /home/user/workspace/gopi-vp-a03d-pre      → REMOVED
git worktree remove /home/user/workspace/gopi-bgatefix-blocked → REMOVED
git worktree list → /home/user/work/GoPi (master) + gopi-ticktock,
                    gopi-voice-out, gopi-wt-residue           (both gone)
ls -d both paths  → No such file or directory
git rev-parse --verify fix/mergebuildgate-module-download-abstain → 45f56317
git rev-parse --verify archive/vp-a03d-probe-20260915             → 03526655
.git/worktrees/   → gopi-ticktock, gopi-voice-out, gopi-wt-residue (no stale dirs)
```

Both branches/tags survive their worktrees, as §4 required. The attachment's
`git worktree list` residue is therefore empty; the remaining entries are live
sessions' checkouts, not residue.

### 7a. The live repro of RC#1 — the guard refuses the cleanup itself

Removal was ONLY possible because the operator's actual cleanup was run through
the tool's `workdir` (no `cd`/`-C` token in the command string). Every spelling a
session would naturally write was refused by the DEPLOYED guard (bin built from
master `39a08e20`, i.e. without §3's fix):

```
cd /home/user/work/GoPi && git worktree remove <wt>   → REJECTED
  target "/home/user/work/GoPi" is outside every declared root ...
git -C /home/user/work/GoPi worktree remove <wt>      → REJECTED (same, names the repo)
git -C /home/user/work/GoPi worktree prune            → REJECTED (same)
```

So the two corpses §1 found were not merely left by inattentive runs: **the guard
made removing them impossible from a cleanup session**, and only the plain
`workdir=repo` form (which never appears in generated code — every internal call
is `git -C <repo> worktree remove <wt>`) slipped through. This is the same live
defect §3 fixed, re-measured on the deployed binary while closing the residue;
after the fix both refused spellings resolve to the positional target
(`TestPathPolicyGitChangeDirIsNotTheTarget`, re-run green: `ok internal/tools`).

### 7b. Newly found, not fixed here: `cd <repo> && git worktree remove <wt>` is unjudged

`gitWorktreePathTargets` scans `splitSimpleCommands` and calls
`gitInvocationStart(tokens)`, which requires the FIRST token to be the git binary
(`mentionsGit`). In `cd <repo> && git worktree <sub> <wt>` the git invocation is
the second simple command and **starts with `git`**, so that arm does match —
but the `cd <repo>` creates no target of its own and the worktree arm judges only
the positional operand, so a target OUTSIDE every declared root would be judged
against the session workdir and refused correctly. The gap is narrower than it
first looks (the worktree shape is judged, not skipped); what is NOT judged is a
`cd`-relative target that resolves differently than the workdir assumes. Recorded
for the follow-up (`01a0a38c`) rather than folded in — the measured defect in this
epic is the `-C`/`cd` base-vs-target misread, which §3 closes and §7a re-measures.
