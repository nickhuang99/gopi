<!-- doc-class: historical -->
# Task Report: `./internal/tools` master-red — the peer-alias gate's stale inventory + the claim/Bidir family's role pin

Branch: `fix/peer-alias-inventory-master-red` off master `249b7938`
Worktree: `~/workspace/gopi-peer-alias-inv`
Commit: `e9d72669` (deliverable) + the `memory_sync_delta_test.go` site below

## Summary

Two red-on-master findings in `./internal/tools`, both diagnosed to a named root cause
and fixed:

1. **`TestPeerAliasGuardDocumentedAtEverySSHLeg`** — the ssh leg added by `ledger_backup.go`
   had **no inventory row** in the gate's `theValidatingSite` map. The spawn site itself was
   *already guarded* (`ValidateSSHPeerAlias` before the exec), so the defect was a **stale
   inventory**, not a security hole. Fixed by recording the row in the form the gate
   **enforces** (`"direct"`), not as free text.
2. **The 14-test Bidir/claim family** (`TestBidir*` / `TestRollback*` / `TestSyncClaim*` /
   `TestExportClaim*` / `TestRunClaimLock*` / `TestPushTerminal*`) — **ONE** root cause, not
   fourteen: the fixtures pin `GOPI_NODE_ROLE=client` (M4.20-B) and then submit through the
   **real** `TaskSubmit` write path, while commit `85859202` added a loud refusal for a
   client/mirror leg with no `sync_to` peer.

The family is green on the branch; each failure carries its named master cause (below).

## Finding 1 — the peer-alias gate inventory (red → green)

**Red on master `249b7938`:**

```
$ go test -count=1 -run TestPeerAliasGuardDocumentedAtEverySSHLeg ./internal/tools/
--- FAIL: TestPeerAliasGuardDocumentedAtEverySSHLeg (0.00s)
    peer_alias_test.go:307: ledger_backup.go spawns ssh but names no validating site — route its peer alias through ValidateSSHPeerAlias and record it in theValidatingSite
FAIL	github.com/nick/gopi/internal/tools	0.659s
EXIT=1
```

**The preliminary check confirmed, not assumed.** `internal/tools/ledger_backup.go`:

```go
func ledgerBackupSSHCommand(peer string, header []byte, artifactPath string) ([]byte, error) {
	// Peer-alias guard BEFORE anything is sent: the option-shaped alias
	// (-oProxyCommand=…) must be refused on every path.
	peer, err := ValidateSSHPeerAlias(peer)     // ← line 672, BEFORE the exec
	if err != nil {
		return nil, fmt.Errorf("ledger backup: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), ledgerBackupSSHTimeout)
	...
	cmd := exec.CommandContext(ctx, "ssh", …)   // ← line 678
```

