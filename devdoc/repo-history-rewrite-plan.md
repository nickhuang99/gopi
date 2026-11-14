<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# Task Report: repository history rewrite — measured plan (PLANNING ONLY)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Epic:** `01a0fd2e-ee73-7151-99b6-481feae086ff` (this run). **Deliverable shape:**
a PLAN. This run — deliberately and by its own constraints — **executed no
destructive operation on any repository**. See §1.

**Topic:** the blobs GoPi history carries that HEAD no longer needs, and the two
available mechanisms for removing them, with cost, blast radius, preconditions
and a rollback.

---

## 1. This run did NOT rewrite anything (read this before the numbers)

**Statement of non-execution.** No `filter-branch`, no `filter-repo`, no
`reflog expire`, no `gc --prune`, no `push --force`, no history rewrite of any
kind was run **against `/home/user/work/GoPi` or any of its worktrees**. Every
destructive-capable measurement below ran on a **throwaway clone under the pinned
scratch root** (`/home/user/workspace/gopi-test-scratch/…`, device id 2052) which
the probe deleted on exit; the shared repository was only ever **read** (`rev-list`,
`cat-file`, `ls-tree`, `log`, `count-objects -v`, `ls-remote`, `clone`/`fetch`
into a throwaway, `pack-objects --stdout` to a file in the session directory).

**Incidental litter, disclosed.** At t+465s a `git filter-branch` invocation was
started **in this worktree** as a cost probe and **timed out at 120 s, leaving a
`.git-rewrite/` directory in the worktree root**. It was deleted
(`rm -rf .git-rewrite`), and the shared repo was then measured **unaffected**:
`git -C /home/user/work/GoPi rev-parse master` → `ca58d6d1…` (unchanged),
`refs/original` **does not exist** in the shared `.git`, no `.git-rewrite` under
it either, and `git for-each-ref | wc -l` → **526** as before. Every cost probe
that needed to COMPLETE was moved to a throwaway clone under the scratch root
from that point on. Reporting this is the point: a partial `filter-branch` is
exactly the failure mode §3.4's "half-rewritten shared store" row describes, and
this run is evidence that it can be entered by accident.

**What this run DID write:** `devdoc/repo-history-rewrite-plan.md` (this file),
its row in `devdoc/DOC-INDEX.tsv`, and the task report — all inside this run's
own worktree `/home/user/workspace/gopi-01a0fd2eee73715199b6481feae086ff`.
Nothing was written into `/home/user/work/GoPi` (Rule 6/29), and the main repo's
working tree was clean before and after.

**Why a plan and not an execution.** Three separate reasons, each sufficient:
1. `git filter-repo` is absent from this host and **Rule 35 forbids installing
   it** (a new dependency, and this is precisely the "don't wrap the problem"
   case). Measured: `command -v git-filter-repo` → exit 1; no
   `git_filter_repo` Python module; `/usr/lib/git-core/` carries only
   `git-filter-branch`.
2. `git filter-branch` writes the **SHARED** `.git` — proven mechanically in
   §3.3 — which is the common object store and ref namespace of **6 worktrees and
   526 refs**, owned by other live runs. Rewriting it is a coordinated, operator-
   present operation, not a side effect of a hygiene task.
3. **HP holds a divergent clone** with its own worktree and 619 reflog entries
   (§6). A rewrite that lands only on peerbox splits the two hosts' histories
   irreconcilably; the peer must be inside the maintenance window.

The previously-landed scope-3 report reached the same conclusion independently
(`devdoc/repo-hygiene-sandbox-evidence-removal-20261003.md`, commit `67ddf5a3`):
*"Treat the leaked material as disclosed, not as remediable by a later cleanup
commit."* This document is the cost model that report asked for.

---

## 2. The measured inventory

### 2.1 Reproducible measurement — the four commands

All four were run in this worktree at `HEAD = 56e17d986066ee7704ef3e209bc856a1b08bd631`
(`master = ca58d6d1`, shared `.git = /home/user/work/GoPi/.git`), git **2.34.1**.
`$A = /home/user/.gopi/sessions/01a0fd2e-ee73-7151-99b6-481feae086ff/artifacts`
holds every raw artifact named below.

```bash
# C1 — every reachable object WITH its path, over ALL refs AND the reflog
#      (the reflog matters: an object reachable only from a reflog is still
#       in the pack and is still served by `git clone`)
git rev-list --objects --all --reflog > "$A/rl-objects.txt"        # 59,124 lines

# C2 — each object's TYPE and SIZE. Paths on stdin, sizes on stdout.
#      NOTE: `cat-file --batch-check` with no format prints only the 4th field
#      NAME, not the size — the format string is mandatory.
awk '{print $1}' "$A/rl-objects.txt" \
  | git cat-file --batch-check='%(objectname) %(objecttype) %(objectsize)' \
  > "$A/rl-sizes.txt"

# C3 — join, keep blobs >= 1 MiB (1,048,576 B), biggest first
#      (a pathless line in rl-objects is a commit/tree: `$2 == ""`)
awk 'NR==FNR{ty[$1]=$2; sz[$1]=$3; next}
     {p=($2==""?"<tree-or-commit>":$2);
      if (ty[$1]=="blob" && sz[$1]+0>=1048576) printf "%d\t%s\t%s\n",sz[$1],$1,p}' \
    "$A/rl-sizes.txt" "$A/rl-objects.txt" | sort -nr > "$A/ge1M-canonical.tsv"

# C4 — per-path-group blob byte totals (the remove-set accounting of §2.4)
```

