<!-- doc-class: historical -->
# Task / merger / worker control specs — doc-truth audit (C2)

**Audited against master `52090dc0`** (the worktree this audit ran in was reset to it:
`git reset --hard master`). Every claim below was re-resolved against that tree; no doc's own
"verified on `<branch>`" line was taken on trust.

**Truth vocabulary** (same five classes the C1 cross-cutting audit established, so the two
reports are greppable together):

| class | meaning |
|---|---|
| **TRUE** | every load-bearing claim resolves on master |
| **DRIFTED** | the doc's own claim is wrong now — doc-side fix |
| **SUPERSEDED** | something later (a ruling, a refactor, a retirement) replaced the claim; the doc does not know |
| **RECORD** | a record of a past run — measured, and it still describes the run it recorded |
| **GAP** | the doc names something with no existence anywhere (path / test / branch) |

**Method, one line each.** Read the file → extract every claim that names code (symbol, path,
test name, CLI spelling, env var, ledger id) → resolve with `code_search` / `keyword_search` /
`read` → where the claim is a *behaviour*, RUN it (the project's own binary and the project's own
test selection, recorded in §4) → where a **requirement doc already holds the verdict**
(`devdoc/requirements/01-task-ledger.md`, `02-merge-and-branches.md`,
`merge-branch-lifecycle.md`), CITE it rather than re-derive. `devdoc/DOC-INDEX.tsv` was used as a
cross-check only — its own classification is a finding (F1).

---

## 1. Verdict table

