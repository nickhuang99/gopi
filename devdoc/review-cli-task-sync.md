<!-- doc-class: normative -->
# Review: gopi task node-sync family (harvest / diff / sync / push / worker)

> **CORRECTED 2026-11-05.** The mechanism this page reviews is DELETED: `SyncFullReconcile`,
> `syncPeerSessionFull`, `syncTickShouldRunFull`, `SyncFullEvery`, the per-peer full counter
> and `GOPI_SYNC_FULL_EVERY` went with the cross-node sync surface (2026-11-05). Every
> F-full-* finding below is a record of a review of code that no longer exists; the Rule 2
> compliance paragraph names `serveGitSyncInterval` / `syncIntervalFromEnv` /
> `SyncFullEvery` as the knobs' single readers, and **none of those readers exists today**
> (that is exactly why the knobs were removed — advertised, zero production readers).
> Removal records: `CONFIG.md`'s REMOVED rows, `internal/config/sync_knob_removal_gate_test.go`.
> The page is kept for its METHOD; nothing in it asks a reader to fix anything.

> **CORRECTED 2026-10-29 (epic `01a186d6`).** The worker round no longer carries
> a peer leg: `workerSyncPeers()` (the per-round db-sync) and
> `workerTakeOverAttempt()` (the peer-row transfer) are DELETED from
> `cmd/agent/cli_tasks_worker.go`. Statements below that describe either of
> them as the round's own behaviour describe the tree BEFORE that epic. What
> the round does today: `devdoc/worker-round-audit.md`.


Reviewer: GoPi (child task of the CLI code-review epic)
Date: 2026-09-02
Branch: `review/cli-task-sync` (worktree `~/workspace/gopi-review-task-sync`)
Scope files: `cmd/agent/cli_tasks.go` (ssh-passthrough), `cmd/agent/cli_tasks_sync.go`,
`cmd/agent/cli_tasks_sync_git.go`, `cmd/agent/cli_tasks_worker.go`,
`internal/tools/memory_sync_{task,delta,session,claim,reconcile}.go`,
`internal/tools/task_sync_git.go`, `internal/config/config.go`.

Review method followed: usage heads → source → engines → docs (devdoc M4.12/13/20/24,
bug052/053) → git log/blame → findings.

---

## 0. Headline findings (P0/P1)

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

### P0-1 — `gopi worker -h` RUNS THE WORKER LOOP (live side-effect on `-h`)

`cmd/agent/main.go:25` `isWorkerSubcommand` checks only `args[0] == "worker"` and calls
`runTaskWorker()` — **no `-h`/`--help` scan, no flag parsing whatsoever**. `gopi worker -h`
therefore starts a real worker: it claimed and EXECUTED a live pending task
(`t-1788305178`, the sibling config/options review task), spawned a nested gopi, and
marked the task **failed** with this review session's UUID as executor when the bash
timeout killed the loop.

This is the worst possible `-h` behavior for a CLI verb: `-h` must never have
side-effects. It also burned a sibling epic task (double-execution risk: the nested
child ran a full LLM session with a code-review briefing).

Fix: `isWorkerSubcommand` must scan argv for `-h`/`--help` (and print a usage head)
before running the loop. A `workerUsageHead` const should be added (Rule 1: named
constant) describing `GOPI_WORKER_MAX_ROUNDS`, the drain-exit contract, and the
`pa-queue` exclusion. Regression test: `TestWorkerHelpDoesNotRun` — invoke
`gopi worker -h` with a queued task and assert nothing is claimed.

Severity note: this is exactly the class of bug the epic's "isXSubcommand" registry
review is hunting; every other subcommand (memory, web, task, …) handles `-h`
correctly, `worker` is the sole offender.

### P0-2 — `-h` for `task sync/harvest/diff/push` prints the whole family usage, not per-verb help; `syncUsageHead` is DEAD TEXT

