<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# Ground-lost strand — 30 rows failed "cannot resolve the ground" because they were born naming their submitter's worktree

**Task**: 01a113a9-108e-75ae-a974-f5ed222b5cd3 · **Date**: 2026-10-07 · **Host**: peerbox
**Scope**: diagnosis of the 30-row strand, the producer-side fix, and the per-row disposition.

## 0. What the strand is

30 ledger rows carry a `reason` whose text is the ground resolution's own refusal:

```
gopi task: <id>: cannot resolve the ground for task <id> in repo
/home/user/workspace/gopi-<hex>: the repo is not a git work tree (or git cannot be
read), so its main checkout cannot be told from a task ground — refusing to start
the run in the repo's main checkout;
remedy: re-point the row at a live git work tree (gopi task update <task_id> --repo <path-to-live-work-tree>)
```

Measured classification of that exact phrase (`memory.db`, read-only):

| class | n | meaning |
|---|---|---|
| **RAW-REFUSAL** (`reason` carries "refusing to start the run") | 29 | the row itself was refused: `failed`, never started |
| PROSE-MENTION (a DIFFERENT row quoting it in its own cancellation note) | 1 | `01a10ec5` (`cancelled`) — it is the *parent* of a refused child and explains it |

So the strand is **29 failed rows + 1 cancelled dependent**. 15 of the 29 are rows where the work had never happened: written the same second as the claim, they never got far enough to produce a report.

## 1. Q1 — who reclaimed the worktree, and when

**Instrument**: `/home/user/.gopi/logs/removal-audit.log` (append-only, one JSON record per removal, carrying `arm`, `pid`, `task_id`, `target`, `evidence`).

Every one of the 25 distinct vanished paths has a `worktree-removal` record — **0 without one** (`artifacts/q1-removal-who.tsv`):

| arm | n (of 25) | what it is |
|---|---|---|
| `finish-cleanup-worktree` | 18 | the OWNER run's own close-out chain |
| `task-self-reclaim` | 4 | a finished run recycling its own ground |
| `task-self-own-sweep` | 3 | the owner-independent pass over this process's own finished rows |

Verbatim evidence line (background, arm, task, worktree) — the shape of all of them:

```json
{"kind":"worktree-removal","arm":"finish-cleanup-worktree","task_id":"01a11004-3f0e-7999-936a-dfcbcdb7dbb9",
 "target":"/home/user/workspace/gopi-01a110043f0e7999936adfcbcdb7dbb9",
 "evidence":"delivery chain's local sync landed: branch already contains the integration branch — nothing to merge, no rebuild; checklist own-ground + integration-lineage (reclaim_ground_checklist.go)"}
```

**Answer**: the ground was removed by its OWN owner's close-out, at the normal end of that run. No premature sweep, no arm freeing somebody else's live ground. The removal was correct — which is the first hint that the defect is not in the reclaimer.

Full census over the 5263 removal records in this log, so the three arms above are read in context:

```
1839 merge-run-probe-ground      976 task-ground-anchor      279 task-self-own-sweep      7 task-row-reclaim
1296 merge-build-gate-ground     675 finish-cleanup-worktree 188 task-self-reclaim        3 worktree-sweep-reclaim
```

## 2. Q2 — was there any retry?

`task_events`, per row (`artifacts/q2-event-sequence.tsv`):

```
claims(=in_progress|claimed events) :  1 claim = 29 rows,  0 claims = 1 row (the cancelled one)
rows with more than one claim        :  0
```

