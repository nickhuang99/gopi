<!-- doc-class: historical -->

# doccheck stored-cell drift — the corpus's DATA catches up with its generator

WHY THIS DOC EXISTS. On 2026-10-14 master, `gopi tool doccheck` exited 1 on a
tree nobody had touched, and the acceptance condition every docclass briefing
names ("doccheck exits 0") was therefore UNREACHABLE for reasons no caller's
change had caused. Three legs answered, and only the third failed:

| leg | before | after |
|---|---|---|
| coverage gaps | `0 gap(s)` | `0 gap(s)` |
| anchor NUMBER coherence | 139 of 139 frozen, 2 NOT PERMITTED, 0 STALE PERMIT | 141 of 141 frozen, 0 NOT PERMITTED, 0 STALE PERMIT |
| stored-cell drift | `788 of 2518 rendered row(s)` | `0 of 2518 rendered row(s)` |
| exit | 1 | 0 |

The third leg was landed by `9aae5a2b9` and made to SPEAK by `5b5f97903` (its
arm had printed a measured drift and then returned the clean value). It is a
good guard: it asks whether each manifest row's stored text is what the corpus's
own classifier renders today. The answer on this tree was "no, for 788 rows",
and the correct resolution is for the DATA to catch up — NOT for the leg to be
narrowed to zero (memory #14158 records that trap by name).

THE SHAPE: TWO GENERATED TABLES, TWO OWN GENERATORS, NO HAND EDIT.

1. The manifest is re-derived by its ONE writer, and only for the paths that
   drifted. `tools/docclass/classify.py <repo> --only PATH` (repeatable) or
   `--only-file LIST` puts the process into its INCREMENTAL writer: named paths
   are re-classified, every other row is carried over byte-for-byte, and the
   splice leaves the file's line order untouched. The whole-corpus spelling
   (`classify.py <repo>` with no scope) is a NO-OP against drift — it carries
   every existing row byte-for-byte by design — so it can neither repair nor
   even see this population.
   Measured cost: one process renders 2517 paths in about 60 seconds, so the
   writer is invoked ONCE with a path list, never in a per-path loop.

2. The anchor-NUMBER freeze table is re-derived by ITS own generator,
   `go run internal/docclass/testdata/anchor-number-census.go <repo> <table>`,
   and never by hand (Rule 36/38: a generated table names its generator).

THE INTERACTION THAT MAKES THIS TWO TABLES AND NOT ONE. The freeze table's
permits are keyed on the manifest's ANCHOR CELLS (the evidence column), so
re-deriving the manifest moves that population as a side effect. It did, and the
movement is a RATCHET DOWN in the generator's own reporting:

* the two `NOT PERMITTED` rows (`devdoc/merge-must-mint-version-tag.md`,
  `devdoc/repo-workspace-hygiene-2026-09-12.md`) were re-classified
  `normative -> historical`, and a historical row's evidence carries no anchor
  cell at all — so those rows left the anchor population, which is why the
  frozen figure reads `141 of 141` afterwards rather than 139 of 141;
* the generator reported `2 permit(s) added, 0 permit(s) removed` on its own
  write, i.e. the commit lands two rows in the table, and the table's own
  `--check` then answers `the freeze is exact`.

THE DRIFT POPULATION, TRIAGED (read-only; the writer is the only thing that
touched the manifest). Of the 788 drifted rows:

* 194 moved their class AND their evidence — 115 `historical -> normative`
  (their evidence now RESOLVES: an anchor, a path, a symbol or a citation the
  codebase has since grown — mechanical, not a policy decision) and 79
  `normative -> historical`;
* 428 moved their evidence only, and the dominant shape is the number-free
  anchor cell: stored `anchor <path>:<line> at <text>` versus today's rendered
  `anchor <path> at <text>` (235 rows), with the live `stale=N` tail dropped
  from 4 more;
* 166 moved their reason only — the live file tail (`historical (N standing /
  M record)`) recounting as citations landed.

WHAT WAS NOT DONE, AND IS NOT A GAP.

* The third leg was NOT weakened, and no cell was hand-edited. Both generated
  tables were written only by their own generators, and the idempotence of each
  is the proof: a second writer run changes nothing (the manifest is
  byte-identical after it), and the census generator's `--check` prints `the
  freeze is exact`.
* The strictness is proven by a RED arm rather than asserted: restoring the
  PREVIOUS revision of the freeze table makes the anchor-NUMBER gate fail, and a
  deliberately drifted manifest row still yields the drift class and exit 1 (the
  arm's own proof drives the real `docCheckReport` through its one seam).

RE-LANDED 2026-10-15 (task `01a1394d-b990-7be8-b4b4-931e85809c58`). The run above
landed on `6dc0400e7` and its branch was dropped; the intent was re-applied to
`9312d3c`, 156 commits later. The 788-row repair is a RECORD of that day, not a
diff to carry: master had meanwhile re-derived its own manifest (835 / 812 lines),
so a re-land inherits nothing. What the corpus drifted by on the new baseline was
re-measured and re-derived through the same ONE writer:
`classify.py . --check` → `1 gap(s)` (this very file, unclassified) plus
`DOCCLASS STALE` on `devdoc/doccheck-abstention-status-and-next-step.md` and
`devdoc/test-budget-package-attribution.md` (a citation line and a live count).
All three were cleared by `classify.py . --only <path>` ×3 — the writer, never a
hand edit — and `gopi tool doccheck` then exited 0 with
`stored-cell drift: 0 of 2542 rendered row(s)`. The anchor-NUMBER leg's freeze
table is EMPTY on this baseline and its own census generator reports
`the freeze is exact` (`271 anchor row(s) scanned, 0 incoherent`), so there was
no second table to carry over.
