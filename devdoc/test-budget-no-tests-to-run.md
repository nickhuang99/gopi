<!-- doc-class: normative -->
# The declared test entry point refuses a filtered run that executed nothing

**Date:** 2026-10-12 · **Task:** `01a12cbe-324f-75a6-860e-f32ba6a7ce39`
**Bug row that produced it:** `01a12cbe-324f-75a6-860e-f32ba6a7ce39` (filed while
archiving another run's evidence: four of five archived `-run` names were
mistyped and the archive read as green).

## The defect

`go test -run <a name that matches nothing>` prints

```
testing: warning: no tests to run
PASS
ok  	github.com/nick/gopi/cmd/agent	3.400s [no tests to run]
```

and **exits 0** — measured, in a real repo package, against a near-miss typo of a
real test name:

| filter | what ran | observed |
|---|---|---|
| `-run ^TestNotifyDeliverStatesOnce$` (typo of the real name) | nothing | `PASS`, `ok … 3.400s [no tests to run]`, **exit 0** |
| `-run ^TestNotifyDeliverStatesTheMessageOnceInTheSession$` | the test | `--- PASS: … (33.58s)`, `ok … 37.062s`, exit 0 |

Raw output: `artifacts/false-green-no-tests-to-run.out` (session dir of
`01a12c7a-43b2-7684-84d2-4ad6a2d5c914`). The marker lives on **stdout**, is not
an error, and changes no status code, so every consumer of the exit status — a
report's `Ran:` line (Rule 31), a background job archive, a hand-written verify
script — reads a mistyped name as a pass. Before this change **nothing in the
repo watched the marker**: `grep -rn "no tests to run" internal/ scripts/ cmd/`
→ **0 hits** (the only hits in the tree are prose in past reports).

The blast radius is the merge/verify path's own verification: a branch-scoped
smoke run is exactly `make test-budget PKGS='-run <Test> ./pkg/'`, so the one
command whose green authorises a merge is the one that goes green when the test
name is wrong.

## The change — one verdict, in the declared entry point

`scripts/test-budget.sh` is the declared entry point (`make test`, `make
test-budget PKGS=…`, `internal/smoke.Command`), so the discrimination goes there
and nowhere else (Rule 14: one definition; no new script, no new state file, no
daemon — Rule 35).

* **A new exit code, `5`**, for `no-test-executed`, distinct from `1` (assertion
  or build failure) and `3` (the per-package budget expired). A reader can now
  tell the three apart by exit status alone.
* **The count is taken from the run's own `go test -json` stream**, not from our
  parsing of human output: one event with `"Action":"run"` is emitted per test as
  it starts. A **skipped** test never emits one, which is what keeps "ran" apart
  from "was selected" — a package whose every test is heavy and skips by default
  is not a mistyped filter.
* **It fires only for a FILTERED run** (`-run` / `-skip` / `-bench`, the flag
  names in the `filter_flags` denylist). With no filter in force, "0 executed" is
  a legitimate reading (`?   pkg  [no test files]` — the package set simply holds
  no test files), and that behaviour is unchanged.
* **Named constants** (Rule 1/13): `no_tests_marker` (the trigger sentence,
  spelled once), `no_match_marker` (the banner), `no_test_executed_exit`,
  `filter_flags`, `executed_action`, `output_action`, `executed_names_report_n`.
* **The banner names the evidence**: the filter as requested, the marker Go
  printed, the executed count, and what the exit code means. The summary prints,
  on every run, `filter:` (the filter in force, or `(none …)`) and
  `executed: N test(s) ran` plus the first few `ran: <pkg> <Test>` names — so a
  filtered green is auditable without opening the stream, and the **absence** of
  those lines is itself a signal.
* **No false failure block**: a "nothing ran" run no longer tails into
  `--- failures (assertion/build; the budget did NOT trip) ---` (it says
  `--- NOT A FAILURE: nothing ran (the filter matched no test) ---`), so the
  verdict cannot be quoted as a failing assertion.

### Cases measured, so the verdict's shape is known at its edges

Run against the real repo through the real script (`bash scripts/test-budget.sh …`):

| invocation | exit | why |
|---|---|---|
| `./internal/smokegate/` (no filter) | 0 | 2 executed — unchanged behaviour |
| `-run '^TestNoSuchName$' ./internal/smokegate/` | **5** | banner + `executed: 0` |
| `-run=^NoSuchTest$ ./internal/smokegate/` (glued form) | **5** | the glued spelling is recognised |
| `-run '^TestCommandIsNotTheWholeTree$' -skip '^TestCommandIsNotTheWholeTree$' …` | **5** | everything was filtered away |
| `-run '^$' -bench Nope ./internal/smokegate/` | **5** | `-bench` is a filter flag too |
| `-run '^TestCommandIsNotTheWholeTree$' ./internal/toolnames/ ./internal/smokegate/` | 0 | `executed: 1` — a package with **no test files** (`[no test files]`, `skip`) must not redden a run that did execute a test |
| `-run '^TestCommandIsNotTheWholeTree$' -list '.*' …` | **5** | `-list` runs nothing **by design**, so "nothing ran" is true and the code says so; the banner's advice line names this case explicitly |

