<!-- doc-class: normative -->
# Task Report: the Rule 22 pin gate's FAIL text now names a remedy a nested module can run

**task**: `01a133522c707292a4acea85596b76ff` · **baseline**: `b99fc60` · **branch**:
`task/01a133522c707292a4acea85596b76ff`

**Epic**: `internal/testutil/scripts_gate_test.go` `TestEveryTempUsingSuiteIsPinned`
(+ `pin_coverage_test.go` `TestPinCoverageTable`) walks EVERY directory holding
`*_test.go` — including the standalone nested Go modules under `devdoc/`. Its remedy text
prescribed `testutil.RunSuiteMain(m, "<name>")`, which Go refuses for a nested module. The
predicate is right; the INSTRUCTION was not actionable for that class.
**Deliverable = repair the text, never relax the predicate.**

## Completed

- **`scripts_gate_test.go`**: the FAIL text is now shape-dependent (Modules ≠ the
  module). `scratchPinFailure(root, rel)` renders one of two remedies, decided by
  `nestedModuleRoot` — the nearest ancestor of the package that holds its own `go.mod`
  (never the repo root, whose own `go.mod` is the gopi module). Every sentence lives in a
  named constant (Rules 1/13): `scratchPinVerdict` (shared diagnosis), `scratchPinFixInTree`,
  `scratchPinFixInTreeOwnRoot`, `scratchPinFixNestedModule`, `scratchPinWhyNestedNotImportable`,
  `nestedModuleMarker`.
- **`pin_coverage_test.go`**: its sibling row carried the same in-tree-only instruction;
  it now routes through `scratchPinCoverageFailure`, reusing the SAME constants, so the two
  gates cannot drift into telling a reader two different things.
- **Regression test** (`scratch_pin_message_test.go`, new): pins the message for BOTH shapes
  (fixture root, real tree) and asserts the nested text does NOT carry the in-tree pin.
- **Cherry-picked `284785aac`** (`-x`, recorded in the message), which removes the one live
  offender's scratch — see "Deconfliction" below.

## The two remedies the text now names (both measured, not asserted)

Run of `artifacts/nested-remedy-probe.sh` in a throwaway module under the scratch home:

1. **REMOVE the scratch** — the temp file was only ever READ, so its bytes become a read-only
   `testdata/` fixture: `grep -c 't.TempDir()'` → `0`, and with `TMPDIR` unset the test passes:
   nothing to pin, nothing to redirect. (This is exactly what `284785aac` did to
   `devdoc/doubao-asr-eval/minclient`, verified independently by that task.)
2. **Redirect it in the module's OWN TestMain** — `os.Setenv("TMPDIR", <dir off />)`:
   `scratch=/dev/shm/.../TestScratchRootDeviceDiffersFromRoot…/001 device=28  / device=2051`
   → PASS. No gopi import is involved, so the module's zero-dependency declaration survives.
3. **(disclosed, not prescribed)** if a module can do neither, the text says so and says to
   report it. Because an honest disclosure cannot pre-empt an unforeseen shape, this is the ONE
   place where a follow-up may need to narrow the predicate for nested modules — see
   "Not Done".

**The in-tree pin really is impossible for that class** (probe B, verbatim):
`import _ "github.com/the operator/gopi/internal/testutil"` → `no required module provides package`;
after a `require`+`replace` edge → `use of internal package github.com/the operator/gopi/internal/testutil
not allowed` **plus** `missing go.sum entry … modernc.org/sqlite` — the whole gopi dependency
tree, in a module whose `go.mod` states it depends on nothing outside the stdlib.

## Not Done (honest)

- **A writer path for the nested remedy is not prescribed by the text.** The gate is a static
  scan of test sources: its pin marker is `pinEntryPointMarkers` (the three `testutil.*`
  spellings). A nested module applying remedy 2 sets `TMPDIR` in its own TestMain, which the
  marker cannot SEE, so that package is detected as scratch-using and unpinned until the
  predicate learns a second marker (e.g. a `TMPDIR` assignment in a TestMain). That is a
  predicate change, which this epic forbids, so it is filed as a follow-up row
  (`model_scan_markers`-shaped, "widen `pinEntryPointMarkers` for nested standalone modules"
  or exclude nested modules from the pin gate with the criterion stated) rather than done here.
  Measured today: `devdoc/doubao-asr-eval/minclient` reached green by remedy 1, so no live
  offender needs it.
