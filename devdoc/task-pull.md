<!-- doc-class: historical -->
# `gopi task pull` — the on-demand peer→local row fetch

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Task:** `01a1153380fc764885ad190532917ccc` (epic *task-pull*), host `hostb-Laptop`,
branch `task/01a1153380fc764885ad190532917ccc`, base master `93848040e`.

## What this is

The RECEIVE leg the operator asked for: a row filed on a loaded peer is brought to
an idle node **on demand**, by an operator-typed verb, with a per-row report.

The investigation that shaped the work is worth recording, because it removed most
of the writing: **the pull half already existed in the repo.** `gopi task receive`
(`SyncRowReceive`, `internal/tools/memory_sync_row.go`) is the mirror of
`gopi task send`: it ssh'es to the peer, runs a ONE-row `SELECT` (`sqlite3 -json`),
and feeds the result into `SyncTaskImportDelta` — the one merge authority the whole
family uses. It does not move the sync anchor and does not push branches. So this
epic built **no engine**; it closed named gaps, added the operator's own spelling
over the same leg, and proved the leg end-to-end with a real HP←peerbox run.

## Completed

1. **The `pull` face** (`internal/taskverbs` + `cmd/agent/cli_tasks.go`,
   `cmd/agent/cli_tasks_sync.go`). `gopi task pull --peer <alias> --task-id T[,T2…]`,
   dispatched through the family's derived surface (`SyncVerbs` → `DispatchVerbs`),
   documented on the EXISTING `gopi task sync -h` page (no new help page), with
   `pullOnly` as its own parse/dispatch case. Every id is one call to
   `tools.SyncRowReceive` — one implementation, two spellings, so there is no second
   reading of the merge and no second anchor decision.
2. **Per-row outcome** (`tools.SyncRowPullLine`, `internal/tools/memory_sync_row.go`).
   The report projects the import's own receipt and prints ITS verdict word:
   `stored` / `already current` / `migrated` / `REFUSED (reason)`, plus `FAILED (reason)`
   when the leg itself could not land the row. Counts still print on the `receive`
   face; the pull face answers the operator's actual question — *what did this ledger
   do with that row* — one line per row.
3. **Exit status decided by EFFECT** (`cmd/agent/cli_tasks_sync.go` `runTaskPull`).
   A row that did not land (unknown on the peer / refused by the import / unreachable
   peer) makes the command exit 1 even when the transport answered. A script can tell
   "the row is here" from "the command ran".
4. **The pre-contact topology gate on the receive leg** (`SyncRowReceive`). This was a
   real gap, not a nicety: `send`, `diff`, `harvest`, `sync` and the reconcile readers
   all call `RequireDeclaredPeer`, and the receive leg — the one peer-taking face in
   the family — did not. It now refuses an undeclared alias BY NAME **before** the
   fetch closure runs, so a typo'd alias cannot dial.
5. **List selector with named refusals** (`syncRowPullIDs`): empty list, empty member
   (`t-a,,t-b`), a repeated id, and no selector at all are each refused by name with
   the re-runnable excerpt. The list is walked in the operator's own order and a
   failed row does not abort the rest — the per-row line already says which row and why.
6. **`--claim` refused by name with its reason** (see `## Not Done`).
7. **Tests** — `cmd/agent/cli_tasks_pull_test.go` (round trip, idempotence, a local row
   left alone, an unknown id, an undeclared peer with a *measured* zero-contact
   assertion, the bad-selector table, the surface/parse/help arms) and
   `TestRowReceiveRefusesAnUndeclaredPeerBeforeContact` in
   `internal/tools/memory_sync_row_test.go` (the tool-layer half of the same gate,
   counting fetch calls). The surface rig gained a row-answer arm so a pull's remote
   SELECT is stood in for while the LOCAL half — the import — stays real.

## Not Done (honest)

