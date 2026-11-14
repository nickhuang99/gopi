<!-- doc-class: normative -->
# The Sessions Tree's SCOPE: a Family Census, and the Range Gate Over It

**Task**: `01a0f4f9-baf5-7f13-a707-d05a68ea12e4` — the RANGE half of the
sessions-tree coverage gate; §2c and the registry row it describes landed under
`01a0f522-152b-7921-890e-25e03f0276e3`.
**Measured**: 2026-10-01, on this host. The COUNTS were read at `55c9bdb1`,
which was this branch's tip when the census was last re-run; the DECISION figures
(the family table, §2c) were produced by the code this commit adds. Every figure
below is printed by a NAMED generator (`gopi scratch-gc families`, `find`) at a
named rev (Rule 38).

> The pin was previously written as `6f2a7d1c`, which **does not resolve as a
> commit** — a Rule 38 pin nobody can check is a rumour with a hash attached.
> Corrected here to a rev that does.
>
> **Which figures move, and which do not.** The COUNTS move: the tree grows as
> runs land (measured across three re-runs while §2c was written: files 92,077 →
> 92,200 → 92,201; shapes 1,448 → 1,473). The DECISION figures do not — the
> per-container rule §2c describes is re-derived from the ledger, not from the
> census, and the numbers in its table (430 directories / 250 containers / 399
> KEEP / 31 candidates / 22 record-less attachment containers) were each
> re-measured at that rev and are what the test fixtures encode.

---

## 1. The defect: an ANCHOR gate cannot ask a RANGE question

`internal/scratch/declared_trees.go` declares the trees this repository creates
and never cleans, and `TestDeclaredTreesCoverRepoWriters` checks that the trees
named by a HAND-WRITTEN list of eight writers are declared. That check is
excellent at what it does — it refuses a writer whose tree is undeclared — and it
is structurally blind to everything nobody wrote a row for.

Measured on the live sessions tree (`gopi scratch-gc families`, 2026-10-01):

| quantity | measured |
|---|---|
| entries walked (files + directories, two levels) | 248,004 |
| distinct SHAPES | 1,448 |
| files | 92,077 |
| directories | 155,927 |
| bytes | 52,221,907,122 (48.6 GiB) |
| entries **no family claimed** before this task | 202,534 entries (files) / 9.3 GiB — the registry's reason for existing |
| the ONE family the declared registry bounded | `*.raw.log` — 1,408 entries / 45.8 GiB |

So the declared tree covered the LARGEST family by bytes and left 202,534
entries — the attachments archive (75,845 directories), the metadata records
(79,148), per-run working directories, a probe's whole checkout — named by
nothing and bounded by nothing. An anchor list answers *"is every tree we know
about covered?"*; it can never answer *"is there a tree we do not know about?"*.
That is a question about the **RANGE**, and only a walk can ask it.

## 2. The SCAN BOUNDARY (what the gate looks at, and what it deliberately does not)

The sessions tree is `~/.gopi/sessions/<container>/<entry>`:

* **the ROOT** (`<container>`), classified at `WhereRoot`;
* **the CONTAINER level** (`<container>/<entry>`), classified at `WhereContainer`;
* **deeper levels are NOT walked.** A container is the ledger's own directory:
  its subtree belongs to the container family, or to the probe that made it. A
  walk that descended would follow a probe's `smoke-clone` into a full
  checkout — turning a two-level census into a whole-repository walk.

The two stanzas immediately below are the boundary made explicit, because a
boundary nobody wrote down is the same defect one level up.

### 2a. The SIBLING trees (adjacent to `sessions/`, OUT of this gate's scope)

Rule 22's scratch accounting covers the scratch tree; these four sit beside
`sessions/` under the same profile and are NOT walked by this gate. They are
recorded here so that "not covered" is a MEASURED statement:

| sibling | measured (2026-10-01, `find`/`du`) | what writes it | disposition here |
|---|---|---|---|
| `archive/` | 8 files / 1.7 GiB | no writer in this tree — three hand-made rescue artifacts (`legacy-t-rows-export.tsv`, `workspace-rescue-20260920/`, a sandbox tgz) | **child task** (§6) |
| `backups/` | 510 files / 925 MiB | `internal/tools/ledger_backup.go` (`LedgerBackupDirName = "backups"`) | **child task** (§6) |
| `notify/` | 4 files / 100 MiB | `notify.cmd` from settings.json (a 51 MB / 135,167-event `task-events.jsonl`) — an operator-configured sink, not a gopi writer | **child task** (§6) |
| `dumps/` | 4,321 files / 102 MiB | `internal/deepseek`'s dump writer, whose STATED default is now the scratch dumps tree (`scratch.DumpDir`, devdoc/tmp-ban-scratch-root.md) — yet files landed here TODAY (2026-10-01 07:36, 24 files) | **child task** (§6) |

The `dumps/` row is the one with a live discrepancy worth naming: the code's
comment says the dumps go to the scratch home, the tree grew 24 files today.
That is a defect in a DIFFERENT layer (the dump writer's directory), it is not
this branch's change, and it is dispatched rather than patched here (Rule 26).

### 2b. The container-entry families NOT bounded here, and why

Declaring a family is not bounding it. The families below are registered with a
decision that deliberately does NOT age anything out, each for a reason a reader
can act on (the `attachments-archive` row used to be one of them and is not any
more — see the third bullet):

* **`attachments-archive`** (75,845 directories) — **`bound:session-attachments`**
  (moved from `keep` on 2026-10-02 by the sibling task
  `01a0f522-11d4-7ddc-996e-0c8666dced9b`, which landed on master while this
  branch was open): the part that provably holds no unique evidence is an
  ORDINARY, EMPTY, unheld archive — measured, 75,602 of 75,851 archives are
  empty. The archives that HOLD content stay KEPT whatever their age, because a
  deleted branch's only surviving copy is an attached bundle
  (`01a0f3de-…/attachments/…/branch-capture-list-windows.bundle` is the only copy
  of archived `feat/capture-self-window`). This document's own `keep` verdict
  above is superseded on that point; the census table below carries the current
  decision.
* **`root-stray-dir` / `task-container` / `sessions-root-residue` / `sandbox-profile`**
  — **legacy** (a measured "no writer in this tree"). Naming them makes them
  visible; bounding them is a separate decision. **Child task** (§6).
* **`session-scratch-dir`** (277 directories / 165 shapes at the time of this
  census) — **`bound:session-raw`**, decided by the CONTAINER's
  lifecycle rather than by a name: no name predicate can enumerate 165
  run-chosen shapes, so the question is asked one level up — a container the
  ledger still describes (`metadata.json`, or a resumable `*.jsonl`) keeps every
  directory inside it forever, and only a container the ledger has forgotten
  (no record of any kind, or one the `session-raw` arm is itself already
  collecting) lets its aged directories go. See §2c.

### 2c. `session-scratch-dir`: bounded by the CONTAINER's lifecycle (this change)

The census above left this family `keep`, with the honest reason "the name is the
run's own choice; no predicate tells a dead one from a live one". **That verdict
was about the NAME, and it was right about it** — the family's measured 165
shapes (`evidence`, `evid`, `ev`, `bin`, `artifacts`, `crops`, `figs`,
`__pycache__`, `smoke-clone`, `master-check`, `probeN`, `acceptN`, …) are chosen
by each run, so no namespace predicate can enumerate them, and the declared-tree
registry is nothing BUT namespace predicates.

The way out is to stop asking about the name and ask about the **container**,
because that is the unit whose life the ledger actually knows:

* `internal/tools` mints one directory per run under the sessions root, and the
  session readers resolve a task id *through* the `metadata.json` inside it;
  `gopi task show` prints a container's artifacts through it; a record may be
  RESUMED from it.
* So the question "is anything in here still needed?" has an answer one level
  up: **the ledger either still describes the container or it does not.**

The predicate is ONE function, `containerHasNoLedger` (`internal/scratch/
sessions_scratch_dir.go`), reading the container's entries once:

| container holds | verdict | measured on this host (2026-10-01) |
|---|---|---|
| a `metadata.json` **or** any `*.jsonl` record | **KEEP** every directory inside it, at any age | 225 containers hold the 399 measured directories of the 430 |
| no record of any kind **and** untouched for the window | **COLLECT** its aged directories | 31 directories, in 25 containers |
| touched inside the window (a container minted before its record is written) | **KEEP** — that window is what separates a run in flight from one the ledger forgot | every one of the 25 no-record containers on this host is younger than the window (the oldest of their 31 directories: 7.97d mtime) |
| a live process inside it, a `.holder` registration, or a symlink | **KEEP** — the EXISTING liveness verdicts (`heldSet` / `unheldRegistered`) + this arm's own symlink refusal (a link is removed WITH its target) | — |
| it is a directory the registry claims for ANOTHER family (`attachments-archive`, `foreign-checkout`, `sandbox-profile`) | **KEEP** — asked through `sessionlayout.ClassifyFamily`, so a name list here cannot drift from the registry | 75,845 `attachments/` directories |

The window is `SessionScratchDirStaleAge` = 30 days, the LONGEST in
`declared_trees.go`, and deliberately so: it is the *floor under the floor*. The
oldest such directory on this host is 14.8 days old, so a live container is long
since archived or removed before its dead sibling ages in — and a month leaves
time for a hand-made evidence directory to still be read by a human.

**The window is the ARM's, and that took a second correction.** `containerEntries`
hands its hook the same `age` it judges the row's own candidates by, and for
`session-raw` that is the `*.raw.log` FILE arm's 14 days. The first version used
that value directly, which made `SessionScratchDirStaleAge` a constant nothing
read while this doc and the registry row both claimed it decides — and made a
15-day-old evidence directory in a forgotten container collectable, a deletion
the documented decision does not authorise. `scratchDirWindow` now floors the
caller's value at the arm's own (`TestSessionsScratchDirWindowFloorIsTheArmsOwn`
is the regression guard, red against that first shape): a caller asking for a
LONGER window is honoured, a shorter one cannot shorten this arm.

**Whose family is a directory is the REGISTRY's answer, not a name list here.**
Three other families live inside a container and each carries its own decision —
`attachments-archive` (KEEP: a deleted branch's only surviving copy is an
attached bundle), `foreign-checkout` (legacy: a probe's whole `git clone`),
`sandbox-profile` (legacy: a materialised `.gopi`) — so the hook asks
`sessionlayout.ClassifyFamily` and refuses anything the registry does not answer
for as `session-scratch-dir`. A name list written here instead would drift the
moment a family is added; asked this way, a NEW container family is respected
without this file being touched.

**The arm is EXTENDED, not duplicated.** The `session-raw` row already walks this
tree one level down and already refuses to remove a container that is not empty of
its own candidates (`containerEntries`' `keptFiles`); what it could not do was
judge a NESTED DIRECTORY — it counted one and kept the container forever. The
change adds one hook where the arm already stands (`containerScratchDirEntries`,
`internal/scratch/sessions_scratch_dir.go`), so there is one tree, one walk, one
plan/sweep pair and one answer to "what is in this container" — no second row over
the same root, no second schedule, no second window to keep in step.

**The sharp edge, and how it was closed.** The container arm's rule is about the
*container's ledger*; the first version therefore treated every directory in a
no-record container as its own candidate. That was safe only by measurement (of
the 75,917 containers holding an `attachments/` archive, 22 lack a `metadata.json`
— all seconds old), i.e. it was safe by a fact that could change tomorrow. The
hook now asks the registry's own classifier first, so an `attachments/` archive,
a probe's checkout and a sandbox profile are routed to their own families at ANY
age and in ANY container — the assumption is no longer load-bearing. The fixture
keeps the shape (an archive and a `bin/` in the same no-record container) so the
routing is asserted rather than assumed.



ONE table, `internal/sessionlayout/families.go`, in the leaf package that already
owns the sessions tree's SHAPE (so `internal/scratch` can import it without a
cycle). Each row carries: a **name**, a **row class**, the **depth** it lives at,
a **decision**, the **tree** it is bound to (only for `bound:`), a **reason**, and
a **membership test**. Four decision values, no fifth:

| decision | meaning |
|---|---|
| `bound:<tree>` | a declared tree in `DeclaredTrees()` collects this family — and the gate requires that tree to EXIST |
| `keep:<reason>` | must NOT be aged out; the reason is the contract that keeps it |
| `legacy` | no writer in this tree (measured) — visible on purpose, bound by a separate decision |

`ClassifyFamily` answers for ANY entry, and returns `ok=false` for one no family
claims. **That `false` is the whole point**: the registry's job is to make a new
family a GATE-VISIBLE OMISSION rather than silence (Rule 15).

The measured census, by family (`gopi scratch-gc families`, 2026-10-01):

| family | decision | entries | shapes |
|---|---|---|---|
| `session-container` | `bound:session-raw` (the empty-container arm only) | 79,342 | 11 |
| `session-metadata` | keep | 79,148 | 1 |
| `attachments-archive` | **`bound:session-attachments`** (empty archives only; landed on master 2026-10-02) | 75,845 | 1 |
| `session-record` | keep | 4,610 | 2 |
| `run-evidence` | keep | 4,395 | 1,205 |
| `session-crash-dump` | keep | 1,773 | 1 |
| `raw-http-log` | **bound:session-raw** (the family itself) | 1,408 | 1 |
| `sessions-root-residue` | legacy | 500 | 15 |
| `task-container` | legacy | 296 | 11 |
| `session-scratch-dir` | **bound:session-raw** (§2c) | 277 | 165 |
| `session-side-record` | keep | 243 | 1 |
| `root-stray-dir` | legacy | 129 | 10 |
| `sandbox-profile` | legacy | 35 | 21 |
| `foreign-checkout` | legacy | 3 | 3 |
| **unclassified** | — | **0** | 0 |

The three `legacy` container families are small in COUNT and large in BYTES: one
`foreign-checkout` container is a whole `git clone` of this repository (~200 MB
each, `internal/smoketest`'s `smoke-clone` shape).

## 4. The gate, and its red-proof

`internal/scratch/sessions_tree_families_test.go`:

* **`TestSessionsTreeFamiliesAreClassified`** — the range gate: walks a fixture
  tree holding ONE entry per measured family, refuses any entry the registry does
  not classify, and cross-checks every `bound:` family against the LIVE
  `DeclaredTrees()` set (a `bound:` naming a tree that does not exist bounds
  nothing; and the tree it names must ROOT at the sessions tree).
* **`TestSessionsTreeGateIsRedWhenAFamilyIsDropped`** — the red-proof: the SAME
  scan and classifier, driven with a ROLE-REDUCED registry, must report the
  dropped family's entries as UNCLASSIFIED. Seven cases, `attachments` first (the
  family that grew unmeasured). A green gate that cannot be shown red is not a
  gate.
* **`TestSessionsTreeGateIsRedForANewFamily`** — the regression the epic asks
  for: a NEW directory and a NEW file shape landed at the root must make the gate
  red, and the failure line must NAME the shape (a report naming no path cannot
  be acted on).
* **`TestSessionsTreeGateOnTheLiveTree`** — the live tree walked, REPORT-ONLY:
  its unclassified shapes are logged, never asserted. The live tree is the
  operator's state; the fixture carries the contract. What IS asserted is that
  the walk read a non-zero number of entries — a walk that read nothing must
  never read as a clean tree.

**The `bound:` cross-check needed one value to have ONE home.** The declared tree
names existed only as literals inside `DeclaredTrees()` (and the session tree's
row spelled `"session-raw"` inline). They are now the exported constants
`TreeNameGoTmp` / `TreeNameRaw` / `TreeNameSessionRaw` (`internal/scratch/
declared_trees.go`) and `sessionlayout.TreeSessionRaw`, referenced everywhere —
so "declared but not swept" and "bound to a tree that does not exist" are both
refusals, and no third spelling can disagree with the other two (Rules 13/14).

## 5. The census face

`gopi scratch-gc families` — the same VERB as the bound half (`trees`), because
they answer one question about one tree and a second verb would be a second
schedule, a second help surface and a second place for the answer to drift
(Rule 3). It walks the sessions tree, prints one logfmt line per SHAPE with the
family, the decision and the entry count, a summary line, and one stderr line per
UNCLASSIFIED shape naming the path that has to be registered.

It is READ-ONLY by construction: the scan never opens a file, never writes, and
is NOT gated by the sweep's off switch (an operator who disabled the sweeps still
needs to see what the tree holds). Exit 1 when the tree could not be read — a
census that could not look must never read as a clean tree.

```
$ gopi scratch-gc families | tail -2
scratch-gc role=cli-families home=/home/user/.gopi/sessions removed=1 stale=none disabled=false shape=zzred_self_chain_test.go.bak family=run-evidence decision=keep dir=false
scratch-gc role=cli-families root=/home/user/.gopi/sessions shapes=1448 unclassified=0 files=92077 dirs=155927 bytes=52221907122
```

## 6. Not done here — the bounds, dispatched as their own tasks

The epic's RANGE half is what this branch lands: the census, the registry, the
gate, the face. The BOUNDS are separate decisions, each with its own risk, and
each is submitted as a child task with this document's measurements as its
briefing (Rule 26 — an incomplete fix is repaired by dispatching again, and
overlap is handled by the briefing):

1. ~~**attachments reclaim**~~ — **LANDED on master** (`dbdfb3b1`, task
   `01a0f522-11d4-7ddc-996e-0c8666dced9b`): an ORDINARY, EMPTY, unheld archive
   directory is collectable past `SessionAttachmentsStaleAge`; an archive holding
   content is kept whatever its age. The item is struck because the tree it
   would bound is now bounded — the work itself is not this branch's.
2. **legacy residue bound** — the four `legacy` families, each needing a
   verdict ("no writer, safe to age" vs "hand-made, keep") rather than an age
   window. → **`01a0f522-134a-75ae-9865-54d9756c26d4`**
3. ~~**session-scratch-dir bound**~~ — **LANDED**: `containerHasNoLedger` +
   `SessionScratchDirStaleAge` (`internal/scratch/sessions_scratch_dir.go`,
   `declared_trees.go`), reached through the `session-raw` row's container arm
   (§2c). The registry row moved `keep` → `bound:session-raw` in the same commit
   (the tree name is the one the arm belongs to — `session-raw`, already named by
   `session-container` and `raw-http-log`).
4. **sibling trees** — `archive/` (1.7 G), `backups/` (925 M), `notify/`
   (100 M), `dumps/` (102 M), including the `dumps/` discrepancy (§2a) between
   the stated default and the files landing today.
   → **`01a0f522-16de-7847-b1e2-d141ff8d66d6`**

Each row is self-contained (context, the measured figures, the constraints in
force, the acceptance) so the executor does not need this document open — and
each names its siblings, because these four touch one area and Rule 26's overlap
handling is a BRIEFING duty.

## 7. Rule 35 (zero dependency) — what this change does NOT add

No new script, no cron line, no daemon, no state file, no lock, no new config
key, no new database. The change is: one Go package (the registry), one test
file (the gate), one arm on an EXISTING verb (`gopi scratch-gc families`), and
three constants that replace literals that already existed. The reclaim paths in
§6 are explicitly NOT created here — deliberately, because creating a reclaim
path without an owner would be the dependency this rule bans.
