<!-- doc-class: normative -->
# The delivery leg runs BEFORE the row claims delivery

One order rule, and the measurement that forced it.

## The rule

A run that took the clone channel (`internal/agent/cloneground.go`) delivers from
the END hook (`finishSessionTaskRecorded`, `internal/agent/taskregister.go`):

1. **deliver** — `finishCloneDelivery` runs `devworkspace.AttemptDelivery` ONCE;
2. **then claim** — only a `delivered` verdict lets the hook write the success
   terminal (`completed`); a `blocked`/`broken` leg lands the row **`failed`**
   with its own sentence in the reason column (see
   "What 'delivery blocked' looks like on the row" — OPEN was the old answer and
   nothing ever re-drove it);
3. the DEFER form of the leg stays as the fallback for the hook's early returns
   (no registered row / a failed ledger close), and one attempt with one answer
   is guaranteed by `cloneDeliveryDone` + `cloneDeliveryVerdict` (`loop.go`), not
   by the number of call sites.

## Why the order is not a style choice

The write choke point measures a claim it did not make:
`tools.deliveryProofRefusal` (`internal/tools/task_delivery_proof.go`) refuses a
`completed`/`verified` write when the tip the run produced is contained in
neither the ancestry nor the patches of `refs/remotes/origin/<branch>` — and that
ref moves **only when the push lands**. So the proof's answer depends on whether
the delivery already happened:

| order | what the proof measures | outcome |
|---|---|---|
| claim → deliver | a destination the push has not moved yet | REFUSED, row stuck `in_progress` |
| **deliver → claim** | a destination the push just moved | allowed, row `completed` |

Measured on the shipped binary at the epic's base (2026-10-24), both sentences on
ONE stderr, in this order:

```
[task-notify] delivery proof: REFUSED <row> — … the tip … is in NEITHER the
              ancestry NOR the patches of refs/remotes/origin/master …
clone channel: delivery delivered — pushed master to origin after 1 attempt(s)
```

The source repo's `master` had moved. The delivery had succeeded; the row was
refused anyway. Nothing was wrong with either duty — they were in the wrong
order.

## What was deliberately NOT done

- **The proof arm was not weakened, bypassed or made order-aware.** It is a
  correct criterion; it simply has to run after the fact it measures. Same for
  `reclaimRunTrackedDirtRefusal`, `reclaimRunResidueRefusal` and
  `deliverableTipRefusal` — untouched.
- **No new tool** (Rule 3), no resident process, no new declaration: the leg, its
  injection field and its three outcome shapes already existed.
- **The non-clone path is byte-identical.** `CloneRunGround()` empty → the leg
  returns before reading config, git or the log, so the worktree channel, the
  chat faces and `task run` children take exactly the path they took before
  (`TestRawPromptRunReleasesItsWorktree` unchanged).

## What "delivery blocked" looks like on the row

The row lands **`failed`**, the reason column carries
`clone channel: delivery did not land — <AttemptDelivery's own words>`, the trail
gets `clone-delivery-not-landed`, and stderr says

```
[gopi] task ledger: <row> LEFT OPEN — the delivery did not land (no `completed`
       claim is written), so the row lands `failed`; recover with `gopi session
       resume <row>` or reissue it: clone channel: delivery did not land — …
```

**`completed` would be a lie, and so — measured later — was OPEN** (bug
`01a19639`, 2026-11-01). The paragraph this replaces argued that `failed` "would
also be a lie (the run did its work)" and that OPEN was "the only honest state".
The premise was that something downstream would re-drive the delivery. MEASURED:
nothing does. Both carriers were retired on 2026-10-30 — the lifecycle pass
(`cmd/agent/task_lifecycle_tick.go`: nothing starts it) and the `retry-delivery`
verb (`taskverbs.RemovedVerbs`) — and `deliveryRetryOwedIDs`, the scan that would
have selected exactly these rows, carries its own note:

```
internal/tools/task_delivery_retry.go:962:// is `delivery-blocked`. No production caller today (see deliveryRetryOwedIDs).
```

So "OPEN" was not a state, it was a **parking space**, and the framework said so
on every read of a real row (`01a19613-9019-71bc-80bf-0f450ec97ce5`, 15:47:31 →
16:52:46, until an operator typed `--status failed` by hand):

```
status:      in_progress
verdict:     dead
evidence:    pid 1453624 is gone — the run recorded a clean end (exit 0, reason
             complete); the WORK may have finished and the row was never closed
resumable:   no
```

`dead` + `resumable: no` + `in_progress` is the one combination no actor can
handle: the dispatcher reads it running, `--resume` refuses it, and the reissue
census lists `failed` rows only.

**`failed` is not the lie the old text feared.** It is the ledger's existing word
for "this row is not a delivered result", and it is the status every other
mechanism already agrees with: it is not a delivery CLAIM, so no proof arm
disputes it (`taskStatusClaimsDelivery` excludes it); it IS resumable
(`taskRunResumeStatusAdmits`), so the recovery route OPEN was supposed to
preserve actually opens; and it lands in the reissue census the operator already
reads. The words `LEFT OPEN` **stay in the line** because the claim genuinely is
still unwritten — they describe the CLAIM, which was always true, while the row
now lands a status a reader can act on.

The `return` that used to precede the ledger write is gone with it, and that
closes the second half of the same defect: the old arm returned BEFORE
`recordTaskRowClosed`, so these rows carried **no `task_row_closed` witness at
all** — measured `grep -c task_row_closed sessions/659b645b-…/session.jsonl` = 0.
"the hook decided not to close" and "the hook never reached the ledger" were
indistinguishable from the transcript, which is the one ambiguity the witness
exists to remove.

## How it is checked

- `go test ./internal/agent/ -run 'TestCloneDelivery|TestRealDeliveryFailuresLandTerminalFailed|TestEmptyGroundIsNothingToDeliverNotAnUndeliveredRun|TestAlreadyDeliveredGroundIsItsOwnCompletion'`
  — delivered → `completed`, blocked (six kinds: dirty, pull, build, push, man,
  disk-exhaustion) → `failed` with the non-delivery sentence, a `task_row_closed`
  witness, and no `nothing-to-deliver` sentence; an empty or already-delivered
  ground → `completed` (the two opposite contracts are unmoved);
- `go test ./cmd/agent/ -run 'TestRawPromptCloneDeliveryOrder'` — two e2e arms on
  the real built binary with ONE variable (the declared build: `true` vs
  `false`): delivered moves the source repo AND lands `completed` AND carries no
  `delivery proof: REFUSED`; blocked moves nothing and lands `failed`.
