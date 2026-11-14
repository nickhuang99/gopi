<!-- doc-class: historical -->
# Post-Merge Gate Re-Run (`go vet ./...`) — Report

Cycle: `fix/postmerge-vet-gate` (worktree `/home/user/workspace/gopi-postmerge-vet-gate`,
base master `1c41bc25`). Live merger daemon (pid 3473055) untouched: the change
is a code path it will pick up on its NEXT rebuild, never a restart, never a
reconfiguration.

## Completed

1. **The gate itself — `internal/tools/rebuild_vet_gate.go` (new).**
   `runPostMergeVetGate(repo) *VetGateResult` resolves, runs and REPORTS one
   bounded `go vet ./...` over the repo the round just closed on. Verdict is
   `PASS | FAIL | ABSTAIN` — the same three spellings the build gate already
   uses (`mergeBuildGateStatus*`), not a second vocabulary.

   - **Why vet is the right command** (the epic's premise, re-measured here):
     `go build ./...` never compiles `_test.go`, so a merge that lands a tree
     whose test trees do not compile is reported as a successful round.
     `go vet ./...` type-checks the test trees while `go build` stays green —
     the one command already in the repo's vocabulary that reds on that class.
2. **Wiring — `internal/tools/rebuild.go`.** `RebuildResult` grew a
   `Vet *VetGateResult` field, and `RebuildAfterMerge` fills it from a
   `defer` placed **before any early return**. That placement is the point:
   the close loop's early exits (no build contract, dirty tree, already-current,
   non-GoPi artifact) all now carry a gate verdict too, so no caller has to
   reason about which path measured the tree. A `defer` is also the ordering
   guarantee — the gate runs on the tree the round LEAVES BEHIND, after the
   declared build has done its work.
3. **Report by name + file:line through the EXISTING channel.**
   `VetFailedEvent = BuildFailedEvent`. Deliberately not a new event name: the
   close loop already publishes every "the artifact did not follow master"
   alarm as `build-failed`, operators subscribe BY NAME in
   `GOPI_TASK_NOTIFY_EVENTS`, and a live daemon must not be reconfigured for a
   new measurement to be heard. The payload's summary says which measurement
   spoke (`gate=vet`). A red gate also writes a log line
   (`taskNotifyLog("merger: VET gate FAILED: …")`). Nothing is swallowed
   (Rule 15b): pass, fail and abstention are all reported, an abstention with
   its reason.
4. **Report-only, never a rollback** (Rule 12). A red gate does not touch
   `res.OK` and never sets `res.Err`; it holds no revert handle. The merge is
   done and good, git history is fine, the declared build succeeded — what is
   reported is that the LANDED TREE does not vet.
5. **Bounded, per Rule 1a/2.** One named constant
   `vetGateDefaultTimeoutSec = 600` (2× the build gate's 300s: vet compiles
   the test trees `go build` never reads), exactly ONE reader — `vetGateTimeout`
   with the single env name `GOPI_POSTMERGE_VET_TIMEOUT_SEC`. An unparsable
   value is an ABSTENTION naming the variable, never a silent fallback
   (Rule 15b). The subprocess runs in its own process group with the shared
   `mergeBuildGateWaitDelay`, so a hung compile dies with its children at the
   deadline.
6. **Zero new dependency** (Rule 10.7): the finding is parsed with stdlib
   `strings.Cut` — no vet JSON reporter, no YAML. `go.mod` / `go.sum`
   unchanged (verified by `git diff master...HEAD -- go.mod go.sum` → empty).
7. **Rules 13/14 honoured by reuse, not by mirroring.** The gate does not own a
   second set of path/cache/status strings: it imports the build gate's
   `mergeBuildGateGoCache`, `mergeBuildGateSharedModCache`, `mergeBuildGateEnv`,
   `mergeBuildGateWaitDelay`, `mergeBuildGateUnresolvedModules` and its status
   constants. GOCACHE stays the shared disk-backed sandbox, so the gate cannot
   fill the operator's `~/.cache/go-build`.
8. **Tests — `internal/tools/rebuild_vet_gate_test.go` (new), 12 tests, all
   pass.** The two acceptance tests are behavioural, against a REAL module and
   the REAL toolchain (the gate's whole job is to run a subprocess and read it,
   so a mocked toolchain would measure nothing):
   - `TestPostMergeVetGate_PlantedRedReportsGateAndFileLine` — plants
     `p/z_red.go` calling an undefined symbol, asserts the verdict FAILs,
     names the gate `vet`, carries `z_red.go:4`, and that the DELIVERED notify
     payload carries `build-failed` + `vet` + the file + the line and does NOT
     read as a pass. A verdict nobody receives is the defect this closes.
   - `TestPostMergeVetGate_CleanTreeIsSilent` — clean tree ⇒ PASS and NO event.
   - `TestPostMergeVetGate_ReportsThroughTheCloseLoop` — the wiring: the gate is
     reached from `RebuildAfterMerge`, not only when called directly.
   - `TestPostMergeVetGate_RedIsReportOnlyNeverARollback` — `res.OK` stays true.
   - `TestPostMergeVetGate_TimeoutIsBoundedAndAbstains` — the bound arm, driven
     through the gate's OWN subprocess (`sleep 30` vs a 1s budget). **No host
     load is generated to produce a number (Rule 27).**
   - Plus: not-a-module abstains; unparsable budget abstains naming the var;
     default budget IS the named constant; escape hatch silences; the parser
     against the toolchain shapes measured on this host; `Ref`/`RefFull`.
   - **Red proof (the test has teeth):** with the wiring disabled via the
     escape hatch (`GOPI_POSTMERGE_VET_GATE=0`), the acceptance test FAILS
     (status comes back ABSTAIN, no event) — i.e. the test fails RED when the
     gate is off, not merely when an assertion text changes.
