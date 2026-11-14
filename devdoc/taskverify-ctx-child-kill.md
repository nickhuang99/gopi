<!-- doc-class: historical -->
# Task Report: archive-tag recovery — `archive/fix/taskverify-ctx-child-kill`

**Verdict**: **SALVAGE** — all three archived commits carry work that master still
lacks, and the defects they fix are reproducible on current master (red tests
below). Re-derived onto a FRESH branch off master; the archived branch/tag was
**read only** (Rule 23) and is untouched.

## Deliverable declaration

- **Branch**: `fix/taskverify-ctx-child-kill-salvage` in the shared repo
  `/home/user/work/GoPi` (worktree `/home/user/workspace/gopi-taskverify-ctx`).
- **Recovery**: `git show fix/taskverify-ctx-child-kill-salvage:devdoc/taskverify-ctx-child-kill.md`
- **Commits**: 3 over master (`git log --oneline master..HEAD`): `7be557cd`
  (prod fix + CLI routing), `0f6ef324` (regression tests), and a docs commit
  carrying this devdoc + its DOC-INDEX row.
- A linked worktree shares the main repo's refs — the commits are already in the
  main repo, no push needed.

## 1. The tagged work, and the instruments (all re-run live, 2026-09-19)

Tag under audit: `archive/fix/taskverify-ctx-child-kill`, tip
`2fc3edabff1b65bd5599dc78709de189e232de2e`, merge-base with master
`114cf2658c7e5fe5ea7dc568f87723a9e939fd23`.

```
$ git cherry -v master archive/fix/taskverify-ctx-child-kill
+ 2dc8ed4abbefdf103b855d8389a8d13e4b7effd0 fix(tools): TaskVerify owns its child ctx — WaitDelay, opts.Timeout, ctx cause
+ db3d05c92711f4f8faf5f77c73c17132549aad80 refactor(cmd): gopi task verify --timeout routes through TaskVerifyOptions.Timeout
+ 2fc3edabff1b65bd5599dc78709de189e232de2e docs: task report for the TaskVerify ctx-ownership fix

$ git rev-list --count master..archive/fix/taskverify-ctx-child-kill
3
$ git rev-list --count archive/fix/taskverify-ctx-child-kill..master
2974
$ git merge-base --is-ancestor archive/fix/taskverify-ctx-child-kill master ; echo $?
1                                            # NOT landed
$ git rev-parse archive/fix/taskverify-ctx-child-kill
2fc3edabff1b65bd5599dc78709de189e232de2e     # tag untouched, tip sha unchanged
```

Every cherry line is `+` (no upstream equivalent) and the tag is not an ancestor
of master, with 2974 commits of master drift since the archive base.

## 2. Per-unit triage (prod fix vs test vs devdoc)

| archived unit | class | verdict | evidence |
|---|---|---|---|
| `2dc8ed4` `internal/tools/tasks.go` (+46) | prod fix, 3 parts | **SALVAGE** | all three parts still absent on master *and* red-testable (§4) |
| `db3d05c` `cmd/agent/cli_tasks.go` (16±) | prod refactor (Rule 2) | **SALVAGE** | master `runTaskVerify` still wraps its own `context.WithTimeout` (`cmd/agent/cli_tasks.go:3786`) while `TaskVerifyOptions.Timeout` is never read |
| `2fc3edab` `devdoc/taskverify-ctx-child-kill.md` (+122) | devdoc | **OBSOLETE (record)** | it is a run record, not code; not ported verbatim — its premise section cites master line numbers that have since moved and a sibling test on the unmerged `feat/cov-tools-rest` branch. This file is its re-derived replacement. |
| `internal/tools/taskverify_ctx_test.go` (+412) | test | **SALVAGE (re-derived)** | the four tests are the repro; ported onto master's symbols and re-run (§4) |
| `internal/tools/testmain_test.go` (+9) | test hook | **SALVAGE (adapted)** | master's `TestMain` grew a lot since the archive base (`PinSuiteForTestMain` / `PinSuiteGopiProfile` / build-cache sandbox); only the 9-line child-mode interception was re-applied, master's body kept |

The three prod parts, each verified absent on master **by symbol** (not by
commit count):

