<!-- doc-class: normative -->
# The writer of a terminal writeback — measured, and why no ownership predicate ships

**Epic 01a1651d.** Row `01a163df-a105-71b8-a297-680c5d35d093` reached `completed`
through its surviving **child** process while `claimed_by` named a wrapper pid
(`1190977`) that was already gone, and its three commits never left its clone
ground. The sibling row `01a16409` shipped the **output** gate — an undelivered
tip is refused at `taskUpdateStampedCore` (`internal/tools/task_delivery_proof.go`).

This document answers the remaining question with measurements: **does a
PROCESS-OWNERSHIP predicate add anything at that same choke point?** The answer
below is **no**, and the reason is not taste — the naive predicate refuses the
legitimate repair it was meant to protect.

---

## 1. Who wrote the terminal status (measured)

The run's own session log carries the identity of the process that executed it:

```
$ python3 -c "…first record of 429bf4a6-…jsonl…"
records: 1052
first type = session_start
first pid  = 1191807
last  type = session_end
```

The ledger's terminal write, verbatim (`task_events`, ordered by id):

```
8071 'in_progress'  at 1792677106.0 ::
8074 'reframe'      at 1792677133.0 :: verdict=DOABLE items=0 origin=429bf4a6-4a2a-4d7c-a080-5dd0ccb34978/1191807
8091 'completed-unclean-main-checkout-deposit' at 1792679579.0 :: run 01a163df-… deposited residue in …
8092 'completed'    at 1792679579.0 :: run closeout (child normal exit, wrapper writeback never landed) — elapsed=2461.3s iter=201 tokens=15397085
```

That digest format is minted in exactly one place, the child-side END hook:

```
$ grep -rn "child normal exit, wrapper writeback never landed" --include=*.go .
./internal/agent/taskregister.go:118:  autoTaskRunCloseoutStatsFormat = "run closeout (child normal exit, wrapper writeback never landed)" + autoTaskStatsFormat
./internal/tools/tasks.go:8940:        taskNotifyLog("task run closeout: %s → %s (child normal exit, wrapper writeback never landed)", …)
```

**The writer is the run's child (pid 1191807's process), not the claimed wrapper.**
The wrapper incarnation is gone, and it left no identity behind:

```
$ ls -d /proc/1190977
ls: cannot access '/proc/1190977': No such file or directory      (exit 2)
$ ls -d /proc/1191807
ls: cannot access '/proc/1191807': No such file or directory      (exit 2)
```

The wrapper's death is also recorded independently, by the signal provenance
sidecar of the same run:

```
{"ts":"2026-10-22T22:04:55+08:00","kind":"signal","signal":"SIGHUP","pid":1190977,"ppid":54349,"pgid":1190977,…}
{"ts":"2026-10-22T22:21:57+08:00","kind":"signal","signal":"SIGINT","pid":1190977,"ppid":54349,"pgid":1190977,…}
```

`pid 1190977` took a SIGHUP and then a SIGINT at 22:21:57; the row's terminal
write landed at 22:32:59 (`1792679579`) — eleven minutes after the wrapper was
signalled, which is why the writeback that survived is the child's fallback
rather than the wrapper's post-`Wait` write.

