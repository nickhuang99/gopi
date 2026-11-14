<!-- doc-class: normative -->
# devdoc: Client Close-out Delivery — Push Before the Dirty Gate, and the Signal When It Fails

Task: fix the finish-cleanup close-out chain on a gopi CLIENT host (HP) so an
untracked scratch file can no longer strand a finished branch, and so that a
branch that fails to reach the peer is **visible** rather than silent.

Branch: `fix/client-closeout-push-delivery` (worktree `~/workspace/gopi-push-delivery`,
based on master `69975030`).

---

## 1. The defect, restated mechanically

`taskFinishCleanup` → (client arm) `taskFinishCleanupChain` ran, in this order:

1. branch/exempt gate + worktree ownership gate;
2. **dirty worktree gate** — `git status --porcelain` non-empty ⇒
   `rep.Skipped = true; return`;
3. push → `git push <peer> <branch>:<branch>`;
4. `git worktree remove --force`;
5. `git branch -d`.

Step 2 returned before step 3. The guard's *purpose* is to protect steps 4 and
5 — the destructive ones (`--force` removal deletes the directory; `-d` deletes
the ref) from a tree a live bond may still be writing in. But because the guard
sat at the head of the chain, it also suppressed the **push**, which is
neither destructive nor directory-dependent:

- `git push <peer> <branch>:<branch>` moves a ref **on the peer** and transfers
  nothing from the working tree; it is idempotent (an up-to-date ref is a
  successful no-op).
- HP runs end with untracked `.evid/` residue. One such file ⇒ the whole chain
  skipped ⇒ the branch stayed on the client host, the ledger said `completed`,
  and the only delivery mechanism the host has never ran again.

That last clause is the part that makes it a *silent* failure: the close-out is
a **single shot per row**. Nothing re-runs it.

## 2. The fix, in three parts

### 2a. The guard's REACH changed — never its tolerance

The one judgement split into two, and the destructive half kept every bit of
its strictness:

| step | before | after |
|---|---|---|
| branch/exempt + ownership gate | first | first (unchanged) |
| branch exists locally | (implicit, later) | explicit probe, before the delivery |
| **push** | *after* the dirty gate | **before** the dirty gate |
| dirty gate | gated everything | gates only ③④ |
| `worktree remove --force` | open if clean | open if clean (unchanged) |
| `branch -d` | open if clean | open if clean (unchanged) |

`finishCleanupWorktreeDirtyReason` is **untouched** — same function, same two
dirty states, same two reason strings, same exact-match sentinel check (so a
residue file named after the sentinel is still refused). What changed is *who
it is allowed to stop*: the chain now returns `Skipped` with the dirty reason
**after** the push has been attempted and recorded on the report. An operator
reading a dirty-skip report sees the push step in `rep.Steps`.

A dirty tree cannot make a push lose work: the ref pushed is a *committed* one.

### 2b. Visibility — the delivery verdict

New: `finishCleanupDeliveryObserve` (in `internal/tools/task_finish_delivery.go`),
called from `taskFinishCleanup` — the one place **every** close-out face
converges (the executor child's end hook, the resident sweep, and the one-shot
`gopi task finish-cleanup` verb all route through it).

It is deliberately **independent of the skip reason**, because the question it
answers is a factual one: *does the peer hold this row's branch?* It asks the
peer directly — one read-only `git ls-remote <peer> refs/heads/<branch>` —
rather than inferring from a push exit code. A chain that stopped on a dirty
worktree may still have delivered the branch on an earlier pass, and a chain
that "succeeded" can lie (the 2026-09-16 hand-push history has both directions).

Outcomes:

| situation | action |
|---|---|
| this pass's push landed | retire any stale `unpushed` mark |
| peer holds the ref | nothing — no record, no alarm |
| peer does not hold it | **record + alarm** (below) |
| plan mode | nothing at all (no mutation, no probe) |

**The record** (`finishCleanupDeliveryRecord`) is written on the ROW, never the
worktree — repo + branch are on the ledger row and survive the directory being
recycled:

- `task_events` row with `event = "completed-unpushed"` (composed as
  `taskStatusCompleted + "-unpushed"` — never a bare literal, Rule 1b) and the
  full sentence as
  the detail (append-only audit trail; bounded to 3 records per row so a
  repeatedly-failing pass cannot grow the trail without end);
- the row's tag **`unpushed`** — the operator's handle *without* opening a
  ledger or running a dry-run: it shows up in `gopi task list` / `task show`,
  and it is also what the safety net scans for (one mark, two readers);