- **The checker's duplication was NOT touched**: `pin_coverage_test.go`'s stub row (line ~8-16)
  and the gate's doc comment (line ~1-27) both RESTATE the same history; that is a
  deduplication task of its own and out of this epic's scope.

## Resolution of the gap (task `01a13368fdc17dfda71ec0b12521146d`)

**Route (a): the detector was taught the shape through `go/ast` — the predicate was NOT
relaxed.** No baseline, no exemption, no comment-satisfiable marker; `pinEntryPointMarkers`
still holds exactly the three in-tree spellings, and nothing previously reported NO-PIN is now
exempt.

- **The completed predicate is ONE function**, `packagePinned` (`pin_redirect_scan_test.go`),
  called by BOTH gates (`TestEveryTempUsingSuiteIsPinned` in `scripts_gate_test.go` and
  `TestPinCoverageTable` in `pin_coverage_test.go`), so their verdicts cannot disagree:
  `containsAny(src, pinEntryPointMarkers)` OR a `func TestMain(m *testing.M)` whose BODY ASSIGNS
  the pin env name (`os.Setenv`/`t.Setenv`).
- **The evidence is the assignment, not prose.** The accepted shape is a call in a parsed
  TestMain body; a comment naming the variable in any spelling adds nothing. The refused
  assigned values are the ones that are the ambient root BY DEFINITION — the banned root's own
  path literal (or a path under it), `os.TempDir()` (including `filepath.Join(os.TempDir(), …)`)
  and `os.Getenv` of the variable itself. Every other directory is accepted, and its DEVICE ID
  stays the runtime fact the FAIL text tells the module to satisfy: Rule 22's criterion is
  untouched — a static scan cannot measure device numbers, and this one does not claim to.
- **The text did not move**, so `scratch_pin_message_test.go` keeps its assertions and gained
  the AGREEMENT test (`TestNestedRemedyIsARecognisedPinShape`): a nested package carrying out
  remedy (2) flips `packagePinned` to true, and the same package with the remedy reduced to a
  comment stays NO-PIN.
- **Red proof (the refused shape stays NO-PIN at the same code path):** with
  `packageDeclaresPinRedirect` forced to `false` — the pre-fix, marker-only predicate — the live
  run reported the failure for a package that had applied remedy (2) exactly as instructed:
  `scratch_pin_message_test.go:207: a nested package that carried out the text's own remedy (2)
  is still reported NO-PIN`, `red-exit=1` (`artifacts/red1-scan-removed.log`). The restore was
  verified against an explicit revision: snapshot `cmp` → IDENTICAL, blob
  `74d7b0e3fb6ce1d59855b1622ee1c1317eb2dc60` on both sides.
- **Green at the tip:** both gates plus all four proof tests pass, and the coverage table reads
  `76 scanned | scratch-using: 53 | pinned: 53` — the completion changes no standing verdict on
  today's tree (no nested package applies remedy 2 yet), which is exactly why the proof is a
  fixture rather than a live offender.

## Verification

- **Build**: `go build ./...` → OK (exit 0); `go vet ./internal/testutil/` → clean.
- **Tests**: `go test -run 'TestEveryTempUsingSuiteIsPinned|TestPinCoverageTable' -count=1
  -timeout 20m -v ./internal/testutil/` → **exit 0** at the tip
  (`--- PASS: TestPinCoverageTable`, `--- PASS: TestEveryTempUsingSuiteIsPinned`,
  `packages: 76 scanned | scratch-using: 53 … | pinned: 53`).
