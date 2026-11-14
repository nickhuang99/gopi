<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# The anchor cell's owner, and a freeze table that is allowed to reach zero

`census-evidence-ok: figures here are this run's own measurements, not a project-wide census`

**Task:** `01a138fc-8eb17f2ca-927436039d6b5cf` (session dir carries the raw evidence
under `artifacts/`). System layer, Rule 28 — landed through the normal merge path.

## The conflict, in one paragraph

Two surfaces write/read the devdoc manifest's anchor cells and could not both be
satisfied. `tools/docclass/classify.py` renders them **number-free** —
`anchor <path> at <text>` — a measured 2026-10-14 decision (task `01a136a0`): a
stored `:NNN` is invalidated by any edit above the cited site *without* changing
what the row claims, so every numbered row is a rewrite obligation for whoever
moves a line (173 of 251 anchor rows were exactly that, and two runs re-deriving
one such row conflict in git). On the other side, `internal/docclass/anchor_number.go`
freezes the "anchor-NUMBER incoherence" population — numbered rows whose `:NNN` no
longer points at the recorded text — in a ratchet-**down** permit table, and three
of its readers refused an **empty** table as such (`"carries no permits — the table
must freeze a population, not be empty"`). So the migration that shrinks the table
to nothing produces an artifact its own reader rejects, and the generator's
regeneration refuses to keep it.

Both halves are ONE state — the table has no rows — and the only thing that tells
them apart is the **tree**: a table emptied while numbered drift is live is a
dropped population; a table emptied because the numberless render has reached every
row is the migration finishing.

## The decision (written where both surfaces read it)

**The number-less render owns the anchor cell's shape.** A stored
`anchor <path>:<line> at <text>` is **legacy data with a terminal state: zero
numbered rows in the manifest.** Nothing anywhere reads a number as a *claim* — the
Go reader already treats it as an accelerator and abandons it on its first miss —
so the migration retires an accelerator, never a claim, and it is finished by
re-deriving rows with the generator, never by hand-editing the manifest (Rule 36).

Stated in two places, both of which a future writer reads *before* writing:

- `internal/docclass/evidence.go` — the anchor cell grammar comment (`WHO OWNS THE
  SHAPE, decided`), beside the constants that spell the cell;
- the census's generated header (`# OWNER: …`), inside the artifact itself, so a
  reader holding the table when the two surfaces disagree has the ruling in hand.

## What changed

1. **`internal/docclass/anchor_number_migration.go` (new)** — the criterion, spelled
   once:
   - `AnchorNumberMigrationReport` — the census's own account (`Scanned`, `Numbered`,
     `Incoherent`, `Permits`, `Ambiguous`), built by the ONE constructor
     `AnchorNumberMigrationOf(findings, permits)` from a single scan;
   - `EmptyIsAccounted()` — the whole criterion: an empty table is green **iff the
     same scan measured no incoherent row**;
   - `AnchorNumberEmptyTableVerdict(rep)` — refuses exactly one state (no permits
     while the scan measures incoherent numbered rows) and names the population,
     the shape and the generator that repairs it; nil for a non-empty table, so it
     can never be used to skip the pairing arms;
   - `Sentence()` — the terminal state as a **measurement** (`254 anchor row(s)
     scanned, 0 incoherent, 0 still record a line number`), so a pass cannot read
     as an absence (Rule 15);
   - `AnchorNumberShapeOwner` — the owner's name, one definition (Rules 13/14).
2. **`anchor_number_baseline_header.go`** — a fifth header figure, `numbered`: the
   manifest anchor rows still recording a line number. The **migration's own
   odometer**, declared and re-derived by the check arm, which is what turns "the
   table is empty" from an absence into a figure a reader can act on (Rule 38).
3. **`anchor_number_baseline.go`** — `AnchorNumberDriftReport.Migration`, measured
   from the same findings *and* the same baseline as the three arms above (a second
   scan here would be a second census).
4. **`testdata/anchor-number-census.go`** — emits the ownership paragraph in the
   header, writes and re-derives the `numbered` figure, applies the criterion in
   the check arm, and prints the migration account on both faces. **Measured bug
   found while doing so:** the check arm was handed the *incoherent subset* of the
   scan, so a check of the freshly-written table re-derived `numbered=142` where the
   write arm had measured `252` and the header *it had just written* failed against
   it. The whole scan is now passed; the arms that need only the drifting rows ask
   `Incoherent()` themselves.
