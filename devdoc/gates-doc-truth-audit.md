<!-- doc-class: historical -->
# Task Report: C5 — gate/test-infra doc-truth audit (6 docs) + GOPI.md Rules 16–21

Slice C5 of the doc-truth audit epic (`devdoc/doc-truth-audit-plan.md` §C5).
Read-only audit: **no code changed, nothing submitted** — the deliverable is this
record of what still holds, what drifted, and what each drift is worth fixing.

Master audited: `52090dc0` (worktree `/home/user/workspace/gopi-gates-audit`,
branch `feat/gates-doc-truth-audit`). Method follows the C1 recipe: extract
falsifiable claims (symbols, file:line, test names, constants, branches), resolve
each with `code_search`/`read`/`rg`/git ancestry, and **run** every named test
that is cheap. Vocabulary: TRUE / DRIFTED (claim wrong now) / SUPERSEDED (a later
decision changed it) / RECORD (historical run log, no standing claim) / GAP.

## Summary roll-up

- **All 6 docs' named gates, packages and tests still EXIST** and the cheap ones
  still RUN GREEN. No doc in this slice cites a test name that has vanished. The
  drift is in **numbers, constants and commit hashes**, plus one dead backup path.
- 6 of 6 docs are unaffected in their central claim — no named gate, package or
  test has vanished. 5 carry DRIFTED details (counts, a retired constant, two
  dead commit hashes, a `/tmp` artifact). The slice's two **normative** docs
  (row 2 `merge-time-contract-gate-20260912`, row 4 `cli-contract-testsuite`) are
  the two that a reader would trust hardest, and both drift in their numbers.
- **GOPI.md Rules 16–21**: all six gate ids, packages, predicates, probe tests
  and escape markers resolve (row 7). Two carry drifted numbers (Rule 17, Rule 20);
  Rule 16's number is already self-corrected in-file.
- The **doc-class manifest gate is RED on master** (F5) — the one finding here
  that is infrastructure rather than prose, and the same gate every doc-truth
  audit slice depends on for its classification.
- One doc's commit hash and one doc's branch commit no longer resolve (F6, F7)
  — the "declared deliverable" half of Rule 6d, where a recovery command in the
  doc now fails.

## Verdict rows (one per doc)

