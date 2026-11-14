<!-- doc-class: historical -->
# doccheck's abstention names the half it measured

**Task** `01a134a1387872e1a76494c50cff2ce6` · branch `task/01a134a1387872e1a76494c50cff2ce6`
· baseline `6ef428c` · 2026-10-13.

## The defect (measured, filed as a bug row while task `01a13426` ran)

`gopi tool doccheck` decides whether this tree has a corpus at all with ONE
predicate, `docCheckHasCorpus` — the manifest **and** the repo's own classifier
(`docindex.HasGenerator`). Both halves are required by design: `tools/docclass/`
is the ONE policy source, and a check that answered without it would be a second
policy wearing the same verb.

The predicate was right. The **refusal** was not. There was ONE sentence for it:

```
doccheck: no devdoc corpus here (no devdoc/DOC-INDEX.tsv) — nothing to check
```

so a tree that HAS `devdoc/DOC-INDEX.tsv` and lacks `tools/docclass/` was told
the manifest was missing — the file sitting on the first line of the reader's own
`ls` — while the half actually absent was never named. That is Rule 15's shape: a
message that states something untrue about which file it measured, sending the
operator to the wrong place. Measured verbatim in
`artifacts/23-doccheck-manifest-present-no-generator.txt` (task `01a13426`):

```
DOC-INDEX.tsv
a.md
doccheck: no devdoc corpus here (no devdoc/DOC-INDEX.tsv) — nothing to check
EXIT=0
```

## The fix

