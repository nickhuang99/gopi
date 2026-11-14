<!-- doc-class: normative -->
# SECOND-OPINION REVIEW: gopi task node-sync family (GLM / HP / 2026-09-02)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

> **CORRECTED 2026-11-05.** The engine this page reviews is GONE: `SyncFullReconcile`,
> `syncPeerSessionFull`, the per-peer full counter, `SyncTickShouldRunFull` /
> `MustSyncFullEvery` and `GOPI_SYNC_FULL_EVERY` were deleted together with the whole
> cross-node sync surface (CONFIG.md carries the removal note; the anti-re-advertisement
> gate is `internal/config/sync_knob_removal_gate_test.go`, and the surviving engine is
> described at the top of `internal/tools/memory_sync_reconcile.go`). The P2 findings
> below are therefore **records of a review of code that no longer exists** — kept
> because the METHOD (parse arm vs engine signature) is reusable, not because the
> defects are live. Nothing in this page asks a reader to fix anything.
>
> **CORRECTED 2026-10-29 (epic `01a186d6`).** The worker round no longer carries
> a peer leg: `workerSyncPeers()` (the per-round db-sync) and
> `workerTakeOverAttempt()` (the peer-row transfer) are DELETED from
> `cmd/agent/cli_tasks_worker.go`. Statements below that describe either of
> them as the round's own behaviour describe the tree BEFORE that epic. What
> the round does today: `devdoc/worker-round-audit.md`.


Independent re-review of the `gopi task` node-sync surface — the multi-node
task-ledger sync verbs (harvest/diff/sync/push/worker). **This is the SECOND
independent pass** (first review on peer1/deepseek consulted as *git history
only*, findings re-derived from source on the current master head a91dace).

Method (per epic): run `-h` heads → read CLI + engines → docs + git history
→ targeted tests → prioritized P0/P1/P2 with file:line evidence.

Scope files:
`cmd/agent/cli_tasks_sync.go` (sync/harvest/diff verbs, serve tick),
`cmd/agent/cli_tasks_sync_git.go` (git push/pull pass + `task push`),
`cmd/agent/cli_tasks_worker.go` (M4.20 worker loop),
`cmd/agent/cli_tasks.go` (tasksUsage, parseTasksArgs, runTaskRun/writeback),
`cmd/agent/main.go` (isWorkerSubcommand),
`internal/tools/memory_sync_{delta,session,reconcile,claim,claim_ssh,task}.go`,
`internal/tools/task_sync_git{,_pushback}.go`,
`internal/agent/taskregister.go` (M4.19 terminal push hook),
`internal/config/config.go` (peer whitelist).

Note: master has already absorbed fixes from the FIRST review
(`2a34ff6 fix(worker): -h prints usage`, `c1f9c7d fix(tasks): per-verb -h
for sync family`) — both P0-1/P0-2 findings of that review are CONFIRMED
FIXED here. This pass therefore focuses on what remains, plus findings the
first pass did not report.

---

## Verdict summary

The node-sync family is, at this head, in good shape on the fundamentals:

- **Rule 1 / Rule 2 compliance is exemplary.** Every literal (timeouts,
  retries, margins, temp prefixes, peer paths, backoff schedules) is a named
  constant with a single env reader (syncIntervalFromEnv, workerMaxRounds,
  SyncFullEvery, serveGitSyncInterval, mergeAuditIntervalFromEnv).
- **Version gate is uniform across every channel.** syncCheckPatchVersion
  guards serve-remote, claim-remote, import-merge and the envelope parse;
  deliverable blobs carry their own version; patch Full is a monotone
  additive field. A peer running an older binary is refused cleanly, never
  mis-merged.
- **Whitelist contract is enforced everywhere.** A peer must be declared in
  sync_peers / GOPI_SYNC_PEERS; session, full, push-only and claim legs all
  gate (syncPeerDeclared / syncResolvePeers).
- **The two-phase claim lock (M4.21), anchor guard (M4.24), the bug053
  human-ticket ruling guard, and the anti-reflow gate** are each
  well-commented, self-consistent state machines with dedicated regression
  tests.

The three genuinely important findings are about **semantics gaps between
documented contract and shipped behavior** (P1) plus a set of smaller
robustness/UX gaps (P2). No P0 was found on this head (the two P0s of the
first pass are fixed).

---

## P1-1 — worker dies (os.Exit 1) on a single failed task run; serve survives
the same failure — the drain contract is broken by design drift