| # | doc | class (DOC-INDEX) | verdict | code evidence | drift / fix | bug-worth. |
|---|---|---|---|---|---|---|
| 1 | `merge-resolve-ledger-busy-dsn-test-contract.md` | historical | RECORD, 2 claims DRIFTED | all 6 resolved files present on master (`cmd/agent/cli_daemon_scope.go`, `daemon_cgroup_e2e_test.go`, `daemon_scope_unit_test.go`, `ledger_dsn_contract_test.go`, `internal/tools/ledger_dsn.go`, `ledger_dsn_test.go`); `ledgerPragmas`(`internal/tools/ledger_dsn.go`), `LedgerReadOnlyDSN`(`internal/tools/memory_sync_tagcase_test.go`); `daemonScopeQuote` has **0 live callers** (`git grep` → 4 hits, all comments) | (a) resolver commit `53c9fdeb` is **not an object** in the shared repo and no archive tag holds it → F6. (b) its two "pre-existing master defects" (duplicate `funcBody`; `TestLedgerOpenersUseProductionDSNContract` red) are **both FIXED** — 1 definition left, ledger gate ok 5.2s/0.8s | 4 |
| 2 | `merge-time-contract-gate-20260912.md` | **normative** | RECORD + SUPERSEDED-in-part (its own §5 banner says so), 2 claims DRIFTED | step ⓪ is the gate call at `internal/tools/task_merge.go:440`, still **before** ① `merge-base --is-ancestor` at `task_merge.go:491`; ⓪b build gate follows; `ContractRefused` `task_merge.go:271`/`repo_sweep.go:429`; `contract-refused=%d` in `cmd/agent/cli_tasks.go:2856`; `GOPI_MERGE_CONTRACT_GATE` single reader `merge_contract_gate.go:403`; all 9 named tests exist (`merge_contract_gate_test.go:138/183/210/235/259/277/296/327/366`, `clicon…_test.go:236/256`) and run ok 9.5s | (a) `mergeContractGateMaxShowCalls` — cited by the doc's Rules-compliance list — is **gone from the tree** (0 hits; the winner-rule rewrite replaced it). (b) `§5 Real merger path (end-to-end)` is a **heading with no body**. (c) the doc's own outline puts ⓪ *after* ①, code has ⓪ first | 2 |
| 3 | `merge-time-contract-gate-20260913-postmerge-winner.md` | historical | TRUE (winner rule, budget, tests) + 1 DRIFTED sentence | `mergeContractGateFileWinner` `internal/tools/merge_contract_gate.go:318`; constants block `:100-133` (`Revisions=3`, `BatchCalls=1`, `BaseRefCalls=1`, `SubprocessBudget=2`); both named regressions exist `merge_contract_gate_winner_test.go:39/110`; `TestMergeContractGate_CostIsBounded` `:424` green; abstention test `:277` green; `cmd/agent -run TestCLIContract`→ok 12.024s, `internal/clicontract`→ok | the 4-way winner table and the 2-subprocess budget are exactly what the code does. DRIFTED: `mergeContractGatePresenceProbeBytes` (`:138`) is documented as detecting "a declaration way below the file's parse anchors" but `headOf` (`:143`) is a plain truncation whose single call site (`:371`) does `strings.Contains` over the first 64 KiB of a 33 KB file → F9 | 2 |
| 4 | `cli-contract-testsuite.md` | **normative** | TRUE in shape, DRIFTED in every count | `TestCLIContractInventoryMatchesDispatch` `cmd/agent/cli_contracts_test.go:291`, `TestCLIContractSettingsConfigFlagRoutesToTaskHandler` `:503`, `TestCLIUserVerbTokensAreAllFromCliVerbs` `:248`; predicate parser reads the live `main.go` chain (`cmd/agent/main.go:859`); `gopiBinOnce`/`gopiTestBinPath` `cmd/agent/gopibin_test.go:64`; env scrub `:446-466` (drops `GOPI_*`/`ANTHROPIC_*`/`DEEPSEEK_*`); `cliContractTimeout = 15*time.Second` `:50`; session-artifact assertion `assertNoCLIContractSession`; `TestCLIContract` → ok 12.024s; `config.BootstrapEnvFromArgs` `main.go:825` | the doc's numbers are stale: **"26 entries"** → 33 `argv:` cases (`:67`), **"22 predicates"** → 27 (`main.go:859`), coverage sentence re-lists 26 verbs. Same for the `bd3d34a5` 25-vs-26 story, which was true then | 3 |
| 5 | `reflow-gate-contract.md` | historical | RECORD, 1 claim DRIFTED, 1 GAP | `syncGateTag` `internal/tools/memory_sync_delta.go:170` (raw node name, `from-` added once — the double-prefix disarm is genuinely fixed); `TestReflowGateTagNormalization` `internal/tools/reflow_extension_test.go:96`; all 7 other named tests exist (`memory_sync_reflow_test.go`, `memory_sync_bidir2_e2e_test.go`, `memory_sync_bidir_e2e_test.go`); the fix **is on master** as `dc1909c7` | (a) the doc's recovery hash `c4745d9` is **NOT an ancestor of master** — master carries the same work as `dc1909c7` (message = the doc's own summary) → F7. (b) `TestReflowEchoStaleMirrorNoReturn` is RED in this environment (refused `task_submit`: "role=client … no authoritative peer"), reproducible test-alone and with `GOPI_SYNC_*`/`GOPI_SETTINGS` stripped → environment/pre-existing, not the doc's fix. (c) GAP: the three-node relay/terminal-homecoming path still has **no dedicated E2E sim** (the doc says so honestly) | 3 |
| 6 | `empty-test-model-sessions-investigation.md` | historical | RECORD, claims TRUE today, 1 artifact DRIFTED | `rawLogPath` is still the session ground-truth field (`internal/agent/session.go:163` writes it when non-empty, `:102` documents it); HOME isolation landed (`newTestAgentEnv` `internal/agent/loop_test.go:858`); **live check**: `~/.gopi/sessions` = 74 608 files, `grep -l '"test-model"'` = **0** → the 797-file backlog is gone and has not regrown | (a) the doc's backup manifest `/tmp/empty-test-sessions-backup-list.txt` no longer exists → the deletion record is unrecoverable, and a `/tmp` path is what Rule 22 bans for scratch artifacts → F10. (b) "797" is unverifiable today (no manifest, no census script) | 2 |
| 7 | **GOPI.md Rules 16–21** (code-side counterpart) | n/a — `GOPI.md` | gate ids/packages/predicates/markers all TRUE; **2 of 6 numbers DRIFTED** | Rule 16 `TestGate1ErrorWrapRedProof` `internal/standards/gate_test.go:155`; Rule 17 `TestGate2TestEnvRedProof` `:192` + `TestGate2EnvRestoreShapeRedProof` `:232` + `TestGate2ScansTheE2ESuite` `:367` (`e2eTestRoot = "testsuite"` `gate.go:160`); Rule 18 `TestGate3ElseAfterReturnRedProof` `:324`; Rule 19 `TestGate4PanicRedProof` `:437` + `panic-ok` `internal/agent/crashdump.go:226`; Rule 20 `TestGate5AccessorPrefixRedProof` `:491` + niladic predicate `gate.go:1058-1072`; Rule 21 `TestGate6EnvReaderRedProof` `:602` + `TestGate6EnvReaderFindsTheRealTree` `:687` + the motivating hook `GCIntervalEnvName` `internal/scratch/gc.go:116`; escape census `no-wrap:`14 / `env-leak-ok:`32 / `else-ok:`2 / `panic-ok:`4 / `name-ok:`2 / `env-reader-exempt:`5; live gate report `Rule 16 hits=7 baselined=6 escaped=1`, `Rule 17 118/91/27`, `Rule 18 1/1/0`, `Rule 19 1/0/1`, `Rule 20 1/0/1`, all `NEW=0 STALE=0` | Rule 17 "96 legacy sites baselined" → **91** (and 27 escapes where the doc frames escapes as "individually reviewed" without a count) → F1; Rule 20 "4 true-positive sites" → **1** (`accessorPrefixBaseline` empty, `getRootImage` `internal/shot/x11.go:575`; `getImageBand` has a receiver arg → not in predicate scope) → F2; Rule 16 "352 pre-existing sites are baselined" → **6** deployed → F3 | 3 |
| 8 | manifest/doc-class gate itself | n/a — tooling | **RED on master** | `TestDocManifestCoversEveryDevdocFile` + `TestDocClassBannerMatchesManifest` FAIL: "`devdoc/DOC-INDEX.tsv`: 2012 rows cover 2041 files … and 4 more unclassified files" (25 named + 4) | the manifest that every class-driven gate reads is 29 rows short → F5 | 7 |

