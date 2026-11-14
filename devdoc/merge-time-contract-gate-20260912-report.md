<!-- doc-class: historical -->
# Task Report: merge-time CLI contract gate (2026-09-12)

**Goal.** Make master-red from a new CLI subcommand structurally impossible at
MERGE time. The 2026-09-12 voice_input incident landed `bd3d34a5` on master with
two red tests because `cmd/agent/cli_contracts_test.go`
TestCLIContractInventoryMatchesDispatch fires only in `go test`, which the merger
(`internal/tools/repo_sweep.go` MergerRepoSweep → `task_merge.go` taskMergeOne)
never runs.

**Branch / worktree.** `feat/merge-time-contract-gate` in
`~/workspace/gopi-mergegate` (worktree, shared object store — Rule 6a).

## Completed

1. **`internal/clicontract`** (new package, stdlib only, no git/exec):
   - `ParseDispatchChain` — the `if isXxxSubcommand(<arg>) || …` chain from
     main.go. Captures the predicate's **argument name** as part of the shape
     (the first live run went blind on a renamed local — a gate that silently
     stops checking is worse than none); a chain mixing argument names, carrying
     a non-predicate condition, or shrinking below the floor is an error =
     abstention.
   - `ParseContractMirror` — `cliContractDispatchPredicates`.
   - `ParseDispatcherTokenMap` — the VALUES of `dispatcherOfToken`, the second
     arm of the project's own guard (a mirrored predicate with no token routed
     to it has no contract CASE and is still red on master).
   - `Compare(tipMain, tipMirror, targetMain, targetMirror, tipTokens)` —
     **branch-introduced** drift only, with mirror coverage as the UNION of both
     sides. A predicate counts as tracked only when mirror **AND** token map
     both cover it.
2. **`internal/tools/merge_contract_gate.go`** — `git show` text only (2 files ×
   2 revisions = 4 subprocesses; no checkout, no build, no test, no LLM),
   abstains on unreadable/repo-without-the-anchor/shape change, escape hatch
   `GOPI_MERGE_CONTRACT_GATE=0` (one reader).
3. **Wired into the real merge decision path** — step ⓪ in `taskMergeOne`,
   **before** any `git merge`, so a refusal is master-safe **by construction**
   (no MERGE_HEAD, no index entry). New action `contract-refused`,
   `TaskMergeCounts.ContractRefused`, `RepoSweepResult.ContractRefused`,
   `contract-refused=%d` in both CLI reports, exit 1 on refusal.
4. **Refusal routed into the EXISTING failure path** —
   `repoSweepRouteContractRefusals` submits ONE branch-rework task reusing the
   established lane verbatim (same summary prefix, same dedup query, same tags),
   so the rework row holds the gate and a second tick reports `duplicate`.
   The briefing quotes the measured drift and names the file to fix; it
   explicitly forbids the two wrong reactions (merge master in / delete the
   branch).
5. **Tests** — 10 merge-gate tests + 16 `clicontract` tests, all green.
6. **`devdoc/merge-time-contract-gate-20260912.md`** — design decision (a) vs
   (b), measured cost, what it does/does not catch, honest limits, evidence.
7. **`internal/standards/baseline.go`** — LINE-NUMBER-ONLY move of two existing
   `task_merge.go` Rule 16 / Rule 18 entries that this change's insertions
   shifted. No new permit, no permitted count change.

## Not Done / honest limits

- The gate does **not** build and does **not** run any test. It closes exactly
  one failure mode — the dispatch chain and the contract-suite mirror drifting
  apart — plus its two siblings (mirrored-but-unrouted predicate; removal
  direction). Compile errors and unrelated red tests still land (a demo branch
  carrying only part of the voice feature merged and master then failed to
  compile — the gate correctly said nothing). A general "never land red master"
  gate means running the repo's declared build/test on the merge path, which is
  a separate, much larger decision (Rule 12's `repos.<path>.build` is where it
  would live).
- Order-only drift in the mirror is not refused (set-based comparison) — the
  test-time guard owns order; refusing on a reorder alone would be a
  false-positive the executor cannot act on.
- `cliVerifyVerbs` coverage and inventory usage fingerprints are not compared.
- `internal/stylegate` and `cmd/agent` TestCLIContract are red at BASE for
  `cmd/agent/cli_voice*.go` + `cli_contracts_test.go` reasons owned by the
  concurrent tasks (01a091fb-1cff/-1e7d). Those files were not touched here.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` — **BUILD_OK**
- Format: `gofmt -l cmd internal` — **clean**
- Tests:
  - `go test -run 'TestMergeContractGate|TestMergerSweep_ContractRefusal|TestTaskMerge' ./internal/tools/ -count=1` — **ok** (10 gate tests, all PASS; the pre-existing taskMerge suite still green alongside them)
  - `go test -count=1 -v ./internal/clicontract/` — **13/13 PASS**
  - `go test ./internal/standards/ -count=1` — **ok**
  - `go test ./cmd/agent/ -run TestCLIContract -count=1` — RED at base (the
    incident itself), GREEN on the demo repo's merged master after the gate let
    the fully-fixed branch land.
- **Live acceptance (real blobs)**:
  - RED: unmodified `bd3d34a5` under master `7f0980e2` → `contract-refused`,
    EXIT=1, master BYTE-IDENTICAL, no MERGE_HEAD, branch intact.
  - RED: half-fixed branch (mirror entry, no token-map entry) → `contract-refused`,
    master byte-identical.
  - GREEN: fully fixed branch → `merged`, master advances, and
    `go test ./cmd/agent/ -run TestCLIContract -count=1` **ok on the merged master**.
- Measured cost: **≈ 8.4 ms per branch** (4 `git show` subprocesses, 0 other git
  calls), 200-run mean; ≤20 branches ⇒ ~0.17 s worst case per repo per tick.
  Pinned by `TestMergeContractGate_CostIsBounded` (PATH shim; can fail — a
  doubled read made it report 6 vs the budget of 4).

  > **Budget superseded 2026-09-13** (`feat/tab-order-actuate-poc` false refusal):
  > the gate judges the post-merge WINNER tree, so it reads 3 revisions × 2 files
  > through **ONE `git cat-file --batch` process** plus **ONE `git merge-base`** —
  > exactly 2 subprocesses per branch, still no checkout, no build, no test
  > process, no LLM. The extra cost is the `git merge-base` itself (measured
  > 21.5 ms on a pair 831 commits divergent); a per-blob winner rule would have
  > cost ~10 ms more on top. The old 4-show/0-other figures above are historical.
  > See `devdoc/merge-time-contract-gate-20260913-postmerge-winner.md`.

## Deliverable declaration (Rule 6d)

- **Branch:** `feat/merge-time-contract-gate` in `~/workspace/gopi-mergegate`
  (a linked worktree of the main repo — the commit is already on the branch in
  the shared object store; nothing was pushed, nothing was written to the main
  working directory).
- **Recovery:** `git show feat/merge-time-contract-gate:devdoc/merge-time-contract-gate-20260912.md`
- The worktree is **kept** (not removed) until this branch is accepted.
