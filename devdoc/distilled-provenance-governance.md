<!-- doc-class: historical -->
# Task Report: distilled.db Provenance Governance (Rule 16)

**Branch:** `feat/distilled-provenance-governance` (clone `~/workspace/gopi-prov-clone`, base `17002c10`)
**Date:** 2026-09-11
**Rule under test:** Rule 16 — Lifecycle Storage Completeness
(`devdoc/PRINCIPLE-lifecycle-storage-completeness.md`)

---

## 1. The premise, re-grounded (the briefing was pre-investigation)

The epic briefing said *"1426 evidence rows with no path"*. Measurement over the
live production DB (`~/.gopi/memory.db`) contradicts that framing:

| Claim in the briefing | Measured reality |
|---|---|
| 1426 rows have no path **in `evidence`** | `evidence` is **EMPTY for ALL 1831 rows** (then-current) — it carries no provenance whatsoever |
| the defect is in `evidence` | there is nothing there to fix; the defect lives in **`sources`** |
| 1831 rows | 1831 at the start of the write, 1839 by the end (concurrent agent sessions keep inserting) |

So the real defect, measured:

> **`sources` is free text.** Of the rows that existed at write time, only ~570
> carried an **addressable** value (a filesystem path). The rest carried either
> nothing (500) or a human descriptor that resolves to no artifact (760).

Rule 16's judgement is exactly this: a row whose origin is `"git_blame tool"` is
*stored* but not *addressable* — a downstream task cannot resolve it back to the
artifact that produced it, so it must re-derive what the row already knew. That
is the DFA-degeneration Rule 16 names.

**Note on the `evidence` column:** it is now *known* empty (1831/1831). It is a
dead column, recorded here rather than silently ignored — but it is out of this
task's scope (a schema cleanup, not a provenance backfill).

---

## 2. Shape distribution (deliverable 1) — re-runnable, sums to the population

Produced by the classifier itself, not by hand, so the number and the code
cannot drift:

```bash
GOPI_MEMORY_DB=~/.gopi/memory.db gopi memory provenance --shape
```

**BEFORE the backfill** (measured 2026-09-11, `sources` value distribution over
the then-1831 rows):

| source_type | count | what it means |
|---|---:|---|
| `import` | 633 | bulk import — the distilled.db / hermes-skills migrations (`导入` 524, `import` 109) |
| `local_path` | 571 | names a filesystem artifact — a downstream task opens it |
| `unknown` | 500 | `sources` is empty — **no origin recorded at all** |
| `descriptor` | 65 | a human label naming no addressable artifact (`git_blame tool` ×49, `smoke-test`, `manual`, …) |
| `session` | 39 | an agent-session label |
| `epic` | 12 | an epic-goal label |
| `node` | 10 | arrived from a peer node via memory sync (`node:<alias>`) |
| `task` | 5 | a scheduled-task origin |
| `url` | 1 | a hyperlink |
| **total** | **1835** | the whole table (1831 + 4 rows a concurrent session inserted mid-run) |

**AFTER the backfill** (1839 rows — 3 more concurrent inserts arrived, which is
itself the operational finding in §7):

| source_type | count |
|---|---:|
| `import` | 633 |
| `local_path` | 565 |
| `unknown` | 500 |
| `descriptor` | 65 |
| `session` | 39 |
| `epic` | 12 |
| `node` | 10 |
| `git_ref` | 6 |
| `task` | 5 |
| `url` | 1 |
| *(empty — see §7)* | 3 |

Sum = 1839 = the live row count. `local_path` dropped 571 → 565 because 6 rows
were refined into `git_ref` (see §5).

---

## 3. Taxonomy (deliverable 2)

