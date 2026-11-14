<!-- doc-class: historical -->
# Task Report: master's three pre-existing reds — Rule 17 ×2, the stale COMMANDS.md block, and the third that measured green

Task `01a11600-baeb-7121-a470-48fee2236732`, branch `task/01a11600baeb7121a47048fee2236732`.

Three reds were reported from another task's branch-scoped budget run. Each one
was measured on this tree BEFORE anything was changed, and each one got the
remedy the code actually needed — which was not the same remedy for all three.

---

## 1. Rule 17 (test env isolation): 2 NEW sites — ESCAPE, not re-shape

Measured on arrival (not taken from the briefing):

```
$ go test -count=1 -run 'TestStandardGates/TestGate2TestEnvRedProof' ./internal/standards/
gate_test.go:101: Rule 17 (test env isolation): 106 total hits, 66 baselined, 38 escaped, 2 new (unannotated)
gate_test.go:113: NEW violation at cmd/agent/chat_session_budget_test.go:58  [os.Setenv/os.Unsetenv … use t.Setenv]
gate_test.go:113: NEW violation at cmd/agent/chat_session_budget_test.go:87  [os.Setenv/os.Unsetenv … use t.Setenv]
```

**Remedy chosen: reviewed inline escape (`// env-leak-ok:`). NOT the shape fix,
and NOT baseline.go.**

Why not the shape fix. Rule 17's remedy order is *fix the shape → reviewed inline
escape → never widen baseline.go*. This task's default expectation was that the
two sites merely "lack the marker". The check is whether a `t.*` API can express
the case, and here it **cannot** — for both sites, in different ways:

- **:58 (`os.Unsetenv`)** — the SUBJECT of the test is the variable's ABSENCE.
  `t.Setenv` can only ever make a variable PRESENT (holding `""`); it has no way
  to express "unset". The case asserts that a chat run inheriting *nothing*
  publishes unlimited — an empty-but-present variable is a different input, and
  the implementation branches on presence, not on the value.