```
$ git grep -n taskVerifyWaitDelay master      → only a devdoc audit list, no decl
$ grep -n "TaskVerifyOptions" master internal/tools/tasks.go
  → Timeout documented as "the parent-side wall-clock cap", never applied
$ grep -n "WaitDelay" master internal/tools/tasks.go → NONE
  (master DOES use WaitDelay in the merge gates — merge_build_gate.go:685,
   merge_union_gate.go:647, and also merge_pass_verify.go:401, a file DELETED on
   2026-11-01 with the retired top-level `gopi merge` verb's post-pass
   verify/withdraw lane — the verify child was simply never given it)
```

Master evolved past the archive base in this function (it now also carries
`taskVerifyGround` / `child.Dir` / the stripped report contract / `%w`), so the
change was **re-derived**, not cherry-picked: the archived `%v` had already been
fixed to `%w` upstream, and the review's other items (pre-spawn dead-ctx check,
`ctx` naming) are already present on master.

## 3. What was changed (surgical, Rule 4)

`internal/tools/tasks.go`
1. new named const `taskVerifyWaitDelay = 10 * time.Second` (Rule 1) and
   `child.WaitDelay = taskVerifyWaitDelay` — the verify child is a *gopi* that
   spawns `go test`/bash helpers; a grandchild inheriting its stdout pins the
   parent's pipe copies, so `Run()` blocked past the kill (the stuck-verify
   mechanism, Rule 1b-precise: this is `exec.Cmd.WaitDelay`, not a function).
2. `TaskVerifyOptions.Timeout` is now **applied**: `Timeout > 0` derives
   `context.WithTimeout`; `0 = unlimited` (the deliberate default) unchanged.
3. the ctx cause is surfaced (` (ctx: context canceled)`) — a ctx kill used to
   read only as `signal: killed`.

`cmd/agent/cli_tasks.go` — `runTaskVerify` passes `Timeout: opts.verifyTimeout`
through instead of wrapping a second `context.WithTimeout`: one deadline, one
implementation (Rule 2). `--timeout N` parsing tests are unchanged and green.

## 4. Red-then-green proof (the defect is live on master)

RED — master's production code (`git checkout -- internal/tools/tasks.go
cmd/agent/cli_tasks.go`) + the new tests (a temporary probe const existed only
so the package compiled; removed afterwards):

```
--- FAIL: TestTaskVerifyCtxCancelKillsChild (0.81s)
    cancelled ctx: cause not surfaced in task verify: FAIL: child gopi run: signal: killed
--- FAIL: TestTaskVerifyTimeoutBoundsChild (62.53s)
    opts.Timeout=2s was IGNORED — TaskVerify still running
--- FAIL: TestTaskVerifyWaitDelayBoundsPipeHold (25.53s)
    TaskVerify held past WaitDelay (10s) — the grandchild's pipes still pin Run()
