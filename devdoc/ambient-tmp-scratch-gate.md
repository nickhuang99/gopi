<!-- doc-class: historical -->
# Task Report: the /tmp ban's remaining hole — AMBIENT scratch writes in test code

Epic: `bug(test): cmd/agent writes test-built gopi binaries to fixed /tmp paths and
never cleans them` (01a08dcc-fef2-7a22-b47b-b48d7031a00b), from the test-suite rigor
audit (`devdoc/test-code-discipline-audit.md`).

## Completed

### 1. Re-measured first: the epic's three NAMED sites were already fixed on master

The briefing's three sites no longer exist. Commit **d7b6d3b** ("test(scratch): extend
the /tmp gate to Go test sources + stop building probe binaries into /tmp", on master
before this task started) already:

| briefing site | state on master @ 0cd8a90 |
| --- | --- |
| `cli_version_test.go:136` `-o /tmp/gopi-version-test` | **fixed** — builds into `os.MkdirTemp(testutil.SuiteTempRoot(), versionBuildDirPrefix)` |
| `home_settings_test.go:93` `-o /tmp/gopi-home-settings-test` | **fixed** — same shape, `homeSettingsBuildDirPrefix` |
| `settings_nilpanic_test.go:102-110` `os.MkdirTemp("", …)` | **fixed** — now `os.MkdirTemp(testutil.SuiteTempRoot(), …)` |
| gate (acceptance) | **exists** — `assertTestSourcesCreateNoTmpPath` in `internal/testutil/scripts_gate_test.go`, run from `TestScriptsAreBoundedAndOffTmp`, 325 test files scanned |

Measured, not assumed: a fresh `go test -count=1 -run 'TestVersion|TestHomeSettings|TestSettings' ./cmd/agent/`
→ `ok` in 5.5s and **zero** new `/tmp` entries (`diff` of `ls -1 /tmp` before/after is
empty; the three `gopi-*-test` files that remain are dated Sep 7–11, i.e. pre-fix
artifacts that nothing regenerated).

### 2. The hole the first pass left OPEN (found by re-measuring, not by reading)

The gate's predicate (`tmpPathIsCreatedBy`) requires a **`/tmp` STRING LITERAL on the
same line** as a creating call. That is exactly right for the fixed-path leak — and
blind to its silent twin: a test that reaches the ambient temp root **without ever
naming it**, `os.MkdirTemp("", …)` (empty dir = `os.TempDir()` = `/tmp` on a default
host) or `filepath.Join(os.TempDir(), …)` + a creating call.

That form was live in the tree (verified: exactly one test-source site repo-wide):

```
internal/agent/testmain_isolation_test.go:58
    dir, err := os.MkdirTemp("", agentSuiteScratchPrefix)   // pinSuiteLedgerEnv
```

and it is a **pin bypass**, not merely a missed literal: the `internal/agent` suite's
TestMain *does* call `testutil.PinSuiteForTestMain`, but this call ignores the pin and
creates a throwaway per-run ledger root **directly on `/tmp`**. Its branch on failure
`return ""` also swallowed the failure (Rule 15b), and `TestSuiteLedgerPin_IsActive`
only asserted that the env var *contained* `agentSuiteScratchPrefix` — a substring the
ambient path satisfies too, so the assertion was blind to the same bypass.

Other call sites were checked and are legitimate: `internal/testutil/scratchroot_test.go`,
`legacy_residue_test.go` and `scratchroot_entrypoint_test.go` use `banProbeDir`
(`/dev/shm`) as a deliberate ban-probe base, never the ambient root.

## The fix (test-code only — no production code touched)

1. **`internal/agent/testmain_isolation_test.go`** — `pinSuiteLedgerEnv` builds its root
   with `os.MkdirTemp(testutil.SuiteTempRoot(), …)` (the per-run pinned root, removed by
   `FinishSuiteForTestMain`) instead of `os.MkdirTemp("", …)`. The failure branch now
   **reports** to stderr instead of returning `""` silently (Rule 15b).
2. **The gate is now two predicates** (`internal/testutil/scripts_gate_test.go`),
   both run inside `TestScriptsAreBoundedAndOffTmp`:
   - `tmpPathIsCreatedBy` — unchanged: a `/tmp` **literal** handed to a creating call.
   - `ambientTmpRootIsCreatedBy` — NEW: an ambient spelling (`os.MkdirTemp(""`,
     `os.CreateTemp(""`, `os.TempDir()`) on the same line as a creating call, **unless**
     the line routes the path into `testutil.SuiteTempRoot()`. The narrow same-line
     predicate is deliberate: `filepath.Join(os.TempDir(), …)` that only builds a path
     for an argument parser is inert and is NOT flagged (a red-on-day-one gate gets
     disabled — the lesson from the first pass), and every repo-wide current use is
     checked to be either a fix-recommending mention or a synthetic test case.
   - `stripComment` was widened from shell-only to also strip whole-line and trailing
     Go `//` comments: the new rule's own documentation (and the test sources' prose
     about the ban) must not redden the gate that enforces it.