**Three traps this recipe exists to avoid** (each cost a wasted round here):

| trap | what it actually returns | correct instrument |
|---|---|---|
| `rev-list --filter=blob:limit=1M --missing=print` | the **missing/unfetched** oids (a partial-clone tool), *not* the large blobs | enumerate `--objects` and size each oid |
| `git cat-file --batch-check <oid>` | the line `oid type 4-path-column` — the 4th field is the **path**, not the size | pass `--batch-check='…%(objectsize)'` |
| `git ls-tree -r -l` column order | `mode SP type SP oid SP size TAB path` | `awk '{print $4, $5}'` is size, path |

A fourth, structural: **`HEAD` is not the population.** 1,868 blobs (299.1 MiB)
live under paths HEAD does not track; `git rev-list HEAD` cannot see them, and a
plan built on `HEAD` alone would under-count by ~19 %.

### 2.2 Totals (all refs + reflog, `HEAD=56e17d98`)

| dimension | blobs | bytes | MiB |
|---|---|---|---|
| **≥ 1 MiB blobs, all refs** | **45** | **359,269,609** | **342.6** |
| … of which reachable from HEAD | 41 | 324,950,990 | 309.9 |
| … of which HEAD does **not** reach | 4 | 34,318,619 | 32.7 |
| all blobs, all refs | 23,027 | 1,667,330,432 | 1,590.1 |
| … under a path HEAD does **not** track | 1,868 | 313,584,595 | 299.1 |
| all reachable objects, all refs | 59,131 | — | — |
| … commits / trees / tags | 8,316 / 27,596 / 192 | — | — |
| HEAD closure, blobs | 21,766 | 1,566,822,702 | 1,494.2 |
| HEAD tracked (worktree, `ls-tree -l`) | 7,316 files | 272,641,025 | 260.0 |
| shared `.git` on disk (`du -sb`) | — | 576,749,881 | 550.0 |
| shared `.git` packs (`size-pack`) | 59,234 in-pack | 403,361 KiB | 393.9 |

The four **HEAD-unreachable** large blobs — the ones the epic names, and the
proof that a HEAD-only sweep is not enough:

| bytes | oid | path (last seen) | reachable from |
|---|---|---|---|
| 26,284,210 | `56769f05` | `gopi-behind` | history / reflog only |
| 5,362,967 | `a49ffa47` | `tests/fixtures/gopi-raw-20260905-024552.log` | history only |
| 1,337,520 | `1a3cbac9` | `docs/archive/demo/materials/112-raw/04-annotated.png` | history only |
| 1,333,922 | `258e1f2f` | `docs/archive/demo/materials/112-raw/03-raw.png` | history only |
| **34,318,619** | | **total** | |

### 2.3 The 45 ≥ 1 MiB blobs, by removal class

Sizes in bytes. Classes A–G are the **remove set**; K is deliberately kept.

| class | path group | blobs | bytes | MiB |
|---|---|---|---|---|
| A | `bin/` | 24 | 215,275,386 | 205.3 |
| B | `agent`, `gopi-behind` (root build outputs) | 2 | 44,174,768 | 42.1 |
| C | `sandbox/` | 1 | 19,732,823 | 18.8 |
| D | `docs/archive/demo/` | 11 | 45,510,182 | 43.4 |
| E | `devdoc/{evidence,assets,requirements/evidence}/` | 4 | 6,839,745 | 6.5 |
| F | `tests/fixtures/gopi-raw-*` | 1 | 5,362,967 | 5.1 |
| G | `.leakprobe*` | 1 | 19,497,922 | 18.6 |
| | **A–G (remove)** | **44** | **356,393,793** | **339.9** |
| K | `scripts/links/amd64/links` | 1 | 2,875,816 | 2.7 |
| | **total ≥ 1 MiB** | **45** | **359,269,609** | **342.6** |

Class K is **not** in the remove set: `cmd/agent/cli_term_links.go` executes that
vendored binary at runtime — deleting it breaks the shipped product (this is the
scope-3 KEEP, re-derived here for the ≥1 MiB population — see §2.5 note).

### 2.4 The remove set as byte mass (per-path-group, all refs + reflog)

Blob bytes carried by each group (not the ≥1 MiB subset — the whole group):

| group | blob bytes | MiB |
|---|---|---|
| `bin/` | 215,275,386 | 205.3 |
| `docs/archive/demo/` | 241,434,823 | 230.3 |
| `gopi-behind` | 26,284,210 | 25.1 |
| `sandbox/` | 22,264,607 | 21.2 |
| `agent` | 17,890,558 | 17.1 |
| `.leakprobe*` | 20,441,862 | 19.5 |
| `devdoc/evidence/` | 9,234,651 | 8.8 |
| `devdoc/assets/` | 5,136,385 | 4.9 |
| `tests/fixtures/gopi-raw-20260905-024552.log` | 5,362,967 | 5.1 |
| `devdoc/requirements/evidence/` | 4,272,554 | 4.1 |
| **remove-set total** | **567,598,003** | **541.3** |
| all other blobs | 1,099,732,429 | 1,048.8 |
| all blobs | 1,667,330,432 | 1,590.1 |

