<!-- doc-class: normative -->
# Task Report: the red-proof RESTORE criterion is index-relative — hardened, and machine-checked

Task `01a1306e-10c6-7051-91d2-413c1cc4601d` · branch `task/01a1306e10c6705191d2413c1cc4601d`

## Completed

- **The recipe, corrected and written down** (this document, §2): a red proof's
  restore is now measured with `git diff HEAD -- <path>` (or the index-free blob
  comparison `git hash-object` vs `git rev-parse HEAD:<path>`), never with an
  index-relative diff.
- **A correction the epic itself needed.** The epic proposed `git diff <file>` as
  the hardened spelling, described as "the short form of `git diff HEAD --
  <file>`". **Measured in this run: it is not.** `git diff <path>` with no
  revision is worktree-vs-INDEX — byte-for-byte the comparison the epic set out
  to replace. The live proof (§3, step 4) caught the wrong spelling before it
  shipped, and `redproofDiffIsIndexRelative` now reports it as a hit rather than
  exempting it.
- **A machine check**: `internal/standards/redproof_restore_gate.go` — a standard
  gate (registered in `gates()`, so `go test ./internal/standards/` and the whole
  `go test ./...` pick it up) that reports the fragile PAIR: an index-seeded
  restore (`git checkout -- <path>`, `git restore <path>`, `git checkout .`) in
  the same unit as an index-relative diff criterion that follows it. It names the
  `file:line` of the criterion, carries the correction in the message, has an
  empty baseline (nothing to permit), and has an inline escape
  (`red-proof-restore-ok:`) for a document that must quote the fragile form in
  order to refuse it.
- **Three halves plus a live proof** in `redproof_restore_gate_test.go`: the red
  proof (fragile pair reported, twelve corrected spellings NOT reported), the
  read floor (files and units) and the real-tree run, and the demonstration that
  runs the actual recipe against a real git index.
- **No new dependency** (Rule 35): the check is a `go test` gate in the suite
  every change already passes, plus one command in prose. No script, no
  scheduler, no resident process, no config key. The one plumbing change is a new
  `gate` field (`textCorpus`) so a text gate can declare WHICH corpus it owns in
  one place — reusing the existing `textFilesUnder` walker and `fenceOpeningRun`
  vocabulary rather than growing a second traversal (Rules 8/13).

## Files

New:
- `internal/standards/redproof_restore_gate.go` — the gate: `redproofRestoreGate()`
  (registered in `gates()`), the unit splitter, and the two predicates
  (`redproofRestoreSeedsFromIndex`, `redproofDiffIsIndexRelative`) with their
  named constants.
- `internal/standards/redproof_restore_gate_test.go` — red proof, corpus+floor,
  real-tree green, and the LIVE fixture (`TestGateRedProofRestore`).
- `devdoc/redproof-restore-criterion.md` — this report, which doubles as the
  corrected recipe (§2).

Changed:
- `internal/standards/gate.go` — one new nullable `gate.textCorpus` field, honoured
  in `scanText` (`internal/standards/gate_test.go` registers the new gate).
- `GOPI.md` — Rule 5's closing paragraph: the restore criterion is stated as
  index-independent.
- `devdoc/rules-reference.md` — the Tier B twin of that paragraph.
- `devdoc/standards-gate.md` — the gate index gains the new row.
- `devdoc/DOC-INDEX.tsv` — ONE row (`redproof-restore-criterion.md`, historical).

Not touched, on purpose: the 127 `git diff --stat` restore claims living in
`devdoc/` and `tasks/` records (`## Not Done (honest)` explains why the gate's
corpus excludes them).

## The measured failure (the reason the gate exists)

Reproduced in this run on a real git index (`TestGateRedProofRestoreLiveFailureAndHardenedCriterion`,
plus the standalone shell run in §3): a file that has been `git add`-ed turns the
recipe's restore into a no-op that **exits 0**, while the fragile criterion still
prints nothing:

| step | command | observed |
|---|---|---|
| plant + stage | `printf …MUTATION… > prod.go; git add prod.go` | index blob = worktree blob, ≠ `HEAD:prod.go` |
| "restore" | `git checkout -- prod.go` | **exit 0**, file still contains `MUTATION` (grep count 1) |
| criterion | `git diff --stat prod.go` | **empty**, exit 0 — reads "restored" |
| hardened | `git diff HEAD -- prod.go` | full diff of the surviving mutation (exit 0) |
| hardened | `git hash-object prod.go` vs `git rev-parse HEAD:prod.go` | `0f516f98…` vs `a53cd175…` — different |

The whole failure is silent by construction: `git checkout -- <path>` copies the
INDEX into the worktree, and `git diff` compares the worktree against the INDEX,
so the two agree with each other while both disagree with `HEAD`.

## Not Done (honest)

- **The 127 devdoc/tasks records that carry a `git diff --stat` restore claim are
  NOT rewritten, and the gate does not scan them — on purpose.** `devdoc/` and
  `tasks/` are RECORDS: a report must stay able to quote the criterion it
  actually used, including a fragile one (the conflict-marker gate's rationale,
  same reasoning). The gate's corpus is the INSTRUCTION surface — `GOPI.md`,
  `devdoc/rules-reference.md`, and the `.go`/`.md` files under `internal/` and
  `cmd/`. Two of the ten scanned-shape carriers in records were additionally
  measured to be **false positives** under a naive predicate (a `git diff` line
  about scope, not about a restore; a whitespace check with `-w`), which is one
  more reason the gate does not police them.
- **One class of fragile spelling is knowingly NOT reported**: `git checkout
  <path>` with no `--` separator. A pure text predicate cannot tell it from
  `git checkout <branch>` (a branch switch), and a gate that fires on every
  branch switch would be routed around within a day. The fragile recipe in this
  tree always spells the path form with its separator. This is a false NEGATIVE,
  and it is the direction this gate deliberately errs.
