<!-- doc-class: normative -->
# The undelivered-commits rung asks the FRESH integration tip, not the ground's stale snapshot

`internal/tools/worktree_sweep_undelivered.go` +
`internal/tools/worktree_sweep_undelivered_equivalent.go`.

## The defect (measured, 2026-10-27, bug row `01a17d1b`)

The rung's PREDICATE is right and is unchanged: *a branch holding commits that no
delivery point has must be kept, because in a clone ground the directory IS the
only repository and `devworkspace.Reclaim` removes it with `os.RemoveAll`.*

What was wrong is the DATA SOURCE the count was taken against. It asked the
ground's own refs:

```
$ git -C /home/user/workspace/01a17a5f-…/ rev-parse refs/remotes/origin/master
2c8710d5798e59e4c6d180e68e5b8013c6838293        2026-10-27 06:29:29
$ git -C /home/user/work/gopi rev-parse master
934dd697d493a7fbfb267cbf4370e47fc8d9bb09        2026-10-27 19:23:41    +12h54m
```

A clone ground's `refs/remotes/origin/master` is a snapshot taken when the ground
was cloned. Nothing in the sweep fetches — correctly, Rule 35 and the epic's own
fence: 208 fetches would turn a read-only plan into a network walk. So 206 of the
208 clone grounds on this host carry a stale snapshot, and work that IS on master
reads as undelivered.

The keep sentence is then false, and it is **permanent**: a commit count relaxes
with nothing (not age, not a dead process), so the ground is never reclaimed and
its row sticks at `in_progress`.

Measured on the bug's own ground: its two commits `84cfb6f` / `c529b09` are on
master. The measurement that decides HOW to fix it:

```
ground 84cfb6f  0…   tree f7bcd8f2…   patch-id 1001d391…   subject "fix(ledger-agreement): judge STRUCTURE not existence, …"
master 3acbb60dd  …   tree 9c1e4b4c…   patch-id 3b143db1…   subject  ← byte-identical
ground c529b09        patch-id 99612d52…   subject "test(ledger-agreement): classify the no-alarm totality assertion …"
master 6e3191153      patch-id 9f97179e…   subject "test(ledger-agreement): re-point the no-alarm totality classification at its new carrier"
```

**Patch identity does NOT answer the question** (the patch-ids all differ, and
`git cherry` lists both ground commits as patch-unique `+`) — this repository
already measured that in `rework_landing_probe.go`. And the two commits need
DIFFERENT legs: `84cfb6f` landed with the same subject, `c529b09` with a rewritten
one.

## The fix — two legs, both read-only, both local

`worktreeUndeliveredSubtractEquivalent(readFrom, repo, branch, probe)` reduces the
count by commits whose work the FRESH integration tip provably carries:

- **Leg ① SUBJECT EQUALITY** — the ground commit's subject carries a signal
  (`reworkLandingSubjectCarriesSignal`: long enough, not this repo's
  sync/pre-merge/merge bookkeeping) and the fresh tip carries a commit with
  exactly that subject. One `git log --format=%H%x09%s <fresh refs>` builds the
  set once per registration.
- **Leg ② CONTENT CONTAINMENT** — every line the ground commit ADDS is present in
  the fresh tip's copy of every file it touches, and every line it REMOVES is
  gone. Covers a re-land under a new message.

`repo` is the fresh source. For a clone ground that is the MAIN CHECKOUT the
anchor record names (`"repo":"/home/user/work/gopi"`), i.e. the checkout the
merger and the operator keep current. When `repo` IS the ground (the adopt arm
passes the directory twice) the legs decline and behaviour is exactly pre-fix —
a repository compared with itself answers "delivered" by construction, the same
self-referential zero the delivery-point builder already refuses for
`refs/heads/<branch>`.

**No fetch, no network, no new artifact**: two local `git` reads per registration
against refs that already exist.

## The direction of error is the safety argument

Every unmeasurable input makes a leg DECLINE to fire, which leaves the commit
counted, which KEEPS the ground: a git failure, an unparseable diff, a binary
file, a file the fresh tip does not have, more than
`worktreeUndeliveredEquivCommitCap` (40) candidate commits, more than
`worktreeUndeliveredEquivFileCap` (24) files or
`worktreeUndeliveredEquivLineCap` (4000) changed lines. A stale FRESH master is
also safe: the legs simply do not fire — the pre-fix behaviour.

## Where it is checked

```bash
go test ./internal/tools/ -run TestUndelivered -count=1
```

| test | pins |
|---|---|
| `TestUndeliveredReleasesAGroundWhoseWorkWasRebasedIntoTheFreshMaster` | subject leg: stale snapshot, landed under a new id → collectable |
| `TestUndeliveredReleasesAGroundWhoseWorkWasRelandedUnderANewSubject` | content leg: landed under a new subject → collectable |
| `TestUndeliveredStillKeepsWorkTheFreshMasterHasNeverSeen` | **the dual**: work nowhere → still refused, unchanged sentence |
| `TestUndeliveredSubstitutesNothingWhenTheFreshSourceIsTheGround` | ground-vs-itself answers nothing |
| `TestUndeliveredRefusesACloneGroundCarryingWork`, `TestUndeliveredAllowsTheSameGroundOnceDelivered`, … | the original rung, unchanged |

Red proof (done, restored by content identity): stubbing
`worktreeUndeliveredSubtractEquivalent` to the pre-fix `return p` makes BOTH
positive tests fail with the exact keep sentence naming 1 commit, and leaves the
dual green — which is what "the fix is the data source, not the predicate" means
in machine-checkable form.

## Real run (no `--apply`)

```
before: 110 lines carry "no delivery point has"
after:   76
01a17a5f before: kept: the ground's branch master holds 2 commits that no delivery point has (…)
01a17a5f after : kept: the ground is younger than the floor (13h6m0s < 24h0m0s)
```

The 34 that stopped being refused are the measured population of the bug. The 76
that remain name work the fresh master genuinely has never seen — the dual,
observed on real grounds rather than only in a fixture.
