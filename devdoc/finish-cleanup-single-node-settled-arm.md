<!-- doc-class: historical -->
# The terminal-state probe refusal on a host with no delivery channel: one criterion, asked in one place
# finish-cleanup on a single-node host: the probe refusal read `Settled=False WouldRun=False`

**Date:** 2026-10-12 · **Branch:** `task/01a12e4d21137f22bf3bdbd240c8ea55`

## The defect

A terminal row that RECORDED a worktree whose directory is gone, on a host that
declares **no sync peer**, printed two fields that disagree about one row:

```
gopi task finish-cleanup --dry-run --json --task <row>
  Settled=False WouldRun=False
  Verdict=worktree unreadable: git rev-parse --verify HEAD:
          chdir <dir>: no such file or directory: chdir <dir>: no such file or directory
```

`Settled=False` says "not finished"; `WouldRun=False` says "no act will run".
A row in that state can never be closed by anybody: nothing is owed, nothing
runs, and the sweep counts it as a refusal forever.

## Root cause (5-why)

1. Why `WouldRun=False`? `FinishCleanupPlan` promotes a skipped row to
   would-run only when the reason carries the owed marker
   (`finishCleanupDeliveryOwedSkipMarker`) — this reason does not.
2. Why no owed marker? The marker is composed by the gone-directory REMOVAL
   composer (`finishCleanupGoneWorktreeDeliveryOwedReason`), and this row never
   reaches it.
3. Why does it never reach the removal composer? The chain stops one rung
   EARLIER, at its own terminal-state probe — `git rev-parse --verify HEAD`
   inside the recorded worktree (`finishCleanupServerRecycle`). With the
   directory gone, exec fails and the probe writes its own reason.
4. Why does `Settled=False` then not catch it? `FinishCleanupReportSettled`
   classifies the gone-directory state by READING the composed reason: the
   `single-node: sync_peers empty …` note (the NO-DELIVERY-CHANNEL arm) or the
   owed marker. The probe's reason carries neither — the probe has no delivery
   reasoning and no peer lookup — so the row fell through to the
   delivery-evidencing branch and lost.
5. Why is that a defect and not a missing feature? Because the FACT the
   classifier needed was already available and already read elsewhere on the
   same host: `config.HasSyncPeers()`. The classifier asked for that fact only
   in a spelling a different composer happens to write — an inference over
   WORDING where a measurement was one call away.

## The fix (minimal, one criterion one reader)

`internal/tools/task_finish_cleanup.go`:

1. `finishCleanupNoDeclaredPeer()` — the ONE reader, in this package, of "this
   host declares no sync peer" (`!config.HasSyncPeers()`). The two composers
   that already decided that fact (`noServerTargetSkipReason`,
   `finishCleanupGoneWorktreeNoPeersNote`) now ask HERE, and so does the
   settled classifier (Rule 14: which host has a delivery channel cannot be
   answered two ways).
2. `finishCleanupUnreadableWorktreeMarker` (`"worktree unreadable"`, the probe's
   own head, previously a bare literal at the probe site) plus
   `finishCleanupUnreadableGoneWorktreeReason(reason, dir)` — the probe refusal
   for THIS row's directory, where "gone" is read from the ONE existing reading
   (`finishCleanupMissingDirReason`'s chdir marker pair), never a second `stat`.
3. In `FinishCleanupReportSettled`'s gone-directory arm, next to the existing
   NO-DELIVERY-CHANNEL return: when the host declares no peer AND the refusal is
   that probe shape → settled.

**What was deliberately NOT relaxed.** The 2026-10-12 fix from this epic's
previous round (`a gone directory is not delivery evidence`) is untouched: the
new arm fires only when `finishCleanupNoDeclaredPeer()`. A host WITH a peer
keeps the old verdict for every one of these shapes — its branch might still
owe the hand-off, and the settled reading would cancel the one act the chain
exists for. That control is a test arm, not a comment: see ARM B below.

**Why not compose the note into the probe's reason?** That was the first
attempt and it is the wrong shape: the probe has not asked whether the delivery
is owed, so writing a delivery sentence there would put a claim about delivery
next to no delivery reasoning at all — the exact mistake the previous round
fixed. The condition belongs where it is DECIDED (the classifier), and it is
asked with the reading that already exists.

## Contract: one criterion, asked in one place (invariant)

The criterion "does this host have a delivery channel?" is a **single source of
truth** inside `internal/tools`: `finishCleanupNoDeclaredPeer()`. The build must
keep it that way, and the following statements MUST hold for the reading this
doc fixes:

1. A gone-worktree row reads settled **iff** no peer is declared. Any future
   caller that needs this fact asks that one function — a second reader (a
   re-inlined `config.HasSyncPeers()`, a fresh peer lookup) is a defect, because
   it lets two answers exist for one question.
2. The gone-directory state is read by ONE predicate pair
   (`finishCleanupMissingDirReason` + `finishCleanupUnreadableGoneWorktreeReason`),
   never by a second `stat` of the path — the criterion is the EVIDENCE, not a
   probe that can disagree with it.
3. Nothing else is relaxed: with a peer declared, every gone-worktree shape
   keeps its pre-fix disposition (`Settled=False`), enforced by the control arm
   of the regression test.

## Verification

- Regression test: `internal/tools/task_finish_cleanup_single_node_settled_test.go`
  — `TestFinishCleanupGoneWorktreeProbeRefusalSingleNodeIsSettled`. It drives the
  REAL chain body (`finishCleanupServerRecycle`, the body a single-node host
  reaches through `taskFinishCleanup`) so the asserted reason string is the one
  the binary prints, and it asserts the fixture's own premise (the marker, the
  vanished path) rather than trusting it.
- **red**: with the new 6-line arm removed, the test fails with the production
  reason string in the message (`evidence/01-red-behavior.log`).
- **green**: with the arm in place, PASS.
- e2e (`evidence/02-e2e.log`, real `bin/gopi`, the briefing's fixture rebuilt):
  CONTROL 2d `False/False → True/False`; CONTROL 2a stays `True/False`;
  CONTROL 2c stays `False/True`; ARM 1 (stranded WITH a peer) stays
  `False/True`.
