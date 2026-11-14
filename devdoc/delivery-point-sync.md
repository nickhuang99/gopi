<!-- doc-class: historical -->
# Delivery-point sync — the worker delivers a branch that is ALREADY aligned with master

Task: **rework of `01a0ac24`** (park mechanism retired) — the still-valid half:
*a worker must deliver a branch that is already aligned with the latest master,
resolve the conflicts its own context can resolve cheaply, rebuild + vet, and
only then deliver; if it cannot, it must not deliver silently.*

Branch: `feat/delivery-point-sync`
Implementation: `internal/tools/task_finish_deliverysync.go` (new, 349 lines) +
`internal/tools/task_finish_premerge.go` (extracted multi-check runner) +
`internal/tools/task_finish_cleanup.go` (the local arm's step).
Evidence scripts: `scripts/nonff-able-fraction.sh` (in-repo, the BEFORE half) and
`scripts/nonff-able-cohort-after.sh` (in-repo, the AFTER half: drives the real
close-out verb over a clone of the repo and re-measures) —
`~/workspace/gopi-delivery-e2e-run.sh` (real-verb E2E),
`~/workspace/gopi-delivery-e2e-real-door.sh` (clone + real declared build).

---

## 1. Why the delivery point (the cost argument)

A branch handed over 200 commits behind master is not a deliverable: it is a merge
problem with the worker's name on it, moved to whoever picks it up. The two
directions cost different things:

| | merge master INTO the branch, at the worker | un-pick the skew, at the merger |
|---|---|---|
| who holds the context | the agent that wrote the files — the design decision behind every line it touches is still in its head | an agent that has never seen the change |
| conflict resolution | one merge, understood against the branch's own intent | one merge, against a change it must first reconstruct |
| history | the branch gains master; nothing is rewritten | same, but 200 commits of skew must be attributed first |
| build/vet | the merged tree is compiled **once**, by the producer | the merged tree is compiled after the resolution, by the consumer |
| when it fails | the branch keeps its check-out and the worker resumes from the collision | a conflict-resolution task is created for work that had an author |

The cheap direction is the one where the person with the context does the merge,
and it is cheap **for the system** too: the merge commit the worker produces is
exactly the merge the merger would otherwise have to make, so nothing is
duplicated — it is moved earlier, to where it is cheapest.

**The delivery point** is the moment the run's worktree is released (close-out):
after it, the branch has no ground, and the next actor is whoever finds it. A
gate downstream (the merger) cannot act on "do not deliver" — by then the delivery
happened. A gate per-caller would be bypassed by every other caller. So the gate
sits in the close-out chain, immediately before the release.

## 2. The boundary with `01a0ab18` — exact coverage, and the gap

`01a0ab18` landed `finishCleanupPreMergeVerify` (`internal/tools/task_finish_premerge.go`,
ancestor of `master`). Its coverage, verified by reading the code on master
(`bf9fbef8`, `git merge-base --is-ancestor bf9fbef8 master` → yes):

| | `01a0ab18` (`finishCleanupPreMergeVerify`) | this task (`finishCleanupLocalSync`) |
|---|---|---|
| sits in | **the CLIENT arm** of `runFinishCleanupChainTarget` (`task_finish_cleanup.go:2109-2145`) — between the dirty guard and `git push` | **the LOCAL / server arm**: `finishCleanupServerRecycle` (`task_finish_cleanup.go:846`), immediately before `git worktree remove` |
| upstream it syncs with | the **peer's** master: `git fetch --force --no-tags <peer> +refs/heads/master:<mirror>` into `refs/remotes/gitPushcheck/master` | **this repo's own** integration branch: `master`/`main`, local first, remote-tracking fallback, tip-most wins — **no fetch, no network** |
| its act | the push, or the withheld push | the worktree release (the branch is preserved and *is* this host's merge input) |
| refusal | `merge --abort` + **no push** + `premerge-refused` | unwind to the pre-merge tip + **no worktree release** + `premerge-refused` |
| its checks | the repo's DECLARED build (`config.ResolveRepoBuild`) | the declared build **+ `go vet ./...`** (Go module roots only) |
| reachable when | a **peer is declared** (`settings.json` `sync_peers` non-empty) | **no peer** — i.e. every single-node deployment |

**The gate was effectively dead on this host, and that is the gap.** Measured
2026-09-17: `~/.gopi/settings.json` has `"sync_peers": []` ⇒ `HasSyncPeers()` false
⇒ `finishCleanupServerTarget() == ""` (`task_finish_cleanup.go:909`) ⇒
`taskFinishCleanup` dispatches to `finishCleanupServerRecycle` *before the client
arm is ever reached* (`task_finish_cleanup.go:846`), and `IsServerNode()` is
`true` on `peerbox`. That arm's whole step set was:

```
① branch resolution     ② ownership gate   ③ dirty gate
④ worktree remove       ← and the branch is KEPT
```

No merge, no build, no alignment check, no ff-ability. So the branch that becomes
the merger's input on this host is neither verified nor ff-able — which is exactly
what the before-measurement shows (100% non-ff-able, §5).

**What this task adds, in one sentence:** the same delivery-point discipline
(would it merge? does the merged state build and vet?) applied to the arm that
this deployment actually takes, with the sync point being the repo's own
integration branch instead of a peer's.

## 3. Mechanism (cheap path always first)

```
① integration ref  → this repo's master (then main), local before remote-tracking, tip-most if both
② up to date?      → merge-base --is-ancestor <ref> HEAD          ⇒ ZERO COST: no merge, no build, no vet
③ trial merge      → git merge --no-edit <ref> in the run's OWN worktree   ⇒ conflict = REFUSED
④ build + vet      → the repo's DECLARED build + `go vet ./...` in ONE detached checkout of the merge commit
⑤ unwind           → refused ⇒ reset --hard to the recorded pre-tip (never a conflicted tree)
```

A clean merge whose checks pass **stays**: its merge commit is the deliverable's
own alignment with master, and it is the merge the merger would have made anyway.

**What it never does** (all asserted in tests):

- never **rebases** — a rewrite of the branch's own commits (Rule 8/23). The
  merge direction is also the direction that does not invalidate tips already
  recorded against the branch;
- never force-merges and never edits code to make a merge succeed;
- never merges in the **main checkout** — the ground must be checked out ON the
  delivered branch (`finishCleanupPreMergeGroundOnBranch`); otherwise ABSTAIN;
- never deletes anything: a refusal keeps branch, worktree and commits;
- never invents a build (Rule 12): a repo with no declared build merges and
  reports the abstention — a repo that is not a Go module root runs no vet either;
- never merges into an already-released worktree, and never silently releases an
  unverifiable one: every abstention is recorded (`premerge-unverified`) and every
  refusal is recorded (`premerge-refused`) on the row's own event trail (Rule 15).
- no re-introduction of the park mechanism: nothing is parked, nothing is queued
  for the merger — the branch simply stays checked out until it is deliverable.

## 4. Measured evidence — the real operator face

Binary built from this worktree (`CGO_ENABLED=0 go build -o … ./cmd/agent/` at
`64f4345a`), the real verb `gopi task finish-cleanup --task T --apply`, a real
ledger created by gopi, real git. Raw transcript:
`~/workspace/out.txt` + `~/workspace/gopi-delivery-e2e/real-door.log`;
re-runnable: `~/workspace/gopi-delivery-e2e-run.sh [world]`.

> The scratch trees quoted below (`~/workspace/gopi-delivery-e2e/`,
> `~/workspace/cohort-*`) are **throwaway** — ~2.7G of clones and worktrees that
> were deleted after the run. The transcripts are archived at
> `~/.gopi/sessions/01a0ac6a-2876-7244-85c3-e481057c76a7/evidence/` (same file
> names) together with the three harness scripts, so every number below is
> re-readable without the scratch tree, and `scripts/nonff-able-cohort-after.sh`
> re-produces §5's table from nothing. Paths inside the pasted output are
> therefore historical, not live.

### Scenario C — already up to date → zero cost (acceptance 3)

Declared build deliberately `exit 9`, so *running it at all* would turn the
scenario red.

```
$ GOPI_SETTINGS=…/uptodate/settings.json gopi task finish-cleanup --task e2e-uptodate --apply
e2e-uptodate branch=feat/deliverable done=[pre-merge,worktree]
finish-cleanup (APPLY): candidates=1 removed=1 settled=0 refused=0
---- branch tip / commit count before: 6744ca06… / 2
---- branch tip / commit count after : 6744ca06… / 2
   ZERO COST: tip unchanged
   ZERO COST: no merge commit
---- worktree released: worktree removed
   [up-to-date] ff-able into master: YES
```

### Scenario B — behind but cleanly mergeable → pre-merge, build, deliver (acceptance 2)

```
$ GOPI_SETTINGS=…/clean/settings.json gopi task finish-cleanup --task e2e-clean --apply
e2e-clean branch=feat/deliverable done=[pre-merge,worktree]
finish-cleanup (APPLY): candidates=1 removed=1 settled=0 refused=0
---- commits on the branch before: 2  after: 4
---- master (d1de0de2…) is an ancestor of the delivered branch?
   YES — the branch now contains master (ff-able at the merger)
---- the merge commit the branch carries:
d4a2eed delivery-point sync: merge master into the delivered branch before close-out
d1de0de master adds a file
---- the DECLARED BUILD actually ran (marker file): marker present
---- worktree released: worktree removed
   [after] ff-able into master: YES
```

### Scenario A — behind + conflict → refused BEFORE delivery (acceptance 1)

```
$ GOPI_SETTINGS=…/conflict/settings.json gopi task finish-cleanup --task e2e-conflict --apply
e2e-conflict skipped: branch does NOT merge with this repo's integration branch
  (conflict classes: content, modify/delete, rename, binary): Auto-merging a.txt
  merge unwound — the worktree is back at the pre-merge tip
finish-cleanup (APPLY): candidates=1 removed=0 settled=0 refused=1
---- worktree KEPT: /home/user/workspace/gopi-delivery-e2e/conflict/wt
---- trial merge unwound (git status --porcelain prints nothing)
---- branch tip unchanged: ba8748c1… (pre-merge ba8748c1…)
---- branch still exists: yes: feat/deliverable
---- the row's own event trail:
premerge-refused|branch does NOT merge with this repo's integration branch (…): Auto-merging a.txt merge unwound …
```

### Scenario D — the merge is clean but the merged state does not build → refused

```
$ GOPI_SETTINGS=…/badbuild/settings.json gopi task finish-cleanup --task e2e-badbuild --apply
e2e-badbuild skipped: the merge with the integration branch does not BUILD — the run was
  NOT closed out (the branch keeps its check-out until it compiles): build: echo
  compile-error-undefined-thing >&2; exit 3: compile-error-undefined-thing
  merge unwound — the worktree is back at the pre-merge tip
   worktree KEPT: yes
```

### The real door — a CLONE of `~/work/GoPi`, the REAL declared build

`~/workspace/gopi-delivery-e2e-real-door.sh <branch>` (Rule 29: a clone, never
the main checkout or a foreign worktree). Two runs, end to end:

**clean branch — delivered** (`~/workspace/gopi-delivery-e2e/real-door.log`):

```
clone HEAD: c890b0fe (master)   main repo master: c890b0fe
worktree (made): …/live/wt  on feat/merger-status-trustworthy
BEFORE: ff-able: 0  not-ff-able: 1  non-ff-able-fraction: 1/1 (100.0%)
build = CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/     ← verbatim from settings.json
$ GOPI_SETTINGS=…/live/settings.json gopi task finish-cleanup --task live-delivery-sync --apply
…
branch tip / commits before: 64f4345a… / 3988
branch tip / commits after : c890b0fe… / 3993
master is an ancestor of the delivered branch?   YES — ff-able into master
worktree released?   removed
AFTER: ff-able: 1  not-ff-able: 0  non-ff-able-fraction: 0/1 (0.0%)
```

**conflicting branch — refused, nothing delivered** (`~/workspace/gopi-delivery-e2e/real-door-conflict.log`):

```
$ GOPI_SETTINGS=…/live/settings.json gopi task finish-cleanup --task live-delivery-sync --apply
live-delivery-sync skipped: branch does NOT merge with this repo's integration branch
  (conflict classes: content, modify/delete, rename, binary): Auto-merging
  cmd/agent/cli_tasks_worker.go merge unwound — the worktree is back at the pre-merge tip
finish-cleanup (APPLY): candidates=1 removed=0 settled=0 refused=1
   exit=0
branch tip / commits before: dd5dde45… / 3894
branch tip / commits after : dd5dde45… / 3894      ← unchanged, nothing delivered
master is an ancestor of the delivered branch?   NO
worktree released?   still present: …/live/wt      ← kept: the ground the worker resumes from
the row's event trail:
premerge-refused|branch does NOT merge with this repo's integration branch (…): Auto-merging cmd/agent/cli_tasks_worker.go merge unwound …
AFTER: ff-able: 0  not-ff-able: 1                     ← the branch is NOT ff-able and NOT handed over
```

## 5. Before/after — the non-ff-able fraction (acceptance 4)

`scripts/nonff-able-fraction.sh <repo> [integration-ref] [days]` defines the
cohort mechanically: every local branch except the integration branch and
`archive/*`, and ff-able ≡ `git merge-base --is-ancestor <integration> <branch>`
exits 0. `days=0` = all local branches; `days=N` = only branches whose tip is
younger than N days (the "newly produced" window).

**BEFORE** (this host's real repo, master `c890b0fe`, 2026-09-17):

```
BRANCH                                       FF-ABLE   BEHIND-BY
feat/daemon-machinery-removal                NO        5
feat/delivery-point-sync                     NO        5
feat/foreground-component-lifecycle          NO        105
feat/merger-park-tip-dedup                   NO        239
feat/merger-status-trustworthy               NO        5
feat/unsourced-kill-provenance               NO        34
feat/worker-term-exit                        NO        103
fix/d2-merge-backlog-terminal-guard-reland   NO        5
fix/heavy-tests-classification-rework        NO        273
fix/worker-termination-attribution           NO        103

cohort: repo=/home/user/work/GoPi integration=master days=0 branches=10
ff-able: 0  not-ff-able: 10  non-ff-able-fraction: 10/10 (100.0%)
```

The same script re-run at the END of this task (2026-09-17T02:07Z, after this
branch's own sync in §7 — so `feat/delivery-point-sync`, the one branch whose
close-out went through the new gate, is the one that flipped):

```
BRANCH                                               FF-ABLE   BEHIND-BYTES
feat/delivery-point-sync                             yes       0
… (the other 9 unchanged) …
cohort: repo=/home/user/work/GoPi integration=master days=0 branches=10
ff-able: 1  not-ff-able: 9  non-ff-able-fraction: 9/10 (90.0%)
```

i.e. the live repo's fraction moved 10/10 → 9/10 because exactly one legacy
branch (this one) has been through the delivery point since the gate landed. The
cohort experiment below is the controlled version of the same measurement —
**every** branch put through the real verb, instead of waiting for each one's
own run.

> **The live number is a moving measurement, and it is not this task's metric.**
> Re-run at the end of the task it reads `8/10 (80.0%)`: a *second* legacy branch
> (`feat/merger-status-trustworthy`) flipped to ff-able at 10:13 by **another
> worker's own run** (`git reflog` shows `reset: moving to master` then a fresh
> commit — its own close-out path, not this task's, and not something this task
> touched). Two directions of evidence live here: the live fraction only falls as
> each branch is delivered (so it is dominated by the legacy backlog, 8 of 10 of
> which predate the gate and have no reason to move), while the controlled cohort
> below puts a branch through the gate *now* and reports the property directly.
> The backlog is not rewritten: moving a legacy branch requires re-landing its
> work, which is the merger's job, not a delivery gate's.

**AFTER** — the sync applied to each branch of that cohort, in a CLONE of the
real repo, with the real declared build, driven by the REAL verb. Re-runnable as
one command (this is the acceptance-4 harness; it needs no hand-written world):

```
GOPI_COHORT_ROOT=<scratch off '/' (Rule 22)> \
  bash scripts/nonff-able-cohort-after.sh --main ~/work/GoPi --gopi <this branch's bin/gopi>
```

Transcript `~/workspace/cohort-final.log`, `exit=0`, master `c890b0fe`,
binary built from this worktree:

```
clone: …/run/repo @ c890b0fe
build: CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/     ← from settings.json, never invented

=== feat/daemon-machinery-removal   (before: ff-able=NO, 5 commits behind)
$ gopi task finish-cleanup --task cohort-after-1 --apply
cohort-after-1 branch=feat/daemon-machinery-removal done=[pre-merge,worktree]
-> feat/daemon-machinery-removal                    ff-able: NO -> YES tip=b550e76a trail=(none)
=== feat/delivery-point-sync   (before: ff-able=YES, 0 commits behind)
$ gopi task finish-cleanup --task cohort-after-2 --apply
cohort-after-2 branch=feat/delivery-point-sync done=[pre-merge,worktree]
-> feat/delivery-point-sync                         ff-able: YES -> YES tip=3c396080 trail=(none)
=== feat/foreground-component-lifecycle   (before: ff-able=NO, 105 commits behind)
cohort-after-3 skipped: branch does NOT merge with this repo's integration branch
  (conflict classes: content, modify/delete, rename, binary): Auto-merging cmd/agent/cli_tasks.go
  merge unwound — the worktree is back at the pre-merge tip
-> feat/foreground-component-lifecycle              ff-able: NO -> NO  tip=f545a311 trail=premerge-refused
=== feat/merger-park-tip-dedup   (before: ff-able=NO, 239 commits behind)
cohort-after-4 skipped: … conflict classes … Auto-merging cmd/agent/cli_tasks.go merge unwound
-> feat/merger-park-tip-dedup                       ff-able: NO -> NO  tip=50f6d4df trail=premerge-refused
=== feat/merger-status-trustworthy   (before: ff-able=NO, 5 commits behind)
cohort-after-5 branch=feat/merger-status-trustworthy done=[pre-merge,worktree]
-> feat/merger-status-trustworthy                   ff-able: NO -> YES tip=c890b0fe trail=(none)
=== feat/unsourced-kill-provenance   (before: ff-able=NO, 34 commits behind)
cohort-after-6 skipped: … Auto-merging cmd/agent/cli_tasks.go merge unwound
-> feat/unsourced-kill-provenance                   ff-able: NO -> NO  tip=c2df1f0d trail=premerge-refused
=== feat/worker-term-exit   (before: ff-able=NO, 103 commits behind)
cohort-after-7 skipped: … Auto-merging cmd/agent/cli_tasks_worker.go merge unwound
-> feat/worker-term-exit                            ff-able: NO -> NO  tip=dd5dde45 trail=premerge-refused
=== fix/d2-merge-backlog-terminal-guard-reland   (before: ff-able=NO, 5 commits behind)
cohort-after-8 branch=fix/d2-merge-backlog-terminal-guard-reland done=[pre-merge,worktree]
-> fix/d2-merge-backlog-terminal-guard-reland       ff-able: NO -> YES tip=51a9c5bd trail=(none)
=== fix/heavy-tests-classification-rework   (before: ff-able=NO, 273 commits behind)
cohort-after-9 branch=fix/heavy-tests-classification-rework done=[pre-merge,worktree]
-> fix/heavy-tests-classification-rework            ff-able: NO -> YES tip=94d6157b trail=(none)
=== fix/worker-termination-attribution   (before: ff-able=NO, 103 commits behind)
cohort-after-10 skipped: … Auto-merging cmd/agent/cli_tasks_worker.go merge unwound
-> fix/worker-termination-attribution               ff-able: NO -> NO  tip=ef772e72 trail=premerge-refused

cohort: 10 branches   became-ff-able(delivered)=5   refused-with-a-recorded-reason=5   non-ff-able-and-UNEXPLAINED=0
```

**Before → after, in the numbers the acceptance names:**

| | BEFORE (no gate) | AFTER (this gate) |
|---|---|---|
| branches delivered ff-able | 0/10 (0%) | **5/5 deliverable ones (100%)** |
| branches delivered non-ff-able **and unexplained** | 10/10 (100%) | **0/10 (0%)** |
| branches non-ff-able but BLOCKED with a recorded reason | 0 (the gate did not exist) | 5/10 — every one with `premerge-refused` on its row |
| non-ff-able fraction among *newly produced* branches | 100% | **0%** — a branch that cannot be synced is no longer *produced* as a deliverable; it stays checked out and the worker resumes |

The last row is the epic's acceptance 4 property, and the reason it is 0% and not
50%: the five `NO -> NO` rows were **refused, not delivered** — their tip is
unchanged, their worktree was kept, and each carries the exact conflict the real
merge reported (`cmd/agent/cli_tasks.go` ×3, `cli_tasks_worker.go` ×2). A refusal
is the gate working: those five never left close-out as an undiagnosed merge
problem (constraint 2 forbids papering over a real semantic conflict). The
`YES -> YES` row is the zero-cost path at cohort scale: already up to date ⇒ the
verb ran the merge-free path, added no merge commit, and did not run the declared
build.

> **A real bug this harness found, in the harness itself** — worth recording
> because it is a green-blind measurement (Rule 15): the script's default cohort
> was taken from the clone's **local** heads, and a fresh `git clone` has only the
> integration branch as a local ref, so the default run reported
> `cohort: 0 branches … UNEXPLAINED=0` and **exited 0** — a clean-looking pass
> over an empty cohort. Two defects fixed here: the default cohort now comes from
> `refs/remotes/origin/*`, and the lock file's directory is created before
> `exec 9>` (the `2>/dev/null` fallback printed `No such file or directory` and
> silently continued). An empty cohort is now impossible on a clone with branches,
> and the earlier draft numbers in this section (`9 branches, 4/9`) came from a
> hand-written script with a stale-lock/waitress hazard; **the transcript above
> supersedes it**.

The equivalent earlier measurement with a hand-written harness
(`~/workspace/gopi-delivery-e2e-after-measure2.sh`, `after-measure2.log`) gave the
same shape on 9 branches (0/9 → 4/9 ff-able, 5 refused-visible, the same five
conflict files) — kept only as a cross-check; the in-repo script above is the
reproducible one, and its two harness bugs (default cohort from local heads,
lock-file directory) were found and fixed by running it.

**Zero-cost half (acceptance 3) confirmed at cohort scale by the `YES -> YES`
row itself:** `feat/delivery-point-sync` was already up to date (`before:
ff-able=YES, 0 commits behind`), and the verb released it at zero cost — tip
`3c396080` unchanged, no merge commit, and the declared build never ran
(§4 scenario C is the controlled proof: tip and commit count identical, with a
declared build that FAILS on purpose, so running it at all would turn the
scenario red).

### Why nothing else changed the number

The gate only moves the number for branches whose **close-out** runs after it
lands. Existing branches stay non-ff-able until their own run — and the sync is
exactly what makes their run ff-able. This is deliberate: a gate that silently
rewrote the whole repo's branches would be an unattended history rewrite.

## 6. Where the quality gate belongs

**The enforcement belongs in the close-out chain** — a gate is only real when it
runs at the moment the delivery happens, on the artifact that is being delivered,
and it is the only layer that can withhold the delivery. A rule in a document is
read by an agent that may not read it, at a moment that may not be the delivery
moment ("原则无效，检查才有效").

**The obligation belongs in `GOPI.md` too**, for a different reason: an agent
must know *why* its close-out may refuse, so the refusal reads as a contract
rather than as a malfunction, and so a worker plans for it (deliver early,
re-merge when master moves). The two are not redundant — the chain enforces, the
rule explains. Proposed rule text (an operator edit to `GOPI.md`, deliberately not
made unilaterally by this task):

> **A worker delivers a branch that is already aligned with master.** Before
> close-out, merge the integration branch into the delivered branch, rebuild and
> `go vet`, and only then release the worktree. A branch that cannot merge, or
> whose merged state does not build, is NOT delivered — it keeps its check-out
> and its refusal is recorded. Never rebase a pushed branch, never force a merge,
> never make a merge succeed by editing code.

## 7. Verification

| what | command | result |
|---|---|---|
| build | `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` | OK |
| build (all) | `go build ./...` | OK |
| vet | `go vet ./...` | exit 0, no output |
| the new scenarios | `go test ./internal/tools/ -run 'DeliveryPointSync' -count=1` | ok (25.7s) |
| the touched package | `go test ./internal/tools/ -count=1 -timeout 30m` | **ok 888.256s, exit 0** (`~/workspace/tools-test-full.log`) |
| the cohort re-measurement | `bash scripts/nonff-able-cohort-after.sh --main ~/work/GoPi --gopi bin/gopi` | exit 0, **10 branches: 5 delivered→ff-able, 5 refused-with-reason, 0 unexplained** (§5) |
| the branch is ff-able | `git merge-base --is-ancestor master feat/delivery-point-sync` | exit 0 (master `c890b0fe`) |

### The tests (all physical asserts, no mocks — git's own semantics are the subject)

`internal/tools/task_finish_deliverysync_test.go` (446 lines):

- `TestDeliveryPointSyncConflictBlocksTheRelease` — A: refusal, worktree kept, merge unwound, tip unchanged, branch alive;
- `TestDeliveryPointSyncConflictIsRecordedOnTheRow` — A's visibility half: `premerge-refused` on the row's trail;
- `TestDeliveryPointSyncCleanMergeBuildsThenReleases` — B: master becomes an ancestor, commit count grows, the declared build RAN (marker), worktree released;
- `TestDeliveryPointSyncFailingBuildBlocksTheRelease` — the build half of the refusal names `build`;
- `TestDeliveryPointSyncVetRunsOnTheMergedState` — the vet half really runs (a non-constant format string planted on master's side);
- `TestDeliveryPointSyncUpToDateIsZeroCost` — C: tip and commit count unchanged, the (deliberately failing) build never runs;
- `TestDeliveryPointSyncNeverMergesInTheMainCheckout` — the Rule 29 guard: no ground on the delivered branch ⇒ abstain, master untouched;
- `TestDeliveryPointSyncIntegrationRefResolution` — `master` beats a remote-tracking ref, `main` resolves, neither ⇒ no ref (never a guess);
- `TestDeliveryPointSyncIsOffByTheKillSwitch` — one feature, one switch (`GOPI_FINISH_CLEANUP_PREMERGE`);
- `TestDeliveryPointSyncPlanModeIsPredictedNotRun` — a dry run predicts the step and touches nothing.

## 8. Not done (honest)

- The **push arm** (a peer declared) keeps `01a0ab18`'s behaviour unchanged: it
  does not run `go vet`, and it syncs with the peer's master only. Making the two
  arms diverge less (one shared check list, a peer-aware local sync) is a
  follow-up, deliberately not folded in — the epic scoped this task to the
  uncovered part, and the client arm's tests pin its current contract.
- The **after** number for the live cohort is bounded by the observation window:
  the fraction falls only for branches whose close-out runs after the gate lands
  (§5 shows the live repo at 9/10 and the controlled cohort at 0 unexplained).
  The script is in-repo so the measurement is repeatable on any date; the
  within-this-task proof is the cohort run plus scenario B's `is-ancestor` check.
- `GOPI.md` carries no rule yet (§6); the proposed text is above, and the
  enforcement does not depend on it.
- Two harness defects were found *by running the in-repo script* and are fixed in
  the same commit, recorded here because one of them was a green-blind
  measurement: (1) the default cohort came from the clone's local heads, so a
  fresh clone measured **0 branches and exited 0** — a pass over nothing; the
  cohort now comes from `refs/remotes/origin/*`. (2) `exec 9>"$W/.cohort.lock"`
  ran before `$W` existed, so the lock silently failed to open and the script
  continued *without* the mutual-exclusion it documents; `$W` is now created
  first. Neither is covered by a Go test — they are shell-level, and the
  reproducible guard is running the script with no `--main` on a clone.

---

## 9. The delivery lane this gate did not cover — the push-back pushes a branch the peer cannot land (2026-09-21, task `01a0c32e`)

§1–§8 close the alignment hole at the **close-out** arms. Two doors stayed open,
and the second is the one the field actually used:

### 9.1 What was already right (measured, not assumed)

| arm | gate | reachable when |
|---|---|---|
| CLIENT (`finishCleanupPreMergeVerify`, `2fdc65e4`) | fetch peer master → trial merge → declared build → **push only if both pass** | a peer is declared |
| LOCAL/SERVER (`finishCleanupLocalSync`, `3c396080`) | resolve this repo's master → `merge-base --is-ancestor <ref> HEAD` cheap path → real trial merge → declared build + vet → unwind; a REFUSAL does not release the worktree | no peer declared |

Both act at the delivery point, and a refusal leaves the branch with the ground
to be fixed in. So "the worker is never told to align" is NOT the defect.

### 9.2 The open door: the push-back lane had no fast-forwardability gate

`gitPushbackPushOne` (`internal/tools/task_sync_git_pushback.go`) pushed **every**
unmerged local branch, and on a rejected push retried it once under
`<name>-<HHMMSS>` — a FORK. The peer lands a branch with exactly one operation,
`git merge --ff-only <branch>`, which succeeds only when the branch CONTAINS the
peer master's tip; so a branch that is behind cannot be landed there, and the
fork cannot either. The push moved the merge problem onto the integrator and
parked it there (memory #1937: one topic arrived as five branches on one tip,
61 pending rework rows).

The gate that closes it existed **only on an archived branch**
(`fix/rework-ff-convergence`, archived as `archive/fix/rework-ff-convergence`) —
it never reached master. Ported here: `gitPushbackFastForwardsOntoMaster`
(`merge-base --is-ancestor <mirror> <branch>` — the merger's own predicate in the
merger's polarity), the rename-retry deleted (a rejection is reported with its
remedy, never forked), `gitPushbackFlush.Renamed` removed with it.

Measured on HP (this task's own host) at the time of writing: **2 of 6** local
non-integration branches — every `rework/` branch — are 36–37 commits behind
master and not ff-able. Those are precisely the branches the lane would have
pushed to the peer as merges the peer cannot take.

### 9.3 What is still NOT closed (honest)

- **The abstention is silent where the delivered branch has no ground.** Both
  close-out arms ABSTAIN (`finishCleanupPreMergeSkipGroundMark`) when the
  branch has no linked worktree — `rework/chat-answer-single-writer-fwd` and
  `rework/chat-three-branches` on this host are exactly that: 36–37 commits
  behind, no ground, so the gate cannot merge them and the delivery is not
  blocked. Closing it needs an alignment path that does not require a worktree
  the run owns (a scratch checkout of the branch). Deliberately NOT attempted
  inside the push lane: rewriting a published branch's content there would
  violate §3's "never rebases a rewrite of the branch's own commits".
- **`gopi harvest`'s push-back is a byproduct of a db sync** — it inherits the
  gate, but a harvest-run branch verdict is visibly refused. A dedicated
  "align my branches before delivery" verb does not exist; the remedy printed by
  the refusal (merge master into the branch, re-deliver) is executed by hand.
- The `pull-master` step of the client pass can report a refusal for a reason
  unrelated to this gate (e.g. a feature branch with no upstream): in the E2E
  world that line is noisy, and it is not a finding of this task.
