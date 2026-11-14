<!-- doc-class: historical -->
# The anchor-number arms at the migration's terminal state (task 01a13be6)

Three arms of `internal/docclass` were red at base `51b5739` — not because anything
drifted, but because each took its SUBJECT from the shipped freeze table's permits
and that table had just legitimately ratcheted to **zero rows**.

## 1. What the terminal state is, and why it is not a defect

`tools/docclass/classify.py` renders the anchor cell NUMBER-LESS
(`anchor <path> at <text>`); a stored `anchor <path>:<line> at <text>` is legacy
data whose terminal state is zero numbered rows. The freeze table censused exactly
that legacy incoherence, so it reaches zero rows as a MEASUREMENT
(`AnchorNumberMigrationReport`), never as a silence: commit `fabcf3c96` is the
regeneration that removed the last 141 permits, and the same scan reports
`291 anchor row(s) scanned, 0 incoherent, 0 of them still record a line number`.

An empty table is an empty SET, and an arm whose subject is that set has no
subject. That is what the three reds were:

| arm | its subject was | at the terminal state |
|---|---|---|
| `TestAnchorNumberCensusShippedTableIsTheWritersFixpoint` | the live incoherent rows, floor `100` | `live = 0` → red on BOTH tables |
| `TestAnchorNumberCensusShippedFaceFiresOnAStaleTable` | "delete one permit from the shipped table" | no row to delete → the green arm stands unwatched |
| `TestAnchorNumberNumberChangeOverTheRealFrozenPopulation` | the permits, indexed `shifted[0]` | `index out of range [0] with length 0` — a panic |

Measured red, verbatim, before any change (evidence file
`red-at-base-51b5739.txt`):

```
--- FAIL: TestAnchorNumberCensusShippedTableIsTheWritersFixpoint
    the detector measured 0 incoherent row(s) (read floor 100) — it is reading the wrong tree
--- FAIL: TestAnchorNumberCensusShippedFaceFiresOnAStaleTable
    the shipped table carries no data row to delete — this arm has no subject
--- FAIL: TestAnchorNumberNumberChangeOverTheRealFrozenPopulation
panic: runtime error: index out of range [0] with length 0
```

## 2. The decisions (each arm, explicitly re-decided)

### 2.1 The floor moves to the population the migration does NOT move