At HEAD the same groups are **2,000 tracked files / 186,107,789 B = 177.5 MiB**
(bin/ is already 0 tracked — `02fcfa2c`, 2026-08-03, removed it and added
`.gitignore:bin/`; the 24 `bin/gopi` blobs are pure history). The remaining
5,316 files / 86,533,236 B are the keep set.

### 2.5 What is deliberately NOT removed (and why)

- **`scripts/links/amd64/links`** (2.7 MiB) — product-read at runtime (§2.3).
- **`tests/fixtures/log-stats-raw/**` (29 files)** — test-read by
  `internal/gopilog/stats_golden_test.go`.
- **`.md` files inside the evidence directories** — text of record.
- **`.sh`/`.py`/`.go`/`.mod` inside them (60 files)** — the *recipe* that
  regenerates the artefacts; deleting them deletes the regeneration ability
  (Rule 39's own distinction).
- **`devdoc/DOC-INDEX.tsv`'s 1,147 revisisions = 544,241,125 B (519.0 MiB)** —
  the single largest *history* sink in the repo, and **out of scope for this
  plan**: it is a live, append-only table of record, not an artefact. Flagged
  here because it dominates any future pack-size discussion; a rewrite of it
  would break every `path`-tier evidence row that cites it.
- **`sandbox/gopi-bin`** (19.7 MiB, tracked at HEAD today) is *in* the remove
  set (class C) — it is a copied binary, not a profile's text of record. The
  other 28 files under `sandbox/` are operator-profile data already discussed by
  scope-3; their removal from history is the *point* of the rewrite, not a
  side effect.

---

## 3. The two routes, compared

### 3.1 Route A — `git filter-repo`

**UNAVAILABLE on this host, and Rule 35 forbids installing it.** Measured:
`command -v git-filter-repo` → rc 1; `python3 -c "import git_filter_repo"` →
`ModuleNotFoundError`; `/usr/lib/git-core/` contains `git-filter-branch` only.

There is, however, **precedent that it has run here before**: a populated
`.git/filter-repo/` metadata directory exists in the shared repo, timestamped
**2026-09-07 14:44**, carrying `commit-map` (1,314 rewrite rows, **0 deletions**),
`ref-map` (30 rows across `master`, `refs/heads/gopi-codex-taskcode`, 8
`refs/remotes/*` and 9 `refs/tags/archive/*`), `changed-refs` (17 refs),
`first-changed-commits` (`1e63f3f4` → `6cf7d1ef`) and `suboptimal-issues`
(90 stale hash references left in commit messages). So the tool ran on a
machine that had it; it is simply not installed **now**, and this run will not
install it. **A future execution should decide Route A on availability, not on
preference** — and if the tool is present, it is the better instrument (§3.4).

Estimated cost **if** it were available (model, not measurement — flagged
because it is a model): one `fast-export | fast-import` pass plus the rewrite
filter. Engine cost is measurable and was measured here (§3.3 P3): the
export/import round trip over **all refs** is the dominant term; filter-repo adds
`--replace-refs` bookkeeping and writes `commit-map`/`ref-map` without which no
clone can be re-pointed.

### 3.2 Route B — `git filter-branch` (the only available instrument)

Present (`/usr/lib/git-core/git-filter-branch`, git 2.34.1).

**The property that decides the whole cost: it writes the SHARED `.git`.** See
§3.3. Consequences: every one of the 6 worktrees of the shared repo is invalidated
at once; all 526 refs must be rewritten together; the run needs a quiesced host,
not just a quiesced worktree; and git's own warning applies — `filter-branch` is
documented as slow and deprecated in favour of `filter-repo`.

### 3.3 Measurements the cost model rests on (all reproduced in this run)

**P1 — a linked worktree owns no store and no refs.** Read from this run's own
worktree: `git rev-parse --git-dir` =
`/home/user/work/GoPi/.git/worktrees/gopi-01a0fd2e…`;
`--git-common-dir` = `/home/user/work/GoPi/.git`;
`--git-path objects` = `/home/user/work/GoPi/.git/objects`; the worktree-private
directory holds only `commondir gitdir HEAD index logs ORIG_HEAD`. **There is no
private object store to rewrite.**

**P2 — `filter-branch` from a worktree writes the common dir.** In a throwaway
clone, an 11-commit first-parent range was filtered **from inside a linked
worktree**: rc 0, the worktree's `HEAD` moved
(`ca58d6d1`→`60d1b489`), the new commit is a **commit in the COMMON store**, and
`refs/original/` landed **in the common `.git`**, not in the worktree-private
dir. This is the mechanical proof behind §3.2.

**P3 — `fast-export | fast-import` (the filter-repo engine) over all refs.**
Measured on a throwaway bare target (`artifacts/p3.log`):
`P3_fast_export_import_all_refs_seconds=261.4`, rc 0, landing
`8230 commits / 519 refs / 22,948 blobs` in a **609,764,449 B** `.git`
(610,525,838 B after `gc --prune=now`). This is the floor under Route A: whatever
else filter-repo does, it pays **this 4.4-minute pass** first — and note the
target is *larger* than the source pack (609.8 MB vs 403.4 MB of packs), because
`fast-import` writes everything fresh with no delta reuse. A Route A execution
therefore needs **~610 MB of headroom** before it even starts filtering, not the
~50 MB the post-rewrite pack suggests.

**Pack-size probes** (throwaway, `pack-objects --stdout`, output to a session file):

| probe | bytes | MiB | seconds |
|---|---|---|---|
| full pack, `--all --reflog` (today) | 382,928,729 | 365.2 | 6.5 |
| full pack, `--depth=0 --window=0` (upper bound, no deltas) | 873,566,938 | 833.1 | 115.5 |
| HEAD-only pack (today) | 351,523,588 | 335.2 | 5.8 |
| **after rewrite** — HEAD closure minus classes A–G | **52,726,678** | **50.3** | — |
| **after rewrite** — all refs minus classes A–G | **52,848,265** | **50.4** | — |

**`filter-branch` per-commit rate** (two independent range-restricted runs on
throwaway clones):

| run | range | wall | s/commit |
|---|---|---|---|
| `fb-range.sh 60` | base..HEAD = **205** commits (60 first-parent + 145 merge-side) | 60.02 s | **0.29** |
| `wt-proof3.sh` | base..HEAD = **11** commits | 2.86 s | **0.26** |

⇒ **~0.26–0.29 s per rewritten commit.** For 8,314 commits: **36–40 minutes** of
pure rewrite, excluding the repack. (An earlier 0.054 s/commit proxy from a
`true` index-filter was **discarded as unrepresentative** — the real filter
`git rm -r --cached --ignore-unmatch <10 paths>` is the cost, and it is 5×
larger. This is exactly the trap Rule 31 exists for.)

**Full-rewrite measurement (job `aftermeasure`)** — `filter-branch` over all of
`master` in a throwaway clone, then `reflog expire` + `gc --prune=now`. The run
was **started and then killed before it printed** (t+855s → t+1397s; the process
was still in its first `filter-branch` pass after 540 s and the session budget
had to be spent on the deliverable). **Its wall-clock figure is therefore NOT
claimed here** — the 0.26–0.29 s/commit rate from the two completed range runs
stands as the measured basis, and reading that job's log is the first step of a
future execution. It is listed here so the gap is visible rather than papered
over with an extrapolated number presented as a measurement.

### 3.4 Cost comparison table

| dimension | **A: `git filter-repo`** | **B: `git filter-branch`** |
|---|---|---|
| available today | **NO** — absent; Rule 35 forbids installing | **YES** — `/usr/lib/git-core/git-filter-branch`, git 2.34.1 |
| writes | a fresh `.git` (recommended), or `--force` on a mirror | **the SHARED `/home/user/work/GoPi/.git`** (proved: §3.3 P2) |
| worktrees invalidated | coordinated re-clone of each | **all 6 at once, simultaneously** |
| refs handled | all refs by default; `commit-map`/`ref-map` written for re-pointing | `-- --all` rewrites all, but **tags need `--tag-name-filter cat`** or they keep old SHAs while pointing at doomed objects |
| reflog | rewritten by the tool; bookkeeping files let you map old→new | **must be expired by hand** (`reflog expire --expire=now --all`) or the old commits stay reachable and the pack never shrinks |
| old→new map | **`commit-map` + `ref-map`** (measured precedent: 1,314 + 30 rows) — the artifact that makes peer re-pointing mechanical | `refs/original/` only: **one ref at a time, no per-commit map**; a 470-tag re-point is by hand |
| measured cost | **261.4 s** for the `fast-export \| fast-import` engine pass over all refs (8,230 commits → 609.8 MB target, `artifacts/p3.log`), **plus** the per-commit filter cost of Route B (same engine underneath) | **0.26–0.29 s/commit ⇒ 36–40 min for 8,314 commits** + repack |
| repack | `git gc --prune=now` (or `--aggressive`); the tool prunes as it goes | mandatory and manual: `rm -rf .git/refs/original && reflog expire --expire=now --all && gc --prune=now` |
| failure mode | aborts before touching the repo when run on a fresh clone | **half-rewritten shared store**; `refs/original/` is the only undo, and it is per-ref |
| Rule 35 verdict | **a new dependency — refused** | zero new dependency — already installed |
| **verdict** | **preferred IF it becomes available** (best map, best rollback, dry-run-able) | **the only route available now**, and only inside a maintenance window with the operator present |

### 3.5 The route this plan recommends — a third shape, no new dependency

Neither A nor B needs to touch the live shared `.git`. The measurement of §3.3
P3 + the pack probes support a **bounded, reversible** procedure that uses only
installed git:

1. `git clone --mirror --no-hardlinks /home/user/work/GoPi <off-/-mirror>` (§7) —
   12.6 s, 535.8 MiB.
2. Run Route B **on the mirror**, not on the live repo: `filter-branch --index-filter
   'git rm -r --cached --ignore-unmatch <classes A–G>' --prune-empty --tag-name-filter cat -- --all`,
   then `rm -rf refs/original && reflog expire --expire=now --all && gc --prune=now`.
3. The mirror's `--all` surface is then the **proposed** history; the live repo is
   untouched throughout.
4. Hand the mirror path to the operator for acceptance **before** anything is
   pushed back; re-point each worktree and the HP clone from it (§6) with a
   `git fetch` + `reset --hard`, never a force-push into a checked-out branch.

This is the shape Rule 35 asks for: **no new artifact, the live repo is not the
subject, and the destructive step happens on a copy that is itself the backup's
sibling.** Its cost is one extra 535.8 MiB and one extra copy pass; in exchange
the "half-rewritten shared store" failure mode disappears.

---

## 4. SHA invalidation range

A commit keeps its SHA **iff neither it nor any ancestor carries a removed path**
(the parent pointer propagates the change). Measured with the remove set of
§2.4 (bin, sandbox, docs/archive/demo, devdoc/{evidence,assets,requirements/evidence},
`tests/fixtures/gopi-raw-20260905-024552.log`, gopi-behind, agent, .leakprobe*):

```bash
git rev-list --all --reflog | sort -u | wc -l                                   # 8,314
git rev-list --all --reflog --not \
  $(git log --all --reflog --no-merges --format=%H -- <the 10 paths> | sort -u) \
  | sort -u | wc -l                                                             #   458 (keep SHA)
```

| population | count | keeps SHA | rewritten |
|---|---|---|---|
| all commits, all refs + reflog | 8,314 | **458 (5.5 %)** | **7,856 (94.5 %)** |
| commits reachable from `HEAD` | 7,830 | **9 (0.1 %)** | **7,821 (99.9 %)** |
| tags | 470 | **0** | **470 (100 %)** |
| reflog entries (`--all`) | 5,474 | 0 | 5,474 |
| refs to rewrite | 526 | — | 526 |

**The 9 preserved HEAD commits** are the first-parent prefix before the first
removal: `d5736e9a c5cef3ca 7c3a9f2c a4bebc27 b05681aa 98c850fe 52213ae2 93f8fa07
f7cec3b6`. The 10th is `0cb982ca` ("feat: add edit tool…", 2026-07-31 22:21:35)
— the commit that **added `bin/gopi-agent`, 8,917,712 B**, and the first commit
whose tree carries a removed path. Everything from it onward (7,821 commits) gets
a new SHA. Verified: each of the 9 ancestors has 0 removed paths in its tree.

**Consequences, stated plainly:**
- **Every SHA in every report, task row, commit-message cross-reference and
  `git show <hash>` in the session logs becomes dangling.** The repo's own prior
  filter-repo run recorded exactly this failure mode in
  `suboptimal-issues`: **90 stale hashes** left inside commit messages, because a
  rewrite cannot edit the text that quotes a commit it is deleting.
- **All 470 tags move.** 214 are `archive/*` (the Rule 9 loser archive) and 192
  are annotated — an annotated tag is a *tag object* containing the commit hash,
  so it is invalidated twice over (tag object + target). `--tag-name-filter cat`
  is required for Route B to re-point them; without it, tags keep their old names
  pointing at objects that the `gc --prune` will delete.
- **`refs/stash` (1 entry, base `9dfc688f`)** and **33 `refs/remotes/*`** are
  rewritten too — 9 of them are `hp/*` tracking refs that will disagree with the
  peer's actual refs until §6 is done.

---

## 5. Blast radius: what a rewrite invalidates

| artifact | today | after rewrite |
|---|---|---|
| shared `.git` on disk | 576,749,881 B (550.0 MiB) | ~50.4 MiB of blobs in a fresh pack (§3.3) ⇒ order 60–90 MiB on disk |
| reachable blobs | 23,027 / 1,590.1 MiB | 1,868 + … — 299.1 MiB of history-only mass plus the HEAD-only copies |
| worktrees of the shared repo | **6** | all 6 invalid; each needs `reset --hard` to the new tip |
| refs | 526 (12 `refs/heads`, 470 tags, 33 `refs/remotes`, 1 stash, 7 `refs/gopi`, 1 `refs/capdemo8-archive`, 1 `refs/hpremote`, 1 `refs/tmp`) | all re-created; 458 commits' worth of side branches may be *gone* (their tips may be deleted commits) |
| reflog | 5,474 entries, `.git/logs` = 1,131,303 B | zeroed (`reflog expire --expire=now --all`) |
| peer clone (HP) | 468,107,527 B, `master=b01ce0f4`, 1 local head, 422 tags, 619 reflog entries, 1 stash, 2 worktrees | must be re-cloned or hard-reset; **it cannot fast-forward** (see §6) |
| the main repo's own working directory | clean (`git status --porcelain` → empty) | must stay clean across the window |

---

## 6. Collateral damage and the remediation checklist

### 6.1 The HP clone is DIVERGENT — measured, not assumed

```
merge-base(peerbox/master ca58d6d1, hp/master b01ce0f4) = 8374e6af  (2026-10-02 10:53:05)
hp ahead of that base   : 31 commits
peerbox ahead of that base: 140 commits
```

So the two masters are **not** in an ancestor relation: neither side can
fast-forward onto the other. A rewrite on peerbox produces a history HP cannot
`git pull` into — any pull is a merge of two unrelated rewritten histories, and a
`push --force` from HP would resurrect the removed blobs. Measured detail:
`hp` has 2 worktrees (`/home/user/work/GoPi` at `b01ce0f4` and
`/home/user/workspace/gopi-m2-landed-wt` detached at `79807932`), and its
`peerbox` remote is `DISABLED-no-push` (only `nick@192.168.7.6/work/GoPi` fetch),
so **HP cannot push into peerbox by accident** — but peerbox's `hp` remote can push
into HP, and `receive.denyCurrentBranch` is **unset** (`git config --get-all` →
rc 1) on peerbox's side.

### 6.2 The checklist (execute in this order, one window, operator present)

**Phase 0 — freeze (see §8).** No worker, no merger, no `gopi task serve`.
All worktrees clean. A recorded list of every live worktree at freeze time.

**Phase 1 — backup.** Mirror clone to a directory **off the `/` partition**
(§7). Verify: `git -C <mirror> rev-parse master` equals the frozen tip, and
`git -C <mirror> fsck --no-progress --connectivity-only` is silent.

**Phase 2 — rewrite on the MIRROR, not the live repo** (§3.5). Record the
mirror's `refs/original` → new-tip mapping by hand if Route A's `commit-map` is
not available: `git -C <mirror> for-each-ref > before.txt` first, then diff
after.

**Phase 3 — the live repo becomes a consumer of the mirror, not its subject.**
Either (a) replace the live `.git` from the mirror and re-checkout each worktree,
or (b) treat the mirror as the new canonical repo and move the old one aside.
*(a) is cheaper; (b) is safer and is the recommendation, because the old repo
stays on disk as the second copy.*

**Phase 4 — 6 worktrees: one at a time, no exceptions.**

For each worktree `W` (measured today: `/home/user/work/GoPi`,
`…/gopi-01a0fd2eee73715199b6481feae086ff`, `…/gopi-01a0fd2ef01a7be6932fafd9b755451a`,
`…/gopi-01a0fd34807c79b0909d29eab89dcbc1`, `…/gopi-01a0fd371710745ebeaba27e5b2da961`,
`…/gopi-01a0fd4de3dc79cf83a3096aae52b3fc`):

```bash
git -C "$W" status --porcelain          # MUST be empty; a dirty tree is committed or stashed, never discarded (Rule 30)
git -C "$W" stash list                  # 1 stash exists in the shared repo (base 9dfc688f) — record it
# after the live repo is replaced:
git -C "$W" reset --hard <new-tip-of-its-branch>    # or: git worktree remove + re-add
git -C "$W" status --porcelain          # empty again
```

Worktrees whose branch tip was a **deleted commit** (a side branch whose only
commits carried removed paths) must be *decided*: re-point, or archive the branch
first. Count today: 3 of the 5 sibling task worktrees sit on `task/*` branches
whose reachability must be re-checked after the rewrite.

**Phase 5 — HP (the divergent peer).** Recommended, and the only shape that does
not lose HP's 31 commits:

```bash
# ON HP: preserve its own work FIRST, do not reset
cd ~/work/GoPi && git status --porcelain          # must be empty
git bundle create /home/user/hp-unmerged-$(date +%Y%m%d).bundle 8374e6af..master   # the 31 commits
git worktree list                                  # 2 worktrees: the detatched one is released first
# then re-clone from peerbox's post-rewrite repo (NOT a pull, NOT a force-push):
mv ~/work/GoPi ~/work/GoPi.pre-rewrite
git clone nick@192.168.7.6:/home/user/work/GoPi ~/work/GoPi
git -C ~/work/GoPi fetch ~/work/GoPi.pre-rewrite <the bundle>   # or: git am / cherry-pick the 31 commits onto the new base
```

`hp`'s 619 reflog entries and its 1 stash are **lost** unless bundled first —
a re-clone recreates neither.

**Phase 6 — unfreeze and verify.**
`git -C /home/user/work/GoPi fsck --no-progress --connectivity-only`;
`git worktree list` shows 6 (or fewer, by decision) healthy entries;
`git rev-list --all | wc -l` is the expected new count;
`git cat-file -e 56769f05db6a9ad5d8637aa8026bd66496525b07` **fails** (the
26 MiB `gopi-behind` is gone) — that is the success probe;
`du -sb .git` is in the expected band; every peer's `git log -1` agrees;
then restart the worker/merger.

---

## 7. Rollback and backup size (measured)

| item | measured | note |
|---|---|---|
| `git clone --mirror --no-hardlinks /home/user/work/GoPi <dest>` | **561,899,541 B = 535.8 MiB**, **12.6 s** | all 526 refs, all reflogs, full pack |
| destination partition | `/home` = device **2052**; `/` = device **2051** | satisfies Rule 22 (off `/`) — verified with `stat -c %d` |
| free space on `/home` | 140 G free of 766 G (81 % used) | one mirror is 0.4 % of free space |
| a second cold full pack (`--all --reflog`) | 382,928,729 B = 365.2 MiB, 6.5 s | the post-rewrite pack for comparison |

**Budget two mirrors, not one** (715 MiB total): one taken *before* the rewrite
(the rollback), one taken *after* acceptance (the deliverable's proof). Both fit
comfortably; neither is on `/`.

**Rollback**: with the live repo replaced rather than rewritten in place (§3.5 /
Phase 3 option b), rollback is `mv` — the old repo directory was never modified.
That is the single strongest reason for the mirror-first shape.

---

## 8. Preconditions to freeze before execution

Every line is a **check that must be measured and recorded**, not an intention:

1. **No live run.** `gopi worker`/`serve`/merger stopped; no `task/*` branch
   with a live holder; the task ledger drained or paused. (Verified today: 5
   sibling task worktrees existed and one moved mid-run — `task/01a0fd2ef0…`
   went `db738897`→`334d01da` *while this run was measuring*, and the shared
   `.git` grew 574,379,917 → 576,749,881 B in 15 minutes. **The repo is live
   under measurement; a rewrite window must stop that.**)
2. **Every worktree clean**: `git -C <W> status --porcelain` empty for all 6, and
   the main repo's own tree too.
3. **The stash is either applied or bundled** (1 entry, base `9dfc688f`).
4. **HP quiesced and bundled** (§6.2 Phase 5) — its 31 unmerged commits are
   preserved *before* anything is re-cloned.
5. **A verified mirror exists off `/`** (§7) and `fsck --connectivity-only`
   passes on it.
6. **Operator present and the window recorded.** The rewrite is ~36–40 min of
   CPU plus repack; it must not run unattended (Rule 35's "prefer a one-shot you
   invoke over a resident that invokes itself", applied to a *human-owned* step).
7. **`refs/original` is empty** — measured today: `/home/user/work/GoPi/.git/refs/original`
   does not exist ⇒ a future `filter-branch` has no prior undo to collide with.
8. **`receive.denyCurrentBranch` decided** (currently unset → git's default
   `refuse` for a checked-out branch; a force-push into HP's checked-out `master`
   is what a careless re-point looks like).

---

## 9. Risks and honest gaps

- **This plan's per-commit rate is an extrapolation** from two completed range
  runs (205 commits → 0.29 s, 11 commits → 0.26 s, i.e. 0.26–0.29 s/commit) to
  8,314 commits ⇒ **36–40 min**. The single full-repo run started this session
  was **killed before printing** (it was still running at 540 s) and its figure is
  deliberately not claimed (§3.3). Re-run it first when executing; treat 36–40 min
  as a *model from a measured rate*, not as a measured total.
- **Route A's cost is only HALF measured.** The tool is absent, so no end-to-end
  figure for `git filter-repo` exists here. What IS measured is its engine: the
  `fast-export | fast-import` pass, **261.4 s** and a **609.8 MB** target for
  these 8,230 commits (`artifacts/p3.log`) — the unavoidable floor, since
  filter-repo runs on that engine. The filter stage above it is **not** measured
  for Route A and no figure is invented for it; the Route B per-commit rate is
  the closest measured proxy, and it is labelled as a proxy.
- **The remove set was decided per path group, not file by file**, in this
  planning step. A group that contains a *recipe* (a `.sh`/`.py`/`.go` under
  `docs/archive/demo/` or `devdoc/evidence/`) loses it too. `docs/archive/demo/` alone is 1,295
  tracked files; the class-level rule used by scope-3 (artefact vs text-of-record
  vs recipe) must be applied **file by file** before the rewrite, and that is a
  task of its own — the largest open item in this plan.
- **`devdoc/DOC-INDEX.tsv` (519.0 MiB of history) stays.** Any expectation of a
  small post-rewrite repo must account for it: the §3.3 "after rewrite" pack
  figures include it, so ~50 MiB is the realistic blob mass — not zero.
- **`sandbox/` is 29 files in HEAD today**, including `sandbox/gopi-bin`. The
  rewrite removes their *history*; the index still carries them. Untracking them
  forward is **scope-3's already-landed half** (`f99c6392`, `67ddf5a3`) for the
  profile data and remains open for `sandbox/gopi-bin`.
- **The 90 stale hashes in commit messages** (the 2026-09-07 run's own record)
  will happen again, with a larger count: this repo's commit messages quote
  commit hashes constantly (merge commits especially). No rewrite tool
  rewrites message text; the count is not predictable from here.
- **A rewrite does not un-disclose anything already cloned.** HP's 468 MiB clone
  and the operator's own mirrors hold the blobs today; §6.2 removes them only
  after re-clone. If any secret ever entered those profile blobs, **rotate it** —
  that is scope-3's standing recommendation and this plan does not change it.

---

## 10. Verification

- Build: `CGO_ENABLED=0 go build -o …/gopi-01a0fd2e ./cmd/agent/` → exit 0,
  29,934,896 B binary. (The diff touches no Go source; the binary was built so
  the `doccheck` face below could be the SHIPPED one, not `go run`.)
- Tests: `make test-budget PKGS='./internal/standards ./internal/testutil
  ./internal/docindex'` — final state (after master was merged in) →
  `exit=1 outcome=fail` with **7** failures, all in `internal/testutil`
  (`TestPinCoverageTable`, `TestScriptsAreBoundedAndOffTmp`,
  `TestEveryTempUsingSuiteIsPinned`,
  `TestTestBudgetHasASingleSourceAndNamesATimeout`,
  `TestWorkerOwnershipPointerPresent`,
  `TestWorkerOwnershipGateStaysGreenAcrossSuccessiveMergeRounds`,
  `TestWorkerOwnershipSlashPairDocIsNotAnOffender`) — **and they are the BRANCH
  BASE's, not this diff's**, measured against a detached control worktree at the
  base commit `56e17d98` rather than asserted:
  - base `make test-budget PKGS='./internal/testutil'` → the **same 7**
    failures by name, `exit=1 outcome=fail`;
  - base `make test-budget PKGS='./internal/standards ./internal/docindex'` →
    `FAILED: github.com/nick/gopi/internal/standards` (`--- FAIL:
    TestRepoHygieneRealTreeIsBaselined`, `TestRepoHygieneCensusReDerivesTheTable`)
    — and **those two are GREEN in the merged run above**, because the base's
    stale baseline rows were exactly what master's `ca58d6d1` ("ratchet the Rule
    39 baseline down by the 13 permits whose files left the tree") removed; the
    merge of `ca58d6d1` into this branch is what carries that ratchet
    (`internal/standards/testdata/repo-hygiene-baseline.tsv | 13 ------`);
  - `go run internal/standards/testdata/repo-hygiene-census.go . …tsv --check`
    prints the **same 8 `-table/+tree (no row)` pairs at BOTH revs** (18 lines,
    exit 1) — the branch adds no census delta;
  - the branch's own diff is exactly two paths (this document +
    `devdoc/DOC-INDEX.tsv`); `repo-hygiene-baseline.tsv` is touched by the merge
    of master, never by this document's commit.
  The gate that *is* this diff's own obligation is Rule 36's, and it is green
  below.
- Ran: `…/gopi-01a0fd2e doccheck` (the binary built above) → `doccheck: the devdoc
  corpus is consistent — every file under devdoc/ has a well-formed row`, exit 0.
  The row did not exist at the first call: `doccheck` → `doccheck: file-with-no-row
  devdoc/repo-history-rewrite-plan.md — every file under devdoc/ must be
  classified (class + reason)` / `1 gap(s)`, then
  `gopi doccheck --fix` → `doccheck: repaired 1 path(s):
  devdoc/repo-history-rewrite-plan.md`, then the green call above. The
  Python face agrees: `python3 tools/docclass/classify.py . --check` →
  `0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv`.
- Ran: `git ls-tree -r HEAD --long | awk '{s+=$4} END{print s}'` → `272641025`
  (260.0 MiB) — the HEAD tracked mass the keep/remove split is taken from;
  the group split is `2000 files / 186107789 B` (remove set) vs `5316 files /
  86533236 B` (keep set).
- Ran: `git clone --mirror --no-hardlinks /home/user/work/GoPi …/mirror-probe`
  → `561899541` B, elapsed `12.55` s; `stat -c '%d %n' / /home /home/user/.gopi`
  → `2051 /, 2052 /home, 2052 /home/user/.gopi`; probe deleted afterwards
  (`ls -d` → *No such file or directory*).
- Ran: `bash artifacts/fb-range.sh 60` (throwaway clone) → `range_60_commits_seconds=60.02`,
  `per_commit_seconds=1.0004` over a **205**-commit range, `rewritten_commits=204`,
  `original_refs_kept=1` ⇒ **0.29 s/commit**. Cross-checked by
  `bash artifacts/wt-proof3.sh` (11-commit range) → `fb_seconds=2.86` ⇒
  **0.26 s/commit**. Both **throwaway clones**, never the shared repo.
- Ran: `bash artifacts/proofset.sh` (throwaway clone) → P2: `fb_rc=0`,
  `wt_HEAD_after=60d1b489…` ≠ `common_HEAD=ca58d6d1…`,
  `refs/original` found **under the COMMON `.git`**, and
  `new_commit_in_common_store=commit` — the §3.2/§3.3 mechanical proof.
- Result: 45 blobs / 359,269,609 B (342.6 MiB) ≥ 1 MiB, of which 4 /
  34,318,619 B (32.7 MiB) are **not reachable from HEAD**; remove set
  567,598,003 B (541.3 MiB) of blob mass; post-rewrite pack 52,726,678 B
  (50.3 MiB) for the HEAD closure and 52,848,265 B (50.4 MiB) over all refs;
  `git status --porcelain` in this worktree ends **empty** (Rule 30).
- Commit: the commit that carries this document — named in the delivery
  declaration of the run that produced it (the hash changes on every amend, so a
  hash written INTO the document is self-invalidating; the branch name is the
  stable address).

## Evidence (self-filed)

- `git rev-parse HEAD` in this worktree → `56e17d986066ee7704ef3e209bc856a1b08bd631`;
  `git rev-parse master` → `ca58d6d1c01e1f37294f35c9210f2c857d7d4777`.
- `du -sb /home/user/work/GoPi/.git` → `576749881`; `git count-objects -v` →
  `in-pack: 59218 / size-pack: 403361 / packs: 36`.
- `awk … | sort -nr` over `$A/ge1M-canonical.tsv` → 45 rows, 359,269,609 B; the
  four NOT-IN-HEAD rows are in `$A/ge1M-final.tsv` with `grep '^NOT-IN-HEAD'`.
- `git rev-list --objects HEAD | awk '{print $1}'` cross-checked against
  `$A/H-set.txt` → `8d46db42…` present (bin/gopi IS in HEAD's closure via
  history), `56769f05…` absent (gopi-behind is NOT).
- `stat -c '%d %n' / /home /home/user/.gopi` → `2051 / 2052 /home 2052
  /home/user/.gopi` — the Rule 22 partition proof.
- `git worktree list` → 6 entries at freeze-check time; `git for-each-ref | wc -l`
  → 526; `git reflog --all | wc -l` → 5,474.
- `command -v git-filter-repo` → rc 1; `ls /usr/lib/git-core/ | grep filter` →
  `git-filter-branch`.
- `ls -la /home/user/work/GoPi/.git/filter-repo/` → 6 files, mtime 2026-09-07
  14:44, `commit-map` 1,314 rows / 0 deletions.
- `git status --porcelain` in `/home/user/work/GoPi` → empty (read-only check).
