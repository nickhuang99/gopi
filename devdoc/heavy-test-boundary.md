<!-- doc-class: historical -->
# Task Report: The Heavy-Test Boundary — Who May Enable the Heavy Set

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task: `01a0b1b5-d902-7c3e-b206-c511dcdb7ebf` (branch `feat/heavy-test-operator-boundary`, from master `45c6ecc1`)

## Completed

The classification was already correct (`b9217d71`): heavy tests are OFF unless
`GOPI_HEAVY_TESTS` is truthy, read by exactly one function (`heavytest.Enabled`).
The defect was **inheritance**: an ordinary env var travels from a shell to
everything below it, so a worker / merger child / `gopi task run` executor that
merely inherited the switch silently burned an hour of host CPU on tests the
operator keeps OFF (classification untouched by this change).

1. **The boundary** — `internal/heavytest/boundary.go` (new). `Require` now asks
   one predicate before it runs a heavy body:
   - `WorkEnvVars` = the work signatures this harness already exports and every
     descendant inherits: `GOPI_EXECUTOR_SESSION_ID`, `HERMES_SESSION_ID`,
     `GOPI_AGENT_SESSION` (the agentguard names), plus `GOPI_TASK_WRAPPER_ID` /
     `GOPI_TASK_RUN_ID` (stamped at spawn for a task child).
   - `WorkRefusal()` → `(reason, signature, declared)`. Any signature present and
     no declaration ⇒ **refuse**. `GOPI_HUMAN_TERMINAL` truthy (the operator's
     negative signature, per `internal/config/agentguard.go`) ⇒ **admit**.
   - `refuseHeavyRun` announces the refusal LOUDLY: the block goes to **stderr,
     unconditionally** — where a `t.Logf` would need `-v`. Measured 2026-09-18
     (amendment): a refused test SKIPS, i.e. the package PASSES, and `go test`
     keeps a passing package's output to itself, so a bare `go test ./...` shows
     NEITHER the stderr block NOR the log line. The channel that makes a refusal
     visible in **every** run is therefore the ledger row (below), which the
     Makefile's smoke target always writes; a run that wants the full block uses
     `-v` (or the `-json` stream). No silent no-op either way: the refusal is
     always COUNTABLE. It writes a `REFUSED` row into the smoke skip ledger and
     `t.Skip`s — the run stays smoke-only; nothing about the classification moved.
   - `WorkSignature` / `WorkRefusalWith(lookup)` let a *seam holding an env list*
     be judged by the same predicate (no second rule to drift).
2. **The operator route (works first try)** — `make test-heavy` now sets
   `GOPI_HUMAN_TERMINAL=1` alongside `GOPI_HEAVY_TESTS=1`. One heavy test by hand:
   `GOPI_HEAVY_TESTS=1 GOPI_HUMAN_TERMINAL=1 go test -run <Test> -v ./<pkg>/`.