- **`--claim` is NOT implemented.** It is admitted by the parser and then refused with
  a sentence that names why and what exists instead. The reason is not "no time": it
  is a **contract conflict in the brief**. Item 5 defines the aim ("so it is not
  claimed twice") and then defines the precondition from the other side — *"`--claim`
  is OPT-IN and explicit. Without it, a pulled row arrives as it is (typically
  `pending`) and THIS node's ordinary dispatcher may claim it on its own schedule"*.
  A claimed row on HP is **not claimable by the local dispatcher at all**
  (`taskRowClaimable`), so a pull that claimed the row would hand the operator the
  precise loss the brief's own default case exists to avoid: fetched, and then only a
  manual release could run it. The family's primitive for moving a row's ownership is
  different and already built — the atomic claim round (`--claim-remote`, a
  synchronous exchange an initiator drives over ssh) or the send-side hand-off
  (`gopi task send --cancel-source`, which cancels the SOURCE row only after the
  peer's ledger has persisted it). Claiming on the pulling side would also leave the
  ORIGIN ledger still `pending`, where its own dispatcher would run the same work.
  **What safe implementation would need**, stated so it is dispatchable: a pull that
  claims must (a) decide the row's ownership atomically against the origin ledger —
  the claim broadcast protocol, not a local write — and (b) resolve what the origin
  row becomes (the send leg's `--cancel-source` hand-off is the existing answer). That
  is a different act from a pull and it is its own change.
- The **round trip in the tests** stands in for the peer's `sqlite3` (a single host
  cannot run the peer's SELECT), so the tests prove the local half plus the wire
  shape, not the peer's SQL. The **real peer leg was exercised live** (§Verification),
  which is what closes that gap.
- **Five reds in the scoped suite are PRE-EXISTING at this branch's base
  `93848040e` — measured, not guessed, and NOT caused by this diff.** They are
  reported here because a suite result is not evidence if it is quoted
  selectively. Each involves a file this diff does not touch
  (`git diff --stat 93848040e HEAD -- <file>` is empty for every one of them):
  - `internal/standards TestCommandsDocGeneratedBlockMatchesSource` +
    `TestCommandsDocGateRedProofStaleGeneratedBlock` — `COMMANDS.md`'s generated
    verb index still points `gopi checkpoint` at `cli_checkpoint.go:167` while the
    source has it at `:185`; the last commits to touch either file (`406e24860`,
    `3540c11be`) are both `merge-base --is-ancestor` of the BASE, so the staleness
    predates this branch. (Repair is the doc's own one-liner:
    `go run internal/standards/testdata/commands-census.go . COMMANDS.md`.)
  - `internal/standards TestStandardGates/TestGate5AccessorPrefixRedProof` —
    `cli_tool_spelling_table.go:170 isUnderscoreException` is an unbaselined
    Rule-20 hit; that line is byte-identical to BASE (its commit `6685f7f83` is an
    ancestor of BASE).
  - `cmd/agent TestCLINamingExceptionsAreStillDispatchable` — `web_search` /
    `web_fetch` have no contract-inventory entry; same file, same antecedent
    commit.
  - `cmd/agent TestPrefetchBusyIsRetriedWithinTheSameBudget` — a wall-clock
    timing assertion ("prefetch gave up after 1500ms … context deadline
    exceeded") that fails under this host's load (load avg 15–32 on 8 cores during
    this run); no file in the path is touched by this diff.
  - `cmd/agent TestChatCLIVocabularyIsClosedEveryAcceptedFlagParses/--tools`
    (second run only) — the fixture's own fd-lifecycle guard (`chat_argv_test.go`,
    byte-identical to base) reports its stderr pipe's fd released by another path;
    a test-harness descriptor race, not a product path.
  Any one of these is a task of its own, not a passenger on a `task pull`
  branch.
- The `--claim` **refusal text** is asserted by `TestTaskPullRefusesClaimByName`
  (`cmd/agent/cli_tasks_pull_test.go`): the flag is admitted, refused non-zero, both
  alternatives are named in the sentence, and the peer is provably not dialled (the
  shim's counter stays 0). Its siblings are covered by the selector table.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK; `go vet ./cmd/agent/
  ./internal/tools/ ./internal/taskverbs/` → clean.
- Tests: `make test-budget PKGS='./internal/tools ./internal/taskverbs ./cmd/agent
  ./internal/standards'` → (see the run recorded in the task's session artifacts;
  scoped to the packages the diff touched, never a whole-tree suite).
  Focused: `go test -run 'TestTaskPull' ./cmd/agent/` → ok (11.0s).
- Ran (Rule 31 — the real surface, real arguments, real state):
  `GOPI_HOME=<sandbox> gopi task pull --peer peerbox --task-id 01a11554-b3b1-731e-8425-2ce4986786f9`
  where that row exists ONLY on peerbox (`peerbox` is the declared peer in
  `settings.json sync_peers`).
- Result, the sequence observed (full transcript:
  `~/.gopi/sessions/01a11533-80fc-7648-85ad-190532917ccc/artifacts/pull-e2e-real-run.txt`):
  ① `gopi task show <id>` → `no task with task_id=…`, exit 1 (HP does not have it);
  ② the pull → `pull peerbox: stored (row 01a11554-b3b1-731e-8425-2ce4986786f9, status=completed)`, exit 0;
  ③ `gopi task show <id>` → **the row, in full**, exit 0 — the presence is the evidence;
  ④ re-pull → `already current`, exit 0 (idempotent);
  ⑤ an id on neither side → `pull peerbox: FAILED (row t-no-such-row-anywhere): … not found in the peer peerbox ledger`, exit 1;
  ⑥ an undeclared alias → `peer "no-such-peer-alias" is not in the sync_peers whitelist … sync topology is declared, never discovered`, exit 1;
  ⑦ an option-shaped alias → `invalid peer alias "-oProxyCommand=x": …`, exit 1;
  ⑧ a list (one present, one absent) → two outcome lines, `1 of 2 row(s) did not land here`, exit 1;
  ⑨ `--claim` → the refusal sentence + `gopi task send --cancel-source` named, exit 1.
- Commit: see the branch tip (a commit hash is the framework's to report, not this
  document's).

## Evidence (self-filed)

- `TestTaskPullRefusesAnUndeclaredPeerBeforeAnyContact` — the zero-contact assertion is a
  counter the shim writes on every leg; the test fails if it is not 0. (Mixed in a
  documented deviation: mix output is checked after a brief idle rather than by log order.)
- `TestRowReceiveRefusesAnUndeclaredPeerBeforeContact` — the tools-layer counter
  (`calls`) is asserted `!= 0` → red, `== 0` → green.
- `TestTaskPullRefusesClaimByName` — the `--claim` sentence names both existing
  alternatives (`sync --claim-remote`, `send --cancel-source`) and the shim counter
  is 0, so the refusal is at the parser and never reaches a peer.
- Rule 30: `git status --porcelain` prints nothing in this worktree at hand-over (the
  evidence files live under the session dir, never in the tree).

## Notes for the next reader

- `task receive` and `task pull` are the SAME leg. Do not "fix" the duplication by
  deleting one: `receive` is the git-free single-row spelling the repo's tests and
  scripts already use; `pull` is the operator-facing list spelling with the outcome
  report. Both dispatch to `SyncRowReceive`.
- The pull moves NO status and NO anchor. A row authored here is never disturbed by a
  peer's older copy (asserted by `TestTaskPullLeavesALocalRowAlone`).
- The surface rig's row-answer arm matches the remote SELECT on the space-free mark
  `task_id` — a `case` pattern is word-split by the shell, and a mark carrying a space
  makes the shim die with `Syntax error` + `exit status 2`, which reads like a peer
  fault rather than a rig fault (measured in this epic's first red run).