- The four `.go` sites the gate found while being written (the gate's own header
  comment, twice, and the test's header comment) were resolved with the gate's
  **inline escape** rather than a baseline — a quoted fragile pair is not a
  permit, and the escape is visible in the data report.

## Verification

```
Build:  go build ./... / go vet ./internal/standards/     -> OK (exit 0)
Tests:  go test ./internal/standards/ -run TestGateRedProofRestore -count=1
        -> ok github.com/nick/gopi/internal/standards  6.462s (4 sub-tests PASS)
        make test-budget PKGS='./internal/standards' -> FAILED, 2 tests, BOTH
        PRE-EXISTING and unrelated to this diff (see the note below the block)
Ran:    the gate's own red proof + corpus/floor + real-tree + LIVE fixture, listed below,
        and the shell reproduction of the failure mode on a real index (§3)
Result: the fragile pair is reported at the criterion's line; the twelve corrected
        spellings are silent; the live fixture shows the no-op restore and the hardened
        criterion catching the surviving mutation; the real tree is green over 2517 files
Commit: 482ba8ed7 (the gate + the recipe) and 9ec5c546a (PROJECT.md Rule 5, rules-reference,
        the gate index), on branch task/01a1306e10c6705191d2413c1cc4601d
```

**The two failing tests in the scoped suite are NOT this diff, and here is the evidence.**
They are `TestCommandsDocGeneratedBlockMatchesSource` and
`TestCommandsDocGateRedProofStaleGeneratedBlock`, both about the generated verb index in
`COMMANDS.md`. This branch touches none of that test's inputs — `git diff --name-only
master HEAD` lists only `GOPI.md`, `devdoc/DOC-INDEX.tsv`, `devdoc/redproof-restore-criterion.md`,
`devdoc/rules-reference.md`, `devdoc/standards-gate.md` and the four
`internal/standards/*.go` files; nothing under `cmd/agent/`, no `COMMANDS.md`, no
`cliverbs`. The disagreement is between TWO committed artifacts and reproduces from
master's own contents (`git show master:COMMANDS.md` records
`cmd/agent/cli_scratch_gc.go:572`; master's `cli_scratch_gc.go` has `CliVerbScratchGC` at
`:615`, moved there by `1ab2e2743`, which is ON master and which regenerated the block in
the same commit). Filed as a bug row, with the raw commands and output:
`01a130ae-343e-7667-b17c-f43df8f59349`
(`~/.gopi/sessions/01a1306e-…/artifacts/brief-commands-doc-census-disagreement.md`).

**Two things the run got wrong and then fixed, both recorded because each is a rule.**

1. **`doccheck --fix` rewrote the whole `devdoc/DOC-INDEX.tsv`** — 712 insertions / 711
   deletions for a ONE-ROW addition (that is a whole-file edit; PROJECT.md Rule 4 is the
   surgical-edit rule). It also perturbs every BARE anchor — the ones a devdoc writes
   without a path — which is what makes the churn expensive rather than merely noisy. The
   file was restored from the commit before it and the single row inserted in sorted
   position: `git diff --stat devdoc/DOC-INDEX.tsv` → `1 file changed, 1 insertion(+)`,
   and `gopi tool doccheck` reports the corpus consistent.
2. **The report devdoc was classified, then the classifier's OUTPUT was amended into the
   right shape** — the first attempt (during the write above) timed out at 30s while the
   bytes landed, which is exactly the hazard PROJECT.md Rule 36 names; the row was written in
   the same commit as the file, never left for the next run.

### Evidence (self-filed)

- `go test ./internal/standards/ -run TestGateRedProofRestore -count=1` → `ok
  github.com/nick/gopi/internal/standards 6.462s` (exit 0), four sub-tests
  PASS: `RedProof`, `CorpusAndFloor`, `RealTreeIsGreen`, `LiveFailureAndHardenedCriterion`.
- Real-tree run with its own figure: `red-proof restore criterion: 2517 file(s)
  scanned, 0 hit(s)` — a read floor of 300 files / 500 units is asserted
  alongside, so "green" cannot be a quiet empty walk.
- Red-before transition observed while the gate was being written: with the
  predicate's `git diff <path>` exemption still in place, the live test failed
  with ``the hardened criterion `git diff prod.go` printed nothing (exit 0) while
  the mutation is live`` — the correction above came from that failure, not from
  reading git's docs.
- `git status --porcelain` in this worktree → empty, after both commits.
- Shell reproduction (§3) — the raw transcript, `dev of / = 66307`, `dev of TMPDIR = 66309`
  (the fixture is off the `/` partition by DEVICE ID, Rule 22, never by spelling).
- **Environment, recorded so the next reader does not chase it**: `/home` was **100 % full**
  during this run (`/dev/nvme0n1p7 458G 434G 94M 100%`; `go env GOCACHE` =
  `/home/user/.cache/go-build` at **134G**). One scoped test run failed to LINK
  (`link: mapping output file failed: no space left on device`) and one
  `git commit --amend` failed with `unable to write loose object file`. Both were disk
  space, neither was a code defect; the fixture directories this run created under
  `${TMPDIR}` were removed afterwards.

## 1. Where the fragile shape lives (measured over the real corpora, before writing the gate)

- `grep -rln "diff --stat"` → 127 files under `devdoc/` + `tasks/`; 219 lines.
- An instruction-surface scan (`GOPI.md`, `devdoc/rules-reference.md`, every
  `.go`/`.md` under `internal/` and `cmd/`): **0** fragile pairs — the fragile
  form is a RECORD habit, not (yet) an instruction. That is exactly why the gate
  lands green: it is a ratchet against the instruction surface acquiring the
  habit, not a clean-up of history.
- The two carrier files whose four candidate pairs survive the record scan
  (`devdoc/testutil-triple-green-20260916.md`,
  `devdoc/tasklist-rearm-flake-controlled-clock.md`) both pair `git checkout --`
  with an index-relative `git diff`, i.e. they are real instances of the shape —
  they stay as they are, as records.

## 2. THE RECIPE (corrected) — what a red proof must write from now on

```
# 1. plant the mutation
#    (edit the production line the test guards)
# 2. run the test and OBSERVE the failure, quoting the assertion message
go test ./<pkg>/ -run <TestThatGuardsIt> -count=1
# 3. restore — seeded from a REVISION, so index state cannot make it a no-op
git checkout HEAD -- <file>          # NOT `git checkout -- <file>` when <file> may be staged
git reset -q <file>                  # drop a staged plant, if there was one
# 4. VERIFY the restore with an INDEX-INDEPENDENT criterion (one of these)
git diff HEAD -- <file>              # worktree vs the committed blob  (spelling A)
test "$(git hash-object <file>)" = "$(git rev-parse HEAD:<file>)"   # index-free (spelling B)
# 5. re-run the test — green again, on the restored tree
go test ./<pkg>/ -run <TestThatGuardsIt> -count=1
```

**What is NOT a criterion** (each of these prints empty for a mutation that
survives in an added file):

- `git diff --stat <file>` — worktree vs **index**;
- `git diff <file>`, `git diff`, `git diff --numstat`, `git diff --name-only` with
  no revision — the same comparison, just a different renderer;
- `git status --porcelain` alone — blind to a staged plant as well.

`git checkout -- <file>` is not wrong in itself — it is wrong as the restore for
a file the run may have **staged**. Spelling A/B is what makes the claim
checkable; the restore is what makes it true.

## 3. The live failure, raw (real index, throwaway repo off the `/` partition)

```
dev of / : 66307
dev of TMPDIR: 66309
=== A) mutate then git add then 'restore' + fragile criterion ===
-- hash before restore attempt: 0f516f989afeef80e0e2a6f4209f45f82c703358
-- exit of checkout: 0
-- file still mutated? 1
-- fragile: git diff --stat prod.go:
(empty)
-- fragile exit=0 (empty above = 'restored' claim)
-- hardened: git diff HEAD -- prod.go:
diff --git a/prod.go b/prod.go
index a53cd17..0f516f9 100644
@@ -1 +1 @@
-func F() int { return 1 }
+func F() int { return 2 } // MUTATION
-- hash criterion: file=0f516f989afeef80e0e2a6f4209f45f82c703358 HEAD=a53cd175dcbd2882e918cf1da5d76183a2f4dcfc
```

`git checkout -- prod.go` **exited 0** and moved nothing (the index held the
mutation), the fragile criterion printed nothing, and both hardened criteria saw
the surviving mutation. The same sequence is asserted end-to-end inside
`TestGateRedProofRestoreLiveFailureAndHardenedCriterion`, which additionally
proves the honest restore (`git checkout HEAD -- prod.go` after `git reset -q`)
turns every criterion empty and the blobs equal.

## 4. The gate

- **Wrong**: a unit (fenced block, or blank-line-separated paragraph) containing a
  restore that seeds the worktree from the **index** — `git checkout -- <path>`,
  `git restore <path>`, `git checkout .` — followed by a diff whose **only**
  sidedness comes from the index (`git diff`, `git diff --stat`, … with no
  revision). The hit names the criterion's line.
- **Right, and proven silent**: `git diff HEAD -- <file>`, `git diff --stat HEAD
  -- <file>`, `git diff --stat <rev> <rev>`, `git checkout <rev> -- <file>`,
  `git restore --source=<rev> <file>`, `git diff --cached/--staged/--no-index`,
  `hash-object` vs `rev-parse`, a restore with no criterion, and an
  index-relative diff with no restore claim (a scope check).
- **Corpus**: `GOPI.md`, `devdoc/rules-reference.md`, and `.go`/`.md` under
  `internal/` + `cmd/` — the surfaces gopi renders into a briefing. `devdoc/` and
  `tasks/` are records and are out.
- **Floors**: ≥300 files and ≥500 units, asserted on the real tree — the walk and
  the unit splitter are both load-bearing, and a broken one must not read green.
- **Baseline**: empty by construction; a fragile criterion is never a state to
  permit.
