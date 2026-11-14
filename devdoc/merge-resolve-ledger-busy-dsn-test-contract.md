<!-- doc-class: historical -->
# Merge resolution: fix/ledger-busy-dsn-test-contract into master

Resolver commit: `53c9fdeb` (parents `4ab3aaca` branch + `99856f0b` master) — a real
two-parent merge commit, `--no-commit --no-ff` reproduction, all hunks resolved,
staged, committed BEFORE any verification (the commit is the deliverable).

## Conflicts reproduced

`git merge master --no-commit --no-ff` → 6 unmerged paths (13 hunks total):

| file | state | hunks |
|---|---|---|
| `cmd/agent/cli_daemon_scope.go` | UU | 3 |
| `cmd/agent/daemon_cgroup_e2e_test.go` | UU | 1 |
| `cmd/agent/daemon_scope_unit_test.go` | AA | 1 |
| `cmd/agent/ledger_dsn_contract_test.go` | AA | 4 |
| `internal/tools/ledger_dsn.go` | AA | 1 |
| `internal/tools/ledger_dsn_test.go` | AA | 1 |

## Resolution policy and why

Every hunk was resolved to **master's side**, and that is a *combination* here, not
a drop: the two sides are not competing edits to the same code, they are two
generations of the same feature, where master's generation is a strict superset of
the branch's and explicitly carries the branch's own measured evidence forward.

- `internal/tools/ledger_dsn.go` — the branch inlined the pragma list in `LedgerDSN`
  (`busy_timeout` + `journal_mode(WAL)`, with its "measured 7 of 8 runs" rationale).
  master keeps the whole DSN contract and **adds** the `ledgerPragmas()` single
  renderer, the third pragma `synchronous(NORMAL)` (measured: a lone fsync costs
  0.7–1.3 s/op on this host), `ledgerSyncMode`, and the new sibling
  `LedgerReadOnlyDSN` (used by `internal/tools/memory_sync_tagcase_test.go`).
  master's doc comment *contains* the branch's zombie-dispose measurement plus a
  second measurement; nothing of the branch's is lost. Taking the branch here would
  have deleted `LedgerReadOnlyDSN`/`ledgerPragmas` — a compile break for a live
  caller, and the branch/baseline conflict class of memory #2013.
- `cmd/agent/cli_daemon_scope.go` — the branch's `daemonScopeDirNameFmt` +
  per-pid `scopeDirName` (cgroupfs dir name) is replaced by master's
  `scopeDirName == scopeUnitName` (one name for both mechanisms, "literally
  scopeUnitName's value", Rule 13/14). The branch's `daemonScopeQuote` (the `sh -c`
  quoting helper) is dropped on master's side **by master's own documented,
  measured decision** (commit `cb536ada`: the `sh -c` wrapper broke the daemon's
  ExtraFiles fd numbering), re-stated inline as the `MERGE
  (fix/ledger-busy-dsn-contract-v2)` note. `daemonScopeQuote` has no remaining
  caller in the merged tree (`git grep`), so its removal is intended, not a loss.
- `cmd/agent/daemon_cgroup_e2e_test.go` — master's assertion matches master's
  no-shell production wrap; the branch's variant asserted the abandoned command
  string.
- `cmd/agent/daemon_scope_unit_test.go`, `cmd/agent/ledger_dsn_contract_test.go`,
  `internal/tools/ledger_dsn_test.go` — the branch's test bodies are subsets:
  master's carry the same assertions plus the scope-dir identity, the
  `ledgerReadOnlyDSNHelperName` / `ledgerIsDSNHelperName` consts, the exempt-permit
  table and the multi-root scan with per-root read-floors.

Completeness is not a judgement call: **all six resolved files are byte-identical to
master's version** (`git show master:<f> | diff - <f>` = 0 lines each), and the
merge introduced no content of its own beyond master's. `git grep -n -E
'^(<<<<<<<|=======|>>>>>>>)'` over the tracked tree: 0 hits; no `MERGE_HEAD`.

## Verification (bounded)

- `CGO_ENABLED=0 go build ./...` → **exit 0** (production builds; catches any
  identifier dropped by a resolution — the memory #2013 failure mode).
- `CGO_ENABLED=0 go build ./cmd/agent` → **exit 0**.
- `go test ./internal/tools/ -run TestLedgerDSNCarriesProductionsContract -count=1`
  → **ok 0.896s** (exercises the resolved `ledger_dsn.go`/`ledger_dsn_test.go`).
- `go test ./cmd/agent/ -run 'TestScopeUnitNameIsPerSession|TestDaemonScopeWrap_OnlyWrapsTheSystemdPath|TestDaemonAwaitReadyQuotesTheLogTail|TestLedgerOpenersUseProductionDSNContract|TestLedgerBareOpenerPredicateReddensOnSyntheticSources'`
  → 2 failures, both **proven pre-existing on master, outside this resolution**
  (see below). `go test ./...` was NOT run: the epic waives it (known pre-existing
  full-suite hang) and it must never block exit.

## Pre-existing master defects found (NOT caused by this resolution)

Both reproduce from master's own content, and the merge neither introduced nor
touched them (`git diff --stat master HEAD -- <file>` is empty):

1. **`cmd/agent` test package does not compile**: `funcBody` is defined twice —
   `cli_tasks_rebuild_pass_test.go:204` and `merger_resume_arm_wiring_test.go:104`
   — `vet: funcBody redeclared in this block`. Both definitions are in master's
   tree (`git grep -n "func funcBody" master -- cmd/agent` lists both), i.e. the
   union in master's own tip commit `99856f0b` ("resolve merge
   fix/ledger-busy-dsn-contract-v2 into master") duplicated a test helper. Every
   test in `cmd/agent` is blocked by this. To run the cmd/agent checks above I
   temporarily moved one file aside and restored it byte-for-byte (worktree clean,
   `git status --porcelain` empty).
2. **master's own ledger gate is red on master's own tree**:
   `TestLedgerOpenersUseProductionDSNContract` flags
   `internal/tools/task_holder_resume_e2e_test.go:54,138,346` for bare
   `sql.Open("sqlite", dbPath)`. That file does not exist on this branch at all
   (it comes from master, commit `362278e2`), the gate file is byte-identical to
   master's, so master alone is red: the gate and the offenders are both master's.

Neither defect is in this resolution's scope (the resolution's job is faithful
fusion, and per the epic only self-inflicted breakage is fixed here); both are
dispatchable from the evidence above.
