<!-- doc-class: normative -->
# The second half of Rule 5's restore criterion: the content-identity reading must be anchored to a REVISION

Task `01a1386d-141c-7518-8fcb-d553cc51d028` · branch
`task/01a1386d141c75188fcbd553cc51d028` · base `9d97a2df5`

## What this closes

Rule 5's red-proof RESTORE rule landed in two halves. The FIRST half —
**an index-relative diff is not evidence** — was landed and machine-checked on
2026-10-12 (`devdoc/redproof-restore-criterion.md`,
`internal/standards/redproof_restore_gate.go`, arms (a) and (b): the fragile
PAIR of an index-seeded restore with an index-relative criterion). This document
is the SECOND half, commissioned as its own task:

> **A content-identity reading anchored to a saved copy of this loop cannot see a
> write made after it was taken** — and the reading does not say so.

The two halves are one criterion seen from two sides. Arm (a)/(b) ask *where the
criterion reads from* (the index, which the restore itself writes). Arm (c) asks
*which two things it compares*. A reading can be index-independent and still
worthless, because both sides of it are artifacts the same loop produced.

## The measured defect (task `01a138317d847ac3806c8a701eb652f3`)

The run's artifact `05-restore-identity.txt` said, verbatim:

```
internal/standards/generated_doc_deps_test.go hash=732aeebc… saved=732aeebc… same=YES
```

Both sides are the same loop's own artifacts: a snapshot file the run wrote
(`*.fixed`) and that snapshot's digest recorded beside it. **Neither side is a
revision.** So the reading is true only about the moment it was taken:

| time | event | blob of `generated_doc_deps_test.go` |
|---|---|---|
| 11:12:03 | the snapshot (`fix.patch`, `*.fixed`) was taken; the reading above was recorded | `732aeebc…` |
| 11:12:0x–11:12:38 | the revert/restore cycle, `same=YES` printed | `732aeebc…` |
| **11:13:12** | **the file was written AGAIN (arm (a)'s fix)** | — |
| 11:15:05 | the commit landed | **`30a26e580…`** |

The reading stayed `same=YES` across all of it. The delivered blob `30a26e580…`
appears in the session's `artifacts/` directory **only inside the run's own
forensic write-ups** (`10-…txt`, `bug-….md`) — files ABOUT the case, not
evidence produced BY the delivered content. So: **the delivered blob has no
content-identity proof belonging to itself.**

A revision-anchored reading cannot have this failure mode, and the reason is
mechanical rather than stylistic: `git rev-parse HEAD:<path>` names a blob that
a later write cannot change. When the file moves, the same command returns a
different answer and the reading **turns red by itself**. A copy, by contrast,
is not a reference to anything — it is another artifact, and the pair can be
re-read forever without either side noticing.

### The timeliness rule that follows (Rule 5, added 2026-10-14)

> **A content-identity reading is valid only for the state it was taken against.
> If the file has been written since, the reading must be RE-TAKEN** — even for
> a `gofmt`, a comment, or one more arm of the test. A reading carried across a
> later write is a claim about a state that no longer exists, and it says so
> nowhere.

This is the prose half. The machine-checkable half is arm (c) below.

## Arm (c): the machine check

`internal/standards/redproof_restore_gate.go` gains a third arm inside the same
gate (no new tool, no new gate registration beyond the one gate, no new config
key — Rule 3/35):

- **Reported**: a unit that performs a **content-identity reading while naming
  NO revision anywhere in that unit**. Two shapes count as a reading:
  - a line carrying a content-digest command (`git hash-object`, `sha256sum`,
    `sha1sum`, `sha512sum`, `sha224sum`, `sha384sum`, `md5sum`, `b2sum`,
    `cksum`, `shasum`) or `cmp` **together with** an equality/inequality
    operator (`=`, `==`, `!=`, `-eq`, `-ne`) — the shell/Go identity
    comparison;
  - a line spelling a verdict as a key/value pair (`same=YES`, `match=false`) in
    a unit that carries **at least two content digests** — the recorded-pair
    shape the measured defect used, which has no command and no operator at all.
- **Exempt (silent)**: any unit naming a revision anywhere — `rev-parse`,
  `cat-file`, `ls-tree`, a bare `HEAD`, a named branch, a colon-qualified
  revision path (`HEAD:file.go`, `0123456:internal/x.go`), or a bare digest that
  is not the value of a `key=` pair. Plus every shape that merely *mentions* a
  digest without comparing one.
- **Escape**: `red-proof-identity-ok:`, a marker of arm (c)'s own (the two arms
  have different subjects, so a document quoting one must not thereby silence
  the other).

Two design decisions worth recording, both measured while writing it:

1. **The digest-command and the operator must sit on the SAME LINE.** A
   unit-scoped test was written first and reported 30 sites across the real tree
   — among them `internal/tools/archive_reason.go:485` (`git hash-object -w
   --stdin` in one line, an `if err != nil` two lines below) and five Go tests
   that write a blob through `hash-object -w --stdin`. Those are not identity
   readings; the line scope removed every one of them and no genuine hit.
2. **A bare digest is a revision unless it is a VALUE.** The measured defect's
   own line carries two 40-hex tokens, both to the right of an `=`. Without that
   carve-out the arm would be silent on the one shape it was built for — the
   regression test for this is the first positive fixture, which is the measured
   line itself.

**Direction of error, stated because it is deliberate**: an unanchored identity
reading is a WARNING SHAPE, not a proven defect — the two sides may be a genuine
pair of revisions spelled in a way the predicate does not classify. The arm
therefore errs toward silence (a bare word is read as a revision) and its message
names the correction rather than asserting the defect.

## The LIVE demonstration (`TestGateRedProofRestoreIdentityLive`)

The arm's claim — *a revision anchor is what makes the reading self-invalidating*
— is measured against a real git repository, not asserted:

1. A file is committed; a snapshot copy (`prod.go.saved`) is written beside it; a
   copy-anchored reading is taken (`file=<blob> saved=<blob> same=YES`) and the
   gate confirms it IS the fragile shape.
2. The file is written AGAIN (`redproofFixtureSwapped`).
3. `git hash-object prod.go.saved` still equals the recorded digest → the
   reading **still says `same=YES`**, while a fresh read of the file returns a
   different digest. The blindness is asserted (and `t.Logf`'d), not described.
4. `git rev-parse HEAD:prod.go` still names the pre-write blob while
   `git hash-object prod.go` no longer does → the revision-anchored reading of
   the SAME file **turns red by itself**.

Step 4 is the whole point of the arm: the difference between the two readings is
not the command, it is whether one side names something a later write cannot
change.

## Files

New:
- `devdoc/redproof-restore-content-identity.md` — this document.

Changed:
- `internal/standards/redproof_restore_gate.go` — arm (c): `redproofIdentityEscape`,
  the named digest-command table, `redproofIdentityCmdRe` / `EqRe` / `ValuedRe` /
  `RevRe` / `HexRe`, `redproofIdentityVerdicts`, `redproofIdentityUnanchored`,
  `redproofIdentityFind`, `redproofIdentityHit` (arms (a)+(b)+(c) merged into the
  gate's one `find`), the gate id and the baseline reason naming both escapes.
- `internal/standards/redproof_restore_gate_test.go` — three new tests:
  `…IdentityRedProof` (four positives including the measured defect verbatim; the
  line-number assertion; eight negatives; the arm's own escape),
  `…IdentityLive` (the demonstration above), `…IdentityRealTreeIsGreen` (its own
  read figure + a PRESENCE assertion that GOPI.md still carries
  `git rev-parse HEAD:`).
- `GOPI.md` — Rule 5's closing paragraph gains the timeliness requirement; the
  first half (index-relative) is untouched.
- `devdoc/standards-gate.md` — the addendum names the second arm.
- `devdoc/DOC-INDEX.tsv` — ONE row for this document (written by the tool, never
  by hand — Rule 36).

## Verification

```
Build:  go build ./internal/standards/          -> OK
Tests:  go test ./internal/standards/ -run RedProofRestore -count=1  -> ok (47.5s)
        go test ./internal/standards/ -count=1                        -> ok
Ran:    see the run report ($GOPI_TASK_REPORT_PATH) — the arm's red proof, the
        twelve-plus correct spellings, the LIVE git fixture, and the real-tree scan
Result: 0 hits on 2668 instruction-surface files; the LIVE fixture prints the
        blind copy-anchored pair and the flipped revision-anchored pair
```

## Not Done (honest)

- **The corpus is the instruction surface, not `devdoc/` or `tasks/`.** Records
  must stay able to quote a fragile reading they actually used — the same
  decision arm (a) made, for the same reason. The 127 `devdoc/`+`tasks/` restore
  claims quoted in `devdoc/redproof-restore-criterion.md` are untouched by this
  arm too.
- **The arm is a WARNING SHAPE detector.** A copy-anchored reading whose two
  sides happen to be spelled as revisions the predicate does not classify is not
  reported (deliberate false-negative direction). Recorded here so the next
  reader does not read the green as coverage it is not.
- **The original run's prose is not rewritten.** This document records what was
  measured; the other run's statements stay as they are (Rule 5: the framework
  records a claimed result, it never rewrites a run's prose).
