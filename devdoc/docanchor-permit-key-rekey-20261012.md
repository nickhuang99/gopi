<!-- doc-class: normative -->
# docanchor permit re-key — the key stops carrying the anchored file's line number

> **Provenance.** `TestDocAnchorsResolveInNormativeDocs` was retired on 2026-10-22 in `5a35f61` (deleted with `internal/docclass`), so a `file.go:NNN` reference in this page is a landing-time record, not an anchor a live gate enforces — nothing resolves one today. The line numbers stay useful to a reader; they are not checked by the suite. Citations below are quoted as they were written — where one of them reads as present tense ("is RED", "resolves", "fails"), read it as the state of THAT run, never as a gate this tree still has.

Task `01a12e76-f614-74a6-8bd6-41f58e9d993e`. Branch `task/01a12e76-f614-74a6-8bd6-41f58e9d993e`.

## Completed

- **`internal/agent/docanchor_test.go`** — the permit key is now
  `doc|kind|<anchor identity>#<ordinal>`:
  - `docAnchorProblemKeyBase` / `docAnchorAnchorIdentity` strip the anchored
    file's line locator from every problem's identity (`docAnchorLineLocatorRe`,
    `.go:[0-9]+` → `.go`).
  - The **ordinal** disambiguates an identity that occurs more than once in one
    doc. It counts **every** anchor of that identity in the doc — the ones that
    resolve and the ones that drift — so it does *not* renumber when a
    neighbouring site is fixed. (`identify()` in `docAnchorScan`.)
  - `docAnchorProblem` gained `Locator` (the absolute `path.go:NNN` the key
    dropped) and `Ordinal`; `docAnchorDetail` is the one place the key's third
    field is spelled.
  - The `locator` survives in the **reason column**: every re-keyed row starts
    `locator <path>:<NNN>; `.
- **`internal/agent/testdata/docanchor-baseline.tsv`** — mechanically re-keyed:
  311 data rows, every row carrying its `locator …;` prefix.
- **Anti-regression tests** (new):
  - `TestDocAnchorPermitKeyIgnoresAnchoredFileLength` — one minimal doc fixture
    scanned against two scratch trees built by the ONE sandbox helper
    (`testutil.TestDeviceScratchDir`, off `/`): the same anchored file at N lines
    and at N+50 lines *inserted mid-file*, so the anchored line really moves. The
    emitted key set must be **byte-identical**, the problem count must not change,
    and **no key may match a `.go:NNN` locator**.
  - `TestDocAnchorPermitKeyStillTracksRealDrift` — the ratchet's other half: a
    resolving anchor emits nothing, and a real drift (anchor past EOF) still emits
    exactly ONE problem, with an ordinal in its key and a locator beside it.
- **`devdoc/docanchor-permit-key-rekey-20261012.md`** (this file), classified in
  `devdoc/DOC-INDEX.tsv` in the same commit (Rule 36).

## How the 311 rows were produced (Rule 38 — no hand editing)

The re-key script is **not** committed: `gopi` is a single program and a
repo-side one-shot converter is a script-shaped dependency (Rule 35). It lives
with this run's evidence; the artifact it produced is committed.

```
python3 ~/.gopi/sessions/01a12e76-f614-74a6-8bd6-41f58e9d993e/artifacts/rekey.py \
        ~/.gopi/sessions/01a12e76-f614-74a6-8bd6-41f58e9d993e/evidence/emit3.txt \
        <pre-re-key baseline.tsv>  <new baseline.tsv>
emitted=310 paired=308 (identity-perfect 308, requalified 0) new=2 stale-dropped=0
wrote 310 rows to new.tsv
```

The input `evidence/emit3.txt` is the gate's **own** output: run with an
empty (header-only) permit list, so the gate emits every problem's key instead
of permitting it. The script pairs each emitted key with the aged row it
replaces — priority order (identical identity → identical locator string →
identical basename, because a row may name a site by basename where the gate
reports the full path) — and prefixes the surviving row's reason with the new
`locator`. Two emitted sites had **no** pre-existing permit under any
doc-anchored spelling; they carry an explicit `NO PRE-EXISTING PERMIT …` reason
and are the honest count of pre-existing drift that the re-key surfaced.

The 2 rows lost on the way were the ratchet doing its job, not a defect: this
change makes two anchors of `devdoc/docanchor-blank-line-fix.md` **resolve**
(previously they pointed at blank lines of `docanchor_test.go`), so their
permits went stale and were deleted. `devdoc/docanchor-blank-line-fix.md`'s own
subject is exactly those anchors.

