<!-- doc-class: historical -->
# Task Report: 全库二次 review — 102-commit window 632c4c4..229387e (master)

Branch: `feat/review-suite-2` · Base: master 229387e · Date: 2026-08-31
Scope: every commit and file in `632c4c4..229387e` — 102 commits, 210 files,
+20 736/−347 lines (excluding text2img-eval fixtures). Prior review 7ab2fa7
covered 632c4c4..bd838bf sync/error/task-queue; this pass re-verifies those
findings against HEAD and extends coverage to the domains it deferred:
bash/dangerous-guard, parallel-tools, crash-dump, zombie/stale, finish-cleanup,
adoption chain, node-role, memory-procedural, brew, effort-values, and the
three UNMERGED branches (`feat/claim-broadcast`, `feat/branch-adoption-chain`,
`feat/node-role-banner`, `fix/memsync-import-atomic`).

## Completed

### 1. Full-suite verification on HEAD (229387e)
- `go build ./...` OK; `go vet` (tools/deepseek/agent/cmd) OK.
- `go test ./internal/tools/ ./internal/deepseek/ ./internal/agent/ ./internal/config/ ./cmd/agent/`
  → **all ok** (560s wall: tools 551.8s, agent 87.9s, cmd 99.7s, deepseek+config <1s).
- `-race` spot check on the concurrent-claim path
  (`TestTaskNextConcurrentClaimRace`) → ok 5.5s.
- 448 test funcs in internal/tools alone; bash family 28 (incl. idle-kill
  under unlimited, escape hatch, cwd whitelist); bidir2 E2E 4/4.

### 2. Re-verification of prior findings 7ab2fa7 (against HEAD)
| Prior | Status at 229387e |
|---|---|
| F1 IsServerNode asymmetric default | **still open, now higher impact** → N1 below |
| F2 error_1214.go dead 0% coverage | **still open** → N2 below (now fully caller-less) |
| F3 syncSSH stderr dropped | fixed by 53a965b: stderr now surfaces in the error (`ssh %s: %v (stderr: %s)`) |
| F4 internal/tools 589s slowest | unchanged (hygiene note; tests are E2E-heavy by design) |

### 3. New findings (all evidence-verified; file:line at HEAD)

**N1 — IsServerNode defaults to SERVER for unlabeled hosts (medium).**
`memory_sync_delta.go:394` returns true whenever the hostname is not in the
peer whitelist. Ambient settings here: sync_peers=[hp,peerbox], host=peerbox →
client (correct today). But any third machine — or this host renamed — silently
becomes "the server": equal-anchor conflicts auto-rule `completed`
(`syncDeltaServerResolve`), and finish-cleanup's client gate
(`task_finish_cleanup.go:245`) skips the 4-step chain. With ≥3 peers (server +
2 clients) the two clients BOTH see "my name ∉ peers" → both rule, possibly
differently, and each believes itself arbiter. The new `role`/`sync_to`
settings keys (23d46bb, node_role.go) exist precisely to declare this —
`IsServerNode` should consult the declared role first (explicit data over
hostname guessing, the same lesson bug041 recorded), falling back to the
current heuristic only when role is undeclared.

**N2 — error_1214.go is dead code resurrected by a merge (medium, hygiene).**
Timeline verified commit-by-commit: 5c6f5ca 12:41 deleted error_1214.go+test
(630 lines) and rewired both call sites to `runMinimalCoreDegrade`; deaf20e
18:21 (sibling branch, forked pre-5c6f5ca at c2bda60) added a 7-line comment
ON TOP OF the still-alive 1214 file and re-noted its call sites; merge 1e63f3f
resolved the conflict by keeping **side B's resurrected file** while taking
**side A's claude_client.go** → the file's 5 funcs (`run1214Ladder`,
`is1214Error`, `stripOptionalFor1214`, `degradeMessagesFor1214`,
`dump1214Request`) now have zero callers (grep: only self-references;
`claudeSendRoundtrip` also defined twice — error_1214.go:64 is the live one
via minimal_core's orphaned comment, build passes only because the type dup
is comment-vs-comment). Its own header says "REMOVE THIS FILE once the root
cause is fixed properly". The deaf20e NOTE (effort_values independence) is
worth keeping — move it into minimal_core.go, then delete the file. Also
reinstate `tailBytes` (deleted from minimal_core by the same merge, still
referenced by claude_client.go:978 — currently defined only inside dead
error_1214.go; single-source it).

**N3 — finish-cleanup `-D` escalation deletes a branch on the strength of a
push that may be a no-op (low-medium).** `task_finish_cleanup.go:458`: after
step ② push "succeeds", a `-d` refusal auto-escalates to `-D`. The comment
argues "safe BY CONSTRUCTION — branch tip IS what the server now holds", but
a push that returns "Everything up-to-date" (branch already on server, master
has NOT moved, branch genuinely unmerged into master) also passes step ② —
then `-D` discards the only local ref to work that exists solely on the
server's feature branch. Risk window is real for the "old branch pushed
long ago, never merged" shape. Suggested guard: only escalate when the push
output actually delivered commits (non-up-to-date), or archive-tag the branch
(`archive/<name>`) before `-D` — matching scripts/post-merge-cleanup.sh's
own convention.

**N4 — TaskDedupScan dead map + quadratic scan without short-circuit
(low).** `memory_sync_task.go:718`: `seenPair[a+"\x00"+b] = true` is written
but never read (the i<j loop already guarantees uniqueness); with
`syncTaskDedupScanMaxRows = 2000` the pairwise ladder is ~2M
`syncTaskDedupDecide` calls per audit. Correct but wasteful; drop the map and
consider a canon-hash bucket pre-group before the O(n²) pass.

