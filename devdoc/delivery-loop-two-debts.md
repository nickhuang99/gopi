<!-- doc-class: normative -->
# The delivery loop's two debts — "failed" is two facts, and one of them was invisible

**Measured:** 2026-10-22 on this host (hosta), against the live ledger
`/home/user/.gopi/memory.db` and the clone grounds standing under
`/home/user/workspace`.

## 1. The symptom, in one measurement

Four rows read `failed` in the ledger while their ground held work that existed
nowhere else:

```
$ git -C /home/user/workspace/01a16a6b-… rev-list --count master --not refs/remotes/origin/master
1
$ git -C /home/user/workspace/01a16a6b-… status --porcelain | wc -l
0
```

ahead 1, dirty 0: the work is finished and committed, and the only copy of it is
in a directory whose owner row is already closed. That state has a name in this
system — a **delivery debt** — and the machinery to repair it already existed
(`task_delivery_retry.go`: the `delivery-blocked` event, the resident pass that
scans for it, the one close-out chain that pushes). What did not exist was a
**writer**: nothing ever put such a row on that queue.

Two separate defects were making that true, and they had to be fixed together —
fixing only the second would have queued rows on a measurement that always said
zero.

## 2. Defect A — the sweep measured the branch against itself

`worktree_sweep_undelivered.go` asks git one question before any removal arm may
delete a ground:

> how many commits does this branch hold that NO delivery point has?

and its delivery-point list was:

```go
refs/remotes/origin/<branch>
refs/remotes/origin/master
refs/heads/master          // ← the integration branch's LOCAL ref
```

The file's own doc says `refs/heads/<branch>` is absent by construction ("the
measurement has to name a place the work exists OUTSIDE the thing being
removed"). That reasoning is right, and the list still violated it — because on a
**clone ground** `deliverable.branch` **is** `master`. Every worker row on this
host records `"branch":"master"`; measured across the live ledger and every
ground standing under `/home/user/workspace`. So when `branch == master` the
third candidate IS the branch under measurement, and git answers 0 by
construction:

```
$ git -C <clone ground> rev-list --count master \
    --not refs/remotes/origin/master refs/remotes/origin/master refs/heads/master
0                                     # what the shipped list said
$ git -C <clone ground> rev-list --count master --not refs/remotes/origin/master
1                                     # the truth
```

A zero from a self-referential measurement is not "delivered"; it is "the
question was never asked". The caller acts on that zero by **deleting the
directory** (`devworkspace.Reclaim` → `os.RemoveAll`), which is precisely the
loss the rung exists to prevent.

**The fix** is applied to the BUILT list, not to the branch's spelling — any
candidate equal to `refs/heads/<branch>` is dropped whatever the branch is
called:

```go
self := refsHeadsPrefix + branch
for _, p := range candidates { if p != self { out = append(out, p) } }
```

