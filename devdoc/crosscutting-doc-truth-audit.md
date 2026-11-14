<!-- doc-class: historical -->
# Cross-cutting doc truth audit (C1) — 20 SPEC/contract docs measured against master

Branch `feat/crosscutting-doc-truth-audit`, worktree
`/home/user/workspace/gopi-crosscut`, base **master `52090dc0`**
(`merge verify/branch-lifecycle-ruling-already-landed`).

**What this is.** A read-only verification pass. Every doc below was read, then
each load-bearing claim that names code (symbol / file / flag / env var /
file:line / test name) was resolved against the tree at that base. **No source
file was changed and nothing was submitted to the task queue.** The deliverable
is the verdicts and the ranked findings; the fixes are left for review to
dispatch.

**Verdict vocabulary**

| verdict | meaning |
|---|---|
| **TRUE** | every load-bearing claim resolves; the doc can be read as current |
| **DRIFTED** | the claim was true when written and is now false/incomplete — doc-side fix |
| **SUPERSEDED** | something later (branch, refactor, ruling) replaced the claim; the doc does not know |
| **RECORD** | a record of a past run; measured, and it still describes the run it recorded |
| **GAP** | the doc names something that has no existence anywhere (path / test / doc) |

**Method, in one line each** (so a reviewer can re-run it): read the file →
extract every `path`, `symbol`, `file:line` and test name → resolve with
`code_search` / `rg` / `git log -- <path>` / `git merge-base --is-ancestor` →
where the claim is a behaviour, RUN the named test and record the output
(`[VERIFIED]` rows carry the command). The truth-class manifest
(`devdoc/DOC-INDEX.tsv`, one row per devdoc file) was **not** taken as evidence —
it was used as a cross-check, and its own state is finding **F2** (the coverage
gate it feeds is red on master).

---

## 1. Verdict table (all 20)