3. **`internal/agent` assertion strengthened** — `TestSuiteLedgerPin_IsActive` now
   requires the pinned ledger path to be **under `testutil.SuiteTempRoot()`**, which the
   ambient form cannot satisfy. Dropping `SuiteTempRoot()` from `pinSuiteLedgerEnv`
   makes it red.
4. **Minimal sample** — `TestAmbientTmpRootCreationPredicate`
   (`internal/testutil/pin_coverage_test.go`, 9 cases + complementarity check): the
   ambient predicate's cases, and an assertion that the **two predicates stay
   separable** (the literal rule must NOT claim ambient lines, else the second rule is
   dead weight hiding behind the first).

## Red-proof (the gate fails on the ORIGINAL form)

`perl`-reverted `pinSuiteLedgerEnv` to `os.MkdirTemp("", agentSuiteScratchPrefix)`, ran
`go test -run TestScriptsAreBoundedAndOffTmp ./internal/testutil/`:

```
--- FAIL: TestScriptsAreBoundedAndOffTmp (0.09s)
    scripts_gate_test.go:158: internal/agent/testmain_isolation_test.go:67 creates scratch
    through the AMBIENT temp root "dir, err := os.MkdirTemp(\"\", agentSuiteScratchPrefix)" —
    an empty dir argument means os.TempDir(), i.e. /tmp on a default host …
```

then restored the file byte-identically (`sha256sum -c` → `OK`) and re-ran → `ok`.
Leaving the suite green without the predicate would have proven nothing about what it
catches.

## Not Done (honest)

- **The three pre-fix `/tmp` artifacts are still on disk** (`/tmp/gopi-contract-test`
  19.7MB Sep 7, `/tmp/gopi-home-settings-test` 19.9MB Sep 8, `/tmp/gopi-version-test`
  20.3MB Sep 11 — ~60MB). Nothing regenerates them and nothing age-GCs them, and no
  live process maps them (checked `/proc/*/maps`). Removing host scratch is a
  **destructive host action outside a test-only ticket**, so it is left to the
  operator: `rm -f /tmp/gopi-contract-test /tmp/gopi-home-settings-test /tmp/gopi-version-test`
  (they are re-buildable artifacts of the old test code, not data).
- **The `/tmp` gate still cannot see a path built on one line and written on another**
  (same-line blind spot). Documented in the predicate's comment; closing it needs an
  AST/dataflow pass, which is a different (much larger) gate — not silently claimed here.
- **The triplicated `gopiBinOnce` build cache** (the sibling ticket
  `01a08dcd-1ed0-…`, last-writer-wins across `cli_version_test.go`,
  `home_settings_test.go`, `cli_contracts_test.go`, `settings_nilpanic_test.go`) is a
  SEPARATE work item and is deliberately NOT touched here — this change is scoped to the
  scratch/cleanup contract so the two can be reviewed independently.

## Verification

- Build: `go build ./...` — **OK**
- Gate + minimal samples: `go test -count=1 -run 'TestScriptsAreBoundedAndOffTmp|TestAmbientTmpRootCreationPredicate|TestTmpPathCreationPredicate|TestPinCoverageTable' ./internal/testutil/` — **ok**
- Suites: `go test -count=1 ./cmd/agent/` — **ok**; `go test -count=1 -run 'TestSuiteLedgerPin_IsActive|TestSuiteTempRootIsPinned|TestTaskregisterSessionStaysOutOfTheRealProfile' ./internal/agent/` — **ok**
- Acceptance (no new `/tmp` entry): `diff` of `ls -1 /tmp` before/after the suite runs — **no new entry**; `ls -1 /tmp | grep -c '^gopi'` unchanged by the run.
- Red-proof: gate FAILs on the original `os.MkdirTemp("", …)` form, green after byte-identical restore — recorded above.
- Commit: see the branch head (below).

## Deliverable

Branch **`fix/test-scratch-ambient-tmp`** in the shared repo (worktree
`~/workspace/gopi-tmpbin-fix`). Recovery: `git show fix/test-scratch-ambient-tmp:devdoc/ambient-tmp-scratch-gate.md`.
