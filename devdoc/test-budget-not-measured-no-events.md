<!-- doc-class: historical -->
# "No structured event" is not "zero tests ran" — test-budget's sixth verdict

**Task** 01a130b2-7eaa-7471-b349-09c4549fe1e3 · **Landing** `scripts/test-budget.sh` +
`internal/testutil/testbudget_noevents_gate_test.go` · 2026-10-12

## The defect, measured

On this host `/home` reached 100% used (2.9G free) and a suite run through the
declared entry point came back with a **0-byte stream**:
`/home/user/workspace/gopi-test-scratch/queue/test-budget/run-1791817933-3481851.json`
(`-r--r--r--`, 0 bytes — the artifact of the pre-fix report
`artifacts/disk-exhaustion-evidence.log`). The report a reader got was:

```
executed: 0 test(s) ran in this run (counted from the stream)
…
stamp: … exit=1 outcome=fail
```

Three separate misreadings live in those two lines, and each is a Rule 15
failure (a failure must NAME itself):

1. **`executed: 0`** reads as "the suite ran and had nothing to do" — the green
   shape of a package with no test files.
2. **`outcome=fail`** reads as "a test failed" — go exited 1 because it could not
   write its own output, which is not an assertion and is not a verdict about the
   tree.
3. **the cause appeared nowhere.** The only trace pre-fix was a stderr NOTE saying
   the *count* could not be read from the missing stream — it named what the
   script could not do, never what actually happened. A reader of the summary,
   of the stamp, or of the failures block got no hint of the full partition.

## The distinction the fix draws

`go test -json` writes exactly **one JSON object per line** and nothing else, so
one line beginning with `{` is the whole test of *"did the toolchain get as far as
the test framework"*. The new verdict keys on that, never on the exit status:

| shape | what it means | verdict |
|---|---|---|
| stream missing / 0 bytes | go wrote no report at all | **6**, `outcome=not-measured` |
| stream non-empty, no line starts with `{` | go spoke; the framework never started | **6**, `outcome=not-measured` |
| ≥1 structured event, zero tests executed, a filter in force | the filter matched nothing | **5** (unchanged), `outcome=no-test-executed` |
| ≥1 structured event, else | the ordinary readings | **0 / 1 / 3** (unchanged) |

**Precedence is stated in the script, not implied.** The not-measured verdict is
decided FIRST: 5 is a statement about a suite that RAN, and with no structured
event there was no suite — so a filtered run over an event-less stream reports 6,
which is the honest code (measured: `-run '^TestNeverEverExists$' ./internal/smokegate/`
under the stub `go` → `exit=6`).

The other four codes keep their meaning exactly: `0` green, `1` assertion/compile
failure, `2` usage/setup, `3` budget trip, `4` `--scope-check` out of scope.

## What the new block prints

`report_not_measured` (in `scripts/test-budget.sh`) prints, in this order:

- the banner `NOT MEASURED, GO PRODUCED NO TEST EVENT`;
- **which shape** was seen, from the evidence (`the stream is 0 bytes` vs `the
  stream holds 93 byte(s) of TEXT but ZERO structured events`);
- the stream's byte and event counts, and the raw `go` exit status with the
  sentence saying why it cannot be read as a verdict;
- **go's OWN words, verbatim** — the head of the stream (600 bytes, NUL-stripped).
  Go's diagnostics are unstructured by design (`build-output` events carry no
  `Package`, and a run that died before the framework emits no event at all), so
  the text is the only place the cause exists. The 0-byte arm says so honestly
  rather than manufacturing a quote;
- **the two filesystem figures** — the artifact directory's own filesystem and
  `/`, each with its device id (Rule 22 is judged by device id, never by a
  spelling) and its free space in bytes (a human's `df -h` rounding is what hides
  a 2G remainder). Both, because on this host they are different partitions: `/`
  sat at 25% with 131G free while `/home` — worktrees, build cache, TMPDIR and
  the artifact queue — was at 100%. A figure for `/` alone next to an ENOSPC
  death reads as a contradiction.

The `executed:` line is still printed (it is part of the record) but **never
alone**: the event-less case adds the sentence `go produced NO structured event at
all` plus a one-line echo of go's words on the following lines, and the failures
block is replaced by `NOT A FAILURE EITHER: no measurement was taken` — a run that
measured nothing must not be presented through the block whose heading is
`failures`.

## Why it is not a new dependency (Rule 35)

Nothing resident was added: no daemon, no cron entry, no state file. The change is
confined to the script the merge/verify path already runs, reusing what was
already there — the per-run stream, the `stamp:` line, the exit-code table. Rule
27 shaped the verification instead of the design: **no disk was filled to
demonstrate the verdict** (that would be manufacturing load); the two shapes were
produced with a stub `go` that answers the tested-binary build with success and
the test run with nothing (or with one line of text), which is byte-for-byte what
the exhausted partition produced.

## Not done (honest)

- **`internal/tools`'s pre-merge classifier does not know exit 6 yet.** It reads
  the entry point's output for the abstention needle `not evidence about this
  tree`, which this fix deliberately does NOT add to the new block (that needle
  makes a run an *abstention because the caller knowingly ran an unmeasurable
  tree*, and inventing it here would make an independent cause reportable as an
  input condition). Wiring the seam properly means citing *this* verdict's own
  constant — `internal/smoke` (or `internal/smokegate`) owning it, the way
  `smokegate.ScopedTarget` is shared — plus a gate arm. **Filed as a separate
  task rather than half-done here** (Rule 26), named in this report's Not Done.
- The pre-fix `NOTE: … stream is missing or empty` stderr line was replaced, not
  kept beside the new block — the block says strictly more (it names the shape,
  the counts and the exit status), and two notices for one cause is the drift
  Rule 13 exists to prevent.

## Verification

See `tasks/<this-task>-report.md` (Rule 5/31): the exact invocations, the four
red-proof mutations with their restores, and the real runs.
