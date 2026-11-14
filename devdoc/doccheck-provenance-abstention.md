<!-- doc-class: normative -->
# Task Report: `doccheck`'s provenance abstention gets its own name, and its exit status joins the census

Task `01a133b2-f6aa-78b4-a0a1-94d6fef89465` · branch `task/01a133b2f6aa78b4a0a194d6fef89465`

## Completed

- **The abstention has a name of its own.** `cmd/agent/cli_doccheck.go` no longer
  prints `docCheckDriftClaimFalse` (`CLAIM NOT TRUE`) when the provenance probe
  cannot run. The new constant `docCheckDriftProvenanceNotMeasured` reports
  `PROVENANCE NOT MEASURED … the permit's claim is neither confirmed nor refuted,
  because the probe could not run in this tree`. The false-claim sentence is now
  reached only by a claim the probe actually refuted, or by an unreadable reason.
- **The two faces' exit strategies are one policy.** `NOT MEASURED` fails the
  verb (`docCheckReportProvenance` became `docCheckAuditProvenance`, returning
  `(claimFalse, notMeasured)`, and the arm returns 1), matching the census
  generator, which has always propagated the probe's error as exit 1 through the
  same judge (`docclass.AnchorNumberProvenanceCheck`).
- **The closing verdict names the right class.** `docCheckReportDrift` now returns
  the failing CLASS (a named constant) instead of a boolean, so the closing line
  cannot call an abstention "anchor NUMBER drift": the abstention has its own
  class `docCheckProvenanceNotMeasuredVerdictClass`, and its own repair direction
  `docCheckProvenanceUnmeasurableRepair` (regenerating the table runs the SAME
  probe on the SAME tree, so "regenerate it" is advice that cannot work there).
- **The "same verdict" assertions in `internal/docclass` are now true and say
  why** — `anchor_number_baseline.go`'s header and `anchor_number_provenance.go`'s
  producer comment both state that the shared judge covers the third answer too
  (unmeasurable = failure for producer, census and verb alike), and record the
  asymmetry this task closed so it is not restored.
- **Regression test (red → green).**
  `TestDocCheckUnmeasurableProvenanceIsNamedAndNotCalledFalse` pins all three
  facts on the measured shape, reading the exit code off the verb itself.
- **The provenance fixtures no longer depend on the ambient repository.**
  `docCheckPermitsFixture` gives its tree its own repository and `git fetch`es the
  project's branches into it, so the freeze commit resolves INSIDE the fixture and
  the write is confined to the fixture's own gitdir. The claim it renders is
  `first-frozen at <freeze>` through docclass's own producer
  (`AnchorNumberBaselineReasonFor … , false`) — the arm that is TRUE of the fixture
  by construction, because its invented key is absent from the freeze population.
  The previous `pre-existing` reason was refuted by the probe, so the old fixtures
  were passing only because their claims were unmeasurable; under the new
  exit strategy they were reported as abstentions until this was fixed.

## Not Done

- **A `--no-checkout` shared clone was tried and abandoned, with the failure
  recorded.** `git clone --shared --no-checkout` exits 129 with no output under the
  suite's environment (both with the ambient env and with this file's hermetic
  `docCheckGitEnv`), so the fixture uses `init` + `fetch <path>` instead. A shared
  clone would also have been the wrong shape here: in a LINKED worktree `.git` is a
  small file pointing at the main repository's gitdir, so a clone of it — and any
  `checkout` run inside such a copy — resolves into the developer's REAL repository.
  Measured while building this change: an earlier version of this fixture (a
  `cp -a .git` copy) rewrote this run's own worktree (a stray `.ref` appeared beside
  the sources); the `init` + `fetch` form leaves `git status --porcelain` empty.
- **`--fix` is not covered by a new test.** It borrows `docCheckReportDrift`, so it
  inherits the corrected exit strategy by construction, and
  `TestDocCheckFixAndReportAgreeOnTheSameCorpus` still passes — but no test drives
  `--fix` on an unmeasurable tree specifically.