`git blame -L 668,675` → `af9324fa` ("feat(ledger): gopi-owned live-ledger snapshot + peer
ship") shipped the ssh leg **with** its validator but **without** the gate's inventory row.
So master was red on a stale row, never on a bypass.

**Fix** (`internal/tools/peer_alias_test.go`):

```go
		// ledgerBackupSSHCommand validates the alias before it execs ssh —
		// "direct" so the gate ENFORCES the call (a free-text row would be an
		// unverified claim: the validator could be dropped and still pass).
		"ledger_backup.go": "direct",
```

**Why `"direct"` and not a sentence.** The gate reads the row two ways: a **free-text** value
only asserts non-emptiness (an *unverified claim* — the validator could be deleted and the row
would still pass), while `"direct"` asserts the file really calls
`ValidateSSHPeerAlias`/`ValidateSSHGitTarget`. So the enforced form also closes a hole the same
file opened — the row now guards the call it documents. (Field semantics unchanged; the sibling
`memory_sync_claim_ssh.go` row legitimately stays free-text: that file assembles argv only,
while its caller validates.)

**After (branch):**

```
$ go test -count=1 -run TestPeerAliasGuardDocumentedAtEverySSHLeg ./internal/tools/
ok  	github.com/nick/gopi/internal/tools	0.688s
```

### Mutation red-proof (the gate is still load-bearing — evidence, both directions)

Mutation A — remove the validator call from the spawn site. The mutation must still
**compile** (a first attempt that simply dropped the `peer, err := ValidateSSHPeerAlias(peer)`
line failed the build, which proves nothing about the gate), so it replaces the call and keeps
`peer, err` bound:

```
$ # ledger_backup.go:672 `peer, err := ValidateSSHPeerAlias(peer)` → `peer, err := peer, error(nil)`
$ grep -c ValidateSSHPeerAlias internal/tools/ledger_backup.go
0
--- FAIL: TestPeerAliasGuardDocumentedAtEverySSHLeg (0.00s)
    peer_alias_test.go:317: ledger_backup.go is recorded as validating directly but never calls the peer-alias helper
FAIL	github.com/nick/gopi/internal/tools	0.905s   (exit 1)
```

Mutation B — remove the inventory row:

```
$ # peer_alias_test.go: the "ledger_backup.go": "direct" row  →  dropped
--- FAIL: TestPeerAliasGuardDocumentedAtEverySSHLeg (0.00s)
    peer_alias_test.go:307: ledger_backup.go spawns ssh but names no validating site — …
FAIL
```

Both reverted; the gate is green again (`ok … 1.206s`). The full peer-alias face is green:

```
--- PASS: TestValidateSSHPeerAliasRefusalTable (0.00s)
--- PASS: TestValidateSSHGitTargetSplitsAliasHalf (0.00s)
--- PASS: TestPeerAliasLiteralGate_OldGuardShapeMustNotReappear (0.02s)
--- PASS: TestPeerAliasGuardDocumentedAtEverySSHLeg (0.01s)
```

## Finding 2 — the claim/Bidir family: one root cause (red → green)

**Red on master `249b7938`**, each of the 18 running tests failing in **isolation too** (so not
a leak or an ordering artefact), 0.28–3.86 s each, all with the **same** message:

```
--- FAIL: TestRollbackLocalClaimRevertsToPending (0.31s)
    memory_sync_claim_test.go:107: seed submit: task_submit: REFUSED — this row was NOT written
    to any local ledger. Authoritative ledger: NONE DECLARED (sync_to). Local ledger this
    process would have written (the node MIRROR): …/001/memory.db. Reason: this node declares
    role=client (a mirror leg) but no authoritative peer is configured (sync_to / GOPI_SYNC_TO).
```

**Named root cause.** The fixtures pin the role and then submit through the real write path:

- `internal/tools/memory_sync_claim_test.go` `claimTestDB()` — `t.Setenv(config.RoleEnv, "client")`
  (M4.20-B: "a server-role sandbox would RULE on the claim-remote merge residue"), then
  `claimSeedTask()` → the real `TaskSubmit`.
- `internal/tools/memory_sync_bidir_e2e_test.go` `newBidirNode()` (line 57) and `pushTo()`
  (line 158) — the same `"client"` pin; `bidirNode.submit()` → the real `TaskSubmit`.
- `internal/tools/memory_sync_delta_test.go` `newDeltaTestDBClient()` (line 54) — the same pin;
  the writeback tests in `memory_sync_delta_cov_test.go` submit after calling it.

Commit **`85859202`** ("fix(task_submit): resolve the ledger ADDRESS before the insert — forward
to the authoritative peer or refuse loudly") made `task_submit_forward.go`
`taskSubmitNeedsForward()` return *true* for a declared client, so `guardTaskSubmitLedgerAddress`
refuses when no `sync_to` peer resolves. The fixtures pin the role that now refuses.

**Fix — pin UNDECLARED, not `"client"`:**

```go
t.Setenv(config.RoleEnv, "")
```

**Why this is correct, and why the `client` pin was never doing the work the comments credited
it with.** `IsServerNode()` (`internal/tools/memory_sync_delta.go`) is the only ruling
predicate, and it is `false` for a **declared client** *and* for an **undeclared** role (N1
fail-safe: an unknown host defaults to CLIENT — "client overreach is cheap"). So undeclared
carries the identical no-ruling face the pin was chosen for. Decisively,
`grep -rn NodeRoleClient --include=*.go internal/ cmd/ | grep -v _test.go` shows the constant
is consulted by **no production branch** — its only production occurrence is inside the refusal
message's own wording. `taskSubmitNeedsForward()` is the *only* consumer that distinguishes
`client` from undeclared, and undeclared is the only role that may write locally.

The pin had to change rather than the submit path having to forward: declaring a `sync_to` peer
would route these unit fixtures to a real ssh peer — the opposite of what they assert.

**The refusal itself keeps its coverage.** `remote_submit_repro_test.go`
(`TestReproRemoteLegSubmitMustNotWriteNodeLocalLedgerSilently`) deliberately pins
`NodeRoleClient` + an empty `sync_to` and asserts the loud refusal, so this fix does not delete
the behaviour — it stops unrelated fixtures from tripping it.

**After (branch):**

```
$ go test -count=1 -run 'TestBidir|TestRollback|TestSyncClaim|TestExportClaim|TestRunClaimLock|TestPushTerminal' ./internal/tools/
ok  	github.com/nick/gopi/internal/tools	44.465s
```

### The same cause reaches beyond the listed 14 (verified, fixed)

Master's whole-package run named `TestProposalRowsAreNeverClaimable` and
`TestTaskRunWritebackTaggedStampsWorkerProvenance` with the identical message. The first shares
`claimTestDB` (fixed by the helper edit). The second has its **own** pin via
`newDeltaTestDBClient`, which the whole-package run never reached — Go's 10m cap panicked
**while running `TestTaskRunWritebackTagsExplicitAndEmpty`**, the third of four sibling
writeback tests that submit through the same helper. `newDeltaTestDBClient` is therefore fixed
at its source (42 callers, of which only `memory_sync_delta_cov_test.go` submits; the other
three caller files never submit, so the undeclared pin is behaviour-preserving for them).

```
$ go test -count=1 -run 'TestTaskRunWriteback|TestProposalRowsAreNeverClaimable|TestTagWorkerClaimUnionIsIdempotent' ./internal/tools/
ok  	github.com/nick/gopi/internal/tools	10.476s
```

## Finding 3 — the per-package budget (recorded; NOT widened)

Recorded from the repo's own instrument — the declared budget is **one** constant
(`scripts/test-budget.sh`: `test_budget="25m"`, Rule 1a/2) and the script already reports the
slowest 15 tests from the `-json` stream (`scripts/test-budget.sh:125`) — **no load was
generated** (Rule 27). Master's whole-`./internal/tools` run:

```
# ~/workspace/_evid/master-tools-wholepkg.txt
--- FAIL: TestTaskRunWritebackTaggedStampsWorkerProvenance (0.34s)
panic: test timed out after 10m0s
	running tests:
		TestTaskRunWritebackTagsExplicitAndEmpty (0s)
FAIL	github.com/nick/gopi/internal/tools	601.029s
MASTER_WHOLEPKG_EXIT=1
```

26 failures were recorded before the package ran out of Go's **default 10m** cap. What that
does and does not establish:

- **Established:** on the master tree the whole package does not finish inside Go's default 10m
  per-package timeout; the failures above keep it from finishing at all.
- **NOT established (honest gap):** that the package exceeds the repo's **25m** budget. The run
  above hit Go's *default* cap, not `test_budget`. Confirming the 25m finding needs a
  `-timeout 25m` whole-package run **plus** the heavy-offenders list, which is exactly the
  classification half.
- **Where that half belongs:** the heavy-test machinery is already in place
  (`internal/heavytest.Require`, `testdata/heavy-tests.tsv` with 9 `internal/tools` rows,
  `scripts/heavy-derive.sh`, `make test-heavy`, and
  `TestHeavyTestInventoryMatchesItsCallSites`). Classifying further offenders — and any change
  under `internal/testutil` / the budget policy — is **system-layer** work (Rule 28: peerbox),
  explicitly out of this task's scope, so it is dispatched as its own task rather than done
  here. The budget was **not** widened.
- **The follow-up is dispatched, not deferred by hand:** task **`#01a0b80d`** — *"./internal/tools
  per-package budget: measure the declared 25m (not Go's default 10m) + classify heavy
  offenders, without load generation"* — carries exactly this half (system layer, Rule 28).

## Files changed

| file | change |
|---|---|
| `internal/tools/peer_alias_test.go` | inventory row for `ledger_backup.go` in the enforced `"direct"` form |
| `internal/tools/memory_sync_claim_test.go` | `claimTestDB` role pin: `"client"` → undeclared |
| `internal/tools/memory_sync_bidir_e2e_test.go` | `newBidirNode` + `pushTo` role pin: `"client"` → undeclared |
| `internal/tools/memory_sync_delta_test.go` | `newDeltaTestDBClient` role pin: `"client"` → undeclared |

No production code changed: the production behaviour was correct in both findings — a *test
inventory* was stale in (1) and a *fixture* tripped a correct new refusal in (2).

## Not Done (honest)

- **The 25m budget figure is not independently confirmed** by this run (see Finding 3: the
  master run hit Go's default 10m; a `-timeout 25m` whole-package measurement plus the
  heavy-offender classification is the dispatched follow-up).
- The four writeback tests in `memory_sync_delta_cov_test.go` were **not** named as failures by
  master's own run (it panicked before reaching three of them); their fix is grounded on the
  shared helper and the one named failure, verified green on the branch, but a full master run
  of that file was not obtained.
- Pre-existing unrelated reds on master seen inside the whole-package run
  (`TestAssertionTotalityAuditGate`, `TestFirstOpen*`, `TestTasksSchemaBuildIsEquivalentAcrossPaths`,
  `TestOperatorTextTellsTheVerifierHeavyTestsDidNotRun`,
  `TestTheDocumentedOperatorRouteActuallyEnablesHeavy`) are outside this task's scope and were
  left alone.
- Three files under `internal/tools` are not `gofmt`-clean **on master already**
  (`task_finish_premerge.go`, `task_staleness_guard_test.go`, `taskledger_cost_test.go`);
  untouched here. All four files this branch edits are `gofmt`-clean.

## Verification

- Build: `go build ./...` — OK
- Gate (acceptance): `go test -count=1 -run TestPeerAliasGuardDocumentedAtEverySSHLeg ./internal/tools/` — `ok github.com/nick/gopi/internal/tools 0.688s` (branch) vs `FAIL … EXIT=1` (master `249b7938`)
- Family: `go test -count=1 -run 'TestBidir|TestRollback|TestSyncClaim|TestExportClaim|TestRunClaimLock|TestPushTerminal' ./internal/tools/` — `ok … 44.465s` (branch) vs 18/18 `FAIL` (master)
- Mutation red-proof: validator removed → FAIL at `peer_alias_test.go:317`; row removed → FAIL at `:307`; both restored green
- Skill/formatting: `gofmt -l` on the four edited files — clean; `go build ./...` — clean
- Commit: `e9d72669` (peer-alias row + the three fixture pins, − `memory_sync_delta_test.go` site committed separately)
- Worktree: `git status --porcelain` clean at close-out (Rule 30)

## Acceptance re-run (independent pass over the finished branch)

Every acceptance claim was re-measured from the finished tree, red and green in the same
session — the before/after pair below is one instrument read twice, not two reports.

| claim | before (master `249b7938`, `~/work/GoPi`) | after (branch `98bc26e5`, `~/workspace/gopi-peer-alias-inv`) |
|---|---|---|
| `-run TestPeerAliasGuardDocumentedAtEverySSHLeg ./internal/tools/` | `FAIL … peer_alias_test.go:307: ledger_backup.go spawns ssh but names no validating site` / `0.737s` / **exit 1** | `ok … 0.748s` / **exit 0** |
| `-run 'TestBidir\|TestRollback\|TestSyncClaim\|TestExportClaim\|TestRunClaimLock' ./internal/tools/` | **exit 1** (named instance: `TestRollbackLocalClaimRevertsToPending` → the `role=client` + no-`sync_to` refusal) | `ok … 71.356s` / **exit 0** |
| mutation A (validator call removed, still compiling) | — | `FAIL … :317 is recorded as validating directly but never calls the peer-alias helper` / **exit 1** → restored → `ok … 0.748s` |
| mutation B (inventory row removed) | — | `FAIL … :310 spawns ssh but names no validating site` / **exit 1** → restored → `ok` |
| `go build ./...` | — | exit 0 |
| `gofmt -l` on the four edited files | — | empty |
| `git status --porcelain` | — | empty (Rule 30) |

Both mutations were reverted from `cp` backups and the restore was verified with both the gate
(`ok`) and `git diff --stat` / `git status --porcelain` (both empty) — the red proofs above come
from a mutation that was actually in the tree, and the branch carries none of it.

## Evidence (self-filed)

- **Gate red on master**: `go test -count=1 -run TestPeerAliasGuardDocumentedAtEverySSHLeg ./internal/tools/` in `~/work/GoPi` (master `249b7938`) → `--- FAIL … peer_alias_test.go:307 …` / `FAIL github.com/nick/gopi/internal/tools 0.659s` / `EXIT=1`; captured at `~/workspace/_evid/pa-master-red.txt`.
- **Gate green on branch**: same command in `~/workspace/gopi-peer-alias-inv` → `ok github.com/nick/gopi/internal/tools 0.688s` (exit 0).
- **Mutation A**: after replacing `peer, err := ValidateSSHPeerAlias(peer)` in `ledger_backup.go` → `--- FAIL: TestPeerAliasGuardDocumentedAtEverySSHLeg (0.01s) / peer_alias_test.go:317: ledger_backup.go is recorded as validating directly but never calls the peer-alias helper`; file restored from `~/workspace/_evid/mut-lb.bak`.
- **Mutation B**: after deleting the `"ledger_backup.go": "direct"` row → `--- FAIL … peer_alias_test.go:307: ledger_backup.go spawns ssh but names no validating site …`; file restored from `~/workspace/_evid/mut-pa.bak`.
- **Restore check**: re-running the gate after both restores → `ok github.com/nick/gopi/internal/tools 1.206s`; `git status --porcelain` listed exactly the three intended files.
- **Family, isolation arm**: `for t in <18 names>; do go test -count=1 -run "^$t\$" ./internal/tools/; done` → every one printed `FAIL` (so the red is not a leak/ordering artefact).
- **Family, batch arm on branch**: `ok github.com/nick/gopi/internal/tools 44.465s`.
- **Production-branch proof for the role fix**: `grep -rn "NodeRoleClient" --include=*.go internal/ cmd/ | grep -v _test.go` → 3 hits, all in `internal/config/node_role.go` (definition/parse) and one inside `task_submit_forward.go:276`'s refusal *message*; no production branch tests for `client`.
- **IsServerNode fail-safe**: `internal/tools/memory_sync_delta.go` — `if role, _ := config.ResolveNodeRole(); role != "" { return role == config.NodeRoleServer }` … and the legacy fallback ends `return false // N1 fix: unknown host defaults to CLIENT`.
- **Master whole-package instrument**: `~/workspace/_evid/master-tools-wholepkg.txt` tail → `panic: test timed out after 10m0s` / `running tests: TestTaskRunWritebackTagsExplicitAndEmpty (0s)` / `FAIL github.com/nick/gopi/internal/tools 601.029s` / `MASTER_WHOLEPKG_EXIT=1`. NOTE: the invoking command for this pre-existing evidence file was not recorded in it (`grep -rl wholepkg ~/workspace/_evid` matches nothing); it is quoted as the file stands.
- **Budget constant**: `scripts/test-budget.sh` → `test_budget="25m"`; slowest-test reporting at `:125` (`slowest_report_n=15`).
- **Pre-existing gofmt dirt on master**: `gofmt -l internal/tools/` in `~/work/GoPi` → `task_finish_premerge.go`, `task_staleness_guard_test.go`, `taskledger_cost_test.go`; the same command against this branch's four edited files → empty.
