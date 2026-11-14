<!-- doc-class: historical -->
# Conflict Sweep — task 01a0adc9 (2026-09-17)

**Goal**: take the six parked, merge-conflicted branches listed in the briefing's
`conflict.txt`, merge as many as can be merged with **superficial (union / guard-clause)
resolutions only**, and for any branch whose conflict is *semantic* (the two sides rework
the same lane, or the branch is built on a retired architecture) submit a **rework** task
with the branch's diff against a known master tag, plus a written explanation.

Master at sweep time: **ee1d0fd9**; nearest older tag: **version/v20260917-0159-c890b0fe**
(= master tag c890b0fe). Deliverable branch: **merge/conflict-sweep-01a0adc9**
(worktree `/home/user/workspace/gopi-conflict-sweep`).

## Outcome table

| branch | conflicts | verdict | where the work went |
|---|---|---|---|
| `fix/d2-merge-backlog-terminal-guard-reland` | 0 | **merged** `e9cef4b2` | landing branch |
| `fix/worker-termination-attribution` | 2 (same const/line) | **merged** `fadf18bb` | union resolution |
| `feat/unsourced-kill-provenance` | 2 (one whole file) | **merged** `ac855270` | dead-copy drop |
| `feat/merger-status-trustworthy` | n/a | **already landed** (= master tip ee1d0fd9) | nothing to do |
| `feat/merger-park-tip-dedup` | 9 hunks incl. a 287-line block | **REWORK** → task `01a0add2-c880-7452-ba73-1bdf1c165970` | archived tag + patch |
| `feat/foreground-component-lifecycle` | 5 files (modify/delete + content) | **REWORK** → task `01a0add2-cc9b-76ab-aeff-b494a2647b8f` | archived tag + patch |

Two branches were **deleted** after their work was preserved (archive tag + patch, see below):
`feat/merger-park-tip-dedup`, `feat/foreground-component-lifecycle`.

## The three merges that did work (and why each resolution is superficial)

1. **`fix/d2-merge-backlog-terminal-guard-reland` — 0 conflicts.** Fast-forwardable
   content; merged as `e9cef4b2` with a merge commit so the branch's provenance is visible
   on the landing branch.
2. **`fix/worker-termination-attribution` — 2 conflicted hunks, same shape**: both sides
   edited the same *stopped-worker* log line. master had just introduced the named
   constant `workerStopLinePrefix`; the branch appended a provenance suffix to the same
   line. Union: **keep master's constant, append the branch's provenance suffix**, and
   latch the attribution target once, after the explicit/claim branch — no behaviour is
   dropped from either side, which is what "superficial" means here.
3. **`feat/unsourced-kill-provenance` — 2 conflicted hunks, one of them a whole file**:
   the branch's copy of `runTaskMerger` came from a world where the merger daemon still
   existed; master's daemon-machinery-removal deleted it. Resolution: **drop the dead
   copy**, keep *both* surviving pieces of each side (`hostLifecyclePrintln` from master,
   `recordStopVerdict` from the branch) and keep master's retired-merger wording.

Verification after every merge: `go build ./...`,
`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`, `go vet ./...` — all green at the
landing tip `ac855270`, `git status --porcelain` empty.

## The two referrals — the full reasoning

Both referrals rest on the same test: **is the conflict resolvable by choosing text, or
does a resolution require choosing meaning?** Text → merge. Meaning → rework.

### A. `feat/merger-park-tip-dedup` (tip `50f6d4df`, 06:26 +0800)

- **merge-base `bf4d1941`**; master reworked *the same lane* — the park-dedup of
  `internal/tools/repo_sweep.go` — in nine commits since: `6d1c89e1`, `2a6e09e8`,
  `9064c2d8`, `69fe2e8c`, `e207811e`, `4aee0bdc`, `17db84c8`, `b627a081` (a Revert),
  `1e538147` — 753+ lines of that one file.
- `git merge` produced **9 conflicted hunks**, one of them a **287-line** conflict block.
- The two sides encode **competing identities for the same object**:
  - master: park row summary = `resolve merge conflict for <branch> in <repo>`
    (`parkTaskSummaryRowIdentityFmt`, repo_sweep.go ~:305); park tag namespace
    `archive/<branch>` (`tasktags.ArchivePrefix`).
  - branch: keys the park on the **work tip** — `repoSweepParkTipTagNamePrefix = "tip-"`
    → tag `archive/tip-<commit>`, summary `park <tag> in <repo>`, renamed ledger-head
    helpers.