## Findings (ranked)

### F1 — Rule 17's baseline count is 91, not the doc's 96; 27 escapes are unaccounted
`GOPI.md:600` — "(96 legacy sites baselined)". Deployed: `testEnvBaseline`
(`internal/standards/baseline.go:479`) = **86** entries; the live report says
`Rule 17: hits=118 baselined=91 escaped=27`. 86 of the 96 are in one list and
**10 legacy sites are gone entirely**; `devdoc/standards-gate.md:96` records the
split that makes the doc's number legible ("allow-list is 91 entries; the row is
the counted split") but the number only ever moved *down*, which is the intended
ratchet. 27 escapes is also invisible in GOPI.md.
**Fix**: state 91 and mirror `standards-gate.md`'s counted split, or point at the
data report as the reader (`go test -run TestStandardGateDataReport -v ./internal/standards/`)
— the Rule-2 discipline applied to a number.
**Bug-worthiness 3/10** — accounting only; no gate behaviour is wrong.

### F2 — Rule 20 says 4 true positives; the gate has 1
`GOPI.md:661` — "4 true-positive sites (`internal/shot/x11.go` `getRootImage`,
`getImageBand`, `handshake`)" counts three names and calls them four.
`accessorPrefixBaseline` (`baseline.go:685`) is **empty**, the report is
`Rule 20: hits=1 baselined=0 escaped=1`, and the predicate is niladic-only
(`gate.go:1058-1072`, `len(fn.Type.Params.List) != 0` → skip), so
`getImageBand(img, y, h)` and `handshake(display, d)` are out of scope by design.
The two annotated sites are `getRootImage` (`x11.go:574-575`) and `handshake`
(`x11.go:432-433`, carrying `//nolint:name-ok …` — a form the gate's escape
constant `// name-ok:` does **not** match, so that annotation is decorative).
**Fix**: say 1 kept site + 2 reviewed out-of-scope sites; normalize the
`//nolint:name-ok` spelling if it is meant to be a gate escape.
**Bug-worthiness 2/10.**