5. **The three blanket refusals are replaced by the criterion** —
   `anchor_number_gate_test.go`'s reader helper (`anchorNumberReadBaseline`),
   `anchor_number_stability_test.go`'s reason-column arm, and
   `internal/standards/devdoc_anchor_number_gate_test.go`'s permit floor. Each also
   **reports** the migration account, so an empty table on a machine and an empty
   table in a log read the same way.
6. **The arms that must survive the terminal state were NOT weakened** and are asked
   *before* it: the read floor (`devdocAnchorNumberReadFloor`) and the ambiguity
   floor stay exactly where they were, because "the table is empty" and "the scan
   never ran" must not read alike (Rule 15/39). The permit floor now holds *while
   there is a population to freeze* (`!EmptyIsAccounted()`), which is the same
   condition stated from the tree side.

## The red proofs (a criterion that only gets greener is not a criterion)

Arms in `internal/docclass/anchor_number_migration_test.go`, over **materialised**
corpora (not predicate calls): one manifest row + an **empty** table, the row's
shape being the only difference between the two trees.

| arm | tree | verdict |
|---|---|---|
| `…IsRefusedWhileNumberedDriftLives` | numbered cell drifting (`:2` vs text on `:3`) | **refused**, naming the artifact, the state, the population and the generator |
| `…PassesWhenTheMigrationIsComplete` | number-free cell | **passes**, and the sentence names the owner and the scanned count; the *same* predicate still refuses the drifted tree, so the green is not "always nil" |
| `…CriterionDoesNotSpeakForAFullTable` | 142 permits | nil — the pairing arms own it |
| `…ReaderJudgesAnEmptyTableOnDisk` | both, through the real on-disk path | green through the helper itself; red through the same Load→resolve→judge calls the helper makes |

In `internal/standards/`:
`TestDevdocAnchorNumberEmptyTableIsJudgedOnTheFixtureTree` — the same pair over the
gate package's own fixture builder (`anchorNumberFixtureTree`), where only the
recorded line differs, so the green arm cannot pass for a predicate that refuses
every empty table.

## Verification

- **Build:** `go build ./...` — OK.
- **Tests:** `go test ./internal/docclass/ -run AnchorNumber` → `ok 55.5s`;
  `go test ./internal/standards/ -run DevdocAnchorNumber` → `ok 9.8s`;
  `make test-budget PKGS='./internal/docclass ./internal/standards ./internal/docindex'`
  → see the report (Rule 5).
- **Ran (Rule 31), the briefing's own repro, end to end:**
  `python3 tools/docclass/classify.py . --only-file numbered-anchor-files.txt`
  → `{"added": 0, "updated": 240, "rows": 2518}`; **0** numbered anchor rows left.
  `go run internal/docclass/testdata/anchor-number-census.go . internal/docclass/testdata/anchor-number-baseline.tsv`
  → `234 anchor row(s) scanned, 0 incoherent … 137 permit(s) removed`, table header-only.
  `… --check` → **exit 0**, `the freeze table is EMPTY and this scan accounts for it:
  234 anchor row(s) scanned, 0 incoherent, 0 of them still record a line number`.
  Raw logs: `~/.gopi/sessions/01a138fc-…/artifacts/repro-check-migrated.log`.
- **And on the shipped tree:** `… --check` → exit 0,
  `254 scanned, 142 incoherent; table carries 142` (regenerated — the `numbered`
  figure is new, so the old header was legitimately stale).

## Not Done (honest)

- The manifest on this branch is **not** migrated: `devdoc/DOC-INDEX.tsv` still
  carries 252 numbered anchor rows, because a 252-row manifest rewrite is a data
  change that collides with every other in-flight branch and belongs to the corpus
  owner's own run (Rule 36: the manifest is regenerated, never hand-edited). What
  the branch delivers is the **criterion** — the proof that the migrated state is
  reachable, green and accounted for. The two candidate rows the census reported as
  unpermitted on this branch's base were reproduced as a *stale-table* state (the
  table had been generated before `cmd/agent/cli_tasks_rebuild.go` moved); the
  regeneration in the report's `Ran:` line fixes exactly that.
- The doccheck repair command was not re-pointed at the census; it reports the
  migration figure and stays read-only, which is the shipped behaviour.
