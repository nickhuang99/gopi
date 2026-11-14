<!-- doc-class: normative -->
# The Sessions Tree's SIBLING TREES — a Range Census with a Decision Each

Status: normative — the standing statement of the profile's sibling-tree
registry, its per-tree decisions, and the `dumps/` discrepancy resolution.
Branch: `feat/sessions-tree-siblings-census`.

Related: `devdoc/sessions-tree-scope-census.md` (the sessions tree's own family
registry), `devdoc/sessions-tree-attachments-bound.md` (the first bound family
inside it), `devdoc/tmp-ban-scratch-root.md` (why no scratch default sits on `/`).

## Why

`internal/sessionlayout` answers the question **"what does the SESSIONS TREE
hold, and what did we decide about each thing in it?"** for exactly one tree,
`<profile>/sessions`. The gopi profile holds four more directories beside it, and
until this change they were measured by a `find` typed into a report:

| tree | measured before (2026-10-01) | writer |
|---|---|---|
| `archive/` | 8 entries / 1.7 GiB | none in this tree |
| `backups/` | 510 entries / 925 MiB | `internal/tools` (`LedgerBackupDirName`) |
| `notify/` | 4 entries / 100 MiB | the operator's `settings.json` `notify.cmd` |
| `dumps/` | 4,321 entries / 102 MiB | *unknown — this is the discrepancy* |

A `find` in a report is an **anchor**: it describes what somebody already
noticed, and it cannot answer a question nobody thought to ask. The `dumps/`
row is that failure made concrete — a directory growing at ~48 files a morning
with no code path in the repository writing it and no report able to say why.
The **range** is the registry: every entry one of these trees yields is
classified against a family with an explicit decision, and an entry no family
claims is REPORTED (Rule 15) rather than falling outside every predicate.

## The `dumps/` discrepancy — resolved

**It is the environment override, not a code path.** The dump writers document
their default correctly:

- `minimalCoreDumpDir()` — `internal/deepseek/minimal_core.go:33` — returns
  `GOPI_ERROR_DUMP_DIR` when it is set, else `scratch.DumpDir(minimalCoreDumpClass)`
  (`"gopi-dumps"`, `minimal_core.go:41`).
- `guardDumpDir()` — `internal/deepseek/error_1214.go:51` — the same shape for
  `GOPI_1214_DUMP_DIR`, else `scratch.DumpDir(guardDumpClass)`
  (`"gopi-1214-dumps"`, `error_1214.go:59`).

Both defaults are the **scratch dumps class tree** (`<scratch home>/dumps/…`,
deliberately off `/`), and both defaults ARE honoured. What writes the profile's
tree is the first-ranked source:

```
/home/user/.bashrc:139  export GOPI_ERROR_DUMP_DIR=/home/user/.gopi/dumps
/home/user/.bashrc:140  export GOPI_1214_DUMP_DIR=/home/user/.gopi/dumps-1214
```

Measured on the live processes (`/proc/<pid>/environ`, three live gopi):
both variables present in every one. A shell-profile export is **durable** — it
outlives the debugging session that added it and is inherited by every child —
which is exactly why the tree kept growing after whatever investigation set it.

**The decision taken, and what was deliberately NOT done.** The knob exists so an
operator can choose where their diagnostics land, so overriding it in code would
be the repository deciding that question. What was missing was not a writer fix
but (a) an honest statement of the precedence where the knob is documented and
(b) a real BOUND on the directory the operator chose — see `CONFIG.md` and the
declared tree below.

## What landed

1. **`internal/sessionlayout/sibling_families.go`** — the sibling registry: the
   five trees (`archive`, `backups`, `notify`, `dumps`, `dumps-1214`), each with
   its families, a `Decision` (`bound:<tree>` / `keep` / `legacy`) and a REASON.
   The profile root arrives through a function seam
   (`RegisterSiblingDataRootResolver`) wired by `internal/config` — this package
   stays a leaf (no import of `internal/config`, no import of `internal/tools`).

2. **Two declared trees in `internal/scratch`** — `sibling-dumps` and
   `sibling-dumps-1214` (`sibling_dumps.go`), swept by the machinery that already
   ticks (`gopi scratch-gc trees`, the host watchdog, the resident worker/merger
   tick). No new script, cron, daemon, or state file (Rule 35). The namespace
   predicate is a dump's own timestamp name (`<stamp>.dump.json`, plus the
   collision form `…-<n>.dump.json`); the window is file-scoped 14 days, like
   the sessions tree's raw logs and for the same reason — a dump is replay
   material for a failure somebody may still be reading.