- a stderr line (`taskNotifyLog`) so a pass log carries it too.

The sentence names the row, the branch, *why* the delivery is unproven, and
both handles, e.g.:

```
task t-abc123: branch feat/x did NOT reach the peer (no sync peer resolved —
nothing to hand the branch to) — the row's close-out left it on this host;
handles: task_events.event="completed-unpushed", tag "unpushed"
```

**The alarm** (`NotifyDeliveryStranded`, `internal/tools/memory_sync_claim.go`)
is a new notify event `delivery-stranded`, delivered through the existing
`GOPI_TASK_NOTIFY_CMD` path with the existing payload shape — same class as
`terminal-push-failed`, deliberately its own name so an operator can subscribe
to "the close-out delivered nothing" without also subscribing to the sync pipe's
end-of-life push failure. It is **not** in the curated default event set; a
subscriber using `"all"` receives it.

### 2c. The safety net — a pass, not a daemon

New: `FinishCleanupDeliveryPass(planOnly)` (same file), wired into
`runTaskLifecycleTick` (`cmd/agent/task_lifecycle_tick.go`) — the tick that both
resident components (worker loop, merger) actually fire, right beside the
existing `serveFinishCleanupSweep`. **No new process, no new daemon.**

It is keyed on the ledger, not the worktree:

1. scan terminal rows carrying the `unpushed` tag (oldest-first, capped at 10
   per pass — a backlog drains over several passes rather than in one unbounded
   flush);
2. per row, resolve repo+branch from the deliverable;
3. peer already holds the ref → clear the mark, push nothing;
4. peer master already contains the tip → clear the mark, **push nothing** —
   this is the resurrection guard: the server's adoption chain merges and
   *deletes* the ref on purpose, and re-pushing would recreate a ref the server
   deliberately removed (the 2026-09-02 stale-branch flood);
5. neither → push, **verify from the peer**, then clear the mark; on failure
   re-record (bounded) so the trail and tag survive the attempt, and fire the
   alarm.

The ancestor test (step 4) needs the peer's master locally, so it is fetched
once per repo per pass; a fetch failure is reported as
`peer master unfetchable — cannot tell integrated from stranded` and the mark
**stays** — never treated as "not merged", because that would push blind.

The peer target is resolved through `finishCleanupDeliveryTarget`, a
package-level seam defaulting to `GitSyncServerTarget` (the same resolver the
git sync channel uses — a pass pushing to a different peer than the chain would
be a second, silently diverging topology). Tests point it at a local bare repo.

Fail-soft per row, never silent: every outcome is a line the tick logs.

## 3. One prerequisite the re-order surfaced

The push now runs for rows the old order never reached, including the
**idempotent second pass** of a fully-recycled row (the first pass pushed,
removed and deleted the branch). Pushing a ref that no longer exists locally
makes git answer `src refspec … does not match any`, which reads as a delivery
*failure* for a row that is actually finished. So an explicit local-branch
existence probe now runs before the delivery, and such a row skips with
`branch <b> does not exist locally (already delivered and deleted, or never
created)`.

That reason is registered with `FinishCleanupReportSettled` as **settled** (not
a refusal): the chain has nothing left to do, and counting it as a refusal is
what made `refused=N would-run=0` read as a stuck sweep. It does **not** claim
the work was delivered — that is exactly what the delivery verdict asks the
peer, separately. A branch gone locally *and* absent from the peer is reported
as the loss it is.

## 4. Compound case (push failed AND worktree dirty)

Both facts are now reported at once, and neither is swallowed:

- `rep.Steps` carries the push step with `OK:false` and git's output line;
- `rep.Skipped` / `rep.SkipReason` carries the dirty reason;
- the worktree and the branch both survive (the `-D` fallback derives its
  standing from *this sweep's own landed push*, so an undelivered ref is never
  force-deleted);
- the delivery verdict then asks the peer; not delivered ⇒ record + alarm.

Covered by `TestFinishCleanupChainCompoundDirtyAndPushFailed`.

## 5. Verification