- **Ran** (the surface this change IS — the gate's own emitted text; every line below is from
  the live run, not from the source):
  - `go test -count=1 -run 'TestEveryTempUsingSuiteIsPinned|TestPinCoverageTable' -v ./internal/testutil/`
    on the BASELINE `b99fc60` (`artifacts/gate-baseline.log`) → **exit 1**, and the emitted text was the
    unrepaired one: `scripts_gate_test.go:313: … the 87%-full / partition behind the 2026-09-11
    exhaustion). FIX: add a TestMain calling testutil.RunSuiteMain(m, "minclient")` — for a package
    that cannot run it.
  - The same command after the repair, still with the offender present (`artifacts/gate-after.log`)
    → **exit 1, identical verdict** (same two tests, same offender, `pinned: 53` of 54) with the new
    text: `devdoc/doubao-asr-eval/minclient (nested, standalone nested module rooted here): … FIX
    (nested standalone module): … (1) REMOVE the scratch creation … (2) … redirect it in this
    module's OWN TestMain … WHY NOT the in-tree remedy here: …`. The predicate never moved.
  - Move-aside control, live (`artifacts/redgreen-and-control.log`, PART 2): a fresh in-tree package
    `internal/zzpinprobe/{probe_test.go}` with `t.TempDir()` and no pin → gate **FAIL** with the
    in-tree remedy naming `testutil.RunSuiteMain(m, "zzpinprobe")`; `mv` it aside → that failure
    disappears and the nested offender's failure remains; the probe was deleted (the worktree's
    `git status --porcelain` lists no probe).
  - Red-then-green for the NEW message test, live (same log, PART 1): the mutation that restores the
    old in-tree-only text for the nested shape → `--- FAIL:
    TestScratchPinFailureNamesTheRemedyForEachShape/nested_module_is_handed_the_nested_remedy,_never_the_in-tree_one`,
    `red-exit=1`; restore verified by an explicit-revision comparison (`cmp` against the snapshot
    taken before the mutation → identical; sha256 `b51a2999…`) → `green-exit=0`.
- **Result**: the branch tip is green on both gates; the baseline reproduces red byte-for-byte;
  the remedy text for the nested shape contains no unactionable instruction and both named remedies
  were executed successfully inside a real nested module.
- **Commit**: `HEAD` on `task/01a133522c707292a4acea85596b76ff` (see the final message for the hash).

## Evidence (self-filed)

- `artifacts/gate-baseline.log` — both gates RED at `b99fc60`, exit 1 (the log also carries the
  unrelated `scratch: GC could not remove … transport endpoint is not connected` lines, which are
  ambient host noise, not this run's finding).
- `artifacts/gate-after.log` — both gates still RED with the repaired text, exit 1, `pinned: 53` of 54.
- `artifacts/gate-tip.log` — both gates GREEN at the tip, exit 0.
- `artifacts/redgreen-and-control.log` — the message test red (`red-exit=1`) and green
  (`green-exit=0`) with the restore compared against an explicit snapshot, plus the in-tree
  move-aside control.
- `artifacts/nested-remedy-probe.sh` + `artifacts/nested-module-remedy-probe.log` — the probe
  module (created under `/home/user/workspace/gopi-test-scratch/`, removed at the end) and the
  raw outputs of both remedies plus the failed in-tree import.
- Device ids measured here (Rule 22's criterion, not a spelling): `/ device=2051`,
  `/tmp device=2051` (the banned shape), `/dev/shm device=28`.

## Deconfliction (a concurrent task removed the live offender mid-run)

`284785aac` (`fix(testutil gate): minclient drops its temp-dir scratch for read-only testdata`)
landed on the peer's branch while this run was working, and is NOT an ancestor of this branch's
base `b99fc60`; the gates read `57` packages that round rather than `76`, and this worktree's
tree still contained the `t.TempDir()` sites. So the base is RED for the same reason `284785aac`
fixed — the same red, different remedies. Per Rule 26 (an incomplete fix is repaired by
dispatching again, overlapping work is expected, and the briefing carries the reason) this run
cherry-picked `284785aac` (`-x`, provenance in the message) after proving the tip green with it,
so the two halves land as one mergeable deliverable: the text repair (this run) and the removal
of the last offender (that one). Without the pick the branch would have been red and the green
evidence unavailable.
