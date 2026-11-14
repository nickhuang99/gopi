<!-- doc-class: historical -->
# Task Report: the worker-ownership matcher stops reading a slash-joined verb PAIR as a launcher

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Epic: slash-pair false positive in the worker-ownership pointer gate (task
`01a0aa27`). Branch `fix/worker-ownership-slash-pair`, worktree
`~/workspace/gopi-workerown-slash`, based on master `37a11c17`.

## Completed

- **Root cause (5-why, measured — not guessed).** `MentionsLaunchShape` in
  `internal/workerownership` was a bare `strings.Contains` over `Needles`. The
  corpus writes an abbreviation to name TWO group verbs at once
  (`` `gopi task run/worker`, `gopi merger` ``), and that pair CONTAINS the
  needle `gopi task run` as a substring. A probe over the real doc confirmed the
  matched needle is `gopi task run` (`MATCH "gopi task run"`), i.e. the author
  never wrote a launcher — the needle was inside the abbreviation.
- **The fix is in the matcher, one rule, in the leaf package**
  (`internal/workerownership/workerownership.go`): a needle occurrence whose
  immediately following byte is the exported separator `LaunchShapeSeparator`
  (`/`) is an ABBREVIATION, not a command, and does not count. EVERY occurrence
  is examined (`MentionsLaunchShape` loops), so a document that abbreviates once
  and writes the shape out later is still a teacher.
- **No document was annotated, none dismissed.** `devdoc/tmpdir-param-not-banner.md`
  and `internal/testutil/workerOwnershipDismissed` are byte-untouched by this
  branch (`git diff --stat` names only the matcher; the dismiss table is
  unchanged). The false positive was removed where it was born.
- **Minimal repro, and it fails on pre-fix source.**
  `internal/workerownership/slash_pair_repro_test.go` (8 cases: 4 pair shapes
  taken from real documents, 4 written-out shapes that must keep matching) plus
  `internal/testutil/worker_ownership_slash_pair_test.go` (the REAL tree: the
  TMPDIR doc must not be an offender, must carry no pointer, the whole-tree
  decision must be empty, and the same sentence with both verbs written out must
  still be reported).
- **Sibling coordination.** `feat/worker-run-entry` (the epic that retires the
  `task run` spelling) was checked: NOT merged into master, but it does touch the
  same needle set — and it solves the same file by ANNOTATING it (its tip
  `99d729f5` adds the pointer line to `devdoc/tmpdir-param-not-banner.md`). This
  branch solves it in the matcher instead. The two are compatible in either
  merge order (see §Merge coexistence below), and this branch keeps the retired
  spelling in the needle set because the audited corpus is full of historical
  `gopi task run` transcripts.

## Root cause, in one line

The matcher answered "does this text contain a launch shape?" with
`strings.Contains`; the corpus's `A/B` abbreviation embeds a whole shape by
accident, so a sentence about two verbs sharing an exit path was read as a
launch instruction. The bytes were correct — the predicate was wrong.

## The discriminating proof (why the green is not vacuous)

A fix that made the predicate match less could produce green while blinding the
gate. Three layers say otherwise:

1. **Red before, green after, on the same file.** Pre-fix (matcher reverted,
   repro in place):
   `TestMentionsLaunchShapeIgnoresSlashJoinedPairs` FAILS on 4 sub-cases —
   `run/worker`, `worker/merger`, `run/serve`, `gopi-worker-up.sh/…`. Post-fix:
   all 8 sub-cases pass, INCLUDING the 4 written-out shapes (`gopi worker`,
   `gopi task run`, the launcher name, and an abbreviation followed by a
   written-out shape).
2. **The needle still fires on the written-out form of the very same sentence.**
   `TestWorkerOwnershipSlashPairDocIsNotAnOffender` rewrites the TMPDIR doc's
   `run/worker` to `run and worker` and requires `MentionsLaunchShape` to report
   it — so the file is green because of the abbreviation rule, not because the
   matcher went blind.
3. **Measured delta over the whole audited corpus — exactly ONE file changes.**
   Comparing the pre-fix predicate (bare `strings.Contains`) against the fixed
   predicate over every audited file, in the same run:

   ```
   DELTA devdoc/tmpdir-param-not-banner.md: old=true new=false
   scanned=1319 deltas=1
   ```

   1319 audited files scanned; the only verdict that changes is the false
   positive. 223 other files still trigger, every anchored location still
   carries its pointer, the dismiss table is untouched.

   (`devdoc/verify-01a0a72c-merger-stop-immediate.md`, which carries
   `gopi worker/merger`, was untracked when this epic was briefed and was later
   committed by another task WITH a pointer; after the fix its pair no longer
   triggers, so its pointer is now a genuine redundancy, not a requirement.
   `TestWorkerOwnershipDismissTableIsHonest` skips anchored files, so it stays
   green in either case.)

