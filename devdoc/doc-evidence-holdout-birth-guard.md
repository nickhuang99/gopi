<!-- doc-class: historical -->
# Task Report: the evidence-freshness gate's hold-out now needs a birth, not just a ratchet

## Completed

- **A birth-time guard in `internal/standards/doc_evidence_gate.go`.** The
  hold-out table `docEvidenceHeldOut` is judged against the table at its BASE
  commit: every entry the base did not carry whose named drift does not fire at
  HEAD is REFUSED, in the same test cycle that adds it. Entries the base already
  carried keep the existing stale-ratchet behaviour (a repaired drift is the
  legitimate way an entry dies).
- **The base is RESOLVED, never assumed.** `docEvidenceHoldOutBase` walks the
  gate file's OWN history (`git log --max-count=64 -- <file>`) and takes the
  newest commit that is an ANCESTOR of HEAD, skipping HEAD itself (the tree under
  test is not its own base). When HEAD is the file's only commit the base is its
  parent — the "HEAD~ at its first commit" the epic names, and the revision at
  which the file does not exist and therefore carries no entry. A commit that is
  not an ancestor (a `pre-merge:` sync landing the integration branch) is skipped
  rather than compared; a history with no comparable commit is REFUSED, not
  passed silently (Rule 15).
- **One parser, two readers.** `docEvidenceHoldOutEntries` reads a hold-out table
  out of the gate file's SOURCE (a revision's blob or the working tree's copy) for
  the historical half, and `docEvidenceHoldOutPaths` reads the compiled map for the
  released half. The authority-parity case pins that the two agree on the released
  file, so the guard cannot judge a table the gate never reads.
- **One predicate, two callers.** `docEvidenceHoldOutBirthGuard(released, base,
  fired)` is a pure function, and `fired` IS `docEvidenceReport.Used` — the same
  set `docEvidenceStaleHoldOuts` reads (which is now `docEvidenceStaleIn`, the same
  rule as a pure function). "Nothing backs it" therefore has ONE definition, and
  the fired set comes from `internal/docclass`'s own resolution.
- **The refusal is actionable**: it names the entry, HEAD, the base commit as
  `git log --oneline` spells it, and the commits that touched the table since the
  base — plus the incident it exists for.

## Not Done

