<!-- doc-class: historical -->
# The three `cmd/agent` reds: attribution from raw streams, and one authority for the sandbox default

**Task**: `01a13cfff3ba79858701a6cebad7fbd1` · branch `task/01a13cfff3ba79858701a6cebad7fbd1`.

The scope run `scripts/test-budget.sh ./internal/docclass/ ./cmd/agent/` used to be
cap-bound; after the branch's heavy classification it fits the budget (cmd/agent
1506s → 643s) but still exits 1 with three top-level FAILs. This document records
what the two reds *are* — because "red in isolation" and "red in the suite" are
different objects, and only one experiment separates them.

## 1. Why isolation is not an attribution instrument

`TestChatTurnLayout_TwoTurnsAreSeparatedAndAligned` and
`TestSignalProvenanceDoorNeverClaimsALossForARefusedRecord` both PASS in isolation
on the branch, and both PASS in isolation *and* in a whole-package run at the
branch's base. They fail only in the branch's whole-package run. A test that
passes alone and fails in the suite is not "flaky" as a character trait: something
about the suite's composition decides the outcome, and the only way to see that
composition is to run the same whole-package command at a revision that differs by
the change under suspicion.

## 2. The comparison, taken from the runs' own JSON streams

Same host, same command, same `GOPI_TEST_HOME`, no load generated (Rule 27).

| run | revision | elapsed (cmd/agent) | exit | stream |
|---|---|---|---|---|
| branch | `019dfccc6` | 643.46s | 1 | `run-1792023762-2062833.json` |
| base (= master = this branch's base) | `064e2cc65` | 1009.23s | 1 | `run-1792028497-2263226.json` |
| **direct parent of the branch** (tighter control) | `4d3029139` | 984s | 1 | `run-1792029794-2339170.json` |

Per-test FAIL lists, read off those two files:

```
base   (1): TestSandboxShowPrecedenceAndNoRegression 0.02
branch (4): TestChatTurnLayout_TwoTurnsAreSeparatedAndAligned 0.03
            TestSandboxShowPrecedenceAndNoRegression 0.03
            TestSignalProvenanceDoorNeverClaimsALossForARefusedRecord 3.89
            TestSignalProvenanceDoorNeverClaimsALossForARefusedRecord/nothing_yet 1.58
```

**Verdict.** The sandbox test is red in *both* ⇒ pre-existing. The other two are
red only in the branch run ⇒ induced — and the mechanism is legible in the same
numbers: the branch's commits classify 38 `cmd/agent` call sites heavy
(`861ed974b`), so hundreds of tests skip, the package takes 366s less wall time,
and the survivors run in a different order and under different load. The two
tests were not edited by that work and are not wrong; the **schedule around them**
changed. A schedule-dependent failure is a real defect in the failing tests (a
test that depends on when it runs is a test that will fail on someone else's
machine), but its repair belongs to those tests' packages, not to the
classification commit — hence this row attributes and hands over the evidence
rather than reshaping someone else's test.

**The direct-parent control, run second and read the same way** (`4d3029139`, the
branch's own base — the revision that differs from `019dfccc6` by exactly this
branch):

```
direct-parent FAILS (1): TestSandboxShowPrecedenceAndNoRegression (0.02s)
branch        FAILS (4): … (the three above)
only in BRANCH (INDUCED) : TestChatTurnLayout_TwoTurnsAreSeparatedAndAligned,
                           TestSignalProvenanceDoorNeverClaimsALossForARefusedRecord,
                           TestSignalProvenanceDoorNeverClaimsALossForARefusedRecord/nothing_yet
only in PARENT           : (none)
in BOTH (pre-existing)   : TestSandboxShowPrecedenceAndNoRegression
```

The tighter control agrees with the base run (`064e2cc65`) on every clause, so the
attribution does not depend on which of the two revisions serves as the base: the
sandbox red predates the branch, the other two do not.

## 3. The sandbox default: two homes, one assertion

**Symptom** (deterministic, red at the branch base, `cli_sandbox_show_surface_test.go:138`):

```
with no channel configured, root=/home/user/workspace/gopi-sandbox,
want the built-in default …/gopi-test-scratch/workspace/gopi-sandbox
```

**Root cause — two different HOMEs, not two different spellings of one path.**
`config.SandboxRootDefault()` is `<workspace home>/workspace/gopi-sandbox`, and the
workspace home is `scratch.WorkspaceUnderHome()`: `$GOPI_TEST_HOME` when the suite
publishes it, else the OS user's home (`internal/scratch/scratch.go:104`). The
probe drives the real binary through `cliContractEnv`, which **scrubs every
`GOPI_*` variable** — including `GOPI_TEST_HOME` — because that scrub is what keeps
a probe off the operator's live profile. So the parent evaluated
`config.SandboxRootDefault()` under the suite's scratch home while the child
resolved the same function under the operator's real `$HOME`. The child is right
about its environment; the parent is right about its own; the assertion compared
the two and called the difference a bug.

**The authority.** For an assertion about *a surface*, the environment that decides
is the surface's — the child's. The expectation must therefore read the same
function the binary reads, evaluated for the home the child was given; it must
never re-spell the path (Rule 2: one reader, one resolution).

**The fix** (`cmd/agent/cli_sandbox_show_surface_test.go`, arm "no channel
configured"): the workspace-home variable is pinned in *both* directions — exported
into the child's environment (`append(cliContractEnv(home),
scratch.WorkspaceHomeEnvName+"="+home)`) and pinned in this process with
`t.Setenv(scratch.WorkspaceHomeEnvName, home)` — so
`config.SandboxRootDefault()` is the resolver's own answer for the same home. No
new literal and no new constant: the asymmetry is removed, not renamed (Rules 1/13).

**The regression test** —
`TestSandboxShowDefaultFollowsTheWorkspaceHomeTheProbePins` — requires an
observable property, not a path: the resolved root equals the resolver's value for
the **pinned** home and lives under it. The ambient scratch home is set to a third
directory first, so the old shape (ambient home decides) reports a root under the
wrong home and the test goes red on its own, independent of how the suite is
invoked. Its red proof: with the child-side pin removed, the probe reports
`root=/home/user/workspace/gopi-sandbox` against the pinned home and fails
(`RED-EXIT=1`); the file is then restored byte-identically (hash
`f5c41fdd0d47998ede767d4f12b74dc32ef08f30`, single-source restore).

## 4. What this does NOT claim

- It does not claim the two schedule-dependent failures are fixed here; they are
  attributed, and the fix is a separate row against the tests themselves.
- It does not claim the sandbox asymmetry was the only instance: **every probe that
  asserts a home-derived default while running the child through `cliContractEnv`
  has the same shape**, because the scrub and the home-derived default are
  individually correct and jointly contradictory. Only the sandbox surface is
  repaired here; the general pattern is named so the next reader can grep for it.
- No new dependency, no new knob, no `heavytest` classification: the three tests
  involved remain ordinary tests on the ordinary path (Rule 27).
