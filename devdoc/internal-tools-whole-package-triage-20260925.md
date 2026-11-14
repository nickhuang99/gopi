<!-- doc-class: normative -->
# Task Report: internal/tools whole-package run — the "cross-test interaction" premise is FALSIFIED

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task row: `01a0d5f2d6827665bbe767c43ab942a5`
Worktree: `/home/user/workspace/gopi-01a0d5f2d6827665bbe767c43ab942a5` (branch
`task/01a0d5f2d6827665bbe767c43ab942a5`, base of the briefing `bf9d20e1`,
delivery-point sync tip `45dc19d5`, master `351736f9`)

## Summary

The briefing asked to "isolate the cross-test interaction around
TestBidirEpic4IdempotentRoundTrip" so that `go test ./internal/tools/ -count=1`
exits 0. **There is no such interaction, and there is no hang.**
`TestBidirEpic4IdempotentRoundTrip` is green in every location measured, and the
whole-package run does not panic at it — it *completes* in ~973 s and reports 14
unrelated failures that are **pre-existing on master**.

No code change is warranted by this investigation, so this document is the
deliverable, and the remaining work is decomposed into the four child tasks the
reframe enqueues (see *Remaining work*).

## What was measured

| # | Observation | Command (verbatim) | Result |
|---|---|---|---|
| 1 | Isolated test is green | `unset DISPLAY; go test ./internal/tools/ -run TestBidirEpic4IdempotentRoundTrip -count=1` | `ok 5.473s` (exit 0) — also `ok 3.964s` in the main checkout |
| 2 | Whole package, 1800 s cap | `go test ./internal/tools/ -count=1 -timeout 1800s` | exit 1, **`FAIL ... 973.321s`**, 14 `--- FAIL` lines, **no** `panic: test timed out`, no `goroutine` dump |
| 3 | The briefing's "panic" | `go test ./internal/tools/ -count=1 -timeout 90s` | `panic: test timed out after 1m30s` — the cap tripped inside `TestDeadHolderReclaimLeavesALiveTwinAlone`, which **passes alone in 0.6 s**; the panic location of a timeout is wherever the clock ran out |
| 4 | Package content vs master | `git diff master..HEAD -- internal/tools/` | **empty** — my branch's `internal/tools` is byte-identical to master's |
| 5 | PRISTINE master control | `git worktree add /home/user/workspace/gopi-control-master --detach master` then the same 14-test run | **the same 14 failures** |
| 6 | The operator's main checkout | `cd /home/user/work/GoPi && go test ./internal/tools/ -run '<14 names>'` at `351736f9` | **the same 14 failures** |
| 7 | Alone, ambient env | `go test ./internal/tools/ -run '^TestTaskShowBriefingFromPayload$'` | FAIL |
| 8 | Alone, pristine env | `env -i PATH=... HOME=... go test ./internal/tools/ -run '^<name>$'` | **13 of 14 FAIL alone with no ambient env at all**; only `TestTaskShowBriefingFromPayload` flips to PASS |
| 9 | Cost shape | `ps`/`/proc/<pid>/stat` during run 2 | ~45 s user + ~16 s sys CPU over 970 s wall ≈ **4 % CPU** — cost is device fsync (the suite ledger is on block-backed scratch), not compute and not a blocked lock |
| 10 | Memory #1296's old hang | `go test ./internal/tools/ -run TestImportReRunIsIdempotent -count=1 -timeout 300s` | `ok 1.147s` — bug `t-1788361069`'s fix (WAL-first DDL + per-path init locks) **is in effect; the historical hang is gone** |

`GOPI_HEAVY_TESTS` is unset in this session, so the heavy bidir tests
(`TestBidir2ImportSymmetry`, `TestBidir2ServeTickAutoSync`, …) **self-skip**: the
whole-package run never reaches the bidirectional E2E fixtures at all. That alone
rules out the briefing's "panics during the memory_sync bidir end-to-end tests".

## The mechanism, named