Every row has the same three-beat life: `in_progress` → `failed` (the run's own writeback) → `failed` with the refusal text. **No row was ever retried**, and that is not an accident of these rows:

- `internal/tools/taskreissue.go:17` — *"WHAT THIS FILE IS, AND IS NOT. It is VISIBILITY, not retry"*.
- `gopi task list --status failed` ends with `failed-needs-reissue: 254 failed row(s), no successor row and no "resolved" tag — reissue or tag them`.

**Answer**: retry is a deliberate non-feature; the failed state is terminal and its exit is a human/PA decision. So candidate fix **C (auto-retry on failure) is not available** — building it would be inventing machinery the ledger refuses on purpose — and a row that fails from a bookkeeping refusal has no path back other than somebody reading it. That is exactly why the fix has to be on the producer side.

## 3. Q3 — what does `deliverable.repo` actually store?

26 distinct values across the 30 rows (`artifacts/q3-repo-shape.txt`):

| shape | n | example |
|---|---|---|
| **GROUND-PATH** `…/workspace/gopi-<32hex>` — a run-scoped linked worktree | 23 | `/home/user/workspace/gopi-01a110043f0e7999936adfcbcdb7dbb9` |
| STALE-NAMED-WORKTREE | 1 | `/home/user/workspace/gopi-g1-tasksurface` |
| MAIN-CHECKOUT | 1 | `/home/user/work/GoPi` (`01a0c7b4`, the earliest failure, 2026-09-22) |

Re-measured today: **28 of the 30 paths are GONE, 2 are ALIVE** (`artifacts/q1-final.tsv`).

**So the `--repo` remedy the refusal itself prints is exactly the right one, and it was never run.** Nothing re-reads a failed row — the same fact as Q2. The stored value is a **worktree path**, which is the whole defect: the ground's lifetime is one run, the row's lifetime is forever.

**The generator, measured.** `TaskSubmit`'s **explicit** arm stamped `opts.Repo` verbatim (`internal/tools/tasks.go`), while the **default** arm has gone through `DurableRepoForSubmit` since epic 01a0d55b. The session log of row `01a1103b`'s submitter proves which arm fired:

```
session 01a11004…/a739aece-….jsonl  →  task_submit tool_use:
  repo = "/home/user/workspace/gopi-01a110043f0e7999936adfcbcdb7dbb9"
```

and that directory is the submitter's own `deliverable.worktree` — removed by the audit record quoted in §1 **5 minutes after** the child was filed (`01a11004` removed 16:06:49Z; child `01a1103b` created 16:01:38Z, failed 16:07:03Z). The child was born naming a directory its own parent was about to delete.

Parent/child join, all five rows that have a parent (`artifacts/q1-owner-join.tsv`): in 4 of 5 the child's `repo` **equals the parent's `worktree`**, i.e. the defect's fingerprint; in 1 (`01a10ec5`) the child's `repo` equals the *grandparent's* worktree.

## 4. Q4 — did the reclaim path check for rows still pointing at the directory?

Read `internal/tools/reclaim_ground_checklist.go` — the ONE checklist every removal arm routes through. It has **exactly two rungs**: ① "the ground is not mine" (this process's own checkout), ② "the branch shares history with the integration branch". Neither asks *"does any ledger row name this directory?"* and no SQL in `task_reclaim*.go` / `worktree_sweep*.go` / `task_finish_cleanup.go` reads `deliverable` at all.

What the arms DO ask is the *liveness* question — a live holder process, a live owning row — which keeps a ground alive while a run is working in it (`finishCleanupWorktreeInUse`, `reclaimLiveSessionRefusal`, `worktreeSweepSessionLiveEvidence`). Nothing keeps it alive for a row that merely *points at* it.

**Answer: NO.** And that is the root cause: the producer minted a reference to a run-scoped directory, and the reclaimer — correctly — has no reason to look for references.

## 5. Root cause (5-why)

1. A row is permanently `failed` with "cannot resolve the ground".
2. Its `deliverable.repo` names a directory that no longer exists.
3. A submitter running inside its own GROUND dispatched that ground as the row's repo (the explicit arm had no durable resolution; rows predating epic 01a0d55b got the same shape through the default arm).
4. The owner's finish-cleanup removed that ground at its own close-out — normally, as designed.
5. **Neither boundary asks the question that would stop it**: the producer does not normalize; the reclaim arms do not look for referencing rows.

The name "worktree 被回收后任务永久 failed" describes the symptom. The mechanism is *a row was born naming something whose life is one run*.

## 6. The fix — the producer, argued

Candidates, and why the choice is A′:

| candidate | verdict |
|---|---|
| **A. refuse to reclaim while a row points at the ground** | **rejected.** Reclamation is the only thing that recycles worktrees, and it is correct: 28 of the 30 referrers will never run again. Blocking on "some row mentions this path" would stop finish-cleanup for dead rows — trading a strand for a landfill. There is no live owner on the other side to protect: the referrer is a *failed row*, not a running one. |
| **B. self-heal at claim time (rewrite the row's repo when the ground is missing)** | **deliberately NOT built.** With the producer fixed, a fresh row cannot carry a dead ground; for the 30 already stranded the rewrite is a write into an operator's ledger on an inference. The repo already has the ONE sanctioned door for that write (`gopi task update <id> --repo <path>`, the remedy the refusal prints), and §7 uses it. |
| **C. auto-retry on failure** | **not available** — retry is a deliberate non-feature (§2). |
| **A′. the producer never stamps a run-scoped path** | **chosen.** It is the only boundary where the correct value is *known* rather than inferred, it needs no new machinery (the ONE resolution already exists: `DurableRepoForSubmit` → `git rev-parse --git-common-dir` via `mergeExecMainRepo`), and it makes the invariant structural: a row can no longer be born naming a directory whose life is one run. |

Implemented in `internal/tools/tasks.go` (`TaskSubmitReturningID`): the explicit arm now runs through the same ONE durable resolution the default arm uses, and — Rule 15 — **reports the substitution on stderr** rather than silently relocating the caller's argument. A main checkout passed explicitly is IDENTITY (`SameDir` guard), so no already-durable row is restamped.

**Does the strand survive the fix?** For rows submitted from now on: **no** — measured with the real binary, red→green, §8. For the 30 already stranded: the mechanism cannot reach them (a failed row is not re-read), which is why they are dispositioned by hand in §7.

## 7. Disposition of the 30

Instruments: exact re-measurement of each row's named test today (real `go test`, §8), a successor search over the ledger (later `completed`/`verified` rows whose summary shares ≥2 distinctive tokens with the stranded row), and the "is it still live" read.

### 7a. The three verb-retirement rows (the briefing's minimum)

**Verdict: all three still live, all three remain unclaimed by any other row, and the parent chain's own cancelled child proves the mechanism.**

| row | row-id's claim | measured today | verdict |
|---|---|---|---|
| `01a1103b` (parent of `01a10ec5`) | "package cmd/agent has 14 failing tests on pristine master e8de1da96" | `go test ./cmd/agent -run <the 12 named Test…> -v` → **9 FAIL** (`artifacts/verb-retirement-14-today.log`): `TestAnnotateVerbRunsARealArgvEndToEnd`, `TestBashScriptStructure`, `TestChatStatus_LiveBlockKeepsTheStateWord`, `TestKeywordSearchHelpPageDiffersFromTopLevel`, `TestKeywordSearchRunPrintsHitsAndNoMatches`, `TestSandboxDirFlagDecidesTheRoot`, `TestToolGroupForwardsToTheSameImplementation`, `TestToolGroupWordsAreTheOnlyDoor`, `TestUndispatchedToolWordsNameADispatchingCanonical` | **NOT superseded — reissue** |
| `01a10e4d` | "finish the verb-retirement migration in cmd/agent's own tests" | a successor row exists and completed: **`01a10e8b-e525` "verb-retirement: drive the remaining test argv through the family doors (121 red names in cmd/agent)" (completed 2026-10-06 08:10)**, filed 8 minutes after this row was refused; the 9 reds above are the残 remainder it did not reach | **partially superseded; the remainder is reissued under `01a113a8-91d0`** (pending, same subject) |
| `01a10ead` | "G6 help-surface inventory family: derivation script passes retired flat verbs" | **still live, verbatim**: `TestToolsMaintenanceHelpSurfaceInventoryMatchesDerivation` FAIL + `…EveryFormIsTestedOrBlockedWithAReason` FAIL (`artifacts/g6-today.log`); the binary still refuses all seven flat words the script drives (`vision_analyze text2img screen_capture act voice_input shot code-search` → "RETIRED — use `gopi tool …`"), `scripts/g6-tools-maintenance-help-surface.sh:61 G6_VERBS` unchanged | **NOT superseded — reissue** |

`01a10ec5` (the cancelled child) **stays cancelled**: its own cancellation note already records that its parent never ran and that the 13 reds it claimed were closed elsewhere (by `01a113e3`'s and `01a113a8-89d9`'s families).

### 7b. The remaining rows — and the line the disposition draws

The successor search (`artifacts/q4-supersession.tsv`) matched **17 of 30** rows to a later `completed`/`verified` row naming the same test or artifact. Where the successor's surface still exists, the subject was **re-measured today** with the real suite; the ones verified green are:

```
TestDocClassBannerMatchesManifest              ok  internal/standards
TestWorkerReleaseOwnWorktree_Acceptance        ok  cmd/agent
TestCovToolsNotifyAlarmRegistryIsTheSourceOfTruth  ok  internal/tools
TestTask_BarePrompt_SIGTERM_WritesSessionEnd   ok  testsuite
TestAgent_WallclockCap_TruncatedRunIsNotCompleted  ok  internal/agent
os.Unsetenv Rule-17 site (tasks_test.go)       now carries  // env-leak-ok: per-call flip  + testutil.SnapshotEnv  → the gate is green
python3 tools/docclass/classify.py . --check   → 0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv
```

**The disposition is exactly two-sided, and the line between them is "is the subject still live":**

- **ARCHIVED `resolved` — 17 rows.** Either a successor row carries the subject and its surface is green today, or the subject was re-measured and is gone. Archived through the ONE write door `gopi task archive <id>`, which records the `resolved` tag **and preserves the original `failed` status** — the historical fact is not erased, only superseded (verified by read-back: `status=failed, tags=bug,…,worker:peerbox,resolved`; `gopi task list --status failed` now reports `[hidden] 83 archived row(s) hidden — tags contain "resolved"`).
- **LEFT `failed` — 12 rows, and this is a deliberate refusal, not an omission.** They are the ones with **no successor and whose subject I could not show to be resolved**: `01a0ce62`, `01a0ce7e`, `01a0ceef`, `01a0d31e`, `01a0d366`, `01a0e1c9`, `01a0e313`, `01a0ef8a`, `01a0f506`, `01a0f765`, plus the two rows **reissued** below (`01a1103b`, `01a10ead` — their new rows carry the subject, and each stranded original is named in its successor's payload so the trail is followable). `01a10ec5` was **already `cancelled`** before this run and stays so. Two measured examples of why these are *not* simply "old, therefore done":
  - `01a0ef8a` ("re-render the remaining 142 素材's 04") — the material directory `scripts/testdata/materials-render-D/` holds **4 files / 2 entries** (`001`, `003`), **zero `04…`**. The subject is plainly **not done**.
  - `01a0cd4a`'s successor gate `TestDocAnchors…` is **RED today** (`internal/agent`, 14 stale permits) — a row whose family is still red was archived only under the successor's own, narrower subject.
  Marking these `resolved` because they are old would be the Rule-26 failure mode in the other direction: **an overstated fix closes the ticket and hides the defect.** They stay visible in the failed list, which is this project's standing queue for what to work on next.

### 7c. The record

Machine-readable, all three: `artifacts/q4-final-disposition.tsv` (per row: `ARCHIVED-resolved` / `left-failed` / `already-cancelled`), `artifacts/q4-supersession.tsv` (the successor each archive rests on, with the matched tokens), `artifacts/q4-disposition.tsv` (each row's own path state + child/live-sibling counts).

**`q4-final-disposition.tsv` is REGENERATED from the ledger by read-back, not carried over from the run's own scratch** (Rule 38: a figure a reader acts on names its generator). The file this run first wrote (`09:35:54`, 7 archived) was a **mid-sweep snapshot** — it was captured while the sweep was still running, so it said `7 ARCHIVED / 23 left-failed` where the finished run reports `17 / 12`. (Its three "duplicate" prefixes — `01a0cb79`, `01a0cc89`, `01a0ce1c` — are **not** an artefact of the sweep: each of those prefixes belongs to two genuinely distinct rows, a fact that persists in the ground-lost set itself.) The generator that reproduces it today, over the same 30 row ids:

```bash
# S = this run's session artifacts dir; the 30 ids come from artifacts/ground-lost-rows.tsv
gopi task list --all --status failed > /tmp/ledger-failed.txt
gopi task list --all                 > /tmp/ledger-all.txt
printf 'task_id\tclass\tsummary\n' > $S/q4-final-disposition.tsv
while read -r full _; do                      # `full` = the 36-char id, verbatim
  ln=$(grep -F -- "$full" /tmp/ledger-failed.txt | head -1)
  if [ -n "$ln" ]; then                       # in the failed list → read its own tag line
    cls=$(printf '%s' "$ln" | grep -q '\[resolved\]' && echo 'ARCHIVED-resolved' || echo 'left-failed')
    sum=$(printf '%s' "$ln" | sed 's/^#[0-9a-f-]*  \[failed\]  //')
  else                                        # not failed at all → its real status is cancelled
    ln=$(grep -F -- "$full" /tmp/ledger-all.txt | head -1)
    cls='already-cancelled (not in the failed list)'
    sum=$(printf '%s' "$ln" | sed 's/^#[0-9a-f-]*  \[[a-z]*\]  //')
  fi
  printf '%s\t%s\t%s\n' "$full" "$cls" "$sum" >> $S/q4-final-disposition.tsv
done < $S/ground-lost-rows.tsv
```

Run over the same 30 ids on 2026-10-07 the table comes out **30 data rows / 30 unique task ids** (`awk -F'\t' 'NR>1{print $1}' | sort -u | wc -l`), **17 `ARCHIVED-resolved` / 12 `left-failed` / 1 `already-cancelled`** — this section's count, reproduced from the ledger alone.

Two instrument notes, each measured rather than assumed:

- **Grep the ledger list by the FULL id.** `ground-lost-rows.tsv` carries 36-char ids, and three of its rows share an 8-char prefix (`01a0cb79`, `01a0cc89`, `01a0ce1c` — each is two distinct rows). A read-back keyed on the 8-char prefix silently matches the wrong row; keyed on the full id it does not. This is also why those three prefixes look "duplicated" in any per-row dump of the swept set: there really are two rows behind each.
- A row's `[resolved]` marker is printed by `gopi task list` and **not** by `gopi task show` — control: `gopi task show 01a0d533-7577-7259-a35a-3f39a5 | grep -c resolved` → `0`, while the same row in `gopi task list --all --status failed` carries `[resolved]`. A read-back through `show` therefore reports 0 archived and is the wrong instrument, not a red result.

- **reissued** (a NEW row carries the subject, present tense): `01a1103b` → `01a113fd-1ea1`, `01a10ead` → `01a113fd-4415`, both filed by this run with the today's re-measurement in the payload.

## 8. Verification

- **Build**: `CGO_ENABLED=0 go build ./...` → OK; `CGO_ENABLED=0 go build -o …/gopi-fixed ./cmd/agent/` → OK.
- **Tests** (branch-scoped, `make test-budget`):
  - `make test-budget PKGS='./internal/tools'` → **exit 0, outcome=pass** (608s; `artifacts/tools-after.log`; stream `run-1791336002-368497.json`).
  - `make test-budget PKGS='./cmd/agent ./internal/tools'` (final, after the commit) → `artifacts/final-test.log`.
  - `make test-budget PKGS='./cmd/agent'` (the SAME command on the unmodified tree, for contrast) → `artifacts/cmd-agent-master-check.log`; the 9 verb-retirement reds it reports are a **pre-existing master** state, unrelated to this change (§7a).
  - New regression test: `go test ./internal/tools/ -run 'TestTaskSubmitLinkedWorktreeRepoIsNormalizedToTheMainCheckout|TestTaskSubmitMainCheckoutRepoIsUnchanged|TestTaskSubmitLinkedRepoRowIsStillRunnable' -count=1 -v` → **3/3 PASS**, and **RED before the fix** (recorded): the first run, against unmodified `TaskSubmit`, failed with
    `the row's deliverable.repo must be the repository's MAIN checkout (…/durable-repo-main) … got …/durable-linked` and
    `the row must still resolve a ground after its submitter's worktree was reclaimed; got the strand verbatim: cannot resolve the ground for task t-submit-durable-runnable`.
- **Ran** (Rule 31 — the surface I changed, for real, with the real binary):
  ```bash
  git -C /home/user/work/GoPi worktree add /home/user/workspace/gopi-r31-probe -b r31/probe-linked
  SB=$(gopi tool sandbox create --label r31-durable --task-id 01a113a9-108e-75ae-a974-f5ed222b5cd3 | tail -1)
  cd $SB && GOPI_HOME=$SB <bin> task submit "R31 live probe: explicit --repo naming a linked worktree" \
        --payload @…/artifacts/my-briefing.md --tags probe --source agent --repo /home/user/workspace/gopi-r31-probe
  GOPI_HOME=$SB <bin> task show 01a113fa-dbbf-7595-94cc-0f09ef0a90b8
  git -C /home/user/work/GoPi worktree remove --force /home/user/workspace/gopi-r31-probe   # the strand's own event
  ```
- **Result**:
  - fixed binary → stderr `task_submit: repo /home/user/workspace/gopi-r31-probe is a linked worktree — stamping the DURABLE main checkout /home/user/work/GoPi so the row outlives the run`; row `01a113fa-dbbf…` → `deliverable.repo: /home/user/work/GoPi`, and **after the worktree was deleted** the same row still reads its repo and resolves a ground.
  - **control, same invocation, old binary**: row `01a113fa-ec73…` → `deliverable.repo: /home/user/workspace/gopi-r31-probe`, i.e. the defect, still there.
- **Commit**: `89a8d5876` on `task/01a113a9108e75aea974f5ed222b5cd3` (+ a follow-up commit correcting this report's own measured figures).

## 9. Evidence (self-filed)

- Q1: `grep '"kind":"worktree-removal"' ~/.gopi/logs/removal-audit.log` → 5263 records, arm census above; the 25-path join → 18/4/3, **0 without a record**.
- Q2: `task_events` per row → claims histogram `1 claim = 29, 0 claims = 1`; `gopi task list --status failed` → `failed-needs-reissue: 254 failed row(s)…`.
- Q3: 26 distinct repos → 23 GROUND / 1 STALE-NAME / 1 MAIN; `[ -d "$p" ]` over them today → **28 GONE, 2 ALIVE**.
- Q4: `reclaim_ground_checklist.go` — two rungs, neither names a ledger row; no `deliverable` SQL in the three removal paths.
- Red→green: the 3-test file's first run (unmodified `TaskSubmit`) failed at the two assertions quoted in §8; after the fix, `3/3 PASS`.
- Rule 24: liveness questions here went to the audit log's pid/start-ticks and to `[ -d ]`, never `pgrep -f`.

## Not Done (honest)

- **12 rows are left `failed` on purpose** (§7b): no successor and no proof their subject is resolved. `01a0ef8a`'s 142 materials are measurably absent; that row is NOT done. I did not re-run their subjects — I measured enough to know I must not call them resolved, which is the honest outcome. They stay visible in `gopi task list --status failed` (`failed-needs-reissue: 238 failed row(s)`; 83 archived rows are hidden behind `--all`) for whoever takes them.
- **`01a0c7b4` is not this strand and was NOT diagnosed.** Its `deliverable.repo` is `/home/user/work/GoPi` — it exists — so it is the *other* failure shape (a stale-but-live repo, or a worktree the repo has since forgotten). It is archived `resolved` **only** because its subject (two bannered devdocs) was separately measured: `go test ./internal/standards -run TestDocClassBannerMatchesManifest` → **ok**, and `classify.py --check` → **0 gap(s)**. Its own failure mechanism is not explained by this document.
- **B (claim-time self-heal) is deliberately absent**, argued in §6. If rows still stranded after this producer fix appear, that is a different question and B becomes worth building; nothing measured today needs it.
- **The 4-row `docanchor` family is still red**: the successor gate I measured (`internal/agent` `TestDocAnchors…`) reports 14 stale permits today. Row `01a0cd4a` was archived under its *narrower* subject (the 24 new drift entries), not under "the docanchor gate is green" — that broader claim is false and I did not make it.
- I did **not** touch any other failed row: `gopi task list --status failed` reports 238 after this run's archives (`[hidden] 83 archived row(s)`), and the remainder is untouched by design (Rule 26).
