<!-- doc-class: historical -->
# The worker delivery chain's three breaks — two lies and one silence

**Task:** `01a16fe2-ba37-7bb5-a3d4-39954efaefb2` (bug / delivery-chain / worker)
**Measured:** 2026-10-25 on hosta, against the live ledger and the clone
grounds under `/home/user/workspace`.

The briefing named three independent defects stacked on one run
(`01a16f9d-88f3-7b53-99a3-130dbb12aa9a`): the ledger reported a clean run as
killed, its commit never reached peerbox, and the repair path could not see the
row. Each is a separate leg below, with its own commit and its own red proof.
**What they have in common is a rule, not a module**: a claim must measure a
state that is already true. Leg A asserted a cause it never read; Leg B made a
claim before the act that made the claim true; Leg C recorded a refusal in a
place the repair path does not look.

---

## Leg A — the dead note read no evidence (`ff2b919`)

**The lie, verbatim from `gopi task status`:**

```
pid 190566 is gone — the run ended (killed/crashed) and the row was never closed
```

**The run's own record, from the very session directory the status line printed:**

```json
{"exit_code": 0, "reason": "complete", "elapsed_sec": 565.4,
 "total_iterations": 68, "total_tokens": 5653090,
 "timestamp": "2026-10-25T05:35:38.094117815+08:00", "type": "session_end"}
```

`exit 0` / `reason: complete` is a clean ending. The old note decided `dead`
from pid liveness alone, so "the process finished and exited" and "the process
was killed" were the same sentence — and it picked the accusing one.

**The fix** is `internal/tools/taskproc_runend.go`: the dead verdict now reads
the run's OWN `session_end` record (exit code + reason) from the same log the
status line already points at. Three arms, each stating only what was measured:

| read | what the note may say |
|---|---|
| `exit_code == 0` and `reason ∈ {complete, quit, eof, interrupt}` | the run ended cleanly — the row was never closed |
| a non-zero exit, or a reason outside that set | it ended `<reason>` (exit `<code>`) |
| no `session_end`, or a log without a reason key | the run ended; the log does not record how |

The third arm is deliberately weak rather than dramatic: an older log has no
`reason` key, and "we cannot tell" is the honest answer to that — a note that
guessed `crashed` there would be the same defect in a quieter voice.

**Red proof:** revert the wording to the old single sentence →
`TestTaskStatusDeadReadsTheRowsOwnSession` fails on all three arms.

## Leg B — the claim ran before the act (`0754f0d`)

The most consequential of the three, and its shape is order, not code.

`runTaskRunOnce` (`cmd/agent/cli_tasks.go`) closed out in this sequence:

1. `writebackOutcome(… TaskStatusCompleted …)` — **declare delivered**;
2. `tools.deliveryProofRefusal` measures that claim: is the tip this run
   produced contained in the ancestry or the patches of
   `refs/remotes/origin/<branch>`? **That ref moves only when the push lands.**
3. The proof therefore refuses — "the work exists only in the run's own ground;
   deliver it … and write back again, or leave the row OPEN";
4. `taskRunCloseoutLanded` sees a non-terminal row and returns;
5. the delivery leg — `cloneRun.finish()`, the thing that pushes — sits BELOW
   all of this and **never runs at all**.

**Measured proof that step 5 never executes** (the leg prints one line per
attempt, so its absence is a count, not an impression):

```
$ grep -c 'delivery attempt' ~/.gopi/sessions/01a16f9d-88f3-7b53-99a3-130dbb12aa9a/output.log
0
$ gopi task show 01a16f9d-… | grep '^result:'
result: … the tip 52db940ee320 (the ground's HEAD) is in NEITHER the ancestry
        NOR the patches of refs/remotes/origin/master … so the work exists only
        in the run's own ground. Deliver it … and write back again, or leave
        the row OPEN
```

A gate that names its own remedy ("deliver it … and write back again") while
the code puts delivering AFTER it can never be satisfied. The row stays open,
its commits live in a ground nobody revisits, and the ledger reads like a run
still in flight — while a second, unrelated face of the same fault prints the
delivery's own success line next to the refusal.