**Family A — expectations that are ENV-SENSITIVE (the one cross-state effect that
is real, and it is the session's env, not another test's).** A gopi worker's
exported session variables flip declared outputs:

* `TestTaskShowBriefingFromPayload` requires
  `submitted_by: human (gopi task submit CLI)`. With the session env present the
  row renders with the **worker's** provenance, the assertion fails, and the
  *same* test passes under `env -i` (observation 8). Whole package + session env
  = 14 FAILs; pristine env = 13.
* Consequence for a reviewer: a run driven by a gopi session sees a state the
  solo test does not. This is why these rows look like "it only fails in the
  suite" — the suite is simply the run that carries the session env.

**Family B — tests broken ALONE, with no ambient env at all** (observation 8 —
13/14). These are real red tests on master, in three sub-groups:

* *Gate-vs-production drift*: `TestEveryWorktreeRemovalArmQuestionsTheGround`
  (the gate at `worktree_removal_ground_gate_test.go:74` demands the ground
  checklist, and production at `run_ground_anchor.go:391` calls
  `removalaudit.WorktreeRemove` without it — the gate reports
  `10 total, 6 checklist-guarded, 3 reviewed-exempt`), and
  `TestDeclaredTopologyWordingHasOneDefinition` (`memory_sync_claim.go`
  re-types the whitelist refusal instead of referencing
  `RequireDeclaredPeer` / `DeclaredPeerGate`).
* *Reconcile mirror close-out*: `TestReconcileMirrorCloseout_{SIGKILL,NonZeroExit,Exit0}`
  — the mirror row stays `in_progress` instead of failed/completed.
