<!-- doc-class: normative -->
# The resolving effort of take-over: is it too large, and does the resolve lane develop?

Task: [3/4] of the epic — audit and record the MEASURED answer to the epic's
first question. Repo `/home/user/work/GoPi`, branch `docs/merge-resolve-effort-audit`,
worktree `/home/user/workspace/gopi-resolve-audit`, base master `8ef4ad54`.

**The epic's question has two halves. They have OPPOSITE answers, and the
difference is which surface you measure.**

| half | measured answer |
|---|---|
| Does **take-over's own** resolving effort stay large? | **NO — it is zero, and it was deliberately deleted.** The take-over tick has no resolve mechanism left to spend effort in: a non-ff branch is REPORTED and nothing else happens. |
| Does a **"resolve" lane** do development it should push back to the worker? | **YES — two live lanes still do exactly that, and one of them is measured doing it 19 times.** They live on the *adoption / merge-backlog* face, not the take-over face. |

Every claim below carries the command or `file:line` that produced it. The
measurements that decide the question are in §1 (take-over face), §2 (the live
lanes and the exact briefing they hand an executor), §3 (how much development
they actually do), §4 (route ratio), §6 (named dispositions).

---

## §1. The take-over face: the resolve machinery is GONE, not large

### 1.1 A non-ff branch is REPORTED — no row, no tag, no worktree touched

`internal/tools/repo_sweep.go:319-341` — `repoSweepNotFFOutcome` is *the tick's
WHOLE answer* to a non-fast-forwardable branch. Its own doc comment (lines
321-338) records that the retired park lane's three side effects — an
`archive/<branch>` tag, one ledger row, a forced worktree removal — were each
removed by epic `merger-remove-park` (2026-09-17).

`internal/tools/repo_sweep.go:1083-1090` is the call site, and it says so in
one sentence:

```
// ③ not ff-able: LEAVE IT ALONE and say so. The tick has no mechanism left
// that ACTS on such a branch — no archive tag (which would permanently remove
// it from every candidate set), no ledger row, no worktree removal.
return repoSweepNotFFOutcome(branch)
```

Corroboration: `internal/tools/repo_sweep.go` contains **zero** occurrences of
`Submit` (`grep -n "Submit\b" internal/tools/repo_sweep.go` → no output), i.e.
the sweep has no dispatch path at all.

### 1.2 The row vocabulary that survives is for OTHER ledgers' rows

`internal/tools/repo_resolve_row.go:8-25` states what remains after the resolve
machinery was deleted (epic `merger-ff-park-file`, 2026-09-16): row *markers*
(`merge-conflict` + `auto-spawn`) whose only surviving purpose is to keep rows
written by **older binaries** out of the worker dispatch
(`internal/tools/repo_resolve_row.go:257` → `resolveRowIsMergerOwned`).

Measured that this is memory and not a producer:

- `grep -rn "resolveRowTagAutoSpawn\|auto-spawn" --include=*.go internal cmd`
  (excluding tests) → the constant is defined at `repo_resolve_row.go:43` and
  read at `:155` (the matcher); **no writer** exists.
- The ledger agrees, by date: rows tagged `auto-spawn` (n=60) span
  **09-10 … 09-16** and stop. Rows tagged `auto-dispatch` — the LIVE lanes'
  marker — continue through **09-17 (51 rows)** and 09-16 (21).
- The resolve rows written by the merger also stopped: the newest row of the
  merger's own shape is `01a0af55` (09-17 20:26), *cancelled*, reason
  `superseded: feat/branch-lifecycle-ruling was resolved and merged by task 01a0af5…`.

### 1.3 The take-over prompt hands the work OFF rather than doing it