| check | result |
|---|---|
| `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` | OK |
| `go vet ./...` | clean |
| `gofmt -l <changed files>` | clean |
| `go test ./internal/tools/` | PASS on the close-out suites; **3 pre-existing reds reproduce identically on pristine master `69975030`** (`TestSuiteScratchRootIsMemoryBacked`, `TestScratchRootFilesystemClassification` — the host's scratch root is ext4, a host-topology assertion; `TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy` — baseline) |
| `go test ./cmd/agent/` (full package) | **FAILED on BOTH trees — pre-existing, not attributable to this branch.** This branch: `FAIL … 482.724s`; pristine master `69975030`: `FAIL … 445.639s`, both under three other sessions saturating the host (load ~8-12), both with NO `--- FAIL:` test line (a package-level budget/timeout red, not a test assertion). The goroutine dump's selected frame is `TestOwnRelease_RefusesLiveSibling` blocked in `scratch.LiveHolders()`; that test passes in isolation on BOTH trees (`ok … 4.688s` branch, `ok … 4.594s` master). This branch touches no file in that path — see §8. |
| master counterpart measured | every red above was reproduced on a pristine `69975030` worktree before being attributed: 3 `internal/tools` reds, 1 `internal/standards` red, the whole-tree gofmt list, and the `cmd/agent` package red. |
| `go test ./internal/standards/` | 1 red, **byte-identical on pristine master `69975030`** (`TestGateCwdReleaseFindsTheRealTree`, worktree-remover inventory floor 5 vs 4 found) — pre-existing, not attributable to this branch |
| guard tolerance, measured | `finishCleanupWorktreeDirtyReason`'s function body extracted from `master:…` and from this branch and compared: **byte-identical**. The tolerance provably did not change; only its reach did. |

Red → green, per the epic's requirement (each test was run against the
pre-change code and observed failing, then green after):

- `TestFinishCleanupChainDirtyWorktree` — **this test's old assertion was the
  defect**: "no steps may run on a dirty worktree". Redefined: a dirty tree
  still refuses both destructive steps *and* the push now lands
  (`assertStep(push, true)`, peer provably holds the ref, worktree + branch
  survive).
  **Red proof, actually run**: the redefined expectation was ported onto a
  pristine `master` worktree (`tmp/redproof-master` @ `69975030`) together with
  the shared fixture helpers, and failed exactly as the defect predicts:

  ```
  --- FAIL: TestFinishCleanupChainDirtyWorktree
      task_finish_cleanup_test.go:228: step push missing from report
      [{Step:worktree Target:…/wt OK:false Detail:untracked-only residue …}]
  FAIL
  ```

  i.e. on the old code the report contains **no push step at all** — the dirty
  gate returned before it, which is precisely the strand. Green on the fix.
  (The red-proof worktree was removed afterwards; nothing left behind.)
- `TestFinishCleanupChainDirtyTrackedStillDelivers` — same split for tracked
  modifications (both dirty states refuse the destructive half; neither holds
  the delivery hostage).
- `TestFinishCleanupChainPushFailKeepsBranch` — failed push ⇒ branch not
  deleted, peer does not hold the tip; chain continues fail-soft.
- `TestFinishCleanupChainCompoundDirtyAndPushFailed` — the compound case above.
- `TestCloseoutRecordsAStrandedDelivery` / `…NothingForADeliveredBranch` /
  `…DeliveredPushClearsAStaleMark` / `…SilentInPlanMode` /
  `…StrandedFiresTheNotifyAlarm` — the ledger signal (event + tag), the
  no-signal-on-healthy-host case, the good-path mark retirement, plan silence,
  and the real notify command receiving the payload.
- `TestDeliveryPassPushesAStrandedBranch`,
  `TestDeliveryPassSkipsAnAlreadyMergedBranch`,
  `TestDeliveryPassSkipsABranchThePeerAlreadyHolds`,
  `TestDeliveryPassPlanTouchesNothing`,
  `TestDeliveryPassUnreachablePeerStaysObservable` — the safety net's five
  required behaviours on a REAL two-node git world (bare "server" + client
  clone), including the resurrection guard asserted against `ls-remote` on the
  bare server, not against a returned struct.

## 6. Design decisions worth knowing

**Why the verdict asks the peer instead of reading the chain's result.** The
chain's result is a statement about *this host's attempt*. Delivery is a
statement about *the peer's state*. The 2026-09-16 hand-push history has a
branch on the peer that no close-out ever put there, and a close-out that
"pushed" a branch the peer did not end up with. Only the peer can answer.

**Why the mark lives on the row and not on the worktree.** Every condition that
stops the chain also stops the push, and a push needs only repo+branch — both
on the row even after the directory is gone. Keying on the worktree would make
the safety net useless in exactly the cases it exists for (a gone worktree).