* *Fixture/system-drift family*: `TestSyncImportTwoProcessConcurrent` (the child
  process's suite pin declines an inherited `GOPI_MEMORY_DB`, and the test
  asserts on the child's output),
  `TestUuidTaskIDSubmitCreatesSessionContainer` (skeleton metadata lacks
  provenance; legacy-hash display),
  `TestTaskSubmitNoRepoDefaultsToCwd`, `TestFinishCleanupWorktreeHeldByCallerIsRecycled`
  (a caller's own cwd is treated as a competing holder),
  `TestParentGateListingAgreesWithFunnel`, and the three
  `TestTaskShowBriefing*` display-contract tests.

## Remaining work (not this run — decomposed by the reframe)

1. **Family A env-sensitivity** — make the display-contract rows independent of
   the session env (or assert the session-env-absent shape explicitly with
   `t.Setenv`), and land a guard so a worker-driven run and a bare run agree.
2. **Gate-vs-production drift** — ground-checklist arm across the 10 removal
   arms, and the peer-whitelist wording single-definition.
3. **Reconcile mirror close-out** — the three `TestReconcileMirrorCloseout_*`
   rows.
4. **Fixture/system-drift family** — the remaining rows (submit/session
   container, cwd-repo default, caller-cwd holder, parent-gate listing,
   two-process import concurrency, uuid skeleton provenance).

## Not Done (honest)

* **No code fix and no repro test were landed**: the epic's target mechanism does
  not exist (observations 4–8), so a "minimal fix for the cross-test interaction"
  would be a fabricated deliverable. The 14 real failures are pre-existing,
  out of this epic's target, and each needs its own investigation.
* **Acceptance "`go test ./internal/tools/ -count=1` exits 0" is NOT met**, and
  is not mettable by this epic's subject. It needs Family A/B work (above).
* The "isolation repro" of the briefing (`unset DISPLAY; … -run
  TestBidirEpic4IdempotentRoundTrip`) **still exits 0** — as required — but it
  was never failing, so it proves nothing about the package.
* `internal/tools` full-suite cost is ~973 s on this host (load 2–3, 4 cores).
  A bare `go test ./internal/tools/` therefore rides Go's **600 s default cap**
  and reports a timeout panic long before the package is done. The canonical
  path already owns this budget: `make test` → `scripts/test-budget.sh`
  (`test_budget="25m"`, overridable with `GOPI_TEST_TIMEOUT`). A raw invocation
  needs `-timeout 1800s`; that is a property of the suite's cost, not of the
  bidir tests.

## Verification

- Build: `go build ./...` → OK (exit 0)
- Vet: `go vet ./internal/tools/` → OK (exit 0)
- Tests: `go test ./internal/tools/ -count=1 -timeout 1800s` → exit 1,
  `FAIL github.com/the operator/gopi/internal/tools 973.321s`, 14 `--- FAIL` lines, none
  of them a bidir test and none of them a timeout event.
- Ran: `git worktree add /home/user/workspace/gopi-control-master --detach master`
  + `go test ./internal/tools/ -count=1 -timeout 300s -run '<the 14 names>'` at
  `351736f9`; and the same in `/home/user/work/GoPi`.
- Result: **the same 14 failures in all three trees** → pre-existing on master,
  independent of this branch (whose `internal/tools` is byte-identical to
  master's).
- Commit: the report commit on `task/01a0d5f2d6827665bbe767c43ab942a5`.

## Evidence (self-filed)

- Run 2 raw transcript: `/dev/shm/gopi-bidir-full.txt` — 183 049 bytes; line
  `EXIT=1` after `FAIL github.com/the operator/gopi/internal/tools 973.321s`; the 14
  `--- FAIL` lines are at lines 67, 206, 385, 504, 540, 573, 637, 660, 682, 706,
  731, 776, 799, 869.
- Run 3 raw transcript: `/dev/shm/gopi-bidir-dump.txt` — `panic: test timed out
  after 1m30s`, `running tests: TestDeadHolderReclaimLeavesALiveTwinAlone (1s)`;
  the in-flight goroutine stack ends in `modernc.org/libc.Xfsync` via
  `_sqlite3WalClose` ← `initKnowledgeBase` ← `openTasksDB` ← `TaskSubmit`
  (`claim_holder_freshness_test.go:41`). The only `connectionOpener` frame is
  the ordinary `select` at `sql.go:1261` — memory #1296's "connectionOpener
  deadlock" reading of this dump was wrong, and #1296 has been corrected.
- `TestDeadHolderReclaim*` alone: `ok github.com/the operator/gopi/internal/tools
  30.145s` — the family that owns the 90 s-capped panic is green on its own.
- `git diff --stat master..HEAD` → only `devdoc/DOC-INDEX.tsv` and
  `devdoc/pty-agent-channel-feasibility.md` (deletions); `git diff master..HEAD
  -- internal/tools/` → empty.

---

## Family A — LANDED: the display contracts no longer inherit the ambient signature

Task `01a0d625048b7a25a969f65f9b6b3d6e`, worktree
`/home/user/workspace/gopi-01a0d625048b7a25a969f65f9b6b3d6e`, base `aa482262`.
This section is the fix for *Remaining work* item 1 above.

### The briefing's premise, measured again — and partly superseded

The briefing named four red tests. Re-measured on this base:

| test | briefing said | measured (this base) |
|---|---|---|
| `TestTaskShowBriefingFromPayload` | red: demands `submitted_by: human (gopi task submit CLI)`, renders the worker's provenance with the session env | **green both directions** |
| `TestTaskShowBriefingFromSession` | (same family) | **green both directions** |
| `TestTaskShowBriefingMissingSession` | (same family) | **green both directions** |
| `TestTaskSubmitNoRepoDefaultsToCwd` | (same family) | **RED in BOTH directions** |

The three provenance rows were already re-greened by `4f2255ac` ("re-green
internal/tools on master — 6 reds from today's provenance + dedup commits"),
which introduced `testutil.PinHumanTerminal` and pinned
`TestTaskShowBriefingFromPayload` with it. The briefing's description of that row
is the pre-`4f2255ac` state; the row was still listed as a family-A member, and
it is, but its red had already been closed.

`TestTaskSubmitNoRepoDefaultsToCwd` is red for an unrelated reason and in **both**
environments: epic `01a0d55b` replaced the raw-cwd default with
`DurableRepoForSubmit(cwd)` — the repository's MAIN checkout
(`git rev-parse --path-format=absolute --git-common-dir`) — so a package test now
stamps `/home/user/work/GoPi` while the test asserted `<checkout>/internal/tools`.
A stale assertion, not an env defect. Its red was hidden behind the failure count
the env-sensitive rows produced.

### Why the existing pin was not the fix

`PinHumanTerminal` pinned ONE of the derivation's inputs. The derivation reads
several (`config.AgentSpawnSignatureNow`: `AgentSessionEnv`, `HermesSessionIDEnv`,
`HumanTerminalEnv`, the pid-carrying anchor, and procfs `ParentIsWorker`), so a
fixture pinned for the human escape but not for the agent-session evidence is
independent in ONE direction only.

**What is RECORD, and what I measured myself (kept apart deliberately).** The
both-directions reds above ("with a gopi session's env the human-branch row
reddens; under `env -i` the non-human-branch rows redden") are the record of the
earlier triage at master `351736f9`, and the mechanism they name is real — a
fixture asserting a provenance VALUE is measuring the ambient signature. At THIS
base I measured the three provenance rows **green in both directions**:

```
$ go test ./internal/tools/ -count=1 -run '^(TestTaskShowBriefingFromPayload|TestTaskShowBriefingFromSession|TestTaskShowBriefingMissingSession)$'
ok  	github.com/the operator/gopi/internal/tools	3.415s
$ env -i PATH=$PATH HOME=$HOME go test ./internal/tools/ -count=1 -run '^(same)$'
ok  	github.com/the operator/gopi/internal/tools	3.989s
```

(all four commands in this document that name this base were run in the pristine
control worktree at `aa482262`, not in the editing tree.) So the sensitivity was
**latent** here rather than red: `4f2255ac` had already removed the observed
symptom, and the one-input pin is what remained — a dependency that a future
fixture could re-expose, in a direction no bare run shows. Closing it is the
right edit even without a red to point at, and the guard below is what keeps it
closed; the pin's own doc comment carries this reasoning for the next reader.

**Discount one sentence in this branch's own history.** The body of commit
`a5a9cdfc` paraphrases the earlier triage's both-directions framing ("with a gopi
session's env … while under `env -i` …") as though this run had reproduced it. It
had not: at `aa482262` the three rows are green in both directions (the two
commands above), and the mirror direction is unverified. The measurement in this
section supersedes that sentence; the commit is left as written rather than
rewritten, so the correction is visible instead of silent.

### The changes, per file

**`internal/testutil/pinbare.go`** (renamed from `humanclaim.go`, committed
separately as a pure rename so the diff reads against the real predecessor) —
`PinHumanTerminal` → **`PinBareTerminalProcess`**: the same three-name trio, but
now the pin is the WHOLE story for the row-provenance path — `HumanTerminalEnv`
SET (so a `human` claim is supported), `AgentSessionEnv` + `HermesSessionIDEnv`
CLEARED (so the ambient agent chain cannot downgrade it). The doc comment names
the two signals deliberately NOT pinned and why: the pid-carrying anchor is read
BY PID and cannot be cleared by `t.Setenv` (it is inert here — `AgentRun()`
returns at its HumanTerminal branch before the anchor is consulted — and its real
consumer is `internal/agent`'s ground-branch pick), and `ParentIsWorker` comes
from procfs, not the environment, so no pin can reach it; a fixture that must not
depend on it asserts the DERIVED value instead.

**`internal/tools/tasks_test.go`** — all three `TestTaskShowBriefing*` fixtures
call `testutil.PinBareTerminalProcess(t)`, and their `submitted_by` assertions
name `taskSubmittedBy(&Task{…})` — the renderer's own function, the ONE definition
of the sentence (Rules 13/14) — instead of a re-typed literal. That is the shape
that CANNOT drift: the assertion and the renderer share one vocabulary, and a
future edit that re-couples the contract to worker provenance changes the value
in BOTH environments rather than in one.

**`internal/tools/tasks_deliverable_test.go`** —
`TestTaskSubmitNoRepoDefaultsToCwd` now asserts the durable-repo contract, taking
its expectation from `DurableRepoForSubmit(cwd)` (the ONE resolution the submit
engine calls) and asserting the contract's own label constant
`TaskShowDeliverableRepoLabel`. The test doc records what it used to assert and
why that became wrong. The epic-`01a0d55b` property is still exercised on every
run, because a package test's cwd is `<checkout>/internal/tools`.

**`internal/tools/task_display_env_guard_test.go`** (new) — the guard:

* **Coverage**: every test function in `tasks_test.go` that asserts a
  provenance-display VALUE (`source:` / `submitted_by:` followed by a literal —
  a *shape* assertion such as `"submitted_by:   "` is env-independent and exempt)
  must call `testutil.PinBareTerminalProcess`. A future fixture written the
  unmindful way fails here.
* **Listing integrity**: each name in `taskDisplayEnvPinnedTests` must still exist
  and still assert the line, so deleting or renaming a guarded test cannot retire
  the guard (a check that can pass by absence is not a check).
* **Spelling tie**: `TestDisplayContractFieldSpellingsMatchTheRenderer` asserts
  the two scanned field names against `TaskShow`'s own output, so a renderer
  rename reddens the guard instead of leaving it scanning for a field nobody
  prints.
* **Predicate red-proof**: a subtest pins that the guard's predicate still
  discriminates (accepts a pinned fixture, rejects an unpinned one, ignores a
  shape-only assertion) — a future edit that relaxes it would otherwise retire
  the guard silently, since a green guard and a no-op guard look identical.

The scanner masks string/rune/comment contents before counting braces, because
the fixtures carry braces inside JSON literals; counting over the raw text would
close a body early and let a violating fixture hide past the truncation point.

### Red-proof (observed, not asserted)

The pin was removed from `TestTaskShowBriefingFromPayload` and the guard run in
the direction that HIDES the defect:

```
$ env -i PATH=$PATH HOME=$HOME go test ./internal/tools/ -count=1 -run '^TestDisplayContractsPinTheProcessSignature$'
--- FAIL: TestDisplayContractsPinTheProcessSignature (0.00s)
    task_display_env_guard_test.go:111: TestTaskShowBriefingFromPayload asserts a provenance VALUE
        without pinning the process signature.
        Add testutil.PinBareTerminalProcess(t) as the fixture's first act: …
FAIL

$ env -i PATH=$PATH HOME=$HOME go test ./internal/tools/ -count=1 -run '^TestTaskShowBriefingFromPayload$'
ok  	github.com/the operator/gopi/internal/tools	0.548s
```

The second command is the point: **the offending fixture still passed**. In a bare
environment the coupling is invisible, which is precisely why a reviewer relying on
`go test` alone cannot notice it — and why the requirement is now mechanical.

### Guard red-proof #2 — and the coverage gap it exposed

The guard's first draft scanned ONE file (`tasks_test.go`). Replacing a
CONSTANT reference with a literal in another file of the same package —
`task_create_unified_test.go`'s `"source:         " + tasksource.Merger` →
`"source:         merger"` — was **not detected** (`ok`, mutation invisible).

That is a real gap, not a hypothetical: a provenance contract can be asserted
from any test file in the package, and one package is one suite sharing one
ambient environment. The scan was therefore widened to every `_test.go` file in
the package (resolved from the guard's own directory, the guard's own file
excluded), and re-run against the same mutation:

```
$ env -i PATH=$PATH HOME=$HOME go test ./internal/tools/ -count=1 -run '^TestDisplayContractsPinTheProcessSignature$'
--- FAIL: TestDisplayContractsPinTheProcessSignature (0.04s)
    task_display_env_guard_test.go:131: TestTaskNewOIDStampsOptionalFieldsInSameInsert asserts a
        provenance VALUE in task_create_unified_test.go without pinning the process signature.
FAIL
```

Against the unmutated tree the guard is green (`ok 0.204s`), so the widened scan
finds no existing violation — the whole package already spells its provenance
expectations in an env-independent shape.
