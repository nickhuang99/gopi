<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# Task Report: the profile's SIBLING TREES — a range census with a decision each

## Completed

- **`internal/sessionlayout/sibling_families.go`** (new): the sibling-tree family
  registry — five trees (`archive`, `backups`, `notify`, `dumps`, `dumps-1214`),
  eleven families, each with a `Decision` (`bound:<tree>` / `keep` / `legacy`) and
  a Reason that IS the contract. Classification is one predicate
  (`ClassifySiblingFamily`), a scan is one walk (`ScanSiblingTree` /
  `ScanSiblingTreeWith`), and the profile root arrives through a function seam
  (`RegisterSiblingDataRootResolver`) so this package stays a leaf.
- **`internal/scratch/sibling_dumps.go`** (new) + two rows in `DeclaredTrees()`:
  `sibling-dumps` and `sibling-dumps-1214`, swept by the machinery that already
  ticks. Namespace predicate = a dump's own timestamp name; file-scoped 14-day
  window. No new script, cron, daemon or state file (Rule 35).
- **`internal/config/scratch_trees.go`**: wires the seam ONCE for both registries
  (`scratch.RegisterSiblingDataRootResolver(GopiDir)` and
  `sessionlayout.RegisterSiblingDataRootResolver(GopiDir)`).
- **`cmd/agent/cli_scratch_gc.go`**: `families` gained `--tree NAME` / `--tree all`
  and `--list`; it now censuses the SIBLING trees through the same printer as the
  sessions tree. The default arm is unchanged (the sessions tree alone).
- **`CONFIG.md`**: both dump-knob rows now state the precedence, the durable
  Default, and the verb that censuses what they accumulate.
- **`devdoc/sessions-tree-siblings-census.md`** (new, class `normative`) + its
  `DOC-INDEX.tsv` row, and the two new help forms added to
  `devdoc/gopi-tools-maintenance-help-surface-inventory.md`.
- **Tests**: `internal/scratch/sibling_tree_families_test.go` (the gate, two
  red-proofs, a live report-only corroboration, and the writer-name pin) and
  `internal/config/sibling_tree_resolvers_test.go` (the seam).

### The `dumps/` discrepancy — resolved

It is the **environment override**, not a code path. Both writers honour their
documented default (`scratch.DumpDir(<class>)`, the scratch dumps class tree);
`~/.bashrc:139-140` export `GOPI_ERROR_DUMP_DIR=/home/user/.gopi/dumps` and
`GOPI_1214_DUMP_DIR=/home/user/.gopi/dumps-1214`, and every live gopi inherits
them (measured: present in `/proc/<pid>/environ` of three live processes). A
shell-profile export is durable, which is why the tree grew to 4,345 files /
102 MiB. The fix is the corrected statement (CONFIG.md + devdoc) plus a real
bound, NOT a writer change — the knob exists so the operator can choose.

### A fifth tree found while measuring

`dumps-1214/` is the same shape for the second knob. It is EMPTY today and
registered anyway: the registry describes the RANGE (which directories are ours
to look at, and what we decided), and an empty tree on one host is not a reason
to leave a knob's target uncensused.

## Not Done

- **No collection was RUN against the operator's trees.** The declared tree
  planned 2,270 files / 48.6 MiB of `dumps/` in a dry run; deleting is the
  operator's call (or the watchdog's own tick). Everything here is report-only
  over their state.
