<!-- doc-class: historical -->
# devdoc: internal/smokegate scratch-root pin backfill (Rule 22)

**Task**: `01a12d34-fe6b-79c6-a10f-adc59c9cf531`  ·  **Branch**: `task/01a12d34fe6b79c6a10fadc59c9cf531`

## Symptom (pristine master tip, before this change)

`internal/testutil`'s two Rule 22 pin gates are RED at the branch point, with no
contribution from any other work:

```
--- FAIL: TestPinCoverageTable
    pin_coverage_test.go:92: internal/smokegate creates scratch (t.TempDir()) but no TestMain
        routes the pin through internal/testutil — its scratch lands on the ambient temp root
    pin_coverage_test.go:99: packages: 72 scanned | scratch-using: 51 | pinned: 50
--- FAIL: TestEveryTempUsingSuiteIsPinned
    scripts_gate_test.go:313: internal/smokegate creates scratch directories (t.TempDir()/os.MkdirTemp)
        but no TestMain pins the scratch root ...
```

`internal/smokegate/smokegate_test.go:87` (`empty := t.TempDir()`) and `:97`
(`dir := t.TempDir()`) are real scratch sites, so the `t.TempDir()` marker set
legitimately fires. Without a pin, both fixtures land on `os.TempDir()` — on this
host the caller's `TMPDIR`, and on a default host `/tmp`, a directory on the `/`
partition behind the 2026-09-11 exhaustion. This is **backfill, not a
regression**: the gate landed after this package existed.

## Root cause (5-why, stopped at the norm)

1. Symptom: the two gates fail naming `internal/smokegate`.
2. Mechanism: the gate demands a pin for every package whose test sources carry a
   scratch marker (`t.TempDir()`, `os.MkdirTemp(`, `os.TempDir()`, a `"/tmp"`
   literal) and finds no `testutil.RunSuiteMain` / `PinSuiteForTestMain` /
   `PinSuiteTempRoot` marker in its test tree.
3. Mechanism: `internal/smokegate` has no `TestMain` at all.
4. Why: the suite-pin system was introduced package by package; `smokegate` was
   created as a zero-dependency leaf and never picked one up.
5. Norm (Rule 22): a test process's temp root must be provably OFF the `/`
   partition — a criterion measured by device id — and the ONE mechanism that
   establishes it is the suite entry point `testutil.RunSuiteMain`.

## Fix (surgical, additive — Rule 4)

`internal/smokegate/testmain_scratchroot_test.go` (new file, test-only, no
production line touched):

```go
func TestMain(m *testing.M) {
	testutil.RunSuiteMain(m, "smokegate")
}
```

The same shape as `internal/capturesurvey`, `internal/cgroupscope`,
`internal/checkpoint`, `internal/boundedfd` … — the ONE entry point 30+ suites
already share, so the pin also installs the notify-env sandbox
(`suiteenv.go`), the ledger sandbox (`ledgerpin.go`) and the worktree-root pin.
A hand-rolled `TestMain` would have given up all four.

## Rule 22 truth — measured, never a path spelling

| what | device id | source |
|---|---|---|
| `/` | `66308` | `stat -c %d /` |
| `/dev/shm` (the memory base) | `27` | `stat -c %d /dev/shm` |
| **this suite's scratch root** | **`27`** | `stat -c %d /dev/shm/suite-smokegate-run-1139196707`, observed live during the run |

The suite root is a `suite-smokegate-run-<N>` directory the pin creates and
removes per run (`PinSuiteTempRoot` → `os.MkdirTemp(home, scratchRunDirName(suite))`),
so it exists only while the suite runs — polled live, not from a script writing
to `/tmp`. Device `27 ≠ 66308` = off `/`.

A second, deterministic leg: with `GOPI_TEST_TMPDIR=/dev/shm/gopi-pin-proof`
(the caller's explicit layer, Rule 22 precedence) the pin materialises
`fixture-suite-smokegate-run-` inside that path, device `27`. This is the leg a
re-run can reproduce without a canary call.

## Not weakened — the pin moved aside must go red at the same point

`mv internal/smokegate/testmain_scratchroot_test.go` aside → the SAME command
fails with the SAME message at the SAME line (`pin_coverage_test.go:92`,
`scripts_gate_test.go:313`), `pinned: 51 → 50`; restoring the file → green. The
delta is exactly the pin, so the green is not a gate that stopped looking.

## Dependency budget (Rule 35 / leaf package)

`go list -deps ./internal/smokegate` (production closure) is unchanged: the
package's ONLY project dependency is itself. The test-only closure gains what
every pinned suite already pulls (`internal/testutil` and its own deps);
`go build ./...` passes, so no import cycle exists. The gate row that used
`smokegate` as a "tiny, dependency-free" probe
(`internal/testutil/testbudget_notests_gate_test.go`) still passes unchanged.

## Evidence

`~/.gopi/sessions/01a12d34-fe6b-79c6-a10f-adc59c9cf531/artifacts/`:
`red-pristine-master.txt`, `green-after-pin.txt`, `green-smokegate-suite.txt`,
`control-pin-moved-red.txt`, `control-pin-restored-green.txt`,
`scratch-root-device.txt`, `leaf-deps-before.txt`, `leaf-deps-after.txt`.

See also `devdoc/tmp-ban-scratch-root.md` (the normative doc for the pin).
