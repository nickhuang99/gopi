<!-- doc-class: historical -->
# A peer name this machine cannot resolve — the diagnosis that was missing

| field | value |
|---|---|
| topic | finish-cleanup / pushback: `sync_peers` alias with no matching git remote |
| note | classified `historical` by `tools/docclass/classify.py` (the manifest is its authority); the finding below is the part with live force |

## The defect

gopi's git legs spell their target `<alias>:<path>` — the alias half is the
DECLARED topology (`settings.json` `sync_peers[0]`, an ssh host), composed by
`finishCleanupServerTarget()` / `GitSyncServerTarget()`. **Nothing requires that
host to also be a configured git REMOTE in this repo**, and when it is not, git's
diagnostic names neither the declared peer nor the remotes that DO exist.

MEASURED 2026-10-12 on hosta: `sync_peers: ["peerbox"]` while `git remote -v`
lists only `origin`. The only existing diagnosis in the lane
(`pushbackBlockedPushURLLineFmt`, measured 2026-10-08 on HP) covers the OTHER
world — a configured remote whose own `remote.<name>.pushurl` is
`DISABLED-no-push`. Its gate is `gitPushbackRemotePushURL`, which returns
`("", false)` for a name that is not a configured remote at all, so the
name-mismatch half had no refusal at all.

## The fix (one definition, many call sites — Rule 14)

- `gitPushbackTargetFailureDetail` — **the ONE entry point**. It decorates a
  target-git failure with the measurable cause and returns anything it cannot
  measure UNCHANGED.
- `gitPushbackGitRemoteNames` / `gitPushbackRemoteNameList` — the measurement
  (`git remote`), rendering `pushbackRemoteNameNone` when the repo has no remote
  at all (a blank would be indistinguishable from a measurement that never ran —
  Rule 15).
- `gitPushbackTargetAliasNotRemote` — the probe: does the target's alias half
  appear among this repo's remotes?
- `gitPushbackAliasNotRemoteLine` — the finding, citing `config.SyncPeersSettingsKey`
  by its constant.

Wired into **three** failure paths that previously each printed git's bare
sentence: `gitPushbackRefusal` (this lane's push), `gitPushbackPlan`'s
peer-master FETCH, and the finish-cleanup chain's own push
(`task_finish_cleanup.go`).

## The correction the red proof paid for

The first implementation gated the new arm on git's phrase
`does not appear to be a git repository` (`pbUnresolvableRepoPhrase`) — the
phrase the pushurl arm already keyed on. **That gate covered the wrong path.**
The red proof's own output showed why: the peer-master FETCH runs BEFORE any
push, and over ssh it fails with

```
ssh: Could not resolve hostname ghost: Name or service not known
fatal: Could not read from remote repository.
```

— a DIFFERENT sentence, containing neither git's phrase nor the alias. So the
phrase-gated version left the live path (the fetch) exactly as silent as before
while its unit-level test passed. The arm is now gated on the **MEASUREMENT**
(the alias is not among this repo's remotes), and git's sentence is carried
along unchanged beneath the finding. This is the shape to remember: **one
unreachable peer produces different sentences depending on the verb that failed
— gate on the measurement, never on the wording.**

## What is NOT in this change

- The settled classifier (`FinishCleanupReportSettled`) still reads a
  gone-worktree row as settled **unconditionally** — `finishCleanupDeliveryEvidenced`
  does not exist on this branch. That half belongs to the in-flight sibling run
  `01a12de4-3bbf` (branch tip `d396ad04f`, +222 lines there). Not duplicated
  here, by design (Rule 29/30).
- Nothing rewrites `sync_peers`, creates a remote, force-pushes or deletes a
  branch (Rule 35 — no new resident machinery either; the diagnosis hangs on
  paths that already run).

## Measured, on real fixtures (Rule 31)

- **B:** private profile, `GOPI_SYNC_PEERS=ghost`, repo whose only remote is
  `origin` → `gopi task push` printed the finding with the remote list and the
  remedy, instead of git's bare sentence.
- **A-push:** a completed row whose RECORDED worktree directory was gone and
  whose branch had never been pushed → `gopi task finish-cleanup --dry-run` said
  `settled=1` (the defect, still live), and `--apply` **pushed anyway**:
  the peer gained `refs/heads/task/demo-a` at the client's tip. This confirms the
  2026-09-21 fix (`1732dc811`) is on master and load-bearing.
