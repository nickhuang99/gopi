<!-- doc-class: historical -->
# Task Report: decouple the CODE-DELIVERY faces from the task-ROW switch

Epic: split the last two conflated axes in the serve tick — `serveGitSyncSweep`
and `serveAdoptionSweep` were gated by `config.TaskSyncEnabled()` (the
task-ROW switch, default OFF), so a node with a declared peer and the row switch
off **never pushed its finished branches and never adopted them**, while a
single task's close-out push-back (`finishCleanupServerTarget`) had always
delivered because it never read the switch. One declared peer must mean ONE
"code delivery is on" state across all three faces; row exchange stays
independently OFF; `sync_peers: []` stays a pure no-op on every face.

## Completed

- **`cmd/agent/cli_tasks_sync_git.go`** — `serveGitSyncSweep` no longer reads
  `config.TaskSyncEnabled()`. Its precondition is now `config.HasSyncPeers()`
  (checked first, before the throttle clock) + the role arm (`tools.IsServerNode`)
  further down, exactly as the close-out push-back decides it. Doc comment
  rewritten to state the code-delivery axis rather than repeat the row-switch
  rationale.
- **`cmd/agent/cli_tasks_adopt.go`** — `serveAdoptionSweep` no longer reads the
  switch and **gains** the declared-peer short-circuit it did not have
  (`!config.HasSyncPeers()` → return before the clock; the briefing's measured
  discrepancy was correct: only the git sweep carried a peer gate).
- **`internal/config/task_sync_switch.go`** — the switch's own header now says
  what it does not gate (all three delivery faces) and calls its remaining
  subjects ROW crossings.
- **`cmd/agent/cli_tasks_sync_pass.go`** — the one-shot face prints a second
  header line (`codeDeliveryWord()`), stating the delivery axis state and the
  role. Reason: the summary line said "task-row exchange OFF" while the pass was
  in fact dialling the peer, which is exactly the misreading that hid the
  delivery half of the pass. Usage page rewritten around the two axes.
- **A real defect found by running the surface (Rule 31), and fixed with it**:
  the pass drove the row delta leg through `serveGitSyncSweep`'s TAIL, whose
  `deltaSyncOn()` early return is only correct for a LIVE tick (there the loop
  body runs the row tick right after). On the one-shot face it **inverted the
  row axis** — measured, shape C (peer + row switch ON) exchanged ZERO rows
  despite the switch being on. The face now calls `runTaskSyncTick()` itself,
  once, after the two delivery legs. A delivery leg must never own the row axis.
- **Regression tests** (deliverable 3):
  - `cmd/agent/task_sync_switch_test.go` — the two former "off-is-no-op" cases
    are replaced by: `…RunsWithTaskSyncOffWhenPeerDeclared` (RED before the fix)
    for BOTH faces, plus `…NoPeerIsPureNoOp` / `…NoPeerIsPureNoOpUnderTaskSyncOn`
    (the single-node half), plus the pre-existing ON controls kept as controls.
  - `cmd/agent/task_sync_pass_test.go` — `TestTaskSyncPassWithPeerStillDeliversWhileRowsStayOff`
    (peer + switch off ⇒ the git pass dials the peer, no row leg),
    `TestTaskSyncPassNoPeerIsPureNoOp` (no peer + switch ON ⇒ no `[serve]` leg
    line at all), and the ON control extended to assert the ROW leg really runs.
  - `cmd/agent/cli_tasks_adopt_test.go` — the throttle/dirty-tree cases now
    declare a peer, since the declared topology became their precondition.
- **`devdoc/task-row-sync-switch.md`** — the row-crossing table is now
  row-only, with a new section for the three delivery faces and their
  preconditions, plus the measured 3-shape matrix and the delta-leg defect.
- **`CONFIG.md`** — the `task_sync` catalog row no longer claims the serve git
  pass/adoption as switch subjects, and its line reference follows the moved
  reader (`internal/config/task_sync_switch.go:87`).
- **`scripts/evidence-code-delivery-axis.sh`** — the checked-in harness that
  produced the matrix (3 declared topologies, real `gopi task sync-pass --once`,
  hermetic `GOPI_SETTINGS`, scratch root asserted off the `/` partition).

