<!-- doc-class: historical -->
# Worktree residue + "unprotected merge" — root cause

**Branch**: `fix/worktree-residue-occupancy` (linked worktree
`/home/user/workspace/gopi-wt-residue`, base `b9ae5cbc`).
**Trigger**: operator report (`empty_worktree.txt`) — 12 git worktree
registrations, 5 branches, and the question *"how can merger merge unprotected
branches? unless some agent does merging without going through merger?"*

**Answer, in one line**: nothing merged unprotected **and** nothing leaked
permanently — the registrations were being **held live by processes** and were
inside the sweeper's **24-hour age floor**; the moment the holders exited, the
sweep reaped 7 of them within minutes. Two real defects rode along and are
fixed here (the report channel for a kept ref, and a registration that could
outlive its directory).

---

## 1. The classification (measured, attachment + live inspection)

| class | count | what it was |
|---|---|---|
| branch-occupied worktree, branch **already an ancestor of master** | 4 | merged work whose branch + worktree survived (cleaned later by merged-cleanup) |
| branch-occupied worktree, branch **genuinely unmerged** | 1 | `fix/rawlog-test-home-leak` (keep — live deliverable) |
| registration whose **directory is gone** | 1 | `merge-build-gate-2747947113/wt-feat-guard-test-daemon-env-isolation`, `git worktree list --porcelain` → *"prunable gitdir file points to non-existent location"* (defect **D3**) |
| **detached HEAD** fixture worktrees, all at ancestor-of-master SHAs | 6 | `gopi-vfy-base`, `gopi-vfy-peak`, `gopi-prefix-master`, `gopi-fwd-revert`, `gopi-master-check`, `cmdagentbase` — testing fixtures, zero unmerged work |

Worktree age was measured on the **git-internal backlink** (`<gitdir>/commondir`
mtime), never the directory mtime (any later access refreshes it): the twelve
were 0–14 h old, i.e. **below the sweeper's 24 h floor**.

**13 live processes** had their `cwd` **inside** those worktrees at first
observation (leaked merger/worker daemons and probe binaries under
`/dev/shm/gopi-test-scratch/suite-*-run-*/…-daemon-bin/`). The sweeper's guard
② vetoes any registration whose path is held by a live process — unconditionally
and correctly (it is the strongest evidence of use). That is *why* the
registrations survived: not a leak, a **live holder**.

The corroboration arrived by itself: by t+455 s the count was **12 → 5** with no
action of mine, because the holders had exited and the tick reaped them.

## 2. "How can the merger merge unprotected branches?" — it cannot

Measured in a scratch repo (three probes, no inference):

```
merge master <- feat, feat checked out in a linked worktree   → exit 128
      fatal: 'feat' is already checked out at '<path>'
branch -d feat (same state)                                   → exit 1
merge master <- feat, checked out, real divergence            → exit 128
```

Git itself is the occupancy guard. The audit of every merge path found **no
breach**: the merger sweep excludes worktree-protected branches
(`repoSweepProtectedBranches` → `mergedCleanupWorktrees`), `TaskMerge` decided by
ancestry, `scripts/safe-merge.sh` refused loser/archived/worktree-held branches,
and every branch found in a worktree was either an ancestor of master
(merged-then-left) or a live deliverable. **No agent merged around the merger**.

The `adoption:` merges on master are the adoption chain's own legitimate landing
path (`gopi task adopt` / serve tick), not a bypass.

### Suspect REFUTED: "the worktree sweeper is not wired in"

`bin/gopi task sweep-worktrees` **is** on the serve tick, in APPLY mode —
`cmd/agent/cli_tasks.go:3065` `serveWorktreeReapSweep()` ←
`serveMergedCleanupSweep()` (throttled, `GOPI_CLEANUP_INTERVAL`, default 15 min).
Its dry run on this host answers the operator's question directly:

```
sweep-worktrees (dry-run): repos=2 reclaimed=0 would-reclaim=0
  keep  age=8h  /dev/shm/gopi-test-scratch/cmdagentbase   (younger than the threshold (8h35m < 24h))
  keep  age=0h  /home/user/workspace/gopi-worker-attr     (younger than the threshold)
```

The residue is a **time-of-day observation of a working system**, plus the two
defects below — not a missing sweeper and not a permanent leak.

## 3. Defect D2 — a healthy landing was reported as a FAILURE

**Measured on master** (RED proof, scratch copy of the new test run against
`b9ae5cbc`):

```
MEASURED on master: adopted=[feat/occupied]
  failed=[feat/occupied: prune ref: git branch -d feat/occupied: exit status 1:
          error: Cannot delete branch 'feat/occupied' checked out at '…/occupied-wt']
```

