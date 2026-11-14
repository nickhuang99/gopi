<!-- doc-class: historical -->
# The declared delivery destination (`sync_to`) now has a delivery reader

Task: `01a14b7c-b5a7-7802-a4f9-b7558e8d060e` · branch `task/01a14b7c-b5a7-7802-a4f9b7558e8d060e`
Base: `master` at the branch's creation point.

## The defect, stated exactly

On a node declared `role: client`, the close-out (`gopi task finish-cleanup`,
`internal/tools/task_finish_cleanup.go`) is supposed to **push the row's branch to
the peer the operator named for delivery, then force-delete the local ref**. The
delete half was already correct — `finishCleanupBranchForceDeleteFlag = "-D"`
(`task_finish_cleanup.go:544`), unlocked by two guards asked *before* the delete
(`finishCleanupBranchForceDeleteReason`: this chain's own push landed THIS ref;
`branchDeleteOrphanRefusalAt`: the tip survives in another ref, the delivery
point included). What was missing was the **delivery channel**.

Measured before the change (this branch, this host — `role: client`,
`sync_to: peerbox`, `sync_peers: ["peerbox","hp"]`):

- `internal/config/node_role.go:52` — `ResolveNodeRole` merged `GOPI_SYNC_TO` over
  settings `sync_to` and **memoised** it. Its only consumer was the startup
  banner (`cmd/agent/main.go:902`).
- `internal/tools/peer_remote_target.go:203` — `peerRemoteDeclaredAlias` answered
  the first usable **`sync_peers`** entry. That is the task-*ledger* whitelist: a
  client that exchanges rows with a second host lists it there, and the branch is
  then delivered to whichever alias happens to come first in that list.

So the key the operator declares for delivery had no delivery reader, and
`devdoc/hp-push-topology-audit.md` G8 recorded it as "decorative". The same is
still stated in `devdoc/requirements/04-sync-and-nodes.md:776-777` — that
sentence is now historical; this document is what supersedes it.

## The change (one function, one priority table)

`internal/tools/peer_remote_target.go`:

```
peerRemoteDeclaredAlias()            →  alias (the face every delivery leg uses)
peerRemoteDeclaredAliasWithReason()  →  (alias, diagnosis)      ← the same selection
```

| # | condition | answer |
|---|---|---|
| ① | `sync_to` declared **and** passes `ValidateSSHPeerAlias` | THAT alias (wins over a disagreeing `sync_peers`) |
| ② | `sync_to` empty | first usable `sync_peers` entry — the pre-existing behaviour, verbatim |
| ③ | `sync_to` declared but **unusable** | the `sync_peers` fallback **plus a named diagnosis** |
| ④ | neither usable | `""` — nobody to deliver to, the one meaning that answer ever had |

Why this shape and not a second resolver: Rule 14 — the alias choice lives in
exactly ONE place, and both faces are one implementation. `peerRemoteSyncTo()`
reads the destination through `config.ResolveNodeRole` (Rule 2: the env var is
read there and nowhere else); the tools layer never touches settings itself.

**The named diagnosis (Rule 15).** A declared destination that cannot be handed
to git/ssh is never silently substituted. The sentence carries four facts: the
declared value, the env var that overrides it, the validator's own refusal, and
the destination actually in force — and it says the declared one is *UNUSED*.
When no fallback is usable either, the sentence says there is **no** peer at all.

That condition is reachable in practice, not theoretical: an option-shaped alias
(`-oProxyCommand=…`) makes `ssh` run a local command, which is why
`ValidateSSHPeerAlias` refuses it — a mistyped or hostile `sync_to` must be
reported, not obeyed.

## What the delete half was NOT changed to do

`-D` remains **guarded**, and this change does not touch either guard. The chain
never force-deletes unconditionally:

- `finishCleanupBranchForceDeleteReason(finishCleanupStepSucceeded(rep, "push"))`
  — no successful push in THIS report, no delete (`fail-soft, report AND keep`);
- `branchDeleteOrphanRefusalAt(branchDir, branch, target)` — the tip must survive
  in some other ref, and the delivery point is consulted (`git ls-remote`).

The `-d` (merge check) half retired in 2026-10-14 (task `01a131bb`) for a
measured reason: ② has just pushed the ref to the delivery point, so the tip is
on the far side while this repo's HEAD does not contain it — `-d` refuses **by
construction**, paying a fixed cost per row (audit stream on HP: 393/393
`-d`+`-D` in lock-step, no `-d` ever standing alone). A client's branch is never
merged locally, so the merge check asks a question this shape can never answer
yes to.

## Evidence (what was run, and what flips)

The chain-level cases are `internal/tools/task_finish_cleanup_client_sync_to_test.go`:

| case | claim |
|---|---|
| `TestClientCloseoutPushesToSyncToThenForceDeletesTheBranch` | the DECLARED destination is dialled (relay-shim argv names it), the ref lands at the peer at the same tip, the local ref is GONE, and `git branch -d` is measured on the same tip and **refuses** ("not fully merged") — so the delete cannot have been authorised by the merge check |
| `TestClientCloseoutKeepsTheBranchWhenThePushDidNotLand` | the same row shape with a receiver that refuses: the push step is recorded **not OK**, and the branch **survives** — the guard, from the other side |
| `TestClientCloseoutRowDeliverableNeedsNoWorktree` | the row shape of this epic (no recorded worktree) still delivers and still collects the ref; the removal phase records *why* it had nothing to collect instead of vanishing |
| `TestPeerRemoteDeclaredAliasPrefersTheDeclaredDestination` (in `peer_alias_sync_to_priority_test.go`) | the priority table itself, case by case, plus the loud-override sentence and the fail-soft empty |

**RED proof (observed, not assumed).** With `peer_remote_target.go` restored to
`HEAD` and the two new test files in place, the chain case fails at its FIRST
assertion — the resolution — with:

```
the close-out must deliver to the declared sync_to ("gopi-leak-shim"),
got alias "client-cleanup-ledger-peer"
```

i.e. before the change the chain delivered to the *ledger* peer and never to the
declared destination. (The priority-table file does not compile against pre-change
source — it calls the new diagnosed face — so it is moved aside for that run; its
red direction is the same measurement stated as a compile-time absence.)

**Why the ssh is real.** `installLeakSSHShim` installs the repository's committed
relay (`internal/tools/testdata/ssh-sentinel-shim.sh`) as git's ssh: git really
spawns ssh, the shim really runs `git-receive-pack` against a sentinel bare repo,
and its argv log is the evidence of *which alias* was dialled. A test can
therefore never write a real node's repository (Rule 30), while still exercising
the push for real (Rule 31).

## Scope, stated honestly

- **Not changed: `GitSyncServerTarget()`** (`internal/tools/task_sync_git.go:1101`).
  The `gopi task sync-git` client pass still derives its peer from `sync_peers`
  and skips this host's own alias. That is the git-*ref sync* lane, not the
  branch-delivery lane, and it has a self-skip rule the alias selector does not
  have. Changing it is a separate decision with its own guard; it is left as it
  was rather than half-migrated.
- **The server arm is untouched.** `finishCleanupServerRecycle` runs where
  `finishCleanupClientArmApplies()` is false, and no delivery happens there by
  design.
- `CONFIG.md`'s `sync_to` row now names its real readers (the config resolver and
  the alias selector) instead of only the resolver.

## Reuse

If you are asked "which peer does a client deliver to?", the answer is one
function: `peerRemoteDeclaredAlias` — `sync_to` first, `sync_peers` as the
fallback, `""` when neither. Do not add a second reader; do not re-read the
settings key in the tools layer.
