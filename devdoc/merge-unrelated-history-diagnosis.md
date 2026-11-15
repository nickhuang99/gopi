<!-- doc-class: normative -->
# `gopi merge` on an unrelated history: diagnosed, not reported as a probe fault

> **Retired mechanism (merge verb removal, 2026-11-01).** The spelling in this
> page's own title was removed: `./bin/gopi merge` exits **1** today. Every
> `gopi merge: …` line below is the verbatim OUTPUT of the 2026-09 runs, quoted
> as evidence and not edited — re-measuring any of it means running the engine
> through `gopi task merge` (same engine, same lock, same `unrelated-history`
> verdict), whose contract page is the FILE `man/gopi-merge.md`.

**Task** 01a0c2b4-1dda-7daa-ad04-7ee35b845167 ·
**Branch** `fix/merge-unrelated-root-diagnosis` · **Date** 2026-09-21

## The report (what the operator saw)

On a **clean** repository, one run printed:

```
gopi merge: holding the single-instance lock; 1 repo(s), ceiling 120000 estimated tokens / 25 conflicted files per branch
gopi merge: exported GOPI_MERGER_LOCK_OWNER=810972:merge … 
  /home/user/work/GoPi feat/sync-conflict-completed error  conflict measurement failed: probe merge: git merge --no-commit --no-ff feat/sync-conflict-completed: exit status 128: fatal: refusing to merge unrelated histories
gopi merge: run over — 1 branch decision(s), lock released with the run, no child left behind
```

The words `error`, `conflict measurement failed` and `exit status 128` read as
**gopi's own fault** — a broken probe, a corrupt repo, something to debug. The
truth is a property of the repository, and the command had no word for it.

## Root cause (5-why)

1. **Symptom** — `conflict measurement failed: probe merge: … exit status 128`.
2. **Mechanism** — `mergeDecideBranch` reaches its ③ diverged arm, calls
   `mergeMeasureConflict`, which materialises the merge in a throwaway worktree
   with `git merge --no-commit --no-ff <branch>` (`cmd/agent/merge_run.go`).
3. **Why the probe failed** — the branch `feat/sync-conflict-completed` is an
   **orphan root**: it shares no commit with `master` (it is an independent
   history, created `git checkout --orphan`, two text files). Git refuses such a
   merge *outright*: `fatal: refusing to merge unrelated histories` (exit 128).
4. **Why that became an "error"** — the probe's arm classifies the git failure
   with `mergeIsConflict(err)` (does stderr contain `CONFLICT`?). A refusal is
   not a conflict, so it fell through to
   `mergeActionError{detail: "conflict measurement failed: " + err}` — the only
   arm in the function that says *this run broke*.
5. **Root norm violated** — the arm conflated **a fact about the repository**
   (the two tips have no common ancestor) with **a fault in the measurement**.
   There was nothing to measure: no conflict exists, no resolver could rework
   it, and nothing may be done to the ref.

