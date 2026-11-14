<!-- doc-class: historical -->
# The refusal that looked like a red: marking a stale-binary refusal inline

**Task:** 01a142e31-dc87-7dba-b3e1-6c875d35a08a · **Measured at:** 2026-10-16
**Related:** `devdoc/test-budget-freshness-refusal-evidence.md` (the block this
change hoists), `devdoc/test-budget-failure-bodies.md` (why a marker line alone is
not evidence), `devdoc/test-budget-run-condition.md` (the drift reading).

## The incident

Task 01a14161 ran the branch-scoped budget suite. Its timeline, from two
independent sources of the same clock:

| time | event |
|---|---|
| 11:48:30 | the run starts (the runner's own start record) |
| 11:48:34 | `fc074a8d1` is committed into the tree |
| 11:48:37 | the runner builds the tested binary (its mtime, and its stamped build identity `f9e7c9d6b8c8…`) |

The tested binary was therefore built from `f9e7c9d6b8…` while the tree already
held `fc074a8d1`. Every CLI e2e freshness guard refuses under that condition — it
will not verify code that is not in the binary it drives — and **eight** of the
run's failing tests were that refusal. **No test body ran for any of them.**

The runner's summary DID name the condition:

```
--- STALE-TESTED-BINARY REFUSALS (the failing test never ran a test body) ---
  N distinct refusal(s) in this stream — that many failing test(s) never ran a test body.
```

— but it printed that block **after** the failure list, and every refusing test
was rendered with the same per-line shape a real code failure uses:

```
--- FAIL: TestX  (github.com/nick/gopi/cmd/agent)
```

An agent reading the failing lines counted eight red tests and diagnosed code
reds/flake. That is a defect of the REPORT, not of the guard: the guard's refusal
is correct and stays.

## The fix — two halves, one predicate

1. **Inline.** `list_failure_tests` marks a refusing test **on its own marker
   line**:

   ```
   --- FAIL: TestRefusesAStaleBinary  (pkg)  [REFUSED: no test body ran — stale tested binary; see the STALE-TESTED-BINARY REFUSALS block below]
   ```

   The set of refusing tests comes from the **shared predicate**
   `gopi_stream_freshness_tests` (`scripts/lib/stream-failures.sh`) — the same
   reader the trailing block uses. The script keeps no second definition of "is
   this a refusal" (Rule 14); it holds the rows in an index keyed
   `\n<package><TAB><test>\n` (a tab is the one byte neither field can contain,
   and the newline cannot appear inside either), because a shell loop over a
   captured list cannot query the library per row without re-running the awk per
   failure.

2. **Hoisted conclusion.** `report_failures` reads
   `gopi_stream_freshness_refusal_count` **before** printing any row, and when it
   is non-zero prints the count and the sentence that stops the marker being read
   as "these are fine" **above** the list. A `grep FAIL` reader now meets the
   correction before the rows it corrects.

Both texts are named constants beside `freshness_heading` (Rule 1), and the count
is a `%s` placeholder composed by the line that prints it — the shape
`freshness_remedy_note` already uses, for the same reason: a number two writers
must agree on is how a summary comes to state a stale count.

**What deliberately did NOT change.** The refusal verdict, its signature
(`stream_freshness_signature`), the artifact line, the guard's verbatim VERDICT
relay, the remedy line and the exit code are untouched. The marker asserts nothing
about the code — it says the row is not a code verdict and points at the block
that carries the evidence. A run with no refusal prints exactly what it printed
before.

## Acceptance — and the red proof

`internal/testutil/testbudget_refusal_inline_gate_test.go`:

* `TestTestBudgetRefusalIsMarkedInlineAndItsConclusionHoisted` runs the **REAL
  script** (`scripts/test-budget.sh ./cmd/agent/`, real args, real git repo) with
  a stub `go` whose `go test` answers a fixture stream carrying ONE refusing test
  **and** one genuinely failing test, so both directions are checked on one
  fixture:
  * the refusing row carries the inline marker (read from the script, Rule 13);
  * the genuine red keeps the plain `--- FAIL: <test>  (<pkg>)` shape with **no**
    marker — the distinction must not swallow ordinary reds;
  * the hoisted lead line (with the refusal count substituted) precedes the first
    `--- FAIL:` line of the report;
  * the `STALE-TESTED-BINARY REFUSALS` block — and the guard's own sentence —
    are still printed: the fix corrects the presentation, never the verdict;
  * **RED PROOF, TWO ARMS** (the fix file is never mutated, so there is nothing
    to restore — its identity is asserted against the commit by content hash,
    `git hash-object scripts/test-budget.sh` == `git rev-parse HEAD:scripts/test-budget.sh`,
    never by an index-relative diff):
    * **constructed** — a copy of the *shipped* script with the three rendering
      constants emptied (the pre-fix rendering by construction, not a paraphrase):
      it must fail both the inline and the hoisted assertion — and still render the
      control's plain marker line, so the red arm measures the rendering and not a
      broken run. It answers *"is the distinction what is being measured"* (it holds
      everything else constant).
    * **revision-anchored** — the script read straight out of the object store at
      the revision **before** the fix (`git show <parent>:scripts/test-budget.sh`,
      the introducing commit found by `git log -S`): the shipped pre-fix bytes,
      nothing constructed. It answers *"was the distinction actually absent
      before"*. When the introducing commit cannot be resolved (an uncommitted
      development tree) the arm logs that it did not run rather than passing
      silently (Rule 15).

The stub `go` builds nothing and answers `go test` with the fixture, so the arm
is seconds long and generates no load (Rule 27): the real 25-minute suite that
produced the incident is not reproduced, it is *represented* by its two
measurable inputs (a stream with the guard's refusal signature, and a genuine
failure beside it) — the same footing
`testbudget_run_condition_gate_test.go` established.

Two honest limits:

1. The fixture's `go build` writes an empty artifact, so the run's own RUN
   CONDITION block reports the drift as **unmeasurable** in this arm. That is the
   existing block's own correct behaviour (it never reports an unreadable base as
   settled, Rule 15) and is unrelated to what this arm measures.
2. The arm asserts the SHAPE of the marked line, not its wrapping: a terminal
   that wraps the long marker line is the terminal's business, and the marker's
   leading `[REFUSED` is what a grep reader meets first.
