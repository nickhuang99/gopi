<!-- doc-class: normative -->
# The RUN CONDITION block: a mid-run CONTENT change is one condition, not N failures

**Task** 01a13c43-14bb-7eda-9c8c-a58a3b546397 · base `cbdaca0` · 2026-10-14/15

## The defect, measured

`make test-budget` builds the tested binary **once**, before the suite starts, and
hands that one artifact to every e2e guard in `cmd/agent` (through `GOPI_TEST_BIN`).
A production source changed **while the suite runs** therefore makes every guard
refuse for the rest of the run.

Two halves were broken, and they had to be repaired together:

1. **The classifier keyed on a retired wording.** `scripts/lib/stream-failures.sh`
   recognised a refusal by `is OLDER than` + the noun `BUILT` — the **mtime** arm's
   sentence. Master's guard decides by **CONTENT** (`decideBinFreshness` →
   `productionSourcesChangedSince`), and on master the content arm's message carried
   **no marker at all**. So the arm that a mid-run content change actually fires was
   invisible to the runner: the report printed N bare `--- FAIL:` rows and named
   none of them as a condition. The failures block and the block that corrects it
   are only one click apart — the reader counts `--- FAIL:` and stops.

2. **`scripts/test-budget.sh` had no RUN CONDITION block at all.** The peer
   branch's union-merge half (`a82abf1ef`/`8f0e3bcca`) is not an ancestor of HEAD,
   so that reading never landed here.

## What was built

### 1. The refusal signature is a property of EVERY arm (Go side)

`cmd/agent/gopibin_identity_guard_test.go` gains `freshnessRefusalSignature` — the
ONE sentence (`this test drives the BUILT binary, so it would verify code that is
not in that binary.`) composed from the existing `cwdProbeFreshnessMarker`, printed
by **all four** refusal arms: build identity, content, and the two mtime arms. The
criterion line every arm prints is opened by the new `freshnessCriterionPrefix`
(`deciding criterion: `), so the runner can **relay** which criterion decided
instead of re-deriving one (Rule 14).

**Rule 31 stays armed.** The content arm still refuses a changed source; nothing was
weakened and **no mtime comparison was re-introduced** into the deciding path. The
mtime walk survives only as the guard's last resort, unchanged.

### 2. The classifier reads the guard's CURRENT signature (shell side)

`scripts/lib/stream-failures.sh`:

- `stream_freshness_signature` / `stream_freshness_criterion_prefix` — the two
  composed lines, named once each (Rule 1/14).
- `gopi_stream_freshness_rows` now keys on the **signature**, and each row is
  `package<TAB>test<TAB>artifact<TAB>text`: it names the **test** that refused and
  the **artifact** it refused — the artifact read out of the refusal sentence's own
  first word (after the test log's `file.go:NN: ` prefix), so it is the value the
  guard printed rather than a second reading of the stream.
- `gopi_stream_freshness_tests` returns `package<TAB>test<TAB>artifact`;
  `gopi_stream_freshness_artifacts` the distinct artifacts;
  `gopi_stream_freshness_criteria` relays the criterion line.

The **passing test that merely quotes a refusal** is still not counted: the row must
also belong to a test whose own event failed.

### 3. The RUN CONDITION block (runner side)

`scripts/test-budget.sh` prints, inside the failure accounting only:

```
--- RUN CONDITION (this run's refusals, against the tested binary's drift from this tree) ---
  drift base: the tested binary's build identity — the revision the RUNNER stamped
  into it (internal/buildidentity), compared by CONTENT …, never by mtime. …
  <N> production source(s) in this run's scope differ from the revision the tested
  binary was built from — so the binary the run drove does not contain this tree:
       differ M internal/agent/clock.go
  this run's refusals, by test (each names the artifact it would not verify):
       --- REFUSED: TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild  (github.com/nick/gopi/cmd/agent)
          would not verify: <run>-tested-bin/gopi
```

Design decisions that are load-bearing:

- **The drift base is the artifact's OWN build identity**, read back out of the
  file (`go version -m`, the operand the linker recorded) — never the value the
  runner *intended* to stamp, and never an mtime. Master retired mtime deliberately
  (an mtime cannot tell a byte-identical rewrite from a real edit), so a summary
  that drifted on mtime would be a SECOND, weaker criterion for a question the guard
  answers by content.
- **Both readings must be present.** The block prints only when the run has
  refusals **and** the comparison was taken. Refusals with no drift are the other
  arm's subject; drift with no refusals is not a condition.
- **An unmeasurable base is reported, never resolved in the artifact's favour**
  (Rule 15): a requested package the binary does not compile, an unresolvable
  scope, or a failing git each print an `unmeasurable` row; a git that answers
  nothing is NOT "settled".
- **Exit status untouched.** The block is text inside the existing failure branch.
- **Rule 35: no new dependency.** It rides the existing runner — no cron, no unit,
  no state file, no new config key.

The heading was corrected during the run to name the **comparison**, not an outcome:
a settled tree prints the block too (it says the refusals are *not* this condition),
and a heading asserting drift would contradict its own detail line.

## Two defects the real run exposed (and that are now fixed + gated)

The first end-to-end exercise (run `1792016857-1697937`) printed the block, the
drift row and the refused test — and also:

```
sed: -e expression #1, char 25: unknown option to `s'
scripts/test-budget.sh: line 1084: rev_reason: unbound variable
```

- The revision reader located the operand with a `sed` substitution; the operand
  carries the module's import path, whose `/` characters **end the `s/…/…/`
  expression early**. It now finds the operand by **string index** and takes the
  hex characters that follow.
- The reason was assigned **inside a command substitution** — a subshell — so it
  never reached the caller, and `set -u` aborted the report: the failure surface
  itself failed. The reader is now a **global-setting function** whose caller reads
  `$rev_resolved` / `$rev_reason`.

Both are the same shape: a diagnostic that fails is worse than no diagnostic.

## Gates

- `cmd/agent/gopibin_identity_refusal_signature_gate_test.go` —
  `TestEveryRefusalArmCarriesTheSignature` scans every `Refusal: fmt.Sprintf(`
  window in the guard and fails if **any** arm lost the signature or the criterion
  prefix (with a floor of 3 arms, so a scan that finds nothing cannot pass);
  `TestFreshnessSignatureAndCriterionPrefixSpellingsAgree` compares **both** composed
  lines against the shell spellings, file to file, in **both directions**.
- `internal/testutil/testbudget_run_condition_gate_test.go` — the block through the
  **real script**: a stub `go` (real toolchain forwarded for `go list`) that builds
  a real stamped artifact and answers with a synthetic stream carrying the content
  arm's refusal. The **drift** arm stamps a real ancestor 50 commits back (its
  production sources demonstrably differ) and asserts the block states the drift and
  names the refusing test; the **settled** arm stamps HEAD and asserts the block
  states the comparison came out settled with **no** differ row.
- The synthetic-stream arms for the classifier live in the pre-existing
  `internal/testutil/testbudget_freshness_gate_test.go`.

Rule 27 is untouched: no load generation, no synthetic pressure, no heavy test; the
fixture is a hello-world module.