## Not Done (honest)

- **A *dense* insertion still moves keys, and this epic does not fix that.**
  Measured: +50 lines inserted into `internal/agent/loop.go` after the import
  block → **21 new / 15 stale** (was 24 new / 36 stale before this change). An
  *append* of the same 50 lines → **0 new / 0 stale**. Three causes remain, and
  none of them is a line number living in a key:
  1. an anchor *of the same doc* that used to RESOLVE can start failing when the
     inserted block lands under it (measured: `devdoc/look-without-fixing-bound.md`
     reports 2 problems before and **3** after — its two anchors (which named
     lines 1906 and 2527 of that file) give way to three others — which changes
     how many problems an identity has, and therefore renumbers it;
  2. inserting a block at the top of a file shifts a doc's anchors *past EOF* once
     they exceed the new length, so their kind becomes `eof` (`blank` → `eof`) —
     a different key for a genuinely different finding;
  3. the ordinal counts anchors of an identity in ONE doc, so a doc whose only
     `loop.go` anchors all sit inside the inserted region sees its ordinals move.
  All three are facts about **which line of the file a doc points at and whether
  that line is blank** — i.e. about CONTENT — not about the file's length, which
  is what the key used to be sensitive to. Removing them needs the permit keyed on
  the *symbol* the doc's row names instead of the anchor form: a corpus-wide
  re-key of another order, filed as a follow-up and not claimed here.
- `gopi tool doccheck` was **not** run (the surface was not exercised); the
  manifest row was added to `devdoc/DOC-INDEX.tsv` directly and the corpus was
  then exercised through the gate itself.

## Verification

- Build: `go build ./...` → OK (exit 0).
- Tests: `make test-budget PKGS='./internal/agent ./internal/standards ./internal/stylegate'`
  → agent PASS (incl. both new guards); the 4 reds printed are **pre-existing and
  unrelated** — `TestCommandsDocGeneratedBlockMatchesSource`,
  `TestStyleGate` (`stylegate ignored-error`, 2 sites in `internal/tools/`, and
  scratch failures in the budget run), `TestCommandsDocGateRedProofStaleGeneratedBlock`,
  `TestGateCwdReleaseCensusArmsAreListed`. None of them is in the docanchor
  family; `internal/agent` is green.
- Ran: `go test ./internal/agent/ -run TestDocAnchorsResolveInNormativeDocs -count=1 -v`
  → `doc anchors verified: 3175 across 743 normative docs (2667 explicit + 508
  bare continuations; 612 bare out of row/table/heading scope; 308 pre-existing
  drift entries permitted, 0 new, 0 stale permits)` — `--- PASS`.
- Ran (acceptance b, invariance): with 50 lines inserted into
  `internal/agent/loop.go`, `go test ./internal/agent/ -run
  TestDocAnchorsResolveInNormativeDocs -count=1` is **unchanged at 0 new / 0
  stale**, and with the same 50 lines **appended**, also 0/0. Both perturbations
  were reverted (`git status --porcelain` clean of `loop.go`).
- Ran (acceptance c, the ratchet): deleting a permit's real site target — the
  edit that made the 2 `docanchor-blank-line-fix.md` anchors resolve — printed
  exactly `stale baseline entry "devdoc/docanchor-blank-line-fix.md|blank|docanchor_test.go#1"`
  and `#2`. Recorded, and the two rows deleted.
- Ran (red-green of the new guard): with `docAnchorProblemKeyBase` reverted to
  the old spelling (locator in the key), and with `docAnchorAnchorIdentity` made
  identity (`return shown`), `TestDocAnchorPermitKeyIgnoresAnchoredFileLength`
  fails with
  `permit key "docanchor_test.go|blank|docanchor_test.go:76#1" still carries a line number — the permit list is back to being invalidated by any edit of the file it anchors`;
  restored, it passes.
- Commit: _see `git log` on the branch_.

## Evidence (self-filed)

- `evidence/emit3.txt` — the gate run whose emitted key set is the re-key input
  (`310 new, 0 stale`, header-only permit list).
- `evidence/final-dense50.txt` — the measured dense-insertion residue
  (`21 new, 15 stale`), the basis of the `## Not Done` item.
- `evidence/redproof.txt` — the red output of the new guard with the identity
  function broken.
- `artifacts/rekey.py` — the one-shot converter, with its `emitted=310 paired=308
  … new=2 stale-dropped=0` line.
