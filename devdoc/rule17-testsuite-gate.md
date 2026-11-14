<!-- doc-class: normative -->
# Task Report: Rule 17 gate — cover the whole test surface (testsuite/)

Ticket: *"gate(test): enforce Rule 17 (t.Setenv) with a baselined gate, once the
49 os.Setenv sites are migrated"*. Verdict after investigation: **already done —
and stronger than proposed**; the residual real gap was a coverage hole, which
this change closes.

## Completed

### 1. Supersession (why there is no second gate)

The ticket's premise — "not done now only because a repo-wide gate that reddens
on 49 pre-existing sites gets disabled" — is stale. The gate it proposes
**exists and is strictly stronger**:

| Ticket asks for | Already in `internal/standards` (landed 2026-09-12) |
| --- | --- |
| fails on `os.Setenv(` in `*_test.go` | gate 2 `testEnvFind` (`gate.go:462`) reports `os.Setenv` **and** `os.Unsetenv` via go/ast |
| baseline allow-list (`file:line`) | `testEnvBaseline` in `baseline.go` — 96 exact `file:line` permits |
| stale-baseline error (ratchet down only) | `enforce()` reports STALE entries and fails |
| inline escape comment | `// env-leak-ok: <reason>` (`testEnvEscape`) |
| predicate must not match comments/strings | AST predicate — a comment naming the function is not a call; the `%v`/prose trap the ticket cites does not apply to an AST gate |
| red proof | `TestGate2TestEnvRedProof`, `TestGate2EnvRestoreShapeRedProof` |

The sibling ticket's worst shape is *also* covered: `testEnvRestoreShapeFind`
reports `os.Getenv(LIT)` captured into a variable and written back with
`os.Setenv(LIT, thatVar)` in the same function — the lossy restore whose
`os.Getenv` cannot see absence.

Adding a second, parallel gate in `internal/testutil` would duplicate the same
predicate in two places (Rules 13/14: one definition, no mirror) and the ticket
itself names the internal/standards model as the thing to copy. So: **reuse,
don't mirror.**

### 2. The real residual gap — `testsuite/` was ungated

`scan()` traversed `prodRoots = {"internal", "cmd"}` only. The top-level e2e
suite `testsuite/` (5 `*_test.go` files) sat **outside every Rule 17 check** —
an `os.Setenv` leak there would be silent. This change:

- adds `testRoots` (prodRoots + `e2eTestRoot = "testsuite"`) in `gate.go`, a
  named constant rather than an inline literal (Rule 1b/14);
- adds a per-gate `scanRoots` override, applied by `scan()`, so **only** gate 2
  widens. The other gates keep `prodRoots`: their baselines are line-keyed
  inside `internal/` + `cmd/`, and widening their file set cannot re-point them;
- adds `TestGate2ScansTheE2ESuite`, which asserts the scan ROOT (not just the
  predicate) reaches `testsuite/` and that ≥1 e2e test file was actually
  traversed — a predicate probe would stay green even if the gate never opened
  a testsuite file.

### 3. Rule 17 text now names the gate

`GOPI.md` Rule 17 "How to verify" now states that the gate scans `internal/` +
`cmd/` + `testsuite/`, pinned by `TestGate2ScansTheE2ESuite`, and the stale
"(95 legacy sites baselined)" count is corrected to the measured 96.

## Not Done

- **No new baseline entries.** The tree is green with ZERO testsuite hits
  (`testsuite/` has 0 `os.Setenv`/`os.Unsetenv`), so the coverage expansion adds
  no permits — the list can still only ratchet down.
- **No conversion of the 96 baselined sites.** They are the child-process /
  process-level save-restore class the baseline header documents; the sibling
  Rule 17 work already ran the mechanical sweep (devdoc/style-sweep-20260912.md).
- **No gate in `internal/testutil`** — deliberately rejected as a duplicate
  predicate (see §1).
- The epic's headline case (the lossy `defer os.Setenv(k, old)` shape) is not
  re-implemented; it is already gated as the shape half of gate 2.

## Verification

- Build: `go build ./...`: **OK**
- `gofmt -l internal/standards/`: **clean**
- `go test ./internal/standards/`: **PASS** (all gates). Measured accounting from
  `TestStandardGateDataReport`:
  - Rule 17: `hits=107 baselined=96 escaped=11 NEW=0`
  - Rule 16 `NEW=0`, Rule 18 `NEW=0`, Rule 19 `NEW=0`, Rule 20 `NEW=0`, Rule 21 `NEW=0`
  - `TestGate2ScansTheE2ESuite`: `Rule 17 scans 334 test files, 5 of them under testsuite/`
  - (grep count of `os.Setenv(` in `*_test.go` is 46; the AST sees 107 real
    `Setenv/Unsetenv` calls — grep also matches calls inside Go string literals
    in gate/probe packages. The gate reports the AST number.)
- **Live red proofs (not just unit probes):**
  - inserted `os.Setenv("GOPI_GATE_PROBE","1")` into `testsuite/task_test.go` →
    `Rule 17: NEW violation (checklist 5.2) at testsuite/task_test.go:266` FAIL
    (reverted; `git status` clean for that file)
  - deleted the still-existing baseline entry `cmd/agent/testmain_test.go:30` →
    `NEW violation … at cmd/agent/testmain_test.go:30` FAIL (reverted)
- Commit: `209c9df` — `test(standards): Rule 17 gate covers the whole test surface (testsuite/)`
  (branch `feat/rule17-testsuite-gate`). The gate this report names is
  `internal/standards` gate 2 `testEnvFind`, verifiable with
  `go test -run 'TestGate2' ./internal/standards/`.

## Files

- `internal/standards/gate.go` — `testRoots`/`e2eTestRoot`, `gate.scanRoots`, `scan()` honours it
- `internal/standards/gate_test.go` — gate 2 `scanRoots`, `TestGate2ScansTheE2ESuite`
- `GOPI.md` — Rule 17 verification line (gate name + testsuite coverage + count)
