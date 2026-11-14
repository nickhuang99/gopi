<!-- doc-class: normative -->
# Shared-integration-ref guard: a `refs/heads/*` write from the wrong worktree

**Status:** landed. Guard: `internal/tools/shared_integration_ref_guard.go`, hooked at
`internal/tools/bash.go` (step 2d, beside the shared-stash arm). Acceptance:
`TestSharedIntegrationRefGuard_AcceptanceSet`,
`TestSharedIntegrationRefGuard_ReproducesTheIncidentWithoutTheGuard`,
`TestSharedIntegrationRefGuard_RefusalWritesNothing` in
`internal/tools/shared_integration_ref_guard_test.go`.

This document is a standing claim about master: it names the code that resolves it
(`internal/tools/shared_integration_ref_guard.go`) and the test that holds it
(`internal/tools/shared_integration_ref_guard_test.go`).

## The incident (measured 2026-10-12, hostb-Laptop)

A run issued, from its OWN linked worktree
(`/home/user/workspace/gopi-01a12d30799b775b8c97f691e282d92c`), a command whose first
step was:

```bash
git update-ref refs/heads/master 1508e3821 2>/dev/null || true
```

Its stated intent (same session log, the preceding record) was to build a **fixture**;
its second invocation did land in a scratch directory, the first one did not. That
worktree belongs to the SAME repository as the operator's main checkout
(`git rev-parse --git-common-dir` → `/home/user/work/GoPi/.git`), so the write landed on
the **main checkout's** integration-branch ref: a full pull was rolled back, the
fast-forward `6f7acc1e1 → dfa56ff93` undone to `1508e3821`.

No tree and no index were written — `.git/logs/HEAD`'s mtime never moved, and the sole
mutation was a plain ref write — so the main checkout's index still held the PULLED tree
while its ref pointed a pull behind. `git status` rendered the difference as **60 staged
paths / 9419 insertions**. A tracked-dirty main checkout **parks the merge queue**
(Rule 30; the reading is `finishCleanupWorktreeDirtyReason`,
`internal/tools/finish_cleanup_survivors.go:884` — the function this page used to
cite, `mergeDetailTrackedDirty` in `cmd/agent/merge_run.go`, went with that file on
2026-11-01 and has no successor of its own), so every branch waiting
to land queued behind a ref nobody meant to write.

The state was repaired before this row ran (measured at t+0 of this task: main checkout
`HEAD` == `refs/heads/master` == peer1 master == `2c83d07a7e`, porcelain 0). This file
is the FIX, not the rescue.

### The two fingerprints (for whoever finds the next one)

- **The reflog line has no message.** `git update-ref <ref> <sha>` (without `-m`) writes
  a reflog entry with neither a tab nor a message; `reset --soft/--mixed/--hard` writes
  `\treset: moving to …`, `checkout -B` writes `\tbranch: Reset to …`. One message-less
  line on the integration branch is the signature.
- **The staged set is a pure function of the ref move.** `git write-tree` equals the tree
  of the commit the ref was rolled back FROM, and `git status --porcelain` is staged-only
  (0 unstaged, 0 untracked) — so it is not somebody writing code.

## Why the seam is gopi's bash tool

The write came through a `bash` tool call. The tool is the only place that sees the
command, the caller's working directory, and the repository state at the moment before
execution — which is exactly what the predicate needs. `internal/tools/shared_stash_guard.go`
(commit `8610eab65`) is the precedent for the exact shape: the same hazard class (a
repository-wide resource mutated from any worktree), the same blast-radius reasoning, the
same hook point, the same `RejectDangerous && !escapeDangerous` hatch.

## Why a guard and not a convention

`refs/heads/*` is the repository's **COMMON** ref namespace: every linked worktree
resolves `--git-common-dir` to the same directory, so a branch ref written from any
worktree **is** the same ref the main checkout sees. There is no per-worktree isolation
to rely on and no code review that catches a shell line inside an agent's tool call. The
proposal on the bug row — "make the hazard visible at the seam that already resolves the
main checkout" — is the same idea one step earlier: notice it *before* the write.

A fixture that needs "a repository whose master is stale" (exactly what the incident's
author was building) has a correct route that needs no shared namespace at all: Rule 34's
`sandbox run`, or `git init` in a scratch directory outside every real repo. The refusal
names that route rather than only saying no.

## The predicate: the holder, not a branch name

The rule is **not** "never write `refs/heads/master`" — the merger, finish-cleanup and the
sync lane all move the integration ref legitimately from the main checkout, and a run must
be free to write the branch its own worktree holds. It is:

> refuse when the target `refs/heads/<branch>` is checked out in a worktree that is **not**
> the caller's.

Three read-only questions, then a decision:

1. **Is the caller in a linked worktree?** `--git-dir` vs `--git-common-dir`. The MAIN
   checkout is never refused *by construction*: it holds its own current branch, which is
   the merger's own path staying permitted.