**Rule 24 caveat, stated as a limitation not a conclusion**: both pids are gone
now, so neither start-tick comparison is retakeable. The identity above is read
from two artifacts that recorded it while it existed (the session log's
`session_start.pid`, the signal sidecar's `pid`), not from a live probe. No
`pgrep -f` was used anywhere in this measurement.

## 2. What the write path could know about the writer at that moment

The choke point is `taskUpdateStampedCore` (`internal/tools/tasks.go:5640`).
It is reached from the incident's door by this chain:

```
internal/agent/taskregister.go:714  tools.TaskRunCloseoutStaleRun(…)
internal/tools/tasks.go:4552        TaskUpdateWithResult
internal/tools/tasks.go:4577        taskUpdate
internal/tools/tasks.go:5583        taskUpdateStamped
internal/tools/tasks.go:5640        taskUpdateStampedCore   ← the one writer
```

The predicate set it actually consults inside the success-claim gate, verbatim:

```
$ awk 'NR>=5640 && NR<=5900' internal/tools/tasks.go | grep -nE "Refusal\(|Record\(|taskTransitionAllowed|validTaskStatus"
 13:  if !validTaskStatus(status) { … invalidStatusRefusal … }
 36:  if !taskTransitionAllowed(cur.Status, status) { … }
 68:  if note, refused := reclaimRunTrackedDirtRefusal(taskID); refused {
 71:  if note, refused := reclaimRunResidueRefusal(taskID); refused {
 83:  if note, refused := deliverableTipRefusal(taskID); refused {
 94:  if note, refused := deliveryProofRefusal(taskID); refused {
105:  TaskMainCheckoutDepositRecord(taskID)          // records, never refuses
```

**Every one of those is a fact about the ROW or its GROUND. None is a fact about
the WRITER.** The single pid read in the whole core is on the `in_progress`
transition, and a terminal transition *clears* it:

```
5802:  identPid := os.Getpid()          // in the status == taskStatusInProgress arm only
…
	if stateChanged {                 // any non-in_progress transition:
		sets = append(sets, tasksInProgressAtColumnName+" = NULL",
			tasksProcPidColumnName+" = NULL",
			tasksProcStartColumnName+" = NULL", …)
```

So at the moment of a terminal write the row is — by design — **stripped of the
process incarnation**, and that is what the ledger shows for the incident row:

```
$ SELECT claimed_by, proc_pid, proc_start_ticks, in_progress_at, session_id
  FROM tasks WHERE task_id LIKE '01a163df%'
  host-b:1190977 | None | None | None | ''

$ SELECT count(*) FROM tasks WHERE proc_start_ticks IS NOT NULL
  7          -- of 1805 rows
```

Seven rows in five weeks' worth of ledger. The identity columns exist, are
correct, and are *empty whenever a close-out runs*, because
`taskUpdateStampedCore` itself nulls them on the way out.

What the writer *could* be identified by at that instant is therefore only:

| source | readable at the choke point? | measured |
|---|---|---|
| `claimed_by` stamp | yes (a column) | `host-b:1190977` — the **dead** wrapper |
| `GOPI_TASK_RUN_WRAPPER_PID` | yes (env of the writing process) | set in the incident's child — it is what `RunCloseoutOwnedBy` reads |
| `proc_pid` / `proc_start_ticks` | **no** — cleared by the same transition | NULL |
| holder liveness | yes, but it answers about the *claim*, not the writer | `pidAlive(1190977)` = false |

Which is exactly the asymmetry that makes an ownership rule hard: the choke
point can see **who claimed**, and it can see **who is writing only if the
writer is the same process** — the one case that needs no guard.

## 3. Evidence that the delivery arm already covers this harm

The arm is not adjacent to the incident's door — it is **in** it. Driving the
real path (`TaskRunCloseoutStaleRun`, the identical entry point the child's END
hook calls) on scratch ledger + scratch git state, with the incident's own shape
(a clone ground whose HEAD is commits ahead of its declared delivery point):

```
$ go test ./internal/tools/ -run TestWriterIdentity_DeliveryArmRefusesTheIncidentsOwnCloseoutDoor -count=1 -v
[task-notify] delivery proof: REFUSED tw-incident — run tw-incident claims `completed` … but its deliverable
  is not at the destination it declared: the tip b10b001a0a27 (the ground's HEAD) is in NEITHER the ancestry
  NOR the patches of refs/remotes/origin/master (…/ground), so the work exists only in the run's own ground.
--- PASS: TestWriterIdentity_DeliveryArmRefusesTheIncidentsOwnCloseoutDoor (0.08s)
```

The row stayed `in_progress` and the trail carries `delivery-proof-owed`. **The
incident's write shape is already refused, without knowing anything about the
writer.**

**Replayed against the destination the ground actually held at write time.** The
ground's tracking ref was not stale-but-lucky: it pointed at `882c645`, which
was the true delivery point at 22:32:59, and every one of the run's three
patches was absent from it:

```
$ git -C <ground> cherry 882c645 f26ab6f
+ 730027931c96f696ebf41103f0f1a950bfb2a13f
+ 5c91f49647a517881e4de21bebca27f652d69710
+ f26ab6f794cbdcd5e4c6eb2295308f6be8924fb2      ← all three ABSENT (no line marked '-')
$ git -C <ground> merge-base --is-ancestor f26ab6f 882c645 ; echo $?
1                                               ← not an ancestor either
```

Both arms of the criterion (`merge-base --is-ancestor` and `git cherry`) answer
"not delivered" on the state that existed when the write was made, so the arm
would have refused the incident's own writeback at its own timestamp — not only
in a reconstructed fixture.

And the arm is what is doing the work, not something else standing nearby — with
the arm neutralized (one line mutated out of the state gate, then restored) the
same test goes red and the undelivered row lands terminal:

```
$ go test ./internal/tools/ -run TestWriterIdentity_DeliveryArmRefusesTheIncidentsOwnCloseoutDoor -count=1
[task-notify] task run closeout: tw-incident → completed (child normal exit, wrapper writeback never landed)
--- FAIL: TestWriterIdentity_DeliveryArmRefusesTheIncidentsOwnCloseoutDoor (0.06s)
    … the child close-out wrote tw-incident back while its ground HEAD 5332c24e0c09 is ahead of the
    delivery point 1ac77976e194 (applied=true out="run closeout: tw-incident → completed")
```

**Restore is verified against a REVISION, not the index** (Rule 5's timeliness
half — an index-relative diff would have written back the very bytes the index
held):

```
$ cp /tmp/tasks.go.bak internal/tools/tasks.go
$ git diff HEAD --stat -- internal/tools/tasks.go
                                    (empty = restored)
$ git hash-object internal/tools/tasks.go ; git rev-parse HEAD:internal/tools/tasks.go
19d588fed34b3efa237ac0e233c70b7c61c1bcb5
19d588fed34b3efa237ac0e233c70b7c61c1bcb5     ← identical
```

## 4. The measurement that decides it: the naive rule refuses the repair

`naiveOwnershipRefusal` in
`internal/tools/task_writeback_writer_identity_test.go` is the smallest honest
spelling of "the writer must be the owner": allow the write only if the writer's
pid equals the row's claimed pid, or equals the wrapper pid the run published.

On the **same fixture with the work pushed** — the *legitimate* close — this
happens:

```
$ go test ./internal/tools/ -run TestWriterIdentity_NaiveOwnershipRuleRefusesTheLegitimateCloseout -count=1 -v
--- PASS: TestWriterIdentity_NaiveOwnershipRuleRefusesTheLegitimateCloseout (0.11s)
```

The test passes only because: **(1)** the delivery arm ALLOWS that write (the
harm is absent, the row lands `completed`), and **(2)** the naive ownership rule
**refuses exactly that same write**. The child that legitimately writes the row
back is *not* the pid the row was claimed under — that is not an edge case, it
is the definition of a wrapper-death repair. A predicate whose false-refusal set
contains the repair it was built to protect is a regression wearing a guard's
clothes.

The false-refusal set is not limited to that one door, and the OTHER half of
the argument is worse — **measured, and it contradicted this epic's first
guess**:

```
$ claimHolder("merge-executor")               -> host=""   pid=0     ok=false
$ claimHolder("merge-verify-staleness-guard") -> host=""   pid=0     ok=false
$ claimHolder("host-b:1190977")           -> host="host-b" pid=1190977 ok=true
```

The merge executor and the merge-verify staleness guard close rows they re-claim
under their own **non-pid** holder stamps (`mergeExecClaimHolder`,
`mergeVerifyStaleGuardHolder` — both named constants). An ownership predicate
keyed on `claimed_by`'s pid therefore cannot see those doors at all, and "no pid
to judge" degrades to a silent **pass**. A rule that is unsafe where it fires and
blind where it does not is not a guard; that is pinned by
`TestWriterIdentity_OtherLegalDoorsAreInvisibleToAnOwnershipRule`, which fails if
either stamp ever starts parsing as a pid claim (the moment the blindness would
become a refusal and the decision must be re-measured).

## 5. What this epic does and does not do

**It does not ship an ownership predicate.** The harm the incident shows is
*undelivered work wearing a success terminal*, and that is precisely and only
what the delivery arm measures. The harm process-ownership would additionally
catch — a stranger writing a row it does not own — has a closed set of doors in
this codebase, all of which write the row's own legitimate outcome.

**What is already in place and was re-verified here**, rather than re-invented:

- the delivery gate at the one choke point (§3);
- the child-side close-out's own two gates (`runCloseoutClaimEvidenceHolds`):
  a **live** claim pid is never closed underneath (so the wrapper's richer
  writeback is never replaced), and a dead claim pid on the run's own row is
  closed *with* the trail saying which fallback wrote it.

**Known residual, recorded not swallowed (Rule 15).** The delivery arm is
three-valued, and a row whose ground and destination are both unreadable closes
as `delivery-proof-unverified`. That is deliberate — a guard that refuses what
it cannot see wedges the ledger — and it is the one shape in which a genuinely
undelivered row could still reach a terminal state. Closing that gap is a
measurement question about the *ground*, not about the *writer*, so it belongs
to the delivery arm, not here.

## Re-checking this decision

```bash
go test ./internal/tools/ -run 'TestWriterIdentity|TestDeliveryProof' -count=1 -v
```

If someone ships an ownership predicate at `taskUpdateStampedCore`,
`TestWriterIdentity_NaiveOwnershipRuleRefusesTheLegitimateCloseout` is the test
that fails, and its message names the constraint: the child END-hook repair
must keep working.
