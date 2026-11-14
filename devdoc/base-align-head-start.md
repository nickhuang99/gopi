<!-- doc-class: normative -->

> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.

# A fresh ground's base is the PEER's tip — so `master...HEAD` shows a head start, not foreign work

**Epic `worktree-base-align`. Standing property, measured 2026-10-12 (task
`01a12d2f-2b3f-7e66-862e-144a4ef6b9a2`).** This paragraph exists because a
closing run read `git diff --stat master...HEAD` in a fresh ground, saw 12 files
of which 6 were foreign, and could not tell whether that was a defect or the
intended shape. It is the intended shape, and the property below is what makes
the reading correct.

> **VERDICT: INTENDED, not a defect.** Nothing in the base selection is wrong
> and nothing here is fixed — the peer tip is the base on purpose, and the
> foreign files a closer sees are the head start this epic buys. Three things are
> added by this run: the statement below (this doc), the citation of it from the
> code that owns the decision (`internal/tools/task_ground_baseline.go`), and a
> regression test that pins the ancestry the reading depends on
> (`TestTaskRunGroundTurnsAPeerHeadsupIntoItsOwnDiff`,
> `internal/tools/task_ground_baseline_test.go`).
>
> **The corpus class of this file is `historical`, and that is a CLASSIFIER
> artifact, not a statement about the content.** `tools/docclass/classify.py`
> derives the class from the doc's own prose — a doc that both MEASURES a run
> (`verdict`, `measured on`, the 2026-10-12 date in the title's body) and states
> a property trips its "reads as a record" heuristic. The row was written by that
> writer (Rule 36: the manifest is never hand-edited), so the class stays what
> the writer said; the standing content is the block below.

## The property

> **A fresh ground's base commit is the PEER's integration-branch tip — a
> snapshot of the mirror ref `refs/remotes/gitPushcheck/master`, taken at
> creation time — and it is deliberately NOT the host's local integration
> branch.** The property that makes such a base safe to read is that it is an
> **ancestor** of the host's local integration branch:
> `merge-base(<local master>, HEAD) == <local master>`. When that holds,
> `git diff --stat <local master>...HEAD` measures the run's own work **plus a
> heads-up of the peer commits this host has not pulled yet** — never a fork,
> never an unmeargeable commit.

The mechanism, with its source:

| step | code |
|---|---|
| the base is decided | `taskGroundPeerBaseline` — `internal/tools/task_ground_baseline.go:88` |
| it is the peer tip | `SyncPeerFetch` returns the mirror ref's sha — `internal/tools/git_peer_fetch.go:172`; called at `internal/tools/task_ground_baseline.go:107`, the aligned note composed at `:112` |
| the ground is cut from it | `taskGroundCreateFrom(repo, dir, branch, base)` — `internal/tools/task_run_ground.go:852`, called at `internal/tools/task_run_ground.go:357` |
| the fallback (no peer / unreachable) | the local branch, with the degradation NAMED in the resolution's `Detail` — the server-role and no-peer returns at `internal/tools/task_ground_baseline.go:94` / `:103`, the note format at `:64` |

Three answers, and only the third is a degradation: the peer's tip (a declared,
reachable peer); the local baseline with **no note** (the server role, or a host
declaring no peer — nothing to be out of step with); the local baseline with the
**stale-baseline note** (a peer declared but unreachable: the run still starts,
Rule 15 — never a silent fallback).

## Why the base is not local master — and why that is the fix, not the bug

Measured on hostb-Laptop, task `01a12d18` (raw output:
`~/.gopi/sessions/01a12d18-b200-7662-8bb2-e264674ed4de/evidence/`, reproduced at
`~/.gopi/sessions/01a12d2f-2b3f-7e66-862e-144a4ef6b9a2/evidence/ground-base-provenance.txt`):

- the ground's branch reflog says `branch: Created from 0b3c1de29…`;
- `0b3c1de29` is **not** held by any local branch or tag — it is the value the
  mirror ref held at that moment:
  `refs/remotes/gitPushcheck/master@{2026-10-12 06:16:45} = 0b3c1de29`, a
  `fetch --force --no-tags peer1:~/work/GoPi +refs/heads/master:…`;