Nothing was wrong with the repo (the operator's "git repo is clean" is exact):
the command's vocabulary was just missing this case.

## The fix

`cmd/agent/merge_run.go`, three parts:

- **Ask the question that cannot fail** — `mergeHasCommonAncestor(repo, branch)`
  wraps `git merge-base <integration> <branch>`; it exits **1 exactly when** the
  two tips share nothing, so `err == nil` is the answer ("do they share a
  commit?"), not a failure to report. It is asked **before** `--fast` and before
  the probe: the branch is unmergeable, so there is no measurement to take and
  no LLM run to spend.
  - Deliberately **not** a match on git's failure text (`refusing to merge
    unrelated histories`): that spelling belongs to another program, and a
    verdict must not depend on its wording.
- **Its own verdict** — `mergeActionUnrelatedHistory = "unrelated-history"`,
  alongside `diverged` / `skipped`, with
  `mergeDetailUnrelatedHistory` naming the cause (`no commit in common with
  master`), the manual way forward (`--allow-unrelated-histories`) and the fact
  that the branch was left as it is. Merging an independent history is a
  judgement about **intent**; that stays a deliberate manual step.
- **Named constants** (Rule 1b): `mergeAllowUnrelatedFlag` is declared once and
  referenced by the detail text and the help page; the help page gained an
  *Unrelated histories* paragraph in the same change (the operator reads the
  diagnosis, then `-h` explains it).

The ref is untouched: no archive tag, no rework row, no hand-over, exit 0 —
identical to the `diverged` contract.

## Verification

**Red** (the new arm disabled with `if false &&`, everything else identical):

```
--- FAIL: TestMergeUnrelatedHistoryBranchIsDiagnosedNotAnError (3.18s)
    … 001 feat/orphan-… error  conflict measurement failed: probe merge: git merge --no-commit --no-ff feat/orphan-…: exit status 128: fatal: refusing to merge unrelated histories
    merge_unrelated_history_test.go:103: the raw probe failure "conflict measurement failed" is still printed
    merge_unrelated_history_test.go:103: the raw probe failure "refusing to merge unrelated histories" is still printed
    merge_unrelated_history_test.go:103: the raw probe failure "exit status 128" is still printed
    merge_unrelated_history_test.go:103: the raw probe failure "probe merge" is still printed
FAIL	github.com/the operator/gopi/cmd/agent	3.816s
```

The fixture's own orphan branch reproduces the operator's line **byte for
byte** — the diagnosis matches the report, not a guess about it.

**Green** (fix restored):

```
--- PASS: TestMergeUnrelatedHistoryBranchIsDiagnosedNotAnError (1.03s)
--- PASS: TestMergeHasCommonAncestorTellsOrphanFromRelated (0.43s)
ok  	github.com/the operator/gopi/cmd/agent	2.186s
```

`go build ./...`: OK. Branch-scoped smoke `go test ./cmd/agent/ -run Merge
-count=1`: `ok github.com/the operator/gopi/cmd/agent 53.142s` (the whole merge command
surface — the new verdict changes no existing arm).

## The test pair (why two tests and not one)

`cmd/agent/merge_unrelated_history_test.go`:

- `TestMergeUnrelatedHistoryBranchIsDiagnosedNotAnError` — drives the **real
  `runMerge(nil)`** against a fixture repo holding a `git checkout --orphan`
  branch, and asserts the operator's two outputs: the report carries
  `unrelated-history`, **neither stream carries** `conflict measurement failed`
  / `refusing to merge unrelated histories` / `exit status 128` / `probe merge`,
  and the branch ref survives intact (same tip, master unmoved, no rework row,
  no child, exit 0) — **REVISED 2026-09-22**: the run now also writes the durable
  retirement (an `archive/<branch>` tag with reason `unrelated-history`); see
  "Revision" at the end of this document.
- `TestMergeHasCommonAncestorTellsOrphanFromRelated` — the **pairing control**:
  the classifier answers both ways on real repositories. Without it, an arm that
  skipped every branch (or a classifier stubbed `false`) would leave the
  regression test green.

## End-to-end run of the real binary

```
$ S=/…/e2e-unrelated-01
$ GOPI_SETTINGS="$S/home/.gopi/settings.json" HOME="$S/home" ./gopi-merge-fix merge
gopi merge: holding the single-instance lock; 1 repo(s), ceiling 120000 estimated tokens / 25 conflicted files per branch
  …/e2e-unrelated-01/repo feat/sync-conflict-completed unrelated-history  no commit in common with master (git merges it only with --allow-unrelated-histories) — not a conflict any merge gopi could resolve; branch left as it is
gopi merge: run over — 1 branch decision(s), lock released with the run, no child left behind
EXIT=0
--- fixture repo state after ---
  feat/sync-conflict-completed
* master
f0610db master root          ← master unmoved, no dirt, branch intact
```

The fixture is a real repository built to the report's shape (master root +
`git init -b feat/sync-conflict-completed` as an independent history, fetched in
as a branch), so this is the operator's scenario end to end: the raw git text is
gone, the verdict is a diagnosis, and the ref is untouched.

## Revision (2026-09-22, epic unrelated-history-durable-retire)

The verdict above was **terminal for one run only**, and that turned out to be
the defect: the branch stayed a merge candidate, so the next pass re-decided it
and the merge-backlog generator re-filed `merge <branch> into master` on every
pass (measured: `feat/sync-conflict-completed` was filed 4×). The refusal is
unchanged — this command still never merges an unrelated history and there is
still no `--allow-unrelated-histories` lane — but the run now leaves a DURABLE,
IDEMPOTENT retirement behind, in the namespace every merge path already honors.

- **The mechanism** is the existing one, not a new file or knob:
  `tools.RetireUnrelatedHistoryBranch` → `repoSweepArchiveBranch` →
  `ArchiveTagReasoned`, writing `refs/tags/archive/<branch>` plus a reason record
  carrying the new vocabulary code `tools.ArchiveReasonUnrelatedHistory`
  (`internal/tools/archive_reason.go`). `repoSweepArchiveExempt` hides the tag
  from `mergeCandidateBranches`, `taskMergeExempt`, `safe-merge.sh` and the
  branch-scan face, and the generator's own filter (`archiveTagAncestry` →
  `merge-base --is-ancestor`) drops the branch from the merge-backlog audit —
  one mechanism, five readers.
- **The ref is never deleted**: the tag is a decision, not a prune, and deleting
  the tag re-arms the branch for a future merge.
- **The refusal is not weakened**: the owner gate still refuses to bury a branch
  whose ledger row says the work FINISHED (`completed`/`verified`) — that branch
  keeps its place in the queue and the run says so, carrying the gate's sentence
  into the report.
- **The report names the action**: branch, verdict, reason code, both roots
  (`refs/heads/master <tip>, refs/heads/<branch> <tip>`) and the retirement. A
  second pass prints `already recorded by an earlier pass` — the same durable
  state, a different sentence, because "0 new tags, 0 rows" is the measured
  property.
- **The writer is idempotent**: `repoSweepArchiveBranch` now probes the archive
  namespace BEFORE the owner gate, so a standing decision recorded by an earlier
  pass is a success even if a ledger row later appears over that branch (the
  original reason is kept; the record is never restamped).
- **Generator-side audit (the epic's second deliverable)**: the filer DOES skip
  archive-tagged branches — `AuditMergeBacklog` filters by archive-tag
  reachability before the branch+tip dedup, so `scanned` drops to 0 for a retired
  branch (measured: `scanned=1` before retirement, `scanned=0` after, and the
  existing `pending` row is itself deduped by the live-status check). The
  `branch-pushed` hint row (`notifyBranchPushed`) is a separate face with no
  durable memory — it files once per push, which is bounded by a human push, and
  is reported here as the residual rather than silently widened.

Regression gates: `cmd/agent/merge_unrelated_history_test.go`
(`TestMergeUnrelatedHistoryBranchIsDiagnosedNotAnError` — RED on master, since
the tag assertion fails there — and
`TestMergeUnrelatedHistoryRetirementIsIdempotentAcrossPasses` for the second
pass) and `internal/tools/archive_reason_test.go`
(`TestRetireUnrelatedHistoryBranchIsDurableAndObeysTheOwnerGate` — reason value,
idempotence, owner-gate refusal, and the generator filter).


`gopi merge -h` carried the matching paragraph (the operator reads the verdict,
then the help explains it). *(Past tense on purpose: that help page went with the
top-level verb on 2026-11-01. The matching paragraph now lives in the FILE
`man/gopi-merge.md`, which is where the retirement sentence sends a reader.)*

## INCIDENT — the first e2e attempt mutated the LIVE repository (2026-09-21)

**This is reported here because it happened while verifying this fix, and it is a
defect in the agent environment, not in the merge command.**

The first e2e attempt set `HOME=<fixture>` and `GOPI_HOME=<fixture>/.gopi`, and
that is **not enough to isolate a gopi run**: this agent's tool environment
already carries `GOPI_SETTINGS=/home/user/.gopi/settings.json`, and
`config.ResolveSettingsPath()` reads `GOPI_SETTINGS` **first**, before any
`GOPI_HOME` default. The run therefore loaded the operator's real `repos` table
and swept `/home/user/work/GoPi` — it landed a real branch into master by
fast-forward (`66252422`, reflog `HEAD@{15:00:33} merge
backup/chat-small-height-rework: Fast-forward`) and deleted that branch's ref
(`removal-audit: branch-delete record for backup/chat-small-height-rework not
written: … no such file or directory` on the fixture's own missing log dir).

**Blast radius (measured, and it is small)**: `GOPI_SETTINGS` is the only leak —
everything else was isolated. The landed commit is *the operator's own* work
(authored 14:57, already recorded in the main checkout's reflog before this run
touched it), and the git config refused the rewrite:

```
$ git merge-base --is-ancestor 66252422 master && echo YES
YES — the ff-merged work IS in master history (content not lost)
$ git branch backup/chat-small-height-rework 66252422      # ref restored
$ git rev-parse --verify backup/chat-small-height-rework
66252422b048663b20276ff73f46fbf16e8fc981
```

The deleted ref named an `unrelated history` itself (`git merge-base
feat/sync-conflict-completed master` exits 1 — the very case this task fixes),
and its commit was already in master, so **no work was lost**; the ref is
restored, no archive tag was created (checked), no other branch was touched
(the 2nd, 3rd and all later runs were `GOPI_SETTINGS`-isolated and acted only on
the fixture).

**The lesson, for every later agent**: to isolate a gopi run you must override
`GOPI_SETTINGS` — `HOME`/`GOPI_HOME` alone do not, because the ambient
`GOPI_SETTINGS` of an agent session wins. Verify the isolation *before* the run
(the run's own header prints the repo list it swept; that line is the check).

## Scope, and what is deliberately NOT done

- The forward sweep in `internal/tools/repo_sweep.go` already treats a
  non-ff-able branch as *leave it alone* and acts on nothing, so it does not
  print this error; `internal/tools/task_merge.go` merges task branches, which
  always share history. The arm fixed here is the **only** site that probed a
  merge it could not perform.
- The merge face still does **not** merge unrelated histories. That is the
  design: sharing no history is a statement about where a branch came from, and
  no conflict resolver gets to decide it.

## Not Done (honest)

- **The 3 `cmd/agent` gate failures in a whole-package run are pre-existing and
  unrelated** — `TestCompletionCandidateSetsCoverTopLevel`,
  `TestTopLevelHelpCoversEveryDispatchedSubcommand` and
  `TestArchiveCLIRefusesACompletedOwnerThenRecordsTheReason` fail on **master
  state too** (measured: with this branch's `merge_run.go` and `DOC-INDEX.tsv`
  stashed and the test file moved aside, the first two still fail with
  `topLevelCommandNames contains "config" but no dispatcher handles it`). They
  name the `config` subcommand's completion/help wiring — a different lane.
  Likewise the 49 devdoc files with no manifest row and the 5 new anchor drifts
  in `internal/agent` are identical on master (the missing-file set was diffed
  against master: `IDENTICAL_PREEXISTING`).
- The branch that produced the report (`feat/sync-conflict-completed`) was left
  exactly as found — this task diagnoses the *reporting*, and merging or
  retiring that history is the operator's call.