- **:87 (`os.Setenv`)** — the flip is **PER-CALL**: the value comes from the
  table (`"20"`, `"0"`, `"1"`), so call N's value must be gone before call N+1.
  `t.Setenv`'s undo scope is the whole TEST, which would leak call 1's value into
  call 2 — the exact failure `internal/stylegate`'s `RuleTestSetenvScope` exists
  to catch. Both sites already pair the set with `testutil.SnapshotEnv`, which is
  the reviewed per-call shape (Rule 17's "explicit os.Setenv + restore pair").

So the calls are correct as written and neither could be converted. The escape is
the right door.

Why the marker had to go on the SAME LINE (and this is the load-bearing detail).
Rule 17's classification is `markerOn(linesAbove(src, line), escape)` and
`linesAbove` (internal/standards/gate.go:642) walks upward from the flagged line,
stopping at the **first non-comment line**. At :58 the line above the call is
`defer testutil.SnapshotEnv(...)` — **code**, which ends the window — while the
existing `// Rule 17: a PER-CALL flip …` prose sits three lines further up,
behind that code line, therefore out of the window. That is precisely why the
file looked annotated and still measured NEW.

Adding comment lines above the call was rejected deliberately: escapes are keyed
by LINE, and this repo carries 66 line-keyed Rule 17 baseline entries, so
inserting lines above a site is how you shift somebody else's permit and turn one
red into a NEW/STALE pair. A trailing same-line marker is contiguous **by
construction** and shifts **nothing** — it is also the in-tree canonical shape
(`internal/testutil/tmproot_assert_test.go:152`).

Applied (both markers verbatim, reason honest per site):

```go
if err := os.Unsetenv(chatSessionBudgetEnv); err != nil { // env-leak-ok: testutil.SnapshotEnv restores this on test return, ABSENCE included — the case's SUBJECT is the variable being absent, which t.Setenv cannot express (it only ever sets a present value)
```

```go
if err := os.Setenv(chatSessionBudgetEnv, value); err != nil { // env-leak-ok: testutil.SnapshotEnv restores this on test return, absence included — the flip is PER-CALL (call N's value must be gone before call N+1), so it is torn down by the defer this file pairs with every set //stylegate:ignore SnapshotEnv IS the explicit restore: the pair is torn down when THIS closure returns, which is the per-call scope the case is about, not at test cleanup
```

**Two markers, not one — measured, not assumed.** `internal/stylegate` runs its
own independent sweep of the same sites under a different literal
(`//stylegate:ignore`, no space) and does not read `// env-leak-ok:` at all.
Before this change `TestStyleGate` reported `stylegate test-setenv: 1 test
hit(s), ceiling 0 — cmd/agent/chat_session_budget_test.go:87`. The `env-leak-ok`
marker silences the standards gate; the `stylegate:ignore` literal silences
stylegate. A site that legitimately needs both carries both — the precedent is
`internal/testutil/tmproot_assert_test.go`, where the two markers share one line.

**baseline.go: untouched.** `git diff master --stat -- internal/standards/baseline.go`
is empty. The ratchet only shrinks; re-keying is a separate task.

After:

```
gate_test.go:126: Rule 17 (test env isolation): hits=106 baselined=66 escaped=40 NEW=0
gate_test.go:129:   escape: cmd/agent/chat_session_budget_test.go:58  [os.Setenv/os.Unsetenv …]
gate_test.go:129:   escape: cmd/agent/chat_session_budget_test.go:87  [os.Setenv/os.Unsetenv …]
```

Arithmetic reconciles: `106 = 66 + 40 + 0`, escapes 38 → 40 (+2 NEW → escapes),
baselined unchanged at 66. Both sites still read **58 and 87** — the trailing
form shifted no line, so no baseline entry moved.

## 2. COMMANDS.md's generated verb index: REGENERATE from its own generator

Measured: `TestCommandsDocGeneratedBlockMatchesSource` failed with exactly the
reported delta (row `gopi scratch-gc`, got `cli_scratch_gc.go:507`, want `:521`),
and the block's staleness also broke `TestCommandsDocGateRedProofStaleGeneratedBlock`
on its **precondition** (`commands_doc_gate_test.go:589`) — the red proof could
not even arm while the committed block was stale. One root cause, two failures.

**Remedy chosen: regenerate with the doc's own generator. Never hand-edited.**

```
$ go run internal/standards/testdata/commands-census.go . COMMANDS.md
commands-census: wrote COMMANDS.md (44 verbs)
$ git diff --stat
 COMMANDS.md | 2 +-
```

The diff is exactly the one stale row, `507` → `521`. The generator owns the
block; editing the number by hand would have made the document agree with the
test while disagreeing with its own generator.

**Regenerated a SECOND time after the delivery-point sync, and that is not
redundancy — it is Rule 30a.** The alignment merged master's newer commits, which
had themselves shifted `cmd/agent/cli_tool.go`, so the block went stale again on a
DIFFERENT row (`gopi tool`, `431` → `462`). Master tip `5276068c1` is itself stale
on that row: its committed COMMANDS.md says `cli_tool.go:431` while its own source
has the anchor at 462 — the drift arrived with the merge and belongs to master, not
to this branch. Re-running the same generator produced the one-row fix and all 12
`TestCommandsDoc*` subtests pass on the merged state. Sequence followed: finish work
→ commit → **then** align → regenerate anything the alignment invalidated.

## 3. `TestCLINamingExceptionsAreStillDispatchable`: reported red, MEASURES GREEN

**Remedy chosen: none — no change made.** Measured before and after every other
work in this task:

```
$ go test -count=1 -run TestCLINamingExceptionsAreStillDispatchable ./cmd/agent/
ok  	github.com/nick/gopi/cmd/agent	1.517s
```

The test exists (`cmd/agent/cli_completion_exceptions_test.go:76`) and passes on
this tree. It is **not** recorded as a fix here, because nothing was fixed — per
Rule 5's honest accounting, claiming a repair for a green test would be a
fabricated result. The most likely reading of the original report is that it was
observed in a different worktree/commit, or mis-attributed from the same budget
run's noise. No third red existed to fix.

---

## Not Done

- **`make test-budget PKGS='./internal/standards ./cmd/agent'` exits 1 on
  `./cmd/agent`'s PRE-EXISTING failures — measured, not assumed, and NOT caused by
  this branch.** This diff touches **zero production Go files**
  (`git diff master...HEAD --name-only -- '*.go' | grep -v _test.go` → nothing):
  two trailing comments in one test file, `COMMANDS.md`, the devdoc index. The
  budget run's failures are in six unrelated families
  (`TestSyncTaskIDRefusedOnEverySessionFace`, `TestMergePromptCoversEveryRequiredSection`,
  `TestMergeUnionRowCheckOnTheRealManifest`, `TestLedgerOpenersUseProductionDSNContract`,
  `TestResumeKeepsADirtyWorktree`, `TestWorkerResumeKeepsTheGuardsIntact`), and they
  were PROVEN pre-existing by running them on a pristine detached worktree at
  master's tip `5276068c1`, with this branch absent:

  ```
  $ go test -count=1 -run 'TestSyncTaskIDRefusedOnEverySessionFace|TestMergePromptCoversEveryRequiredSection|TestMergeUnionRowCheckOnTheRealManifest' ./cmd/agent/
  --- FAIL: TestSyncTaskIDRefusedOnEverySessionFace (0.00s)
  --- FAIL: TestMergePromptCoversEveryRequiredSection (0.00s)
  --- FAIL: TestMergeUnionRowCheckOnTheRealManifest (0.47s)
  FAIL	github.com/nick/gopi/cmd/agent	0.724s
  ```

  They also fail in ISOLATION (single `-run` set, no budget machinery), so neither
  this change nor the runner is implicated. `TestResumeKeepsADirtyWorktree` fails on
  a missing fixture path under `/dev/shm/suite-cmd-agent-run-*` — the host was
  carrying load average 17–20 on 4 cores from concurrent runs during the budget
  attempt. **In scope: 0 failures** — the three reds named in the epic are all
  green. The residual `cmd/agent` reds are other families, red on master too, and
  are named here rather than absorbed (Rule 26).

- **`internal/lineedit/ambiguous_width_test.go:148`** — red under
  `internal/stylegate`'s `test-setenv-scope` (1 hit, ceiling 0). PRE-EXISTING on
  master and **byte-identical to it** (`git diff --quiet master -- internal/lineedit/`
  → identical), untouched by this diff, and a DIFFERENT family from the three reds
  scoped here. Left alone deliberately; named so it is dispatchable rather than
  silently absorbed (Rule 26).
- **`stylegate ignored-error` (6 production sites)** — also measured on master,
  also a different family, untouched.
- **Re-keying the 66 line-keyed Rule 17 baseline entries** — never in scope
  (the ratchet only shrinks; a re-key is its own task).
- **`go test ./internal/stylegate/` stays red** — on the two pre-existing families
  above, NOT on any site this task touched (`test-setenv: 0` after the change).


## Verification

- Build: `CGO_ENABLED=0 go build ./...` — the packages compiles; `go test`
  compiled and ran the packages named below.
- Tests: `make test-budget PKGS='./internal/standards ./cmd/agent'` —
  see `## Evidence` for the run's own output.
- Ran (the exact invocations, verbatim):
  - `CGO_ENABLED=0 go test -count=1 -run 'TestStandardGates/TestGate2TestEnvRedProof' -v ./internal/standards/`
  - `CGO_ENABLED=0 go test -count=1 -run TestStandardGateDataReport -v ./internal/standards/`
  - `CGO_ENABLED=0 go test -count=1 -run 'TestCommandsDoc' -v ./internal/standards/`
  - `CGO_ENABLED=0 go test -count=1 -run 'TestCLINamingExceptionsAreStillDispatchable' ./cmd/agent/`
  - `CGO_ENABLED=0 go test -count=1 -run 'TestChatBudget|TestChatOnce|TestChatHelpChangesNoBudget' ./cmd/agent/`
  - `go run internal/standards/testdata/commands-census.go . COMMANDS.md`
- Result: Rule 17 `PASS` (NEW=0, escapes 40, baselined 66, line numbers 58/87
  unchanged); all 12 `TestCommandsDoc*` subtests `PASS` including the two red
  proofs that could not arm while the block was stale; `cmd/agent` `ok`; the
  generator wrote `COMMANDS.md` and the resulting diff is the single stale row.
- Full `./internal/standards/` package: `go test -count=1 -v ./internal/standards/`
  → `ok github.com/nick/gopi/internal/standards 157.014s`, **153 PASS, 0 FAIL, EXIT=0**.
- `make test-budget PKGS='./internal/standards ./cmd/agent'`: `./internal/standards`
  passed; `./cmd/agent` exits 1 on six PRE-EXISTING unrelated families, reproduced
  on master tip `5276068c1` in a detached worktree with this branch absent (see
  `## Not Done`). **In-scope families: 0 failures.**

## Evidence (self-filed)

- Rule 17 arithmetic: `go test -run TestStandardGateDataReport -v ./internal/standards/`
  → `Rule 17 (test env isolation) (checklist 5.2): hits=106 baselined=66 escaped=40 NEW=0`
  (before the change the same command printed `hits=106 baselined=66 escaped=38 NEW=2`).
- Gate red proof green: `--- PASS: TestStandardGates/TestGate2TestEnvRedProof (3.97s)`.
- Both escapes are listed BY NAME in the report, i.e. the gate counts them as
  escapes and not as baseline permits: `escape: cmd/agent/chat_session_budget_test.go:58`
  and `escape: cmd/agent/chat_session_budget_test.go:87`.
- COMMANDS.md: `git diff COMMANDS.md` shows one changed line: `cli_scratch_gc.go:507` → `:521`.
- baseline.go untouched: `git diff master --stat -- internal/standards/baseline.go` → empty output.
- Third red: `go test -run TestCLINamingExceptionsAreStillDispatchable ./cmd/agent/` → `ok` (measured, no change made).
- Rule 30: `git status --porcelain` in the worktree — prints nothing at close-out.
- In-scope half of criterion 4, re-measured at close-out (the budget job was killed
  when it stalled on the pre-existing `cmd/agent` families, so both halves are run
  directly here):
  `go test -count=1 -run 'TestChatBudget|TestChatOnce|TestChatHelpChangesNoBudget|TestCLINamingExceptionsAreStillDispatchable' ./cmd/agent/`
  → `ok github.com/nick/gopi/cmd/agent 2.953s`; and
  `go test -count=1 ./internal/standards/` → `ok github.com/nick/gopi/internal/standards 49.059s`.
- Generator idempotence (the deliverable-(b) check): re-running
  `go run internal/standards/testdata/commands-census.go . COMMANDS.md` on the
  committed file prints `commands-census: COMMANDS.md already up to date (44 verbs)`
  and leaves `git diff -- COMMANDS.md` empty — i.e. the committed block IS
  `cliindex.Block(verbs)`, not a hand-tuned approximation of it.
