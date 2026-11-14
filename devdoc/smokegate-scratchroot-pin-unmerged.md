<!-- doc-class: historical -->
# A completed row's fix that never crossed the wire

**Measured 2026-10-12 (HP / hostb-Laptop).** Row `01a12d3f-be40-7604-97bd-3ab7394cd78a`
read `completed`, its commit `3771ebc93` existed, its report existed — and the
Rule 22 gate it claimed to turn green was **still red on peer master**, because
the ref carrying the fix lived and died in one host's own ref store.

This document records the shape, because the shape is what recurs. It is not
about `internal/smokegate`.

## What was measured

The fix commit was reachable from exactly one ref in the whole repository:

```
$ git for-each-ref --contains 3771ebc93 --format='%(refname)'
refs/heads/task/01a12d3fbe40760497bd3ab7394cd78a
$ git merge-base --is-ancestor 3771ebc93 peerbox/master ; echo exit=$?
exit=1
$ git ls-remote --heads peerbox 'task/01a12d3f*'      # empty — the peer never saw it
$ ssh peerbox 'cd /home/user/work/GoPi && git rev-parse --verify --quiet \
      refs/heads/task/01a12d3fbe40760497bd3ab7394cd78a; echo exit=$?'
exit=1
$ ssh peerbox 'cd /home/user/work/GoPi && gopi task list | grep -c 01a12d3f'
0
```

The row's own event trail carries no delivery event at all — `in_progress` →
`reframe` → `completed`, and nothing else (`task_events`, read read-only; the
`sqlite3` CLI is not installed on this host, so the query ran through python3's
`sqlite3` module against `file:~/.gopi/memory.db?mode=ro`).

## Why: two mechanisms, both measured

**① This node does not exchange rows, and it is the ref that needed moving.**
The run's own banner says so:

```
[gopi] node: hostb-Laptop (client→peerbox)
[gopi] task-sync: off (declared in task_sync: false — automatic task-row exchange disabled)
```

`task_sync: false` gates the auto row exchange (claim broadcast / terminal push /
serve-tick delta pass / submit forward). It does **not** gate the branch
hand-over — `internal/config/config.go:413` says exactly that: *"a client node
with task_sync=false still hands its branch back at finish-cleanup"*. So the
branch's crossing the wire rested entirely on finish-cleanup, which is the
per-task push site (`task_finish_cleanup.go`, chain step ② and
`gitPushbackPlan`).

**② finish-cleanup refused — and refuses today, for the same row, on the same
input.** The refusal is about the MAIN checkout the run was launched from, not
about its own worktree:

```
$ gopi task finish-cleanup --dry-run --task 01a12d3f-be40-7604-97bd-3ab7394cd78a
  01a12d3f-… completed   close-out refused: MAIN repo dirty (the checkout the run
  was launched from, not this run's worktree) — /home/user/work/GoPi: worktree
  dirty (live bond protected) — tracked changes: M  CONFIG.md — inspect it by
  hand; this close-out refuses rather than cleaning (no git clean / checkout -- /
  stash / commit is run here)
finish-cleanup (dry-run): candidates=1 would-run=0 settled=0 refused=1
```

`git -C /home/user/work/GoPi status --porcelain` prints 50 entries, all staged,
and `CONFIG.md`'s mtime is `2026-10-12 07:08:51` — minutes before that run
finished. The refusal is **by design** (`finishCleanupMainRepoDirtyPrefix`,
`internal/tools/task_finish_cleanup.go:2059`, introduced by `4c6cbcd6e`
2026-09-23 "feat(closeout): read the MAIN repo too — clean + HEAD on the
integration branch"): the chain will not clean another party's checkout, so the
push is withheld rather than run from a workspace whose state it does not own.

**The reconstruction, labelled as one.** That the 07:31 close-out hit *this*
sentence is inference: measured are (a) the same verb refuses today for this
row, (b) the main checkout was dirty then, (c) the row's trail has no delivery
event. There is no log line from that pass — the agent-face `output.log` is the
child's, not the wrapper's.

## It is not one row's accident

Across every local `task/*` branch at the time of measurement:

```
ADOPTED-INTO-MASTER peer-ref=0  task/01a12d470b6c78e2ab63247c5be1d25d
ADOPTED-INTO-MASTER peer-ref=0  task/01a12d4735be72249e25c96ab86b22d9
ABSENT-from-master  peer-ref=0  task/01a12d22dac07bdea9f895926cdc64ea
ABSENT-from-master  peer-ref=0  task/01a12d2ca3a67c5db2a1fa03081027d5
ABSENT-from-master  peer-ref=0  task/01a12d2f2b3f7e66862e144a4ef6b9a2
ABSENT-from-master  peer-ref=0  task/01a12d30799b775b8c97f691e282d92c
ABSENT-from-master  peer-ref=0  task/01a12d32819d7b6284643e956ba09d63
ABSENT-from-master  peer-ref=0  task/01a12d3e81c17028bddb24143aa4a338
ABSENT-from-master  peer-ref=0  task/01a12d3fbe40760497bd3ab7394cd78a
ABSENT-from-master  peer-ref=0  task/01a12d4cac777cc7b99cbc5078d74c27
```

The two that are contained in peer master are the two **the peer itself merged**
(both sitting at the peer-master tip `3db654fb7` at that instant) — and neither
has its own ref on the peer, which is expected: the peer adopts a branch into
master and deletes the ref. The other eight are HP-origin work that has no ref
on the peer at all.

**The reading**: the adoption line (peer merges HP work) is moving — while the
hand-over line (HP pushes a finished branch to the peer) is the one that fails
silently when the main checkout happens to be dirty at close-out time. A run
therefore cannot tell from its own ledger whether its deliverable crossed: the
row reads `completed` either way.

## What this run did about it

Landed the pin again (`internal/smokegate/testmain_scratchroot_test.go`,
byte-identical to the `3771ebc93` version) on a branch that **contains every
peer-master commit** (`git rev-list --count HEAD..peerbox/master` → `0` at the
delivery-point sync), proved the gate red→green on the real gate, and filed the
systemic half as a bug row rather than patching the system layer from a client
(Rule 28).

## What a future reader should take from it

- **A `completed` row is not evidence that its branch crossed the wire.** For a
  ref, read the ref: `git ls-remote --heads <peer> <branch>`, or the peer's own
  `rev-parse`. `git merge-base --is-ancestor <commit> <peer>/master` decides
  adoption; ancestry and content are two different questions.
- **`git rev-list refs/remotes/<peer>` never answers this.** Remote-tracking refs
  are pruned to what the peer's own heads point at, so a fix commit that lives
  only on a local branch is unreachable from every one of them
  (`git rev-list refs/remotes/peerbox | grep -c 3771ebc93` → `0`).
- **The dirty-MAIN-checkout refusal is a delivery refusal, not a housekeeping
  one**, and its blast radius is the whole deliverable: no push, no removal.
  Neither clean nor commit the checkout on another run's behalf — the refusal is
  the guard working.
- **The `sqlite3` binary is absent on this host.** Read `~/.gopi/memory.db`
  read-only with python3's `sqlite3` module (`file:…?mode=ro`) instead.
