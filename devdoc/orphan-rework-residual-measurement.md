<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# Two orphan branches, reworked: what was already landed, and what survived

**Task** `01a1164132c9798983f67ca2d44005d4` · **Class** residual measurement of a
rework · Re-anchor: master **`9ea9d75b8`** · Rework branch
`task/01a1164132c9798983f67ca2d44005d4`.

Two task branches could not auto-merge into master and their refs were already
deleted, leaving only the archived `git diff <merge-base> <branch>` attachments:

| orphan | commits behind | conflicted files at archive time |
|---|---|---|
| `task/01a115b6603d7844974c041b7a47dfc1` (14 files) | 72 | 3 |
| `task/01a11604af56794a95ec7432045eb6c6` (4 files) | 62 | 1 |

Rule 23 forbids continuing on the archived ref and the stale diff is never
`git apply`ed (it reintroduces the conflicts by construction). So each intent file
was **measured against current master** and only the survivors were re-authored.

**The finding, stated first: master had already landed essentially all of BOTH
branches' product intent.** Of the two attachments' 15 distinct intent files, 11
are landed verbatim-or-better, 2 are superseded by a later, larger design, and 2
are the only genuine survivors (both are *regression guards*, not product code).
The one product change re-authored here is a comment correction in
`cmd/agent/cli_tasks_sync.go`; the one product line restored is the repo-named git
error in `internal/tools/task_merge.go`.

## Completed

### The measurement method (so the verdict is re-runnable)

For every intent file: (1) read the attachment hunk to recover the INTENT
identifier; (2) grep that identifier/behaviour on current master; (3) name the
commit that landed it (`git log -S<symbol>` / `git log -- <file>`); (4) where the
identifier is absent, decide whether absence means "not done" or "done
differently" by **running the attached branch's own gate on pristine master** —
the empirical arbiter, since a symbol miss is not a feature miss (Rule: 命中≠实现
cuts both ways). Step 4 was decisive for the spelling table below.

The pristine-master probe (this run, master `9ea9d75b8`, load 4.02, no load
created — Rule 27):

```
go test ./cmd/agent/ -count=1 -run 'TestCLINamingExceptions|TestSyncProgPrefix|TestPrefetch|TestSyncBarePeers' -v
→ ok  github.com/nick/gopi/cmd/agent  10.904s   (every listed test PASS)
```

### Branch `task/01a115b6…` — 14 intent files