3. **The seams** — the declaration cannot travel down:
   - `internal/tools/tasks.go`: `SpendBoundaryEnvVars = []string{config.HumanTerminalEnv}`
     + exported `IsSpendBoundaryEnv` (ONE predicate for both consumers).
   - `internal/tools/childenv.go`: `stripWrapperIdentity` (the spawn seam shared by
     `bash` and `job`) now drops them too.
   - `cmd/agent/task_child_env.go`: `taskChildExecEnv` strips them and **reports**
     every removed name (a silent strip would make "nothing leaked" and "nothing
     was checked" indistinguishable).
   - Mirrors carry drift tests (Rule 14): `internal/config/heavy_boundary_names_test.go`,
     `internal/tools/heavy_boundary_names_test.go`.
4. **Tests** — `internal/heavytest/boundary_test.go` (unit on the real predicate +
   e2e on a **generated** fixture module: task-child refuses, operator runs,
   default still skips with the classification's marker, refusal writes the
   ledger); `cmd/agent/task_child_spend_boundary_test.go` (the seam, against a
   fake env list — no ambient signature, no `os.Setenv`); `internal/tools/heavy_boundary_operator_text_test.go`
   (the operator text must say it, and the target it names must actually set the
   declaration).
5. **verify_gate operator text** — `internal/tools/verify_gate.go`: a new item 6 in
   the merge/verify-lane briefing. It states heavy tests did NOT run **by design**,
   that an attempted opt-in from a work context was **REFUSED** and why, that the
   authority is the operator's declaration which the seam strips from the child,
   and that a verdict depending on a heavy test means **recording no verdict** and
   leaving the decision to the operator. No new status surface was invented
   (`INCONCLUSIVE` is not a status in this repo — checked; the real set is
   pending/claimed/in_progress/completed/failed/cancelled/verified/reopened).
6. **Sibling-escape family audit** (below).

## Sibling test-escape env family (deliverable 4)

| env var | site | inheritance consequence | gated today |
|---|---|---|---|
| `GOPI_HEAVY_TESTS` | `internal/heavytest/heavy.go:43` (reader `Enabled`) | an inherited value buys an hour of host CPU the default forbids | **YES — this change** (loud refusal at the call site; declaration stripped at the executor seam; `make test-heavy` declares it) |
| `GOPI_TASK_CHILD_FAIL_TEST` | const `cmd/agent/cli_tasks.go:143`; guard `cmd/agent/task_child_fail_guard.go:251` (`refuseIfUnsafeTaskChildFailEscape`) / `:270` (`assertTaskChildFailEscapeSandboxed`); called `cmd/agent/cli_tasks.go:1450` | turns the child's own fail-outcome path into an injection knob | **YES — already (prior task `01a0a940`)** — refuses unless sandboxed: ledger inside the scratch temp root / HomeDir, temp root off `/` by device id, not the live `~/.gopi` |
| `GOPI_ALLOW_DANGEROUS` | `internal/config/env.go:202`, reader `internal/tools/bash.go:99` (`escapeDangerous`) | disables dangerous-command rejection for every shell below | operator knob by design (Rule 22/25 family); the *per-command* path is the agent's own tool call, not an inherited gift — deliberately NOT newly gated here |
| `GOPI_WORKER_MAX_ROUNDS` / `GOPI_WORKER_IDLE_EXITS` / `GOPI_WORKER_IDLE_SEC` | `cmd/agent/cli_tasks_worker.go:81/123/98` | bound a worker's lifetime (cost control, not an escape) | loop configuration — a child inheriting them changes the loop it is not running; NOT an escape |
| `GOPI_SKIP_CLAIM_BROADCAST` | `internal/config/settings.go:19` | suppresses a claim broadcast | operator setting (settings.json-backed); no authority gain |
| `GOPI_SKIP_TERMINAL_PUSH` | `internal/agent/taskregister.go:531` | suppresses the terminal push | operator setting |
| `GOPI_MERGE_CONTRACT_GATE` / `GOPI_MERGE_UNION_GATE` / `GOPI_MERGE_BUILD_GATE` / `GOPI_POSTMERGE_VET_GATE` | `internal/tools/merge_*_gate.go:93/93/78`, `rebuild_vet_gate.go:118` | turns a MERGE gate off (a real escape) | operator knobs, each with its own ONE reader + refusal/`res.Reason` naming the variable; the merge lane is driven by the operator/merger, and a task child never drives a merge — **not newly gated here** |
| `GOPI_AGENT_SPAWN_GUARD_OFF` / `GOPI_ALLOW_AGENT_SPAWN` | `internal/config/agentguard.go:107/102` | disables the agent-spawn guard | **YES** — agentguard's whole point; the same signature ladder this boundary follows |

Scope judgement (honest): the family is larger than one deliverable. This change
closes the **spend** escape (`GOPI_HEAVY_TESTS`) and the **authority** leg that
would re-open it (`GOPI_HUMAN_TERMINAL`), and reports the rest with its gating
status. The merge-gate knobs are the strongest remaining candidate for the same
treatment (they are read by the merger lane, not by a task child) — deliberately
not touched here, because touching them would be a change to the merge path this
task was told not to gate.

## Not Done (honest)

- The merge-gate `*_GATE` knobs (row 7 above) are **reported, not gated**. Gating
  them means deciding who may drive a merge — a system-layer question, and this
  task's briefing forbids widening the status surface / touching the merge path.
- `internal/tools/bash.go`'s `bashChildEnv` still discards the removed-name list,
  so a bash-tool strip is not announced. JobStart already logs; leaving bash's
  report quiet was deliberate (it would change output every agent sees), but it
  IS an asymmetry worth a follow-up.
- The `internal/heavytest` mirror of the agentguard names is a real mirror with
  a drift test, not a shared constant package: the leaf constraint (its
  in-package tests are imported BY scratch/tools/cmd tests) forbids importing
  `internal/config`. Recorded as the reason in `boundary.go`; if a future change
  makes a leaf constant package possible, that is the cleaner shape.

## Rejected alternative

**A Makefile-only change** (`make test` unsets the var): rejected because the
boundary must hold for ANY invoker — a plain `go test ./...`, a script, a
worker's own spawn. The Makefile is one invoker among many, and the defect
survives it.

**A permission file / token / registry**: rejected by the briefing (and by
Rule 22/25's spirit): it makes the operator's own route stateful and breakable,
and gives the agent a thing to fetch. The signature ladder already answers "is an
agent above me?" with no new state, and the var itself is the whole problem.

## Verification

- Build: `go build ./...` → OK.
- Vet: `go vet ./internal/heavytest ./internal/tools ./internal/config` and
  `go vet ./cmd/agent` → clean (no output).
- Focused suites: `internal/heavytest` (boundary unit + e2e) OK;
  `internal/tools` (`TestHeavyBoundary*`, `TestOperatorText*`,
  `TestTheDocumentedOperatorRoute*`) OK; `internal/config` (`TestHeavyBoundary*`) OK;
  `cmd/agent` (`TestSpendBoundary*`, `TestTaskChildEnv*`) OK.
- `make test-budget PKGS='…'` (branch-scoped smoke) + skip-ledger contents: see
  `## Evidence (self-filed)`.
- Commit: <filled at commit time>

## Evidence (self-filed)

**A) CONSTRUCTED ACCEPTANCE CASE — task child carrying the opt-in (must SKIP +
REFUSE)** — `GOPI_HEAVY_TESTS=1 GOPI_TASK_WRAPPER_ID=01a0b1b5-wrapper GOPI_TASK_RUN_ID=01a0b1b5-wrapper go test -count=1 -run TestHeavyEvidence -v ./`:

```
=== RUN   TestHeavyEvidence
HEAVY TESTS REFUSED: this process is a WORK context (an agent session is above it, or it IS a task child),
  so the heavy set stays OFF even though GOPI_HEAVY_TESTS is set.
  WHY THIS PROCESS: inherited work signature: GOPI_TASK_WRAPPER_ID, GOPI_TASK_RUN_ID
  Heavy tests are the OPERATOR's deliberate, off-peak choice; a value that was INHERITED
  (a dev shell, a briefing that says "run the full suite", a wrapper script) must never buy
  an hour of host CPU that the default forbids.
  WHAT HAPPENED: the heavy set did NOT run — this is a REFUSAL, not a silent no-op.
  The run stays SMOKE-ONLY, exactly as if GOPI_HEAVY_TESTS had been unset: the classification
  is untouched, every heavy test still SKIPS itself, and the skip ledger records a
  REFUSED row so the refusal is countable (`make test` leaves its ledger at .heavy-skipped.tsv).
  OPERATOR ROUTE (works first try): from your own terminal —
      make test-heavy                     the documented opt-in (whole tree, off-peak)
      GOPI_HUMAN_TERMINAL=1 go test -run <Test> -v ./<pkg>/     one heavy test
  Both DECLARE the terminal you are sitting at. Declare it per invocation, never in a
  shell profile: a profile would speak for every agent started below it.
--- SKIP: TestHeavyEvidence (0.00s)
PASS
ok  	github.com/nick/gopi/internal/heavytest/boundaryevid	0.002s
A_EXIT=0
```
(`HEAVY BODY RAN` absent — the heavy body never executed.)

**B) OPERATOR ROUTE (must RUN)** — `GOPI_HEAVY_TESTS=1 GOPI_HUMAN_TERMINAL=1 go test -count=1 -run TestHeavyEvidence -v ./`:

