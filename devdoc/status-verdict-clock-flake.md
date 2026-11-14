<!-- doc-class: normative -->
# The `task status` selector flake: a clock-derived field inside a byte comparison

Task row: `01a11df4-153d-787d-aa19-cfb97d256e1f` (cmd/agent).
Files changed: `cmd/agent/cli_tasks_status_selector_test.go`, `testdata/flakes.tsv`.

## The defect

`TestTaskStatusSelectorAndRowIDPrintTheSameVerdict` asserts the epic's headline:
the verdict an operator reaches through `gopi task status <row-id>` is the one he
reaches through `gopi task status --branch B` — same bytes. The two renders are
two invocations of the built binary, and each render carries a field that is a
render of `now` rather than a statement about the row: the recorded-runner start
instant.

`gopi task status` prints that instant twice from one tick count, through
`procStartTimeDisplay` (`internal/tools/taskproc.go:900`):

```
recorded:    pid=3698588 start=2026-10-09T08:10:57+08:00 cmd="…"   ← RFC3339 (also JSON)
started_at:  2026-10-09 08:11:00                                   ← the human column
```

Both are second-granular and both are computed AT RENDER TIME:

```
boot  = now - /proc/uptime          ← uptime is printed with CENTISECOND precision
start = boot + ticks/HZ             ← a fact about the row
```

So the computed `boot` — and therefore the rendered second — carries up to ~10 ms
of sawtooth that depends only on WHEN the renderer read `/proc/uptime`.

## Correction of the epic's premise (measured, not argued)

The briefing's framing was "two calls that cross a second boundary → the field
differs by 1 s". **That premise does not reproduce the failure**, and saying so is
the point of this section:

| measurement | result |
| --- | --- |
| the pre-fix raw byte comparison, two invocations 1.1 s apart | **EQUAL** — no flake |
| 400 real CLI invocations of ONE seeded row in one process (200 pairs), raw byte comparison | **1 distinct `recorded_start_at`, 0 mismatched pairs** |
| the same row rendered 24× per tick phase, driving the real renderer over one second of tick positions | at tick offset `+5`: `start=…08:19:27+08:00` AND `start=…08:19:28+08:00` from the SAME row — **1 risky phase in 100** |
| `/proc/uptime`-derived `boot`, 400 samples 3 ms apart | spread **9 ms** |

The reason a one-second gap is *safer* than a 10 ms gap: the fixture is created at
instant `T`, and its phase against the boundary is fixed at that moment. A pair of
invocations 1 s apart renders the same phase twice; a pair whose gap is not a
multiple of 1 s lands on a different phase each time. The failure probability per
invocation is the ~10 ms window in which the sawtooth straddles a boundary —
independent of how long the pair took.

The delivered comparison removes that field; the defect class is still real, still
clock-driven, and now registered (`testdata/flakes.tsv`).

## The fix

`statusVerdictDiff` is the ONE comparison. It normalises exactly the two
clock-derived shapes (RFC3339 instants, and the `started_at:` column) to
`<CLOCK-DERIVED>` and then compares **every other byte raw** — the `verdict:`
word, the row address, the evidence chain, the resumable flag. The headline test
requires it to be empty; the red proof requires it to be NON-empty for a verdict
that really differs, so a comparison that always said "equal" could not pass both.

The normalisation is **not** a weakening: nothing is turned into a wildcard, the
two strings are still compared for byte equality, and the excluded class is the
one field that does not describe the row.

### Two guards against the comparison becoming a tautology

1. **Non-vacuity of the exclusion** — the headline test asserts that BOTH renders
   carry a clock-derived field, i.e. that the fixture still records a runner
   identity. If it ever stops, the test fails loudly instead of passing with an
   exclusion that excludes nothing.
2. **Red proof** (`TestTaskStatusVerdictComparisonCatchesAGenuinelyDifferentVerdict`),
   three directions: a clock-only difference is NOT reported; a changed verdict
   word / row address / resumable flag IS reported; and end to end, two ledgers
   whose rows answer differently (`in_progress` vs `pending`) are reported as
   different by that same function.

### Honest boundary

The normalisation is text-shaped, not field-aware: a clock-shaped string printed
from ROW DATA (a summary or a recorded cmdline carrying an RFC3339 stamp) would be
normalised too. This fixture carries neither, so the assertion is exact here; a
future fixture with a timestamp in its summary must compare the structured
`--json` fields instead. This is recorded in the test's own comment.

## Verification

| check | result |
| --- | --- |
| `CGO_ENABLED=0 go build ./...` | exit 0 |
| `go test ./cmd/agent/ -run TestTaskStatusSelectorAndRowIDPrintTheSameVerdict -count=50` | **50/50 PASS** (floor: an earlier `-count=100` was 100/100) |
| red proof, comparison normalisation removed | **FAIL** (`the by id/branch render carries NO clock-derived field`; `two renders differing ONLY in their clock-derived fields were reported as a verdict difference`) |
| red proof, comparison made to always report "equal" | **FAIL** (`the verdict word / row address / resumable flag changed and the comparison reported NO difference`) |
| red proof, product mutation: `--branch` mis-resolved to another row | **FAIL** (`gopi task status … --json …: exit 4 (want 0)`) — the assertion still measures the verb |
| `go test ./internal/standards/ -run 'TestFlakeRegistry\|TestFlakeScan\|TestFlakeDeclaration\|TestFlakeLedgerOwner\|TestFlakeCatchMechanismStaysOffTheSmokePath'` | ok |

## Why `-count` and not load

PROJECT.md Rule 27: the instrument for a determinism defect is repetition of the
TEST, never pressure on the host. Nothing here generated device load; the
measurements above are sequential invocations and file reads. The one probe that
did place a row at a risky phase did so by choosing a TICK OFFSET, not by loading
the machine.