- `runTasksSubcommand` scans for `-h` and prints `tasksUsage` (the whole family blob)
> **Retired mechanism (2026-11-05).** The cross-node sync family this page
> describes — `gopi task sync`, `diff`, `harvest`, `push`, `send`, `receive`,
> `pull`, `sync-pass` — is DELETED. No verb in this binary opens a session
> against another node's ledger; each of those spellings still dispatches to a
> removal sentence, and work that must run on another host is FILED there with
> `gopi task submit`. The text below is the record of the mechanism as it was,
> kept verbatim because it is the evidence the removal is judged against.

  for ANY verb. `gopi task sync -h`, `gopi task harvest -h`, `gopi task diff -h`,
  `gopi task push -h` all print the same ~80-line family help and exit 0.
- `cli_tasks_sync.go:38` defines `syncUsageHead` — a detailed, up-to-date per-verb
  help covering `--full`, `--serve-remote` protocol, `--claim-remote`, `--push-only`,
  `--task-id`, `--tags` semantics — **but nothing ever references it** (verified:
  only its own definition + comment). It is dead code (likely orphaned when the
  family-usage refactor replaced per-verb heads).
- Consequence: the help surface (the ONLY place a user sees flag semantics) is
  missing `--full` entirely (M4.24's most important flag), and the `sync` line in
  `tasksUsage` omits `--claim-remote`/`--push-only`/`--task-id` from the flag list
  (they are mentioned in prose). The M4.24 `task diff` verb is documented NOWHERE in
  the live help (only in the dead `syncUsageHead`).

Fix: wire `syncUsageHead` into the `-h` path for the sync family (per-verb heads,
or at least append the sync-family section when the verb is sync/diff/harvest/push),
and add `task diff` + the M4.24 `--full` to `tasksUsage`.

### P1-1 — `syncUsageHead` flags `--serve-remote` etc. not in `tasksUsage`'s flag whitelist? (verified: they ARE) — no, see P0-2. (self-corrected during review)

---

## 1. `gopi task sync` — bidirectional delta session

Design: `SyncPeerSession` = one ssh round trip: push our delta (rows with
`updated_at > anchor`) as a v2 JSON patch on stdin to the peer's
`gopi task sync --serve-remote`; the peer merges it and returns ITS delta envelope
on stdout; we merge that locally; then BOTH anchors advance (ours keyed by the peer
domain). Anchors are forward-only (`syncStateCommit`), with a 5s backdate margin
(`syncAnchorMarginBackdate`) against clock skew. Terminal states never regress.
Equal-anchor conflicts are ruled by the server (completed + sync-resolved) — with the
M4.16/bug053 guard: a real human/llm ticket with payload is NEVER blind-flattened
(`syncRowIsMirrorNoise`).

Findings:
- **F-sync-1 (protocol versioning is sound)**: `syncCheckPatchVersion` gates every
  import channel (serve-remote, claim-remote, import-merge, delta-envelope,
  import-tasks) with strict got>want / got<want / got==0 semantics. A peer running an
  older binary refuses the newer patch with an actionable upgrade message. Good.
- **F-sync-2 (--tags partition is symmetric, rides the patch)**: `patch.TagsFilter`
  travels to the peer whose serve-remote mirrors the partition on ITS push.
  Verified in `syncPeerSession` + `SyncRemoteServe`. The `LIKE` gate uses comma-bounded
  exact tag match (no substring drift). Good.
- **F-sync-3 (P2) `--peer` + `--peers` both given → `--peer` silently wins**:
  `runTaskSync` line 144: `if opts.peer != "" { peers = []string{opts.peer} }`.
  No error, no warning. A user typing `--peers a,b --peer c` gets ONLY c. Should
  error or at least warn.
- **F-sync-4 (P2) `--status` + `--peer` is allowed and silently ignores the peer**:
  `--status` prints anchors for ALL peers from `--peers`/whitelist; a `--peer` given
  alongside is ignored (the parse gate rejects only `--status`+`--tags`). Minor.
- **F-sync-5 (P2) `--push-only`'s default "whole window" can be a large/empty patch**:
  `SyncTaskPushOnly("")` pushes the whole delta window to every peer — the window
  could be thousands of rows; there is no size guard or confirmation. Acceptable for
  a manual operator verb, but the serve tick's push reuses it (see F-sync-6).

## 2. `gopi task sync --serve-remote` / `--claim-remote` — peer-side handlers

- **F-serve-1 (P2) stdout discipline is enforced by convention only**: the comment
  says "prints NOTHING else on stdout" — `SyncRemoteServe` returns the envelope bytes
  and `runTaskSync` writes them + `\n`. `syncFatal` writes to stderr. Good, but the
  `printSyncSession` human lines from the INITIATOR's session never reach the peer
  (they are printed locally), so the envelope is safe. The one risk: any
  `fmt.Println` accidentally added in the serve-remote path would corrupt the
  protocol. A `//go:vet` or a test asserting `SyncRemoteServe` output parses as JSON
  would harden this (there IS an E2E sim: `syncSessionFromEnvelope`).
- **F-claim-1 (P1) claim-remote is the peer's ONLY way to learn a fresh claim** —
  but it only works for rows the peer already has (`row-not-found` = uncontested).
  bug052 documented this: dispatch's db-push must use `--serve-remote` (delta import
  creates rows), NOT `--claim-remote`. The two verbs' division of labor is correct
  and now documented in bug052. OK.