- **The two stale entries were NOT re-deleted here** — that is task `01a12d1b`'s
  work (commit `225aa45ef`), and this run does not repeat it. The deletion is
  already IN this branch, measured rather than assumed: `git merge-base
  --is-ancestor 225aa45ef HEAD` → exit 0 and `git log --oneline -3 --
  internal/standards/doc_evidence_gate.go` → `d8b77253f`, `225aa45ef`,
  `8c5f4812e` (a task-`01a12d1b` hand-over merge, `acbce8e2a`, brought it in). An
  earlier draft of this bullet said the opposite ("this worktree does not carry
  it, its merge base is `6f7acc1e1c4`") — the merge base is real but the inference
  from it was wrong, and one `--is-ancestor` call refutes it; it is corrected here
  rather than left standing (Rule 41: 实测推翻推断，以实测为准).
- **`docEvidenceHoldOutSource` (the reason text a held-out finding carries) is
  unchanged**: with the table empty it is unreachable, and re-wording prose no path
  prints would be churn.

## Not Done (the claim I removed)

The first draft of this report asserted a latent trap: that `docEvidenceHoldOutSource`
carries a bare ` at ` (docclass's anchor-text separator), so a future manifest row
anchored into this file would record truncated text and never resolve. It was
written from reading the constant and then checked against the tier's own rule —
`Evidence.Check` matches the recorded text as a SUBSTRING of a normalised line, and
text cut at an ` at ` is still a PREFIX of that line, so it resolves. **The claim
was wrong and is retracted rather than shipped** (Rule 41: 推测不得充当判据). What IS
measured stays: `grep -c "internal/standards/doc_evidence_gate.go" devdoc/DOC-INDEX.tsv`
→ `0`, i.e. this file carries no manifest row of its own.

## Verification

- Build: `go build ./...` → OK (`CGO_ENABLED=0`).
- Tests: `make test-budget PKGS='./internal/standards ./internal/docclass'` →
  `packages: 2 finished, 1 with a failure … exit=1 outcome=fail`. The failure is
  PRE-EXISTING and unrelated: `TestCommandsDocGeneratedBlockMatchesSource` reports
  `COMMANDS.md`'s generated verb index still carrying `cmd/agent/cli_doccheck.go:118`
  while the source now has that anchor at `:152`, and its own red proof fails on the
  same precondition. The diff that landed it is `03f2d1dfe` (the anchor-NUMBER work)
  moving that file; THIS run's diff touches only `internal/standards/doc_evidence_gate.go`
  and its test (`git diff --name-only` → those two paths), and the effect on the
  other 2 gate package's tests was not run (Rule 27: no whole-tree run).
  `go test -count=1 ./internal/standards/ -run DocEvidence` → `ok
  github.com/nick/gopi/internal/standards  1.959s`; `./internal/docclass` fully green
  in the same budget run.
- Ran: `go test -count=1 ./internal/standards/ -run 'DocEvidence'` — the real
  surfaces (the fixture proofs, the four birth-guard proofs, and
  `TestDocEvidenceGateOnTheRealManifest`, which reads `devdoc/DOC-INDEX.tsv`
  through `docclass.Load` and resolves it against this worktree).
- Result: green. The real-manifest case logs
  `go test -count=1 ./internal/standards/ -run DocEvidenceGateOnTheRealManifest -v`
  → `devdoc/DOC-INDEX.tsv: 731 normative row(s) resolved against the tree, 0 drift
  finding(s) (0 held out: []), 0 hold-out(s) stale; birth guard vs base d8b77253f
  (+0 added, 0 unbacked)` — re-derived at commit `64619df9c` (an earlier draft of this
  line carried "728", measured before this branch's own docs landed; the figure that
  ships is the one the command above prints at the delivered commit, not the older
  one — Rule 38).
  With the guard mutated to the pre-guard world (the refusal append deleted) the
  same tree goes RED — `TestDocEvidenceHoldOutBirthGuardRefusesUnbackedAddition`
  and `...ResolvesItsBaseFromTheFileHistory` both fail — and green again when
  restored: the observed red→green the epic asks for.
- Commit: the branch tip of `task/01a12d41-c7117eecaf953fb0cc982c64` — the commit that carries this
  report (its own hash cannot be written into a file it contains).

## Evidence (self-filed)

- **The real seam, driven by hand at the delivered tip (`891cfefe5`).** The fixture
  proofs call `docEvidenceHoldOutBirthGuard` directly; this probe goes through the
  gate's OWN test cycle instead. An entry was typed into the released table
  (`var docEvidenceHeldOut = map[string]string{ "devdoc/doc-evidence-holdout-birth-guard.md": "REAL-SEAM PROBE …" }`,
  the value `sed -i`-inserted at the map's single definition site, line 79) and
  `go test -count=1 ./internal/standards/ -run TestDocEvidenceGateOnTheRealManifest`
  run against the untouched tree:

  ```
  --- FAIL: TestDocEvidenceGateOnTheRealManifest (0.35s)
      doc_evidence_gate_test.go:254: docEvidenceHeldOut names devdoc/doc-evidence-holdout-birth-guard.md, but no drift fires there any more — the hold-out must ratchet down: delete its line from internal/standards/doc_evidence_gate.go (a hold-out left behind silently stands in for a drift that no longer exists)
      doc_evidence_gate_test.go:267: docEvidenceHeldOut names devdoc/doc-evidence-holdout-birth-guard.md, but no drift fires there at HEAD (891cfefe59d7b4c00c88ccbb04195a8bb584b5f2): the entry was ADDED since d8b77253f ([]) and nothing backs it. A hold-out is BORN with the drift it names — at the commit that adds it, the gate must be able to report that drift but for the entry. Point it at a drift that fires, or leave it out and let the gate report the drift as it finds it.
      doc_evidence_gate_test.go:269: devdoc/DOC-INDEX.tsv: 731 normative row(s) resolved against the tree, 0 drift finding(s) (0 held out: [devdoc/doc-evidence-holdout-birth-guard.md]), 1 hold-out(s) stale; birth guard vs base d8b77253f (+1 added, 1 unbacked)
  FAIL
  ```

  Whole output filed at
  `~/.gopi/sessions/01a12d41-c711-7eec-af95-3fb0cc982c64/artifacts/birth-guard-real-seam.txt`.
  Two readings to take from it: (a) the birth guard fires on the real cycle and names
  the HEAD it judged, the base it resolved (`d8b77253f`) and the count (`+1 added, 1
  unbacked`); (b) the ratchet fires *alongside* it — that hand-added entry is both
  pre-existing-to-nothing and drift-less, so the two readers disagree only in the
  reason they give, never in the verdict, which is the division the epic asked for.
  `git checkout -- internal/standards/doc_evidence_gate.go` restored the file; the
  probe left `git status --porcelain` empty (measured after the restore) and the
  full `-run DocEvidence` suite green (`ok … 1.411s`, 9/9) in the tree that ships.

- `go build ./...` → `BUILD_ALL_OK` (exit 0).
- REAL SEAM (the guard at the surface it exists for, not in a unit test): a bare
  entry was added to the RELEASED table —
  `var docEvidenceHeldOut = map[string]string{"devdoc/a-drift-that-does-not-exist.md": "temp probe"}` —
  and `go test -count=1 ./internal/standards/ -run DocEvidenceGateOnTheRealManifest`
  went RED with the birth-guard line verbatim:
  `docEvidenceHeldOut names devdoc/a-drift-that-does-not-exist.md, but no drift fires
  there at HEAD (64619df9cab7b5b58e3c233365ffb21d95d66644): the entry was ADDED since
  d8b77253f ([]) and nothing backs it … (birth guard vs base d8b77253f (+1 added,
  1 unbacked))`. Note it fires ALONGSIDE the ratchet line on the same entry, which is
  the intended division: the ratchet catches a hold-out with no run behind it on the
  NEXT run, the birth guard catches it in the commit that adds it. Restored →
  `git status --porcelain` empty.
- `go test -count=1 ./internal/standards/ -run 'DocEvidence'` → `ok
  github.com/nick/gopi/internal/standards 1.959s` (green, both the fixture proofs
  and the real-manifest case).
- RE-MEASURED at the branch tip `cd9dce761` (the docs-only commits above moved HEAD
  after the runs recorded here, so the same two surfaces were executed again at the
  delivered revision rather than carried forward): `CGO_ENABLED=0 go build ./...` →
  exit 0; `go test -count=1 ./internal/standards/ -run DocEvidence -v` → 9/9
  `--- PASS` by name, `ok … 2.117s`; `TestDocEvidenceGateOnTheRealManifest` logs
  `731 normative row(s) … 0 drift finding(s) (0 held out: []), 0 hold-out(s) stale;
  birth guard vs base d8b77253f (+0 added, 0 unbacked)`. The probe was repeated at
  this revision too — a bare entry added to the RELEASED map
  (`"devdoc/__birth-guard-probe__.md"`) reds `TestDocEvidenceGateOnTheRealManifest`
  with the birth-guard line `… the entry was ADDED since d8b77253f ([]) and nothing
  backs it` and, on the same entry, `1 hold-out(s) stale` from the ratchet: the two
  readers disagreeing is the point — they catch the same entry at different times.
  Restored → `git status --porcelain` empty.
- MUTATION 1 (guard removed, the pre-guard world):
  `_ = fired[p] // MUTATION: …an added entry is never refused` →
  `--- FAIL: TestDocEvidenceHoldOutBirthGuardRefusesUnbackedAddition` ("an entry
  added with no drift firing behind it was not refused: … Unbacked:[]") and
  `--- FAIL: TestDocEvidenceHoldOutBirthGuardResolvesItsBaseFromTheFileHistory`
  ("the unbacked entry was not refused"); restoring the file → `ok … 1.536s`.
- MUTATION 2 (the gate's red path): removing `rep.New = append(rep.New, finding)`
  → `--- FAIL: TestDocEvidenceGateRedProof` ("the finding was not reported as
  NEW: … no longer carries the recorded text …") — the original red proof still
  bites.
- The two stale entries are already gone UPSTREAM (this run does not redo task
  `01a12d1b`), measured on this worktree's own history rather than asserted:
  `git log --oneline -- internal/standards/doc_evidence_gate.go` → `225aa45ef
  fix(standards): ratchet the evidence-freshness gate's hold-out table to empty`
  on top of `8c5f4812e`, and `git merge-base --is-ancestor 225aa45ef HEAD` →
  exit 0 (it is an ancestor of this branch). The real-manifest case agrees:
  `0 held out: []`.
- This run's diff touches two files only: `git diff --name-only` on the base →
  `internal/standards/doc_evidence_gate.go`,
  `internal/standards/doc_evidence_gate_test.go` (plus the report and its one
  manifest row).
- The gate's own path in the manifest: `grep -c "internal/standards/doc_evidence_gate.go"
  devdoc/DOC-INDEX.tsv` → `0` (this file carries no row of its own). No other claim
  is made from it — the retracted one is recorded under "Not Done (the claim I
  removed)".