**Files/lines:**
- `cmd/agent/cli_tasks_worker.go:139` — workerRound calls `runTaskRun(...)`
- `cmd/agent/cli_tasks.go:1008-1012` — `runTaskRun` = `runTaskRunOnce` +
  `os.Exit(1)` on ANY error
- `cmd/agent/cli_tasks.go:986-990` — a child `gopi run` that exits non-zero
  (a normal "the LLM run failed" outcome) → `writebackOutcome(failed)` then
  `return fmt.Errorf(...)` → `runTaskRun` exits the process
- contrast: `cmd/agent/cli_tasks.go:1383-1386` — the serve daemon calls
  `runTaskRunOnce` directly and `continue`s on error (bugfix-task-p1,
  commit 0d759d0, "a failed run must NOT kill the daemon")

**The bug:** a worker that claims a task whose child run FAILS (exit≠0 —
a routine occurrence; the row is correctly written terminal `failed` by
writebackOutcome BEFORE the error return) then hits `os.Exit(1)` in
runTaskRun. The whole worker process dies — the remaining queued tasks are
never drained, and the process exits 1. This directly contradicts the
worker's own M4.20-D drained-exit-0 contract ("a drained queue is SUCCESS
— not an error") and the loop header's promise
(`runTaskWorker` prints "exit on drained queue").

**History (design drift):** `e02bb98` (M4.20-A) wrote the worker against
`runTaskRun`'s os.Exit contract deliberately ("a worker dies stateless,
exactly like any runner" — cli_tasks_worker.go comment). Commit `0d759d0`
later fixed the SERVE daemon to survive failed runs (the identical child-
failure path) but the worker was never migrated to `runTaskRunOnce` +
continue. So today serve and worker have opposite failure semantics for the
same event, and the worker's own stated "drain the queue" purpose fails on
the first flaky task.

**Why it is not merely cosmetic:** with the writeback already terminal, the
worker dying provides no safety value (no double-run risk — the row is
`failed` and unclaimable). It only (a) abandons the rest of the queue, and
(b) exits non-zero for a condition that is not an infrastructure error.
GOPI_WORKER_MAX_ROUNDS (0 = unlimited drain) is then meaningless in
practice for any queue that contains a failing row.

**Suggestion (P1):** workerRound should call `runTaskRunOnce` directly,
log the error and `continue` — mirroring the serve daemon. The os.Exit
wrapper remains correct for the standalone `gopi task run` CLI. Add a
regression test: seed N tasks where the first child exits non-zero
(harness test escape env exists: `taskChildFailTestEnv=1` in runTaskRunOnce
drives a synthetic child failure) and assert the worker still drains the
remaining N-1 and exits 0.

---

## P1-2 — M4.19 ② "must-succeed terminal push" covers ONLY the auto-registered
mirror row, NOT the claimed ledger row that task run / the worker actually
executes

**Files/lines:**
- `internal/agent/taskregister.go:161-211` — `finishSessionTaskRecorded`:
  the ONLY production caller of `PushTerminalTaskSync` (line 207), invoked
  with `taskID := a.autoTaskRowID` — the child agent session's
  AUTO-REGISTERED mirror row (created at loop.go runEngine start by
  maybeRegisterSessionTask, tagged `wrapper:<parent-id>`), NOT the claimed
  task-row being executed.
- `internal/tools/memory_sync_claim.go:690-728` — PushTerminalTaskSync =
  the "END-of-life must-succeed push (epic M4.19 ②)".
