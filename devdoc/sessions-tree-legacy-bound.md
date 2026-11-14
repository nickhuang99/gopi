<!-- doc-class: historical -->
# The Sessions Tree's LEGACY RESIDUE: Bound, and the Family Gate Closed

**Task**: `01a0f522-134a-75ae-9865-54d9756c26d4` — bound the four `legacy`
families the family registry (`internal/sessionlayout`) decided about but left
unbounded.
**Measured**: 2026-10-01, on this host, at the commit this branch lands; every
figure below is printed by a NAMED generator (`gopi scratch-gc families`,
`gopi scratch-gc trees --dry-run`, `find`) at that rev (Rule 38).

---

## 1. The defect: a decision nobody implemented

`internal/sessionlayout/families.go` registers a family per shape a real
sessions tree yields and takes an explicit decision about each —
`bound:` / `keep:` / `legacy:`. But a decision is only a decision if something
ACTS on it, and the cross-check
(`internal/scratch`'s `TestSessionsTreeGateIsRedWhenAFamilyIsDropped`) only
refuses a `bound:` family whose named tree does not EXIST in
`scratch.DeclaredTrees()`. The five residue families were all `legacy:` — the
registry saying, in effect, *"nobody has decided"* — for four of them a
statement that was true only because nobody had written the tree.

Measured on the live tree (`gopi scratch-gc families`, 2026-10-01,
`~/.gopi/sessions`):

| family | entries | shapes | what it is | bound by |
|---|---:|---:|---|---|
| `session-scratch-dir` | 277 | 165 | a run's per-container working area | **not yet** (see §5) |
| `sandbox-profile` | 35 | 21 | a `.gopi`-holding directory one level down | `session-legacy/containers` |
| `sessions-root-residue` | 500 | 15 | root-level residue files (391 `*.dump.json`, `run-*.log`, `note.txt`, …) | `session-legacy/files` |
| `task-container` | 296 | 11 | the PRE-session layout (`t-*` 246 + `process-*` 50) | `session-legacy` |
| `root-stray-dir` | 129 | 10 | root-level dirs (`evidence/`, `zz-*`, `pick`, …) | `session-legacy` |
| `foreign-checkout` | 3 | 3 | a `.git`-holding directory one level down (`smoke-clone/`, `repo/`) | `session-legacy/containers` |

(The entries/shapes columns are the family registry's own rollup —
`gopi scratch-gc families`, run 2026-10-01 on `/home/user/.gopi/sessions`:
1,471 shapes, 92,194 files, 156,038 directories, 49.1 GiB, and
**0 unclassified entries**.)

## 2. What was built — three declared rows, one root, no new machinery

`internal/scratch/declared_session_legacy.go` adds three rows to the declared
set. All three root at the sessions tree ITSELF (the same root the
`session-raw` row already uses), and each carries one of the registry's own
EXPORTED predicates as its namespace rule — so the deciding half and the
collecting half are one value, not two spellings that could drift:

| row | candidates | namespace predicate |
|---|---|---|
| `session-legacy` | root-level DIRECTORIES | `sessionlayout.IsRootTaskContainerName` ∨ `IsStrayRootDirName` |
| `session-legacy/files` | root-level FILES | `sessionlayout.IsRootResidueFileName` |
| `session-legacy/containers` | ONE LEVEL DOWN inside an aged, unheld container | `.git` / `.gopi` marker child (`GitMarkerName` / `SandboxProfileMarkerName`) |

**One row per SHAPE, never one wide row.** A declared tree's entry shape is its
`Kind` (a directory removed whole, a file, a file one level down) and its
predicate is asked per NAME — so a row that took both directories and files
could not be spelled at all. A new kind, `EntryKindFileMatch`, was added for the
file row: its namespace is an exact SHAPE TABLE (a `*.dump.json`, `run-*.log`,
`note.txt`, …), which no one suffix spells, and an EMPTY suffix on a file kind
means "every file in the tree" — exactly the blunt rule `EntryMatch` exists to
prevent. The gate `TestDeclaredTreesAreWiredAndNamed` now refuses a
`EntryKindFileMatch` row that carries no predicate, so the new kind cannot be
used to smuggle one in.

**The whole-tree sweep and the metadata level** stayed untouched: the three
rows reuse `removeCollectableOfTree` (the ONE delete path), `heldSet` /
`heldIn` (the indexed `/proc` verdict) and `unheldRegistered` (the `.holder`
channel) rather than re-deriving any of them.

## 3. The age: 180 days, and why that is the honest number

`SessionLegacyDirStaleAge = 180 * 24h`, against the raw-log tree's
`SessionRawLogStaleAge`. Three measurements decided it:

1. **The candidate is a DIRECTORY removed whole** — `evidence/` (112 of them),
   a `t-<epoch>/` container (246 `t-*` + 50 `process-*`), a probe's clone.
   Nothing re-derives any of them and no reader can inspect what the sweep took.
2. **The writer is gone, but SOP is not.** These are the pre-session layout's
   containers and hand-run probes' directories: "residue" is a statement about
   the CODE, not about how recently a human read one.
3. **30 days would take almost all of it on the first tick.** The newest
   measured container is `2026-09-27` — 4 days old when this was written. A
   30-day window means the first sweep after this lands deletes 296 containers,
  113 `evidence/` trees, 391 dumps and 2 checkouts. That is not a bound an
  operator can consent to.

180 days keeps the property the epic wanted — the tree stops growing without
limit, nothing can outlive the window — and makes the FIRST sweep collect
nothing on this host (below), so the operator reads the plan before anything
goes. The window is the operator's lever to LOWER: the CLI's
`--scratch-gc-stale-min` override is **not** applied by this arm (measured: the
dry run with `--scratch-gc-stale-min 60` still printed `stale=4320h0m0s` —
`runScratchGCTrees` passes `tree.StaleAge`), so today the only way to see a
shorter plan is the test harness, not the flag. That is a PRE-EXISTING property
of the arm, recorded rather than changed here (Rule 26: it is a separate
decision about a shared surface).

## 4. Verification

- **Build**: `go build ./internal/scratch ./internal/sessionlayout` → OK;
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK.
- **Tests**: `go test ./internal/scratch/` → ok (33.3s, whole package).
  Four new cases in `declared_session_legacy_test.go`, each a real acceptance
  rather than a restatement:
  - `TestSessionLegacyTreeClaimsExactlyTheBoundFamilies` — SET EQUALITY in both
    directions against the registry, with an under-bound control (an
    unclassified root directory and file must stay UNCLAIMED).
  - `TestSessionLegacySweepCollectsTheBoundResidueAndKeepsTheRest` — the sweep
    run for real: `tree=session-legacy removed=3`, `/files removed=1`,
    `/containers removed=2`; the root, the container, the session record and
    the two unclassified controls survive.
  - `TestSessionLegacyContainerRowIsGatedByTheContainer` — BOTH safety gates on
    a container old enough to be collected: a live process standing in it
    (a separate `sleep` child, resolved through `/proc`), and a `.holder`
    registration naming a live owner — neither can pass because of the age rule.
  - `TestSessionLegacyLiveTreeCensus` — the plan face against the operator's
    REAL sessions tree, read-only, asserting that every candidate belongs to a
    family the registry binds and that the tree's entry count is identical
    before and after.
- **Ran**: `GOPI_HOME=<sandbox> ./bin/gopi scratch-gc trees --dry-run` then
  `… scratch-gc trees` against a sandboxed sessions tree seeded with one member
  per family, and `gopi scratch-gc families` against the live tree.
- **Result**: the dry run named exactly the bound members and nothing else —
  `t-1788897004`, `zz-live-probe`, `note.txt`, and the two marker directories
  inside the container (`smoke-clone/`, `sandbox-home/`) — while the
  unclassified `brand-new-family/` and `unheard-of-artifact.bin` were NOT
  planned by any legacy row. The real sweep then reported
  `tree=session-legacy removed=3`, `tree=session-legacy/files removed=1`,
  `tree=session-legacy/containers removed=2`, and every control (the root, the
  session container, the `.jsonl` inside it, and both unclassified entries)
  survived. On the LIVE tree all three legacy rows plan **0** entries: nothing
  in that tree is older than 180 days, so this change removes nothing on this
  host and only stops the tree from growing without limit from here on.

## 5. Not Done (honest)

- **`session-scratch-dir` stays a `keep:` family** — 277 entries / 165 shapes,
  every one a directory a run created inside its own container (`evidence/`,
  `probe.*/`, `redproof-*/`, …). It is NOT bounded here, and the reason is a
  decision this task did not take: those names are an OPEN set (a new probe
  name appears every week), so a namespace predicate would be a catch-all
  ("every directory in a container") and the container's own lifecycle — the
  thing that would make "the run that made it is gone" answerable — is what the
  sibling task `01a0f522-152b-7921-890e-25e03f0276e3` owns. It is still
  REPORTED by `gopi scratch-gc families`, which is what a `keep:` decision
  means.
- **The `session-raw` row's own directory arm still has no namespace rule.**
  Measured in the sandbox run above: `tree=session-raw` planned
  `brand-new-family/`, `smoke-clone/`, `t-1788897004/` and `zz-live-probe/` —
  root-level directories with no `*.raw.log` inside them, i.e. the whole
  sessions tree's top level once its files age out. That is a PRE-EXISTING
  behaviour of `EntryKindTreeFileSuffix`'s empty-of-candidates arm
  (`containerEntries`), not something this change introduced, and tightening it
  is a separate decision about a row another task owns — it is recorded here
  rather than fixed silently, because a reader of this file is the person most
  likely to meet it.
- **`--scratch-gc-stale-min` does not reach this arm.** `runScratchGCTrees`
  passes `tree.StaleAge` for both the plan and the sweep, so the flag the
  package documents as "the caller's override" is inert on `scratch-gc trees`.
  Pre-existing (observed with `--scratch-gc-stale-min 60` → `stale=4320h0m0s`),
  NOT introduced here, and NOT fixed here: it changes a shared CLI surface, and
  a reviewer should decide it as its own change. It is recorded because it is
  the exact lever a reader would reach for when they want to see a shorter plan
  of what this bound would take.
- **No live-tree deletion was performed.** The validation ran against a
  sandboxed profile (Rule 34); the live tree was only ever READ, and the census
  is reported, not asserted.