| # | doc | DOC-INDEX class | verdict | load-bearing evidence |
|---|---|---|---|---|
| 1 | `cli-task-surface-restructure-20260915.md` | normative | **TRUE** on the invariant, **DRIFTED** on the census → **F4** | groups all present: `internal/taskverbs/verbs.go:193` (FamilyVerbs), `:223` (RemovedVerbs `{Run}`), `:242`, `:260`, `:278`, `:302` (derived union incl. RemovedVerbs). Its 5 gate tests exist and are GREEN (§4). Live render: `gopi task -h` = **17** `^  gopi ` synopsis lines, **0** service lines (matches FamilyVerbs 17, not the doc's 15/26 census). The §5 `audit-merges` defect is **fixed in the tree** (`cmd/agent/cli_tasks.go:1088`/`:1255` flag table + parse, with the fix comment) |
| 2 | `merger-stop-takeover-contract.md` | normative | **SUPERSEDED** (every subject entry point closed 2026-09-19) → **F3** | its fix half is EXACTLY in the tree: `cmd/agent/merger_restart.go:527` `mergerWaitLoopGone`, `:465` `mergerLiveLockHolder`, `:493` `mergerWaitLockFree` (kept for START), `:212`+`:353` `mergerRestartRoundInFlightFmt`; discriminator `internal/tools/merger_schedule.go:657` `MergerProcessIsLoopPID` with both readers `:684`/`:709`. Its commit `92ffbda2` **is an ancestor** of this tree. But the control face it is a contract FOR is closed (measured, §4) and **4 of its named tests were deleted** by the retirement (`f9ff1a89`, −410 lines `cmd/agent/merger_restart_takeover_contract_test.go`) |
| 3 | `merger-single-owner-architecture-20260905.md` | normative | **SUPERSEDED** — and the requirement docs already say so → **F5/F7** | cite, don't re-derive: `devdoc/requirements/02-merge-and-branches.md` R5/§6.2 G-3 and `merge-branch-lifecycle.md:355`, `:440` (G-3: *"the protection the old note demanded is **not restated anywhere in the new design**"*), `:562` (G-8: *"the dedup token became the owner"*). Master's rule is the take-over prompt's: `cmd/agent/merger_takeover_prompt.md` §3.6.0 (*"ONE resolver per branch"* / *"the DEFAULT is the resolver lane"*) + `internal/tools/repo_sweep.go:30-31` (*"Resolution is the take-over path's job"*) |
| 4 | `task-model-db-column.md` | historical | **RECORD ✔** (one citation superseded) → **F9** | `internal/tools/tasks.go:157` `tasksModelColumn` = `ALTER TABLE tasks ADD COLUMN model TEXT DEFAULT ''`; `:268` `model TEXT DEFAULT ''` in the CREATE TABLE literal; `:908` `ensureTasksSchema` runs the ALTER and swallows `duplicate column name`; `:1425` `func TaskNew(summary, taskID, sessionID, model string)`; `:3951` `Model string`; `internal/agent/taskregister.go:252` `TaskNewOID{… Model: a.Model, Source: "auto"}`. Rendering verified live: `gopi task list` → `(model deepseek-flash, source llm, created …)`. Its 4 tests GREEN (§4) |
| 5 | `task-model-backfill.md` | historical | **RECORD ✔** — still the working instrument, its measurement is a period fact → **F8** | `scripts/backfill-task-model.sh` exists, executable, and RE-RUN today reports `resolved=0 unresolved=637 db=/home/user/.gopi/memory.db mode=DRY-RUN` — the end state this run was about, reproduced by its own script. Its *"9 total tasks, 9 with `model=''`"* is a 2026-08-29 ledger; the ledger now holds **2035** rows |
| 6 | `task-submit-model.md` | normative | **RECORD ✔** on the mechanism, **DRIFTED** on its coverage conclusion → **F8** (sharpest) | all four claimed tests exist and pass (§4); code present: `tasks.go:3339` `taskExecutorSessionModel`, `:3355` `taskBackfillModel`, `:3055` `TaskChildSessionEnv`, `:3063` `TaskExecutorSessionEnv`, `:3951` submit `Model`; CLI `cmd/agent/cli_tasks.go:395`/`:410`/`:578`/`:783`. Its ③ conclusion (*"本库 91 条空 model 行全部无 session 指针"*) was measured on ~205 rows and is **false for ≥48 rows today** |
| 7 | `gopi-vs-codex-task-type-specialization.md` | historical | **RECORD ✔** — honest report, its recommendations are **DELIVERED-NOT** → **F5** | unimplemented, measured: no `review` verb anywhere in `cmd/agent`/`internal/config` dispatch, no `--output-schema` in the tree, no `-C`/`-root` scope flag; `internal/toolnames/names.go` carries 47 `Tool*` constants against one generic loop. Its §4 says "recommendations" and §6 already records the caveats, so the doc is not lying — it is unowned. Its A/B evidence dir is **GONE** (`~/workspace/gopi-codex-taskcode/.exp` does not exist: fixtures + 5 logs); the codex source proof **is** re-verifiable (`~/work/codex/codex-rs` exists) |
| 8 | `scheduler-integration-design.md` | roadmap | **SUPERSEDED / dead roadmap** → **F6** | `internal/scheduler` does not exist; **no** `RegisterTask` symbol anywhere in the tree; **no** `~/.gopi/tasks/<id>.json` pattern. Its own Phase-0 header calls the present *"GoPi **5 工具** + 同步 ReAct loop"* and lists `remote` as a tool — today 47 tool-name constants, no `remote` tool. Its planned async-remote need was answered by the worker + task-ledger path (and its resident-daemon sibling has since been built and retired twice) |
| 9 | `merger-self-driving.md` | normative | **SUPERSEDED within 24h** — its central admission is false on master → **F1/F2** | the doc's decision row is *"`--every D` … retired → **admitted**"*; master: `gopi merger --every 20s` → **exit 1** through `retireMergerFace` (`cmd/agent/merger_retired.go:58`, `mergerRetiredCause`), the same closure as `--stop`. No retirement note in the doc (`grep -i retired` → only its own 2026-09-17 table column). See §3 for why it is in this slice |

**Roll-up:** 3 TRUE/RECORD-with-caveat (1, 4, 5, 6 minus their named drifts), 3 SUPERSEDED by the
2026-09-19 retirement (2, 3, 9), 1 unowned backlog (7), 1 dead roadmap (8). **No doc in this slice
became FALSE by lying**: every one was accurate for the run it recorded, and the drift is
*someone else's later ruling* the doc cannot see. That is what makes a banner — not a rewrite —
the right remedy.

---

## 2. Findings, ranked by worthiness

### F1 — Four control specs still teach a live merger control face that was closed 2026-09-19 (action now)

> **STATUS UPDATE (2026-09-19, task `01a0b1cd-fa6a-7bc9-81c0-1ccc54150c92`).** Partly
> closed since this audit ran: `devdoc/merger-self-driving.md` now carries a dated
> `CORRECTION` block (the `--stop`/`--restart` restoration, the full retirement,
> and a table resolving every test name it cites against the tree), and the
> sibling specs this audit did not list — `daemon-machinery-removal.md`,
> `merger-pure-cronjob.md`, `merger-loop-restart.md`, `merger-stop-immediate.md`,
> `merger-stop-takeover-contract.md`, `merger-status-operator-text.md`,
> `merger-graceful-restart-20260912.md`, `merger-daemon-flag-20260912.md` — got the
> same dated note. `cmd/agent/merger_takeover_prompt.md` was DELETED with the
> implementation, so its gate-covered artefact is gone rather than stale.


`gopi merger` / `gopi task merger` / `gopi task serve` are closed at both entry points
(`cmd/agent/merger_retired.go:58` `retireMergerFace`, called from `isMergerSubcommand` and the
`taskverbs.Serve`/`taskverbs.Merger` case). Measured, §4: `merger --stop`, `merger --every 20s`,
`merger --status`, `task merger --status`, `task serve --status` → **exit 1**, each printing the
retirement + pointer. The exported `gopi merge` face is untouched (`gopi merge -h` → exit 0).

Yet four documents still describe the resident face as operative, and **three of them are still
classified `normative` in `devdoc/DOC-INDEX.tsv`** — i.e. inside the docclass gate's watched set:

- `devdoc/merger-stop-takeover-contract.md` (normative)
- `devdoc/merger-self-driving.md` (normative — its central *admitted* row)
- `devdoc/merger-single-owner-architecture-20260905.md` (normative)
- `cmd/agent/merger_takeover_prompt.md` §3.6.0 — a **shipped, gate-covered** artifact teaching a
  three-route conflict-ownership rule for a daemon whose entry points are closed. Its gate
  (`cmd/agent/merger_takeover_prompt_gate_test.go`) checks the prompt covers every required
  SECTION and is embedded — so it stays **green over a retired subject**: a gate that cannot
  notice its subject died.

**Suggested fix:** one retirement banner per doc pointing at `devdoc/merger-retired.md` (which
already carries the operator's report and the re-development notes), plus a re-classification of
rows 2/3/9 off `normative` — note the retirement is explicitly *"kept for re-development"*, so the
banner must say "the design is preserved; the entry points are closed", not "this is wrong".
**Bug-worthiness: MEDIUM-HIGH** for the two docs a reader would act on (the contract + the
self-driving admission); LOW for the architecture note (already superseded in the requirement
docs, which a reader reaches first).

### F2 — a live in-code comment cites a test file that no longer exists (cheapest real fix here)

```
cmd/agent/merger_loop.go:90:	// together by mergerEveryFlagIsTheSchedulesSpelling (merger_loop_test.go).
```
`cmd/agent/merger_loop_test.go` **does not exist** — deleted (−205 lines) by the retirement
`f9ff1a89`. The invariant it named still has a surviving guard
(`cmd/agent/cli_tasks_advert_gate_test.go:73` lists `--every` among the service flags the ledger
group may not restate), so this is a pointer, not a hole.
**Suggested fix:** re-point the sentence at the surviving gate, or delete it. **Bug-worthiness:
LOW** (one line, zero behaviour) — listed second only because it is the only finding whose fix is
inside production code.

### F3 — the stop/restart contract lost 4 of its 6 named guards at retirement

`merger-stop-takeover-contract.md` names `TestMergerStopContract_LockOutlivesItsCreator`,
`…_ProbeReportsFreeOnceTheLockIsReleased`, `…_TheStopWaitsOnTheLoopNotOnTheLock`,
`TestMergerRestartDoesNotTouchTheTakeoverInFlight`, `TestMergerStopEndsALiveLoop` and
`TestMergerRestart_HelpDocumentsTheControlFlags`. All six are **gone from the tree**
(`f9ff1a89` deleted `cmd/agent/merger_restart_takeover_contract_test.go` −410,
`merger_restart_e2e_test.go` −888, `merger_loop_test.go` −205, plus the singleton/stop-holder/tick
suites). The *implementation* they guarded is deliberately kept ("the tick body is preserved
verbatim … the RE-DEVELOPMENT starts from them") — so the guarded code survived while its
guard did not.
**Suggested fix:** record in the doc that the guards were removed with the entry points, and that
the re-development must restore them (they are the anti-regression trio for the exact defect the
doc fixed). **Bug-worthiness: MEDIUM** — not a live defect; a re-development hazard, because the
next implementer inherits 4,000 deleted lines and no test naming the invariants.

### F4 — the CLI-surface census is a pre-adoption snapshot; the doc's own "guards updated" table names a deleted test

`FamilyVerbs` is **17** on master (`internal/taskverbs/verbs.go:193`: archive, dedup-scan,
finish-cleanup, for-branch, improvements, list, reopen, search, show, status, submit,
sweep-worktrees, sweep-zombies, tag-resumed, update, verdicts, verify), not the doc's 15; the
before/after table's **26 spellings** is likewise a snapshot — the doc predates `for-branch`,
`finish-cleanup` and `dedup-scan`, and post-dates `run` (now `RemovedVerbs = {Run}`). Both are the
healthy outcome the doc's own design predicted (adding a verb to a group needs no other edit) —
the drift is only that the table is presented as current. Separately, its "Guards updated" table
lists `TestMergerRestart_HelpDocumentsTheControlFlags (e2e)` — **deleted**.
**Suggested fix:** date-stamp the census as "as of 2026-09-15, when adoption was provably 26/26".
**Bug-worthiness: LOW** (doc-side).

### F5 — the whole task-type-specialization backlog is unowned (review rubric / output schema / scoped root)

Measured: 0 of the doc's 4 recommendations shipped (`gopi review` does not exist as a verb,
`--output-schema` appears nowhere, no `-C/-root` scope flag). The A/B evidence that argues for
them is **gone from disk**. So the recommendation now rests on the codex side alone (still
re-verifiable) and on an unowned devdoc row.
**Suggested fix:** operator decision — promote the minimum borrow (#1 rubric entrypoint + #3
output schema) into the backlog with a fresh instrument, or retire the doc with a banner. Do NOT
re-file as a bug without that decision: "a design doc describing unshipped work" is
planned-absent, which the audit method explicitly leaves to the operator.
**Bug-worthiness: LOW as a bug, MEDIUM as a decision.**

### F6 — `scheduler-integration-design.md` is a dead roadmap with no tombstone

`internal/scheduler` absent, no `RegisterTask` symbol, no `~/.gopi/tasks/<id>.json`. The doc
describes GoPi as *"5 工具 + 同步 ReAct loop"* with `remote` as a tool.
**Suggested fix:** archive banner ("subject never landed in this repo; the async-remote need was
met by the worker/task-ledger path"). **Bug-worthiness: LOW.**

### F7 — unreachable pointers in the single-owner note

`merger-single-owner-architecture-20260905.md` names the work item **`t-1788565001`**
(`proc_c85071d942d5`) and the killed daemon pid **1330936**. Under the uuid-v7 task-id migration
(and 756 row ids missing from the ledger — `01-task-ledger.md` **F6**, measured there on the live
DB), a `t-<unix>` id is not resolveable today; no banner marks the note retired.
**Suggested fix:** fold into F1's banner (the note is superseded, and its pointers are historical).
**Bug-worthiness: LOW** — cite `01-task-ledger.md` F6 rather than re-measuring the purge.

### F8 — the model column covers **two thirds** of the ledger; the residue conclusion is now false for ≥48 rows (the one finding worth a ticket)

Measured on the live ledger (`~/.gopi/memory.db`, 2026-09-19):

```
total rows 2035 | model='' 637 (31.3%) | of those with a NON-empty session_id 48
empty by source: auto 300, human 286, llm 51
```
The backfill script's own dry run confirms the mechanism is live, printing e.g.
`unresolvable (left empty): 01a0b625-… (session eecb350a-…)` — a row that **does** name a session
and is still declared unresolvable. `task-submit-model.md` ③ concluded the opposite as a domain
truth: *"本库 91 条空 model 行全部无 session 指针（human submit 行 session_id 恒空）"* — measured
when the ledger was ~205 rows. Two consequences: `gopi task list` renders
`(model (unknown))` for a third of the ledger, and the 300 empty **`auto`** rows sit oddly against
the doc's own claim that auto rows are *born* with `Model: a.Model` in the same INSERT
(`internal/agent/taskregister.go:252`) — so either they predate that path, or something writes
over it. I did **not** chase that root cause (one budget; it is a ledger question, not a doc
question) — the honest deliverable is the measurement that makes it dispatchable.
**Suggested fix:** diagnose why `auto` rows are empty (the `TaskNewOID` path is the one place the
model is captured at create), then re-run the resolver over the 48 session-pointer rows. Do NOT
mass-rewrite: the ledger is append-only by design and `task-model-backfill.md`'s own evidence
(*"only the `tasks.model` column changed"*) is exactly what makes the residue permanent.
**Bug-worthiness: MEDIUM-LOW** — a display-quality defect, not a correctness one, on 31% of rows.

### F9 — a superseded CLI citation in the model-column record

`task-model-db-column.md` verifies *"`gopi task new --model deepseek-v4-flash` → row persists"*
live. That spelling is **removed** (measured: `gopi task new: removed (M4.5) — task creation via
the CLI was removed — use "gopi task submit <summary>"`). Harmless — the removal message is the
correct pointer — and the *claim* it was making (the model persists) is still true via
`task submit --model NAME` (`cmd/agent/cli_tasks.go:395`).
**Bug-worthiness: LOW** (a one-word doc-side date-stamp).

### F10 — `DOC-INDEX` classes retired specs as `normative` (INFO, feeds F1)

Three of this slice's superseded docs are classified `normative`, which is the docclass gate's
**scope** (`devdoc/DOC-INDEX.tsv` → `internal/docclass`). A retired design therefore stays inside
the watched set and its anchors keep being re-checked — correct for anchors, misleading for the
reader who takes `normative` as "this claims to describe master now".
**Suggested fix:** none here; the classification is regenerated by `internal/docclass` tooling, so
the durable fix belongs with F1's banner convention. **Bug-worthiness: LOW.**

---

## 3. Coverage and limits (including one correction to the briefing)

- **The briefing's "8 docs" is 9.** `devdoc/doc-truth-audit-plan.md:230` (the audit epic's own
  partition) lists C2 as those 8 **plus `merger-self-driving`**, added by the plan's §2.0
  correction with the reason spelled out: *"added in the §2.0 correction — it sat in the 677-name
  bucket and was owned by no slice"*. The briefing dropped it. It is audited here as row 9, and it
  turned out to be **the most drifted doc in the slice** (its central decision was reversed within
  24h) — i.e. exactly the doc the name-based partition would have left unowned.
- **Cited, not re-derived:** `devdoc/requirements/01-task-ledger.md` (the ledger model, the append-only
  rule, and its F6 purge measurement), `devdoc/requirements/02-merge-and-branches.md`
  (R4/R5, G-1/G-2/G-3/A6), `devdoc/requirements/merge-branch-lifecycle.md` (G-3, G-8) — these hold
  verdicts for the single-owner and conflict-ownership claims and are on master.
- **Out of scope:** the internals of the merger family beyond the claims each doc makes
  (`internal/tools/merger_*.go` is ~400KB across 40 files); the 2026-09-16/17 merger epics except
  where a doc's claim depends on them; the docclass manifest's own correctness (only its
  classification of this slice's rows is reported, F10).
- **Report-only.** Per the epic's acceptance, nothing was fixed and **nothing was submitted**.
  Every finding carries a suggested fix and an honest worthiness estimate for the operator.
- **One thing I could not verify:** the A/B evidence for row 7 (`~/workspace/gopi-codex-taskcode/.exp`)
  no longer exists on this host, so that doc's measurements are unfalsifiable *by re-running* —
  its codex-source claims are not.

---

## 4. Verification (commands run, output observed)

Base: `git rev-parse master` → `52090dc066f1245d16057cac433c7f60f44a49d3`; the audit worktree was
reset to it before any claim was checked.

```
$ CGO_ENABLED=0 go build -o ./.gopi-audit-bin ./cmd/agent/          # exit 0
$ ./.gopi-audit-bin task -h | grep -cE '^  gopi '                    # 17   (FamilyVerbs = 17)
$ ./.gopi-audit-bin task -h | grep -cE '^  gopi (task )?(merger|worker|serve)( |$)'   # 0
$ ./.gopi-audit-bin merger -h ; echo $?                              # retirement line; exit 1
$ ./.gopi-audit-bin merger --every 20s ; echo $?                     # retirement line; exit 1
$ ./.gopi-audit-bin merger --status ; echo $?                        # retirement line; exit 1
$ ./.gopi-audit-bin task merger --status ; echo $?                   # retirement line; exit 1
$ ./.gopi-audit-bin task serve  --status ; echo $?                   # retirement line; exit 1
$ ./.gopi-audit-bin merge -h ; echo $?                               # exit 0  (the KEPT one-shot face)
$ ./.gopi-audit-bin task merge -h ; echo $?                          # exit 0
$ ./.gopi-audit-bin task new "x" --model y                           # "removed (M4.5) … use task submit"
$ ./.gopi-audit-bin task list | head -3                              # "(model deepseek-flash, source llm, …)"

$ go test -count=1 -run 'TestNoServiceOrSyncVerbInLedgerGroup|TestLedgerGroupTeachesNoServiceBlock|TestSyncVerbsHaveExactlyOneHeadHome|TestNoHelpTextIsDuplicatedAcrossPages|TestTaskFlagMemberAuditMerges|TestParseTasksArgsAuditMergesReadsItsFlags|TestParseTasksArgsSubmitModelFlag' ./cmd/agent/
ok  	github.com/nick/gopi/cmd/agent	4.831s
$ go test -count=1 ./internal/taskverbs/
ok  	github.com/nick/gopi/internal/taskverbs	0.029s
$ go test -count=1 -run 'TestTaskSubmitModelFlag|TestTaskUpdateWithResultBackfillsModel|TestTaskNewModelPersisted|TestTaskNewDefaultModelEmpty|TestEnsureTasksSchemaMigratesModelColumn|TestTaskShowModelFromSession' ./internal/tools/
ok  	github.com/nick/gopi/internal/tools	24.175s

$ bash scripts/backfill-task-model.sh                               # DRY-RUN
---- resolved=0 unresolved=637 db=/home/user/.gopi/memory.db mode=DRY-RUN
$ sqlite3 ~/.gopi/memory.db "select count(*) total, sum(case when coalesce(model,'')='' then 1 else 0 end) empty_model, sum(case when coalesce(model,'')='' and coalesce(session_id,'')<>'' then 1 else 0 end) empty_with_session from tasks;"
2035|637|48

$ git merge-base --is-ancestor 92ffbda2 HEAD && echo YES     # YES  (the stop-contract fix IS on master)
$ git merge-base --is-ancestor f9ff1a89 HEAD && echo YES     # YES  (the retirement IS on master)
$ git show --stat f9ff1a89 | grep contract
 cmd/agent/merger_restart_takeover_contract_test.go | 410 ----------
$ grep -rl TestMergerStopContract_ --include=*.go .          # (no hits — the guards are gone)
$ grep -n "mergerEveryFlagIsTheSchedulesSpelling" cmd/agent/merger_loop.go
 cmd/agent/merger_loop.go:90:	// together by mergerEveryFlagIsTheSchedulesSpelling (merger_loop_test.go).
$ ls cmd/agent/merger_loop_test.go                            # No such file or directory
```

## 5. Not done (honest)

- **No fixes applied** — the epic's acceptance is report-and-rank, nothing submitted. F1/F2 are
  ready to dispatch as-is; F8 needs an operator decision before it becomes a ticket.
- **F8's root cause not chased** (why 300 `auto` rows carry an empty model although the create
  path stamps `a.Model`). I measured the residue and the population, not the writer.
- **The `<4`-doc families and the CLI/help constants beyond this slice were not swept** — the
  boundary is the C2 doc list in `devdoc/doc-truth-audit-plan.md:230`.