FAIL	github.com/nick/gopi/internal/tools	90.364s
```

GREEN — with the fix restored:

```
--- PASS: TestTaskVerifyCtxCancelKillsChild (2.81s)
--- PASS: TestTaskVerifyTimeoutBoundsChild (7.17s)
--- PASS: TestTaskVerifyWaitDelayBoundsPipeHold (11.30s)
ok  	github.com/nick/gopi/internal/tools	22.045s
```

`TestTaskVerifyDeadContextFailsClosed` (4th test) is green **both** ways — it
pins behaviour master already had via `exec.CommandContext` (no child spawned
for an already-dead ctx), kept as the guard the two changes must not regress.

## 5. Files

- `internal/tools/tasks.go` — const + `WithTimeout` + `cmd.WaitDelay` + ctx cause
- `internal/tools/taskverify_ctx_test.go` — 4-test regression suite (NEW)
- `internal/tools/testmain_test.go` — 9-line child-mode interception hook
- `cmd/agent/cli_tasks.go` — `--timeout` routed through `TaskVerifyOptions.Timeout`
- `devdoc/taskverify-ctx-child-kill.md` + `devdoc/DOC-INDEX.tsv` — this report + its index row

## 6. Verification

- Build: `go build ./...` → OK (no output, exit 0)
- `gofmt -l` on the four touched files → empty
- `go vet ./internal/tools/ ./cmd/agent/` → clean (no output)
- Branch-scoped tests (Rule 27 — the touched packages ONLY, never the tree):
  `make test-budget PKGS='./internal/tools/ ./cmd/agent/'` with
  `GOPI_BIN=<worktree>/bin/gopi`, budget 50m. **This run did NOT go green**, and
  the honest record is below (see `## Partial: the whole-package gate on a
  shared host`). What IS green and RE-verified against this branch: the 4 new
  regression tests and the whole `TestTaskVerify` family (16 tests), the
  `cmd/agent` tests the diff touches (`TestParseTasksArgsVerify`,
  `TestTaskFlagMemberVerify`, `TestVerifyGateLandingCarriesTheDurableCloseLoop`),
  `go build ./...`.
- Pre-existing gate failures are RED ON MASTER ITSELF, measured in a mirror
  worktree of master with none of this branch's changes: `TestAssertionTotalityAuditGate`
  (assertion_totality_audit_test.go:118, four unclassified negative-totality
  assertions in `task_merged_cleanup_test.go` / `rebuild_vet_mergeset_test.go` /
  `wip_worktree_repro_test.go`) and the docclass/docanchor family. None of them
  names a file this branch touches.

## Partial: the whole-package gate on a shared host (honest)

`make test-budget PKGS='./internal/tools/ ./cmd/agent/'` was attempted four
times; none reached a green whole-package exit, and the failures are
HOST-and-MASTER conditions, not this branch's assertions:

| attempt | instrument | outcome |
|---|---|---|
| plain `go test ./internal/tools/ ./cmd/agent/ -count=1` | Go's default 10m per package | killed at 10m — `panic: test timed out` with idle `database/sql.connectionOpener` goroutines (the host was at load 10-12 with two other agents' suites running), exactly the condition `scripts/test-budget.sh` was written for |
| `make test-budget` (25m, GOPI_BIN unset) | project gate | `bin/gopi absent` → the CLI E2E guards FAIL by design (`TestTaskListReadPathE2E`: "set GOPI_BIN to THIS tree's build"); rebuilt the binary and re-ran |
| `make test-budget` (25m, GOPI_BIN set) | project gate | exit 3 TIMEOUT at 1504s (host 6.05→7.97): `internal/tools` and `cmd/agent` both exceeded 25m while three other agents' suites ran (load 6-12). Named failures in that run were pre-existing gates (below) |
| `make test-budget` (50m, GOPI_BIN set) | project gate | still not green: `TestAssertionTotalityAuditGate` + `TestDocAnchorsResolveInNormativeDocs` FAIL (both also FAIL on master, mirror worktree `9e6d7844`, verified) and one tools test flaked (`TestTaskRunningSuffixMonotonicAcrossRearm`, `age = "4h0m1s", want "4h0m0s"`) |

Root cause, measured rather than guessed: the suite's first ledger open pays the
host's block device (`test-budget` itself prints "the ledger sandbox … is on
block-backed filesystem type 0xef53 … a loaded host costs a suite more wall clock
than its budget assumes"), and this host carried 2-3 concurrent foreign suites
throughout (load 6-12 on 4 cores). The reading is NOT "tests failed" — it is
"the budget expired on a loaded host", which is how `test-budget.sh` reports it
(exit 3, never 1).

What this does NOT do: it does not weaken the gate. No whole-tree run was ever
attempted (Rule 27), no load was generated (Rule 27's ban), and the one test
that flaked is named here rather than retried until it passed.
- Gate honesty note: `go test ./internal/standards/` is **RED on master itself**,
  in a separate mirror worktree at master `07198611` with none of this branch's
  changes (measured, output below) — pre-existing docclass debt of other
  branches. This branch's own DOC-INDEX row is present and is not among the
  gate's complaints, so no new gate debt is added here.
- `git merge-base --is-ancestor archive/fix/taskverify-ctx-child-kill HEAD` → 1
  (the salvage did NOT land the archive ref; the archive branch/tag remains).
- `git status --porcelain` in the worktree → empty (Rule 30).

## Not Done (honest)

- **No CLI-level E2E with the real binary** (the archived report ran
  `gopi task verify <id> --timeout 2` against a sandboxed profile). The
  in-process red/green run above exercises the same `exec.CommandContext` +
  `WithTimeout` + `WaitDelay` path with real processes, real signals and real
  reaping, and it is the instrument that reproduces the defect deterministically;
  a full E2E additionally needs a real verify child and a live ledger, which is
  the merger's path. Not run here; not claimed.
- **Rule 11 (LLM adoption proof) is N/A as written**: no new tool, no prompt or
  routing change — `TaskVerify` is reached from `gopi task verify` / the merge
  gate, so there is no tool_use trace to observe.
- The archived devdoc was **not** ported verbatim (it is a record of the earlier
  run, and this report is its replacement) — so the archive's third commit has
  no counterpart commit here by design.
- Nothing was merged; the tag and branch were not deleted (PA decides).

## Evidence (self-filed)

- red run: `go test -run 'TestTaskVerifyCtxCancelKillsChild|TestTaskVerifyTimeoutBoundsChild|TestTaskVerifyWaitDelayBoundsPipeHold' -count=1 ./internal/tools/` on master's prod code → `FAIL github.com/nick/gopi/internal/tools 90.364s`, three named failures quoted in §4.
- green run: same command with the fix → `ok github.com/nick/gopi/internal/tools 22.045s`, three PASS lines quoted in §4.
- `go build ./...` → exit 0, no output. `gofmt -l <4 files>` → no output. `go vet ./internal/tools/ ./cmd/agent/` → no output.
- `git cherry -v master archive/fix/taskverify-ctx-child-kill` → three `+` lines (§1). `git rev-parse archive/fix/taskverify-ctx-child-kill` → `2fc3edab…` (unchanged before and after this run).
- `git merge-base --is-ancestor archive/fix/taskverify-ctx-child-kill HEAD` → exit 1.
- `git status --porcelain` (worktree) → empty.
- gate baseline (mirror worktree at master `07198611`, none of this branch's changes):
  `go test ./internal/standards/ -count=1` → `--- FAIL: TestDocManifestCoversEveryDevdocFile`,
  `--- FAIL: TestStandardGates`, `--- FAIL: TestStandardGateDataReport`,
  `FAIL .../internal/standards 36.195s`; the docclass complaints name
  `devdoc/cli-links-carrier.md`, `devdoc/raw-round-grammar-archive-remeasure.md`,
  `devdoc/taskid-whitespace-normalize-salvage.md`,
  `devdoc/web-m2-b5b8b9b12-remeasure.md`, `devdoc/worker-term-exit-salvage.md`
  and a duplicate row for `devdoc/daemon-death-pipe-fd-shape.md` — none of them
  this branch's files. `git worktree remove --force` afterwards.

### Second measurement pass (same run, later — the numbers this report is filed on)

The host stayed loaded (load 6-12 on 4 cores, 2-3 concurrent foreign suites), so
the whole-package gate was re-read rather than retried blindly:

- `go test ./internal/tools/ -run TestFirstOpenBlockDeviceCostIsTheSync -count=1 -v`
  **alone** → `--- PASS … (110.53s)`, `ok github.com/nick/gopi/internal/tools 110.853s`
  — i.e. the test that timed out inside the 10m whole-package run is green in
  isolation, which locates the budget expiry in the HOST's aggregate load, not in it.
- `go test ./internal/tools/ -run TestTaskVerify -count=1 -v` → all 16
  (`TestTaskVerify*`) PASS, `ok … 42.393s`; the 4 new ones:
  `TestTaskVerifyCtxCancelKillsChild (1.77s)`, `TestTaskVerifyTimeoutBoundsChild (3.49s)`,
  `TestTaskVerifyWaitDelayBoundsPipeHold (11.38s)`, `TestTaskVerifyDeadContextFailsClosed (3.56s)`
  (first three FAIL on master's prod code — §4).
- `go test ./cmd/agent/ -run 'TestParseTasksArgsVerify|TestTaskFlagMemberVerify|TestVerifyGateLandingCarriesTheDurableCloseLoop|TestHomeFlagBeatsEnv' -count=1 -v`
  → 4 PASS, `ok github.com/nick/gopi/cmd/agent 5.416s` (with `bin/gopi` rebuilt in the
  worktree, which the CLI E2E guards require).
- pre-existing RED, cross-checked in a mirror worktree of master `9e6d7844`
  (no branch changes): `go test ./internal/tools/ -run 'TestAssertionTotalityAuditGate|TestTaskRunningSuffixMonotonicAcrossRearm'`
  → `--- FAIL: TestAssertionTotalityAuditGate (0.62s)`, `FAIL …/internal/tools 5.861s`
  — so that gate is master's, not this branch's.
- `make test-budget PKGS='./internal/tools/ ./cmd/agent/'` (GOPI_BIN set,
  25m then 50m) → exit 3 TIMEOUT / not green on the loaded host; stamped
  `elapsed=1504s … outcome=timeout` in the run's own stamps log. No whole-tree
  run was attempted (Rule 27); no load was generated (Rule 27's ban).

Note on drift: master advanced during this run (`07198611` → `9e6d7844`, other
agents merging); the branch is off `07198611` and therefore predates those merges.
It is not stale in the parts this diff touches (none of those files moved), but a
rebase may be owed at merge time — reported, not hidden.