The integration branch's **remote** tracking ref stays: it is a real delivery
point (the merger's answer), and it is what makes a finished branch's ground
truly spent.

## 3. Defect B — disposal closed the row and left the queue at the same instant

The disposal paths (the zombie sweep's confirmed kill, the wrapper's
abnormal-child exit) wrote `failed` and nothing else. `failed` is terminal by
design and `failed-needs-reissue` counts such rows honestly — but the count
cannot tell the two debts apart, and the delivery retry's scan keys on a trail
event (`delivery-blocked`) that nothing wrote. So the row left the queue in the
same statement that closed it.

`task_delivery_owed_handoff.go` adds the **handoff**, and deliberately it is a
RECORD, not a status:

> **RETIRED 2026-10-31 (task 01a19052).** This file and its tests were DELETED:
> the alone-deletion probe found them DEAD-ALONE and increment 3 found zero
> production references — its last production caller went with the zombie sweeper
> (`042e237b`), and the retry scan it feeds has no consumer left either. What
> remains is this record of what the handoff was FOR. Recovery, if a writer is
> ever wanted again: `git show c49dc227:internal/tools/task_delivery_owed_handoff.go`.

- **No value is minted, no transition is added.** The status model is untouched;
  a `failed` row stays terminal, and `failed-needs-reissue` keeps meaning what it
  says. What changes is that the row's trail now carries the fact that lets an
  operator (and a pass) tell the two debts apart.
- **The probe is the SAME one the sweep uses.** `taskGroundUndelivered` calls
  `worktreeUndeliveredCommits` through the sweep's own ground-shape decision
  (clone ground → its own refs; linked worktree → the shared repo's), so the
  ledger's verdict and the sweep's refusal cannot disagree. One implementation,
  two readers (Rules 13/14).
- **The event name IS the retry scan's key** (`DeliveryRetryBlockedEvent`), so
  the existing resident pass picks the row up with no second mechanism
  (Rule 35: no daemon, no timer, no script, no new table).
- **A row that owes nothing writes nothing.** A delivered ground, a missing
  worktree, a gone directory and a nonexistent branch all answer "not owed";
  and a branch sharing no ancestor with the integration branch is handed to the
  lineage rung rather than queued (the delivery chain would refuse it for the
  same reason and the row would cycle).

## 4. Why not a new status

Considered and rejected: `completed-undelivered` (or any minted status value).
It would need a `taskStatusSet` entry, a `taskAllowedTransitions` edge, a
`taskSchemaStatusCHECK` change, a sync-layer row mapping on every peer, and a
decision from every consumer that switches on `completed`. The trail event gets
the same operator-visible distinction for one `task_events` INSERT, and it rides
a scan that already exists. A status is a claim about the work; whether a push
happened is a claim about the CHANNEL, and the channel already has its own
vocabulary (`delivery-blocked` / `delivery-verified` / `delivery-noop`).

## 5. Verification

Red proof (mutation, then restore, then re-green) — the shipped self-referential
list restored, run against the new test:

```
--- FAIL: TestUndeliveredBranchItselfIsNeverADeliveryPoint
    delivery points contain the branch ITSELF (refs/heads/master) — the count is
    then taken against the thing being measured and git answers 0 by construction
--- FAIL: TestUndeliveredMasterBranchCloneGroundIsMeasured
    ahead = 0, want 1: a clone ground on master with one unpushed commit holds
    exactly one commit no delivery point has
```

Red proof of the handoff (the write deleted, the answer still returned):

```
--- FAIL: TestRecordDeliveryOwedWritesTheRetryScanKey
    the owed row's newest event = "", want "delivery-blocked" — that name IS the
    retry scan's key; anything else lands a record the pass never reads
```

End-to-end, through the REAL disposal verb on a sandbox ledger whose row named a
real clone ground holding one unpushed commit:

```
$ GOPI_HOME=<sandbox> gopi task sweep-zombies --kill
  [task-notify] zombie sweep 01a16ae7-…: delivery owed: the run closed without
      delivering — the ground holds 1 commit(s) no delivery point has
      (refs/remotes/origin/master, refs/remotes/origin/master)
  [task-notify] zombie sweep 01a16ae7-…: KILLED (stalled, … — confirmed zombie)
  status: failed                                       # ← still honestly failed

$ GOPI_HOME=<sandbox> gopi task retry-delivery
  01a16ae7-… already-merged branch=master peer=peerbox:~/work/GoPi — the peer's
      master already contains 0592c9ae… — left alone
```

The last line is the whole point: the row that used to vanish from every queue
now reaches the delivery path, and the delivery path answers from the PEER
rather than from a local guess.

## 6. What is NOT claimed

- **Not every `failed` row owes a delivery.** The rows this document measured
  were `zombie disposal` and `abnormal child exit` shapes whose ground still
  held commits. A row whose work never reached a commit is a plain failure and
  is recorded as one.
- **The structural fix prevents NEW occurrences; it does not retroactively
  deliver the four grounds measured above.** They are still there, still ahead 1,
  and the handoff only fires at disposal time — a row already terminal at the
  time of this change keeps its trail as it was. Reaching them is
  `gopi task retry-delivery --task <id>` per row, once their deliverable is
  checked; that is an operator decision, not this code's.
- **The signal layer is out of scope.** A run interrupted by an external
  `SIGTERM` still records the kill as it does today (`signalrec`,
  `signal-unsourced`); what changed is that the DISPOSAL that follows no longer
  drops the row's undelivered work silently.