The `-list` reading is deliberately a `5` rather than an exemption: this line
exists to stop a *silent* green, and `-list` is an explicit "tell me the names"
inspection whose output a reader cannot mistake for a pass — but a command that
executed no test still must not exit 0. (An earlier design considered forcing
`-list` through even with a non-matching `-run`; that would make the script's
scope differ from the operator's argv, so it was rejected.)

## The gate

`internal/testutil/testbudget_notests_gate_test.go` — three tests, each covering
a hole the others cannot (Rule 30's "checked, not trusted"):

1. `TestBudgetFilterMatchingNoTestIsNotAPass` — the acceptance pair, **executed
   against the real repo through the real entry point** (Rule 31): impossible
   name ⇒ exit 5 + banner + `executed: 0` + the filter echoed + outcome
   `no-test-executed` + **no assertion-failure block**; real name ⇒ exit 0 +
   `executed: 1` + no banner. It also asserts the run names its stream
   (`raw json:`) and stamps itself (`stamp:`).
2. `TestBudgetNoTestsMarkerIsStillWhatGoPrints` — the script's trigger sentence is
   **derived from the script's own constant** (`no_tests_marker="…"`) and then
   looked for in a `go test -json` stream this test produces with the same
   impossible filter, and the executed-count action is read from the script's
   `executed_action` and asserted to read 0 there. The verdict is keyed on Go's
   message: a reworded upstream message would silently switch it off, so here it
   is a **red test** instead.
3. `TestBudgetNoTestsVerdictRunsInAFixtureModule` — the **same script staged into
   a throwaway module** (`example.test/notestsfixture`, one package, one ordinary
   test), both arms: impossible ⇒ 5, the fixture's own test ⇒ 0 + `executed: 1`.
   The verdict is a property of the script, not of this repo's package layout.

Each run is isolated (`t.Setenv(scratch.HomeEnvName, t.TempDir())`), so a
neighbour's stream on this shared host can never be read as this run's evidence
(the artifact-sharing defect the concurrent gate documents — `internal/testutil/testbudget_concurrent_test.go`).

**Red proof** (verdict disabled with `if false; then` in the script):

```
testbudget_notests_gate_test.go:117: a filtered run that executed no test exited 0, want 5
--- FAIL: TestBudgetFilterMatchingNoTestIsNotAPass (4.34s)
testbudget_notests_gate_test.go:236: in a fixture module, a filtered run that executed no test exited 0, want 5
--- FAIL: TestBudgetNoTestsVerdictRunsInAFixtureModule (0.88s)
```

Green with the verdict restored (3/3 PASS). Raw output:
`gate-red-proof.out`, `gate-green.out` (this task's session dir).

## Verification (Rule 5 / Rule 31)

* **Build:** `go vet ./internal/testutil/` → clean; `gofmt -l` on the new file →
  empty; `bash -n scripts/test-budget.sh` → OK.
* **Tests:** `go test ./internal/testutil/ -count=1 -run
  'TestBudgetFilterMatchingNoTestIsNotAPass|TestBudgetNoTestsMarkerIsStillWhatGoPrints|TestBudgetNoTestsVerdictRunsInAFixtureModule' -v`
  → 3/3 PASS (`ok github.com/nick/gopi/internal/testutil 10.667s`).
  The packages this diff touched, through the declared entry point:
  `make test-budget PKGS='./internal/testutil/ ./internal/smokegate/'`.
* **Ran (the surface itself, real arguments, real repo):**
  * `make test-budget PKGS='-run TestBudgetNoSuchTestEverExists ./internal/smokegate/'`
    → script stamp `exit=5 outcome=no-test-executed`, banner
    `NO TEST EXECUTED, THE FILTER MATCHED NOTHING`, `executed: 0`; make reports
    `*** [Makefile:61: test-budget] Error 5` (`PIPESTATUS=2`, make's own exit for
    any recipe failure — the script's 5 is in the stamp). `artifacts/make-no-match.out`
  * `make test-budget PKGS='-run TestCommandIsNotTheWholeTree ./internal/smokegate/'`
    → `executed: 1 test(s) ran`, `filter: -run TestCommandIsNotTheWholeTree`,
    stamp `exit=0 outcome=pass` (`PIPESTATUS=0`). `artifacts/make-match.out`
* **Result:** the same command, one differently-spelled filter, moves from exit 5
  with a named verdict to exit 0 with a count — the false green is gone on the
  merge path's own command.

## Not Done (honest)

* **`make` swallows the code.** `make test-budget` exits 2 for *any* failing
  recipe; the script's own code (0/1/3/5) reaches a caller only through the stamp
  line and the banner. Changing make's behaviour would be a second entry point to
  this verdict, so it was left alone — but a reader who only looks at `make`'s
  status cannot tell 5 from 1. Named here rather than silently accepted.
* **`go test -list` is reported, not exempted** (see the table above). A caller
  that uses `-list` as an inspection now sees exit 5.
* **The verdict is blind to an unseen filter flag.** `filter_flags` is a
  denylist: a future Go flag that filters the test set would leave the check
  asleep. Deliberate — an allowlist would have to guess which flags filter, and
  guessing would produce a verdict out of a plain `/.../` run.
* **`go test ./pkg/ -run X` typed by hand still exits 0.** This change covers the
  repo's *declared* entry point; the bare `go test` bypass (which Rule 39/27
  already discourage on the merge path) is untouched.
* **The same shape may exist on other entry points** (`scripts/flake-catch.sh`
  passes `-run` through to `go test`; `testsuite/` and the e2e scripts run their
  own `go test` lines). Only the declared merge/verify entry point was changed —
  widening to every caller would be a second policy, not this fix. Recorded so a
  later reader knows the boundary was chosen, not overlooked.