- **The abstention still depends on the history looking the way the judge assumes.**
  `AnchorNumberProvenanceCheck` resolves `AnchorNumberBaselineFreezeCommit`
  unconditionally (its `claimed` slice is empty only when no row carries a readable
  claim), so a permit carrying a valid claim is unmeasurable in any tree that does
  not carry gopi's own history. That is what makes the archived-tree verdict
  correct, and it is not changed here — but a corpus checked in a *shallow clone
  that has unrelated trunks* would report `NOT MEASURED` for a question a full
  clone could answer. Naming that would need the judge to distinguish "no base
  supplied" from "base named and absent", which is a change to the judge, not to
  this face. Not attempted.
- **`--fix` is not covered by a new test.** It borrows `docCheckReportDrift`, so it
  inherits the corrected exit strategy by construction, and
  `TestDocCheckFixAndReportAgreeOnTheSameCorpus` still passes — but no test drives
  `--fix` on an unmeasurable tree specifically.
- The `prose-anchor` / `prose-path-line` families carry the same shape of arm; they
  were not audited for the same asymmetry.
- **A pre-existing, unrelated failure was FOUND and FILED, not fixed** (Rule 41):
  `go test ./internal/tools/ -run TestAssertionTotalityAuditGate -count=1` fails on
  the base commit with a self-contradicting output — an assertion reported
  `UNCLASSIFIED` while the same run prints allow-list `why:` rationales for it.
  `git diff --stat b888461f2 HEAD -- internal/tools internal/standards` is empty, so
  it is not this diff's. Filed as task `01a13461-ec9a-7c5c-be32-7e0fd40a7e1f`
  (tags `bug`); evidence:
  `<ev>/artifacts/preexisting-assertion-totality-failure.md`.

## Verification

- Build: `go build ./...` — OK; `CGO_ENABLED=0 go build -o <ev>/gopi-after-doccheck ./cmd/agent/` — OK.
- Tests:
  - `go test ./cmd/agent/ -run 'DocCheck' -count=1` — `ok  github.com/nick/gopi/cmd/agent 7.235s` (all DocCheck arms, including the new one).
  - `go test ./internal/docclass/ ./internal/docindex/ -count=1` — `ok internal/docclass 6.979s`, `ok internal/docindex 1.093s`.
  - `make test-budget PKGS='./cmd/agent ./internal/docclass ./internal/docindex'` (the packages this diff touches) — `exit=0 outcome=pass elapsed=738s stream=run-1791879786-152145.json`, all three packages green (`ok github.com/nick/gopi/cmd/agent 735.390s`).
  - `go test ./internal/standards/ -run TestCommandsDocGeneratedBlockMatchesSource -count=1` — failed on the stale generated line reference, `ok` after regenerating (below).
- Ran: **(acceptance tree — `git archive HEAD | tar -x`, no `.git`)**

  ```
  $ gopi-after-doccheck tool doccheck --repo <ev>/evidence/tree-archived
  doccheck: anchor NUMBER coherence (recorded text still present, line number stale):
  doccheck:   PROVENANCE NOT MEASURED (NOT checkable here — …): provenance not checkable here — provenance probe: resolve 0b3c1de29: …
  doccheck: the permits' claims could not be judged in THIS tree — its history is not readable here …; the same tree refuses the census internal/docclass/testdata/anchor-number-census.go for the same reason
  doccheck: 0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv
  doccheck: this run exits 1 on provenance NOT MEASURED (no verdict about the permits was reached) …
  $ echo $?
  1

  $ go run internal/docclass/testdata/anchor-number-census.go <tree> <tree>/internal/docclass/testdata/anchor-number-baseline.tsv --check
  anchor-number-census: provenance probe: provenance probe: resolve 0b3c1de29: git rev-parse … exit status 128 (fatal: not a git repository)
  exit status 1
  $ echo $?
  1
  ```
  `grep -c 'CLAIM NOT TRUE'` over the doccheck output: **0** (was 1 before the fix).
- Ran: **(the normal worktree — this repo, with `.git`)**

  ```
  $ gopi-after-doccheck tool doccheck
  doccheck:   163 of 163 are frozen in internal/docclass/testdata/anchor-number-baseline.tsv (…)
  doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row
  $ echo $?
  0

  $ go run internal/docclass/testdata/anchor-number-census.go . internal/docclass/testdata/anchor-number-baseline.tsv --check
  anchor-number-census: table matches the tree — the freeze is exact
  $ echo $?
  0
  ```
- Result: on the archived tree the two faces agree (both exit 1) and the abstention
  is named; on the normal worktree both stay exit 0; every exit code was taken with
  `cmd > file 2>&1; echo $?` (never through a pipe).
