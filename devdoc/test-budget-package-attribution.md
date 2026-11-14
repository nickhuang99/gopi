<!-- doc-class: normative -->
# Package-level failures name THEIR OWN evidence — attribution by import path, never by basename

Task: `01a12e7a-b997-7869-81a5-b907b7f6d8b8` (branch `task/01a12e7ab997786981a5b907b7f6d8b8`).

## What was wrong (defect 1 — inherited, now fixed upstream)

`scripts/test-budget.sh` rendered its failures block by TEXT SHAPE:

```bash
fan_out_failures() { grep -a -o -- '--- FAIL: [^"\\]*' "$raw_json" | head -n; }
```

A `--- FAIL:` line inside an event's `Output` field is DATA, not a verdict — a
PASSING test prints exactly that line when it quotes its own fixture
(`internal/tools/task_finish_premerge_checks_test.go:86` quotes `pmcFailBlock`).
Measured on the queue's own 600 historical streams: 21 streams reported names
that carry no `Action=="fail"` event at all, and 4 streams reported two failures
while carrying none. Both phantom names (`TestAnchor`,
`TestPublishedAnchorResolves`) are **not test functions of this repository**:
`go test ./internal/tools -list 'Anchor'` lists neither, `-run '^TestPublishedAnchorResolves$'` prints `[no tests to run]`. <!-- selector-ok: THIS LINE IS THE MEASUREMENT — the selector names a test that does NOT exist, and `[no tests to run]` is the evidence it does not. That absence is this paragraph's whole point. --> The same block
printed NOTHING for a package that never compiled (Rule 15).

That half is fixed by the upstream branch
`task/01a12e5899fb7ee9834bf807e8564fde` (commit `c21c84962`,
`scripts/lib/stream-failures.sh`): every failure-shaped event is normalised to
`kind<TAB>package<TAB>test-or-text`, so `gopi_stream_failures` answers "which
TESTS failed" and `gopi_stream_package_failures` answers "which PACKAGES failed
with no test to name". Verified here on a real capture (three-way, below):
phantom gone, package-level named.

## What is still wrong (defect 2 — the gap this branch closes)

Two holes remain in the upstream shape, both **measured with a real
`go test -json` capture**, not inferred:

1. **`report_package_failures` attributes compiler diagnostics by basename.**
   It resolves the quoted lines with
   `gopi_stream_build_output "$raw_json" | grep -F -- "$(basename "$pkg")"`.
   Two packages in one run can share a basename (`p1/tools`, `p2/tools` — the
   fixture is a throwaway module's own `go test -json ./...`), so **each heading
   quotes the other package's compiler line as its own evidence**. It is not
   silence; it is a wrong answer wearing a right one's shape.
2. **A `package`-kind verdict quotes the whole stream's build output.** For a
   TestMain that exits non-zero the block resolved its evidence with
   `gopi_stream_build_output "$raw_json"` — the entire stream's build-output
   rows. The package's OWN evidence (`exit status 3`, an `Action=="output"` row
   with NO `Test` field) was printed nowhere, and if any other package failed to
   compile, that other package's lines were shown as this verdict's.

## The fix

`scripts/lib/stream-failures.sh` — the same one-extractor-several-views shape,
extended by two normalised kinds and one attribution helper (Rule 14):

* `gopi_stream_failure_events` additionally emits `package-output` rows: an
  `Action=="output"` row that carries NO `Test` field is the PACKAGE's own
  output (a TestMain exit, a panic caught before any test ran). Rows carrying a
  `Test` stay that test's log and are not emitted here.
* `gopi_stream_rows_for_kind STREAM KIND PACKAGE` — the third column of one
  kind's rows belonging to ONE package, matched on the **full import path**
  (never a substring). `gopi_stream_build_output_for` and
  `gopi_stream_package_output_for` are its two named readers; the stream-wide
  `gopi_stream_build_output` stays for a reader who wants the whole picture.

`scripts/test-budget.sh` — `report_package_failures` quotes each heading's OWN
evidence: a `build` section prints that package's compiler diagnostics, a
`package` section prints that package's own output rows. When the stream carries
none for a heading, the block **says so** instead of printing a bare heading
(Rule 15).

Deliberately NOT changed: fixture strings and the `--- FAIL:` marker text the
report still prints (the marker is how a reader recognises a failure; the
defect was SELECTING by it, not printing it).

## Evidence

Three-way comparison on the SAME raw json, by running the real
`scripts/test-budget.sh` end to end against a stub `go` (evidence:
`e2e-three-way-twobroken.txt`, `e2e-three-way-testmain.txt`,
`e2e-three-way-realrun.txt` in this session's `artifacts/`):

| stream | OLD (master `88b7584af`) | upstream fix (`0e443ceff`) | this branch |
|---|---|---|---|
| `real-twobroken-same-basename.json` (two build-failed packages sharing a basename) | failures block EMPTY | both named, but **each quotes the other's compiler line** | each quotes ITS OWN line |
| `real-testmain-exit.json` (TestMain `os.Exit(3)`) | failures block EMPTY | named, **no evidence under it** | named + `exit status 3` |
| `run-1791780424-737215.json` (a real run: 2 real failures + 2 phantom names) | 2 real + 2 phantom (`TestAnchor`, `TestPublishedAnchorResolves`) | 2 real, no phantom | 2 real, no phantom |

Red→green gate: `internal/testutil/testbudget_package_attribution_gate_test.go`
— `TestBudgetBuildDiagnosticsStayWithTheirOwnPackage` and
`TestBudgetPackageLevelVerdictQuotesItsOwnEvidence`, both failing against
`0e443ceff` and passing with the fix.