- **Why that is not superficial**: a "take both" resolution leaves two live park
  identities. The dedup probe looks up one key; the write mints the other → the same
  unmerged work is parked again on every sweep. That is precisely the defect class of
  ledger row `01a0ac25` ("merger re-parks the same stale branch tip every sweep") and the
  cancelled row `01a0ac24`. Choosing an identity is a semantic decision, so it goes back
  to a developer who can also read master's fix for `01a0ac25` and decide whether the
  branch is now superseded.
- Work preserved: tag **`archive/feat/merger-park-tip-dedup`**; patch
  `rework/feat-merger-park-tip-dedup.branch-only.patch` (merge-base `bf4d1941` →
  `50f6d4df`: 5 files, 1020 insertions / 92 deletions).

### B. `feat/foreground-component-lifecycle` (tip `f545a311`, 08:12 +0800)

- **merge-base `6a6aef6d` — 121 commits behind** master; in that window master **deleted
  the machinery the branch modifies**: `cmd/agent/cli_merger_daemon.go` no longer exists
  on master (merger = pure cronjob), while the branch modifies that file and re-introduces
  `runTaskMerger` / `mergerStopped` → a modify/delete conflict.
- Two of the five conflicted files are a **duplicate of what master already has under a
  newer name**: the branch adds `component_log.go` / `componentLifecycleLine`; master
  (committed ~40 min before the branch tip) has `cmd/agent/host_lifecycle_log.go` /
  `hostLifecyclePrintLn` doing the same job. "Take both" would ship two parallel lifecycle
  loggers.
- The other conflicts are `internal/standards/baseline.go` (a numbers table master has
  since shifted) and devdoc.
- **Why that is not superficial**: the branch's subject *is* the daemon's component
  lifecycle. Merging it resurrects a retired architecture; the superficial alternative
  (drop the daemon changes, keep the rest) deletes the branch's core subject. Either way
  the decision is architectural.
- Work preserved: tag **`archive/feat/foreground-component-lifecycle`**; patch
  `rework/feat-foreground-component-lifecycle.branch-only.patch` (merge-base `6a6aef6d` →
  `f545a311`: 11 files, 824 insertions / 38 deletions — including the branch's own
  143-line measured-failure record, which the reworker should read first).

## Patches for the reworker (how to read them)

Written under the session directory, `rework/`:

- `rework/<branch>.branch-only.patch` = `git diff <merge-base> <branch-tip>` — **only the
  branch's own work**; this is the patch to read for intent.
- `rework/<branch>.vs-master.patch` = `git diff master <branch-tip>` — carries master's
  newer text too, so it is for *seeing both sides of each conflict*, never for `apply`.
- `rework/<branch>.vs-tag.patch` = `git diff version/v20260917-0159-c890b0fe <branch-tip>`
  — the same work measured from the last known master tag, as the briefing requested.
- The landing branch itself: `merge-conflict-sweep-01a0adc9.vs-tag.patch`.

Each rework task carries its two most relevant patches as **attachments**, so the reworker
does not depend on this session directory surviving.

## Suggestion to the operator (avoid producing these branches at all)

The two referrals are not unlucky conflicts; they are a **dispatch-time** problem that the
briefing's own README already names — *"a fix must be narrow"*. Both branches were opened
from a base that master then reworked in the same lane (A) or deleted outright (B), and in
both cases the branch was authored within hours of master's change. The cheap guards:

1. **Base-freshness gate at author time**: `git rev-list --count <merge-base>..master` on
   the branch before declaring it done. A branch 100+ commits behind (B: 121) is not a
   branch, it is a fork; it should be re-based or re-opened before its tip is ever parked.
2. **Lane ownership**: A and 1e538147 touched the *same* function in the same file within
   hours. A per-lane claim (the ledger already knows the lane from the task row) would have
   made one of the two wait for the other.
3. **`resolve merge conflict for <branch>` rows are a queue, not a formality**: the row
   already carries the branch and the repo. Adding "merge-base distance" and "files both
   sides changed" to the park briefing would let a sweep like this one classify
   merge-vs-rework without cloning anything.

## Verify

```bash
cd /home/user/workspace/gopi-conflict-sweep
go build ./... && CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ && go vet ./...
git status --porcelain          # empty
git log --oneline -4            # ac855270 fadf18bb e9cef4b2 …
```
