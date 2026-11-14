<!-- doc-class: historical -->
# Git Sync Channel — the serve tick's git half (epic 2026-09-01)

M4.13 (t-1788093118) delivered only the delta protocol + harvest; the serve
tick pass's git channel had no owner (responsibility-matrix audit; second
case of a completed epic with a sub-feature orphaned).

## Design (role-asymmetric, per the epic's 定案)

One engine, two passes (`internal/tools/task_sync_git.go`), selected by
`tools.IsServerNode()`:

**HP (client) — GitSyncClientPass**:
1. push local `feat/*` back to `<peer0>:/home/user/work/GoPi` — bug038 idempotency
   (git's up-to-date detection = exit 0; re-ticks are free)
2. `git pull --ff-only` — the client NEVER merges; diverged master =
   reported skip, never fatal

**peerbox (server) — GitSyncServerPass**:
1. `git fetch --prune` every declared sync peer
2. adoption chain (`AdoptionSweep`, Submit=true): completed-ledger gate →
   probe merge → ③a clean commit + `branch -d`/`-r -d`; ③b conflict
   dispatches a merge-conflict task (Bond session) — red line ④ kept:
   Go discovers/tries/dispatches, the agent judges semantics

Both: fail-soft per step (`GitPassStep` verdicts), denyCurrentBranch on a
master push = normal skip; order git → db (deliverable{branch} lands first).

## Trigger
- serve tick: `serveGitSyncSweep()` pass step, `GOPI_SYNC_GIT_INTERVAL`
  (seconds, default 900, ≤0/junk = off), throttled like the adoption sweep;
  when the M4.13 delta auto-sync (GOPI_SYNC_INTERVAL) is off, it also runs
  `runTaskSyncTick()` so db sync still rides the same pass
- manual: `gopi task push` (M4.18 — the same atom, no separate code path)

## E2E (real HP↔peerbox, 2026-09-01)
- ① HP `task push`: 5 branches pushed to peerbox refs
  (feat/branch-adoption-chain feat/merge-backlog feat/node-role-banner
  feat/sync-git-channel fix/memsync-import-atomic) — verified via
  `ssh peerbox git for-each-ref`; peerbox's stale feat/claim-broadcast ref
  correctly refuses a non-FF push (reported in push_failed, pass exits 0)
- ② peerbox `task push` (server role): fetch + adoption sweep ran clean;
  ③ master alignment: peerbox master a586ab1 = the integration point; HP
  master (5c6f5ca, ahead) needs a peerbox-side merge first — see divergence note
- ④ re-tick idempotency: `task push` re-run pushes the same branch list
  as no-op successes (git up-to-date), zero failures

### Live divergence note (2026-09-01)
HP master 5c6f5ca (deepseek refactor + task-id-collision merge) is NOT an
ancestor of peerbox master a586ab1, and a586ab1 ⊄ 5c6f5ca: the two masters
diverged BEFORE this epic. ff-only correctly refuses (the pass reports
"skipped (diverged)" — by design; the client must not merge). Resolution
path: peerbox merges HP's master (server integrates), then HP's next pass
fast-forwards. NOT done automatically here: the divergence predates this
feature and merging 30+ commits of two masters is a human/PA decision.

Also observed: `git merge --ff-only` inside HP's checkout (83ed846, a
feature-branch checkout) refused against peerbox/master because HEAD ≠
master — the pass runs `pull --ff-only` which follows the checked-out
branch; a feature-branch checkout is the normal client state, so a
diverged/feature-checkout master is a *skip*, not an error.

## Tests
- internal/tools TestGitSync{ClientPassPushAndPull,ClientPassNoPeerSkips,
  ClientPassDivergedMasterRefused,PushLocalBranchesExcludesMaster} — real
  bare+clone fixtures (mocks cannot prove up-to-date idempotency or ff-only
  refusal semantics)
- cmd/agent TestServeGitSync{Interval,SweepThrottles}, TestRunTaskPushGit*
  (positional refusal), TestParseTasksArgsPushAcceptsBare

## E2E addendum: self-peer bug + full-alignment run (2026-09-01, live)

**Bug found in live E2E (fixed in 2733642)**: HP is a client but its peer
list is [hp, peerbox] — the target was peers[0] = "hp" = ITSELF, so every
push failed with 无法读取远程仓库 (ssh to self with no key). strace showed
git push hp:/home/user/work/GoPi. Fix: GitSyncServerTarget now skips the peer equal
to the node identity (GOPI_SYNC_NODE > os.Hostname, the syncSelfTag rule),
re-verified: 0 self-push failures, 13 branches pushed in one pass.

**Node identity bootstrap on HP**: HPs hostname is hostb-Laptop, which


## E2E addendum: self-peer bug + full-alignment run (2026-09-01, live)

**Bug found in live E2E (fixed in 2733642)**: HP is a client but its peer
list is [hp, peerbox] — the target was peers[0] = hp = ITSELF, so every push
failed (ssh to self with no key). strace showed git push hp:/home/user/work/GoPi.
Fix: GitSyncServerTarget now skips the peer equal to the node identity
(GOPI_SYNC_NODE > os.Hostname, the syncSelfTag rule); re-verified: 0
self-push failures, 13 branches pushed in one pass.

**Node identity bootstrap on HP**: HP hostname is hostb-Laptop, which
does NOT match its peer alias hp in the topology — role detection, master
alignment runs and the adoption sweep need the alias. Fixed via
/home/user/.config/environment.d/gopi-node.conf + /home/user/.profile exporting
GOPI_SYNC_NODE=hp (session-scoped; new shells pick it up). General
deployment footgun: every node should export GOPI_SYNC_NODE=<its alias
in the peers list>.

**3. master alignment**: HP and peerbox masters converged at 7c497b3
(HP merge 5f70dbd of peerbox 78fe5fa; peerbox merge 7c497b3 of HP 4bcacbb;
ff-only sync both ways). git rev-parse HEAD identical on both nodes.

**2. peerbox pass on the real server**: fetch hp + adoption sweep — clean
merges landed (node-role-identity, testsuite-coverage, m47-e2e-hp-check,
ftsync-rebuild-rescue, serve-tick-git-sync, branch-backlog,
sync-git-channel-hpfix); conflicted branches aborted and archived per
Rule 9 (archive/feat-merge-backlog-20260901,
archive/fix-memsync-import-atomic-20260901,
archive/feat-claim-broadcast-20260901). Branch graveyard on peerbox: 14
heads down to 1 (claim-broadcast, worktree-held, archived).

**4. repeat-tick idempotency**: consecutive task push runs on both nodes
are no-ops on converged state (push list = up-to-date no-ops, adopt
scanned=N adopted=0); the only recurring report is the legitimately
diverged claim-broadcast non-FF — correct per contract.

**Deployment note**: peerbox installed /usr/local/bin/gopi predates this
feature — task push there required a fresh build from /home/user/work/GoPi. PA
should rebuild the installed binary after merging.