### F3 — Rule 16's "352" is a historical count; the deployed baseline is 6
`GOPI.md:553` — "352 pre-existing sites are baselined". `errorWrapBaseline`
(`baseline.go:53`) = **6** entries; report `Rule 16: hits=7 baselined=6 escaped=1`.
The codemod the rule *scheduled* has run: the 352 sites were fixed, not allowed.
`devdoc/standards-gate.md:11-25` already carries the superseded-numbers banner
(and is itself stale: it says Rule 16 = 10 / Rule 18 = 7; today 6 and 1).
**Fix**: replace GOPI.md's number with the reader command, and re-derive the
banner numbers from the report rather than restating them (this file is the third
copy of the same number).
**Bug-worthiness 3/10.**

### F4 — A normative doc cites a constant that no longer exists
`devdoc/merge-time-contract-gate-20260912.md` §Rules-compliance lists
`mergeContractGateMaxShowCalls` among the constants it introduced. `git grep
mergeContractGateMaxShowCalls` → **0 hits**; the winner-rule rewrite
(`merge_contract_gate.go:100-133`) replaced it with
`mergeContractGateBatchCalls`/`mergeContractGateBaseRefCalls`/`…SubprocessBudget`.
The same doc's §"MEASURED cost" table still describes "2 files × 2 revisions"
against a code path that reads 3 revisions per file. §5 "Real merger path
(end-to-end, `MergerRepoSweep`)" is a heading with **no content**.
**Fix**: refresh the two constant/revision references to the batched budget (the
doc's own SUPERSEDED banner already points at doc #3), finish or delete §5.
**Bug-worthiness 2/10** — a stale enforcer reference, the exact class the C1
recipe lists as high-yield.

### F5 — The doc-class manifest gate is RED on master (29 files unclassified)
`go test -run 'TestDocManifestCoversEveryDevdocFile|TestDocClassBannerMatchesManifest' ./internal/standards/`
→ FAIL: `devdoc/DOC-INDEX.tsv`: **2012 rows cover 2041 files**, listing 25
unclassified files + "… and 4 more". The manifest is `internal/docclass/docclass.go:42`
(`Root + "/DOC-INDEX.tsv"`), generated once by `tools/docclass/classify.py`, and it
is the source of truth for the class-driven gates — including the banner gate that
every audit slice's own report must satisfy. Note the C1 deliverable
(`devdoc/crosscutting-doc-truth-audit.md`) has **no row** among them, i.e. past
audit slices' reports are part of the backlog.
**Fix**: re-run the generator to append the 29 missing rows (class + reason +
banner), then re-run the banner gate; a one-command repair, but it belongs to a
task that owns `devdoc/DOC-INDEX.tsv` (this audit only adds its own row).
**Bug-worthiness 7/10** — a red gate on master, and the input every doc-truth
audit classifies against.

### F6 — Doc #1's resolver commit `53c9fdeb` does not exist anywhere
`devdoc/merge-resolve-ledger-busy-dsn-test-contract.md:3-4` names `53c9fdeb` as
"the deliverable". `git cat-file -t 53c9fdeb` → *fatal: Not a valid object name*;
no `archive/*` tag holds it; `git branch -a` has no branch carrying it. Both named
parents exist and are on master (`4ab3aaca`, `99856f0b`). The doc's central
*content* claim — all six resolved files byte-identical to master — holds
(all six present on master).
**Fix**: re-point the doc at the merge commit that is on master, or add one line
recording that the resolver commit was consumed by master's own resolution
(`99856f0b`), so the record is not a dead pointer.
**Bug-worthiness 4/10** — the Rule-6d failure shape (a named deliverable nobody
can fetch); here the work itself survived.

### F7 — Doc #5's recovery hash is wrong: the work landed as `dc1909c7`
`devdoc/reflow-gate-contract.md` ends "Commit: `c4745d9` on
`fix/reflow-gate-contract` (recovery: `git show fix/reflow-gate-contract --stat`
from `~/work/GoPi`)". `git merge-base --is-ancestor c4745d9 master` → **NO**;
`git show fix/reflow-gate-contract` → *unknown revision* (branch gone, no tag).
`git log -S 'func syncGateTag'` on master → **`dc1909c7` fix(sync): reflow-gate
contract — double-prefix disarm + Peer wiring + terminal homecoming**, i.e. the
same work, on master, under the doc's own one-line summary.
**Fix**: annotate the doc with "landed on master as `dc1909c7`" so its recovery
command resolves.
**Bug-worthiness 3/10.**

### F8 — The Rule-17 baseline's own honesty note contradicts its list
`internal/standards/baseline.go:479-490` states, as a correction of an earlier
false claim: "Measured against the 96 entries below: 31 carry a reason on the
offending line itself, 43 within +/-3 lines, 47 within +/-6 — i.e. 53 entries have
no reason even 3 lines away". Today the list holds **86** entries and the report
baselines **91**, so a measurement inside the ratchet's own file is stated against
a population that no longer exists — in exactly the way that comment was written
to prevent.
**Fix**: make the measurement a printed row of `TestStandardGateDataReport`
(counted at run time), or delete the three numbers and keep the qualitative
statement.
**Bug-worthiness 3/10** — self-drift inside the guard that reports drift.

### F9 — `mergeContractGatePresenceProbeBytes` documents a check that does not exist
`internal/tools/merge_contract_gate.go:134-141`: "`…PresenceProbeBytes` bounds the
head probe that decides whether the contract suite still declares the dispatcher
token map. **A declaration way below the file's parse anchors would be a moved
file**, not a documented one". Deployed: `headOf` (`:143`) is a plain truncation
and its **only** call site (`:371`) runs `strings.Contains(headOf(testSrc),
clicontract.DispatcherTokenMapAnchor)` — a *containment* test over the first
64 KiB of a 33 283-byte file (`wc -c cmd/agent/cli_contracts_test.go`). No
position/fraction comparison exists, so the "moved file" condition the comment
names cannot be detected; the constant is not under budget because there is no
budget.
**Fix**: either test the anchor's byte offset against a fraction of the blob (a
real presence check, which is what the doc's "M4" claims) or drop the sentence and
keep the truncation as the cost bound it actually is.
**Bug-worthiness 2/10** — no correctness impact; the abstention path is
deterministic, and the anchor check itself is sound.

### F10 — Doc #6's deletion manifest lived in `/tmp` and is gone
`devdoc/empty-test-model-sessions-investigation.md`: "backed up list to
`/tmp/empty-test-sessions-backup-list.txt`". The file does **not** exist
(`ls` → No such file or directory), so the 797-file deletion has no recoverable
record, and the location was a `/tmp` path — precisely the class of scratch path
Rule 22 bans and the sweep that rule describes has since cleaned.
**Fix**: no recovery is possible for this record; any future bulk deletion writes
its manifest under the run's session/scratch tree, and the doc should say the list
is gone rather than implying it is fetchable.
**Bug-worthiness 2/10** — historical record only.

### F11 — Doc #5's GAP is still open: no E2E for the terminal-homecoming path
The doc is honest ("The three-node relay repair claim from sync-reconcile.md's
residual is now unblocked but has no dedicated E2E sim yet"), and it is still true:
the reflow suite carries no three-node sim (`memory_sync_reflow_test.go`,
`memory_sync_bidir2_e2e_test.go`, `memory_sync_bidir_e2e_test.go` —
none of them tagged heavy in `testdata/heavy-tests.tsv`, and none exercises a
third-node relay), while the fixed predicate (`NOT (status IN
(pending,claimed,in_progress) AND tags LIKE from-<gate>)`) is a *conjunction* that
nothing end-to-end pins.
**Fix**: one E2E sim task (three nodes, terminal verdict homecoming) — submit it,
do not re-derive from this report.
**Bug-worthiness 4/10** — a real coverage hole behind a correctness claim, but the
unit-level fence (`TestReflowGateTagNormalization`) does hold.

### F12 — The reflow/Peer tests are not isolated from the ambient task-run environment
Evidence, measured twice: `go test -run TestReflowEchoStaleMirrorNoReturn
./internal/tools/` → FAIL
(`task_submit: REFUSED … this node declares role=client … no authoritative peer is
configured (sync_to)`), reproducing test-alone and with `GOPI_SYNC_NODE`,
`GOPI_SYNC_TO`, `GOPI_SYNC_ROLE` **and** `GOPI_SETTINGS` stripped. The rule's own
remedy is in the tree — `internal/testutil/envsnapshot.go:46` `SnapshotEnv` and the
node-role reset `config.ResetNodeIdentityForTest` (named in the doc's own root-cause
note) — but the file only sets `GOPI_SYNC_NODE` (`memory_sync_reflow_test.go:29,
67, 98`), so role/peer resolution can still reach the host's real settings through
the inherited `GOPI_SETTINGS` (`cliContractEnv`, by contrast, drops every
`GOPI_*` var: `cmd/agent/cli_contracts_test.go:446-466`).
**Fix**: give the reflow tests the same scrub the CLI contract suite uses (or pin
role/`sync_to` explicitly via `SnapshotEnv`) — a small, high-value fence because
the rule's own rationale ("a dev shell or a task-run child exports …") is exactly
this environment.
**Bug-worthiness 5/10** — a suite that is green only on a clean host is a suite
that fails for whoever inherits a `GOPI_SETTINGS`; it is why the "Tests" line of
this report must name the failing test rather than claim a green `internal/tools`.

## Coverage and limits

- **In scope**: the 6 named docs, their claims about code (symbols, constants,
  tests, CLI/env surfaces, branches/commits) and GOPI.md Rules 16–21 as the
  code-side counterpart — gate id → package → predicate → probe test → baseline
  count → escape marker.
- **Out of scope / not audited**: prose-only design reasoning in the 6 docs (e.g.
  the (a)-vs-(b) merger cost argument, the union-vs-forward comparison rationale);
  any doc outside this slice; the live merger's runtime behaviour (the gate's
  verdicts were exercised through its tests, not through a merger tick on this
  host).
- **Not run**: the whole-tree suite (Rule 27/`internal/smokegate` — the merge path
  is smoke, branch-scoped), and `cmd/agent`'s full package. `TestReflow*` is red in
  this environment for the pre-existing reason F12 documents; I did not "fix" it
  here (read-only audit) and I did not treat it as evidence against doc #5.
- **Classification** taken from `devdoc/DOC-INDEX.tsv` (`cli-contract-testsuite`
  and `merge-time-contract-gate-20260912` are `normative`; the other four
  `historical`) — itself 29 rows short (F5).
- Numbers quoted as "today" are master `52090dc0` at this run's clock; the
  Rule-16/17/18 baselines are the deploy-time arrays, which move by fixing a site
  (the ratchet's stated intent), so any count here is a snapshot, not a constant.

## Verification

- Build: not applicable — **no production or test file was modified** (`git status`
  lists only this report + its manifest row).
- Tests run (all with `-count=1`, CGO-free, branch-scoped to what this audit
  touches):
  - `go test -run TestStandardGateDataReport -v ./internal/standards/` → **ok 17.262s**
    (`Rule 16 hits=7 baselined=6 escaped=1`; `Rule 17 118/91/27`; `Rule 18 1/1/0`;
    `Rule 19 1/0/1`; `Rule 20 1/0/1`; `Rule 21 0/0/0`; every gate `NEW=0`)
  - `go test -run TestStandardGates ./internal/standards/` → included in the run above
  - `go test -run TestCLIContract ./cmd/agent/` → **ok 12.024s** (doc #4's suite, green)
  - `go test -run 'TestMergeContractGate|TestMergerSweep_ContractRefusal' ./internal/tools/` → **ok 9.526s** (docs #2/#3)
  - `go test ./internal/clicontract/` → **ok 0.005s**
  - `go test -run TestLedgerOpenersUseProductionDSNContract ./cmd/agent/` → **ok 5.248s**; `… ./internal/tools/` → **ok 0.822s** (doc #1's defect 2, now fixed)
  - `go test -run 'TestSmokePathPrescribesNoWholeTreeSuite|TestHeavyTestInventoryMatchesItsCallSites|TestGate5AccessorPrefixRedProof|TestGate6EnvReaderFindsTheRealTree' ./internal/standards/` → **ok 5.194s** (Rule 27 path + Rule 20/21 predicates)
  - `go test -run 'TestDocManifestCoversEveryDevdocFile|TestDocClassBannerMatchesManifest|TestDocManifestRowsAreWellFormed' ./internal/standards/` → **FAIL (F5 only)** — and it fails on the *pre-existing* 29 files, not on this one: after this report's own row was added the gate prints `2013 rows cover 2042 files` with **no `gates-doc-truth-audit` error line** (both counters moved by exactly the report + its row).
  - `go test -run TestReflowEchoStaleMirrorNoReturn ./internal/tools/` → **FAIL** — F12 (pre-existing, environment)
- Commit: `feat/gates-doc-truth-audit` (this report + its `DOC-INDEX.tsv` row).
- Deliverable location / recovery (Rule 6d): branch **`feat/gates-doc-truth-audit`**;
  `git show feat/gates-doc-truth-audit:devdoc/gates-doc-truth-audit.md` (the branch
  lives in the shared object store of `~/work/GoPi` — a worktree commit, no push).

## Evidence (self-filed)

- F1/F2/F3 counts: `awk` over `internal/standards/baseline.go` → 6 / 86 / 1 / 0 /
  0 / 0 entries; the gate's own printed rows: `Rule 16 … baselined=6`,
  `Rule 17 … baselined=91`, `Rule 20 … hits=1 baselined=0 escaped=1`.
- F5: gate output quoted verbatim above ("2012 rows cover 2041 files", 25 named
  files + "… and 4 more unclassified files").
- F6: `git cat-file -t 53c9fdeb` → `fatal: Not a valid object name 53c9fdeb`;
  `4ab3aaca`/`99856f0b` → `commit`; both `merge-base --is-ancestor master` → YES.
- F7: `git log -1 --format=%H -S 'func syncGateTag' -- internal/tools/memory_sync_delta.go`
  → `dc1909c7…`; `merge-base --is-ancestor dc1909c7 master` → ON-MASTER;
  `… c4745d9 master` → NO; `git show fix/reflow-gate-contract` → unknown revision.
- F9: `wc -c cmd/agent/cli_contracts_test.go` → 33283 (vs the constant's 65536);
  `grep -n 'headOf(' internal/tools/merge_contract_gate.go` → `:143` (def) and
  `:371` (sole call).
- F10: `ls /tmp/empty-test-sessions-backup-list.txt` → No such file or directory.
- F12: quoted refusal twice — unsanitized, and with
  `env -u GOPI_SYNC_NODE -u GOPI_SYNC_TO -u GOPI_SYNC_ROLE` / `-u GOPI_SETTINGS`.
- Doc #6 live census: `~/.gopi/sessions` → 74 608 files; files containing
  `"test-model"` → 0.
- Doc #1 `daemonScopeQuote`: `git grep -c daemonScopeQuote` → 4 hits, all comment
  lines (`cmd/agent/cli_daemon_scope.go:779,781`, `daemon_cgroup_e2e_test.go:515`
  + 1) → no live caller, as the doc claims.
- Rule-12/22/25/27 side checks: `{"repos": {"/home/user/work/GoPi": {"build":
  "CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/"}}}` present in
  `~/.gopi/settings.json`; `internal/scratch/tmproot.go` `AssertTempRootOffRootFS`,
  `internal/config/tmpdir.go` `ResolveTmpDirConfigured`, `scripts/lib/tmp-ban.sh`,
  `cmd/agent/daemon_scope_no_privilege_test.go`, `cmd/agent/merger_takeover_prompt.md`,
  `internal/testutil/rule27_gate_test.go`, `testdata/heavy-tests.tsv` (repo root,
  matching GOPI.md's path) and `internal/standards/smoke_path_gate_test.go` all exist.

## Not Done / honest limits

- **Nothing was submitted or fixed** (the epic's acceptance): every finding above is
  a report, not a dispatch. Repair tasks are the operator's call.
- The 29 unclassified `devdoc/` files (F5) were **not** repaired here — I added only
  this report's own row, because `devdoc/DOC-INDEX.tsv` is shared infrastructure
  and a 29-row regeneration belongs to a task that owns it.
- The `TestReflow*` red (F12) was not investigated further than its mechanism: the
  refusal text names role/`sync_to`, and the test file's isolation was read, not
  rewritten.
- Doc-internal design arguments (why gate (a) over gate (b), the union bias, the
  `Compare` conjunction) were checked against the code's *behaviour* but their
  cost measurements were **not** re-created here (Rule 27: no synthetic load; the
  numbers the docs report are wall-clock on a machine that no longer matches).
- One nuance, not a finding: doc #2's "union mirror coverage" rule was written for
  a tip-based basis; under the winner basis (`merge_contract_gate.go:318`) the pair
  handed to `Compare` is already post-merge, so the union is nearly a no-op and the
  conjunction (`covered[p] && tokenMapped[p]`, `internal/clicontract/clicontract.go`
  `Compare`) is what does the work. The claim stays true; its rationale is stale.
