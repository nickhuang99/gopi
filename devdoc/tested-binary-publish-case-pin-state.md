<!-- doc-class: historical -->
# The tested-binary PUBLISH case vs a case-level pin — one state, two contracts

Task 01a12ceb5b0272c2971406fa986fda2e (2026-10-04). Companion to
`devdoc/tested-binary-pin.md` (the pin's own design); this document records the
defect that pin left in its own regression guard.

## The defect, and how it was measured

`internal/testutil/gopibin_pin_test.go` holds the pin's two halves as two cases:

- `TestGopiBinPrefersTheSuitePinnedBinary` — the precedence arm. It asserts, in
  this order, that `$GOPI_TEST_BIN` wins over the tree's `bin/gopi`, and that
  `$GOPI_BIN` wins over `$GOPI_TEST_BIN`. A case's own build outranks the suite
  pin **by design**.
- `TestGopiBinPublishesTheTreeBuildToChildren` — the publication arm. `GopiBin`
  must leave `$GOPI_TEST_BIN` set, so a CHILD process the suite spawns resolves
  the same revision instead of building or hunting one of its own.

With `GOPI_BIN` already in the environment, the second case failed. Measured
(`artifacts/repro-before.txt`, [`[1]` with the pin, `[2]` without]):

```
[1] GOPI_BIN=<abs>/bin/gopi go test ./internal/testutil/ -count=1 -run TestGopiBin -v
    --- FAIL: TestGopiBinPublishesTheTreeBuildToChildren (0.00s)
    FAIL  github.com/nick/gopi/internal/testutil  10.808s      exit 1
[2] env -u GOPI_BIN go test … (same verb)                        exit 0
```

**Mechanism (5-why, root cause first).** Why did it fail? Because `before` —
`os.Getenv(SuiteBinEnv)` read AFTER the case's own `t.Setenv(SuiteBinEnv, "")`
— was empty, and the case demanded that the call fill it in. Why did the call
not fill it in? Because `resolveGopiBin` returns at its **first arm**,
`$BinaryPinEnv`, before the suite-build arm is reachable (`run.go`, the
documented precedence 1..5). Why is that arm first? Because the pin's whole
point is that a caller's deliberate build is not second-guessed. So the case was
asserting the **suite build's** publication in a state where the suite build is
provably never entered — the contract and the environment state contradicted each
other, and only the contract was wrong.

**Not a relaxed assertion.** The alternative reading — "assert publication
anyway" — would have to DEMOTE the caller's pin so this package could build a
second binary the run never asked for. That is a different contract, and it
contradicts the case directly above it. A PASS in the `GOPI_BIN` state would
have been a green claiming a build that provably did not run (Rule 15).

**Cost of the defect.** Any acceptance shape that hands the suite its own binary
(`GOPI_BIN=$PWD/bin/gopi go test ./internal/testutil/`) read as a package FAIL,
so the package's REAL regressions were invisible behind a case that could never
pass in that state.

## The fix

`internal/testutil/gopibin_pin_test.go`, surgical (Rule 4):

1. The publication case now carries a **precondition**: a `$BinaryPinEnv` naming
   a USABLE path ⇒ `t.Skipf`, with the reason naming the precedence that made
   the suite arm unreachable. Order matters: the state check comes BEFORE
   `t.Setenv(SuiteBinEnv, "")`, so the case never rewrites a pin it has just
   decided to stand off. An unusable pin is deliberately NOT skipped — that is
   the fatal arm `resolveGopiBin` already owns (no silent substitution).
2. `TestGopiBinPublishCaseSkipsWhenACasePinAlreadyResolved` is the new
   regression guard. The case's outcome (skip vs fatal) is not observable
   in-process — a `t.Fatalf` would fail the very suite asserting it — so the
   guard re-executes this test binary with a usable `$BinaryPinEnv` and asserts
   the CHILD's exit status and `--- SKIP:` line. It also refuses a child that
   ran no test at all (`"--- SKIP: "`, not a bare "SKIP"), so a rename cannot
   turn it into a vacuous green; the case's name is a single constant (Rule 13).

## The RED → GREEN of the guard (measured, one mutant)

The guard's red proof used a mutant of the STATE, not of the assertion: a
regression of exactly the shape being guarded (a pin that resolves the case
early, when the suite publication the case asserts cannot happen).

```
# RED — the child is handed the pin's value with a suffix appended to the path,
# i.e. an ABSENT path: resolveGopiBin takes the fatal arm and the publication
# case has no such skip in the pre-fix contract
    BinaryPinEnv+"="+caseBin+"REDPROOF"
  → gopibin_pin_test.go:86: GOPI_BIN=…/case-built-gopiREDPROOF is set but not usable: stat …: no such file or directory
  → --- FAIL: TestGopiBinPublishCaseSkipsWhenACasePinAlreadyResolved (6.55s)
  → FAIL  github.com/nick/gopi/internal/testutil  13.510s        exit 1

# GREEN — mutant reverted (identical invocation)
  → --- PASS: TestGopiBinPublishCaseSkipsWhenACasePinAlreadyResolved (4.20s)
```

Raw output: `artifacts/redproof.txt`. Note the mutant's limit honestly: the
absent-path arm reds because the guard requires the SKIP line, which the
**pre-fix pre-check-less case would not have produced in that state** — but it
reds through the fatal path, not by re-running the old assertion.
`artifacts/repro-before.txt` carries that older red directly (0.00s, the
`before is empty` fatal), so the defect itself is evidenced by a verbatim run,
not by the mutant.

## Verification after the fix

```
[1] GOPI_BIN=<abs>/bin/gopi go test ./internal/testutil/ -count=1 -run TestGopiBin -v
    --- SKIP: TestGopiBinPublishesTheTreeBuildToChildren (0.00s)     ← not FAIL
    --- PASS: TestGopiBinPublishCaseSkipsWhenACasePinAlreadyResolved
    ok    github.com/nick/gopi/internal/testutil  18.984s           exit 0
[2] env -u GOPI_BIN go test … (same verb)
    --- PASS: TestGopiBinPublishesTheTreeBuildToChildren (3.24s)    ← original path
    ok    github.com/nick/gopi/internal/testutil  20.873s           exit 0
[3] GOPI_BIN=<abs>/bin/gopi go test ./internal/testutil/ -count=1   (whole package)
    ok    github.com/nick/gopi/internal/testutil  98.796s           exit 0
```

## A different, unowned flake observed while verifying

The first whole-package run of this task (`GOPI_BIN=… go test
./internal/testutil/ -count=1` while `go build ./...` ran in parallel) reported

```
FAIL  github.com/nick/gopi/internal/testutil  95.818s
```

with **no failing test name captured** (the command's output was read through
`tail -5`). Two subsequent whole-package runs — one of them deliberately
repeating the `go build ./...` overlap — printed `ok` (98.796s / 87.834s). It is
recorded here as an unowned, unreproduced observation with its command, not as a
diagnosis; a `-v` run of the same shape is what would name the case. It is
**not** attributable to this change: the three runs differ only in load.

## A second, DIFFERENT failure measured while closing out — `make` shadows the pin

The acceptance also names `make test-budget PKGS='./internal/testutil'` **with**
`GOPI_BIN` set. Measured 2026-10-12: that shape is still red, and **not** for the
reason this fix addressed — it is a second, independent defect, filed as task
`01a12d14-513a-7b21-baee-d08e75acd312` (Rule 41).

```
$ GOPI_BIN=$PWD/bin/gopi make test-budget PKGS='./internal/testutil'
  --- FAIL: TestGopiBinPublishesTheTreeBuildToChildren (0.00s)
  gopibin_pin_test.go:86: GOPI_BIN=bin/gopi is set but not usable: stat bin/gopi: no such file or directory
```

Note the value in the failure: `bin/gopi`, a RELATIVE path — while the caller
exported an absolute, existing one. Mechanism, measured:

- `Makefile:19` declares `GOPI_BIN := $(BIN_DIR)/gopi`. `:=` **overrides** an
  environment value of the same name (`GOPI_BIN=$PWD/bin/gopi make -p | grep
  '^GOPI_BIN'` → `GOPI_BIN := bin/gopi`; the same query with `-f /dev/null` → the
  caller's absolute path).
- Because the variable was **imported from the environment**, make keeps its
  **export bit**, so the recipe's child sees the OVERRIDDEN value: `envprobe.mk`
  prints `RECIPE_ENV: GOPI_BIN=bin/gopi`. With no `GOPI_BIN` in the caller's
  environment, nothing is exported and `make test-budget` is unaffected — which is
  why the ordinary run never showed this.
- `go test` then runs each package with CWD = that package's directory, so the
  relative `bin/gopi` does not resolve ⇒ `resolveGopiBin`'s first arm takes its
  FATAL path (no silent substitution, by design).

**It is not this change.** The HEAD version of the test file, in that same state,
fails the same way (`$AR/I2-real-makefile-shadow.txt`): the case never mattered.
**And it is not my earlier "flake" either** — the whole-package `FAIL` this
document recorded above with no failing test named was THIS, not a flake; that
paragraph is corrected in place below.

Raw output: `artifacts/I-make-clobbers-GOPI_BIN.txt`,
`artifacts/I2-real-makefile-shadow.txt`, `artifacts/K-export-attribute.txt`,
`artifacts/D2-make-test-budget-absolute-GOPI_BIN.txt`.

## Correction: the "unowned flake" recorded above was not a flake

The earlier paragraph in this document ("A different, unowned flake observed while
verifying") reported a whole-package `FAIL … 95.818s` whose failing test was not
captured because the command's output was read through `tail -5`. Re-measured with
the same invocation and the output read in full (`artifacts/D-make-test-budget-absolute-GOPI_BIN.txt`,
its parsed `-json` stream naming the case), the failure is the `GOPI_BIN=bin/gopi`
fatal above: `make`'s shadowing, deterministic, not load-dependent. The earlier
"two subsequent runs printed ok" observation stands, and is explained — in those
runs the pin reached `go test` as an absolute path.

**What is NOT claimed**: the arithmetic that makes one run red and another green
in that paragraph is now unattributed rather than a flake; no further runs were
spent on it.

## What is NOT claimed

- No production code was touched. `run.go` is unchanged; the fix is the case's
  own precondition plus a guard.
- The guard does not assert that no OTHER case asserts the suite build in a
  pinned state; it pins the one measured contradiction.
- The skip in `[1]` is a real skip, not a green: the suite-build publication is
  still asserted by `[2]`, which is the shape every ordinary run takes.

## Guidance for the next agent

- **A guard case must state the state it holds in.** Two cases about ONE
  precedence table cannot both assert the arms they do not reach; make the state
  explicit and let the other state be proved elsewhere.
- **Do not "fix" this by asserting publication in the pinned state.** That
  requires demoting a caller's pin, which is the precedence the case above
  exists to keep.
- **A subprocess fixture must assert a UNIQUE marker of the outcome**
  (`"--- SKIP: "`, never a bare `"SKIP"`), or a case that ran nothing reads as
  the behaviour under test.
- **Evidence paths for this task**: `~/.gopi/sessions/01a12ceb-5b02-72c2-9714-06fa986fda2e/artifacts/`
  (`repro-before.txt`, `repro-after.txt`, `redproof.txt`,
  `fullpkg-with-gopibin.txt`, `fullpkg-contended.txt`, `flake-hunt.txt`).