**Why two handles (event + tag) for one fact.** They have different readers.
The event is the append-only audit row (greppable with `sqlite3`, and what a
future forensic pass reads); the tag is the operator's handle in `task list` /
`task show` and the safety net's index. `FinishCleanupDeliveryStranded(event)`
and `FinishCleanupDeliveryUnpushedTagName` export the reader side so no consumer
re-types the literal (Rules 13/14).

**Why the pass is not a daemon.** The briefing's constraint, and the right one:
the tick already exists, already runs on both resident components, and already
carries the sibling `finish-cleanup` sweep. A new process would be a new thing
to supervise for no added reliability.

**Why the raw git runner gained a deadline parameter instead of a twin.**
The delivery probe asks one remote-ref question and must not inherit the
chain's whole-invocation budget (60s) — a probe that hangs on an unreachable
peer would stall the close-out it is trying to report on. So the deadline
became the only variable: `finishCleanupGitRaw` (unchanged callers, chain
budget) and `finishCleanupGitRawTimeout` (the probe, `finishCleanupDeliveryProbeTimeout`)
are one implementation behind one `exec.CommandContext`. No second runner, no
duplicated argument plumbing.

**Why `delivery-stranded` is not in the curated default event set.** A
subscriber who listed statuses should not silently start receiving a new class
of alarm; a subscriber who asked for `"all"` gets it. Same convention as the
other synthetic alarms.

## 7. Files

| file | change |
|---|---|
| `internal/tools/task_finish_cleanup.go` | re-ordered chain (delivery first), local-branch probe, dirty gate now gates only ③④, delivery verdict hook in `taskFinishCleanup`, `finishCleanupBranchGoneReason` + settled registration, timeout-parameterized git runner |
| `internal/tools/task_finish_delivery.go` | **new** — delivery verdict (probe/record/observe/clear) + the safety-net pass |
| `internal/tools/task_finish_delivery_test.go` | **new** — safety-net acceptance on a real two-node git world |
| `internal/tools/task_finish_delivery_observe_test.go` | **new** — the observability half (ledger + tag + real notify command) |
| `internal/tools/task_finish_cleanup_test.go` | dirty-worktree expectation redefined to the fixed contract; tracked-dirty and compound cases added |
| `internal/tools/tasks.go` | `taskRemoveTag` (the inverse of `taskAddTag`) |
| `internal/tools/memory_sync_claim.go` | `NotifyDeliveryStranded` |
| `internal/tools/tasknotify.go` | `delivery-stranded` event constant + inclusion in the `"all"` set |
| `cmd/agent/task_lifecycle_tick.go` | the delivery pass wired into the resident tick |

## 8. Not done (honest)

- **`go test ./cmd/agent/` is UNRESOLVED — the package red is PRE-EXISTING, but this branch is not claimed green on an idle host.** The full-package run FAILED on this branch (`482.724s`) AND on pristine master `69975030` (`445.639s`), both while other sessions were saturating this host (load ~8-12), and both with no `--- FAIL:` test line — a package-level budget red, not a failing assertion. The goroutine dump names `TestOwnRelease_RefusesLiveSibling`, blocked inside `internal/scratch.LiveHolders()`; that test passes in isolation on BOTH trees (this branch `ok … 4.688s`, pristine `ok … 4.594s`). So the red is not this branch's doing — but a shared red is still a red, and this change does not get to call itself verified until a full-package run passes on an idle host.
  Nothing in this branch touches `task_reclaim_self.go` / `scratch.LiveHolders` / `cli_tasks_worker.go`; the one `cmd/agent` file it changes is `task_lifecycle_tick.go` (+14 lines).
  Command to settle it on an idle host:
  `ANTHROPIC_AUTH_TOKEN=dummy go test ./cmd/agent/ -count=1 -timeout 900s`.
- The `unpushed` mark is written by the delivery verdict on the CLIENT arm
  only. A row that strands on the SERVER node (which never pushes by design)
  is not marked — correct, since there is no delivery for it to owe, but worth
  naming: the tag means "this row owes the peer a branch", not "this row is
  unfinished".
- The safety net retries at most `finishCleanupDeliveryScanMax` (10) rows per
  tick. A large backlog drains over several ticks; if that is ever too slow, the
  cap is one constant.
- No e2e test drives a full `gopi task finish-cleanup` CLI invocation against a
  live peer; coverage is at the package level (chain + verdict + pass) on real
  git repos. The CLI verb's wiring is exercised by the existing `cmd/agent`
  suite.