3. **The census face** — `gopi scratch-gc families` gained a tree selector
   (`--tree NAME`, `--tree all`) and an enumeration (`--list`), and now censuses
   the profile's sibling trees through the SAME printer as the sessions tree.
   The default arm is unchanged (the sessions tree alone): a default silently
   widened to six trees would multiply every existing reader's output.

4. **`CONFIG.md`** — both dump knobs' rows now state the precedence, the durable
   default, and the verb that censuses what they accumulate.

### The decisions, in one table

| tree | family | decision | why |
|---|---|---|---|
| `archive` | `archive-rescue-tarball` | `legacy` | a packed rescue nobody in this tree re-derives; an age window would destroy the only copy |
| `archive` | `archive-export` | `legacy` | a probe's raw export, kept because a reader may still be its only reference |
| `archive` | `archive-rescue-dir` | `legacy` | the unpacked rescue (1.6 GiB of the tree's 1.7 GiB); bounded by nothing, on purpose |
| `backups` | `ledger-backup-snapshot` | `keep` | the writer's own artifact — RESTORE material; retention belongs to the memory subsystem, not here |
| `backups` | `ledger-backup-staging` | `keep` | the `.partial` name of a snapshot being written; deleting one mid-write destroys the run producing it |
| `backups` | `ledger-backup-sidecar` | `keep` | SQLite's `-wal`/`-shm`; removed WITH their artifact, never on their own age |
| `backups` | `ledger-backup-foreign` | `keep` | hand-made snapshots (15 of the tree's 21 top-level entries), each the only copy of a prior ledger state |
| `backups` | `worktree-residue-archive` | `legacy` | `wt-release-<date>/`, `wt-sweep-<date>/` from release scripts that no longer exist in this repo (119 MiB) |
| `notify` | `notify-event-stream` | `keep` | the operator's own feed; the WRITER is their configuration, so the retention contract is theirs |
| `dumps` | `error-request-dump` | `bound:sibling-dumps` | replay material, never read back by the writer; bounded by name + age |
| `dumps-1214` | `error-request-dump` | `bound:sibling-dumps-1214` | the second knob's target; EMPTY on this host, and registered anyway — the registry describes the range, not today's file count |

## The measurement, after

`gopi scratch-gc families --tree all` and `ls -A` / `du -sh` per tree (2026-10-01,
this host; the file count for `dumps/` moved from 4,321 to 4,345 during the
session — it grows while you measure it, which is the point of the census):

```
tree=sessions  shapes=1475 unclassified=0 files=92110  dirs=155208 bytes=51478781531
tree=archive   shapes=3    unclassified=0 files=2      dirs=1      bytes=1239612
tree=backups   shapes=17   unclassified=0 files=18     dirs=3      bytes=844402688
tree=notify    shapes=4    unclassified=0 files=4      dirs=0      bytes=103839185
tree=dumps     shapes=2    unclassified=0 files=4345   dirs=0      bytes=94932694
tree=dumps-1214 shapes=0   unclassified=0 files=0      dirs=0      bytes=0
trees=6 shapes=1501 unclassified=0 files=96479 dirs=155212 bytes=52523195710
```

Two things this measurement makes visible that the `find` could not.

**The census is ONE LEVEL, and the "before" column above was `du`.** `archive`
reports 2 files + 1 directory while its `du` is 1.7 GiB, because 1.6 GiB of it is
inside `workspace-rescue-20260920/`, whose contents are that rescue's business
and not this registry's (descending would be a census of the operator's rescue
data). Both numbers are true; they answer different questions, and this doc
states which is which rather than letting a reader subtract them and conclude a
loss.

**`dumps/` is bounded in principle but not yet by age.** The four lines a reader
should check are one dry run away:

```console
$ gopi scratch-gc trees --dry-run | grep sibling-dumps
scratch-gc role=cli-trees ... tree=sibling-dumps stale=336h0m0s disabled=false
would-remove <profile>/dumps/20250819-…  tree=sibling-dumps age=15322h…
```

An aged dump is a candidate; a `.txt` note or a directory in the same tree is
NOT, whatever its age — the predicate is the name, and that is the safety story.
Measured on the gate's fixture, whose four entries were given the SAME date
(2025-01-01 — all four long past the 14-day window) so that AGE cannot be what
separates them:

```
$ ls -la --time-style=+%F <fixture>/.gopi/dumps
-rw-rw-r-- 1 the operator the operator    0 2025-01-01 20250101-010101.dump.json
-rw-rw-r-- 1 the operator the operator    0 2025-01-01 20250101-010102-1.dump.json
drwxrwxr-x 2 the operator the operator 4096 2025-01-01 keepdir
-rw-rw-r-- 1 the operator the operator    0 2025-01-01 notes.txt

$ gopi scratch-gc trees --dry-run
removed=2 stale=336h0m0s tree=sibling-dumps dry-run=true
would-remove <fixture>/dumps/20250101-010101.dump.json    tree=sibling-dumps age=15323h4m
would-remove <fixture>/dumps/20250101-010102-1.dump.json  tree=sibling-dumps age=15323h4m
```

Two planned, two left alone, at identical age and identical size. What separated
them is the NAME — which is exactly the claim the bound makes, and the difference
between a bound and a delete button.

## The gates

| gate | where | what it refuses |
|---|---|---|
| `TestSiblingTreeFamiliesAreClassified` | `internal/scratch/sibling_tree_families_test.go` | an entry in a real sibling tree that no family decides; a `bound:` family naming a tree `DeclaredTrees()` does not hold |
| `TestSiblingTreeGateIsRedWhenAFamilyIsDropped` | same | the RED-PROOF: dropping ANY family in the fixture must report its entries UNCLASSIFIED — per tree, one case each, plus a control run |
| `TestSiblingTreeGateIsRedForANewFamily` | same | a new directory/file shape landing in a sibling tree in silence |
| `TestSiblingSiblingNamesMatchTheirWriters` | same | drift between the registry's spellings and the writers' (`LedgerBackupDirName`, `dumpFileSuffix`, both env knobs, both documented defaults, and the two `RegisterSiblingDataRootResolver` wiring lines) |
| `TestProfileSiblingTreeResolversAreWired` | `internal/config/sibling_tree_resolvers_test.go` | the seam falling back to its unwired sentinel — which would render every sibling tree "not configured on this host", a clean-looking answer for a tree sitting right there |
| `TestProfileSiblingTreesAreDistinctDirectories` | same | the copy-paste slip that would point both dump trees at one directory |
| `gopi scratch-gc families --list` / `--tree NAME` | `cmd/agent/cli_tools_maintenance_help_surface_cases_test.go` | an advertised form nobody typed (Rule 31); the inventory is regenerated in the same commit |

The live trees are REPORT-ONLY in the gate — they are the operator's state, not
this repository's contract — and the live corroboration asserts only that each
walk RAN and read something: a walk that read nothing must never be mistaken for
a clean tree.

## Not done

- **No retention decision was invented for `backups/` or `notify/`.** Both are
  `keep:` with a reason, not `bound:`. `backups/` is restore material
  (disaster-recovery contract, not a sweeper's call) and `notify/` is written by
  the OPERATOR's own `notify.cmd`, so the retention contract is theirs. If either
  should be bounded, that is a decision to take with the operator in hand — the
  registry now has the row to record it in.
- **The `archive/` rescues are not swept**, and 1.6 GiB of the tree's 1.7 GiB is
  one such directory. Deliberate: they are the only copy of what they hold, and a
  bound without a restore story is a delete button.
- **No collection was RUN against the operator's `dumps/` tree** — the dry run is
  the proof, and the deletion is the operator's to take (or to let the watchdog
  take on its own tick). Measured in the same run: the `sibling-dumps` row planned
  **2,270 files / 50,910,561 bytes (48.6 MiB of the tree's 102 MiB)**, the oldest
  at 593h — and `sibling-dumps-1214` planned 0, because that tree is empty. So the
  bound has real candidates on this host, not an empty plan.
- **That 48.6 MiB is 2,270 of the tree's 4,345 files, i.e. the rest is inside the
  window.** The window was chosen at 14 days on purpose (a dump is replay material
  while a failure is being diagnosed); the operator can shorten it with
  `gopi scratch-gc trees --stale-min N` or lengthen it in the row's
  `SiblingDumpsStaleAge`. Widening or narrowing it is a decision about THEIR
  diagnostics, which is why the row carries its reason in code rather than a
  number somebody tuned once.