## Merge coexistence with `feat/worker-run-entry`

That branch (tip `99d729f5`) annotates `devdoc/tmpdir-param-not-banner.md`
instead of fixing the matcher. Both orders are safe and produce the same final
state (clean gate, doc annotated by it):

- **This branch first:** the doc still carries no pointer; their later merge
  adds it as an ordinary content change (no conflict — this branch does not touch
  the file).
- **Theirs first:** the doc carries the pointer. This branch's matcher then
  reports nothing for it, and `TestWorkerOwnershipDismissTableIsHonest` is
  unaffected (the file is not in the dismiss table and is not an anchor). Their
  branch also widens the needle comments to describe `task run` as retired,
  which this branch leaves intact.

The two changes are complementary: theirs stops NEW documents from regrowing the
problem, this one stops the abbreviation from being misread at all.

## Files

- `internal/workerownership/workerownership.go` — the matcher rule
  (`LaunchShapeSeparator`, `abbreviatedWithSlash`, occurrence loop).
- `internal/workerownership/slash_pair_repro_test.go` — minimal repro, compiles
  pre-fix on purpose (names no post-fix symbol).
- `internal/workerownership/workerownership_test.go` — production-side pin: the
  exported separator equals the repro's local value, and `abbreviatedWithSlash`'s
  three boundary cases.
- `internal/testutil/worker_ownership_slash_pair_test.go` — the real-tree
  regression + the written-out red-proof.
- `devdoc/worker-ownership-slash-pair-20260917.md` — this report.

## Not Done (honest)

- **A pre-existing, unrelated red in the same package** (`internal/testutil`,
  NOT caused by and not fixable in this branch):
  `TestEveryTempUsingSuiteIsPinned` fails because `internal/lineedit` creates
  scratch dirs with no `TestMain` pin. MEASURED on untouched master
  (`~/work/GoPi`, 37a11c17 tip) with the identical failure text — so the fix
  belongs to whoever owns that snippet the gate names, never to this epic. The
  worker-ownership gate tests are unaffected (the run above shows them all PASS).
- The matcher stays an OCCURRENCE rule, not a grammar. `gopi worker/` at end of
  line (a real shape followed by a slash) reads as an abbreviation; measured, no
  file in the audited corpus has that shape, and the two gate tests plus the
  planted-offender red-proof stay green. Tightening it further (e.g. requiring a
  known right-hand verb) would need its own needle vocabulary and is not part of
  this defect.
- The gate does not verify that a pointer statement is TRUE (a doc could carry
  the pointer untruthfully). That is the dismissal/annotation review's job, and
  this task deliberately did not touch it.
- `TestGate2EnvRestoreShapeRedProof`-style gates were not re-run tree-wide; the
  packages that consume the matcher were (`internal/workerownership`,
  `internal/testutil`, `internal/tools`, `cmd/agent`).

## Verification

```
go build ./...                                  OK
go test ./internal/workerownership/ -count=1    ok
go test ./cmd/agent/ -run 'Ownership|WriteToolOwnership|Peak' -count=1
                                               ok  (4.2s — the writer E2E)
go test ./internal/tools/ -run 'WorkerOwnership|Write|Edit' -count=1
                                               ok  (19.6s)
go test ./internal/testutil/ -run TestWorkerOwnership -count=1
                                               ALL PASS (12 tests incl. both gate tests)
PRE-FIX repro (matcher reverted): 4 sub-cases FAIL, written-out cases pass
POST-FIX repro:                    all 8 sub-cases pass
corpus delta: scanned=1319 deltas=1 (devdoc/tmpdir-param-not-banner.md only)
```

Full-package `go test ./internal/testutil/` also ran: the ONLY failure is the
pre-existing `TestEveryTempUsingSuiteIsPinned` red reproduced on untouched master
above. Full `go test ./internal/tools/` (whole package) exceeded Go's 10m package
budget on this host — the ownership-relevant subset is green and the slow package
is a known, separately-owned budget item, not a verdict on this branch.

Commits on `fix/worker-ownership-slash-pair`:

```
33a26173 fix(workerownership): a slash-joined verb PAIR is an abbreviation, not a launcher
630ae7ee test(workerownership): minimal repro for the slash-pair shape
8006d3da test(workerownership): pin LaunchShapeSeparator and the left-hand test
8fbb070e test(testutil): the real TMPDIR doc is not an offender, and the written-out form still is
40a67a20 docs(worker-ownership): report the slash-pair matcher fix with its evidence
```