## The measured matrix (3 shapes × 4 faces, real binary)

`GOPI_SETTINGS=<fixture> GOPI_SYNC_GIT_INTERVAL=1 GOPI_SYNC_INTERVAL=1
GOPI_ADOPTION_INTERVAL=1 GOPI_SYNC_NODE=axis-test-self ./bin/gopi task
sync-pass --once`, inside a throwaway repo, peer alias `axis-test-peer.invalid`
(cannot resolve — a leg that never dialled cannot name the host):

| shape | topology | branches delivered (git/ref pass) | branches adopted (adoption sweep) | rows exchanged (delta) |
|---|---|---|---|---|
| A | peer declared, `task_sync` **OFF** | RUNS — 3 dialling line(s) naming the peer | RUNS — `[serve] [adopt] scanned=1 adopted=0 …` | **0** |
| B | no peer, `task_sync` ON | no-op — 0 lines | no-op — 0 lines | **0** |
| C | peer declared, `task_sync` ON | RUNS — 3 lines | RUNS — 1 line | 1 line (`[serve] sync <peer>: memsync sync: ssh … exit status 255`) |

Shape A is the epic: delivery runs while rows stay put. Shape B is the
single-node no-op on every face. Shape C is the control proving the fixtures are
reachable at all. Full raw output: reproduced by
`bash scripts/evidence-code-delivery-axis.sh`.

## Not Done

- **The resident merger daemon still does not exist.**
  `mergerSerialFaceLegs` (which calls both sweeps) remains reachable only from
  `gopi merger --once`; there is no live loop running these passes every tick.
  This branch changes the GATES, not the wiring — the epic's own scope. The
  passes are executable and observable today only through `gopi task sync-pass
  --once`.
- **A real second host was not driven.** The delivery legs were proven to RUN,
  dial and report; the end-to-end landing of a delivered branch on a real peerbox
  was not re-measured in this session (a peer alias that cannot resolve is the
  deliberate hermetic choice). Item ④ of the acceptance ("branches delivered?")
  is therefore measured as "the delivery leg dialled the peer with the branch
  push argv" plus "the adoption sweep scanned", not as a ref that moved on a
  remote host.
- **`mergeDeadHolderReclaimLeg` / `mergerPassOnce`** were not audited for other
  switch couplings — out of this epic's stated boundary.

## Verification