- **No retention decision was invented for `backups/` or `notify/`.** Both are
  `keep:` with a reason — `backups/` is restore material (a disaster-recovery
  contract, not a sweeper's call) and `notify/` is written by the operator's own
  `notify.cmd`, so its retention contract is theirs.
- **`archive/` is not swept**, and 1.6 GiB of its 1.7 GiB is one unpacked rescue
  directory. A bound without a restore story is a delete button.
- **The census walks ONE level**, so `archive`'s reported 1,239,612 bytes is the
  tree's own entries, not its `du` (1.7 GiB). Both numbers are true and the doc
  says which is which.

## Verification

- **Build**: `go build ./...` → exit 0; `CGO_ENABLED=0 go build -o bin/gopi
  ./cmd/agent/` → exit 0.
- **Tests**: `make test-budget PKGS='./internal/sessionlayout ./internal/scratch
  ./internal/config ./cmd/agent'` → PASS for all four packages. The FIRST run of
  that command went red on two real defects, both fixed rather than exempted:
  - `internal/config` `TestSessionsDirNameNoHardcodedLiterals`: the selector's
    sessions name was a literal `"sessions"`; it now reads `config.SessionsDirName`
    (Rule 13).
  - `cmd/agent` `TestToolsMaintenanceHelpSurfaceInventoryMatchesDerivation`: the
    two newly advertised forms were absent from the help inventory; the inventory
    is regenerated and renumbered in this commit (Rule 31).
- **Ran** (Rule 31 — the changed surface, with real arguments):
  - `./bin/gopi scratch-gc families --list`
  - `./bin/gopi scratch-gc families --tree all`
  - `./bin/gopi scratch-gc families --tree dumps`
  - `./bin/gopi scratch-gc families --tree nope` (the refusal)
  - `./bin/gopi scratch-gc trees --dry-run`
  - `gopi scratch-gc families --tree dumps` / `--tree dumps-1214` against a
    FIXTURE profile (`GOPI_HOME=<fixture>`), for the bound's positive and
    negative cases.
- **Result**: see `## Evidence` below.
- **Commit**: the branch tip at close-out — subject
  `feat(sessionlayout): census the profile's SIBLING TREES with a decision each`.
  The HASH is deliberately not written here: this report is amended INTO that
  commit, so a hash spelled inside it names the commit the amend replaces, and a
  hash that resolves to nothing is the failure Rule 5's evidence section exists
  to refuse. The authoritative reader is the ref itself:
  `git log -1 --format=%H task/01a0f52216de7847b1e2d141ff8d66d6`. After the
  amend, `git status --porcelain` printed nothing.

## Evidence (self-filed)

- `gopi scratch-gc families --tree all` after the change (the tree the census
  face now reaches, with the unclassified count that is the gate's live half):
  ```
  tree=sessions  shapes=1475 unclassified=0 files=92110  dirs=155208 bytes=51478781531
  tree=archive   shapes=3    unclassified=0 files=2      dirs=1      bytes=1239612
  tree=backups   shapes=17   unclassified=0 files=18     dirs=3      bytes=844402688
  tree=notify    shapes=4    unclassified=0 files=4      dirs=0      bytes=103839185
  tree=dumps     shapes=2    unclassified=0 files=4345   dirs=0      bytes=94932694
  tree=dumps-1214 shapes=0   unclassified=0 files=0      dirs=0      bytes=0
  trees=6 shapes=1501 unclassified=0 files=96479 dirs=155212 bytes=52523195710
  ```
  Before the change the arm printed the sessions tree alone and named none of the
  other four — the four rows above had no census face at all.
- `gopi scratch-gc trees --dry-run` → the bound reaches the real tree:
  ```
  removed=2270 stale=336h0m0s tree=sibling-dumps bytes=50910561 dry-run=true
  would-remove <profile>/dumps/20260906-184540.dump.json tree=sibling-dumps age=593h14m47s bytes=18095
  removed=0 disabled=false tree=sibling-dumps-1214 bytes=0 dry-run=true
  ```
  and the operator's tree still held 4,345 entries afterwards — the dry run
  deleted nothing.
- **The bound's predicate, measured on a fixture the run owns** (`GOPI_HOME=<fixture>`,
  four entries at the tree root: two aged dumps, an aged-irrelevant `notes.txt`,
  a `keepdir/`):
  ```
  tree=dumps shape=<id>.dump.json       family=error-request-dump decision=bound count=1
  tree=dumps shape=<id>-N.dump.json     family=error-request-dump decision=bound count=1
  tree=dumps shape=keepdir/             family=NONE decision=unclassified count=1
  tree=dumps shape=notes.txt            family=NONE decision=unclassified count=1
  trees=1 shapes=4 unclassified=2 files=3 dirs=1 bytes=0
  ```
  and the same fixture through the sweep planned EXACTLY the two dumps:
  ```
  would-remove <fixture>/dumps/20250101-010101.dump.json    tree=sibling-dumps
  would-remove <fixture>/dumps/20250101-010102-1.dump.json  tree=sibling-dumps
  ```
  `notes.txt` and `keepdir/` were NOT planned. Then all four entries were given
  the SAME mtime (2025-01-01, long past the window) and the walk re-run: still
  exactly the two dumps planned, the two others untouched — AGE cannot be what
  separates them, the NAME is, which is the whole safety story.
- `gopi scratch-gc families --tree nope` → exit 1, stderr:
  `no censused tree is named "nope" — known: sessions, archive, backups, notify, dumps, dumps-1214 (or all)`.
- **The gate was RED for the right reason before it went green** (measured, twice,
  and both were real defects it caught rather than test bookkeeping): the first
  run failed `TestSiblingTreeGateIsRedWhenAFamilyIsDropped` on THREE subtests
  because `backupForeignPrefixes` contained `memory.db.backup`, which SHADOWED the
  two families ordered before it — dropping either row went on matching through the
  catch-all, i.e. the gate would have been decoration. After removing it, the same
  test failed on the sidecar subtest because the snapshot family matched by prefix
  alone and swallowed `…-wal`. Both fixes are in the registry, not the test:
  ```
  --- FAIL: TestSiblingTreeGateIsRedWhenAFamilyIsDropped/the_ledger_snapshot_loses_its_decision
  --- FAIL: TestSiblingTreeGateIsRedWhenAFamilyIsDropped/the_staging_name_loses_its_decision
  --- FAIL: TestSiblingTreeGateIsRedWhenAFamilyIsDropped/the_sidecar_loses_its_decision
  ```
- `gopi doccheck` equivalent — `python3 tools/docclass/classify.py . --check`:
  `0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv`; the new doc's row reads
  `normative ... cited by internal/sessionlayout/sibling_families.go:53`, which is
  the classification the doc is written to earn.
- `git status --porcelain` in this worktree: the modified/added files of this
  change and nothing else — no edit landed in the main repo.