9. **Measured on this host** (Go 1.24.5, TMPDIR on `/dev/shm` — off `/`,
   Rule 22):
   - clean tree: `go vet ./...` prints **nothing** on either stream, exit 0 ⇒
     **silence IS the pass** for this command, and the gate treats a zero exit
     with zero diagnostics as PASS.
   - planted red: exit 1; diagnostics on **stderr** prefixed either `vet: ` or
     bare `<file>:<line>:<col>:`, and `FAIL\t<pkg> [build failed]` on stdout.
   - empty module: exit 1 + `"./..." matched no packages` ⇒ ABSTAIN (not red).
   - warm shared cache, whole repo: `go vet ./...` ≈ **21.3s** — comfortably
     inside the 600s budget on the smoke path.

## Not Done (honest — Rule 26 corollary)

- **Migrations are NOT run.** The raw briefing's "the vet gate and any
  repo-declared migrations" asks for a migration hook: a `migrations` entry in
  the settings `repos` table, a bounded runner, and a reporting story for a
  failed migration. **Nothing in this repo consumes or declares such a thing
  today** (`config.ResolveRepoBuild` knows only `build` / `build_timeout_sec`),
  so inventing the contract here would be a guess dressed as a feature. The vet
  half is the half the epic's acceptance can test with a planted red gate, and
  it is the half that landed. A migration hook is a separate task with its own
  contract to design.
- **`serveRebuild` (cmd/agent) early-returns before `RebuildAfterMerge`** on
  the no-contract and dirty-tree paths, so the gate is not reached there. Those
  paths log today; closing this needs `cmd/agent` fixtures for a dirty/no-contract
  repo. It is a reach gap, not a wrong report — the merge/verify path the epic
  names (a round that merged and rebuilt) always reaches the gate.

## Verification

- **Build**: `go build ./...` → **exit 0**.
- **Vet**: `go vet ./...` over this branch → **exit 0**.
- **The repo's OWN declared test gate — green**:
  `bash scripts/test-budget.sh ./internal/tools/` → **exit 0**, 472s wall,
  `packages: 1 finished, 0 with a failure`, `outcome=pass` (loaded host: load
  16.3→5.6 on 4 cores). The new tests appear in its slowest-15 list
  (`PlantedRed…` 11.29s, `CleanTreeIsSilent` 10.81s, `ReportsThroughTheCloseLoop`
  6.90s) — comfortably inside that gate's 25m budget.
- `go test -count=1 -run TestPostMergeVetGate -v ./internal/tools/` → **9/9 PASS**
  in 60.0s (the new tests' isolated wall time).
- `go test -count=1 ./internal/standards/` (Rules 16–21 gates) → **ok 30.7s**.
- **A pre-existing over-budget, recorded honestly rather than blamed on this
  branch**: raw `go test -count=1 ./internal/tools/` FAILs at **Go's default 10m
  per-package budget** (605.3s, `panic: test timed out`, goroutine dump of idle
  `database/sql connectionOpener` goroutines). The SAME package on **master
  `664b3784`** takes **692.7s** (`WALL=708.38s`) — it passes only because that
  run was given `-timeout 900s`, and it would already fail at the default. So the
  whole-package wall time on this loaded host sits at/over the default budget
  **independently of this change**, which is exactly what
  `scripts/test-budget.sh`'s own header documents ("on this host that default is
  not a hang detector, it is a load detector … the reported verdict is then
  `FAIL … 600.3xx s` — text that reads like a failing assertion while proving
  nothing about the code"). **No load was generated to produce any of this
  (Rule 27)** — the numbers are other tenants' load plus the ledger first-open
  cost.
- `go test ./internal/stylegate/` → FAILs, but only on sites this branch does
  NOT touch (`internal/tools/merge_union_gate.go:609` ignored-error,
  `cmd/agent/cli_tasks_exec_output_test.go:181` unchecked-close);
  `grep -c rebuild_vet_gate` over its output = **0**. Pre-existing on master.
- **Commit**: `4fdb6684` on `fix/postmerge-vet-gate` (worktree-shared, so it is
  already in the main repo — no push needed, Rule 6c).

## Files

- `internal/tools/rebuild_vet_gate.go` (new, ~470 lines)
- `internal/tools/rebuild_vet_gate_test.go` (new, 12 tests)
- `internal/tools/rebuild.go` (modified: `RebuildResult.Vet` + the deferred call)
- `devdoc/postmerge-vet-gate.md` (this report)