`anchorNumberReadFloor = 100` stood on the **incoherent** population. Its own
comment already said what a floor must not do ("a threshold such a change trips is
measuring the criterion rather than the detector") — and the migration is exactly
such a change. It is replaced by two floors that name their population:

* `anchorNumberCorpusFloor = 200` — the rows `AnchorNumberFindings` RESOLVES off
  the manifest (`AnchorNumberMigrationReport.Scanned`, 291 here). This is the
  "wrong tree / fragment" question every arm shares, and it survives the migration.
  It stays in the arm that walks the real tree
  (`TestAnchorNumberGateOnTheRealTree`), which is where it is asked first.
* `anchorNumberMaterialFloor = 1` — a fixture corpus is small by construction, so
  it is held to "the material is not empty". `anchorNumberNumberedFloor = 1` is its
  sibling for a corpus's NUMBERED rows.

The dead constant `anchorNumberReadFloor` is removed rather than left unused
(Rule 13), and the remaining arms' non-vacuity is asserted where it is earned:
`anchorNumberAssertShippedTableIsTheWritersFixpoint` fails if it checked **zero**
permits against a live finding.

### 2.2 The red arm's subject: a MUTATED permit, not a deleted one

Deleting a permit was a good subject while the shipped table had rows. At the
terminal state the wrongness that is reachable — and the direction the ratchet
calls an ERROR — is a permit that covers NOTHING, and it is reachable without any
shipped row at all: replace a data row's recorded TEXT and the key it names no
longer resolves. The generator's own report names it:

```
STALE PERMIT (in the table, no longer drifts): devdoc/material-fixture.md	src.go	a recorded text no line …
```

So `TestAnchorNumberCensusShippedFaceFiresOnAStaleTable` mutates the last data row
of the material's table and requires the generator to exit non-zero, say
`the table is not the tree`, say `STALE PERMIT`, and name the **key** it broke.
The stored row is read through the shipped reader and the key is computed through
`AnchorNumberBaselineStoredKey` (the column indices are the named constants), so
the assertion cannot name a different row than the one it mutated. A table with no
data row still fails loudly — that is the arm's own no-subject guard.

The verdicts are read by different sentences on purpose: the historical arm read
`NOT PERMITTED` (an uncovered drift) while this one reads `STALE PERMIT` (a permit
covering nothing). The assertion names the arm it needs, so a non-zero exit through
an unrelated arm (an unreadable reason, an untrue claim) fails the test.

### 2.3 The number-change arm: subjects from the corpus, not from the table

The permits were never what that arm is about. It asks whether a **number** moves a
row's identity, so its subjects are the corpus's NUMBERED rows — `f.Number > 0`,
non-empty text, `MatchCount == 1` (an ambiguous row's number is not a
determination). Those do not go to zero with the table: the current manifest still
records numbers on most of its anchor rows, and this run measures 0 numbered rows
only because the manifest was already migrated — at which point the arm's property
has genuinely **expired** and its failure message says so, naming the generator and
requiring a re-decision rather than a quietly passing arm.

The comparison keeps BOTH spellings of "the number does not re-key":
* the IDENTITY half — `anchorNumberNumberChangeShift` moves the number, the
  `TextAt` and the class, and asserts `Key()` unchanged;
* the PAIRING half — the stored rows are handed to `AnchorNumberBaselineCompare`
  against the shifted population and must produce an EXACT delta. A key that never
  moved would pair even if the comparison dropped those rows, so the pairing half is
  what makes the identity half evidence.

`(b)` is unchanged in substance: on the same material a TEXT change must produce
`unpermitted = 1, stale = 1`.

## 3. Where the material comes from (`anchor_number_material_test.go`)

`anchorNumberFaceCorpus` is the ONE decision, made by measurement:

1. the **repository's own tree and table**, whenever that table carries permits —
   the ordinary state and the strongest material; else
2. a **minimal REAL corpus** (one manifest row + the file it cites + an empty table
   at the artifact's own path) whose table the **shipped generator itself writes**.

The generator is EXECUTED (`go run internal/docclass/testdata/anchor-number-census.go
<root> <table>`), never imitated (Rule 31), and the fixture's permits are read back
through the package's own reader before any arm may use them.

Two traps this measured, both worth keeping:

* **The table argument must be ABSOLUTE.** The child's cwd has to be the shipped
  checkout (the program is a `go run` of a path inside this module), so a
  repo-relative table argument named the SHIPPED artifact — the first run of the
  helper overwrote `internal/docclass/testdata/anchor-number-baseline.tsv` with the
  fixture's single row, and every arm afterwards was reading the fixture through the
  shipped path. Caught by the red texts themselves, repaired with
  `git restore`, and the artifact is byte-identical to `HEAD` in the final tree.
* **The child needs git routing.** The generator's provenance probe resolves the
  freeze commit and reads the base manifest/anchored files out of the object store,
  and a temp corpus is not a repository: without `GIT_DIR`/`GIT_WORK_TREE` the probe
  dies with `fatal: not a git repository` and the WRITE arm exits 1 before writing
  anything. `anchorNumberMaterialGitEnv` sets exactly those two names (spelled from
  the constants `anchor_number_fixture_isolation_test.go` already owns, and whose
  mirror-image helper STRIPS them so a fixture can never write into the real
  repository). Only READS are routed; the corpus root stays argv[1], so the routing
  changes where git reads objects, never which tree is scanned.

## 4. What is NOT claimed

* **No generator change.** The generator's artifact semantics are untouched; the
  table is legitimately empty and stays that way.
* **No new skip.** Nothing here is `t.Skip`. The only skip in the family is the
  pre-existing, auditable one in `anchor_number_write_summary_test.go`, which names
  the empty table and the scan that accounts for it. The new arms fail loudly and
  say WHICH population was empty.
* **The property in 2.3 has a lifetime.** When the last numbered row leaves the
  corpus, that arm must be re-decided (deleted or replaced by a property that still
  has a subject) — that is written into its failure message, so the next reader
  meets a loud red rather than a green arm asserting nothing.
