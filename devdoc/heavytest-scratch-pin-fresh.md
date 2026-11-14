<!-- doc-class: historical -->
# Task Report: pin the `internal/heavytest` suite scratch root (Rule 22 gate red at base)

Branch: `fix/heavytest-pin-fresh` (fresh branch from master `a9a96cd7`, per Rule 23 —
the prior attempt `fix/heavytest-scratch-pin` @ `f646aace` was READ, not continued).

## The defect

`internal/heavytest` allocates scratch directories — `boundary_test.go:81`
(`boundaryFixture`'s generated fixture module) and `boundary_test.go:265`
(`TestBoundaryRefusalWritesTheLedger`'s skip-ledger path) — but had no `TestMain`,
so nothing pinned the suite scratch root and both `t.TempDir()` calls resolved to the
AMBIENT temp root: `/tmp` on this host, a directory on the `/` partition (the
87%-full boot partition behind the 2026-09-11 exhaustion). Exactly the implicit
fallback Rule 22 exists to ban.

The gate is static and was doing its job:
`internal/testutil/scripts_gate_test.go:264`, `TestEveryTempUsingSuiteIsPinned`.

## Completed

- Added `internal/heavytest/testmain_scratchroot_test.go` with
  `func TestMain(m *testing.M) { testutil.RunSuiteMain(m, "heavytest") }`.
- **Route (a) PIN, not (b) RELOCATE** — the deciding evidence: both scratch sites are
  created BY THIS PACKAGE'S OWN tests (a throwaway fixture module; a ledger file), not by
  a shared helper delegating to an already-pinned suite, so there is nothing to hand off to
  `internal/scratch.MkdirTemp`. A package pin is the only correct answer here, and it also is
  what the gate's own message names. `boundary_test.go` runs smoke (unclassified by
  `heavytest.Require`), i.e. on the merge/verify path — the pin is what matters most there.
- **Leaf design intact**: `internal/heavytest/heavy.go` documents this package as a leaf with
  ZERO project imports. The `testutil` import sits in the `_test.go` only, so the production
  import set is unchanged — proven with `go list -deps`, not argued (see Evidence).
- **The two gates AGREE; only their totals differ (the epic's question, answered).**
  `TestEveryTempUsingSuiteIsPinned` and `TestPinCoverageTable` scan the SAME predicate and the
  SAME marker set (`usesScratchTemp`, `pinEntryPointMarkers`) and both flagged exactly
  `internal/heavytest` as the one unpinned scratch-using package. The disagreement in the
  counts is a different number, not a different verdict:
  - `TestEveryTempUsingSuiteIsPinned` fails with `internal/heavytest` (one message) and its
    own read floor `scanned >= gateMinTempPackages (5)` — a floor over a different quantity.
  - `TestPinCoverageTable` reports `packages: 41 scanned | scratch-using: 32 | pinned: 31`.
  - The briefing's "42 scanned" came from a tree that carried one extra test package
    (the branch it was measured on); here the same scan reads 41. So the marker scan does NOT
    under-detect — the widened detector found 32 of 32, `+0 newly detected`, i.e. no package
    reaches scratch through a channel outside the marker set.
- After the pin: coverage reads `internal/heavytest … PIN / PIN` and `pinned: 32` — pinned ==
  scratch-using, no unpinned package left.

## Not Done

- Nothing deferred. The fix is the whole defect: one pin file, no production code touched, no
  gate predicate/baseline/exemption edited (Rule 27 — a silent skip would be a deleted test).

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK (no output, exit 0); `CGO_ENABLED=0 go vet ./internal/heavytest/` → OK.
- Tests:
  - `CGO_ENABLED=0 go test -count=1 -run TestEveryTempUsingSuiteIsPinned ./internal/testutil/` → `ok  … 0.866s`
  - `CGO_ENABLED=0 go test -count=1 -run TestPinCoverageTable -v ./internal/testutil/` → `ok … 0.705s`, `pinned: 32`
  - `CGO_ENABLED=0 go test -count=1 -v ./internal/heavytest/` → `ok github.com/nick/gopi/internal/heavytest 2.779s` (all PASS, incl. TestBoundaryRefusalWritesTheLedger)
- Commit: see `git log -1` on `fix/heavytest-pin-fresh`.

## Evidence (self-filed)

- **RED at base** (before any edit, fresh worktree on `a9a96cd7`):
  `go test -count=1 -run TestEveryTempUsingSuiteIsPinned ./internal/testutil/` →
  `--- FAIL: TestEveryTempUsingSuiteIsPinned (0.06s)` / `scripts_gate_test.go:264: internal/heavytest
  creates scratch directories (t.TempDir()/os.MkdirTemp) but no TestMain pins the scratch root`.
- **NOT-WEAKENED CONTROL** (this is what proves the green came from the pin, not from a relaxed
  predicate): moved the pin file aside (`mv internal/heavytest/testmain_scratchroot_test.go
  /home/user/workspace/.heavytest-pin-aside.go`), re-ran the gate → the IDENTICAL `FAIL` at
  `scripts_gate_test.go:264` with the identical message; restored → `ok`. No allow-list,
  baseline or skip was added anywhere.
- **Gate green after**: `ok github.com/nick/gopi/internal/testutil 0.866s`.
- **Read floor (vacuous-pass guard) still satisfied, and re-checked by construction**: with the
  pin removed the gate still reported the heavytest message rather than the `audited only %d
  temp-using test packages` fatal — i.e. `scanned` (31+) is far above `gateMinTempPackages = 5`
  in both the red and the green run. `TestPinCoverageTable`'s own zero-guards
  (`legacyCount == 0 || currentCount == 0`) and monotonicity check (`current < legacy`) both
  passed: `scratch-using: 32 by the ORIGINAL markers, 32 by the widened markers (+0 newly detected)`.
- **Leaf proof**: `CGO_ENABLED=0 go list -deps ./internal/heavytest | grep -c 'nick/gopi'` → `1`
  (only `github.com/nick/gopi/internal/heavytest` itself; `testutil` and its transitive deps are
  `_test`-only). Measured before AND after the change — unchanged.
- **Rule 22, measured not spelled**: `stat -c %d /` → `2051`; the repo's scratch home
  (`/home/user/workspace/gopi-test-scratch`) → `2052` — different devices, so the scratch is
  off the `/` partition. Runtime confirmation that the pin actually installs for this suite:
  `GOPI_DEBUG=1 go test -run TestBoundaryRefusalWritesTheLedger -v ./internal/heavytest/` printed
  `testutil: suite "heavytest": the ledger sandbox
  /home/user/workspace/gopi-test-scratch/suite-heavytest-run-1084266320/suite-ledger/memory.db …`
  — the suite now runs under its own pinned scratch root instead of the ambient one.
- **Rule 30**: `git status --porcelain` in the worktree lists only
  `?? internal/heavytest/testmain_scratchroot_test.go` before the commit; nothing left after it.