| # | file | intent | verdict | evidence |
|---|---|---|---|---|
| 1 | `cmd/agent/cli_tool_spelling_table.go` | rename `isUnderscoreException` → `keepsUnderscoreSpelling`, discriminate by the row's DOOR, so the two retired web words are no longer projected as exceptions | **SUPERSEDED — not re-applied** | master carries `underscoreException()` (a Rule-20-clean name) at line 170 and the door predicate `pageAddress() { return r.door }`. The attached predicate would have changed the PROJECTION, and master's exception count is a deliberate design: `TestCLINamingExceptionsAreStillDispatchable` PASSES on pristine master, and the web row is a *declared second door* (`toolGroupSecondDoorWord`, `cmd/agent/cli_tool.go:282`), pinned by `TestToolGroupDeclaredSecondDoorIsPrintedAndPinned` + `TestToolGroupWordsAreTheOnlyDoor`. The attached diff predates that design; re-applying it would have removed a word's live home from the help page — landing the very defect its own comment says it removes |
| 2 | `cmd/agent/cli_completion_exceptions_test.go` | rename the consumer | **LANDED differently** | master calls `r.underscoreException()` at line 58 — the rename half landed at `f3ed514d3` ("drop the `is-` prefix on the spelling-table accessor"), the door half did not, by design (see #1) |
| 3 | `cmd/agent/cli_tasks_sync.go` | `syncProgPrefix` composed from the sub-verb, not `taskDNSuffix` | **LANDED** | `syncProgPrefix = syncFamilyPrefix + syncSubverb + " "` (line 412); the `gopi task task sync` defect is gone. Residual: two comments still pointed at `taskDNSuffix` — corrected here (below) |
| 4 | `cmd/agent/cli_tasks_sync_taskid_surface_test.go` | the composition guard | **SURVIVOR (re-authored)** | The guard was absent on master (attach prints no `TestSyncProgPrefixDoesNotDoubleTheFamilyWord`) though the fix had landed: the fix had **no regression guard**. Re-authored against master's actual constants |
| 5 | `cmd/agent/cli_tasks_sync_peers_bare_test.go` | the unrelated-files census gains rows | **LANDED** | master's `syncPeersBareUnrelatedFiles` already carries the whole `task 01a11683…` master-red-triage block **including** `cli_tasks_sync.go`, `merge_prompt.md`, `repo_context_sandbox_guard_test.go`, `cli_memory_help_surface_basic_test.go` — with the same reasons, moved into the list's own "master-red triage family" section. Only a rework-specific block was appended here for this branch's own two files (the gate derives the changed set from `git diff HEAD`) |
| 6 | `cmd/agent/merge_prompt.md` | `gopi session resume` spelling | **LANDED** | line 703: `` `gopi session resume <raw-log> --task <id>` `` |
| 7 | `cmd/agent/repo_context_sandbox_guard_test.go` | `runGopiMapped` census entry + the shape predicate | **LANDED** | `"runGopiMapped": true` twice (lines 194, 1156) and the `*ast.Ident` arm of the runner predicate (line 1164) |
| 8 | `cmd/agent/cli_memory_help_surface_basic_test.go` | the fixture opens through `tools.LedgerDSN` | **LANDED** | line 1178: `sql.Open("sqlite", tools.LedgerDSN(sb.db))` |
| 9 | `devdoc/requirements/01-per-module-briefings.md` | "gopi session raw logs" | **LANDED** | line 218 |
| 10 | `COMMANDS.md` | line numbers `cli_checkpoint.go:185`, `cli_memory.go:197`, `cli_tasks.go:475` | **LANDED** | lines 116 / 126 / 141 carry exactly those |
| 11 | `devdoc/DOC-INDEX.tsv` | row for the branch's doc | **LANDED** | the attached row was a line-number drift fix for a doc master has since re-anchored |
| 12 | `cmd/agent/chat_prefetch.go` | budget margin, budget arming above the stat, busy-arm order, retry-loop memory | **LANDED (the product half)** | `153d7b054` landed the one-clock identity, `skipLadder`, and `isBusyErr`-first ordering; `afeca1477` dropped the dead duplicate arm. Master's shape is *stronger* than the attachment's: `prefetchLookupDetailed` opens the ONE clock above path resolution and the query segment is its remainder, so the identity holds by construction instead of by a 300ms margin. `TestPrefetchBusyIsRetriedWithinTheSameBudget` and `TestPrefetchRecordCarriesSegmentTiming` both PASS on pristine master, so the attached measurement's own acceptance is already green |
| 13 | `cmd/agent/chat_prefetch_segments_test.go` | the arm-order + retry-cap guards | **SURVIVOR (re-authored)** | The fix landed WITHOUT its guard: no `TestPrefetchBusyGiveUpIsReportedAsBusy`, no `TestPrefetchRetryRespectsItsCapAndKeepsTheReason` on master |
| 14 | `internal/tools/task_merge.go` | name the repo in git's error | **SURVIVOR (LANDED as a one-line restoration)** | `taskMergeGit` still returned `git %s: %w: %s` with the repo known only to `cmd.Dir`. Restored with the `-C %s` arm |

### Branch `task/01a11604…` — 4 intent files (all code intent landed)

| # | file | verdict | evidence |
|---|---|---|---|
| 1 | `cmd/agent/chat_prefetch.go` | **LANDED** | `prefetchResult.ElapsedMs`, `prefetchLookupPhased`, `skipLadder`, `isBusyErr`-first at both the query and the fresh-dial arm — all on master; landed by `153d7b054` + `afeca1477` |
| 2 | `cmd/agent/chat_prefetch_segments_test.go` | **LANDED** | the exact-identity assertion (`sum != lookup.Ms`, with its "tolerance is what let the defect hide" comment) is on master at line 125-140 |
| 3 | `devdoc/memory-prefetch-segment-identity-and-lock-attribution.md` | **LANDED** | present, 16,867 bytes, class `historical` |
| 4 | `devdoc/DOC-INDEX.tsv` | **LANDED** | the doc's row is at line 996 — re-anchored by the corpus's own drift, so the attached row was its old position |

**Branch 2 therefore contributes no re-implementation.** The whole branch-2 intent
— the measurement, the fix, the two regression guards and the report doc — is on
master; this run's probe re-confirms it green.

### Re-authored survivors (the deliverable)

1. `internal/tools/task_merge.go` — the repo-named git error (`git -C %s %s: …`),
   including its measured rationale. Restores the attachment's intent; the
   repo-less arm is byte-identical to before, so no caller sees a new message for
   the flagless path.
2. `cmd/agent/cli_tasks_sync_taskid_surface_test.go` —
   `TestSyncProgPrefixDoesNotDoubleTheFamilyWord`. **Re-authored, not copied**: the
   attachment composed its expectation from `gopiProgramWord` plus a byte-slice of
   the family prefix, a spelling that no longer exists on master; the rework
   asserts against the family prefix itself plus the general "no adjacent repeated
   word" shape, so a future re-composition cannot slip the same defect back in
   under a different pair of constants.
3. `cmd/agent/chat_prefetch_segments_test.go` — two guards re-authored against
   master's one-clock lookup:
   - `TestPrefetchBusyGiveUpIsReportedAsBusy` — a hard `BEGIN EXCLUSIVE` holder
     outliving the budget, so the refusal and the expired deadline always overlap
     and the ARM ORDER is what decides the report. Independent takes, each its own
     lookup, because the overlap is a race.
   - `TestPrefetchRetrySpendsItsCapAndStopsOnTheRefusal` — the loop spends its full
     cap (the bound is the CAP, and refusing a retry "for want of room" only stops
     the counter), and a deadline that expires mid-wait still ends it with an
     error.
   - **Deliberately NOT restated** from the attachment: its claim that a lock met
     on an earlier attempt must outlive the deadline as the reported reason.
     Master measured that a retry can be refused by the context before it reaches
     SQLite and removed the loop's memo on purpose; the arm order is what keeps a
     refusal from being read as a deadline. Restating the memo would have been
     re-applying the stale diff instead of re-authoring against master.
4. `cmd/agent/cli_tasks_sync.go` — two comments retired the `(taskDNSuffix)`
   pointer, which had led a reader to a constant the excerpt must never be
   composed from (Rule 13: the drift that this file's own guard exists to catch).
5. `cmd/agent/cli_tasks_sync_peers_bare_test.go` — this branch's own files listed
   in the unrelated-files census with their reasons, which is what that gate asks
   for on an unmerged branch.
6. This document + its `DOC-INDEX.tsv` row.

## Not Done (honest)

- **`keepsUnderscoreSpelling` / the door-discriminated projection was NOT
  re-applied** (branch 1, files 1-2). This is the one attached intent I judged
  wrong for current master, and I am reporting it rather than quietly landing it:
  master's reachability-side predicate (`TestCLINamingExceptionsAreStillDispatchable`,
  which passes for the web rows through the family-address arm) plus the
  *declared second door* machinery supersedes it. If a reviewer disagrees, the
  change is small — but it is a behaviour change to a pinned, documented surface
  (`devdoc/cli-naming-convention.md` §"The `web` exception"), not a rework.
- **The `prefetchBudgetMarginMs` / `prefetchWorkingDeadline` design was not
  re-applied** (branch 1, file 12). Master fixed the same defect at the origin
  (one clock, the query segment as its remainder), which is strictly better than a
  named narrowing of the working budget; re-adding the margin on top of the
  identity would be adding a knob that can no longer fire.
- **No red-proof of the two re-authored prefetch guards.** They are driven against
  master's already-fixed code, so they pass; proving them red would mean
  reintroducing the arm-order defect in the worktree for a measurement, which this
  budget did not allow. The guards pin the ORDER and the CAP by construction; the
  claim I make is "these contracts are now guarded", not "I measured the old
  failure".
- **The attached branch's load-qualified acceptance run** (`load_before > 12`,
  5 rounds) was not reproduced: reproducing it needs a host under load, and Rule 27
  forbids manufacturing one. Master's own report of that run is the evidence of
  record.
