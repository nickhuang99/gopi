<!-- doc-class: historical -->
# The devdoc manifest's write path: classify at the door, not at the gate

**Task** `01a0db31-19cc-725f-b8b9-0115f62b4060` (peerbox), 2026-09-26. The epic:
`devdoc/DOC-INDEX.tsv` kept leaking documents. A new file under `devdoc/` could
land with no manifest row and no class banner, and the ONLY thing that noticed
was `go test ./internal/standards/` — running on the integration branch, long
after the author's session was gone, and unrelated to whatever diff happened to
be in flight. Three instances were measured in ONE day; the third
(`devdoc/repo-hygiene-main-checkout-pollution.md`) landed with neither row nor
banner and made every session's standards run red.

This is the record of what was built, what was measured, and what was left.

## The root cause, stated once

**The manifest had a read side and no write side.** Every check of it
(`TestDocManifestCoversEveryDevdocFile`, `TestDocManifestRowsAreWellFormed`,
`TestDocClassBannerMatchesManifest`) runs at TEST time and can only REPORT. The
row was written by whoever remembered to run `tools/docclass/classify.py`, and
`internal/docclass` — the package every gate reads — had no writer at all.

The failure shape is Rule 26's: *the doc gets written, the manifest row does
not.* A read-side gate cannot close that, because by the time it runs the write
has already happened and the author has moved on.

## What was built

`internal/docindex` — the write side, deliberately small:

| piece | what it answers |
|---|---|
| `Row` / `Cell` | one row RENDERER: four columns, single-line cells, no separator inside a cell |
| `EnsureDoc` | the conservative fallback: banner + row, idempotent, pure insertion, never re-classifies an already-classified document |
| `ReadManifest` / `EnsureRow` / `reindex` | the manifest held as LINES, with every index DERIVED from them |
| `ScopeCheck` | the BRANCH-SCOPED reading: what do the documents THIS BRANCH adds carry? |
| `Repair` | writes the missing half, keeping what a malformed row still said |
| `SyncBranch` | the delivery-path step: classify the branch's own additions and COMMIT them |
| `ClassifyWithGenerator` / `ClassifyWrittenFile` | the ONE classifier: the repo's own `tools/docclass/classify.py` |

### It delegates to the repo's own classifier (the load-bearing decision)

