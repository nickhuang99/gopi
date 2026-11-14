<!-- doc-class: normative -->
# The failures block could not name its own failures — and the run it hid had 12 E2E tests that never executed

> **Provenance.** `TestDocAnchorsResolveInNormativeDocs` was retired on 2026-10-22 in `5a35f61` (deleted with `internal/docclass`), so a `file.go:NNN` reference in this page is a landing-time record, not an anchor a live gate enforces — nothing resolves one today. The line numbers stay useful to a reader; they are not checked by the suite. Citations below are quoted as they were written — where one of them reads as present tense ("is RED", "resolves", "fails"), read it as the state of THAT run, never as a gate this tree still has.

Task: `01a1329b-8902-7b7a-837d-f908ae3b7433` (branch `task/01a1329b89027b7a837df908ae3b7433`).

## 1. The premise, and what the artifact says

The briefing asserted that `scripts/test-budget.sh` reported **four `--- FAIL` rows with no
failure body**, forcing a control-worktree dig to rule out a regression. **The run's own
stream refutes the first half, and makes the second half worse than described:** the bodies
were there the whole time, and the run actually holds **15 failing tests**, not four.

```bash
S=/home/user/workspace/gopi-test-scratch/queue/test-budget/run-1791849386-333342.json
python3 - "$S" <<'EOF'
import json,sys
rows=[json.loads(l) for l in open(sys.argv[1],errors='replace') if l.startswith('{')]
print("records:", len(rows))
for i,r in enumerate(rows):
    if r.get('Action')!='fail': continue
    body=[rows[j]['Output'] for j in range(i-1,-1,-1)
          if rows[j].get('Action')=='output' and rows[j].get('Test')==r.get('Test')]
    print(r.get('Package','').split('/')[-1], r.get('Test'), "elapsed=%s"%r.get('Elapsed'),
          "bodylines=%d"%len(body))
EOF
```

```
records: 27399
cmd/agent      TestBareRunKeepsAllItsArtifactsInOneSessionDir  elapsed=0.01  bodylines=3
internal/agent TestDocAnchorsResolveInNormativeDocs            elapsed=5.15  bodylines=220
internal/agent TestPlainPrompt_FirstRequestCarriesNoTurnStamp  elapsed=0.02  bodylines=3
internal/agent TestRepairRepoMovesARowOntoALiveWorkTree        elapsed=0.44  bodylines=4
… (15 failing tests in total)
```

So the honest statement of the symptom is **not** "the failures had no body" — it is **"the
failures block prints no body"**. That distinction is the whole of this document.

## 2. Why the block printed markers only

`report_failures` drew its input from `gopi_stream_failures`, which is by construction a
`package<TAB>test` **list**:

```bash
list_failure_tests() {
    gopi_stream_failures "$raw_json" |
        awk -F"$stream_col_sep" -v n="$slowest_report_n" 'NR <= n { printf "--- FAIL: %s  (%s)\n", $2, $1 }'
}
```

Nothing in the library returned a test's *output rows* at all: `gopi_stream_failure_events`
**discarded** every `Action=="output"` row carrying a `Test` field

```bash
if (action == pout) {
    if (fstr($0, ktest) != "") next      # ← the test's whole log, dropped here
    print kind_pout sep pkg_of($0) sep unesc(fstr($0, kout))
}
```

and `Action=="fail"` carries no body to fall back on: **all 17 fail events in this stream
read `Output=None`** (measured, not inferred). A failure's reason exists in exactly one
place — the test's own output rows — and the runner read the one structure that excludes
them. The rows the briefing read as body-less were therefore a **reporting** defect, not a
missing measurement.

## 3. What the fix does

`gopi_stream_failure_events` now normalises a test's own `output` rows as a distinct kind
(`test-output`, shape `package<TAB>test<TAB>test<TAB>line`) instead of dropping them, and
`gopi_stream_failure_bodies STREAM PACKAGE TEST` reads ONE test's log back — **attributed by
full import path and test name**, so `p1/tools` can never be handed `p2/tools`'s assertion
text (the defect `devdoc/test-budget-package-attribution.md` records for build output, with
a worse consequence once the text is evidence). `list_failure_tests` prints each marker with
that test's own rows under it, and prints a **distinct sentence** when the stream carries
none:

```
--- failures (from this run's own events: Action=="fail"; the budget did NOT trip) ---
  failing tests: 15
  --- FAIL: TestBareRunKeepsAllItsArtifactsInOneSessionDir  (github.com/the operator/gopi/cmd/agent)
      === RUN   TestBareRunKeepsAllItsArtifactsInOneSessionDir
          bare_session_one_dir_e2e_test.go:78: …-tested-bin/gopi (mtime 2026-10-13T07:56:32+08:00)
          is OLDER than …/internal/agent/clock.go (mtime 2026-10-13T07:56:33+08:00) — …
      --- FAIL: TestBareRunKeepsAllItsArtifactsInOneSessionDir (0.01s)
  --- FAIL: TestNoOutputRowsAtAll  (…)
      (NO output rows in this stream for this test: the 'Action":"fail' event carries no body of its own, so this verdict is recorded with its evidence absent — the full stream below is the only place left to look)
  package verdict: FAIL: github.com/the operator/gopi/cmd/agent
  package verdict: FAIL: github.com/the operator/gopi/internal/agent
full stream: /home/user/workspace/gopi-test-scratch/queue/test-budget/run-1791849386-333342.json
```

Two arms, kept apart on purpose: **"the body printed" and "the stream carries none" must not
share a rendering** (Rule 15) — an empty list and an absent measurement read alike. The
package-verdict lines are the other half: the test list is capped at `slowest_report_n`
(15), the package set is not, so a reader relying on the block is never reading a prefix of
it.

## 4. What those bodies turned out to say (the reason this mattered)

Reading them back, the failing rows are not one defect but four sources — which is exactly
what a marker-only report was hiding.

**Twelve of the fifteen** are `cmd/agent` E2E tests that failed in `0.01 s` with the SAME
sentence, because `scripts/test-budget.sh` pre-builds a tested binary once and exports
`GOPI_TEST_BIN`, and `assertGopiBinIsFresh`
(`cmd/agent/gopibin_identity_guard_test.go:132`, 14 call sites) refuses to verify code
absent from it. The guard is correct; the artifact it
refused is the runner's own. The set is stable because the trigger is the mtime comparison,
not the tests' contents.

**And the gap is not always a build race** — which is a defect of its own, filed separately
as bug `01a132e2-6ad8-76c6-a6dd-b26d8149b022`. The same file is reported with **two
different mtimes inside the one run**: `internal/agent/clock.go` at `07:56:33` for the test
at stream index 242, and at `07:58:50` — **2 m 18 s into the run**, after the runner's build
step — for the tests at indexes 25490 and 26076. The tested binary's mtime is confirmed on
disk as `07:56:32`. So a tracked source file was rewritten *while the suite was executing*,
and the consequence is that 12 real E2E tests executed nothing in that pass. The remaining
three failing rows are a commit-order-dependent fixture
(`TestRepairRepoMovesARowOntoALiveWorkTree`), pre-existing doc-anchor drift that is also red
on clean master (`TestDocAnchorsResolveInNormativeDocs`), and one flake that passes in
isolation on the same HEAD (`TestPlainPrompt_FirstRequestCarriesNoTurnStamp`).

## 5. Traps measured while doing this

- **An apostrophe inside an awk program that the shell single-quotes closes the quote.**
  Measured twice (`syntax error near unexpected token '('`, then `awk: (END OF FILE)`) before
  the cause was obvious: in this file the awk program is a single-quoted shell string, so
  `else's` in an awk comment breaks the parse — and **`bash -n` cannot see it** (the shell is
  happy; awk is not). Rephrase the comment; never escape.
- **`… | head -n` inside the report prints `write error: Broken pipe` into the report.**
  Measured as noise between two failure blocks. Take the slice with `awk 'NR <= n'` on a
  here-string instead.
- **`gopi_stream_failure_count` silently returned `0` on a real failing stream** before this
  change, because the library did not parse at all under the system awk. A helper that fails
  to load is a helper that answers zero.
- **A gate that searches the whole report is satisfied by its own assertion text.** The body
  arm is therefore asserted with an index comparison (body must sit AFTER the marker), and
  the red proof was taken against the real script — not against a re-implementation in Go.

## 6. Verification

- Gate: `go test ./internal/testutil/ -run TestBudget -count=1` → `ok` (85 s, after restore).
- Red proof, three mutations of the real script, each caught at its own assertion:
  body reader removed · package-verdict line dropped · absent-body sentence blanked.
- Restore verified by content hash (`sha256sum -c`), not by an index-relative diff (Rule 5).
- Branch-scoped budget: `make test-budget PKGS='./internal/testutil'`.
- End-to-end replay of the real code path against the run's real stream, without running a
  suite: a stub `go` hands the script the stream, and the block is read as-is.