| type | meaning | addressable under Rule 16? | confidence semantics |
|---|---|---|---|
| `local_path` | `sources` names a filesystem artifact | **yes** | open it directly |
| `url` | `sources` is a hyperlink | **yes** | fetch it |
| `git_ref` | `sources` is a git reference (a branch like `feat/x`) | **yes** | resolve with git, not a reader |
| `node` | arrived from peer node `<alias>` via sync | **yes** | the origin host is named |
| `task` | origin is a scheduled task; the id resolves in the ledger | **yes** | query the ledger |
| `epic` | origin is an epic goal (text lives in the ledger) | **yes** | query the ledger |
| `import` | bulk migration — the **channel** is named, the upstream row is not | no | channel known, row not |
| `session` | an agent-session *label* (no id) | no | a label, not a handle |
| `descriptor` | a human label naming no artifact | no | nothing resolvable — **the free-text era's honest residual** |
| `unknown` | `sources` is empty | no | **no origin recorded — never fabricated** |

The ninth bucket `unknown` is a real, measured bucket (500 rows), not a
catch-all: it is the only truthful label for a row that recorded no origin.
`descriptor` is deliberately kept **distinct** from `unknown` — an absent label
and an unresolvable label are different kinds of untraceable, and lumping them
would hide that.

### The classification ladder (data-driven, measured — not guessed)

Order is load-bearing, and every rung is chosen from an **observed false
positive**, not from taste:

| # | rule | → type | why this position |
|---|---|---|---|
| 1 | empty / whitespace | `unknown` | nothing to classify |
| 2 | `://` | `url` | a URL **contains slashes** — must precede the path rule or it misfiles |
| 3 | branch prefix + `/` + **no** file marker | `git_ref` | a **refinement** of rung 4 (see §5) |
| 4 | `/` | `local_path` | the measured signature of an addressable artifact |
| 5 | `导入` \| `import` | `import` | two measured import families (CJK + ASCII) |
| 6 | `node:` | `node` | the sync layer's provenance stamp |
| 7 | `session` | `session` | 39 live rows |
| 8 | `task…` \| `task t-` | `task` | prefix **and** mid-string forms |
| 9 | `epic` | `epic` | 12 live rows |
| 10 | (residual) | `descriptor` | non-empty text naming nothing |

Rungs 1–10 **partition** the population: every row gets exactly one type — this
is asserted by `TestProvenanceLadderIsTotal`, not assumed.

**Two measured false positives killed the naive heuristics** (this is why the
rules are what they are):

* `.md` as a "path" signature also matches **free prose** —
  `epic: read GOPI.md + git status of gopi-glm-fix worktree` is not a path.
  A slash is the only cheap signature that prose never carries here.
* A **URL** contains slashes, so the path rule must sit *below* the URL rule.

---

## 4. The backfill (deliverable 3) — additive, transactional, idempotent

```bash
gopi memory provenance --shape        # read-only distribution (default)
gopi memory provenance --backfill     # fill rows whose source_type is empty
gopi memory provenance --reclassify   # re-derive EVERY row from its `sources`
gopi memory provenance --counts       # the verification query
gopi memory provenance --latent       # recoverable vs truly-lost (§6)
```

Implementation: `internal/tools/memory_provenance.go` (+ `_test.go`), CLI surface
in `cmd/agent/cli_memory.go`.

**Contract:**

* **Additive** — the single `UPDATE` touches `source_type` only. `sources`,
  `topic`, `question`, `answer`, `search_kw` are never written. Proven by
  snapshotting all five columns before/after and diffing byte-for-byte
  (`TestProvenanceBackfillIsAdditiveAndIdempotent`), and again against the
  physical backup (§8).
* **Idempotent** — the `WHERE` anchors on an empty `source_type`, so an
  already-classified row is not even re-written. A second run reports `filled=0`.
* **Transactional** — one `BEGIN IMMEDIATE`; a crash mid-pass leaves the table
  unchanged rather than half-classified.
* **Non-destructive** — never deletes, never rewrites `sources`. The
  classification is a **pure function of `sources`**, so a wrong label is always
  repairable via `--reclassify` without data loss.
* **No magic numbers (Rule 1)** — the column name, all ten bucket names and all
  markers are named constants; the SQL `CASE` is **rendered from the ladder
  slice**, so the distribution query, the `UPDATE` and the tests cannot drift.