`cmd/agent/merger_takeover_prompt.md:399` (§3.6, "A branch that cannot be
merged → a rework TASK, never a tag") directs the tick to **file a rework task
with the diff as an attachment and delete the branch** — the payload says the
reworker *"starts a FRESH branch from current master"* and *"Do not `git apply`
the diff"* (lines 425-431). The tick therefore does **no** conflict resolution
of its own: its budget goes to a diff + a submit.

Route ratio for take-over's rework lane: **4 rows** carry the `rework` tag
(`grep`-equivalent query over `tasks`), of which 2 cancelled, 2 pending; the
newest is `01a0b01d` (09-18 00:05, `rework fix/merger-loop-restart…`).

**Verdict for §1: take-over's resolving effort is NOT too large — it is zero by
construction, and the machinery that used to make it large (`resolve child`,
row-per-resolve tracking, park tag) is deleted, not idle.** The remaining cost
is one `git` probe per branch per tick
(`internal/tools/repo_sweep.go:338`: *"one git probe per branch"*).

---

## §2. The two LIVE lanes that still instruct an executor to RESOLVE

Both write the same deterministic summary and both hand a *self-contained
conflict-resolution briefing* to a fresh worker session.

### 2.1 Lane A — `gopi task merge` conflict dispatch (post-receive hook / serve)

- Writer: `internal/tools/task_merge.go:657` `taskMergeDispatchConflict`,
  summary rendered at `:672` from the constant
  `taskMergeSummaryVerb = "resolve merge conflict: %s into master"`
  (`internal/tools/task_merge.go:40`).
- Call site: `internal/tools/task_merge.go:381` — reached only when
  `opts.Submit` is true (`:379-380` returns report-only when it is not).
- Briefing: `internal/tools/task_merge.go:703` `taskMergeConflictPayload`. The
  instruction text (verified verbatim in the ledger payload of `01a0a7d4`,
  tags `merge-conflict,auto-spawn`):

  > `Task: merge this branch into master, resolving conflicts in your own`
  > `session (read the conflict hunks, choose sides, verify the result).`
  > `Acceptance: the merge completes, the project builds, the test suite passes.`

- Faces that run it with `Submit: true`:
  `cmd/agent/cli_tasks_merge.go:28` (`gopi task merge --submit`) and
  `cmd/agent/cli_tasks.go:4114` (`serveMergeBacklogAudit`).
- **`serveMergeBacklogAudit` is DEAD in production.** `grep -rn
  "serveMergeBacklogAudit" --include=*.go cmd internal | grep -v _test.go`
  returns only its own definition (`cli_tasks.go:4092`, `:4100`) — no caller.
  Its two test callers remain (`cmd/agent/cli_tasks_audit_test.go:57,65`).
  It lost its call site in `d11af9a6` (2026-09-04, *"merger pass is
  config-driven per-repo sweep — retire merge-backlog row pipeline from the
  pass"*): `git show d11af9a6 -- cmd/agent/cli_tasks.go` shows
  `-	if n := serveMergeBacklogAudit(); n > 0 {`.
- **The post-receive hook does NOT reach lane A.** `.git/hooks/post-receive:15`
  runs `bin/gopi task audit-merges --repo … --submit`, and that face is
  `runTaskAuditMerges` → `tools.AuditMergeBacklog`
  (`cmd/agent/cli_tasks.go:5428-5439`), whose summary verb is
  `merge <branch> into master` (`internal/tools/task_merge_backlog.go:29`) —
  a merge-backlog row, not a `resolve merge conflict:` row.
- **Therefore lane A's ONLY live trigger is the manual CLI**
  (`gopi task merge --submit`). Every `resolve merge conflict:` row that carries
  lane A's `Task: merge this branch into …` shape (n=10, §2.3) came from an
  operator or agent running that command by hand, not from a tick or a hook.

### 2.2 Lane B — adoption conflict dispatch (serve tick / cross-node channel)

- Writer: `internal/tools/task_adoption.go:491` `adoptionDispatchConflict`;
  summary literal at `:517` — `"resolve merge conflict: " + br.Name + " into master"`
  (the doc at `internal/tools/task_merge.go:38-40` calls this *"the dedup key
  shared with adoptionDispatchConflict's spelling"*).
- Call site: `internal/tools/task_adoption.go:363` inside `AdoptionSweep`.
- Briefing: it appends the **conflict hunks themselves** —
  `internal/tools/task_adoption_conflict.go:64` `adoptionMergeConflictBriefing`
  walks `<<<<<<<` … `>>>>>>>` blocks per conflicted file, capped at
  `adoptionHunkCap = 40` lines per hunk (`:60`). Its acceptance text
  (`task_adoption.go:511-513`): *"Acceptance: the merge into master completes,
  the project builds and the test suite passes. Work in your own
  session/branch, push back when green — the server adopts the branch
  automatically on a later serve tick."*
- Live faces: `cmd/agent/cli_tasks_adopt.go:114` (`serveAdoptionSweep`, called
  from the merger tick at `cmd/agent/cli_tasks.go:2726`) and
  `internal/tools/task_sync_git.go:279` (the cross-node sync pass).

### 2.3 What a "resolve" row is ACTUALLY instructed to do — three payload shapes

Measured over every row whose summary starts `resolve merge conflict:`
(n=86), splitting by payload shape:

| payload shape | rows | writer | summary of the instruction |
|---|---|---|---|
| `Conflict resolution: …` | 9 | Lane B (live) | conflict file list + `--stat` + **the hunks** + branch intent from the ledger + "Work in your own session/branch, push back when green" |
| `Task: merge this branch into …` | 10 | Lane A (live) | tip + changed files + `--stat` + "resolving conflicts in your own session … the merge completes, the project builds, the test suite passes" |
| legacy resolver prompt (`You are the merger's conflict resolver for branch …`) | 67 | retired merger lane | reproduce the merge, read each hunk, **decide for each — combine both sides where both are meaningful, do not silently drop either side's intent**, edit, `git add`, **commit first**, then a bounded build+test |

The third shape is where the historical **development** happened, and it is the
sharpest statement of the problem the epic asks about: the instruction is not
"decide which side wins and move on" — it is *"combine both sides where both are
meaningful"*, i.e. **author new code inside the merge**. That shape's writer no
longer exists (§1), but the two live lanes above still route real work to a
conflict-resolving executor rather than back to the branch's author.

---

## §3. Measured development done inside resolve commits

Instrument: a resolve commit is a merge commit whose message is
`resolve merge … into master`. For each one the base is
`git merge-base <p1> <p2>`, the conflict-candidate paths are
`(diff base→p1) ∩ (diff base→p2)`, and "authored in the resolve" is a path that
differs from **both** parents. Upper bound on textual conflict volume is
`Σ_paths min(lines_changed_side1, lines_changed_side2)`.

```
git log --oneline --grep="^resolve merge " | wc -l                     -> 71
```

Census (`/tmp/conflict_volume.txt`, `/tmp/handlines.txt`):

| measure | value |
|---|---|
| resolve merge commits measured | **71** |
| conflict-candidate paths (changed on both sides) | sum 238, median 2, max 17 |
| paths differing from BOTH parents (hand-authored) | 11 of 71 commits have none; the rest 1-7 paths each |
| hand-authored lines in conflicting files | sum 10 854, median **93**, q3 202, max 785 |
| upper-bound conflict volume (`min(±)` per path) | sum 12 246, median **50**, p90 454, max 1 594 |
| commits where the bound is 0 (already-integrated merge) | 6 of 71 |

Distribution of the hand-authored figure: 14 commits ≤20 lines, 22 commits
21-100, **35 commits >100 lines**.

Sample sizes, largest first
(`git log -1 --format='%h %s'` on each `/tmp/conflict_size.txt` row):
`6a105a48` `feat/worker-ownership-write-path` 1 594 lines; `d9b3f6ad`
`fix/worker-log-session-attribution` 1 541; `faf5f219`
`backup/peakhours-exact-on-ec6ba9f1` 1 196. Smallest: `2190b417`,
`55296ffb`, `6761cad8` — 0.

**This is the measurement the epic asked for: a resolve row is not a
formality.** The median resolve commit authored ~50-93 lines in files that
conflicted on both sides, and in three cases the resolved file was rewritten far
beyond the conflict (the branch-vs-parent diff in those paths is what the
1 594-line row bought). The clearest single case is recorded on master itself:
`task 01a09fc2` (`tasks/01a09fc2-report.md:7-35`) — *"The conflict was found and
resolved semantically, not by side-picking"* — the resolver wrote a **union
predicate** (`spendingStart(args) && spendingStart(dispatchArgs)`), **removed**
the branch's third slice as redundant, and **re-measured** a standards baseline
key rather than borrowing either parent's (`grep -n 'escape a literal prompt'
cmd/agent/main.go` → one hit, line 990). That is feature development, performed
inside a conflict resolver.

### 3.1 The merge that says the whole thing: 7 resolve rows for ONE branch

`feat/merger-always-rebuilds` was resolved **7 times** (rows `01a0a72f`,
`01a0a736`, `01a0a74c`, `01a0a762`, `01a0a7b2`, `01a0a7ca`, `01a0a7d4`, all
`completed`, 09-16 06:27 → 09:28), and the branch finally landed at 09:35 as
`09ebf131 resolve merge feat/merger-always-rebuilds into master`. Its second
parent is `46838d9b` — a *merge of master into the branch*, which is what the
branch's owner was doing while seven resolve rows kept being minted for it.

Per-branch re-minting across the whole family: **8 branches have >1 resolve
row, accounting for 16 of the 86 rows** (the branch list is in the census
output; `feat/worker-graceful-drain` has 5, `feat/merger-always-rebuilds` 7).

Row wall time (created→ended): median **450 s**, 55 rows under 15 min, 8 in
15-60 min, **23 rows over one hour**, max 19 h.

---

## §4. Route ratio: how many branches take which route

Distinct branches (71 resolve commits vs 70 distinct branch names in the
ledger is a naming-vs-scope artifact of `...into master` summaries; the ledger
figure 70 is the one I use for rows).

| route | distinct branches | rows | source |
|---|---|---|---|
| resolve route (a `resolve merge conflict:` row) | **70** | 86 (74 completed / 9 failed / 3 cancelled) | ledger query, §2.3 |
| rework route (branch-rework lane, `resolve merge conflict: rework:` prefix) | **23** | 28 (21 completed / 5 failed / 2 cancelled) | `internal/tools/repo_resolve_row.go:57`, `reworkSummaryLive` (`internal/tools/task_merge.go:961`) |
| take-over's own rework filing (`--tags rework,merge`) | **4 rows** (2 cancelled, 2 pending) | — | `cmd/agent/merger_takeover_prompt.md:399-431` |
| merge-backlog route (`merge <branch> into master`, executed by Go infra) | — | — | `internal/tools/task_merge_backlog.go:29` |

Take-over filings are effectively **one-shot**: the prompt's step 4 says
*"file the rework task and DELETE the branch"* — the branch leaves the sweep set
the same tick (`cmd/agent/merger_takeover_prompt.md:415-431`), so §1.3's 4 rows
are the whole take-over route, while resolve rows grew to 86.

**Resolve rows outnumber take-over rework rows 86 : 4, and the resolve route is
the ONLY route whose rows were re-minted for the same branch (16 extra rows).**
The difference is structural, not accidental: a resolve row's branch is
*deleted by the merger after landing*, but nothing deletes a *re-minted* row —
and lane A/B file a fresh row whenever the branch is seen unmergeable again
while no live row exists (`adoptionConflictTaskLive`, `task_adoption.go:470`;
`task_merge.go:673-681` — the dedup is live-status only).

### 4.1 The resolve rows' branches are all gone — and that is the design

```
python3 (query 70 distinct branches → git rev-parse --verify) :
  distinct resolve branches: 70 | ref exists: 0 | deleted/absent: 70
```

Distinct-branch naming aside, what this measures is that **the resolve route
consumes its branch in every case**: nothing survives to be re-probed, and yet
8 branches produced more than one row — so the re-minting happened *before* the
landing, while the row was terminal (completed in 16 cases) or the branch was
re-observed. That is the same defect class the take-over prompt's §3.6.1 case ④
was written to kill: *"a fresh row per tick for the same branch"*
(`cmd/agent/merger_takeover_prompt.md:586`).

---

## §5. Compliance with the lifecycle ruling on master

`devdoc/branch-lifecycle-ruling.md` (landed `1e5a3f29`, 2026-09-17 20:26)
ruled that a branch has exactly two states and that a "conflict/" namespace or
tag is void. On master today:

- `git tag | grep -c '^archive/'` → **160** archive tags on this repo — history
  only; `repo_resolve_row.go:70-92` (`repoSweepArchiveBranch`) documents that
  the merge-time compile gate is the *only* remaining creator, and that the
  take-over path needs its ref alive and **untagged**
  (`cmd/agent/merger_takeover_prompt.md:252-258`: *"Archive tags are history,
  never a filter"*).
- `serveMergeBacklogAudit` is dead code (§2.1) — a lane that could still mint
  rows, reachable only from tests.
- The park lane's summary writer
  (`parkTaskSummaryRowIdentityFmt`) is **no longer in the tree**:
  `grep -rn "parkTaskSummaryRowIdentityFmt\|parkTaskSummaryFmt" --include=*.go
  internal cmd` → no output. Its 71 historical rows (`resolve merge conflict for
  …`) are 65 cancelled (reason `superseded: the park/resolve-conflict mechanism
  is retired`), 6 completed.

Compliance is therefore **clean on the take-over face and the park face**, and
the only live resolve lanes are the two named in §2, which are adoption/hook
face — they were never in the ruling's scope.

---

## §6. Named disposition for each finding

| # | finding | evidence | disposition |
|---|---|---|---|
| F1 | Take-over's own conflict resolution is deleted; a non-ff branch is reported with no row/tag/worktree side effect. | `repo_sweep.go:319-341`, `:1083-1090`; zero `Submit` in `repo_sweep.go`; `auto-spawn` rows stop 09-16 | **KEEP** — this is the state the epic wanted; do not re-add a resolve mechanism to the tick. |
| F2 | Take-over's rework route is one-shot and small (4 rows, 2 cancelled). | `merger_takeover_prompt.md:399-431`, `§3.6.1` case ④ | **KEEP** — §3.6.1 case ④ (diff → queued → delete) is what stops the loop; the 7-row re-mint below is the counter-example that proves it is needed. |
| F3 | **Lane A** (`gopi task merge` conflict dispatch) hands a fresh worker a full conflict-resolution job at merge time, with "the test suite passes" as its acceptance. | `task_merge.go:40`, `:657`, `:672`, `:703`; verbatim payload of `01a0a7d4`; `cli_tasks_merge.go:28` | **KEEP** — its only live trigger is an operator's deliberate `gopi task merge --submit`; the automatic trigger is dead (F4). |
| F4 | `serveMergeBacklogAudit` (the only automatic lane-A trigger) has **no production caller** — dead code kept green by its own tests, and it lost its call site in `d11af9a6` (2026-09-04). | `grep -rn "serveMergeBacklogAudit" … \| grep -v _test` → definition only; callers only at `cli_tasks_audit_test.go:57,65`; `git show d11af9a6 -- cmd/agent/cli_tasks.go` → `-	if n := serveMergeBacklogAudit(); n > 0 {` | **DISPATCH** (filed, see §7) — delete it with its tests, or wire it deliberately; dead automatic dispatch that looks live is the defect class this audit is about. |
| F5 | **Lane B** (adoption conflict dispatch) is live on every merger tick and every sync pass, and it embeds the conflict hunks so the executor can start editing immediately. | `task_adoption.go:491`, `:517`, `:363`; `task_adoption_conflict.go:60`, `:64`; `cli_tasks_adopt.go:114` → `cli_tasks.go:2726`; `task_sync_git.go:279` | **DISPATCH** (filed, see §7) — decide whether merge-time conflict resolution belongs to the branch's author (rework route, §3.6) or to a fresh adoption worker; today the two routes coexist with no rule choosing between them. |
| F6 | Resolve rows are re-minted for the same branch: 8 branches, 16 extra rows; one branch took 7 rows in 3 h. | `/tmp/conflict_volume.txt` census; rows `01a0a72f…01a0a7d4`; dedup is live-status-only at `task_merge.go:673-681` / `task_adoption.go:470` | **TIGHTEN** — needs the terminal-row identity the take-over prompt already uses (`reworkSummaryLive`, `internal/tools/task_merge.go:961`); filed with F5 because the fix belongs to whoever owns the lane. |
| F7 | A resolve commit is not a formality: median ~50-93 authored lines, 35 of 71 commits >100 lines, one case rewriting a predicate and re-measuring a baseline key. | §3 census; `tasks/01a09fc2-report.md:7-35` | **DISPATCH** — this is the substance of the epic's second half; the measurement is handed to the task that owns the route decision (F5). |
| F8 | The 160 `archive/*` tags and the retired park rows are history, not live state — no live writer, all park rows cancelled/superseded. | `git tag \| grep -c '^archive/'` → 160; park-summary writers absent from the tree; 65/71 rows cancelled with `superseded: the park/resolve-conflict mechanism is retired` | **KEEP** — documented as history in `repo_resolve_row.go:8-25` and `merger_takeover_prompt.md:252-258`; no cleanup owed (Rule 9 keeps archived losers). |
| F9 | `resolveRowTagAutoSpawn` is defined and matched, written by nobody. | `repo_resolve_row.go:43`, `:155`; no writer in `internal/`+`cmd/` non-test | **KEEP** — its stated purpose (`repo_resolve_row.go:19-25`) is to keep **older binaries'** rows out of the worker queue; deleting it would re-open that hole for historical ledgers. |

---

## §7. Gaps filed (no "noted for later")

Two gaps are OUT of this branch's scope by construction — both are live
dispatch/route POLICY on the adoption face (system layer, peer1), and neither
can be fixed by editing a document:

- **F4 + F5 + F6 + F7** are filed as **task `01a0b1b3-0499-7ee4-911e-55b2d32fe620`**
  (`priority=3`, tags `merger,adoption,resolve-lane,rework-route,system-layer`),
  submitted from this session via `task_submit`; the row carries the full
  self-contained briefing — the file:line evidence of §1-§3, the measured
  86:4 ratio, the 7-row re-mint case, and the decision the fix must make.

Everything else was either verified as already-correct (F1, F2, F8, F9 — no
change needed) or is a KEEP with its reason recorded above.

## §7.1 Not Done (honest)

- **The route decision itself is NOT implemented here.** Both live lanes are
  untouched: the branch is a doc-only audit, and F4/F5/F6 are live dispatch
  policy on the adoption/serve face (Rule 28 — the system layer belongs to the
  system host), so they are dispatched rather than edited. This branch does not
  claim to have fixed the 86:4 asymmetry; it measures it and hands over the
  decision.
- **F6's fix is named, not written.** The re-minting repair needs the lane
  owner's choice of terminal-row identity (DISPATCH above); no test in this
  branch asserts the 7-row case is impossible, because nothing in this branch
  changes that behaviour.
- **Rows tagged `auto-dispatch` were counted but not individually classified.**
  §1.2/§4 use them only as a date-boundary signal (they continue past 09-16,
  `auto-spawn` does not); a per-row classification of all 72 `auto-dispatch`
  rows was not done, so the §4 table's "rows" figures are for the
  `resolve merge conflict:` summary family only.
- **`/tmp` census scratch files are not re-runnable from the committed doc.**
  §9 gives the commands; the raw census outputs (`/tmp/conflict_volume.txt`,
  `/tmp/conflict_size.txt`) lived outside the worktree and are not attached.

## §8. Verification (this branch — measured, not asserted)

```
# this run, worktree /home/user/workspace/gopi-resolve-audit
CGO_ENABLED=0 go build ./...                                        -> exit 0 (no output)
go test ./internal/tools/ \
  -run 'Adoption|ResolveRow|RepoResolve|TaskMerge|RepoSweepNotFF' -count=1
                                                                    -> ok  github.com/the operator/gopi/internal/tools  77.786s
git status --porcelain                                              -> (empty before this doc's edit)
```

Doc-only branch: no production file is modified, so no new gate is needed;
the two commands above also confirm the facts the doc cites are still true on
this base.

## §8.1 Files

- `devdoc/merge-resolve-effort-audit.md` (this file) — the only file changed.
  No production code, no test, no constant is touched by this branch; every
  finding is either KEEP (verified already-correct) or dispatched (§7).

## §9. Evidence commands (re-runnable)

```bash
# census of resolver merge commits (§3)
git log --oneline --grep="^resolve merge " | wc -l                      # 71
# per-commit conflict volume (median 50, p90 454, max 1594)
#   base=merge-base p1 p2 ; both=(base..p1 ∩ base..p2) ; hand=differs from both parents
# ledger census (§2.3 §4)
sqlite3 -readonly ~/.gopi/memory.db \
  "select status,count(*) from tasks where summary like 'resolve merge conflict:%' group by 1"
# dead lane (§2.1 / F4)
grep -rn "serveMergeBacklogAudit" --include=*.go cmd internal | grep -v _test.go
# live lanes (§2)
grep -rn "func adoptionDispatchConflict\|func taskMergeDispatchConflict" --include=*.go internal/tools
```