- **F-claim-2 (P2) claimLockMaxRows=8 guard**: parse guard, fine.
- **F-claim-3 (P2) self-exclusion guard** (`StartClaimLockBroadcast` filters peers
  equal to self) exists because of a real incident (HP listed itself). Good defensive
  code — but the guard compares BOTH `GOPI_SYNC_NODE` and hostname, and `--as`
  overrides could still name a peer... acceptable.

## 3. `gopi task push` (git channel, M4.18) + serve-tick git pass

Design: `gitSyncClientPass` (push local feat/* to server + pull master --ff-only) /
`gitSyncServerPass` (fetch + AdoptionSweep clean-merge/delete, conflicts dispatch a
Bond task). One shared engine, `GitPassReport` steps all fail-soft. Order: git first,
db second (deliverable branch must exist before rows migrate).

- **F-push-1 (P1) gitSyncErrLine is fragile**: `gitSyncErrLine` takes the LAST
  `": "` in the whole error string. git errors often contain multiple `": "` (path
  + message); the "one line is enough to diagnose" contract can truncate to a
  misleading tail. Cosmetic for ops but worth a targeted fix (split on the LAST
  newline, not `": "`).
- **F-push-2 (P2) `gopi task push` has NO `--db`-equivalent isolation knob**: the
  git pass operates on the cwd's repo + `GitSyncRepoRoot()`; a human running it in
  the wrong checkout pushes the WRONG repo's branches. The CLI doc says "topology is
  declared, never guessed" but the repo is guessed from cwd. `--repo` would be safer.
- **F-push-3 (P2) role resolution is implicit**: `IsServerNode()` = own hostname not
  in sync_peers. The prior review (memory #1182) flagged `IsServerNode` returns TRUE
  when sync_peers is EMPTY (no peers → server → finish-cleanup skips — fails safe).
  For the git pass specifically: an empty peer list → `GitSyncServerPass` skips with
  "no sync peers declared" (fail-soft), client pass skips with "no sync peer". OK.
- **F-push-4 (P2) gitSyncMaxBranches=20 cap silently truncates**: a repo with >20
  local branches pushes only 20 and reports the rest as never-tried (not failed).
  The `for-each-ref` ordering is alphabetical; the truncated remainder is silently
  dropped from `BranchesPushed` AND `PushFailed`. Should report "N more not pushed".

## 4. `gopi task harvest --peer [--since N] [--remote-db] [--no-push]`

Design: one-way PULL: ssh `sqlite3 -json` SELECT `updated_at > anchor`, apply in ONE
transaction (insert new tagged from-<peer>; same-id newer wins; terminal never
demoted; equal stamps must be byte-identical or conflict), anchor advances inside the
same commit (never past a failed row), THEN git push-back of local non-master
branches (unless --no-push).

- **F-harv-1 (P2) harvest and sync share the sync_state table but use DIFFERENT
  anchor semantics**: harvest writes `anchor` = max(updated_at) SEEN with a 5s rewind
  (`harvestAnchorRewind`); sync writes `anchor` = peer-domain delta end. Both keyed
  by `peer` in the same table. A node that harvests and syncs the same peer mixes
  the two anchor clocks — harvest's anchor is in the PEER's time domain (their
  updated_at), sync's too (peer domain), so they are compatible in principle, but a
  harvest after a sync session re-pulls the delta window (safe, idempotent). No
  correctness bug, but the double bookkeeping deserves a comment (it exists, in
  harvest's own comment). Low risk.
- **F-harv-2 (P2) `--since N` is a one-run override that does NOT save the anchor**:
  `TaskHarvest` skips `harvestAnchorSave` when `opts.Since != 0`. So `--since 0`
  re-pulls FULL history but leaves the stored anchor untouched — the NEXT plain
  harvest resumes from the old anchor, potentially re-pulling a huge window again.
  Intended? The doc says "--since overrides for one run" — the one-run semantics is
  explicit, but the asymmetry (override runs don't advance the anchor, even when they
  pull newer rows) means a `--since` run's progress is LOST. Worth documenting
  clearly or advancing the anchor to max(seen).
- **F-harv-3 (P2) harvest's six counters include `harvested` + `migrated` both
  meaning "inserted"** (new vs newer-wins); `duped` = idempotent already;
  `conflict` = equal-stamp differing; `skipped`/`failed`. The CLI line
  `harvested N rows: harvested=X migrated=Y ...` double-counts: `harvested+...` all
  six are summed into N and also reported individually. Cosmetic.
- **F-harv-4 (P2) exit code 2 on failed rows** (`counts.Failed > 0 → os.Exit(2)`):
  distinctive and documented. Good.

## 5. `gopi worker` (M4.20 loop)

Design: plain temporary process (no daemon/flock); parallelism = process count;
death is stateless (every durable fact in the ledger + sync channel). Round: claim
(`TaskNextFiltered` CAS) → execute via `runTaskRun` (reuses the whole single-runner
chain incl. claim broadcast + writeback) → must-succeed terminal push (inherited
from writeback) → `workerSyncPeers` pull-then-push per peer → next. Drained (with
one idle re-check) → exit 0. `GOPI_WORKER_MAX_ROUNDS` caps rounds.

- **F-wkr-1 (P0) `gopi worker -h` runs the loop — see P0-1 above.**
- **F-wkr-2 (P2) `workerRound` counts a YIELD as "nothing ran"** and the drained
  re-check (`workerIdleRecheckMS=1000ms`) then re-claims — a transient claim-refusal
  (e.g. a peer refusing the broadcast) briefly looks drained. The comment claims the
  re-check "distinguishes a real drain from a transient refusal", but it does not:
  `workerRound` returns false for BOTH empty and ceded/yielded; the re-check cannot
  tell them apart. A persistent-yield loop could spin 1s/round forever with
  `GOPI_WORKER_MAX_ROUNDS=0` (default unlimited). In practice a yield exits the
  process (runTaskRun's os.Exit on claim-lock yield), so the loop only sees
  `res.Empty` — the yield path is effectively unreachable inside the loop. Still, the
  comment overclaims; worth tightening.
- **F-wkr-3 (P2) workerSyncSettleDelayMS=250ms is a magic number** (Rule 1: it IS
  named, good — but its purpose is "belt-and-braces for a peer that just came up",
  i.e. a race the sync protocol should tolerate by design; the delay papers over it).
  Acceptable; noted.
- **F-wkr-4 (P2) no `--db`/`GOPI_MEMORY_DB` override on `gopi worker`**: the loop
  uses the default memory.db path. For sandbox E2E the docs say to use `task run`
  with the skip env; a worker in an isolated GOPI_HOME works (GOPI_HOME redirects
  the DB). OK by design.
- **F-wkr-5 (P2) worker parallelism is process count, but two workers on the SAME
  node share one queue**: the CAS makes double-run impossible (verified by the 2-worker
  E2E). The claim BROADCAST, however, is per-process: worker A's broadcast claims the
  peer lock; worker B's claim of the NEXT row broadcasts independently — fine. No
  shared-state hazard. Good.
- **F-wkr-6 (P2) `workerExcludedTags = "pa-queue"`** — a FIXED list, hard-coded
  string, while `serve --skip-tag` takes a user list. The worker's contract is
  documented, but the fixed list means a new protected tag requires a code change.
  Acceptable (documented), noted as design choice.

## 6. `gopi task diff --peer` (M4.24) and `sync --full`

Design: diff = read-only set-compare of both ledgers' full (task_id,status) over the
harvest sqlite3 channel; exit 1 on difference; names the repair (`--full`). full =
whole-ledger replay (AfterSource=0 both sides), idempotent via rank merge; anchors
advance to the proven end.

- **F-diff-1 (P2) diff is undocumented in the live help** (see P0-2): `task diff` is
  absent from `tasksUsage` and the want-list error message ("want list|show|update|
  search|submit|run|serve|archive|ingest|...|merge" — `diff` is missing entirely,
  while the RETIRED `ingest` is still listed). A user hitting a typo gets a list that
  doesn't include a valid verb.
- **F-diff-2 (P2) diff's ssh channel is `sqlite3 -json` on the peer** — a NEW
  external dependency the peer must have (sqlite3 CLI). Harvest already uses it, so
  the peer node must already satisfy it. Consistent; noted.
- **F-full-1 (P2) `--full`'s anchor advance is UNCONDITIONAL** (`syncPeerSessionFull`
  commits directly, bypassing the pending-count guard): the pass itself IS the
  reconciliation, so this is by design. The residual: rows imported by THIS node
  (from-<self> tag) never re-export even in a full pass (anti-reflow gate residue,
  documented in `sync-reconcile.md`); diff reports them as missing-on-peer and the
  peer's own full pass repairs. Known, documented.
- **F-full-2 (P1) the serve-tick full counter advances on the counter BEFORE the
  session runs** (`syncTickShouldRunFull` is called before `SyncFullReconcile`/`SyncPeerSession`),
  but the comment says "the counter advances only on SUCCESSFUL delta sessions (a
  failed session must not burn the count)". Reading `syncTickShouldRunFull`: it
  increments the stored counter and returns whether to run full; on a FAILED session
  the counter has ALREADY been saved incremented. The comment is misleading — a
  failed session DOES burn the count. Verify: `syncTickShouldRunFull` loads count+1,
  saves it, returns full-flag. If the session then fails, the incremented count is
  persisted. So the "must not burn the count" intent is NOT implemented. **This is a
  real bug in the guard's intent** (minor severity: the periodic full just runs one
  session early after a failure — self-corrects, but the stated contract is violated).

## 7. ssh-passthrough argv robustness (`isTasksSubcommand`)

Handles three shapes: (a) plain `task <verb>`; (b) forwarded full ssh argv
(`-o BatchMode=yes bnode "gopi task sync --serve-remote"`) — skips `-o X` pairs and
lone flags, then takes first non-flag as host and re-splits the remote command,
finding the `task` token; (c) the login-shell single-quoted form (`ssh host "gopi
task sync --serve-remote"` arrives as ONE argv element) — unquotes via `Fields` and
retries.

- **F-ssh-1 (P1) `strings.Fields` re-splits the remote command and CAN MANGLE
  quoted arguments**: case (b) does `strings.Fields(strings.Join(rest[1:], " "))` —
  a remote command containing a quoted string with spaces (e.g.
  `gopi task submit "a task with spaces"`) is split on the spaces, so the payload
  arg breaks into multiple words. For the sync family the remote commands are
  flag-only (`sync --serve-remote`, `claim-remote`), so today it is safe — but the
  passthrough is generic across the whole task family, and `task submit "…"` /
  `task search "two words"` over ssh would be mangled. The case-(c) path has the same
  `Fields` issue. **Low-probability today (only flag-only remote verbs are used), but
  the re-split is lossy by construction** — a `sync --peer "my host"` (alias with a
  space, unusual) would break. Worth a comment or a shell-aware split.
- **F-ssh-2 (P2) case (b) `-h`/`--help` handling**: a forwarded `ssh host "gopi task
  -h"` → the passthrough finds `task` and calls `runTasksSubcommand` with `-h` →
  family usage printed. Fine.
- **F-ssh-3 (P2) the passthrough retries BOTH shapes (b) and (c) unconditionally**:
  if shape (b) finds no `task` token it falls through to (c) on `args` (the ORIGINAL
  argv), not on `rest`. For an option-carrying argv (`-o X host cmd`), shape (c)
  checks `len(args)==1` — false, so it is skipped. Correct, no double-match risk.

## 8. Rule 1 / Rule 2 / magic-number audit

- Rule 1 (named constants) is consistently applied across all four CLI files and the
  engines: `syncSSHTimeout`, `syncAnchorMarginBackdate`, `harvestAnchorRewind`,
  `claimLockBackoffCap`, `terminalPushRetries`, `gitSyncTimeout`, `gitSyncMaxBranches`,
  `workerIdleRecheckMS`, `workerSyncSettleDelayMS`, `syncFullEveryDefault`,
  `reconSSHTimeout`, etc. Excellent compliance.
- Rule 2 (single source of truth): `GOPI_SYNC_INTERVAL` read in exactly
  `syncIntervalFromEnv`; `GOPI_WORKER_MAX_ROUNDS` in `workerMaxRounds`;
  `GOPI_SYNC_FULL_EVERY` in `SyncFullEvery`; `GOPI_SYNC_GIT_INTERVAL` in
  `serveGitSyncInterval`; `GOPI_MERGE_AUDIT_INTERVAL` in `mergeAuditIntervalFromEnv`.
  One exception: `workerExcludedTags` string is duplicated conceptually with serve's
  `--skip-tag` (fixed vs user list) — documented, not a Rule-2 violation.
- **F-rule-2-1 (P2) `serveGitSyncInterval` FAILS SILENTLY on a malformed env value**
  (returns 0 = disabled) while `syncIntervalFromEnv`/`mergeAuditIntervalFromEnv`
  treat a malformed value as an ERROR. The git-interval reader's comment even says
  "a typo disables rather than misfires — the adoption interval's contract", i.e.
  the three readers DISAGREE on the malformed-value contract (error vs disable).
  Inconsistent error philosophy across sibling env readers — worth unifying.

## 9. Protocol / dedup / idempotence notes (positive)

- Import is idempotent by construction: `syncDeltaMergeRow` treats equal
  status+tags as `already` (no-op); rank-merge dedups re-sends; terminal never
  regresses; `RowsAlready` counts make re-runs free. Verified in the delta tests
  (`TestBidirEpic4IdempotentRoundTrip` etc.).
- The M4.16/bug053 fix (real tickets never blind-flattened) is the most important
  semantic correction in this family — the server ruling now checks
  `syncRowIsMirrorNoise` and raises a `ledger-conflict-human-task` alarm instead of
  fake-closing work. This directly addresses the bug050/051/052 incident chain.
- `--full` reconciliation + `task diff` + the anchor guard form a coherent
  "reconciliation over increments" story; the guard's pending-count check is cheap
  and fail-safe (refuses to advance, tells the operator to run diff/--full).

---

## Prioritized findings

### P0 (fix now)
1. **`gopi worker -h` runs the worker loop and executes tasks** — `isWorkerSubcommand`
   must handle `-h`/`--help` before `runTaskWorker`. Live incident: claimed and failed
   a sibling epic task (t-1788305178) during this review.
2. **`syncUsageHead` is dead code; `task sync/harvest/diff/push -h` print only the
   family blob; `task diff` is absent from the live help AND from the "unknown
   subcommand" want-list (which still lists the retired `ingest`).** Wire the
   per-verb help, add `diff`/`--full` to `tasksUsage`, fix the want-list.

### P1 (should fix)
3. **Serve-tick full-counter contract violated**: `syncTickShouldRunFull` persists the
   incremented counter BEFORE the session runs; a failed session still "burns the
   count" — contradicts the stated intent ("a failed session must not burn the
   count"). Self-corrects over time, but the guard's contract is mis-implemented.
4. **ssh-passthrough `strings.Fields` re-split is lossy for quoted remote args** —
   safe for today's flag-only remote verbs, but `task submit "…"` over ssh would
   mangle. Document or shell-aware split.
5. **`gitSyncErrLine` truncates to the LAST `": "`** — can produce a misleading tail;
   split on the last newline instead.

### P2 (nice-to-have)
6. `--peer` + `--peers` silently prefers `--peer`; `--status` + `--peer` ignores the
   peer. Error or warn.
7. Harvest `--since` override runs don't advance the stored anchor (progress lost on
   the next plain harvest); document or advance to max(seen).
8. `gitSyncMaxBranches` silently truncates the push list (>20 branches); report the
   skipped remainder.
9. `gopi task push` guesses the repo from cwd — add `--repo`.
10. `serveGitSyncInterval` fails silent on malformed env while sibling readers error —
    unify the contract.
11. Worker round-counting comment overclaims (yield vs empty distinction); tighten.
12. `harvest` and `sync` share `sync_state` with different anchor rewrite rules —
    add an explicit cross-reference comment (partially present).

---

## Verification
- Build: `go build -o bin/gopi ./cmd/agent/` — OK (in the review worktree).
- Usage-head inspection: `gopi task sync -h` / `harvest -h` / `diff -h` / `push -h`
  print the family blob; `gopi worker -h` RUNS the loop (P0-1 evidence).
- Source review: all files in scope read in full.
- Docs: `bug052-dispatch-dbsync.md`, `bug053-m416-human-ruling.md`,
  `m420-worker-loop.md`, `sync-reconcile.md` reviewed; GOPI.md has no sync-family
  documentation (only the historical-incident line 167).
- Git history: `git log` on the sync/worker files shows the design evolution
  M4.13 delta → M4.18 claim broadcast → M4.20 worker loop → M4.24 reconcile;
  `git_blame` on `syncUsageHead` shows it introduced in b3b71b6 (M4.13) and extended
  in 0bc02c0 (M4.16) — then orphaned.
- No tests were run beyond the build (review task = docs only; the delta/worker
  suites are covered by prior reviews, memory #1182).

## Incidental incident log (for the epic)
During this review, running `gopi worker -h` (to capture its usage head) side-effected
the live ledger: it claimed and executed `t-1788305178` (sibling epic task
config/options review), the nested gopi was killed by the bash timeout, and the row
was left `failed` with executor `4d410346-248e-46ae-ac7f-be26b67897b8`. The task's
work itself was not corrupted (it was already in flight on another process; its row
may need a manual reopen). This is P0-1's live proof.