**N5 — Timeout constants three-way inconsistency for the same remote
exchange (low, Rule-1 discipline).** One peer roundtrip is bounded by
`syncSSHTimeout=15s` (memory_sync_task.go:140) but the peer side runs
export→merge→export over SQLite that may queue on busy_timeout 15s —
a slow peer burns the leg and the initiator reports unreachable, while
finish-cleanup's git step allows 60s (`finishCleanupGitTimeout`) and
`remoteReportTimeout=30s` bounds the CLI face's ssh writeback. Three bounds,
one logical operation family. Pick one budget (suggest 30s) shared by all
ssh-leg callers via a single named const + reference sites.

**N6 — `30*time.Second` inline literal in serveFinishCleanupSweep
(hygiene, Rule 1).** `cmd/agent/cli_tasks.go:961` — the const block at :707
(`remoteReportTimeout`) and :931 (`serveFinishCleanupThrottleMin`) is the
established pattern; the sweep's own ledger-open budget should be named
(`serveFinishCleanupDBTimeout`), matching `finishCleanupDBTimeout` in
tools.

### 4. Merge-bifurcation audit (3 unmerged branches + 1 landed-by-other-path)
- `feat/claim-broadcast` (7 ahead of fork, incl. M4.18 refusal→yield E2E,
  memory_sync_claim.go 445 lines, yield-probe tests): **not in master**;
  content does not overlap the dedup/txn work; no merge conflicts expected
  beyond cli_tasks.go adjacency. Its `git diff master` shows it would also
  DELETE devdoc/sync-error-tasks-review.md + text2img fixtures — rebase
  before merge so only its own files land.
- `feat/branch-adoption-chain` (0c183cc..5cb29fb): adoption chain + nil-ctx
  fix; master LACKS the nil-ctx guard (16cdd25: `ctx = context.Background()`
  in the CLI face) — until merged, `gopi task adopt` on master segfaults
  database/sql. Merge priority: high.
- `feat/node-role-banner` (23d46bb): node_role.go + banner; the IsServerNode
  fix (N1) should build on THIS branch's declared-role data.
- `fix/memsync-import-atomic` (4b05426): its fix content landed in master via
  1dcee44 (same BEGIN-IMMEDIATE design, master version is the kept one) —
  the branch is a **duplicate delivery**; safe to archive-tag + delete after
  cherry-picking 88a10f2/9924831's ambient-pinning test hardening IF not
  already present (88a10f2's `t.Setenv(config.SyncPeersEnv, ...)` IS in
  master's delta_test via the bidir lineage — verified).

### 5. Incident records verified (both properly closed)
- t-tick-* production-ledger leak (d76fb5a report): rows deleted, anchor
  rewound 1788143199→1788159686, structurally prevented via
  `syncSSHExchangeHook` injection seam (nil hook = prod exec path; tests must
  inject). Design is sound — the hazard is now unreachable from the tools
  layer.
- bug027 taskshow column-width regression: fixed width-agnostic
  (`regexp status:\s+failed`), regression tests green at HEAD.

### 6. Adoption audit (Rule 11) — window 632c4c4..HEAD, 55 sessions, 9 954 tool_use
| tool | calls | share | verdict |
|---|---|---|---|
| bash | 6427 | 64.6% | dominant (expected) |
| context_trim | 1621 | 16.3% | adopted fast |
| edit/read/write | 1073 | 10.8% | healthy |
| **code_search** | **252** | **2.5%** | **×8 vs the 0.3% baseline — adoption proof landed** (1 "No symbol found" in window) |
| memory_* family | 268 | 2.7% | feedback>search: the judge-loop is live |
| git_blame | **1** | 0.0% | still ~0 — prompt routing for git history queries remains unconvincing |
| bash-calls-containing-grep | 2784 | 28.0% of bash | grep-in-bash persists for file/line-context work; code_search covers definitions only — acceptable, but the *definition* greps inside bash should keep shrinking |

### 7. Testsuites vs gaps (deferred from prior review)
- Covered: dispatcher claim race (concurrent, race-clean), two-process
  concurrent import (txn test), bidirectional 4-test addendum, dedup ladder
  (10 tests), finish-cleanup 6-fixture chain, bash dangerous-guard 28,
  zombie verdict 10, adoption chain (branch-local 294-line test file),
  node-role 103-line test file, procedural memory 6, brew 8.
- Gaps (unchanged priorities): (a) no E2E for `gopi task sync` over a REAL
  ssh leg since the shim tests only hook it — the m49 leak incident argues
  for one sandboxed real-ssh test with an alias→localhost map; (b) no test
  exercises IsServerNode with ≥3 distinct peers (N1's collision shape);
  (c) finish-cleanup has no test for the up-to-date-push→`-D` path (N3);
  (d) testsuite/ covers task+chat+web happy paths only — no error-scenario
  stdin for task sync/serve-remote CLI faces.

## Not Done (honest ledger)
- No code changes shipped: this was a read-only review pass per scope; N1–N6
  are filed here for the PA to dispatch (N1+N2 are the merge-blocking pair).
- The 4 unmerged branches were NOT merged/archived by me — merge is PA-owned
  (Rule 9); the audit above is the input to that 裁决.
- testsuite/ reorg (prior review's deferred item) remains deferred — no
  owner assigned yet.
- 52 text2img-eval fixture images counted in the diff window were not
  re-reviewed (covered by the t2img case-study's own IoU verification).

## Verification
- Build: `go build ./...` → OK; `go vet` 4 packages → OK
- Tests: tools+deepseek+agent+config+cmd/agent → all ok (560s);
  `-race TestTaskNextConcurrentClaimRace` → ok
- Adopt-audit: 55 session JSONLs parsed (9 954 tool_use), numbers in §6
- Commit: <see feat/review-suite-2 tip — this report + no code change>