`tools/docclass/classify.py` reads a document's CONTENT — does it name an
artifact that resolves, is it cited by a current artifact — and `banner.py`
writes the banner. Those two are the corpus's classifier, and a write hook that
imposed its own opinion would make a document's class depend on WHICH DOOR it
came through. So the hook runs the generator (`classify.py --only <path>`, then
`banner.py --only <path>` — the INCREMENTAL modes, so one write never rewrites
2400 rows) and falls back to its own conservative default only where there is no
generator to ask (another project's `devdoc/` tree, a test fixture). The actor is
recorded either way, in `Fixup.ClassifiedBy` and in the row's own reason column.

The hook runs AFTER the write, and that ordering is the point: the generator
READS the file it classifies, so a document that is not on disk yet cannot be
classified by it.

### Where it is wired

1. **The write tool** (`internal/tools/write.go`) and **the edit tool**
   (`internal/tools/edit.go`) — the two doors through which an agent creates a
   document. Off the corpus they are a no-op; if the classifier cannot run, the
   WRITE still stands and the failure is recorded (the documented write is the
   deliverable — a classifier that breaks must not eat it).
2. **The delivery-point sync** (`internal/tools/task_finish_docclass.go`, wired
   into `finishCleanupLocalSync`) — the last moment the run owns its branch.
   It measures what the branch adds (three-dot against the integration branch),
   writes what is missing, and COMMITS it, then the existing alignment merge and
   build/vet run on the classified tree. Committing matters: a repair left dirty
   is invisible to the merger and reads as residue at close-out (Rule 30).

### What was deliberately NOT done

- **The read-side gates were not weakened.** They are the backstop and the
  evidence; "make master green" is not a reason to relax them.
- **The writer does not classify by content itself.** It delegates; the fallback
  is a record (claims nothing), never a guess at a standing claim.
- **A refusal, not a sweep.** The classification commit is made with `git
  commit`, which takes whatever is staged — so a ground holding anything this
  check did not write is REFUSED before a single byte is written, and the
  refusal is reported on the row's trail (Rule 15).
- **`gopi merge` was not touched.** See *Not Done* below.

## Measured on master while building this (the leak has three faces, not one)

Read-only census of `devdoc/DOC-INDEX.tsv` + `git ls-tree`, 2026-09-26:

| face | count | what it does to every session |
|---|---|---|
| malformed rows (3 fields, hand-typed, trailing evidence column dropped) | **6** (`:2422`–`:2427`) | `ParseManifest` refuses the WHOLE file → all three docclass gates fail, one line per file |
| duplicate rows for one path | **4** (`repo-hygiene-main-checkout-pollution`, `chat-delta-cadence`, `task-sync-banner-source`, `worktree-removal-forensics`) | the coverage gate fails on the duplicate |
| files with no row at all | **2** (`memsync-asset-import-batched-lock.md`, `tui-turn-status-live-hint.md`) | the coverage gate fails, one entry per file |

The first two faces are HANd repairs: the same omission, repaired by hand, in the
file that cannot be validated by hand. That is why the repair goes through the
write path's own library rather than through an editor.

**After the repair** (same tree, `go test -run TestRepoCorpusScopedGaps -v
./internal/docindex/` with `GOPI_DOCINDEX_EVIDENCE=1`, which reads the REAL
corpus through the shipped reading):

```
rows=2416 malformed=0 duplicated=0 missing=0 unbannered=0   ← right after the repair
rows=2417 malformed=0 duplicated=0 missing=0 unbannered=0   ← at the branch's final tip
```

The row count moves by one because this branch's OWN document
(`devdoc/docindex-write-path.md`) lands through the very write path it describes:
the write hook classified it on the way in. So the census that read 2403 rows /
2404 files when the leak was diagnosed reads **2417 rows covering 2417 files**
here, and the manifest's growth is accounted for: 6 malformed rows rewritten in
place, 4 duplicates collapsed, 2 files given the row they lacked, 1 new document,
and the index-meta row the generator rewrote.

`go test ./internal/standards/` — the package that was red on master before this
branch — is green (see the report's `## Verification`).

The 6 malformed rows were repaired by keeping the class and reason they already
carried (rewritten in place, evidence column restored), the 4 duplicates by
keeping the LAST row (the hand repair), and the 2 missing files through the
repo's OWN generator (`classify.py --add-missing`), which is also why the
`index-meta` row of the manifest itself now says what it is.

## Not Done (honest)

- **`gopi merge` does not run this check.** The close-out is where a branch is
  classified today; a branch that reaches the merger WITHOUT passing through a
  close-out on this host (pushed from another host, an operator's manual merge)
  is still only caught by the read-side gates on master. Closing that means
  wiring the same reading into `internal/tools/task_merge_*`'s require-stage
  gate, which is the merge machinery — peerbox's layer, and its own change.
- **An unprovable dirty tree abstains instead of refusing** inside `SyncBranch`
  (the classification cannot be committed, so it is recorded and the delivery
  continues). The alternative — refusing the delivery — would turn a working
  tree into a failed close-out for a defect the read-side gate still reports.
- **The windows where the write tool's classifier silently does nothing**
  (a corpus file written by a shell command, `cat >`, `python3`, a generator)
  are not closed: those writes do not pass through the tools at all. The
  close-out's branch-scoped check is the net for them, and it now names them.
- **The three documents the earlier session repaired without review** (see the
  report's `## Not Done`) still carry rows a hand wrote; two of them also carry
  a duplicate. The repair makes the file VALID, not the rows reviewed.