- Ran: **`COMMANDS.md` is a GENERATED artifact and the diff moved a line it cites.**
  `go test ./internal/standards/ -run TestCommandsDocGeneratedBlockMatchesSource`
  failed with `got: … cmd/agent/cli_doccheck.go:194` / `want: … :234` (the verb
  index records the line its table is produced at). Regenerated with the command the
  gate itself prints — never hand-edited:
  `go run internal/standards/testdata/commands-census.go . COMMANDS.md` →
  `commands-census: wrote COMMANDS.md (44 verbs)`, `git diff --stat COMMANDS.md` =
  1 file, 1 insertion, 1 deletion; the gate then passed (`ok internal/standards`).
- Commit: the fix and this report share the branch's commit chain
  (`git log --oneline -3` on `task/01a133b2f6aa78b4a0a194d6fef89465`); a report cannot
  name its own commit's hash, since any edit to it rewrites that hash.

## Evidence (self-filed)

- `git status --porcelain` in the worktree, before commit: ` M cmd/agent/cli_doccheck.go`, ` M cmd/agent/cli_doccheck_test.go`, ` M internal/docclass/anchor_number_baseline.go`, ` M internal/docclass/anchor_number_provenance.go` (+ this report). After commit: empty.
- **Pre-fix measurement (the defect, reproduced):** `gopi-before-doccheck tool doccheck --repo <archived tree>` printed
  `doccheck:   CLAIM NOT TRUE (regenerate the table): provenance not checkable here — …` and **exited 0**; the census on the same tree exited **1**. Files: `<ev>/evidence/before-doccheck.txt` (`exit=0`), `<ev>/evidence/before-census.txt` (`exit=1`).
- **RED proof and restore.** I mutated `docCheckAuditProvenance`'s error arm back to the pre-fix behaviour (print `docCheckDriftClaimFalse`, return `notMeasured=false`), then ran the new test: it FAILED with
  `the abstention is not NAMED …` and printed the pre-fix output — `CLAIM NOT TRUE …` followed by `doccheck: the devdoc corpus is consistent` (the false-certification shape). Restore measured with an explicit revision-independent hash, not an index-relative diff:
  `sha256sum cmd/agent/cli_doccheck.go` = `sha256sum <ev>/evidence/cli_doccheck.go.green` = `2d50a5d0e2b960e442c25a059d48ea49b824f323966067e67acd8fb3fa4a981f`; after restore `go test ./cmd/agent/ -run 'DocCheck' -count=1` → `ok`. **An earlier restore attempt of mine silently failed (`cp` reported "cannot stat", and a hash comparison between two empty strings printed "RESTORE VERIFIED") — the mutation was still in the file and only the non-vacuous re-run caught it. Recorded because it is the exact trap `devdoc/redproof-restore-criterion.md` names.**
- **Anchor-line discipline (measured, not assumed):** my first version of this change inserted 34 lines above `docCheckUsage` in `cmd/agent/cli_doccheck.go`, which moved the frozen anchor `devdoc/mergeset-published-anchor-union.md:1333 → anchor cmd/agent/cli_doccheck.go:161 at // modes, what each writes…` down to line 195. Both faces went red for that reason alone (`NOT PERMITTED … number-drifted`; census `1 unpermitted`). I did **not** regenerate the table: the new constants were moved below the anchored line (constants need not precede their users in Go) and the comments compressed so the anchored line is back at 161, verified by `grep -n "// modes, what each writes" cmd/agent/cli_doccheck.go` → `161`. Evidence: `<ev>/evidence/worktree-census.txt` (red run) → green after the move.
- Evidence directory: `/home/user/.gopi/sessions/01a133b2-f6aa-78b4-a0a1-94d6fef89465/evidence/`
  (`before-doccheck.txt`, `before-census.txt`, `after-doccheck.txt`, `after-census.txt`,
  `worktree-doccheck.txt`, `worktree-census.txt`, `gopi-before-doccheck`, `gopi-after-doccheck`,
  `tree-archived/`, `cli_doccheck.go.green`).
- The frozen table `internal/docclass/testdata/anchor-number-baseline.tsv` was **not modified**
  (`git status --porcelain` never listed it), and neither were the DOC-INDEX data rows.