- Build: `go build ./...` → exit 0 (no output); `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK (the binary the measured runs used).
- Vet: `go vet ./...` → exit 0, no output.
- Tests (branch-scoped smoke; the whole tree belongs to the off-peak heavy path):
  - `go test ./cmd/agent/ -run 'TestServeGitSyncSweep|TestServeAdoptionSweep|TestServeAdoptionThrottle|TestServeAdoptionDirtyTreeSkip|TestTaskSyncPass|TestClaimBroadcastVerdict|TestLedgerAgreementRunsWithTaskSyncOff|TestServeGitSyncInterval|TestServeAdoptionInterval' -count=1` → `ok github.com/nick/gopi/cmd/agent`
  - `go test ./cmd/agent/ -run TestTaskSyncPass -count=1` → `ok … 6.899s`
  - `go test ./internal/standards/ -run TestConfigDoc -count=1` (worktree copy of CONFIG.md + devdoc) → `ok`
  - `go test ./internal/standards/ -count=1` → FAIL, **pre-existing on the base tree** (verified by running the same command in a detached worktree at HEAD): `devdoc/handoff-20260922-bill-analysis.md`, `devdoc/handoff-20260922-0503.md`, `devdoc/session-budget-prompt.md` are classed `historical` without their banner. Not mine to fix here; the count of mislabelled rows and the 735 bannered docs are byte-identical base vs branch.
  - `go test ./internal/agent/ -run TestDocAnchors -count=1` → FAIL, **identical before and after** (measured in a detached worktree at HEAD: same "2138 across 455 normative docs … 12 new, 4 stale permits", same stale baseline entries). My edits to `devdoc/task-row-sync-switch.md` added no anchor and broke no anchor.
  - `go test ./internal/smokegate/ -count=1` → `ok`.
- Ran (Rule 31 — the real surface, three shapes):
  `GOPI_SETTINGS=<peer-off.json> GOPI_SYNC_GIT_INTERVAL=1 GOPI_SYNC_INTERVAL=1 GOPI_ADOPTION_INTERVAL=1 GOPI_SYNC_NODE=axis-test-self ./bin/gopi task sync-pass --once`
  Result (shape A): `gopi task sync-pass: code delivery ON — role client …`; `[serve] [git-client] push: 1 branch(es) failed: … ssh: Could not resolve hostname axis-test-peer.invalid …`; `[serve] [adopt] scanned=1 adopted=0 ref-kept=0 dispatched=0 duped=0 skipped=1 failed=0`; no `[serve] sync` line; exit 0.
  Shape B (no peer, switch ON): `code delivery OFF (no sync peer declared …)` and NO `[serve] ` line at all; exit 0.
- Ran (acceptance (c) — the ROW paths still honour the switch, live binary, peer declared):
  - switch OFF → `gopi task submit "axis evidence row"` wrote the row locally: `Task queued: 01a0c610-… (status=pending, source=human, tags=—)`.
  - same fixture with `task_sync: true` → `task_submit: REFUSED — this row was NOT written to any local ledger. … this node declares role=client (a mirror leg) but no authoritative peer is configured (sync_to …)`.
  The pair is the A/B: the switch still decides the row path, and nothing else.
- Red-then-green (acceptance (d)): the new cases were run in a detached worktree at the pre-fix HEAD with the new test files copied in —
  `TestTaskSyncPassWithPeerStillDeliversWhileRowsStayOff` FAIL ("with a peer declared the delivery axis must report ON; got: … every leg is a no-op"),
  `TestServeGitSyncSweepRunsWithTaskSyncOffWhenPeerDeclared` FAIL ("did not run with the row switch OFF and a peer declared"),
  `TestServeAdoptionSweepRunsWithTaskSyncOffWhenPeerDeclared` FAIL,
  `TestServeAdoptionSweepNoPeerIsPureNoOp` FAIL (pre-fix the sweep had no peer gate at all). Same cases green on the branch.
- Commit: `0aeeda91 feat(sync): decide the code-delivery passes by the declared
  topology, not the task-row switch` (12 files, +648/−144).
- Deliverable: branch `feat/code-delivery-axis-split`.
  Recover any file with `git show feat/code-delivery-axis-split:<path>`;
  the branch lives in the shared repo (worktree), so no push is needed.
  Worktree: `/home/user/workspace/gopi-codedeliv-split`.

## Evidence (self-filed)

- `go build ./...` → exit 0; `go vet ./...` → exit 0 (both printed nothing).
- Matrix lines, shape A: `[serve] [git-client] push: 1 branch(es) failed: server-master fetch: git fetch --force --no-tags axis-test-peer.invalid:~/work/GoPi … ssh: Could not resolve hostname axis-test-peer.invalid` and `[serve] [adopt] scanned=1 …` and `grep -c '[serve] sync' A.log` = `0`.
- Shape B: `grep -c '[serve] ' B.log` = `0`; the whole log is the two header lines plus `pass done`.
- Shape C (post-fix): `grep -c '[serve] sync' C.log` = `1`.
- Row-path A/B: `Task queued: 01a0c610-1cd2-77b3-a49d-c846976998a1` (switch OFF) vs `task_submit: REFUSED — this row was NOT written to any local ledger` (switch ON).
- Pre-existing gate failures reproduced on the base: `go test ./internal/standards/ -count=1` in a detached worktree at HEAD → same three `historical`-without-banner docs; `go test ./internal/agent/ -run TestDocAnchors` → identical summary line `2138 across 455 normative docs … 12 new, 4 stale permits`.
- Rule 22: the evidence harness refused to run until it measured its scratch root off the `/` partition (`scratch: /home/user/workspace/tmp/…  (device 2052 vs / 2051)`); no load was generated (Rule 27) — a handful of tiny commits in a throwaway repo.