**The fix: deliver first, claim second** — the same answer `99ad8a2` gave the
bare-prompt face. The leg moves above the writeback and its verdict decides the
claim: `delivered` lets the proof that follows see the tip AT its destination
and the row lands `completed`; `BLOCKED`/`BROKEN` writes no `completed` at all —
the row keeps the leg's own sentence as its reason (Rule 15: a run that failed
to deliver does not get to say "completed").

**One call site, deliberately.** `cloneRun.finish` is not idempotent by itself;
"at most one delivery attempt per run" is a property of the CALL SITES. The old
site is replaced by a comment saying why it is not coming back, because a second
copy is a second attempt.

**Red proof:** move the block back below the claim →
`TestWorkerCloneDeliveryRunsBeforeTheCompletedClaim` fails, and its output
reproduces the defect word for word.

## Leg C — the refusal was recorded where nothing looks (`2c8570e`)

The proof's refusal is a sentence in the row's `result` column and a notify
line. The repair path (`gopi task retry-delivery`, and the resident pass behind
it) scans rows whose newest trail event is `delivery-blocked` — and a refusal
wrote no such event. So the row was refused *and* invisible to the one verb
built to repair it:

```
$ gopi task retry-delivery 01a16f9d-88f3-7b53-99a3-130dbb12aa9a
retry-delivery: no row owes a delivery (no row's newest event is delivery-blocked)
```

**The fix**, `internal/tools/task_delivery_owed_refusal.go`, has two halves:

- the refusal arm records `delivery-blocked` **in the same breath** as the
  refusal (the event key comes from
  `internal/tools/task_delivery_retry.go`'s own constant — Rule 14, one
  spelling of the value, never a second literal);
- `deliveryRetryOpenOwedReason` teaches the retry scan that an **OPEN** row
  owing a delivery *is* a candidate. Before it, `taskTerminalStatus` answered
  "not a candidate" at the first guard, so the event alone would have changed
  nothing — the refusal would have been recorded, queued, and still unread.

**Red proofs:** removing the one line that queues the refusal →
`TestDeliveryRefusalPutsTheRowOnTheQueue` reports
`newest event="delivery-proof-owed"` and the scan does not yield the row. The
control arm `TestDeliveryRefusalQueueDoesNotClaimAnUnrelatedOpenRow` keeps an
ordinary OPEN row (no delivery owed) out of the queue — a repair path that
claimed every open row would be a new defect wearing this one's clothes.

---

## What was NOT done, on purpose

- **No new tool, no resident process, no new config key** (Rule 3 / Rule 35).
  Every leg reuses machinery that already ran: the status reader, the delivery
  leg and its rounds, the retry scan and its event vocabulary.
- **The proof arm was not weakened, bypassed, or taught to be order-aware.** It
  is a correct criterion that must run after the fact it measures. Same for
  `reclaimRunTrackedDirtRefusal`, `reclaimRunResidueRefusal`,
  `deliverableTipRefusal`: untouched.
- **The non-clone path is byte-identical.** An empty `CloneRunGround()` returns
  before reading config, git or the log, so the worktree channel, the chat faces
  and `task run` children take exactly the path they took before.

## How it is checked

```bash
go test ./internal/tools/ -run 'TestTaskRunEnd|TestTaskStatusDead|TestDeliveryRefusal'
go test ./cmd/agent/    -run 'TestWorkerCloneDeliveryRunsBeforeTheCompletedClaim'
```

The Leg B arm is an end-to-end test on the real binary with ONE variable — the
declared build, `true` vs `false`: a `true` build must push (the source repo's
master moves), land `completed`, and print no `delivery proof: REFUSED`; a
`false` build moves nothing and leaves the row OPEN.

## The rule this episode leaves behind

**A claim must measure a state that is already true.** Leg A claimed a cause
from a signal that does not carry it (pid liveness). Leg B claimed an outcome
before the act that produces it. Leg C measured nothing wrong — it recorded the
right fact in a place no reader was pointed at. In all three, the code and the
words disagreed, and the words were the only thing an operator saw.