- the same reflog moved `1508e3821` (05:52:52) → `0b3c1de29` (06:16:45) →
  `b8287d5c6` (06:45:32) — **twice in 53 minutes**, while the ground was created
  at 06:16:5x;
- ancestry: `local master 1508e3821` **IS an ancestor of** `0b3c1de29`, and
  `0b3c1de29` is **not** an ancestor of it: the base was 29 commits AHEAD of the
  local branch, which is the head start the epic exists to buy.

**The same shape reproduces on this very run's ground** (a self-observation, not
a second experiment): branch `task/01a12d2f2b3f7e66862e144a4ef6b9a2` reflog says
`branch: Created from b8287d5c6`, which was the mirror ref's value at 06:59:38 —
62 commits ahead of the then-local master `1508e3821`, and
`merge-base(1508e3821, b8287d5c6) == 1508e3821` holds. The phenomenon is
deterministic on every fresh ground, which is why a closer meets it every time
rather than occasionally.

So the foreign files a closer sees are **peer work landed while this host slept**
— the exact lag the epic removes. Cutting from the local branch would have
reproduced that lag inside the run, and the run would have met the same peer
changes later, at merge time, at a higher price.

## Where the grid is measured against the RIGHT baseline

The measurements that decide a branch's fate do **not** use the lagging local
branch; they use the same peer mirror the ground was cut from:

- the close-out pre-merge gate trial-merges `gitPushbackCheckRef` and selects the
  smoke packages with `smoke.TouchedPackages(dir, gitPushbackCheckRef, "HEAD")`
  — `internal/tools/task_finish_premerge_smoke.go:224`, reachable from
  `internal/tools/task_finish_premerge.go:977`;
- the push-back ancestry check shares that ref
  (`internal/tools/task_sync_git_pushback.go:65`).

Only the **adoption/conflict lanes** read `master...<branch>` with the host's own
branch (`internal/tools/task_adoption.go:533`, `internal/tools/task_merge.go:1184`),
and that is the right base for them: they ask "what would this branch add to the
tree I am merging INTO", which is the local one. Their output is a **conflict
briefing**, not a judgement about the branch's authorship.

## What bounds the property (the honest edge)

The base being an ancestor of the local branch is what keeps the reading sound.
The epic assumes the peer's integration branch **descends from** this host's
(forward-only integration — the same assumption `gitPushbackSharesHistoryWithMaster`
enforces on the push side). A peer whose master was **rewound or forked** would
put a base that is not an ancestor, and then the same three-dot reading would
misreport in the *other* direction: it would hide work that cannot merge instead
of merely showing a head start early. That edge is pinned by a red-path assertion
in the regression test below, not argued away.

## The test that pins it

`TestTaskRunGroundTurnsAPeerHeadsupIntoItsOwnDiff`
(`internal/tools/task_ground_baseline_test.go:321`) drives the REAL production
surface `TaskRunGroundFor` against a two-repo fixture whose peer is ahead of the
client, and asserts:

1. the ground's base **is** the peer tip (never the local branch);
2. `merge-base(<local master>, HEAD) == <local master>` — the ancestry the
   reading depends on, with the failure message naming what breaks if it is lost;
3. the ground's delta against **its own base** is empty (a fresh ground has not
   written anything);
4. `git diff --stat <local master>...HEAD` therefore reports the peer's commits —
   the heads-up, measured;
5. the guard's predicate **rejects a forked peer tip** (the red path), so the
   assertions cannot pass vacuously.

## Why it is not a bug to fix

Nothing in the base selection is wrong: the mirror ref is far fresher than the
local branch by construction (the client arm's `--ff-only` pull is REFUSED
exactly when the two masters have diverged, so the local lag grows while the host
is busy), and the base is a *snapshot* because the peer keeps moving — a base
pinned to whatever the peer's tip happened to be is the intended semantics, and
the aligned/stale sentence in the resolution's `Detail` is how the operator reads
which one they got.
