<!-- doc-class: historical -->
# The failure list is read from the stream's events, not from a marker's text shape

Task: `01a12e58-99fb-7ee9-834b-f807e8564fde` (branch `task/01a12e5899fb7ee9834bf807e8564fde`).

## The defect (measured, not inferred)

`scripts/test-budget.sh` rendered its failures block from a **text harvest** over
the whole `go test -json` stream:

```bash
fan_out_failures() {
    grep -a -o -- '--- FAIL: [^"\\]*' "$raw_json" | head -"$slowest_report_n"
}
```

A `--- FAIL:` line inside an event's `Output` field is **data**, not a verdict:
a PASSING test prints that line whenever it quotes a `go test` result. Measured
on this script's own narrow run
(`queue/test-budget/run-1791782756-1190003.json`, task `01a12e42`):

| | |
|---|---|
| text harvest (the block) | 6 names |
| `Action=="fail"` events with a `Test` field | 4 |
| names in the block that exist as tests of this repo | 4 of 6 |

The two phantom names — `TestAnchor`, `TestPublishedAnchorResolves` — are printed
by `TestPublishedAnchorGateRefusesAFailingGate` and
`TestFinishCleanupLocalSyncQuotesTheFailingPackage`, **both of which PASS**;
`go test ./internal/tools/ -list 'TestAnchor$|TestPublishedAnchorResolves'` lists
neither, and `-run '^TestAnchor$'` prints `[no tests to run]`.

The same block printed **nothing** for the package-level failures: a package that
never compiled (`Action=="build-fail"`, `FailedBuild` set) carries **no `Package`
and no `Test` field**, so no marker line ever carried it. A build failure — the
loudest thing a Go run can produce — was invisible (Rule 15).

## The shape of the fix

`scripts/lib/stream-failures.sh`: **one extractor, several views** (the shape
`scripts/lib/stream-scope.sh` established, Rule 14).

`gopi_stream_failure_events STREAM` normalises every failure-shaped event into one
TAB-separated line:

```
kind            package                test or text
test            <import path>          <failing test name>
package         <import path>          (empty — a package-level verdict)
build           <import path>          (empty)
build-output    <import path>          <the compiler's own line>
```

so the JSON field extraction (and its tolerance of the two encodings Go may emit)
exists in exactly ONE place; each public function is then a trivial read over it:

| function | answers |
|---|---|
| `gopi_stream_failures` | the failing TESTS, `package<TAB>test` |
| `gopi_stream_failed_packages` | every failed package, whatever the kind |
| `gopi_stream_package_failures` | the failures with NO test to name: `build` / `package` |
| `gopi_stream_build_output` | the compiler's own diagnostics, unescaped |
| `gopi_stream_failure_count` | how many tests failed — from the same events as the list |

`scripts/test-budget.sh` renders the block in `report_failures`, which prints the
readable marker text (`--- FAIL: <test>  (<package>)` — a reader still recognises
the shape) plus a second block for the failures that have no test to name, with
the compiler's own lines quoted under them.

## Three findings the real streams produced (each one changed the code)

1. **A failing package also emits a Test-less `Action=="fail"` event** (measured:
   `{"Action":"fail","Package":"…/internal/standards","Elapsed":37.803}` next to
   its four per-test failures). "A fail event with no Test" therefore does NOT
   mean "there is no test to name" — the `package` kind is reported only when the
   package has neither a build failure nor a named failing test. Without this, the
   block announced a package-level failure for every ordinary failing package.
2. **A JSON-string read anchored on a shape regex keeps the fields that follow the
   value.** The first build-output read produced
   `… undefined: thisIdentifierDoesNotExist "}` — the `,"Elapsed":…}` tail. The
   read now scans to the value's terminating quote, stepping over `\"`.
3. **`awk` reads the stream correctly where `grep` needs `-a`.** GNU awk does not
   re-encode a NUL byte, so `"…\001…"` does not collide with the unescape
   placeholder this library uses; the gate pins the NUL case anyway.

## Why a library and not inline code

Same reason as `stream-scope.sh`: a reporter that waits for a real run to print a
fixture is not a guard. As a library it is called with **synthetic streams** by
`internal/testutil/testbudget_failures_gate_test.go` — the "a passing test printed
a `--- FAIL:` line" case is asserted, not waited for — and the end-to-end arm runs
the **real script** against a stub `go` on PATH (the pattern
`testbudget_concurrent_test.go` established: cheap, deterministic, no test bodies,
no device work — Rule 27).

## What is NOT changed

- The TIMEOUT block still reads the panic text with `grep -a`
  (`fan_out_timeout_marker`): `panic: test timed out after` is Go's own panic
  output inside an output event, not an event action, and no passing test's
  fixture can produce it.
- `scripts/e2e_smoke.sh` keeps its `grep -c '^--- FAIL: TestTask_'` harvest of a
  **plain-text** log: that file is not a JSON stream and has no events to read.
- The stream-scope check, the per-run stream naming and the exit-code contract are
  untouched.