Scope is the exit-0 abstention branch only; the predicate is untouched.1. `docCheckMissingCorpusHalf(root)` **measures** which half is absent and
   returns its repo-relative path (or `""`). Order follows the predicate: the
   manifest first, then the generator directory, then each script **one by one** —
   `HasGenerator` refuses a repo with only ONE script ("half a generator is no
   generator"), so the sentence can name the script an operator still has to add.
2. `docCheckHasCorpus` becomes `docCheckMissingCorpusHalf(root) == ""` — the SAME
   one measurement answers both the predicate and the sentence, so the verdict and
   the message cannot disagree about which tree this question applies to.
3. Two sentences, chosen by the measured path compared against
   `docindex.ManifestRel` (never a re-typed spelling):

   ```
   doccheck: no devdoc corpus here (no devdoc/DOC-INDEX.tsv) — nothing to check
   doccheck: no devdoc corpus here (no tools/docclass; the manifest devdoc/DOC-INDEX.tsv is present) — nothing to check
   ```

   The generator sentence names the absent path from `docindex`'s own constants
   (`GeneratorDirRel`, `ClassifyScriptName`, `BannerScriptName`) and states the
   manifest's presence outright — the fact the old message got backwards.
   Arguments are rendered by `docCheckAbstentionArgs`, so the sentence is a
   format string plus the measured path; the tests assert the
   `fmt.Sprintf`-rendered constant, never a hand-typed substring.

**Why not name both halves in the generator sentence** (the acceptance allows
"the classifier or both halves"): naming both scripts is not measurable for the
general case, because *which* script is absent can only be known by looking —
and "no devdoc corpus here (no tools/docclass) — the manifest … is present" is
already the specific truth. When only ONE script is missing the sentence names
that script, which is strictly more actionable.

## The drift this change nearly caused (measured, and repaired)

`cmd/agent/cli_doccheck.go` carries a **frozen anchor**:
`devdoc/mergeset-published-anchor-union.md` records
`anchor cmd/agent/cli_doccheck.go:161 at // modes, what each writes, and the exit
convention a caller gates on.`, and `gopi tool doccheck` refutes it when the line
moves. My FIRST version inserted 15 lines above `docCheckUsage`, which shifted the
anchored line 161 → 176, and the verb said so:

```
doccheck:   NOT PERMITTED (drifts, absent from the baseline): devdoc/mergeset-published-anchor-union.md#1342: anchor cmd/agent/cli_doccheck.go:161 at // modes, what each writes, and the exit convention a caller gates on. — number-drifted (recorded text is at :176)
```

Repaired the way the file's own comment prescribes and the precedent
(`devdoc/doccheck-provenance-abstention.md`, task `01a1312f` — the same trap, 34
lines, 161 → 195) — **by moving the new constants BELOW the anchored line instead
of regenerating the table** (constants need not precede their users in Go). The
old comment block was restored to its exact 4-line shape so the line lands back on
161:

```
$ grep -n "// modes, what each writes" cmd/agent/cli_doccheck.go
161:// modes, what each writes, and the exit convention a caller gates on.
```

`docCheckNoGeneratorRoot` now lives beside `docCheckProvenanceUnmeasurableRepair`,
in the abstention-vocabulary block **below** the anchor. No table was regenerated.

## Not Done (honest)

- Nothing deferred. The change is confined to `cmd/agent/cli_doccheck.go`; the
  drift arm's abstention on an absent baseline (`docCheckReportDrift`) is
  untouched, as the briefing requires.
- The two pre-existing `doccheck` anchor drifts are filed, not fixed (previous
  section): they belong to documents this branch does not own.
- The sentences remain distinct code paths, not one templated sentence; a fourth
  half would need a fourth constant. That is deliberate — each sentence is a claim
  about one measured fact.

## Verification

- **Build**: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → `BUILD OK`.
- **Tests**: `go test ./cmd/agent/ -count=1 -run 'TestDocCheck'` → `ok
  github.com/nick/gopi/cmd/agent 23.296s` (all nine doccheck tests, including the
  two new ones).
  `make test-budget PKGS='./cmd/agent'` → **`exit=0 outcome=pass`**, observed on
  branch tip `649891770` (754 s, 4351 `pass` / 0 `fail` in its JSON stream):

  ```
  stamp: 2026-10-13T19:57:39+08:00 host=hosta pkg=./cmd/agent budget=25m elapsed=754s
         load_start=4.96 3.82 2.85 load_end=2.02 2.29 2.46 cores=32
         exit=0 outcome=pass stream=run-1791892659-2028397.json
  ```

  Full transcript `artifacts/budget-cmd-agent.txt` (the run's own session
  directory, Rule 37).
- **Ran** (Rule 31, the real binary — `artifacts/real-run-abstention-cases.txt`,
  final build):

  ```
  $ ls /dev/shm/doccheck-no-generator/devdoc/
  DOC-INDEX.tsv
  a.md
  $ ./bin/gopi tool doccheck --repo /dev/shm/doccheck-no-generator
  doccheck: no devdoc corpus here (no tools/docclass; the manifest devdoc/DOC-INDEX.tsv is present) — nothing to check
  EXIT=0
  ```

  and the two control trees:

  ```
  $ ./bin/gopi tool doccheck --repo /dev/shm/doccheck-no-corpus        # neither half
  doccheck: no devdoc corpus here (no devdoc/DOC-INDEX.tsv) — nothing to check
  EXIT=0

  $ ls /dev/shm/doccheck-half-generator/tools/docclass/               # only classify.py
  classify.py
  $ ./bin/gopi tool doccheck --repo /dev/shm/doccheck-half-generator
  doccheck: no devdoc corpus here (no tools/docclass/banner.py; the manifest devdoc/DOC-INDEX.tsv is present) — nothing to check
  EXIT=0
  ```

  The same two acceptance cases, re-run on the branch TIP with a binary built
  from it (`/tmp/gopi-dc-final`, `CGO_ENABLED=0 go build -o … ./cmd/agent/`) in a
  sandbox off `/` (Rule 22: fixture root device `66305` vs `/`'s `66308`) —
  `artifacts/final-verify-runs.txt`:

  ```
  $ ls <sb>/final-verify/manifest-only/devdoc/
  DOC-INDEX.tsv
  $ /tmp/gopi-dc-final tool doccheck --repo <sb>/final-verify/manifest-only
  doccheck: no devdoc corpus here (no tools/docclass; the manifest devdoc/DOC-INDEX.tsv is present) — nothing to check
  exit=0
  $ ls -A <sb>/final-verify/neither/devdoc/          # (empty)
  $ /tmp/gopi-dc-final tool doccheck --repo <sb>/final-verify/neither
  doccheck: no devdoc corpus here (no devdoc/DOC-INDEX.tsv) — nothing to check
  exit=0
  ```

  The PRE-FIX sentence, for contrast, is the baseline source's own constant
  (`git show HEAD:cmd/agent/cli_doccheck.go`):
  `docCheckNoDocClassRoot = "doccheck: no devdoc corpus here (no %s) — nothing to
  check"`, rendered with `docindex.ManifestRel` always — which on case (1) names
  the file `ls` lists first.

- **Result**: case (1) names the classifier and states the manifest's presence —
  it no longer claims `devdoc/DOC-INDEX.tsv` is missing; case (2) still names the
  manifest; case (3) names the exact missing script. All exit 0, as an abstention
  must (nothing is WRONG with such a tree).
- **Commit**: `94776f0c9` (branch `task/01a134a1387872e1a76494c50cff2ce6`; the
  tip at hand-over is whatever `git rev-parse HEAD` says, and the invariant that
  matters is checkable: `git log --oneline --name-only 94776f0c9..HEAD` lists
  **this document only** — no code, no test — so the deliverable a reader wants is
  byte-identical at `94776f0c9` and at the tip). Named this way on purpose: an
  amended-away hash (`376a71e47`, this line's first draft) survives only in the
  reflog, so a fresh clone cannot `git show` it — a citation that expires is not a
  citation.
- **GREEN (post-restore)**: `go test ./cmd/agent/ -count=1 -run 'TestDocCheck'` →
  `ok … 23.296s`.

## Evidence (self-filed)

- **RED proof** — production mutated in place to the PRE-FIX sentence
  (`fmt.Printf(docCheckNoManifestRoot+"\n", docindex.ManifestRel)`),
  `go test ./cmd/agent/ -count=1 -run 'TestDocCheckAbstention|TestDocCheckHalfAGenerator'`
  → **FAIL**, observed verbatim:

  ```
  --- FAIL: TestDocCheckAbstentionNamesTheHalfItMeasured/manifest_present,_classifier_absent_—_the_reported_defect
      cli_doccheck_test.go:1069: the abstention does not name the half it measured
          want: "doccheck: no devdoc corpus here (no tools/docclass; the manifest devdoc/DOC-INDEX.tsv is present) — nothing to check\n"
          got:  "doccheck: no devdoc corpus here (no devdoc/DOC-INDEX.tsv) — nothing to check\n"
  --- FAIL: TestDocCheckHalfAGeneratorNamesTheMissingScript
      cli_doccheck_test.go:1099: the abstention does not name tools/docclass/banner.py — the script that IS missing …
  ```

  Full output: `artifacts/red-proof-output.txt`.
- **RESTORE, measured by content** (the criterion is an explicit revision-free
  byte comparison, not an index-relative diff):
  `test "$(sha256sum cmd/agent/cli_doccheck.go | cut -d' ' -f1)" = "$(cut -d' ' -f1 artifacts/cli_doccheck.go.fixed.sha256)"`
  → equal, `9bc71f0404d937c8bd9f69ddfd2810b361133ec6459e3fa8dbc6d0125e1e2bd7`, both
  before the mutation and after the restore.
- **`go vet ./cmd/agent/`** → clean (it caught a real arity bug in the first
  draft of the generator sentence — `%s reads arg #4, but call has 3 args` —
  which is why the sentence carries exactly two verbs).
- **Corpus (Rule 36)**: `gopi tool doccheck` run after this document landed (see
  the report's closing run); the document is classified through the write hook in
  the same commit.

## Filed while working (Rule 41) — NOT this task's subject, NOT fixed here

`gopi tool doccheck` on this branch's own baseline checkout exits 1 with TWO
unpermitted anchor-number drifts that this change did not cause and does not
touch:

```
doccheck:   NOT PERMITTED … devdoc/hp-push-topology-audit.md#997: anchor cmd/agent/cli_tasks_worker.go:638 at serveGitSyncSweep(workerRoleLabel) — number-drifted (recorded text is at :650)
doccheck:   NOT PERMITTED … devdoc/scratch-gc-flight-join.md#1699: anchor cmd/agent/cli_worker_control.go:2167 at func (c workerDaemonCtx) installFileWatcher() (uninstall func()) { — number-drifted (recorded text is at :2183)
```

Measured at the baseline commit itself — `git show HEAD:cmd/agent/cli_tasks_worker.go`
puts `serveGitSyncSweep(workerRoleLabel)` at **650** and
`git show HEAD:cmd/agent/cli_worker_control.go` puts `installFileWatcher` at
**2183** (the documents record 638 / 2167), and neither key is in the frozen
table (`grep` → no match). Neither file is in this branch's diff
(`git diff --name-only HEAD` → the two `cmd/agent` files + `devdoc/DOC-INDEX.tsv`).
Filed as bug row **`01a13512-2d06-7f2d-b81a-3b06f88fec4f`** with the full evidence
(`artifacts/bug-doccheck-exits-1-on-clean-master.md`); regenerating a frozen
census is a reviewed decision, not a reporter's edit.

**Consequence for this task's acceptance**: the "`gopi tool doccheck` reports the
corpus consistent" clause is satisfied on its **classification half** — the verb
prints `doccheck: 0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv`, including
this document's row — and the anchor-permit half is red for the two pre-existing
reasons above, unchanged by this branch.

## Files

- `cmd/agent/cli_doccheck.go` — the abstention branch, its two sentences, the
  measurement, and the predicate delegating to it.
- `cmd/agent/cli_doccheck_test.go` —
  `TestDocCheckAbstentionNamesTheHalfItMeasured` (four sub-cases, EXACT sentence
  equality, plus "must not claim a present file is missing") and
  `TestDocCheckHalfAGeneratorNamesTheMissingScript`.
- `devdoc/DOC-INDEX.tsv` — this document's row (Rule 36).