| # | doc | DOC-INDEX class | verdict | load-bearing evidence |
|---|---|---|---|---|
| 1 | `cli-naming-convention.md` | normative | **DRIFTED** (1 stale test name) | rules 1–4 match `cmd/agent/cli_verbs.go` (21 `CliVerb*` consts), `internal/taskverbs/verbs.go`, `internal/toolnames/names.go`; but its enforcer row names a test that does not exist → **F1** |
| 2 | `cmd-agent-contract-reds.md` | historical | **RECORD** ✔ | both reds fixed in production; `go test -run 'TestParseTasksSweepZombiesArgs\|TestEveryHelpConstantFitsTheWrapBudget' ./cmd/agent/` → ok 2.300s |
| 3 | `centralized-architecture-20260830.md` | normative | **SUPERSEDED** (its whole "在途/待实现" list has landed) → **F3** | ①–⑤ all present: `internal/config/config.go:285-289` (compute_nodes), `cmd/agent/dispatch.go:274`+`:308` (load-balance dispatch), `:102`/`:117` (ssh exec), `internal/config/config.go:532` (`report_to`), `cmd/agent/dispatch.go:226` (push back) |
| 4 | `gopi-md-design.md` | historical | **RECORD** ✔ (one illustrative line obsolete) | mechanism as designed: `internal/agent/loop.go:2218-2221` (reads `./GOPI.md` from cwd), `internal/agent/context.go:132`, `:300`, `:307` (2nd system message), `internal/agent/resume.go:46` |
| 5 | `path-policy-cd-base.md` | normative | **SUPERSEDED** at line 16 → **F4** | "that fix lived only on `fix/worktree-residue-closeout`, **not on master**" — `internal/tools/path_policy.go:617` `gitInvocationStart` consumes `-C` **on master**, pinned by `TestPathPolicyGitChangeDirIsNotTheTarget` (`internal/tools/bash_path_policy_test.go:218`) ✔ |
| 6 | `taskid-vs-sessionid-architecture-20260909.md` | normative | **DRIFTED** (pre-decision state described as current) → **F5** | doc:7 "task_id = t-<unix>"; master: `internal/tools/tasks.go:1248` `taskIDGenerate` mints **uuid v7** (rationale comment `:1242-1247`); `taskIDPrefix = "t-"` (`:134`) is legacy-render only (`:1675`); named SPEC `devdoc/SPEC-db-cleanup-uuid-taskid.md` does not exist (**GAP**) |
| 7 | `todo-vs-writetaskarea-design-20260909.md` | historical | **RECORD** ✔ (converged into the `task` tool) | `internal/toolnames/names.go:57-64` — ToolTodo + ToolWriteTaskArea merged into ToolTask, ToolTaskEvaluate retired; `internal/agent/loop.go` def names `toolnames.ToolTask` |
| 8 | `todo-list-design-evolution.md` | historical | **RECORD** ✔ with anchor drift → **F6** | "2 状态 / 0 新增工具" holds for the in-session list; its reference block cites `loop.go:586-610` as the rules source — that region is now `efficiencyRules`; `taskManagementRules` is `internal/agent/loop.go:641` |
| 9 | `session-model-banner-fix.md` | historical | **RECORD** ✔ [VERIFIED] | fix landed: `cmd/agent/main.go:990-999` resolves the vision model **before** `newAgent` (comment names the banner); `go test -run TestNewAgent_ImageInputRecordsVisionModel ./cmd/agent/` → ok |
| 10 | `PRINCIPLE-lifecycle-storage-completeness.md` | historical | **GAP + DRIFTED** → **F7** | calls itself **"Rule 16"**; GOPI.md Rule 16 is the `%w` gate (`GOPI.md:538`), and the lifecycle rule is **not in GOPI.md** (`grep -c 'Lifecycle Storage' GOPI.md` → 0). Its own "检查才有效" clause has no check. |
| 11 | `attachment-index-contract.md` | normative | **TRUE** ✔ | `internal/tools/task_session.go:91` `TaskAttachmentReplicaMissingLabel`, used at `internal/tools/tasks.go:2668`/`:2679`; its test `cmd/agent/cli_tasks_attach_index_test.go` exists and is cited by DOC-INDEX row evidence |
| 12 | `wf-contract.md` | historical | **RECORD** ✔ with one stale gotcha → **F8** | `emissionContract` `internal/agent/loop.go:616`; `trustedCompletion` `internal/agent/loop.go:4376`; `maxRoundsWithoutTaskList = 3` `internal/agent/loop.go:602`; `internal/agent/wfcontract_test.go` asserts the interpolation. Its `.gitignore` claim is stale: `.gitignore:6` is **`/agent`** (anchored) and `git check-ignore internal/agent/wfcontract_test.go` prints nothing |
| 13 | `perf-vs-claude-fix-plan.md` | roadmap | **DELIVERED** (R2–R7 implemented; R1's cited values are stale) → **F9** | R1 `DefaultContextWindow = 200000` (`internal/agent/context.go:19`), `DefaultCompactTriggerRatio = 0.75` (`:33`) vs the doc's 900000 / 0.9 / 0.6 / `TrimThresholdRatio` (**symbol gone**); R4 `executeToolBatchParallel` `internal/agent/loop.go:4557`; R5 buffered sync `internal/agent/session.go:262-279`, `internal/agent/rawlog.go:218-224`; R6 `stalenessCacheTTL = 2s` `internal/tools/symbol.go:73`; R3 → `emissionContract`+`trustedCompletion`; R2 anchored at `internal/agent/loop.go:128`, lives at `:641`; R7 heartbeat fields still unread (the doc itself predicted this) |
| 14 | `roadmap.md` | roadmap | **DRIFTED** (1 of 8 named paths gone) → F10 | `internal/deepseek/stream.go` **does not exist** (merged into `internal/deepseek/claude_client.go`); `internal/deepseek/client.go`, `internal/deepseek/types.go`, `cmd/agent/main.go`, `internal/tools/{read,write,edit,bash}.go` all exist |
| 15 | `m420-rank-model.md` | normative | **TRUE** ✔ [VERIFIED] | ladder matches `internal/tools/tasks.go:2943-2955` (`taskStatusRank`, incl. `taskStatusReopened: 0`); `syncDeltaApplyNewer` `internal/tools/memory_sync_delta.go:832`; `go test -run 'TestStatusRankTable\|TestRankApplyMatrix\|TestRankGuardAtomicity' ./internal/tools/` → ok |
| 16 | `taskfindjsonl-layout-determinism.md` | historical | **RECORD** ✔ | precedence in `internal/tools/tasks.go:1968` `taskFindSessionJSONLDeepOnly` + `taskFindSessionJSONLForRow`; test `internal/tools/taskledger_cost_test.go:284`; its commit `ee125fe3` **is an ancestor of master** |
| 17 | `taskfindjsonl-layout-fixture-isolation.md` | historical | **RECORD** ✔ with a dead recovery path → **F11** | its commit `14b4ee41` **is an ancestor of master** (`git merge-base --is-ancestor` → true), yet the doc's Rule-6d recovery command names `fix/taskfindjsonl-layout-determinism-v3`, a ref that **no longer resolves** |
| 18 | `deepseek-v4.1-flash-model-naming-and-kv-architecture-20260910.md` | historical | **RECORD** ✔ (finding not adopted in code) | alias collapse is real: `internal/pricing/pricing.go:125` (`modelDSFlash`), `internal/pricing/deepseek_v41_test.go:126` (vision alias priced in the same regime); production defaults are still `deepseek-v4-flash-vision-exp` (`internal/deepseek/claude_client.go:132`) and `BrewModel = "deepseek-v4-flash"` (`internal/tools/memory_brew.go:33`) while `/home/user/.gopi/settings.json` says `deepseek-flash` |
| 19 | `design-decision-process-group-20260903.md` | historical | **RECORD** ✔ (deferral held) | no `Setpgid` in the serve/worker child spawn path; the two live `Setsid` sites (`cmd/agent/cli_daemon_spawn.go:449`, `cmd/agent/merger_takeover.go:452`) are a different mechanism, each documented; the only `Setpgid` uses are a test helper and the build-gate child (`internal/tools/rebuild_vet_mergeset.go:535`) |
| 20 | `game-agent-testbed-design.md` | historical | **RECORD** ✔ (increment 1 not built; one gap closed since) | no `sgt-*` probe anywhere (`rg 'sgt-' --include=*.go` → 0); but its actuation gap was partly closed later: `gopi act` / `CliVerbAct` (`cmd/agent/cli_act.go:38`, added `bbbeaa94` 2026-09-11) |

**Roll-up:** 9 TRUE/RECORD-clean (2, 4, 7, 9, 11, 15, 16, 19, 20), 8 with a
concrete doc-side defect (1, 3, 5, 6, 8, 10, 12, 13, 14, 17 — see F1–F11), 1
informational (18). The 10 defect docs map one-to-one onto **F1, F3, F4, F5, F6,
F7, F8, F9, F10, F11**; **F2** is about `devdoc/DOC-INDEX.tsv` itself rather
than about a doc in the 20, which is why the finding list runs to F11.
**No doc in this family asserts a behaviour the code
contradicts in a way that would misdirect a run into a broken action**; the
defects are stale *pointers* (test names, branch names, line anchors, "not yet
done" lists).

---

## 2. Findings, ranked

Ranking = (chance a reader acts on the wrong thing) × (cost of acting wrong).
"Bug-worthiness" is my estimate of whether this deserves a bug task of its own;
the queue is untouched per the acceptance.

### F1 — `cli-naming-convention.md` names a test that does not exist  *(worth: low, doc-only)*
- doc:66 → ``TestSafeExceptionsAreStillNeeded`` (`cmd/agent/cli_completion_exceptions_test.go`).
  `rg` over the whole tree: **zero hits**. The real enforcer is
  `TestCLINamingExceptionsAreStillDispatchable`
  (`cmd/agent/cli_completion_exceptions_test.go:66`) — it does exactly what the
  doc describes (each exempt token must still dispatch and still differ from its
  dash twin). The doc's NEXT row already names
  `TestCLINamingExceptionsDocumentedInTopLevelHelp` (`:94`) correctly.
- **Fix:** one-line rename in the table. **No code change.**
- Why it ranks first anyway: this doc is the *normative naming contract* every
  CLI run reads; a reader who greps for the enforcement it promises finds
  nothing and concludes the rule is unguarded.

### F2 — `devdoc/DOC-INDEX.tsv` is already short: the docclass **coverage gate is RED on master**  *(worth: medium-high — a red gate on master)*
- `go test -run TestDocManifestCoversEveryDevdocFile ./internal/standards/` on
  master `52090dc0` → **FAIL**: the gate's own summary line reads *"2012 rows
  cover 2041 files under devdoc/"* (measured with this task's own changes
  stashed, so the number is master's, not mine) — **29 files have no row**, 25 of
  which the gate prints (its per-file detail is capped) before
  *"... and 4 more unclassified files"*.
- The gate's own comment states the intent: *"A new devdoc file with no row
  fails here (it would otherwise be invisible to every class-driven gate — the
  'anchor coverage is a lie' defect this work removes)"* — i.e. the manifest has
  silently allowed ~29 unclassified files.
- Consequence: every doc added since the manifest landed is **outside**
  `docanchor`'s scope and outside the banner convention. The unclassified set
  includes docs an agent would read as current (`merger-help-truth.md`,
  `task-process-liveness-predicate.md`, `procstat-parse-single-source.md`,
  `repo-context-gate-recognition.md`, …).
- **Fix:** run the generator (`tools/docclass/classify.py`, provenance-only, not
  part of the build) or add rows by hand — the content-first rule applies, so a
  bulk filename pass would be the exact defect the design replaced.
  **Bug-worthiness: medium-high** (a red coverage gate on master means the
  manifest cannot be trusted as a scope source until it is green; it is also the
  gate that makes every other doc gate honest).

---

### F3 — `centralized-architecture-20260830.md`: every "在途/待实现" item has landed  *(worth: low-medium, doc-only)*
DOC-INDEX classifies it **normative** ("standing claim about master, cited as the
live statement"), which is right for ①–⑤ — but the doc's closing sections read
as an open backlog and are all done:

| doc says | master |
|---|---|
| "M4.6+M4.7 chat 表 + remote dispatch（在途）" | chat table landed (`internal/tools/chat.go:78`); `feat/chat-remote-dispatch` merged (`80f8dd10`, `99e5893f`) |
| "stale sweep 位置（t-1788052204）——在途" | `serveStaleSweep` `cmd/agent/cli_tasks.go:3056`, fired on the serve tick, pinned by `cmd/agent/cli_serve_stale_test.go:66` |
| "`gopi shot`（t-1788043085）——待实现" | landed |
| "verify-loop（feat/task-verify-loop）验收待 M4.6 merge 后" | the dependency has long since merged |
- **Fix:** move ①–⑤ into a short normative body and mark the backlog section
  `[ALL LANDED 2026-09-…]` (or split the doc). **Bug-worthiness: low.** (An
  operator reading the backlog would re-dispatch finished work — Rule 26 makes
  that cheap, which is exactly why the stale list should not linger.)

### F4 — `path-policy-cd-base.md` line 16 asserts a fix is **absent from master** when it is present  *(worth: medium)*
- doc:14-16 — "Consumed by `gitInvocationStart` since the 2026-09-15 `-C` fix;
  **that fix lived only on `fix/worktree-residue-closeout`, not on master**."
- master: `internal/tools/path_policy.go:617` `func gitInvocationStart(tokens
  []string)` consumes `-C`, and `TestPathPolicyGitChangeDirIsNotTheTarget`
  (`internal/tools/bash_path_policy_test.go:218`) is green.
- **Worst direction of doc rot:** a merger/verifier reading this concludes master
  lacks a guard it actually has, and may re-apply a landed fix or distrust the
  path policy. Applies to a doc that DOC-INDEX marks **normative**.
- **Fix:** replace the sentence with a dated superseded note and re-anchor on the
  symbol. **Bug-worthiness: medium** (a "master is missing X" claim is
  actionable in the wrong direction).

### F5 — `taskid-vs-sessionid-architecture-20260909.md` describes the pre-decision state as current  *(worth: medium)*
- doc:7 "task_id = **t-<unix 秒>**（tasks.go taskIDPrefix…）" and the body still
  frames "消灭 t-<unix>" as the decision **to be taken**; the doc names the
  implementer spec `SPEC-db-cleanup-uuid-taskid.md`, which **exists nowhere**
  (the real follow-up doc is `devdoc/uuid-taskid-phase.md`).
- master: `taskIDGenerate` mints uuid v7 (`internal/tools/tasks.go:1248`, with
  the "uuid v7 was chosen over v4 for time sortability" rationale above it);
  `taskIDPrefix = "t-"` (`:134`) survives only to render/parse legacy rows
  (`strings.TrimPrefix(taskID, taskIDPrefix)` at `:1675`).
- So the *ruling* is implemented and the *doc* is frozen one step before it.
  **Fix:** add a dated outcome header ("landed: `taskIDGenerate` → uuid v7;
  `t-` is legacy-render only; the SPEC became `devdoc/uuid-taskid-phase.md`").
  **Bug-worthiness: medium** — this doc is the id-convention reference the
  briefing names as highest fan-in.

### F6 — `todo-list-design-evolution.md`: line anchors drifted off their subject  *(worth: low)*
Its reference block cites `loop.go:586-610` as the "TASK MANAGEMENT RULES
readonly 表述源". At base `52090dc0` line 586 is inside `efficiencyRules`
(`[…EFFICIENCY RULES…]`, `loop.go:~587`) and `taskManagementRules` starts at
`internal/agent/loop.go:641`. Same file, wrong region — a reader lands on the wrong block.
**Fix:** re-anchor by symbol (`var taskManagementRules`, `var efficiencyRules`).
**Bug-worthiness: low.**

### F7 — `PRINCIPLE-lifecycle-storage-completeness.md` calls itself "Rule 16", which is a different rule  *(worth: medium)*
- The doc's H1 is "**Rule 16**: Lifecycle Storage Completeness" (`:2`); its
  DOC-INDEX class is historical, but a reader who finds it via the task
  ledger/PROJECT context reads "Rule 16" as a GOPI.md rule.
- GOPI.md's Rule 16 is the `%w` gate (`GOPI.md:538`), and the lifecycle rule
  appears **nowhere** in GOPI.md (`grep -c 'Lifecycle Storage Completeness'
  GOPI.md` → 0). The doc's own closing argument — 陈述原则改变不了行为，检查才能
  — is unfulfilled: there is no check for storage completeness, while the six
  rules that *are* gated (`internal/standards/`) all carry one.
- **Fix (needs a call, not a copy-edit):** either renumber/retitle the doc to a
  devdoc principle (drop "Rule 16"), or promote it into GOPI.md with a gate.
  **Bug-worthiness: medium** — a phantom rule number inside a doc corpus that is
  read as a rule system.

### F8 — `wf-contract.md`'s `.gitignore` gotcha is stale (and was fixed)  *(worth: low)*
doc:82-84 — "`git add -f` is required … `.gitignore` carries a **bare `agent`
pattern** that silently ignores everything under `internal/agent/` for NEW
files … worth a follow-up fix on master".
- master `.gitignore:6` is **`/agent`** (anchored), with a comment that names the
  exact incident ("unanchored \"agent\"/\"gopi\" accidentally ignored
  `cmd/agent/*` and `internal/agent/*`"); `git check-ignore -v
  internal/agent/wfcontract_test.go` prints nothing, and the file is tracked.
- **Fix:** strike the gotcha (the follow-up landed). **Bug-worthiness: low** —
  but note the shape: this is the second doc in the family whose "master is
  broken" note outlived the fix (see F4).

### F9 — `perf-vs-claude-fix-plan.md`: the plan is delivered, its "Where" anchors are not  *(worth: low)*
Shape: a 2026-08-22 plan doc whose **proposals were all superseded by different
solutions**, so a reader sent there for "how context is bounded today" gets the
wrong numbers — and DOC-INDEX classes it **roadmap** (intended future work).

| lever | doc says | master |
|---|---|---|
| R1 | `DefaultContextWindow = 900000`, `TrimThresholdRatio = 0.9` → lower to 0.6 | `internal/agent/context.go:19` = **200000**; `internal/agent/context.go:33` `DefaultCompactTriggerRatio = 0.75`; **`TrimThresholdRatio` no longer exists** |
| R1 (bash) | reduce `softOutputSize` 16KB → 8KB (`internal/tools/bash.go:24`) | `internal/tools/bash.go:29` = 8KB, comment cites "(was 16KB — R1, devdoc/perf-vs-claude-fix-plan.md)" |
| R2 | restrict the per-round list in `internal/agent/loop.go:128` | rule block lives at `internal/agent/loop.go:641`; cadence `maxRoundsWithoutTaskList = 3` (`:602`) |
| R3 | remove double end-turn confirmation | solved by `emissionContract` (`internal/agent/loop.go:616`) + `trustedCompletion` (`internal/agent/loop.go:4376`) — a *different* mechanism (wf-contract) |
| R4 | parallel read batches | `executeToolBatchParallel` `internal/agent/loop.go:4557` |
| R5 | stop fsyncing every event | buffered `internal/agent/session.go:262-279`, `internal/agent/rawlog.go:218-224` |
| R6 | cache the code_search walk | `stalenessCacheTTL = 2 * time.Second` `internal/tools/symbol.go:73` |
| R7 | heartbeat is dead code, "wire it or remove it" | **still dead**: `Agent.HeartbeatInterval` / `HeartbeatStaleRounds` are assigned (`internal/agent/loop.go:2445-2446`) and never read anywhere |

- **Fix:** add a status header per lever (DELIVERED-as / SUPERSEDED-by / OPEN) and
  re-anchor R2/R7. **Bug-worthiness: low** — except **R7, which is a live loose
  end**: an env-var knob (`GOPI_HEARTBEAT_SEC`/`GOPI_HEARTBEAT_STALE`, read at
  `internal/agent/loop.go:2363-2372` and `internal/config/env.go:174`) that configures a field
  nothing consumes. That is Rule 21's shape (a knob that reads like a working
  knob) — **worth a small dedicated task** (wire it into `maybeNudgeStaleTaskList`
  or delete the fields + env entries).

### F10 — `roadmap.md` names a package file that no longer exists  *(worth: low)*
Its architecture tree lists `internal/deepseek/stream.go` ("SSE 流解析器"); the
file is gone (the SSE path lives in `internal/deepseek/claude_client.go`). The other seven named
paths exist. **Fix:** drop/update the tree line. **Bug-worthiness: low.**

### F11 — `taskfindjsonl-layout-fixture-isolation.md` carries a dead recovery command in its Rule-6d declaration  *(worth: medium)*
- The doc's deliverable declaration says the work lives on
  `fix/taskfindjsonl-layout-determinism-v3`, worktree
  `/home/user/workspace/gopi-layoutdet-v3`, with recovery
  `git -C /home/user/work/GoPi show fix/taskfindjsonl-layout-determinism-v3:…`.
- Measured: **the commit `14b4ee41` is an ancestor of master** (`git
  merge-base --is-ancestor 14b4ee41 master` → true, merged via `9021fa9b`), and
  the branch ref **no longer resolves** (`git log fix/taskfindjsonl-layout-determinism-v3`
  → "unknown revision"). So the work is safe — the recovery instruction is what
  is wrong: it points at the one location that was cleaned up.
- **Fix:** append a landed note ("merged into master as `14b4ee41` via
  `9021fa9b`; recovery = `git show master:internal/tools/taskledger_cost_test.go`").
  **Bug-worthiness: medium** — Rule 6d exists so a deliverable can always be
  found; a record whose only recovery path dead-ends defeats it, and a reader has
  no way to tell "the branch was cleaned up after merge" from "the work was
  lost".

### Adjacent cross-cutting docs (admitted content-first, not in the 20)
Sweeping the name-neither bucket for the same family found five more docs read as
normative by CLI/tool runs. All five are DOC-INDEX `historical` — correct — and
each is a record of a landed change; one carries a stale inventory:

- `devdoc/GOLDEN-RULES.md` — quotes-with-incidents collection; no code claims. ✔
- `devdoc/cli-verbs-constants.md` — the `cliVerbs` SSOT report; `cmd/agent/cli_verbs.go`
  holds 21 `CliVerb*` constants today (the doc's count is by-then). ✔
- `devdoc/cli-subcommand-groups.md` — memory/web group form landed
  (`cmd/agent/cli_memory.go:191` `memorySubcommandNames`, `cmd/agent/cli_completion.go:127`). ✔
- `devdoc/cli-help-dedup.md` — deprecation contract applied; the old spellings
  are gone from `cmd/agent/cli_verbs.go`. ✔
- `devdoc/loop-tool-registry-review.md` — **stale inventory**: it counts "exactly
  **20** tool definitions" and lists `todo` / `memory_store` / `write_task_area`
  / `task_evaluate`, all four now merged or retired
  (`internal/toolnames/names.go:57-64`), and its `validateToolArgs` coverage claim
  ("19 groups = all 20 tools") describes a validator shape that no longer exists.
  Its 20→17 transition is exactly what `TestGOPIRule3_MatchesClaudeToolDefs`
  (`internal/agent/gopidoc_sync_test.go:54`) now guards for GOPI.md — the tool
  inventory has **three** faces (code, GOPI.md, this review doc) and only two are
  gated. **Worth: low-medium**, doc-only.

---

## 3. Cross-cutting observation (the pattern behind F3/F4/F8/F9/F11, plus F2)

Every defect in this audit has the same mechanism: **a doc records a *delta*
("not on master yet", "requires `git add -f`", "the fix is on branch X",
"in flight") and the delta later closes without the doc hearing about it.** The
repo already solved this for the *anchor* case — `internal/agent/docanchor_test.go`
gates every `file:line` in every DOC-INDEX-normative doc, and
`internal/standards/docclass_gate_test.go` keeps the manifest honest — but
**nothing gates a delta**: branch names, "not yet landed" clauses, test names,
inventories, and `.gitignore` exemptions are all invisible to both gates. Two of
the findings (F4, F11) are pure delta rot; two more (F3, F9) are delta *lists*;
and F2 is the same mechanism pointed at the gate's own input — the manifest that
scopes the check has been allowed to fall behind the tree it classifies.

A cheap next step (not done here — it needs a review call, and it would be a new
gate, which is its own task): extend the doc-side gate to **classified claims** —
a line carrying a landable-delta marker (`not on master`, `on branch <ref>`,
`在途/待实现`, `git add -f`) must be either resolve-able against master or carry
a dated `[landed]`/`[superseded]` marker. Even without that, F4/F8/F11 show the
fix is often just a marker, so the cost per doc is minutes.

---

## 4. Verification (what was actually run)

| claim | command | observed |
|---|---|---|
| build clean at the audit base | `go build ./...` | exit 0, no output |
| doc anchors: does any of the 20 drift? | `go test -count=1 -run TestDocAnchorsResolveInNormativeDocs ./internal/agent/` | **FAIL** on master `52090dc0` — summary line: `doc anchors verified: 1950 across 404 normative docs … 201 pre-existing drift entries permitted, **18 new, 8 stale permits**`. Grep of the failure list for each of the 20 filenames → **0 hits**: no C1 doc contributes to the new drift (all 18 belong to other docs: `branch-task-linkage`, `bug-verify-closure-20260912`, `requirements/test-infrastructure`, `cwd-and-lifetime-recovery`, `delivery-point-sync`, `hippocampus`, `task-notify-terminal-gap-20260912`, `why-worktree-residue`) |
| doc-class gates | `go test -count=1 -run 'TestDocAnchor\|TestDocClass\|TestDocBanner' ./internal/standards/` | ok 0.862s |
| CLI naming enforcers (F1) | `go test -count=1 -run 'TestCLINaming' ./cmd/agent/` | ok (both real tests present and green) |
| GOPI.md Rule 3 tool inventory | `go test -run TestGOPIRule3_MatchesClaudeToolDefs ./internal/agent/` | ok — GOPI.md's 17-tool line agrees with `claudeToolDefs` |
| cmd/agent contract reds (#2) | `go test -run 'TestParseTasksSweepZombiesArgs\|TestEveryHelpConstantFitsTheWrapBudget' ./cmd/agent/` | ok 2.300s |
| banner fix (#9) | `go test -run TestNewAgent_ImageInputRecordsVisionModel ./cmd/agent/` | ok |
| rank ladder (#15) | `go test -run 'TestStatusRankTable\|TestRankApplyMatrix\|TestRankGuardAtomicity' ./internal/tools/` | ok 2.245s |
| path policy `-C` (#5) | `go test -run TestPathPolicyGitChangeDirIsNotTheTarget ./internal/tools/` | ok |
| taskfind fixture (#17) landed | `git merge-base --is-ancestor 14b4ee41 master` | true (⇒ F11) |
| taskfind determinism (#16) landed | `git merge-base --is-ancestor ee125fe3 master` | true |
| worktree clean (Rule 30) | `git status --porcelain` in the worktree | empty at report-writing time; re-checked after the commit |
| manifest coverage (F2) | `go test -count=1 -run TestDocManifestCoversEveryDevdocFile ./internal/standards/`, run **with this task's changes stashed** | **FAIL on master** — `2012 rows cover 2041 files under devdoc/`, 25 unclassified paths printed + `... and 4 more unclassified files`. Unclassified examples measured: `devdoc/merger-help-truth.md`, `devdoc/task-process-liveness-predicate.md`, `devdoc/procstat-parse-single-source.md`, `devdoc/repo-context-gate-recognition.md`, `devdoc/requirements/99-summary.md`, `devdoc/orphan-guard-delivery-point.md` |
| my own doc is classified | `awk -F'\t' '$1=="devdoc/crosscutting-doc-truth-audit.md"' devdoc/DOC-INDEX.tsv` | `historical` row added (this report is a dated record of a run against `52090dc0`, cited by no current artifact — the manifest's own rule for a past-run doc) |

**Not done here, honestly:** no fix was written for any finding (read-only audit
by design), nothing was submitted to the ledger, and F7/R7 (F9) need a *decision*
(wire-vs-delete; rename-vs-promote) that belongs to review rather than to this
task. The `perf-vs-claude-fix-plan.md` R7 knob and the un-gated tool-inventory
face in `loop-tool-registry-review.md` are the two items I would dispatch first.

**The one file this task did change**, and why: `devdoc/DOC-INDEX.tsv` gained a
single row for this report. Without it the (already red) coverage gate would also
be red *because of this task* — the report would join the 29-file unclassified
set, which is the defect F2 describes. No other file was touched.

**Coverage boundary of this audit:** I verified *that* the named artifacts exist
and *that* the named tests pass; I did not re-measure the numeric claims that are
not code pointers (token counts, wall times, row counts in the ledger, M1S/M1R
session statistics in `game-agent-testbed-design.md`, the alias-probe shas in the
DeepSeek doc). Those are properties of a run, not of master, and re-measuring
them was outside this slice — a reviewer should read them as dated observations.

**One methodology note for the reviewer (about my own base):** the reference
implementation of `.gitignore`/doc gates verified here is master `52090dc0`;
`internal/agent/docanchor_test.go` currently exits **non-zero on master** (18 new
+ 8 stale permits, none of them in this doc family). So "the doc gates are green"
is *not* a claim this report makes — it reports the one gate whose failure is
about *this* family's own subject (coverage, F2) and states the anchor gate's
state as measured.