A candidate branch held by a linked worktree is ② probed with
`git merge --no-commit --no-ff`, which **succeeds** (git only refuses a branch
that could *fast-forward* into its own checkout), ③a commits the landing, and
then `branch -d` fails because the branch is checked out. Master filed that
expected, benign outcome in **`Failed`** — so every serve pass logged
`failed=1` for work that landed correctly, and the operator's error channel
carried no signal (Rule 15b: a channel that cries wolf is a channel nobody
reads).

My first hypothesis was the opposite (that the refusal was misclassified as a
conflict) — the measurement killed it, and the gate built for it was **reverted
after being measured**: skipping occupied branches would *delay* a real landing
for no benefit. Only the report channel was wrong.

**Fix** (`internal/tools/task_adoption.go`): a new `AdoptionCounts.RefKept`
channel with one named marker (`adoptionRefKeptMarker`); the kept ref is named
with git's own reason on one line, `Failed` returns to meaning *failure*, and
both report faces (`cli_tasks_adopt.go`: serve log + `gopi task adopt`) render
`ref-kept=`. The ref is still not force-deleted — the merged-cleanup pass owns
it once the worktree is provably dead.

## 4. Defect D3 — a registration could outlive the directory it points at

Mechanism, **measured** (git 2.34.1, three probes in scratch, recorded in the
test file):

| state | outcome |
|---|---|
| registration, directory GONE, nobody prunes | still listed by `git worktree list --porcelain` |
| registration, directory GONE, `worktree prune` | collected |
| registration, directory PRESENT + `"initializing"` lock | `remove --force` **refused** ("cannot remove a locked working tree"); `-f -f` overrides |

So git does **not** collect a registration by itself once the directory is gone:
`git worktree prune` is the only verb that does, and `mergeBuildGateScratch`'s
cleanup was `os.RemoveAll(workRoot)` **alone** — the exact single line that
produced the live `merge-build-gate-*/wt-*` orphan. (The build path's removal
*does* repair the registration, but only on its failure branch.)

**Fix** (`internal/tools/merge_build_gate.go`): the cleanup is now
`mergeBuildGateScratchCleanup(repo, workRoot)` — remove the directory **then**
reap this call's own registrations (`remove --force` → drop a stale lock →
retry → `prune`), reporting every refusal. A registration can no longer outlive
its directory whatever the reason the directory went away.

## 5. D1 — the leaked-holder story, and why there is NO code fix here

The first observation showed 13 processes with `cwd` inside the worktrees. By
the time I could act on it they were **gone**, and the two survivors were not
suite leaks: one manual probe daemon (`gopi-merger merger`, exe
`/tmp/gopi-probe-fix-debug`, started 00:42, still holding
`gopi-conflict-flags`) and the operator's own live merger. So:

- **Not reproducible as a suite leak** — `pgrep -x gopi-merger` = 0 before and
  after running the merger-daemon e2e tests, and the exact-name probe
  (`gopi-merger-daemon`) found none. Inventing a teardown "fix" for a leak I
  cannot reproduce would be a fabricated fix, so there is **none**.
- The real, honest recommendation is process hygiene, not code: a probe daemon
  started **without `--every`** (unbounded) keeps a worktree live forever, and
  the sweeper will correctly refuse to touch it. Start probes with `--once` or a
  bounded `--every`, or reap them when the probe ends.

## 6. Verification

- `CGO_ENABLED=0 go build ./...` — OK.
- `go test ./internal/tools/` and `go test ./cmd/agent/` — see the report for
  the full-suite output.
- New tests (all in `internal/tools/`):
  - `TestAdoptionLandedButRefKeptIsNotAFailure` (D2 contract; RED proof on
    master in the transcript above),
  - `TestAdoptionFailedChannelStaysHonestForRealErrors` (the other direction:
    `RefKept` must not swallow a real conflict),
  - `TestMergeBuildGateCleanupReapsTheRegistration` (D3 contract),
  - `TestMergeBuildGateCleanup_RemoveAllAloneLeavesTheRegistration` (D3
    mechanism, re-runnable: asserts the leak so a future "simplify it back to
    one RemoveAll" fails in test rather than on the host).

## 7. What was NOT done (honest)

- **No gate skipping occupied branches.** Measured, then reverted — the merge
  works; skipping would delay landings.
- **No fix for the leaked probe daemons** — not reproducible; recommendation
  only (§5).
- **`gopi-conflict-flags` and its 13 h-old worktree were left untouched**: the
  holder is a live process, the correct reason to keep it. Releasing it is the
  holder's job, not the sweeper's.
- The prunable registration named in the attachment was collected by the
  system's own reaper during this session (12 → 5 registrations); the D3 fix
  prevents new ones rather than papering over the old one.