### Live run evidence

```
run 1  scanned=1835 filled=1835 unchanged=0    unknown=500
run 2  scanned=1835 filled=0    unchanged=1835 unknown=500   ← idempotent
run 3  scanned=1835 filled=0    unchanged=1835 unknown=500   ← idempotent (again)
```

---

## 5. Two refinements the live data forced

**`git_ref`.** 6 rows carried a bare branch (`feat/resume-arm-trim`) which the
slash rule filed as `local_path`. A branch is resolved with *git*, a path is
opened with a *reader* — different resolution, so a different type. The rung is
a **refinement of `local_path`, not a competing rule**: its `NOT` guard over the
file-shaped markers (`.md .go .py .rs .sh .json .txt internal/ cmd/ devdoc/
scripts/ docs/ src/`) means it can only fire where `local_path` would otherwise
claim a bare ref. Measured: the 64 other slash-bearing non-path rows **do** name
a real file (`feat/memroadmap-m1 devdoc/memroadmap-m1.md`) and correctly stay
`local_path`.

*Rejected alternative, honestly recorded:* a broader "branch-ish" rule (no `.`,
no `~`, no `/…/`) would have swept in 46 more rows — but 8 of them carry a task
id, and the guard would have misfiled genuinely addressable rows. The narrow,
pre-guarded rule was chosen because it is provably a refinement.

**`--latent`** — see §6; it is the measurement that keeps `unknown` honest.

---

## 6. How many rows are honestly untraceable (deliverable 4 + 6)

`unknown` = 500. But **`unknown` must not be over-claimed as "the origin is
lost"**, and measuring says it isn't:

```bash
gopi memory provenance --latent
→ unknown rows: 500  latent (a path in the row's own text): 495  no path anywhere: 5
```

**495 of the 500 `unknown` rows name a file in their own topic** — row 31's
topic is `hermes_cli/plugins.py — plugin manager & …` while its `sources` is
empty. Their provenance is **latent**: recoverable by a future pass that
harvests the topic/question text, without redoing the work. The measurement uses
the **same signature the `local_path` rung uses** (a slash), so a looser probe
cannot inflate the "recoverable" number.

**Only 5 rows carry no artifact reference anywhere in their own text.** Those 5
are the strong-sense untraceable remainder.

So the honest statement of this task's outcome:

| population | count | status |
|---|---:|---|
| addressable (`local_path`/`url`/`git_ref`/`node`/`task`/`epic`) | **599** | resolvable downstream |
| recoverable-in-principle (`unknown` + 495 latent) | **495** | provenance present, just never copied into `sources` |
| *genuinely* unaddressed (`import`/`descriptor`/`session`/`unknown`-with-no-path) | **742** | see below |
| of that 742, truly nothing anywhere | **5** | the honest floor |

The 742 breaks down as 633 `import` (the channel is known, the upstream row
isn't — a bulk migration's inherent limit), 65 `descriptor` (a writer who knew
the origin wrote a label instead of an address), 39 `session` (a label without
an id), 5 `unknown` with no path anywhere.

**Why 742 is acceptable as an honest unknown:** none of it can be improved by
guessing. `unknown` is exactly "no origin recorded" and fabricating a source to
reach 100% fill would be the fraud the acceptance criteria forbid. The
*actionable* part is the 495 latent rows, and that is filed as a follow-up
(§9) rather than silently folded into the "done" number.

---

## 7. 10-row random sample — row-by-row cross-check (deliverable 5)

`SELECT … ORDER BY random() LIMIT 10` over the live table, each row's own text
read against its assigned label:

| id | `sources` | assigned | verdict |
|---:|---|---|---|
| 119 | `distilled.db导入` | `import` | ✅ CJK import marker, verbatim |
| 288 | `distilled.db导入-Ollama评估` | `import` | ✅ same family |
| 395 | `distilled.db导入-Ollama评估` | `import` | ✅ |
| 631 | `distilled.db导入-Ollama评估` | `import` | ✅ |
| 1082 | `~/work/GoPi feat commit 395e2a0 devdoc/chat-simplified-path.md` | `local_path` | ✅ names two real files + a commit |
| 1130 | `feat/chat-remote-dispatch 4721052+56adcfb+49a60ba, t-1788060307, 2026-08-30` | `local_path` | ⚠️ **defensible but imprecise** — a branch + commits + a task id; no file path. Addressable, but `git_ref`/`task` is the sharper label. A rule that caught it would misfile real paths (§5, the rejected alternative) |
| 1229 | `git_blame tool` | `descriptor` | ✅ names a capability, not an artifact |
| 1541 | `~/work/GoPi task t-1788904463` | `local_path` | ✅ the path rung wins — a stronger address than the task id |
| 1611 | `GoPi feat/hippocampus-adoption worktree ~/workspace/gopi-hippo-adopt; internal/agent/hippocampus.go,…; devdoc/hippocampus.md §4` | `local_path` | ✅ dense but genuinely addressable |
| 1677 | `~/work/GoPi devdoc/webcam-capture-rpi5-agent-evaluation.md + devdoc/webcam-capture-evidence/ (commit 587624f…)` | `local_path` | ✅ |

**9/10 unambiguous; 1/10 (id 1130) is addressable-but-imprecisely-bucketed, and
that imprecision is reported rather than hidden.** Every assigned label was
defensible against the row's own text; none was fabricated.

*(An earlier sample in this same session found id 31 → `unknown` whose topic
names `hermes_cli/plugins.py`. The label is correct **about the field** — the
`source_type` describes where `sources` points, not what a future enrichment
could infer. That is exactly the distinction `--latent` exists to surface.)*

---

## 8. Physical backup (constraint 2 — before ANY write)

```bash
cp ~/.gopi/memory.db ~/.gopi/backups/memory.db.bak-pre-provenance-20260911-113832
```

Verified as a *physical* copy (not a reflink/hardlink — the known past pitfall),
by comparing content hashes before any write:

```
7092a6fb1f07289c82100a046bdec8c5641af8818916b3bc8e9ccc579d634b82  backup
7092a6fb1f07289c82100a046bdec8c5641af8818916b3bc8e9ccc579d634b82  live
```

**Additive proof at the data level** — every pre-existing row (`id <= 1879`)
diffed against the backup, per column:

```
topic       (rows<=1879): IDENTICAL ✅
question    (rows<=1879): IDENTICAL ✅
answer      (rows<=1879): IDENTICAL ✅
search_kw   (rows<=1879): IDENTICAL ✅
sources     (rows<=1879): IDENTICAL ✅
```

---

## 9. Verification

| check | result |
|---|---|
| `go build ./...` | OK |
| `go vet ./internal/tools/ ./cmd/agent/` | OK |
| `go test ./internal/tools/ -run TestProvenance` | **PASS** (7 tests) |
| `go test ./internal/tools/ ./cmd/agent/` (full) | **PASS** |
| `gofmt -l ./internal ./cmd` | clean |
| live idempotency (backfill ×3) | filled=1835, **0**, **0** |
| live reclassify ×2 | identical labels |
| `PRAGMA integrity_check` | `ok` |
| total rows == labelled rows | 1836 == 1836 at verification time |

Tests added (`internal/tools/memory_provenance_test.go`):
`TestProvenanceLadderAssignsEveryFixture`, `…LadderIsTotal`,
`…TaxonomyIsDocumented`, `…BackfillIsAdditiveAndIdempotent`,
`…ReclassifyIsPure`, `…AddressabilitySplit`, `…CountsSumToPopulation`,
`…LatentSplit`. The fixture table doubles as the classifier's specification and
is run through the **production SQL path** (`provClassifySource`), not a Go
re-implementation.

---

## 10. Rule 16 mapping (explicit)