- **The two orphans' *other* intent** (the parts not visible in a `git diff`
  against their own merge-base — e.g. commit messages, session artifacts) was not
  recovered: only the archived diffs were available.

## Verification

- **Build:** `go build ./...` → rc=0; `go vet ./...` → rc=0 (was rc=1 mid-run on an
  `undefined: errors` import and a duplicated constant, both fixed before the
  final run).
- **Declared build:** `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → rc=0
  (`bin/gopi`, 64,469,664 bytes). This is the command `settings.json` declares for
  this repo (Rule 12), run from this worktree.
- **Tests (exact invocations, in the run's own log):**
  - `go test ./cmd/agent/ -count=1 -run 'TestCLINamingExceptions|TestSyncProgPrefix|TestPrefetch|TestSyncBarePeers' -v`
    → `ok github.com/nick/gopi/cmd/agent 10.904s` — **pristine master, before any
    edit**; every listed test PASS.
  - `go test ./cmd/agent/ -count=1 -timeout 900s -run 'TestPrefetchBusyGiveUpIsReportedAsBusy|TestPrefetchRetrySpendsItsCapAndStopsOnTheRefusal|TestSyncProgPrefixDoesNotDoubleTheFamilyWord|TestSyncBarePeersTestCoversEveryChangedFile|TestCLINamingExceptions' -v`
    → the reworked tree (result recorded in the task report).
  - scoped budget suite: `make test-budget PKGS='./cmd/agent ./internal/tools'`
    → `cmd/agent` **PASS** (whole package; `TestPrefetchBusyGiveUpIsReportedAsBusy`
    25.48s); `internal/tools` FAIL with exactly **3** assertion failures, all in
    `task_ground_baseline_test.go` — a file this branch does not touch.
    **Attributed by measurement**: they fail identically on PRISTINE master
    (`9ea9d75b8`) in a separate detached worktree, with the same message
    (`the task id is not a full uuid, so no ground name can be derived from it`)
    ⇒ **pre-existing at master**, not a regression of this rework. Budget not
    tripped (1437s wall, 25m/package).
  - `go test ./internal/tools/ -count=1 -run 'TestMerge|TestTaskMerge'`
    → `ok github.com/nick/gopi/internal/tools 39.038s`.
  - `python3 tools/docclass/classify.py . --check` → `0 gap(s) between devdoc/ and
    devdoc/DOC-INDEX.tsv`.
  - `gopi tool doccheck` → `doccheck: the devdoc corpus is consistent — every file
    under devdoc/ has a well-formed row`, rc=0.
  - **DOC-INDEX union, measured as an intersection, not asserted** (this is the
    append-only rule, and the bug that rule exists for): `comm -13
    <(git show master:devdoc/DOC-INDEX.tsv | sort) <(sort devdoc/DOC-INDEX.tsv)`
    → exactly ONE added line, this document's row; `comm -23` → **0** — master's
    rows are all still present, so the union dropped nothing.
- **Ran (the product surface this diff changes):** `internal/tools/taskMergeGit` is
  reached by `gopi task merge`; the restored arm fires when the repo flag is set.
  The byte-level check `grep -n 'git -C %s' internal/tools/task_merge.go` shows the
  arm; the flagless path is unchanged.
- **Merge probe:** the reworked branch against master in a DETACHED worktree —
  `git merge --no-commit --no-ff master` → rc=0, zero unmerged paths, then
  `git merge --abort` with master's HEAD and `MERGE_HEAD` verified unchanged
  (recorded in the task report).
- **Three `internal/tools` failures are PRE-EXISTING, measured by a control
  worktree:** `TestTaskRunGround{CutsTheBaseFromThePeerTip,
  DegradesExplicitlyWhenThePeerIsUnreachable, SkipsThePeerFetchOnTheServerRole}`
  fail identically on PRISTINE master (`9ea9d75b8`) in a separate detached
  worktree, with the same message (`the task id is not a full uuid, so no ground
  name can be derived from it`) in `task_ground_baseline_test.go` — a file this
  branch never touches. Reported, not fixed here.
- **Commit:** on `task/01a1164132c9798983f67ca2d44005d4`.

## Evidence (self-filed)

- Pristine-master probe output: every `--- PASS` line for `TestPrefetch*`,
  `TestCLINamingExceptions*`, `TestSyncProgPrefixIsNotTheErrorPrefix`,
  `TestSyncBarePeers*`; the gate's own log line
  `cmd/agent files this branch changed: 0`; final `ok … 10.904s`, `PROBE_RC=0`,
  `load_before=4.02`.
- Landing commits, from the object store (never a second checkout — Rule 29):
  `git log --oneline -5 -S"ElapsedMs" -- cmd/agent/chat_prefetch.go` →
  `153d7b054`; `git log --oneline -3 -- cmd/agent/cli_tool_spelling_table.go` →
  `f3ed514d3`; `git log --oneline -3 -- devdoc/requirements/01-per-module-briefings.md`
  → `d864bc409`.
- Identifier greps recorded verbatim in the tables above (`prefetchLookupPhased`
  line 925, `skipLadder` line 1091, `underscoreException` line 170,
  `runGopiMapped` lines 194/1156, `LedgerDSN(sb.db)` line 1178,
  `gopi session resume <raw-log>` line 703, COMMANDS.md lines 116/126/141).
- Both orphan refs confirmed absent (`git rev-parse --verify --quiet
  refs/heads/<branch>` → exit 1) and unarchived (`git tag -l` empty for both), so
  the archived diffs are the only record of their intent.