- `internal/tools/tasks.go:2994-3026` — `TaskRunWriteback` (the CLAIMED
  row's terminal writeback) contains NO push call.
- `cmd/agent/cli_tasks.go:1127-1144` — `writebackOutcome` routes worker
  rows through `TaskRunWritebackTagged` → still no push.
- usage/docs claiming otherwise: `cmd/agent/cli_tasks_worker.go:15-17`
  ("③ terminal state lands via the standard writeback, whose M4.19 ②
  must-succeed push ... guarantees the terminal status reaches every sync
  peer — no worker-side push code here"); `cmd/agent/main.go:57-59` (workerUsageHead);
  devdoc/m420-worker-loop.md step 3 ("inherited").

**Consequences, both verified by code reading:**
1. The claimed ledger row's terminal state (completed/failed) travels to
   peers ONLY through the best-effort `workerSyncPeers` session (worker) or
   the serve tick's SyncPeerSession — never through the must-succeed push.
   If the peer is unreachable at that moment and the tick is off, the peer's
   copy of the row can sit `in_progress` indefinitely (the zombie class the
   stale sweep exists for). The M4.19 ② "no reason to fail" guarantee does
   not hold for the row that actually matters.
2. The push that DOES run targets the child's auto-registered mirror row —
   a row the peers may never have imported (its first import would be a
   future delta sync). The peer's claim handler answers `row-not-found`
   (uncontested-accepted) and writes nothing — so the "must-succeed" push
   frequently no-ops on the wire, giving a false sense of delivery.

**History:** 0f65cf7 originally pushed `a.SessionLog.SessionID()`; the
current code pushes `a.autoTaskRowID` — either way it is the mirror row.
The gap (claimed row never pushed) has existed since M4.19 and the worker
doc inherited the wrong claim in M4.20.

**Suggestion (P1):** call `PushTerminalTaskSync(taskID)` for the CLAIMED
row in `writebackOutcome` (or in TaskRunWriteback) after the terminal
write, alongside the existing mirror-row push — or, if the design intends
delta-sync as the claimed row's carrier, correct the worker usage head /
m420 doc to say so and document why the must-succeed push is mirror-only.

---

## P1-3 — serve-tick full-reconciliation counter burns on FAILED sessions
(and on failed FULL sessions), contradicting its own comment

**Files/lines:**
- `cmd/agent/cli_tasks_sync.go:311-323` — runTaskSyncTick calls
  `tools.SyncTickShouldRunFull(p)` BEFORE the session runs; the comment
  directly above says "The counter advances only on SUCCESSFUL delta
  sessions (a failed session must not burn the count)."
- `internal/tools/memory_sync_reconcile.go:438-457` — `syncTickShouldRunFull`
  unconditionally does `count := load+1` and `save(count)` (or save(0) when
  the full threshold is hit) on EVERY call — with no knowledge of whether
  the session that follows succeeds.
- `internal/tools/memory_sync_reconcile.go:458-460` — the comment
  "consumes the counter AFTER a successful incremental session" does not
  match the implementation's call site.

**The bug:** tick N calls ShouldRunFull → counter becomes 1 (say N=10);
the session then FAILS (peer unreachable / ssh timeout) → the tick
`continue`s. Next tick the counter is already 1 — the failed session
burned a count. After 10 consecutive failures the tick runs a FULL
reconcile against a peer it could not reach, which also fails — and that
failure has already RESET the counter to 0, so the backstop is consumed
without ever having run. The stated purpose ("the periodic backstop that
eventually catches anything a delta window ever silently dropped") is
degraded exactly when the channel is unhealthy.

**Suggestion (P1):** restructure the tick to run the delta session first
and only on success call the counter/decision for the NEXT session, or make
SyncTickShouldRunFull a two-phase consume (decide → run → commit/rollback
the counter based on session success). Add a regression test simulating a
failed session and asserting the counter is unchanged.

---

## P2 findings (robustness / UX / dead surface)

### P2-1 — `sync --full --tags X` is accepted by the parser but silently
drops the tags partition
- `cmd/agent/cli_tasks.go:1321-1333` — the sync parse arm rejects many flag
  combos but never `--full` + `--tags`; `cmd/agent/cli_tasks_sync.go:158`
  calls `SyncFullReconcile(p, nil)` — tagsFilter hard-nil.
- The engine (`memory_sync_reconcile.go:200` `syncPeerSessionFull`) actually
  ACCEPTS a tagsFilter parameter — the CLI just never passes it.
- **Suggestion:** reject `--full --tags` at parse time with a pointed error
  (full = whole-ledger by definition), or thread `opts.syncTags` through.

### P2-2 — `sync --peer X --peers a,b` is accepted; `--peer` silently wins
- `cmd/agent/cli_tasks.go:599-600` reads both; no mutual-exclusion check;
  `cmd/agent/cli_tasks_sync.go:147-148` then overrides the list with the
  single `--peer`. A typo'd invocation silently narrows the session.
- **Suggestion:** reject the combination at parse time (both specify the
  target set; they cannot mean different things).

### P2-3 — `task push -h` / `task diff -h` show the sync-family head but
never document the git-channel semantics of `push`
- `cmd/agent/cli_tasks.go:2057-2062` prints `syncUsageHead` for verbs
  sync|diff|harvest|push, then tasksUsage. syncUsageHead documents sync/
  diff but says nothing about `push` (git channel) — which is documented
  only in the family tasksUsage block (`cli_tasks.go:225-232`). Push IS
  actually covered there, so this is cosmetic: the head implies push is a
  sync-verb when it is a git-verb. The head/file are otherwise fine (the
  first review's "syncUsageHead dead code" finding is FIXED — it is now
  referenced at cli_tasks.go:2059).
- **Suggestion (cosmetic):** add a one-line push entry to the head or drop
  push from the head-printing verb set.

### P2-4 — worker claim errors masquerade as "queue drained" (exit 0)
- `cmd/agent/cli_tasks_worker.go:96-108` — `workerRound` returns false on a
  claim ERROR as well as on an empty queue; `runTaskWorker:85-91` then
  prints "queue drained — exiting" (or "other holder active") and returns
  exit 0. A transient DB error on BOTH the round and its 1s re-check makes
  the worker report drained-success with a non-empty queue. Error is only
  on stderr, exit code lies.
- **Suggestion:** distinguish Empty from Err in workerRound's return and
  exit non-zero on a genuine claim error after the re-check.

### P2-5 — syncFull/claim/worker shares: `SyncTickShouldRunFull` env typo
behavior is fine (error), but the serve tick prints the FULL report line
via `MustSyncFullEvery()` which silently falls back to the default on any
error — acceptable, but the full-run cadence env (`GOPI_SYNC_FULL_EVERY`)
is not in the family usage text. Minor doc gap; usage head mentions the
default (10) but not the env name. Suggest adding the env name to
syncUsageHead's --full paragraph (it is present in tasksUsage's sync
paragraph already).

### P2-6 — worker's db-sync leg prints a summary line but the "migrated"
count conflates imported+mirrored
- `cmd/agent/cli_tasks_worker.go:174-182` — `pulled = RowsImported +
  RowsMirrored` while the session's own stats report them separately.
  Cosmetic/observability only.

---

## Positive confirmations worth recording (second-opinion value)

- **Anchor semantics are coherent:** receive-anchor per peer, committed only
  after a successful merge, forward-only (syncStateCommit clamps backwards),
  margin-backdate 5s absorbs clock skew, and the M4.24 anchor guard refuses
  advances when pending counts diverge — with `task diff` + `sync --full`
  as the executable repair pair. devdoc/sync-reconcile.md matches the code.
- **Claim broadcast two-phase lock is internally consistent:** local CAS →
  remote CAS → advance; refusals yield (revert-to-pending, claimed_by kept),
  unreachable retries with ALOHA backoff capped at 30s/3 rounds; the
  self-preseed and self-exclusion guards close the real incidents
  (claim-lock self-refusal, from-<peer> rows unreachable by claim patches —
  1e8e8e3/c4745d9/b988b64 history shows genuine incident-driven fixes with
  regression tests).
- **bug053 guard is sound:** syncRowIsMirrorNoise distinguishes real human/
  llm tickets (never blind-flattened) from registration noise; the
  ledger-conflict-human-task alarm path exists.
- **`worker -h` and per-verb `-h` are now side-effect-free** (regression
  tests TestWorkerHelpDoesNotRun lock this through the REAL binary).
- **ssh argv robustness is well handled in the sync/claim legs:** remote
  commands travel as ONE quoted argv element (syncRemoteGopi comment,
  memory_sync_task.go:155-158) precisely to avoid the agent-loop hazard;
  the isTasksSubcommand ssh-passthrough skips -o pairs/lone flags before
  host detection and handles the login-shell single-arg shape.

## Verification performed

- `go build ./cmd/agent/` OK; `/tmp/gopi-review2 worker -h`, `task sync -h`,
  `task diff -h`, `task push -h` all exit 0 and print usage (no side
  effects — worker -h prints usage head, does not run the loop).
- `go test ./internal/tools/ -run 'TestSyncTickFullCounter|TestPushTerminalTaskSync|TestReconCompare|TestAnchorGuard' -count=1` → ok (5.7s)
- `go test ./cmd/agent/ -run 'TestWorker|TestWorkerHelp|TestWorkerSync|TestWorkerDrained' -count=1` → ok (6.6s)
- Targeted grep/read evidence cited above; git log/blame consulted for
  design-drift attribution (e02bb98 → 0d759d0 asymmetry, 0f65cf7 M4.19 hook
  target).

## Not Done / scope notes

- No live two-node E2E was run (no peer ssh reachable from this review
  worktree; the review is static + unit-level, per the docs-only mandate).
- P1-1/P1-2/P1-3 are suggestions with proposed fixes, not implemented here
  (review task = report only).