| Rule 16 clause | how this work satisfies it |
|---|---|
| **16a** — every stage's output must land in addressable storage | the origin of each knowledge row is now a **typed, queryable** field (`source_type`) instead of free text a downstream task must re-read and re-interpret |
| **16b** — a new feature must answer "which link does it complete?" | this completes the **记忆 (memory) link**: it makes what an earlier task learned *resolvable*, so a later task does not re-derive it |
| **16c** — broken links must be listed, not implied | §6 lists them with counts: 500 `unknown` (495 recoverable, 5 not), 633 `import`, 65 `descriptor`, 39 `session` |
| **16e** — prefer reusing the existing store over new stores/daemons | **no new store, no new daemon, no new table**: one additive column + one CLI verb on the existing `gopi memory` family (which is **zero attention cost** — it is not an LLM tool) |

Also satisfied: **Rule 1/13/14** (no bare string keywords; every value from a
named constant; the ladder is the single source of truth for all ten bucket
names), **Rule 3** (no new top-level tool — the surface is a CLI subcommand),
**Rule 4** (surgical edits), **Rule 5** (this document), **Rule 8** (files added
one by one).

---

## 11. Not done / residual problems (honest list)

1. **The `evidence` column is dead** (empty for every row). Nothing was done
   about it; the schema cleanup that removes or repurposes it is a separate
   task.
2. **495 latent rows are measured but not recovered.** Harvesting the path from
   `topic`/`question` into `sources` is a follow-up (§9 of the epic); it needs
   its own accuracy discussion (the topic path may point at a *subject*, not an
   *origin* — e.g. row 31 discusses `hermes_cli/plugins.py`, which is arguably
   the subject of the lesson rather than where the lesson came from).
3. **Newly inserted rows start with an empty `source_type`** until the next
   backfill run. This is by design (the store path does not classify — that
   would put classification logic on the write path and its cost on every
   store), but it means the invariant is "eventually labelled", not
   "always labelled" — observed live as 3 unlabelled rows from concurrent
   sessions during verification. A hook that classifies on insert would close
   it; it is not in this task's scope and should be decided deliberately.
4. **`git_ref`/`task` precision** — row 1130 (§7) is addressable but imprecisely
   bucketed. The narrow rule was chosen over a broader one that would misfile
   real paths (§5); the imprecision is quantified, not hidden.
5. **`import` cannot be made per-row addressable** without the upstream
   migration's provenance, which is no longer available.
6. **The worktree was destroyed twice by an external sweep** during this task
   (see §12) — the clone fallback worked, and the incident is filed as a bug.

---

## 12. Environment incident (recorded for the next agent)

`git worktree add ~/workspace/gopi-prov -b feat/distilled-provenance-governance`
was created **twice**, and within ~7 minutes each time both the directory **and
the branch** were gone (`git worktree list` empty entry, `git branch` empty,
`reflog` unknown revision, no `archive/*` tag). Zero commits existed in either,
so no work was lost. Suspected: the 30-min `scratch-gc-watchdog` cron sweep or
the merger's `repo_sweep.go` tidy step (which removes leftover worktrees and
`branch -D`s merged branches). Filed as bug **`01a08e8f-3368-7bc0-a0f7-f61f7ebc6e79`**
with repro notes. Work continued in a **clone** (Rule 6a's explicit exception —
a clone is not in the worktree registry, so the sweep cannot prune it).

---

## 13. Recover / re-run

```bash
# the deliverable
git show feat/distilled-provenance-governance:internal/tools/memory_provenance.go
git show feat/distilled-provenance-governance:devdoc/distilled-provenance-governance.md

# re-run the whole thing from scratch (idempotent)
gopi memory provenance --shape
gopi memory provenance --backfill
gopi memory provenance --counts
gopi memory provenance --latent

# rollback (the backup, byte-identical to the pre-write DB)
cp ~/.gopi/backups/memory.db.bak-pre-provenance-20260911-113832 ~/.gopi/memory.db
```

Commit hashes: see the branch log (`git log --oneline feat/distilled-provenance-governance`).