```
=== RUN   TestHeavyEvidence
    evid_test.go:14: HEAVY BODY RAN
--- PASS: TestHeavyEvidence (0.00s)
PASS
B_EXIT=0
```

**C) DEFAULT UNCHANGED** — nothing set: `HEAVY TEST SKIPPED (set GOPI_HEAVY_TESTS=1 to run)` + `--- SKIP`, `C_EXIT=0` (the classification's own path, no refusal line).

**D) THE REFUSAL IS COUNTABLE** — ledger contents:
```
TestHeavyEvidence	REFUSED: inherited work signature: GOPI_TASK_WRAPPER_ID
```

**BASE-RED (the red half of red→green)** — the SAME constructed case against the
baseline revision `45c6ecc1` (`/home/user/workspace/gopi-heavy-baseline`, detached
worktree), probe body a no-op per Rule 27:

```
=== RUN   TestTaskChildMustNotRunHeavy
    basered_test.go:12: BASE-RED: the heavy body RAN in a task child — no boundary at this revision
--- FAIL: TestTaskChildMustNotRunHeavy (0.00s)
FAIL
BASELINE_EXIT=1
```

Same case on this branch: SKIP + the refusal block, exit 0.

**Worktree hygiene** — `git status --porcelain` in the run's worktree lists only
this change's files (6 modified, 5 new; the `.evid/` entries I deleted while
probing were restored with `git restore .evid` in the same session — restored,
not committed).