2. **Does the repository have more than one worktree?** A single-checkout repo shares no
   ref with anybody, and is never refused — the ordinary local flow.
3. **Which worktree holds the target branch?** `git worktree list --porcelain` plus each
   entry's `--abbrev-ref HEAD`, read FRESH per command (the merger and the close-out chain
   add, remove and reclaim worktrees continuously; a cached list would judge a stale world).
   The caller's OWN entry is excused; any OTHER entry is the refusal, and its path is named.

**Fails closed.** A worktree whose checkouts cannot be read leaves the target branch
unjudged, and an unjudged write to a shared namespace is refused rather than permitted
(Rule 15). A worktree directory that has already VANISHED is a different fact — it
provably holds nothing — and is skipped, so a concurrent `worktree remove` cannot freeze
every guarded command.

## What is deliberately NOT covered — the measured gap

The guard governs exactly `update-ref` and `symbolic-ref`. An earlier draft of the guard
file asserted that this was safe because "every sibling verb carries its own worktree
refusal". **That claim was measured false** on git 2.34.1 (2026-10-12), and the correction
is the most reusable thing this task produced. Every line below was issued from a LINKED
worktree and aimed at a branch held by ANOTHER worktree:

| command | exit | effect |
|---|---|---|
| `git update-ref refs/heads/<b> <sha>` | 0 | ref **MOVED** — the incident's path (now guarded) |
| `git update-ref -d refs/heads/<b>` | 0 | ref **DELETED** (now guarded) |
| `git branch -f <b> <sha>` | 0 | ref **MOVED** — **unguarded** |
| `git branch -M <b> <b2>` | 0 | ref **RENAMED** — **unguarded** |
| `git fetch . +refs/heads/x:refs/heads/<b>` | 0 | ref **MOVED** — **unguarded** when the caller stands on another branch |
| `git checkout -B <b> <sha>` | 0 | caller switched onto `<b>` — ref rewritten by a different route |
| `git branch -D <b>` | 1 | `Cannot delete branch '<b>' checked out at '<other worktree>'` |
| `git worktree add -B <b>` | 128 | `'<b>' is already checked out at '<other worktree>'` |
| `git fetch . +x:<caller's OWN branch>` | 128 | `Refusing to fetch into current branch … of non-bare repository` |

**The two conditions are not the same condition.** `branch -D` and `worktree add -B`
refuse "held by ANOTHER worktree"; `fetch` refuses only "the CALLER'S OWN current branch".
So "I do not need to guard this — git will refuse it" is false for `branch -f`, `branch -M`
and the fetch refspec, which is exactly why the guard's scope is stated as a MEASURED
boundary rather than an assumed one.

That residual gap is filed as its own bug row (task `01a12db7-79cd-7a17-846d-9799c5a27abb`,
tagged `bug`, parent this row). It was deliberately not closed here: `branch -f`,
`branchform.FlagCreateForce` (`-B`) and `worktree add -B` are also **gopi's own lanes**
(`internal/tools/task_deliverable_worktree.go`, `internal/branchform/branchform.go`), so
widening the predicate without its own acceptance set would trade a known hole for a
false-positive surface on legitimate work. The two "out-of-scope, measured gap" cases in
the acceptance test pin that boundary on purpose: the day the guard is widened, those
cases MUST MOVE to the refused set with their own rows — not be quietly deleted.

Also deliberately not covered, and why:

- **`git push`** — writes a REMOTE ref; a local non-bare target is refused by the
  receiver's `denyCurrentBranch`.
- **`git commit` / `merge` / `rebase` / `reset`** — write the caller's own branch or HEAD.
- **`git tag -f`** — measured exit 0, but it writes `refs/tags/*`, which no worktree checks
  out, so no index can desync through it.
- **A write expressed through a variable or a helper script** (`sh -c "$CMD"`,
  `python3 -c …`, a stored script) — the guard reads the command text, so a refname that
  only exists at runtime is not seen. This is the same reach limit every command-text
  policy in this package has; it is stated rather than implied.

## Evidence

- Reproduction (fixture outside every real repo, off the `/` partition):
  `~/.gopi/sessions/01a12da8-5d7a-7de7-87b8-862c50797d24/artifacts/refguard-reproduction.txt`
  — §4 is the incident's shape re-measured (ref moved, 1 staged path, 0 unstaged, 0
  untracked, index tree == the rolled-back commit's tree), §5 is the verb table above.
- The incident's own forensics: row `01a12d86` and its artifacts; memory records #9362,
  #9363, #9364 (diagnosis) and #9375 (the verb table).

## Rule 35 note (a persistent dependency?)

None. The guard adds no cron line, no unit, no daemon, no state file, no new config key
and no new environment variable: it is a function inside the `bash` tool, sharing the
hatch that already exists (`GOPI_ALLOW_DANGEROUS`). There is nothing to own, reclaim, or
explain to the next reader — which is the intended shape.