**Rule 27** — every heavy probe body in the evidence harness is a deliberate
no-op (`t.Log`/`t.Fatalf`); no test generated device load, and the classification
was never weakened to make a number appear.

---

## Amendment — close-out verification and two narrow repairs (2026-09-18, second session)

The delivery above was re-verified independently against the brief's seven
acceptance gates, on a fresh worktree (`~/workspace/gopi-heavy-verify2`) on the
same branch, after the first worktree was reclaimed mid-run by the merge
machinery (the branch survived intact; nothing was lost). Two narrow repairs
landed, both inside the boundary's own files:

1. **A dead second predicate removed.** `cmd/agent/task_child_env.go` carried an
   unexported `isSpendBoundaryEnv` that nothing called — it was kept alive only
   by `TestSpendBoundaryPredicateMatchesTheList` asserting it agreed with
   `tools.IsSpendBoundaryEnv`. A mirror of a shared predicate is exactly what
   Rule 14 bans ("never mirror, never bare-write"): the seam now asks
   `tools.IsSpendBoundaryEnv` alone, and the test keeps the two assertions that
   measure the real thing (membership + prefix-adjacency).
2. **The visibility claim corrected to what was measured.** The refusal block
   goes to stderr unconditionally, but a refused test SKIPS and `go test` prints
   nothing for a passing package — so a bare `make test` shows neither the block
   nor the log line; the **ledger row** is the channel present in every run. The
   comment in `boundary.go` now states that instead of implying a bare run shows
   the block. The enforcement is unchanged and still countable (that is the part
   the brief's "loud, never a silent no-op" gate actually requires).

Re-verified at the amended tip: `gofmt -l` clean; `go build ./...` OK;
`go vet` on heavytest/tools/config/cmd-agent/standards clean; suites
`internal/heavytest`, `internal/standards` (heavy inventory gate), `internal/tools`,
`internal/config`, `cmd/agent` all `ok`; classification-skip ledger
(`internal/scratch`) writes 5 criterion rows; constructed acceptance case A
(task child + opt-in) REFUSES and skips, B (operator declaration) runs the body,
C (default) takes the classification skip, D writes the `REFUSED` ledger row;
base-red at `45c6ecc1` reproduces (the heavy body RAN inside a task child).
