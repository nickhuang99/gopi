<!-- doc-class: normative -->

# GoPi Coding Rules — Full Reference (Tier B)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

> **Quoted, not prescribed.** This file is the verbatim ARCHIVE of the rule text
> that `PROJECT.md` (Tier A, the tracked project contract) states behaviourally. Where a rule below quotes a
> worker/daemon launch shape (`gopi worker`, `gopi task run`, `nohup …`), it is
> quoting a sentence for the reason it stands in the ruleset — an anti-pattern
> being named, an incident described, a retirement recorded — never a recipe to
> run. The operator owns the worker and the daemon; an agent must never start
> one — submit the work instead (`gopi task submit`). See
> devdoc/worker-ownership.md.

> **What this is.** The full text of every rule that `PROJECT.md` (Tier A, the
> injected SYSTEM PROMPT [2/2]) states behaviourally. Tier A is deliberately
> small because it is re-injected on every round of every session; the bytes
> that were costing the most — post-mortems, measured tables, "what was tried
> and failed", rejected alternatives — live here and are read on demand.
>
> **Nothing was deleted or reworded.** Every section below is the text of the
> rule as it stood in the Tier A file at commit 2d04bf96 (the moment of the split;
> that file became `PROJECT.md` on 2026-10-19 when it was split by content kind),
> moved verbatim. Tier A keeps each rule`'`s normative statements and names
> the machine check that catches a violation; when a rule has no machine gate,
> its operational text stays in Tier A *and* is reproduced in full here.
>
> **Precedence.** Tier A is the constraint you act on. This document is the
> argument behind it. If the two ever disagree, Tier A governs and this file
> has a bug — report it.

## Contents

Rules 1–34, The Commit Path, and the Machine-Checkable Gates section, in the
order they appear in Tier A. Headings are `## Rule N: …` — the same spelling
Tier A uses, so `grep -n "## Rule 27" devdoc/rules-reference.md` lands on the
full text of any rule.

---


> GoPi reads this file from the current working directory at startup.
> These are HARD CONSTRAINTS — not suggestions.
>
> **Configuration lives in `CONFIG.md`** (repo root): every env var, settings
> key, CLI flag, its default and its precedence, each with the source line that
> reads it. Read it before changing a knob, and update it in the same commit —
> `internal/standards/config_doc_gate_test.go` fails the build when the doc
> invents a knob, drops a catalogued one, or carries a line reference that no
> longer resolves.
>
> **Read it before EXPLAINING a knob too — not only before changing one.**
> "Which setting controls X?", "why did this node do/didn't do Y?", "is Z the
> gate for W?" are all answered in `CONFIG.md` with the source line that decides
> them. Answering from memory of the code, or from grepping one call site, is
> how the SAME knob gets attributed to the wrong axis (measured 2026-09-22: a
> question about which key gates branch push-back was answered "`sync_peers`"
> when `CONFIG.md` already said `role` owns push-back and `sync_peers` is the
> task-ledger peer whitelist). A wrong answer about configuration is a defect
> with the same cost as a wrong change to it — cite the doc, or state that you
> did not read it.


## Rule 1: No Magic Numbers or Bare String Keywords

### 1a. Magic numbers

Every literal number used for timeouts, sizes, limits, thresholds MUST be
declared as a named constant at the top of the file.

```go
// BAD — magic numbers scattered everywhere:
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
client := &http.Client{Timeout: 120 * time.Second}
timeout := 300 * time.Second

// GOOD — one place to find and change:
const (
    defaultAgentTimeout  = 0                // 0 = unlimited
    defaultHTTPTimeout   = 0                // 0 = no HTTP-level timeout
)
```

Why: The scheduler project had FOUR separate timeout bugs because timeout
values were inlined in different files (NewAgent, Run, runNonStreaming, HTTP client).
If they were all named constants, a single search would find all of them.

### 1b. Bare string keywords

The same discipline covers **string literals that carry meaning** —
identifiers, keywords, and values compared, switched on, written to a
store, or naming a file/directory/CLI verb. The hardcoding review
(2026-09-09) found 9 findings that were ALL string literals, not numbers:
tool names, task-status values, tag names, source values, CLI verbs,
directory names. If a class below has a constant or constant package, its
value MUST come from there — never typed inline:

- **Tool names** → `internal/toolnames` — ToolRead / ToolBash / … are the
  wire names the LLM emits. Never `"read"` inline.
- **Task-status values** → `internal/tools` exported consts
  (TaskStatusPending / TaskStatusClaimed / TaskStatusCompleted …).
  Never `"pending"` / `"claimed"` / `"completed"` inline.
- **Tag names** → exported consts (merge-backlog / merge-conflict / archive …).
- **Source values** → exported consts (`auto` / `human` / `llm`).
- **CLI verbs** → a cliVerbs-style constant source (each `isXxxSubcommand`
  predicate compares a constant, not a verb literal).
- **Directory / file names** → named consts (taskSessionsRootName, …).

```go
// BAD — bare string keywords bypassing a defined constant:
dir := filepath.Join(home, "sessions")     // "sessions" is taskSessionsRootName's value
switch st { case "pending": … }            // "pending" is TaskStatusPending's value
name := "read"                             // toolnames.ToolRead's value

// GOOD — one source of truth; renaming = one-line change:
dir := filepath.Join(home, taskSessionsRootName)
switch st { case TaskStatusPending: … }
name := toolnames.ToolRead
```

Why: a bare string is one typo away from silently mismatching the value
the rest of the system writes or compares. The 2026-09-09 review found
`"sessions"` written 10× as a literal around a constant used only 2×;
renaming then becomes an N-site grep-and-replace that misses one site and
corrupts persisted data (Rule 13 guards against this regression).

## Rule 2: Single Source of Truth for Deadlines

If a deadline can be overridden by env var, the constant is the default,
and exactly ONE place reads the env var.

```go
const defaultAgentTimeout = 0 * time.Second  // 0 = unlimited

timeout := defaultAgentTimeout
if v := os.Getenv("GOPI_TIMEOUT_SEC"); v != "" {
    if n, _ := strconv.Atoi(v); n > 0 {
        timeout = time.Duration(n) * time.Second
    }
}
```

## Rule 3: Tools Are Minimal — Don't Add New Ones

GoPi has 17 tools: read, write, edit, bash, code_search, keyword_search, web_search, web_fetch, git_blame, job, task, context_trim, task_submit, memory, vision_analyze, text2img, screen_capture.

`claudeToolDefs` in `internal/agent/loop.go` is the source of truth for that
list — `TestGOPIRule3_MatchesClaudeToolDefs` asserts the two agree, so a tool
added in code without updating this line fails the build. Read-only or
context-scoped member: `task` with action=view (returns the harness-tracked task
list) — task(action=commit)/task(action=review) are its in-session working-
area side effects, and task(action=reframe) is the epic reframe+evaluate
call (DOABLE | TOO_BIG + roadmap | IMPOSSIBLE + reason — `task_evaluate` was
RETIRED by the merge, its function absorbed here);
`context_trim` (drops a dead-end tool_result from context).


Do NOT add new top-level tools. All extensions go through:
- code_search for symbol definition lookups (functions, structs, interfaces, vars, consts) — one call returns file:line + kind + signature
- keyword_search for full-text search over any file (docs, comments, config, scripts)
- bash for build/test/run/git/file operations — NOT for searching code: grep/find are slower than the search tools and return raw lines you must filter yourself
- read/edit/write for file content
- memory (with action=search|store|update|feedback) for the knowledge base (hippocampus)
- task_submit for work that belongs in a fresh session, not this one; task(action=reframe) to judge an epic you have investigated (DOABLE / TOO_BIG+roadmap / IMPOSSIBLE+reason)
- Task-level configuration for agent behavior

## Rule 4: All Edits Are Surgical

Use edit() only for targeted string replacement — never whole-file replacement.
If old_string would match more than half the file, use write() instead.

## Rule 5: Build + Test + Commit + Document

**No task is complete without a written deliverable.**

Every change cycle: `go build` → `go test` → commit → **write report**.
Never report "done" without a document. No document = task not finished.

Report location: `devdoc/<feature>.md` or `tasks/<task-id>-report.md`

Minimum required content:
```
# Task Report: <summary>

## Completed
- What was implemented (list)

## Not Done
- What was deferred or skipped (be honest)

## Verification
- Build: <output>   (e.g. "go build: OK")
- Tests:  <output>   (e.g. "make test-budget PKGS='./internal/tools ./cmd/agent': 12/12 PASS")
          smoke, branch-scoped to what the diff touched — never the whole tree
          (`internal/smokegate`; a whole-tree run belongs to the off-peak heavy path)
- Ran:    <the exact invocation of the surface you changed, verbatim, with real
          arguments — the CLI verb+flags, the entry point, the payload you sent.
          `-h` is not a run. See Rule 31: a surface you did not execute is not
          delivered.>
- Result: <what you actually observed: output, exit code, or the state you
          queried afterwards to confirm the claimed effect>
- Commit: <hash>     (e.g. "a1b2c3d feat: ...")

## Evidence (self-filed)
- One line per gate claim: the command and the output you saw, or the
  measured value itself (exit code, `/proc/<pid>/cgroup` reading, the
  red-then-green transition). A claim about a fact the FRAMEWORK measures
  (your worktree's state, whether the branch merged) is the framework's to
  report — do not restate it here.
```

**A gate result the run did not observe is not a reportable result.** The
measured case (task `01a0af50`, 2026-09-17): a report asserted "`git status
--porcelain` empty in both worktrees (Rule 30)" while the same run's close-out
machinery printed five dirty entries for that worktree. The claim carried no
observation, and the observation that existed contradicted it — a reviewer has
no instrument for that. So a gate claim carries its own observation (the
`## Evidence (self-filed)` section above), or it is not made. The framework
does not re-run the gates a report claims; it **records** the sentences that
assert a result with nothing behind them (the seal's gate-claim note rides the
row's own trail) and never rewrites a run's prose.

**The red-proof RESTORE is a gate claim, and its criterion is machine-checked.**
A red proof ends by restoring the mutated production file *and* verifying the
restore, and the verification is the claim — so it is held to the rule above. An
index-relative diff is not evidence of a restore: a restore seeded from the index
writes back the very bytes the index holds, so on an ADDED file the restore is a
no-op that still exits 0 and the diff still prints nothing. Measure it with an
explicit revision — `git diff HEAD -- <path>`, or the index-free blob comparison
`test "$(git hash-object <path>)" = "$(git rev-parse HEAD:<path>)"` — and never
with `git diff <path>` alone, which is worktree-vs-INDEX (measured 2026-10-12;
the correction is one of this rule's own examples of a criterion that only looks
different). Recipe, measured failure and the gate:
`devdoc/redproof-restore-criterion.md`;
check: `go test ./internal/standards/ -run TestGateRedProofRestore`.

**The same rule has a TIMELINESS half (added 2026-10-14).** A content-identity
reading is anchored to *something*; when both sides are artifacts of the same run
— a snapshot copy (`*.fixed`) and its recorded digest — the reading is true only
about the moment it was taken, and **a later write does not move it**. Measured
(task `01a138317d847ac3806c8a701eb652f3`): the reading
`hash=732aeebc… saved=732aeebc… same=YES` was taken at 11:12:03, the file was
written AGAIN at 11:13:12 (arm (a)'s fix), the commit landed at 11:15:05, and the
delivered blob was `30a26e580…` — while the reading still said `same=YES` and
said nothing about the file having moved. So: anchor at least one side to a
**revision** (`git rev-parse HEAD:<path>`, `git show <rev>:<path>`,
`git cat-file -p <rev>:<path>`) — a revision is a thing a later write cannot
change, so the reading turns RED by itself when the file moves — and **if the
file has been written since the reading, RE-TAKE it**, even for a `gofmt`, a
comment or one more test arm. Recipe and the machine check (the same gate's
second arm; inline escape `red-proof-identity-ok:`):
`devdoc/redproof-restore-content-identity.md`.

## Rule 6: Isolated Work Directory + Git Push Semantics

**NEVER develop in the project's main directory** — another agent may
be working on a different task in the same project.

### 6a. Worktree is the STANDARD — clone only as an explicit exception

**Always use a linked worktree — never clone by default.** A worktree shares
the main repo's object store (no full copy), so it is far cheaper than
`git clone` + `rm -rf` (saves the clone/delete I/O on every task). Creating
the worktree and branch IS part of the development work — gopi does it
itself. First check whether a worktree for your branch already exists
(reuse it if so); otherwise create it — **never wait for or expect the
dispatcher (PA) to pre-create one** (worker mode: PA only dispatches and
never touches the child's development environment):

```bash
git worktree list   # shows the main repo + every linked worktree + branch

# 1) PROVE the name is free BEFORE creating it. --quiet prints nothing; the
#    exit status IS the verdict: 0 = the ref exists = TAKEN, 1 = free.
git rev-parse --verify --quiet refs/heads/feat/<name>

# 2) exit 1 → free: create it yourself.
git worktree add ~/workspace/gopi-<task> -b feat/<name>
cd ~/workspace/gopi-<task>
```

**A TAKEN NAME MEANS PICK ANOTHER NAME — never make room for it.** Exit 0 on
step 1 means `feat/<name>` is somebody's branch: reuse is not a shortcut, it is a
claim on another task's work, and the decision to take it is not yours to make
silently (a name whose branch you believe is finished still needs its owner's
row checked — `gopi task list`). So: choose a different name and run step 1
again. **Never** use the force spellings to overwrite an existing branch — they
do not refuse, they RESET the ref to your base and the commits it held survive
only in the reflog (measured: `git reflog show <branch>` → `branch: Reset to
HEAD`, the lost tip one entry below). The force spellings are `-B` on the create
verbs and the `-f`/`--force` flag beside a create flag; GOPI.md names them here
as the banned vocabulary, and `internal/branchform` + the standards gate
`TestBranchForceGateRedProof` fail the build if one appears in this file or in a
briefing gopi renders. This step checks the NAME; the PATH has its own failure
mode — git silently reuses a pre-existing EMPTY directory — which is
`briefings/brief-worktree-path-must-be-fresh.md` and is not fixed by this step.

- If a worktree already has YOUR branch checked out: work directly in
  that worktree. No clone needed. **A LINKED worktree shares the main repo's
  refs — committing in it already updates your branch in the main repo, so NO
  push is needed for that commit.** That sentence is about a MEASURED linked
  worktree and nothing else: it does **not** hold for a clone-channel ground,
  whose commit lives only in its own directory until the channel publishes it
  (see 6c). Measure the shape before you rely on it — `devworkspace.GroundModelOf`
  (`internal/devworkspace/ground.go`) is the ONE discriminator, and your own
  briefing's `[Your ground — ALREADY CREATED for you]` block states which shape
  it measured.
- **Cloning is the EXCEPTION**, allowed ONLY when a worktree cannot be
  created (e.g. read-only environment) — and the clone must still push
  its branch back per 6c. Default workflow: gopi creates the worktree
  (add + branch), works in it, PA merges and removes the worktree after
  acceptance. (2026-08-28: worktree confirmed as the standard after the
  clone/rm -rf I/O cost showed up in a task that cloned + deleted.)

```bash
git clone ~/work/GoPi ~/workspace/gopi-<task-id>/
cd ~/workspace/gopi-<task-id>/
git checkout -b feat/<task-name>
```

### 6b. After cloning, ALL tool paths MUST point inside the clone

The most common way to wreck work: reading/editing files with the MAIN
repo's absolute paths after cloning. Once you clone:

- **Every** read/write/edit/bash/code_search/keyword_search call must use
  paths under `~/workspace/gopi-<task-id>/` — relative paths (your CWD is the
  clone) or the clone's absolute path. **NEVER `~/work/GoPi/...`** paths:
  an edit "succeeds" there silently but lands in the main repo, where
  another agent is working.
- Self-check after each edit: `git status` in the clone must list the
  file you just changed, and `git status` in the main repo must NOT
  list it.
- If you discover edits in the main repo: STOP. Revert only the files you
  touched (`git restore <the files>` inside the main repo — never
  `git restore .`, which could wipe another agent's work), then re-apply
  the same edits in the clone. Do NOT continue in the main repo "just
  this once".

### 6c. Getting the branch back: the ground's own channel — two shapes, two answers

**Which answer applies is decided by WHICH KIND of ground you are in**, never by
a preference and never by the channel that dispatched you. The shape is MEASURED,
and the measurement has ONE definition:
`devworkspace.GroundModelOf(dir)` (`internal/devworkspace/ground.go`) — a
DIRECTORY named `devworkspace.GroundGitEntryName` (`.git`) means a CLONE of its
own (the clone channel's workspace), a FILE means a LINKED WORKTREE of another
repo. Everything that has to state a delivery consequence renders it FROM that
answer: the run's own briefing block, the delivery-proof arm
(`internal/tools/task_delivery_proof.go`) and the publish guard
(`internal/tools/git_publish_target_guard.go`) all ask the one function, so no
two of them can disagree. The two shapes have OPPOSITE correct answers, and
applying one to the other is the measured defect (four refs on the server on
2026-10-21 that no merge path consumes; measured 2026-10-27: a CLONE ground whose
own briefing called it a linked worktree and told it no push was needed).

- **A measured LINKED WORKTREE ground?** Done. The commit is already on the
  branch in the shared repo — that IS the deliverable. Verify with
  `git log --oneline -3` and stop. **You do not push.**
- **A /tmp clone with no channel behind it?** `git push origin feat/<task-name>`
  is the route — it works ONLY if that branch is not checked out in the receiving
  repo, so check `git worktree list` first. If the branch is checked out anywhere
  in the main repo (including a worktree), the push is refused by
  `receive.denyCurrentBranch`; the refusal is normal, not an error to fix.
  Instead, pull the commit into the worktree from the clone:

```bash
# from inside the assigned worktree:
git fetch ~/workspace/gopi-<task-id> feat/<task-name>
git merge --ff-only FETCH_HEAD
```

- **A CLONE ground (`.git` is a DIRECTORY — the clone channel's workspace)?**
  **Neither instruction above applies to you.** The channel has its own DELIVERY
  LOOP (clean → pull --rebase → declared build → push of the branch the ground
  stands on). **It runs ONCE PER RUN, from this run's END hook (the close-out,
  immediately before the row's status write) — NOT at a round boundary: there is
  no per-round push, and no push to catch mid-run.** (The briefing renders this
  sentence from `internal/tools/task_briefing_publish_clause.go`, the ONE source
  of the wording — if the two ever differ, the code is right and this is the
  bug.) So **do not push at all**: no `git push <remote> HEAD:<some-name>`, no
  new `feat/<name>`, no push of the integration branch from your checkout. A
  destination the RUN chose **creates a ref on the server that nothing merges
  and only an operator can delete** (and naming the integration branch from
  another checkout **moves** the server's integration branch). It is refused
  before it runs (`internal/tools/git_publish_target_guard.go`, on the bash
  tool), and `GOPI_ALLOW_DANGEROUS=1` is an operator decision, never a run's.
  Commit on your ground's branch, leave `git status --porcelain` empty, and
  report; publishing is the channel's job. A commit the server does not have yet
  is a line in your report — **not** a reason to invent a ref.

  **A push you have not seen YET is not evidence that the loop is broken** — it
  has not had its turn, and it gets exactly one. "No push during my rounds,
  therefore the delivery leg did not run" is a false conclusion with a measured
  cost: row `01a17921-fa90` passed twenty rounds, reported the leg as broken,
  and was wrong — its own log ends `clone-channel: attempt 1/M: push accepted`,
  and its commit was an ancestor of origin/master. The leg RAN; the report was
  noise. **The evidence that it RAN** is the channel's own line in that run's
  log (`clone-channel: attempt N/M: push accepted`, M being the channel's own
  brake `devworkspace.MaxPushAttempts`, or `clone channel: delivery delivered`).
  **The evidence that it did NOT** is the row left OPEN at close-out carrying
  the channel's non-delivery reason (`clone channel: delivery did not land — …`,
  `DeliveryDidNotLandReasonPref`). Quote one of those lines; never an inference
  from silence. If the commit is still not on the server when the RUN closes,
  that is the delivery loop's verdict to report — say it in your output.

**Why the shapes are separated, and why the prose is the fix here** (task
`01a1604a`). The stray refs were not written by gopi's Go code — they were written
by a MODEL that read this rule (written for a `/tmp` clone that has no channel) and
obeyed it: the run's own log says *"Better: push to a named branch"*. The
machine check (`git_publish_target_guard.go`, task `01a16033`) refuses such a push
**after** the model has decided to push; the delivery-loop clause bound into a
clone-channel run's briefing (`internal/tools/task_briefing_publish_clause.go`,
rendered at `cmd/agent/cli_tasks.go` under `if cloneRun != nil`) states the route
**before** the decision exists. Briefing scope and enforcement scope are one
condition, so a worktree run is never told a clone-channel rule.

### 6d. Deliverable declaration BEFORE cleanup (MANDATORY — data-loss prevention)

**NEVER delete your clone/worktree without first declaring the deliverable's
persistent location.** The FINAL message of every task MUST state:

1. **Branch name** where the deliverable lives (e.g. `feat/gopi-vs-codex-compare`)
2. **Recovery command** to fetch it (e.g. `git show feat/X:devdoc/report.md`)
3. Confirmation the commit is in the MAIN repo (pushed / worktree-shared)

Deleting the clone/worktree is ONLY allowed after this declaration. A
deliverable that exists only in a deleted clone = LOST work = unacceptable.
Historical incident (2026-08-28): a task pushed its branch then deleted its
clone per 6d, but its final message named no branch — the report looked lost
until recovered from the branch. The declaration above is now the gate.

### 6e. Clean up

```bash
cd ~ && rm -rf ~/workspace/gopi-<task-id>/
```

Merge is handled separately after review. Do NOT merge your own branch.

## The Commit Path — 7 Steps In Order

Each step names the mechanism that ENFORCES it. An unenforced step is prose
only — the next agent may skip it. (Step 7 was the unenforced one until
2026-09-22; the incident is `briefings/brief-commit-path-ancestry-check.md`.)

1. **Create the worktree + branch from master** — `git worktree add … -b feat/<name>`
   (Rule 6a). Enforced: the literal command, run by the agent — preceded by Rule
   6a's branch-name precheck (`git rev-parse --verify --quiet
   refs/heads/feat/<name>`; a taken name means pick another name, never a force
   spelling).
2. **Merge master into the branch before commit** — `internal/tools/task_finish_deliverysync.go`
   (delivery-point sync). Enforced, but SHORT-CIRCUITED by a dirty worktree.
3. **Build + test** — `go build ./...` + the project's suite (Rule 5). Prose only.
4. **Commit** — Rule 5's cycle. Prose only.
5. **Push to the peer** — finish-cleanup chain step ② (`task_finish_cleanup.go`)
   and `gitPushbackPlan` (`task_sync_git_pushback.go`). Enforced: the push is
   refused unless the branch fast-forwards onto the peer's master.
6. **Remove the worktree + the branch REF** — finish-cleanup chain steps ③④
   (same chain; Rule 30 gates the clean worktree). The branch REF is removed by
   Rule 9 step 2's one writer (`gopi task archive`), never by a hand-run
   ref deletion: that deletion would leave no audit record.
7. **The branch must DESCEND from master** — `gitPushbackSharesHistoryWithMaster`
   (`task_sync_git_pushback.go`, `merge-base --all` = empty ⇒ refused, by name).
   Enforced at the client push; NO server-side or receive-path check exists —
   a ref that arrives by another route (a manual `git push`, a fetch) is still
   unchecked, and only `gopi task merge`'s `unrelated-history` verdict catches
   it after the fact. *(This row read `gopi merge` until 2026-11-01; the
   top-level verb was removed by the merge-removal epic and the family spelling
   runs the same engine.)*

## Rule 7: Devdoc Everything Worth Mentioning

After implementation, if anything was tricky, surprising, or a design decision:
- Write a brief devdoc in `devdoc/<feature>.md`
- Future agents have no memory of this session — devdocs are their only window

## Rule 8: Git Safety — Add Files One by One

**NEVER use `git add -A` or `git add .`** — risks committing sensitive data.

```bash
git add internal/task/pool.go internal/task/pool_test.go
git commit -m "feat(pool): ..."
```

## Rule 9: 竞争收尾流程（民主裁决后）— 3 步固定 + safe-merge 前置

竞争结束（民主裁决后）必须**立即**执行 3 步，不等"有空再清理"：

1. **胜者分支 merge 到 master**（PA 验收通过后）
2. **败者分支归档清理**：`gopi task archive <branch> --reason hand-mistake --detail "<为什么>"` + `git worktree remove <path>`
   —— ref 的移除由 `gopi task archive` 的 reclaim 腿完成（它写 `archive/<branch>` 与 WHY 记录的同时删掉 ref），
   这里**不得**再出现裸的 `${git} branch -d|-D`：那是一次没有留痕的删除。
3. **复查**：`git branch` + `git worktree list` —— 确认只留 胜者 / master / 进行中分支

**merge 前置守卫**：任何 merge 前必须跑 `bash scripts/safe-merge.sh <branch>` 确认通过
（✅ exit 0 才允许 merge）。该脚本拦截三类漏网分支：
- 已打 `archive/<branch>` 存档 tag 的分支（败者）
- `loser/*` / `archive/*` 前缀分支
- 仍被 worktree 占用的分支

被拦截 = 该分支是败者或未清理 —— 立即回到第 2 步完成归档，而不是强行 merge。

## Rule 10: Case-Study Habits (text2img)

Seven habit rules from the text2img case-study (devdoc/text2img.md). These
are mirrored in the agent's system prompt [ENGINEERING RULES]; this rule
section keeps them visible to project-level agents (GOPI.md readers):

1. **Constraint dual-path**: blocked by a constraint (proxy/API/deps/permission)?
   Weigh bypass (mirror/alternative) vs accept — CREATE within the constraint
   FIRST (self-built renderer over mirror-swap).
2. **Pixel-level truth for render tasks**: verify render/vision output with
   pixel-level ground truth (IoU — PIL/FreeType), never downstream OCR scores
   (OCR can't measure render quality loss).
3. **Root cause before params**: on failure, dig the root cause first
   (5-why: symptom → mechanism → norm); A/B parameter tuning only after the
   root cause is confirmed — never "it crashed, tweak params".
4. **Minimal sample separation**: verify on the smallest sample (single char /
   zoomed / extreme value — z.txt 知知知); never trust "looks right" —
   layer truth checks to puncture it.
5. **Every fix carries `go test -run TestX`**: any bug fix ships with a
   minimal repro test (regression guard).
6. **Verification recipes go to memory**: proven verification methods/recipes
   → memory(action=store) (reuse, don't redo — e.g. "the 20px recipe lives in our
   own library #907").
7. **Dependency preflight**: assess dependency needs before a new task —
   ZERO-DEP is the target (stdlib first; external deps are the last resort,
   not the default).

## Rule 11: Tool Development Requires LLM Adoption Proof (nick deliverable standard)

**任何工具开发/修改（新增工具、修改工具描述、改 prompt 路由引导）必须以"LLM 真实调用该工具"为验收标准。**

- 写代码/改 prompt → 重建二进制（CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/）
- 设计 **2 个以上天然触发该工具的真实任务场景**（任务文件让 LLM 只能/应该用该工具完成）
- 跑真实任务（`echo "<任务>" | ./bin/gopi`）→ 验证 LLM 实际调用了工具（检查 ~/.gopi/sessions/*.jsonl 的 tool_use 记录）
- LLM 未调用 → 迭代修改 prompt（描述/路由）→ 重跑 → 直到稳定调用
- **只有验证通过才允许 commit** —— 光写代码+单元测试 = 半成品（LLM 没被说服使用，工作未完成）
- 验证证据（会话 id + tool_use 记录摘录）随 devdoc 报告交付，PA 亲自复核

**机理**：工具的价值 = LLM 真的用它。prompt 是工具的唯一说服机制（routing 引导 + 描述首句路由决策 + 命令式锚点）。全量会话审计（2026-08-27）：git_blame 0 次/21435、code_search 0.3%——工具存在但没被说服 = 白写。

## Rule 12: A Task Repo Declares Its Build — The Server Rebuilds After Merging Into Master

**Any git repo can be a task field** (not only /home/user/work/GoPi — the
generalization is a repo per task, and building is project-specific). For
gopi to rebuild the project's binary after a merge round, the repo's build
command must be **declared**, never discovered:

- **Declaration**: settings.json per-repo table —
  `"repos": {"/path/to/repo": {"build": "make build", "build_timeout_sec": 600}}`.
  GoPi itself declares `"build": "CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/"`
  (no Makefile dependency for itself; OTHER repos may declare `make` or
  anything their build needs).
- **No entry = no build**: a repo with no `repos.<path>.build` entry is
  skipped silently after a merge — no error, no guess (a docs-only repo, or
  a repo whose build isn't declared yet).
- **The table is ALSO the merger sweep list** (architecture-fix-v2,
  2026-09-03): every `repos` key is a repo the merger cd's to and sweeps
  each tick. An entry with an EMPTY build is a valid PURE MERGE TARGET —
  swept for branches but not rebuilt (merged-not-rebuilt; found=false for
  the build resolution, never a config error). So `{"repos":{"/repo/a":
  {"build":"make build"},"/repo/b":{}}}` sweeps both, rebuilds only a.
- **The close loop**: after a successful merge round (merger pass / manual
  `gopi task merge`) lands ≥1 branch into master, the server runs the
  repo's declared build in the working directory (master checked out) and
  verifies the binary's version equals the new master tip. Merge → cleanup
  → **REBUILD** is the last step: without it the machine runs the OLD code
  against NEW master (the recurring stale-bin incidents).
- **Build failure ≠ rollback**: the merge is done and good — git history is
  fine. A failed build (or a version mismatch) fires the `build-failed`
  notify event (subscribe via GOPI_TASK_NOTIFY_EVENTS) so the operator
  knows the binary didn't follow master. Never a crash, never a rollback.
- **To be a gopi task repo you declare your build**: repos.<path>.build is
  the contract that makes a repo a first-class task field.

## Rule 13: Constants Exist to Be Referenced — Never Bypass Them

**A constant that is defined but bypassed by bare literals is a defect.**
Defining `taskSessionsRootName` is not the discipline; *referencing* it
everywhere the value is needed is. The hardcoding review found constants
defined once and bypassed many times in the SAME package — not a
cross-package reachability problem, but missing backfill discipline:
`taskSessionsRootName` (defined 1×, referenced 2×, bypassed 10×),
`syncTaskUnknownNode` (defined 1×, bypassed 3× by sibling files).

```go
// BAD — the constant exists, yet the literal bypasses it:
func sessionDir(home string) string {
    return filepath.Join(home, "sessions") // "sessions" IS taskSessionsRootName
}

// GOOD — reference the constant; a rename changes exactly one line:
func sessionDir(home string) string {
    return filepath.Join(home, taskSessionsRootName)
}
```

**The exhaustive test is the only anti-regression guard.** A grep-gate test
asserts a given literal occurs NOWHERE outside its single definition site,
so a future re-inline or copy fails CI. Precedent:
`TestUnknownNode_MirrorLiteralGate` (internal/tools/memory_sync_task_test.go)
scans every production `.go` file in its dir and fails any that still carry
`"unknown-node"` — the literal may live only at the `syncTaskUnknownNode`
definition. Pattern:

```go
// test that closes the hole — the literal may appear in exactly one file
for _, f := range productionGoFiles(t) {
    if f == definitionFile { continue }        // the ONE allowed holder
    if bytes.Contains(read(f), []byte(`"unknown-node"`)) {
        t.Errorf("%s carries the literal — reference %s (defined in %s)",
            f, "syncTaskUnknownNode", definitionFile)
    }
}
```

Why: a constant and its bypassed literals silently drift apart — one site
gets renamed (or fixed for a data-corruption bug) and the bypassed sites
keep the old value, corrupting whatever is written or compared.

## Rule 14: Shared Values Live in a Small Exported-Constant Package

**A value shared across packages must have exactly ONE definition, in a
package every consumer can import.** Go unexported constants cannot cross
packages, so authors default to mirroring the constant in each package or
typing the bare literal — the archive-tag namespace, the "sessions"
directory name, and the source values (auto/human/llm) were the three
cross-package classes that dominated the hardcoding review's major
findings. The `internal/toolnames` package is the proven precedent: a tiny
leaf package (23 constants, zero imports of its own, zero import cycles)
that is the single source of truth for tool wire names.

```go
// BAD — mirror the constant into every package that needs it:
//   internal/a: const archiveTagPrefix = "archive/"
//   internal/b: const archiveTagPrefix = "archive/"   // drift waiting to happen

// BAD — unexported constant + bare literals elsewhere:
//   internal/a: const archivePrefix = "archive/"      // unexported: a-only
//   internal/b: filepath.Join("archive", name)        // bare literal (Rule 1b)

// GOOD — small exported-constant package (toolnames pattern):
//   internal/tasktags/tags.go:
//     package tasktags
//     const ArchivePrefix = "archive/"
//   consumers (any package) import internal/tasktags and use tasktags.ArchivePrefix
```

Rules of thumb:
- **Small and leaf**: only constants (and maybe their docs) — no business
  logic, no imports of project packages, so no consumer creates a cycle.
- **Exported**: every value other packages share is exported — an
  unexported constant in a shared package is a bare literal in disguise.
- **Never mirror, never bare-write** (Rule 1b): a second definition is
  drift; a bare literal is a defect (Rule 13).
- **Consumer-side CLI/user-facing verbs** that are not LLM wire values are
  still strings needing a source of truth — give them a constants file in
  the owning package, not a per-call literal.

Why: every mirrored copy is a future inconsistency — the fix lands in one
copy, the others keep the old value, and the mismatch corrupts persisted
state (tags written under one spelling and searched under another).

## Rule 15: No Silent Failure

**Errors must be handled or recorded — never swallowed. Two GoPi-specific
silent-failure traps:**

### 15a. `omitempty` can silently drop fields the decoder REQUIRES

`json:"field,omitempty"` omits a field when its value is the zero value —
fine for optional outgoing fields, but if the far side strictly validates
presence (or a required semantic value happens to be the zero value), the
message becomes invalid or the semantic is lost. GoPi precedent: the
DeepSeek deserializer rejected requests whose zero-valued fields were
omitted (`thinking` empty / `tool_use` without input → 400). The fix was a
non-empty placeholder that survives omitempty, and validation of what
MUST be present:

```go
// BAD — required-but-empty value silently vanishes from the wire:
Content string `json:"content,omitempty"`  // Content "" → key absent → decoder error

// GOOD — if the receiver requires the field, drop omitempty so presence is
// always explicit (zero value still round-trips):
Content string `json:"content"`

// GOOD (fallback) — keep omitempty only if you guarantee a non-zero value
// is always assigned before marshaling, and test that guarantee:
Content string `json:"content,omitempty"`  // always assigned non-zero; zero-value case covered by test
```

Decision rule: `omitempty` belongs on OPTIONAL outgoing fields only. If a
field's presence or a non-zero value is required on the wire, do not let
`omitempty` erase it — and add a test that round-trips the zero-value case
(future_field_regression_test.go precedent).

### 15b. Swallowed errors

`err` must be handled or recorded — an error discarded with `_ =` or
ignored after a call that can fail hides the failure. GoPi precedent: a
`>/dev/null` redirect swallowed every remote handoff error, so a failed
nohup handoff looked successful. Prefer explicit propagation or logging:

```go
// BAD — swallowed:
nohupCmd.Run()                // error discarded
_, _ = io.Copy(dst, src)     // silently truncated copy

// GOOD — handle or record:
if err := nohupCmd.Run(); err != nil {
    log.Printf("handoff failed: %v", err)   // at minimum: record it
    return err                              // better: propagate
}
```

Decision rule: every error return is either checked-and-handled,
propagated, or logged with context — never bare-discarded. Comment the
rare intentional discard (`// deliberately ignored: best-effort flush`).

---

## Machine-Checkable Gates (Rules 16–21)

Rules 1–15 are prose: they describe the right thing to do and trust an agent to
notice. **These six are enforced by a test that fails red** — they land in the
build where prose never did. The whole implementation (predicate, baseline,
red-proof tests, run instructions, backlog) is `internal/standards/` +
`devdoc/standards-gate.md`; each rule below carries the checklist row it comes
from, its machine check, and what was deliberately *rejected* so a future agent
does not "re-add" it as a gap.

Run all six: `go test ./internal/standards/` (it runs as part of the module
suite — off the merge path, which is smoke and branch-scoped per
`internal/smokegate`; no external linter, no new dependency — stdlib
`go/parser` + `go/ast`, zero CGO).

Common mechanics (one paragraph, because all six share them): each gate scans
`internal/` + `cmd/` and fails on any hit that is neither in its **baseline** —
an exact, explicitly-reasoned `file:line` in `internal/standards/baseline.go` —
nor marked with the gate's **inline escape** comment. A baseline entry that no
longer matches is also an error (a stale permit is a silent permission), so the
allow-list can only ratchet down. An **inline escape** is the opposite of a
baseline: it says *this line is correct and here is why*, and the site keeps
showing up in the data report
(`go test -run TestStandardGateDataReport -v ./internal/standards/`) as a
deliberate exception rather than disappearing into a suppression.

### Rule 16 — `fmt.Errorf` must wrap with `%w`, not `%v` (checklist 2.2)

A wrapped error rendered with `%v` breaks the chain: `errors.Is`/`errors.As`
branches silently never fire. Write `%w`. If the wrapped error is deliberately
not part of the chain, mark the line `// no-wrap: <reason>`.

```go
// BAD — errors.Is(err, ErrNotFound) will never match:
return fmt.Errorf("load config: %v", err)
// GOOD:
return fmt.Errorf("load config: %w", err)
```

**How to verify:** `go test -run TestStandardGates/TestGate1ErrorWrapRedProof ./internal/standards/`
— the gate flags only `fmt.Errorf` calls whose `%v` argument is error-shaped, so
`fmt.Sprintf`/log lines are untouched. 352 pre-existing sites are baselined
(codemod is its own task; this list may only shrink).
**Deliberately rejected:** a whole-tree `errorlint`/`golangci-lint` gate — the
tree has 352 hits, a red-on-day-one gate gets disabled. The baseline is the
adoption mechanism, the gate is the ratchet.

### Rule 17 — tests must use `t.Setenv`, never `os.Setenv` (checklist 5.2)

`os.Setenv` in a test leaks process state into sibling tests (order-dependent
flakes); `t.Setenv` restores automatically. Use `t.Setenv`; an individually
reviewed exception is marked `// env-leak-ok: <reason>`.

```go
// BAD:  os.Setenv("GOPI_X", "1")              // leaks past the test
// GOOD: t.Setenv("GOPI_X", "1")               // restored by testing.T
```

**`t.Setenv`'s undo scope is the TEST, so it is the wrong tool for a per-call
flip.** A closure that re-points an env var *per call* (a transport hook
answering one peer per invocation) needs the flip undone **when the call
returns**; a `t.Setenv` there leaves the last call's value installed for the
rest of the test. That case is the reviewed `os.Setenv` + restore pair
(`internal/stylegate` `RuleTestSetenvScope` enforces the direction), and the
restore must be **`internal/testutil.SnapshotEnv`** — it captures with
`os.LookupEnv`, so an ABSENT variable comes back absent. The hand-rolled
`prev := os.Getenv(k)` + `os.Setenv(k, prev)` form is **lossy**: `os.Getenv`
renders *unset* and *set to ""* identically, so the variable stays PRESENT
(holding "", or a `t.TempDir()` the framework already removed).

```go
// BAD (the shape gate 2 reports — "os.Getenv cannot restore absence"):
prev := os.Getenv("GOPI_MEMORY_DB")
os.Setenv("GOPI_MEMORY_DB", dbPath)
os.Setenv("GOPI_MEMORY_DB", prev)              // "" when it was UNSET
// GOOD (per-call flip):
defer testutil.SnapshotEnv(t, "GOPI_MEMORY_DB")()
os.Setenv("GOPI_MEMORY_DB", dbPath)
```

**How to verify:** `go test -run TestStandardGates/TestGate2TestEnvRedProof ./internal/standards/`
(the call form, the registry arm that scans the tree) and
`go test -run TestGate2EnvRestoreShapeRedProof ./internal/standards/` (the lossy
save/restore shape, with four negative probes pinning precision — a TOP-LEVEL
exhaustive test that holds no `gates()` entry, so its own name is the whole
command: `TestStandardGates/<it>` would select no subtest and exit 0).
The gate scans **every** test tree — `internal/`, `cmd/` **and the top-level
`testsuite/` e2e suite** (`internal/standards` `testRoots`; the root list is
pinned by `TestGate2ScansTheE2ESuite`, so dropping a tree fails the gate rather
than silently un-gating it).
(96 legacy sites baselined; note `t.Setenv` is *incompatible* with
`t.Parallel`, so a "just parallelise it" fix is not the answer here).
**Deliberately rejected:** mandating `t.Parallel` on every test — see checklist
§13; it interacts badly with these env sites until the cleanup lands.

### Rule 18 — no `else` after a terminating `if` body (checklist 4.3)

If the `if` body ends in `return`/`break`/`continue`, the `else` is dead weight
and the branch should be de-indented (guard clause). A reviewed exception is
marked `// else-ok: <reason>`.

```go
// BAD:                                  // GOOD:
if n < 0 { return 0 } else { return n } // if n < 0 { return 0 }
                                        // return n
```

**How to verify:** `go test -run TestStandardGates/TestGate3ElseAfterReturnRedProof ./internal/standards/`
(7 legacy sites baselined).
**Deliberately rejected:** `gocritic`'s full `ifElseChain` ruleset — it also
rewrites `if/else-if` chains, a judgement call; this gate is purely syntactic.

### Rule 19 — never `panic` outside `init`/`main`/`Must*` (checklist 2.7)

Panicking **instead of** returning an error takes the choice away from the
caller. Constructors that cannot fail usefully may panic under the `Must` naming
convention. A deliberate panic (e.g. re-panicking inside a `recover`) must carry
`panic-ok: <reason>` in the comment block directly above the function.

```go
// BAD:  func load(p string) string { if p == "" { panic("empty") } … }
// GOOD: func MustLoad(p string) string { … panic("empty config path") }
```

**How to verify:** `go test -run TestStandardGates/TestGate4PanicRedProof ./internal/standards/`
— the gate reports at the *enclosing function* line, so the escape must sit in
that function's comment block; a *non-contiguous* marker does not escape
anything. Baseline is EMPTY by construction: the one existing site
(`internal/agent/crashdump.go recoverRunPanic`, the crash-dump re-panic) is a
true positive that is deliberately kept and annotated. Any future `panic` is
reported as new.
**Deliberately rejected:** a repository-wide `panic` ban (checklist §13) — it
would forbid the crash-dump re-panic, which is the correct behaviour;
`init`-time invariant panics are also legal.

### Rule 20 — no `get`/`is`/`handle` prefix on an accessor (checklist 1.4; Google Style decisions `#getters`)

A method that only reads a value should be named for the value (`Name()`, not
`GetName()`). The check is scoped to **niladic methods** on purpose: with
arguments the name is a command, and a package-level `Get`/`Handle` is a command
or a tool name — GoPi's own tool surface is full of them. A reviewed exception is
marked `name-ok: <reason>` **in the comment block directly above the function**
(a function declaration line is not always a valid comment-emission point).

```go
// BAD:  func (c *Config) GetName() string { … }
// GOOD: func (c *Config) Name() string    { … }
// GOOD: func (c *Config) Get(key string) string { … }   // has an argument ⇒ command
```

**How to verify:** `go test -run TestStandardGates/TestGate5AccessorPrefixRedProof ./internal/standards/`
— 4 true-positive sites (`internal/shot/x11.go` `getRootImage`, `getImageBand`,
`handshake`) are annotated and visible in the data report; baseline is EMPTY, so
a new prefixed accessor fails immediately.
**Deliberately rejected:** a blanket ban on `get`-prefixed identifiers — GoPi
legitimately has `getenv`-style and tool-name shapes; the gate's scope (niladic
method + `+Uppercase`) is what keeps its false-positive count at zero.

### Rule 21 — every env-name constant must have a REAL reader (checklist row 1b/env)

A constant that names an environment variable must be **read** somewhere — in a
call's argument list (`os.Getenv`, `t.Setenv`, `config.EnvBool`, a helper funnel
such as `boolEnvTrue(...)`), in production or in a test. A hook that is declared,
documented and read by nobody is worse than a missing feature: the help text and
the devdoc both claim the knob works, so an operator who sets it concludes the
behaviour is active while nothing consumes the value. An env constant whose
reader deliberately lives elsewhere (an alias to another package's constant, or a
namespace prefix rather than a knob) is marked `// env-reader-exempt: <reason>`
in its doc comment — a reviewed, reasoned, visible exemption, never a weakened
check.

```go
// BAD — declared, documented, read by nothing (the defect that motivated the gate):
const scratchGCMinEnv = "GOPI_TEST_SCRATCH_GC_MIN"   // no os.Getenv(scratchGCMinEnv) anywhere

// GOOD — read inside an env call's argument list:
if v := os.Getenv(scratchGCMinEnv); v != "" { … }

// GOOD — an alias to the single source of truth is not a reader, and says so:
// env-reader-exempt: alias of scratch.GCIntervalEnvName, which owns the reader.
const ScratchGCIntervalEnvName = scratch.GCIntervalEnvName
```

**How to verify:** `go test -run TestStandardGates/TestGate6EnvReaderRedProof ./internal/standards/`
(the predicate on synthetic sources) and
`go test -run TestGate6EnvReaderFindsTheRealTree ./internal/standards/` (the
exhaustive scan — it fails if the reader index comes back empty, so the gate
cannot read green-blind). Baseline is EMPTY by construction: the hook that
motivated the gate (`GOPI_TEST_SCRATCH_GC_MIN`) was wired in the same change.
**Deliberately rejected:** flagging an inline `os.Getenv("GOPI_X")` bare literal —
that is Rule 1b's business, not this gate's; the predicate is scoped to *declared
constants*, which is what keeps its false-positive count at zero.

### Why these six and not the other ten

A rule is a gate only when the check is **deterministic** (a predicate over the
AST, no judgement call), **cheap** (stdlib, runs inside `go test`), and
**adoptable** (green on the day it lands, red only on new violations). Rules
1–15 describe the desirable state; these six *compute* it. The 13 remaining
Google-extracted items (initialisms, error-string shape, `%w` placement, naked
returns, keyed struct literals, gofmt-clean, doc comments, package names, …) are
checkable and are listed with their predicates in
`devdoc/coding-standards-checklist.md` §16.1 and
`devdoc/standards-gate.md` (backlog); the six that are *not* mechanically
checkable are excluded with reasons in §16.2 — **do not re-propose them as
gaps.**

## Rule 22: Temp Workspaces Never Live on the `/` Partition (a criterion, not a value)

**Every temporary workspace a gopi process or its children create must be
provably OFF the `/` partition.** `/tmp` is a directory on `/` on this host, and
`/` is the 147G boot partition the 2026-09-11 exhaustion took from 19G to 1.5G
free before another 23.5G of residue accumulated there. This is a CRITERION —
state it as a property to check, never as a path to copy:

- **Check the property, never a spelling.** "Temp work is off `/`" is decided by
  measuring the device id of the resolved temp path against the device id of
  `/` (`stat -c %d`, or `internal/scratch.AssertTempRootOffRootFS` /
  `scripts/lib/tmp-ban.sh:gopi_assert_off_root_fs`). A path is never acceptable
  merely because it is not spelled `/tmp` — a different directory on the same
  partition is the same defect, and an explicit TMPDIR pointing at `/tmp` is
  the same defect as the implicit fallback to it.
- **No hardcoded `/tmp` in scripts.** A script that names `/tmp` (or any fixed
  path it did not measure) has chosen a partition it cannot verify. Take a
  directory from the scratch tree instead: `gopi_scratch_mkdir` /
  `gopi_scratch_queue` in `scripts/lib/tmp-ban.sh`, or `internal/scratch.MkdirTemp`
  in Go. A bounded loop that writes temp data must ALSO be bounded by an
  iteration cap, not only by `timeout` (a fast loop outruns the clock).
- **Prefer the explicit layer over the ambient default.** The system default
  (`TMPDIR` unset → `os.TempDir()` → `/tmp` on a default host) is exactly the
  implicit fallback that produced the residue: nobody chose it and nobody
  noticed it. Configure the workspace instead — the ordinary `tmpdir` parameter,
  settable three interchangeable ways through the `internal/config` catalog
  (`--tmpdir PATH` > a caller-exported `TMPDIR` > `GOPI_TMPDIR` > the
  settings.json `tmpdir` key), which gopi publishes into its OWN environment so
  every child it spawns inherits it, and which never overrides a caller's
  `TMPDIR`. That precedence lives in exactly one place,
  `config.ResolveTmpDirConfigured`; the parameter is documented in
  `devdoc/tmpdir-param-not-banner.md` (and `gopi --help`). With nothing
  configured gopi publishes its OWN default — an ordinary directory under the
  workspace (`<home>/workspace/_tmp`), because the workspace is the operator's
  stated working space and the default must be the obvious user-facing one
  rather than a tuned one. An operator who wants the memory-backed scratch tree
  (measured faster for SQLite scratch: 411.9ms vs 16.8ms for a fresh ledger DB)
  sets the parameter — that choice is theirs, not the default's. A temp root
  that cannot be provided or written fails LOUDLY, naming the path, the reason
  and the channel that supplied it (never a silent fallback to `/tmp`), and an
  ordinary run never announces its temp root: the value is readable on demand
  with `GOPI_DEBUG=1`.
- **Never fix this at the host level.** GoPi's temp-root policy lives strictly
  inside gopi's own process tree — its environment, its settings, its children.
  `/home/user/.bashrc`, `/home/user/.profile`, `/etc/tmpfiles.d`, `/etc/environment` and every
  other host-level configuration are out of scope: an operator tried that and
  rolled it back, and a tool that rewrites a host's shell profile to fix its own
  temp paths is not a fix.

Why a criterion and not a value: a value (a default temp path) is copied into
every script and rots the moment the host's geometry changes; a criterion is
re-checked by the measurement above and stays true on a host where `/tmp` is a
separate filesystem. The mechanical halves of this rule already exist and are
the ones to call — `internal/scratch.AssertTempRootOffRootFS` (Go, measured
device ids + explicit/implicit classification), `internal/testutil`'s suite
entry-point pin, and `scripts/lib/tmp-ban.sh` (shell). Rule 22 states the
contract those implement; it does not replace them, and it does not restate
Rule 1 (which bans magic literals, a different failure) or Rule 10.7 (the
dependency preflight that motivates the zero-dependency scratch tree).

## Rule 23: Never Continue Work on Someone Else's Failed Branch — Open a Fresh One

**Ban**: do not check out a branch someone else's attempt failed on and
commit there — not a failed/archived branch (`git checkout fix/some-failed-attempt`
→ edit → commit). **Correct practice**: open a FRESH branch from master —
prove the name is free first
(`git rev-parse --verify --quiet refs/heads/feat/<name>`; exit 0 = TAKEN → pick
another name, never a force spelling), then create it
(`git worktree add /home/user/workspace/gopi-<task> -b feat/<name>`) and implement
your own way. The full step — including why a taken name is a claim on someone
else's work — is Rule 6a. Branches cost nothing.

**Encouraged**: DO read the old branch first — `git log <branch>`,
`git show <branch>:<file>`, `git diff master...<branch>` — the failed path is
information, not garbage.

**Why**: a hard bugfix/feature takes several walls; the branch records the
path already walked (including attempts that only LOOK wrong); editing on top
of it erases that reasoning, so nobody can see why it got there — two people
pulling one diff. Failed attempts are evidence, kept on purpose. The old path
may be needed again: a problem can recur. Ref: Rule 9 (archive keeps the
branch = keeps that path).

## Rule 24: Process Independence Is Judged by cgroup — Never by PPID or setsid

**Criterion**: the ONLY evidence that a process has left its login session is
`/proc/<pid>/cgroup` NOT matching `session-*.scope`. PPID=1, PGID==SID and a
successful `setsid()` are not evidence — they say nothing about the cgroup.

**Counterexample**: `SysProcAttr{Setsid: true}` (or `setsid nohup … &`) does
NOT change the cgroup — the process still dies with the ssh/terminal session
that spawned it, because systemd tears the whole `session-*.scope` down.

```
grep -q 'session-.*\.scope' /proc/<pid>/cgroup && echo "NOT detached" || echo "detached"
```

**Correct practice**: a resident process that must truly detach acquires its
OWN cgroup (`systemd-run --scope`, measured ~36ms; `--user --scope` when a
user bus answers); if it falls back to setsid-only, that degradation must be
announced, never passed off as detached.

**Sibling trap** (same family — "it looks independent" taken as evidence):
never probe liveness with `pgrep -f "<pattern>"`; it self-matches (the shell
or ssh process issuing the command carries the pattern in its own cmdline).
Use `pgrep -x <name>` or `ps -p <pid>`.

**Why**: `setsid` reshapes process group/session; systemd scopes are cgroups.
Two machine-wide kill incidents came from this gap — devdoc/worker-cohort-kill-20260912.md,
devdoc/signal-provenance-20260913.md; mechanism, rejected criteria and the
choice of this landing spot: devdoc/detach-judgment-rule.md.

## Rule 25: Daemons Run Unprivileged — a Privilege Is Never the Remedy for a Cgroup Escape

**Ban**: never prescribe `sudo`/root (or a root SYSTEM scope) as the fix for a
resident daemon's cgroup-escape failure — not in a diagnostic line, not in a
usage head, not in a dispatched task summary. A privilege is not a missing
ingredient; asking the operator for one is itself the bug.

**Measured (uid 1000, cgroup v2 — every row RUN, nothing inferred; full table in
devdoc/no-privilege-daemon.md)**:
- `sudo gopi merger --daemon` lands in the cgroup of the **unprivileged shell that
  launched it** (`/user.slice/user-1000.slice/session-N.scope`) — a process
  **inherits its cgroup from its launcher**, so `sudo` changes the uid and nothing
  about the membership;
- root has no user manager of its own to migrate into
  (`/sys/fs/cgroup/user.slice/user-0.slice/user@0.service` does not exist), so
  mechanism ② cannot rescue it either;
- no unprivileged migration works either: cgroup v2 gates a `cgroup.procs` write on
  the **shared ancestor** of source and target, and `user-1000.slice` is
  `root:root 0755` — `mkdir` under the delegated subtree succeeds, the migration
  returns `EACCES`.

So a privileged daemon buys nothing and costs the whole process tree to root. The
principle is the operator's: **no privileged solution for daemon running**.

**Correct practice**: run the daemon unprivileged and **announce** the
degradation (`setsid-only`, still in the login session scope) — never a silent
downgrade and never an invented remedy. The honest levers are `--require-scope`
(refuse the start instead of degrading) and a host service unit, which is the
**operator's own deliberate switch** — not a password gopi asks for.

**Anti-regression**: the operator-facing TEXT is what is gated, not just the code
path. `cmd/agent/daemon_scope_no_privilege_test.go` parses every production
`.go` file of `cmd/agent` and fails any STRING LITERAL carrying a prescription
shape (the bare `sudo` spelling, `re-run it as root`, `as root inside a SYSTEM
scope`, `Privileges (which mode needs them)`, `requires super user`, …), with a
read-floor so a broken glob cannot pass vacuously and a presence half so the
correction cannot be satisfied by deleting the explanation. **Mention is not
prescription**: the corrected text names the privilege to DENY it ("no privilege
is needed", "running it as root does not obtain the escape either") — that is the
correction working.

**Counterexample (the defect this rule closes)**: task `01a09d00` ("escape cgroup
requires super user") was dispatched, its branch grew a hint prescribing
`systemd-run --scope --unit=… <gopi argv>` as root plus a "Privileges (which mode
needs them)" usage section, and master adopted it (`f4f761c5`) — teaching every
future reader that gopi's daemons need root.

**Why**: a hinted privilege is followed (the operator did ask for passwordless
sudo), which silently re-roots the daemon, its children, its worktrees and its
ledger for zero gain — and it hides the real answer, which is that the escape
does not exist on that host.

## Rule 26: An Incomplete Fix Is Repaired by Dispatching Again — Never by Waiting

**Practice**: when a fix turns out to be incomplete, **dispatch a new task**. Do
not stall on the question "is someone already working on this?" A duplicate
dispatch costs one agent's time; a stalled repair leaves the defect live.

**Dispatching the same work twice is allowed and expected.** Agents judge for
themselves whether a task is already done — the briefing must therefore carry
**the reason and the evidence**, not instructions to avoid overlap. Write what
is broken, what is already known, what was tried, and what "done" means; the
executor decides whether to continue, redo, or补齐 the remainder.

**Corollary — an incomplete fix must be narrow, never wrong.** When a fix lands
a partial answer, say so in its own report (`## Not Done (honest)`), name the
remaining gap, and make it dispatchable. A fix that overstates its coverage is
worse than one that never landed: it closes the ticket and hides the defect.

**Overlap is handled by the briefing, not by withholding work.** If two tasks
may touch the same area, each briefing states the sibling task id and what it is
doing, so the executor can read its current state and coordinate. Do not leave
the overlap unstated, and do not use it as a reason to skip the dispatch.

**Why**: the worktree-residue family (2026-09-14) sat unfixed across several
reports because each one closed with "the code fix is not in this branch" and no
one re-dispatched. The operator's framing: *"if it repairs incompletely, dispatch
a new one to cover it — even duplication is fine, the agent judges for itself;
you only have to write the reason clearly."* This is the ordinary way bugs get
fixed.

## Rule 27: NEVER Generate Device Load to Satisfy a Test — FORBIDDEN, No Exceptions

**Forbidden.** No test, script, benchmark, harness, or acceptance condition may
saturate a shared resource — the block device, the CPU, the memory, the network —
in order to produce a number. This is not a discouragement; it is a ban. There is
no size at which it becomes acceptable, and no epic that authorises it.

**Concretely forbidden** (each of these has actually happened):
- `dd if=/dev/zero of=... oflag=direct conv=fsync` in any loop, at any width.
- `while :; do ...; done` around any writer, reader, or syscall that touches a device.
- Spawning N parallel workers whose purpose is to make a metric (e.g.
  `/proc/pressure/io` "full avg300 ≥ 90%") cross a threshold.
- Any "synthetic pressure" / "load generator" / "stress" helper, however briefly
  it is meant to run, however bounded its loop claims to be.

**Why the ban is absolute, and why it is not about resource etiquette**: our
system has **no requirement this test serves**. gopi is not an online service with
an SLA, it holds no latency promise under device contention, and we do not measure
`/proc/pressure/io` — that is a kernel metric, not a product metric. The load was
never measuring our code; it was manufacturing a broken environment and then
measuring that.

**Dispatched twice as "just to reproduce a slow fsync"**: 2026-08-27 (`/` filled),
2026-09-11 (48 concurrent writers, `/` to 1.5G free, hermes had to be killed),
2026-09-16 (three overlapping `iopress.sh` batches × 16 workers = 48 concurrent
`oflag=direct conv=fsync` writers, load 56.42, every `dd` in state D). Three
incidents, same shape, same excuse.

**The reasoning that produces this is itself the defect.** A test suite was slow
because a `tx.Commit()` fsync was slow. The correct instrument is to **measure
that `Commit()`** — it is our code and it is directly observable. The wrong
instrument is to **flood the shared device until the slowness appears**, then
report that the suite is slow. The first measures the subject; the second
manufactures the subject's environment and measures the damage. Whenever a task
concludes "I need to create load to see this", the conclusion is wrong: find the
direct measurement instead.

**Worse than an ordinary runaway, in two ways**:
1. **It outlives its producer.** Every `iopress.sh` was `PPID=1` — when the Bond
   that launched it died, the load kept running. That is why three batches of
   three different ages were alive at once.
2. **It is invisible to every gate.** `grep -rn 'iopress'` over the repo returned
   **zero hits** — it was hand-written into a worktree's `.evid/`, so no checked-in
   check could see it. That is why the rule written after the second incident did
   not prevent the third.

**What to do instead** — for each of these there is a direct instrument:
- "the operation is slow" → measure that operation (`time`, a duration assertion,
  a histogram), never the device.
- "it is slow under contention" → say so as a hypothesis, and file it; do not
  build the contention. If a contention-dependent regression is genuinely needed,
  it belongs in a **checked-in, explicitly opt-in, capped** test that never runs
  on the merge/verify path.
- "the suite exceeds the budget" → measure the suite's wall time and name the
  slow package/test, or measure the one call implicated. A suite budget is a
  property of our code, not of the host's disk.

**Heavy tests are classified, not tolerated.** A test that takes minutes of real
device work, or that any reasonable person would not want on a shared machine
while other work runs, must be **tagged as heavy and excluded from the
merge/verify path** (development and merging need smoke coverage; heavy runs
belong in an explicit off-path run). Skipping is only acceptable when the skip is
**listed and auditable** — a silent skip is a deleted test wearing a passing
green, which is the `t.Skip`-without-a-reason failure mode this repo already
counts.

**This rule is enforced by a check, not by this paragraph.** If you are reading
this because you were about to write such a test: don't. If you find one that
exists: delete it, and report where it came from so the acceptance condition that
motivated it is corrected too — removing the generator without removing the
number it was written to hit leaves the next agent to reinvent it.

**A note for the specific case that produced this rule (2026-09-16)**, so the
next reader does not repeat its exact reasoning: the epic was "the `cmd/agent`
suite exceeds Go's 10m package budget". The diagnosis underneath it was **real
and useful** — every fixture's ledger open pays the host's disk cost, ~360 tests
sum to more than 10 minutes, and the ledger's first-open schema batch commits
inside one transaction (`memory.go`, measured parked in `tx.Commit() → xfsync`).
What was wrong was the *instrument chosen to demonstrate it*: the branch reached
for a machine-wide `≥90% /proc/pressure/io full avg300` condition, i.e. it made
the host sick to prove the host was sick. The measurements it produced are
legitimate observations to cite (a single fsync at 0.7-1.3 s/op **on a
saturated host**), and the fixture-cost conclusion stands on its own by simply
**timing the suite and naming the slow call** — no saturation required. Keep the
conclusion; never keep the generator.

**Where the rule is CHECKED (2026-09-16, so the ban is no longer prose only).**
The rule lived in this document after the second incident and the third still
happened, so it is now mechanical:

- `internal/testutil/rule27_gate_test.go` — the check. Red on a `dd` with
  `oflag=direct`/`conv=fsync` (absolute, loop or not), on an unbounded
  `while :; do … done` wrapping a device writer, on `dd` naming a raw block
  device, and on a committed synthetic-pressure helper (`iopress`, `stress-ng`,
  `loadgen`, …). It scans the WORKING TREE, untracked files and dot-directories
  included — that is where 2026-09-16's generator actually lived (`.evid/`), and
  it red-proofs both the static file form and a script generated at run time. It
  also PRINTS the generator shapes it finds in sibling checkouts under
  `~/workspace/` (a report, not a failure: those belong to another agent's
  worktree) — measured 2026-09-16, the original batch was still parked in
  `gopi-firstopen/.evid/`.
- `internal/standards/heavy_inventory_gate_test.go` — the companion check for
  the classification rule: every heavy-test call site must appear in
  `testdata/heavy-tests.tsv`, every row must have a call site, and the gate
  packages may NOT be classified heavy (a merge must never land with its own
  gates skipped).
- Heavy tests are **off by default**: a test declares itself with
  `heavytest.Require(t, criterion)` and skips unless `GOPI_HEAVY_TESTS` is
  truthy. `make test` is the smoke (merge/verify) path; `make test-heavy` is
  the ONE named opt-in, deliberately run off-peak. The classification criterion,
  the inventory and the three incidents are recorded in
  `devdoc/heavy-tests-rule27-gate.md`.
- **On the merge/verify path the rule is: SMOKE, BRANCH-SCOPED — never the whole
  tree** (`internal/smokegate` owns that sentence and the smoke command itself,
  Rule 14). Smoke because the path runs under a per-branch cap while the merge
  lock is held; BRANCH-SCOPED because a merge can only regress what its diff
  touched; never the whole tree because that is the off-peak heavy path's job.
  `TestSmokePathPrescribesNoWholeTreeSuite` (internal/standards) fails on a
  whole-tree command span in any merge/verify-path briefing — a Go string
  literal under `internal/tools`/`cmd/agent`, or a shipped briefing artifact at
  that path (`cmd/agent/merger_takeover_prompt.md`, which the retired daemon
  took with it — the gate reads it when it is there). Comments and docs are NOT
  gated: the ledger's baseline sweep legitimately runs the whole tree, and a
  report has to be able to quote the command this rule retired.
- The retired instrument: the epic's `≥90% /proc/pressure/io full avg300`
  acceptance clause is withdrawn and replaced by a direct measurement of the
  implicated call (the per-run `go test -json` stream's per-package wall time +
  slowest tests, and `execSchemaBatchOnce`'s `tx.Commit()` duration). See
  `devdoc/ledger-dsn-cmd-agent-contract.md`, "the pressure clause, REPLACED".

## Rule 28: Two Hosts, Two Jobs — peerbox Owns the System Layer, HP Owns Features

**The division** (operator's decision, 2026-09-16):

| host | role | work |
|---|---|---|
| **peerbox** | server | **system-level**: worktree lifecycle, branch/merge machinery, the merger, task ledger, sync, scheduling — the parts that define how work flows |
| **HP** | client | **feature development** that does **not** touch the system layer |

**Why it is a rule and not a preference**: everything that went wrong in
2026-09-16's incidents went wrong *because both hosts worked the same layer at
once* — fourteen HP branches sat on a stale baseline while peerbox reworked the
same areas, so each one needed a rebase to land and then a compile fix anyway
(`fix/dump-name-collision` merged with "zero conflicts" and did not build). The
division exists so a feature branch comes back **mergeable**, not so the hosts
have different labels.

**What "does not touch the system layer" means, concretely.** A feature task is
HP-shaped if it:
- changes product behaviour a user can see (`cmd/agent/cli_*.go` command
  surfaces, `web/` UI, a `internal/<feature>/` package of its own), and
- does **not** change how work is scheduled, claimed, merged, or cleaned up
  (`cli_tasks*.go` runner paths, `merger*`, `task_reclaim*`, `worktree*` sweep,
  `internal/testutil` gates, `internal/standards` gates).

A task that needs both is two tasks; the system half goes to peerbox, the feature
half to HP.

**Migrating a task to the other host is a three-step move, and the cancel is not
optional**: export the payload, submit it on the target host, and **cancel the
original row** with a reason naming the new row id. Without the cancel both hosts
claim it and the work runs twice (Rule 26's duplicate-dispatch allowance is for
*repair*, not for an unowned second run). Tag the new row so its origin is
visible (e.g. `migrated-from-peerbox`).

**Before dispatching to HP, check the briefing's own assumptions.** The GUI tasks
migrated on 2026-09-16 each opened with "read
`devdoc/gui-chat-roadmap.md` first, `git show docs/gui-chat-roadmap:…`" — and
that branch **never existed**; the file was on `master` and untracked in one
working tree. A briefing that instructs the reader to `git show <branch>:<path>`
must have that branch verified to exist on the host that will run it, or the
executor starts blind.

## Rule 29: The Main Repo Hosts Work In Progress — Never a Scratch or Reference Target

**Ban**: never point a task's development at a branch/worktree that is not its
own. Using a *foreign* branch's worktree in the main repo as a scratch
directory, a test target, a comparison baseline, or a reference checkout is
forbidden — a worktree is a repo **alias** (it shares the main repo's object
store and refs), so driving experimental commands inside another task's
worktree mutates the very workspace its owner is building in.

**Correct practice**: clone (or `git archive` / `git show <branch>:<path>`) when
you need another branch's content. A clone is a separate repository with its own
object store — the isolation you were reaching for. Read-only inspection of
another branch costs nothing and needs no checkout at all:

```bash
git fetch origin feat/the-other-branch       # not even needed for a local branch
git show feat/the-other-branch:path/to/file  # read a file without touching a worktree
git diff master...feat/the-other-branch      # compare without checking anything out
git rev-parse --verify --quiet refs/heads/feat/<mine>   # exit 0 = TAKEN → pick another name
git worktree add /home/user/workspace/gopi-<mine> -b feat/<mine>   # YOUR OWN worktree only
```

The precheck line is Rule 6a's step: a taken name is somebody's branch, so pick
another name and re-run it — the force spellings (`-B` on a create verb, or a
`-f`/`--force` flag beside one) do not refuse, they RESET that ref to your base.

**Why**: the main repo is not a lab bench, it is the shared floor where every
in-flight task stands. An experiment run inside someone else's worktree is
indistinguishable, from the outside, from that task's own work — and the
merger's branch-attribution verdicts (`internal/tools/branch_attribution.go`)
report a live process standing in a checkout as evidence that the branch's owner
is still working. A borrowed worktree therefore does not merely risk a conflict:
it makes the ownership record *lie*, which is the one input the merger uses to
decide whether to recycle a branch. Two workers whose paths cross in one
worktree produce exactly the "whose run is this?" ambiguity the attribution
classifier exists to remove.

**The mechanical half, and what it can and cannot do.** The classifier names the
checkout a process holds and the ledger row it attributes the branch to, so a
borrowed worktree becomes *visible* (two different rows claim one checkout:
`branchAttributionRowWorktree` returns the row's recorded directory and
`branchAttributionCheckout` reports the directory actually registered — a
mismatch is reported, not resolved). It does not *prevent* the borrow, and it
must not be allowed to: a rule enforced by guessing intent would refuse honest
work. Prevention is the discipline above — one task, one worktree, one branch,
and a clone for everything else.

## Rule 30a: "Aligned" Is Not "Finished" — the Delivery-Point Sync Runs LAST, and Proves Nothing About the Work

**The delivery-point sync (aligning your branch with the integration branch) is
the LAST step before hand-over, not a milestone you tick off along the way.**
Do it once, at the moment you are ready to release the branch. Aligning early
buys nothing: the integration branch keeps moving, so an alignment done an hour
ago is stale — you would have to redo it anyway.

**And a successful alignment says NOTHING about whether your work is done.** It
is a statement about a *merge* ("this branch now contains the integration
branch"), not about your *run* ("the work is finished"). A run can be perfectly
aligned and still be mid-flight: unpushed commits, iteration still going, the
row still `in_progress`. Measured 2026-09-22: two branches were "aligned", their
rows read `in_progress`, and one holder was still alive — while machinery read
the alignment as evidence of completion and freed the worktree out from under
the live run.

**The order that holds:**

1. finish the work
2. commit it (`git status --porcelain` prints nothing — Rule 30)
3. **then** align with the integration branch (delivery-point sync)
4. push / hand over

Never report "done", never let anything treat you as done, on the strength of an
alignment. Only a terminal row whose branch carries the work is done.

## Rule 30: A Run Leaves Its Worktree CLEAN — `git status --porcelain` Prints Nothing

**Before a run reports ready, `git status --porcelain` in the run's worktree
must print nothing.** Not "mostly clean", not "clean except scratch" — nothing.
This is not tidiness; it is the difference between a deliverable that can be
merged and one that is permanently invisible: an unremovable dirty worktree is
picked up by no merge path, while the task row may still read `completed`.

```bash
cd <your worktree> && git status --porcelain   # must print nothing
```

**Both shapes of dirt are violations, and their remedies are OPPOSITE** — which
is why "clean up" is not the rule:

- **Untracked entries (`??`) are litter.** Evidence files, logs, generated
  scripts, scratch checkouts — anything not meant as a deliverable — belongs
  *outside* the worktree (under the session directory, the established home of
  a run's outputs) or must be deleted before the run ends. The worktree is not
  a scratch pad.
- **Tracked changes (`M`, `A`, `D`, `R`, `C`) are an UNCOMMITTED DELIVERABLE.**
  Commit what is deliverable. Deleting it, or "cleaning the scratch", destroys
  the work — this is the worse shape and the one that also reads as someone
  else's in-progress edit to every guard downstream.

**The rule is checked, not trusted** (原则无效，检查才有效). At close-out the run's
own recorded worktree is measured, and a non-empty `git status --porcelain`
becomes an attributed record on the row's own audit trail — event
`completed-unclean` for untracked litter, `completed-unclean-tracked-work` for
uncommitted tracked work, each quoting the offending entries and naming the
worktree — plus the same sentence on stderr. Nothing is deleted, no branch is
touched: the violation becomes **visible** instead of silently poisoning the
branch. `GOPI_TASK_RUN_RESIDUE_ENFORCE=1` additionally refuses the `completed`
writeback for the run's OWN untracked litter (the row is left open so the run
can clean up and write back).

**An uncommitted TRACKED change is not merely recorded — it REFUSES the
`completed` claim.** A run whose deliverable *is* the code it wrote has not
delivered if that code is in no ref: the branch points at an unmodified base,
so it is invisible to the merge queue while the row reads `completed` — the one
status nobody re-reads. Measured 2026-09-16 (task `01a0af50`): the framework
detected the dirt and said so four times, the row was written back `completed`
anyway, and ~1000 lines of finished, tested work sat uncommitted in an abandoned
worktree until a human rescued it. So for the run's OWN worktree holding a
tracked change (`M`/`A`/`D`/`R`/`C`/`U`/… — anything porcelain v1 does not mark
`??`), the `completed`/`verified` writeback is refused: the row stays OPEN and
the refusal names the worktree and the exact entries. The run commits its work
and writes back; an operator who has reviewed the tree may allow that one
writeback with `GOPI_TASK_DIRTY_CLOSEOUT_ALLOW=1` (the override is recorded as
its own event, so "recorded" and "deliberately let through" stay
distinguishable). The gate reads ONE list of delivery claims, so it guards the
verifier's `verified` stamp exactly as it guards the run's own `completed` — a
dirty row cannot be handed a clean bill of health one door later.
**The gate refuses the STATUS claim — it never commits,
stashes, discards or deletes anything**, because the commit message is part of
the deliverable and the tree may hold another session's live work. Two
conditions keep it off a tree that is not the closing run's: the worktree must
be git-attributable to the row's own branch AND no other row may hold a live
claim on that same directory — a sibling run's live work is never this run's
refusal, nor is untracked test residue (whose handling is the paragraph above,
unchanged).

**This rule is not a licence to build a tolerating machine.** Do not weaken the
"uncommitted work is work" guards (`reclaimSkipDirtyReason` and the reclaim
refusals built on it) to make a dirty worktree recyclable; do not add heuristics
that classify some untracked paths as "probably scratch, safe to delete". A
dirty worktree is a defect the producer prevents — never a condition the
machinery learns to live with.

## Rule 31: A Command You Did Not Run Is Not Delivered — Execute the Surface You Changed

**A build that compiles and a suite that passes are NOT evidence that your
change works.** They are evidence that your change did not break the things the
suite already covers. If the code path you added has never been executed, you
have not tested your change — you have tested your change's surroundings.

**The requirement.** Before reporting ready, every surface you changed must have
been **invoked for real, with real arguments, against real state**, and you must
report the invocation and its output:

- **A CLI command or subcommand** — run it. Not `-h`. Run the real verb with
  real arguments on a real ledger/repo. If it refuses, that refusal is a result
  to report; if it claims success, the claimed effect must be visible afterwards
  (query the thing it says it changed).
- **A flag on an existing command** — run the command **with that flag**. A new
  flag whose code path has never entered is untested, whatever the coverage
  number says.
- **A function with a caller you added** — exercise the caller end to end, not
  the function in isolation.
- **A new file format / wire payload / protocol leg** — produce one and consume
  it, or send it across the real boundary and read the other side.

**A unit test is not a substitute for this.** A test calls your function the way
the test author imagined. The failure mode this rule exists to catch is the
opposite: the code is correct in isolation and unreachable, or reached with an
argument shape nobody ever passes, in a mode nobody ever selects. Only running
the actual entry point finds that.

> **Retired mechanism (2026-11-05).** The cross-node sync family this page
> describes — `gopi task sync`, `diff`, `harvest`, `push`, `send`, `receive`,
> `pull`, `sync-pass` — is DELETED. No verb in this binary opens a session
> against another node's ledger; each of those spellings still dispatches to a
> removal sentence, and work that must run on another host is FILED there with
> `gopi task submit`. The text below is the record of the mechanism as it was,
> kept verbatim because it is the evidence the removal is judged against.

**The measured case (2026-09-21).** `gopi task sync --push-only --task-id T`
documents, in its own help text: *"with `--task-id T` exactly that row travels
(the claim broadcast's manual face — idempotent)"*. It was implemented, it
compiles, and it was reported done. Run for real, it fails twice over:

```
$ ./bin/gopi task sync --push-only --task-id 01a0c2b1-...
gopi task sync: memsync push: no sync_peers declared

$ GOPI_SYNC_PEERS=hp ./bin/gopi task sync --push-only --task-id 01a0c2b1-...
push hp: unreachable: ssh hp: exit status 1
  (stderr: gopi task sync: claim-remote: want 1..8 row(s), got 2181)
```

**One invocation.** That is the whole test. It would have been found by typing
the command once — the same command a human then tried to use, in the workflow
this feature was built for. Instead the operator found it, and the operator had
to file two bug tickets for a feature that was reported complete.

**Why Rule 5 did not stop this, and why "add a test" is not the fix.** Rule 5
requires `go build` → `go test` → commit → report. Both of its verification
lines were honestly satisfied: the build was green and the suite passed. Neither
line can see this class of defect, because **the suite does not contain the new
entry point**. The feature was never reachable by any test that existed, and no
coverage metric says "this CLI verb was never typed". A regression test someone
adds *after* being told to is worth less than the one invocation that was
already available.

**Evidence required in the report** (this is the `## Verification` section Rule
5 already defines — this rule tells you what belongs in it):

```
## Verification
- Build:  <output>
- Tests:  <whole scope + pass count>
- Ran:    <the exact invocation, verbatim, including flags and arguments>
- Result: <actual observed output / exit code / the queried state after>
```

**A report that says "implemented" without a `Ran:` line for the surface it
changed is incomplete** — the same standing as a report with no document at all.
If the surface genuinely cannot be run in the environment (needs hardware, a
peer host, credentials you lack), say exactly that in `## Not Done`, name what
is blocked and why, and say who can run it — **an honest "I could not run this"
is acceptable; a silent claim of completion is not.** The rule bans the silent
claim, not the honest limit.

**Two constraints, so this rule cannot be satisfied by cheating:**

1. **Do not run a load generator, saturation script, or synthetic press to make
   the invocation "produce" something** — Rule 27 already bans this absolutely.
2. **Do not widen the thing you are testing to make the invocation succeed.**
   If the peer accepts 1..8 rows and you sent 2181, the defect is on the sender.
   Raising the cap would have made the command "work" while deleting the
   protocol property it enforces. Cite the rule you are tempted to relax in
   `## Not Done` instead.

**This rule is the same principle as Rule 30, applied one level up.** Rule 30
says a deliverable in no ref has not been delivered; Rule 31 says a surface in
no execution has not been tested. In both cases the producer's honest belief is
not the evidence — the recorded observation is.

## Rule 32: A Test Gets Its Database From the Scaffold — Never From a Hand-Rolled Path

**The one call, and it is the default answer:**

```go
path := testutil.NewTestDBPath(t)                      // WRITER: a fresh per-test DB
db, err := sql.Open("sqlite", tools.LedgerDSN(path))    // open it through the tools layer

db := testutil.Sample(t)                                // READER: shared, read-only, seeded
path := testutil.SampleDBPath(t)                        // READER, when a subprocess needs the path
```

`internal/testutil/dbfixture.go` owns both shapes. The writer shape gives you a
fresh, empty database under the pinned scratch root, pins `GOPI_MEMORY_DB` (and
masks the legacy spelling), and refuses to hand you a path inside the operator's
live profile. The reader shape gives you a database that was built **once per
run** from one documented seed definition and opened **read-only** — its DSN is
`mode=ro&_pragma=query_only(true)` and its file is mode `0444`, so a write through
it fails instead of damaging the sample. Signatures, the seed rows a test may
assert on, the stability promise, and a worked before/after migration:
`devdoc/testdb-scaffold-contract.md`.

**NEVER hand-roll a database path or a schema in a test.** Name the anti-pattern:
a helper that resolves its own path (`filepath.Join(t.TempDir(), "memory.db")`)
and opens it — the shape that exists ~300 times in `internal/tools` alone, in 28
differently-named per-file helpers. Do not add the 29th. A helper that takes the
path as a parameter is not the problem (the caller owns the pin); a helper that
invents the path and never pins the environment is. Each one type-checks with or
without the `t.Setenv` on the next line, so nothing catches the day it is deleted
— and with no `GOPI_MEMORY_DB` in force, a tools-layer call in that test resolves
the operator's **append-only** ledger, where a fixture row is permanent (that is
how `t-stale-sibling` made the merger report a phantom `/tmp` repo for days).

**NEVER touch the operator's profile.** `testutil.RefuseOperatorProfileWrite`
(and `PathLiesInOperatorProfile`) anchor on the OS user record, not `$HOME` — a
refusal there is the guard WORKING, not an obstacle to route around. Do not
weaken it, and do not "fix" a red test by unsetting the guard.

**Never commit a binary `.db` fixture.** The seed DEFINITION is the artifact
(`SampleSeed` / `SampleSeedEvents` / `SampleDocs`); the file is a build product,
built at test time.

**A device-backed fixture belongs in the scratch home, not in the source tree.**
If a test's subject needs a real block device, take the directory from
`testutil.TestDeviceScratchDir(t, sub)`, which places it in the shared, GC'd
scratch home (off `/`, Rule 22) and MEASURES the backing, skipping where there is
no device. Do not reach for the checkout with `os.MkdirTemp(".", …)`: that is a
scratch directory inside the source tree, where a `git add` run while your test
is mid-flight commits it — measured 2026-09-21: five
`internal/tools/firstopen-device-*/` directories holding 341 untracked test `.db`
files, 106 of them once captured in a commit, and the repo-root junk gate is
blind to a nested tree because it validates repo-root paths only.

**How a violation is caught:** `internal/standards/db_scaffold_gate_test.go`
(`go test -run TestTestDBScaffoldIsUsedOrTheLedgerEnvIsPinned ./internal/standards/`)
flags (a) a test **function or helper** that resolves a database path itself and
opens sqlite without pinning the DB environment, and (b) a scratch directory
created inside the source tree. A reviewed exception carries `db-scaffold-ok: <reason>`
inline; a site that pre-existed the gate carries an exact `file:line` permit with
its reason, and a permit that no longer matches fails the gate (the allow-list may
only ratchet down).

**The gate is PARTIAL, and this rule says so rather than implying otherwise.** It
does not catch a path resolved in one helper and opened in another, an opener
reached through a variable, or a test that pins the env and then ignores the
pinned path. The accepted residual is the corpus migration
(`devdoc/testdb-scaffold-migration-inventory.tsv`: ~300 sites in `internal/tools`,
~30 elsewhere; sibling ticket `01a0c2e7-…`), not a claim that the gate covers it.

## Rule 33: `parent_task_id` Is a Dependency GATE — a Child Runs Only After Its Parent Is Released

**The gate.** A pending row is claimable **iff** its `parent_task_id` is empty
**OR** the parent row's status ∈ **{completed, verified}** — the EXACT release
set. Every other parent status (`pending`, `claimed`, `in_progress`, `failed`,
`cancelled`) parks the child. A `parent_task_id` naming **no row at all**
(a dangling id) is also NOT claimable: an unresolvable dependency is never
assumed satisfied. The check is ONE predicate — `taskRowClaimable`
(`internal/tools/tasks.go`), deciding via `taskParentGateDenied` — so every
dispatch entry point that shares the claim funnel (worker round / `gopi task
next` / serve) and the pending view's `[queue-skip]` marker give the SAME
answer.

```bash
gopi task submit "<child briefing>" --parent-task-id <parent-id>   # chain construction
gopi task list --status pending
# t-…  pending  …  [queue-skip] parent t-parent  not released (status=pending; releases on completed|verified)
# t-…  pending  …  [queue-skip] parent t-ghost   missing from the ledger
# t-…  pending  …  [next]                        ← the row a dispatcher takes next
```

**The anti-pattern this names.** Treating `parent_task_id` as a *lineage note*
— a tree link that is rendered by `task show` and respected by nobody — so a
child whose parent is still running (or was cancelled, or failed) is dispatched
into a fresh session against a premise that does not hold yet. Its mirror,
also an anti-pattern: "the parent is nearly done, just claim the child". A
chain the queue ignores is not a chain; it is prose.

**Why the gate.** A split announces a dependency and the framework must honour
it, not the reader. `parent_task_id` is already written by the paths that
produce real chains — the verify loop's rework child (`TaskVerifyToReopen`,
`<id>-r1`) and reframe's roadmap children (`ReframeRoadmapAgent` fan-out) both
link the row they derive from — so a child that runs before its parent
invalidates its own briefing.

**The check (mechanical, so this rule is not prose only):**

```bash
go test -run TestParentGate ./internal/tools/    # the gate's own tests
```

The tests drive the REAL surfaces (`TaskNextFiltered` — the shared claim
funnel — and `TaskList` — the pending view), and pin the red path: a pending
parent parks the child **while an unrelated row is still offered** (a park is
never a head-block), the parent reaching `completed`/`verified` hands the child
to the very next claim, a dangling parent id is denied explicitly, parentless
rows are unaffected, and the listing's `[next]` marker agrees with the row the
funnel actually claims.

**The operable resolution — there is no bypass.** Finish the parent (`gopi task
update <parent> --status completed`), or cancel the child (`gopi task update
<child> --status cancelled`). That is the whole set of moves: there is no
override flag, no cascade cancel, no timeout/staleness release, and no
`--force`. A `cancelled` or `failed` parent parks its children **forever by
design** — the parked row stays visible and legible (`[queue-skip]` + the
parent id + the blocking status), and the operator's answer is the child's
cancellation, not a weakened gate. `gopi worker <explicit task-id>` still runs
a parked row: the gate governs the DISPATCH funnel, the explicit id is the
operator's own door, and an operator who typed the row id has taken the
decision the gate exists to refuse to a queue. (`gopi task run` was RETIRED —
epic worker-run-entry; the explicit-id verb belongs to the worker, and the
gate's own A/B proof exercises exactly that: the funnel drains while the parked
row sits untouched, then `gopi worker <parked id>` runs it.)

**Honest partial coverage.** The gate is enforced at the claim funnel and the
pending view (one predicate, two readers). It does not — and by design does not
— stop `gopi worker <task-id>`, `gopi task resume`, or a row driven by the sync
layer on a peer ledger: those are explicit, operator- or replication-owned
paths, not dispatcher claims. A child whose parent is released only LATER is
re-examined at the next claim (nothing is remembered or cached between claims),
so a released chain drains in the ordinary queue order.

## Rule 34: The Sandbox Is ONE Configured Shape — Never an Invented Directory

> **STALE IN ONE ROW — the ledger (corrected 2026-09-26; `GOPI.md` is authoritative).**
> This archive is Tier B: the table below still lists `.gopi/memory.db` as a
> **symlink** to the shared read-only sample. That shape was declared the DEFECT by
> commit `40035f03` ("the ledger is a real writable private file, not a link to the
> read-only sample") after being measured: the write was refused
> (`attempt to write a readonly database`) or a writer **replaced the symlink node**
> by renaming a sibling into place, so the sandbox silently read a different file than
> it pointed at. **The ledger is a REAL, private, `0600`, WRITABLE file** at
> `<sandbox>/.gopi/memory.db`; the sample is used as a **seed, by VALUE (COPY)**
> (`GOPI_SANDBOX_LEDGER_SEED`, `internal/sandbox/ledger.go`), never linked. Every
> other row of this table is correct, and the settings row is still a symlink —
> read-only by contract.

**Get the sandbox by ASKING for it.**

```bash
SB=$(gopi sandbox create --label build --task-id "$TASK_ID" | tail -1)   # the ONE initializer
gopi sandbox show                                                       # where sandboxes live, and which channel decided
GOPI_HOME="$SB" <your command>                                          # a private profile, the standard shape
GOPI_HOME="$SB" gopi log --last                                         # prove it: the sandbox's OWN sessions
```

**The shape is fixed, and every sandbox is private.** The PROFILE ROOT inside a
sandbox is `<sandbox>/.gopi` — `GOPI_HOME` is a HOME, so every reader resolves
`<GOPI_HOME>/.gopi/...` (`config.GopiDir`), and the initializer materialises
exactly that path (`config.ProfileDirIn` / `config.ProfilePathIn`, the ONE join
both halves reference). One sandbox directory therefore holds:

| entry | what it is |
|---|---|
| `.gopi/settings.json` | **symlink** to the REAL settings file — **read-only by contract** |
| `.gopi/memory.db` | **symlink** to the **shared read-only sample** (`internal/testutil`'s generator — `testutil.SampleDBPath`) |
| `.gopi/sessions/`, `.gopi/raw/`, `.gopi/logs/` | **private, empty, writable** — this run's own output |
| `.sandbox-owner.json` | task id + pid + kernel start ticks + created-at — the marker that makes reclaim automatic |

The profile level is not decoration: materialising `sessions/` BESIDE the sandbox
(the pre-2026-09-26 shape) produced a directory the docs described and no reader
resolved — `GOPI_HOME="$SB" gopi log --last` answered `open $SB/.gopi/sessions:
no such file or directory` while `ls $SB/sessions` showed a directory sitting
there. The check for the shape is therefore the reader's own invocation:
`GOPI_HOME="$SB" gopi log --last` must list the sandbox's sessions.

**NEVER invent a sandbox.** The named anti-pattern: a per-script or per-run
directory built by hand — `SB=$(mktemp -d)`, `SB=/home/user/workspace/sbx-$$`, an
`os.MkdirTemp` inside a test body — with `GOPI_HOME` exported into it and the
contents written ad hoc. Every one of those is a different shape, none is
reclaimed, and each is a second, unreviewable answer to "what does an isolated
profile contain?" (Measured census, 2026-09-23: 27 `scripts/*.sh` set
`GOPI_HOME` to a directory they made themselves; see
`devdoc/agent-sandbox-unify.md`.)

**The location is CONFIGURATION — never a hard-coded path.** It resolves exactly
like `tmpdir`, through ONE function (`config.ResolveSandboxRoot`,
`internal/config/sandbox.go:188`):

```
--sandbox-dir PATH  >  $GOPI_SANDBOX_DIR  >  settings.json "sandbox_dir"  >  <home>/workspace/gopi-sandbox
```

There is deliberately **no ambient channel**: nothing outside gopi reads a
sandbox root, so a second shell-level variable would be a knob with no reader.
Ask for the resolved value with `gopi sandbox show` (`CONFIG.md` carries the
same row); never spell a path in a script.

**Only the config file is a symlink, and it is read-only by contract.**
`memory.db` is a symlink to the **shared** sample, never a copy and never a
second generator — a sandbox that needs rows the sample lacks uses the WRITER
scaffold (`testutil.NewTestDBPath` + the tools layer, Rule 32), it does not grow
its own database. Why the settings link must never be write-opened: POSIX
`rename(2)` **replaces the symlink node** rather than writing through it, so a
"saves settings" code path inside a sandbox silently swaps the link for a
regular file — the measured 2026-08-28 `.flash_settings.json` → `{}` incident.
A sandbox configured by its OWN contents is refused by the initializer for the
same reason: that shape silently tests nothing.

**Reclaim is AUTOMATIC — do not write a cleanup hook.** `t.Cleanup` does not run
on SIGTERM/SIGKILL, which is exactly when an abandoned sandbox appears, so the
reclaimer rides machinery that already ticks and needs nobody to remember:

- `gopi scratch-gc sandboxes` — the host-level one-shot (the existing watchdog
  executes the same verb), and
- the **existing** resident worker/merger tick (`scratchGCTick`), which sweeps
  the same tree on its own cadence.

Both apply ONE predicate (`internal/scratch/sandbox_gc.go`, `SweepSandboxes`):
a sandbox is collected only when it is **ours** (name marker `sandbox-`) AND
**aged** (72h, `scratch.SandboxStaleAge`) AND **unowned** — no live incarnation
of the recorded (pid, start ticks) pair. A live process standing inside it
(`/proc/<pid>/cwd`) vetoes the delete too, and **every unknown is a KEEP**: an
unreadable `/proc`, an unparseable marker, a missing marker, a probe that
errored. The asymmetry is the point — a sandbox that survives one more sweep
costs disk; a sandbox deleted out from under its owner destroys a run's private
state. The documented root is refused by name (the filesystem root, the user's
home, the gopi profile), a foreign directory name is never a candidate, and the
**root itself is never removed at any age** — only its aged, unowned children.

**The check.**

```bash
go test ./internal/sandbox/ ./internal/scratch/ ./cmd/agent/ -run 'Sandbox|Sweep|ScratchGCTick'   # shape, precedence, marker, the three reclaim directions
gopi scratch-gc sandboxes --dry-run    # what WOULD go, with the reason per sandbox
```

The regression guard pins all three directions the epic names: a SIGKILLed
owner's sandbox is collected on the next sweep, a live holder's survives, and a
repeat run changes nothing (`internal/scratch/sandbox_gc_test.go`,
`cmd/agent/scratch_gc_tick_sandbox_test.go`).

## Rule 39 — "git 存代码不存 binary": the reasoning

### The measured state, and why a rule was needed at all

At the gate's landing (master `83dac97c`) the tracked tree carried **7,318
paths / 284.6 MB of blob content**, and the scan found:

| class | paths | what they are |
|---|---|---|
| `oversize` (> 256 KiB) | 198 | 172 under `docs/` (demo screenshots, replay films), 14 `devdoc/` (evidence PNGs, a 2.1 MB transcribed log), 4 committed `sandbox/` trees, plus a 19 MB ELF, a 2.8 MB static `links` binary, and **two genuinely tracked 300 KB Go source files** |
| `magic` | 753 | 727 PNG, 18 JPEG, 8 GIF — the same demo/evidence population, plus the ELF, the static binary and four sqlite ledgers under `sandbox/` |
| `nul` | 56 | `.gz` captures, `.wav`/`.pcm` voice samples, `.raw` analyser output |

1007 `(path, class)` permits; the union is 822 distinct paths.

The interesting number is the **2 tracked 300 KB `.go` files**. They are the
reason the gate could not simply be a size threshold: `cmd/agent/cli_tasks.go`
(319 KB) and `internal/agent/loop.go` (378 KB) are legitimate source, so a rule
"nothing over 256 KiB" would have been wrong on the day it landed, and a rule
with a size threshold *plus* a hand-waved exception is not a rule. The resolution
is the one every gate in `internal/standards/` uses: a **frozen permit with a
reviewed reason** — here, `oversize SOURCE (.go): legitimately tracked — the size
threshold targets non-source blobs, not source files`. The permit says out loud
what a threshold cannot, and it is visible in a file a reviewer reads.

### Why the scan is `git ls-files` and not a worktree walk

`scratch_gate.go` made this argument first and it is the same one here: the
defect is "this is TRACKED". A worktree walk would have reported the 135 MB of
untracked scratch residue the 2026-09-26 incident left behind
(`devdoc/scratch-repo-residue-01a0db54.md`) — real litter, but Rule 30's
litter, with a different remedy (it is not in any ref, so it cannot be removed
from history and it never ships). Two rules, two scars: **Rule 30 owns the
worktree, Rule 39 owns the index.** Merging them would have produced one gate
that reports the other rule's defect and hides this one behind it.

### Why a permit is keyed on (path, class), not on path

This was found by writing the test, not by thinking about it: the first version
keyed the table on the path alone, and the real-tree test failed on the **second**
row for `devdoc/assets/bm-pricing-20260830.png` (829 KB *and* a PNG signature —
one file, two classes). A path-keyed table can hold one class per path, so the
other one is re-reported as NEW on every run: a permanent red that trains a
reviewer to ignore the gate. Keying on the pair fixes it and buys the sharper
ratchet for free — a path frozen as `oversize` that later carries an ELF magic
has not been cleaned, it has **traded** one violation for another, and the
verdict says so in its own bucket rather than accepting the path because some
row for it exists.

### The three ways to fool a gate like this, and the proof against each

1. **A predicate that stopped matching reads green forever.** The red proof
   builds a REAL throwaway repository (`git init` → write → `git add -A`) whose
   index carries one blob per class — a 256 KiB+1 text file, a real PNG
   signature, an ELF, a 1 MB+1 font, a NUL-carrying `.gz` — and requires the
   scan to report exactly the six `(path, class)` pairs it put there. The same
   fixture then serves as the complement: `src/main.go` and `docs/notes.txt`
   must NOT appear in the report, so a predicate loosened to "report everything"
   fails too.
2. **A permit table that silently shrank.** `loadBaseline` refuses a missing
   file (an absent table would read every permit as a new violation), an
   empty table, an unknown class, an empty reason, a short row and a duplicate
   `(path, class)`, each naming the line — and the real-tree test asserts a floor
   on the path count plus a RATIO floor on the permits (at least half the
   violations the same run's scan reports), so "the scan returned nothing"
   cannot pass as "the tree is clean" (Rule 15). The permit floor was an absolute
   row count (1007 → `500`) until 2026-10-03, when three legitimate clean-ups
   (`1c868ff6` −325, `49a56355` −181, `981054bc` −72) walked the table to 429 and
   reddened the test: the floor and the ratchet measure the same axis in opposite
   directions, so an absolute floor can only tighten as the ratchet works. The
   ratio is not a weakening — it catches a truncated or hand-trimmed table at any
   scale, which the absolute `500` could not do at all below 1000 hits
   (`baselineFloorVerdict`, pinned by `TestRepoHygienePermitFloorIsNotScaleBound`).
3. **A gate that is right on the fixture and wrong on the tree.** The real-tree
   test runs the SHIPPED table against the SHIPPED index and requires
   `baselined == hits`, printing `7318 tracked paths, 1007 violations, all
   baselined by 1007 permits`. That number is the gate's own instrument: if a
   later change loosens a limit, the counts drift and the test says so.

### The census is re-runnable, not quoted (Rule 38)

The table carries its generator's invocation in its own header, and
`internal/standards/testdata/repo-hygiene-census.go` re-derives it byte for byte
(`--check`, exit 1 on any delta) — a test runs exactly that and requires exit 0.
The census is a `main` package under `testdata/`, where the Go tool ignores it:
nothing in the build compiles it and nothing imports it, so it is documentation
that EXECUTES rather than a second implementation to keep in sync. The one thing
that CAN drift is the pair of threshold constants, which are duplicated by
necessity (a `main` package cannot be imported by the gate); a threshold moved
in one place and not the other shows up immediately as a `--check` delta,
because the class of every affected blob changes.

### The boundary with its neighbours, stated so a later reader does not merge them

- **Rule 22** (temp roots off the `/` partition) is about WHERE a process writes;
  Rule 39 is about what a commit carries. A blob can be born on the correct
  partition and still be the wrong thing to track.
- **Rule 30** (a run leaves its worktree clean) is about the worktree; Rule 39 is
  about the index (above).
- **Rule 34** (one configured sandbox shape) is why the committed `sandbox/`
  trees are a defect twice over: they are the four rows whose permit reason names
  Rule 34 — an execution environment, never a tracked artefact.
- **Rule 35** (zero dependency) forbids the tempting fix: a pre-commit hook, a
  scheduled sweep or a helper script that enforces hygiene outside the suite.
  The gate runs in `go test ./internal/standards/`, which every change already
  passes through; adding a second mechanism would be the dependency this rule
  refuses.

### The one honest limit

The freeze is a **freeze**, not a fix: the gate is green on this tree because
1007 permits say "known, reviewed, and someone owns the removal". The permits
name epic `01a0fd2e` scope 3 as that owner, and the ratchet is what keeps the
number from growing while the epic works — but a reader must not mistake green
for "the repo no longer stores binaries". It does; the gate makes the next one
impossible and the existing ones countable.

## Rule 40 — hermes-agent 属于 HP：the reasoning

> Tier A is PROJECT.md Rule 40 (behaviour + the machine-check verdict). This is the
> evidence it was distilled from: what actually happened, why the obvious check
> cannot decide it, and why the instrument chosen is a warning.

### The misroute, as measured (2026-10-02/03)

| when (local) | what | evidence |
|---|---|---|
| 2026-10-02 20:51:39Z (2026-10-03 04:51 local) | row `01a0fe63` (`…-0c2c-74c0-86d2-e1b1997389f9`) submitted on **peerbox's** ledger with `deliverable={"repo":"/home/user/work/hermes-agent"}` | peerbox `tasks` row; the repo check had PASSED (the path is a real work tree on peerbox: non-bare, branch `hot-note`) |
| until 04:56 | the row sits **unclaimed** — peerbox is the ledger holder (`role=server`) and `sync_peers` is empty, so nothing forwards or claims it | peerbox settings.json (`role=server`, `sync_peers: []`) |
| 2026-10-03 04:56:16 | `01a0fe63` set to **cancelled**, reason `MIGRATED to HP: 01a0fe67-34cd-7c77-8293-d8992869f928` | peerbox row's reason column |
| 2026-10-03 04:56:11 | the row is re-filed on **HP** as `01a0fe67` (`…-34cd-7c77-8293-d8992869f928`) | HP ledger row, `created` stamp |
| 2026-10-03 07:26:02 | HP's worker closes `01a0fe67` — `status=completed`, `deliverable.branch=task/01a0fe6734cd7c778293d8992869f928`, `deliverable.worktree=/home/user/workspace/gopi-01a0fe6734cd7c778293d8992869f928` | HP ledger row |

The migration itself was the three steps Rule 40 now names: export the payload
(the peerbox session container `/home/user/.gopi/sessions/01a0fe63-…/metadata.json` holds
the 6214-character payload verbatim — `gopi task show` is the export, there is
no export/import subcommand), re-submit on HP, cancel the original naming the new
id. The cancelled row is kept (the ledger is append-only) and its reason is the
trail.

### The root cause, in five whys

1. **Symptom**: a hermes-agent task never ran.
2. **Why**: it was filed on peerbox's ledger, and no HP worker reads that ledger;
   peerbox's `sync_peers` is empty, so nothing forwards it either. A row on the
   wrong ledger is not late — **it is never dispatched**.
3. **Why it looked correct**: `task_submit` verified the repo
   (`taskRepoVerifiable` → `git rev-parse --is-inside-work-tree`) and it
   **passed**.
4. **Why that check cannot help here**: `/home/user/work/hermes-agent` is a
   real, non-bare work tree on **both** hosts — on HP it is the project, on
   peerbox it is the git upstream. The verification answers "is this path a git
   work tree on *some* filesystem this process can see", which is TRUE.
5. **Why that is a property of the check, not a bug in it**: ownership is a fact
   about a **machine**; the seam holds a fact about a **path**. No amount of
   path comparison separates the two, because the paths are the same string.
   **Nor is the hostname a sound gate**: a renamed host, a container, a peer
   shell, a forwarded submit — each would make a refusal fire on the legitimate
   row. (`IsServerNode` in `memory_sync_delta.go` shows the same shape: its
   hostname half is explicitly fail-safe to "not server" precisely because a
   hostname is weak evidence.)

### Why a warning and not a refusal

The seam is the funnel **every** submit face passes through, including the
internal auto-submit paths (reports, merge-backlog, adoption) that cannot
answer a question about intent. A refusal there would let a wrong hostname
block legitimate work; a warning costs one line and lands **before** dispatch,
in the same confirmation the submitter reads (the shape
`task_attachment_claim.go` established for "the prose claims what the row does
not carry": report it, never block it). The row is still dispatchable and still
readable either way — the operator's remedy is the three-step migration, not a
re-think.

### Why the table has one entry

Exactly one such collision has been measured (above). A general "wrong host for
this repository" model built from a single incident would be a guess wearing a
rule's clothes — and it would need a per-project ownership table nobody has
authority to fill in from the outside. One measured entry, one hostname
constant, and the honest statement that **this rule has no gate** (Rule 40 Tier
A says so in those words) is the size the evidence supports. When a second
collision is measured, that is when the table earns its second row.

---

## Rule 41 — 发现异常即取证、即提 bug：the reasoning

> Tier A is PROJECT.md Rule 41 (the threshold, the three requirements, and the honest
> machine-check verdict). This is what it was distilled from: the episode that
> supplied its central example, why the threshold is deliberately low, and what
> the submit seam can and cannot observe.

### The episode the third requirement is drawn from (2026-10-11)

Task `01a12b01-1c35-71cf-a554-b578dad6c6a5` filed a bug row about stale-sweep's
treatment of zombie `in_progress` rows. Its argument rested on **two causal
claims**, and neither rested on an observation of the thing it asserted:

| claim | what it rested on | what the measurement said |
|---|---|---|
| "the stale-sweep re-arm bumps `updated_at` and keeps a zombie alive" | a **comment** in `internal/tools/tasks.go` describing the column as write-churn | the notify sink `~/.gopi/notify/task-events.jsonl` carries **21 `stale` events per row**, and each event **snapshots the `updated_at` from before the bump**, a constant 1800 s away from the event instant (21/21 at 1800 or 1801 s) — the bump precedes the notification by one stall window, so the sentence was being read backwards |
| "it was claimed and died 2 s later" | **one** observation of `last_toolcall_at - claimed_at` = +2 s | the same batch's 30 positive rows include **4 at +1 s** that terminated normally; +2 s is unremarkable in the distribution |

The correction task `01a12b06-1875-706b-8726-af98c19cb602` **retracted both
claims** and kept only the four facts that a single command reproduces. The
mechanism it used is the one Rule 41's third requirement generalises: every
sourced statement is labelled **实测** or **推断**, and the latter is explicitly
declared unusable as a criterion (`devdoc/task-01a12b01-brief-correction.md`
§3, tables S1–S14).

The cost shape is why the requirement is not tidiness. A wrong criterion does
not stay inside the row that carries it: it is **read as established** by the
next executor, who builds on it, and by the reviewer, who has no instrument to
tell an observation from a paraphrase. Retracting it cost a whole second task —
which is cheap compared to the alternative, where the correction never arrives
because nobody re-reads the premise.

### Why the threshold is deliberately low

The asymmetry is measurable, and it is the whole argument:

- **A redundant row** costs one agent's session. It is not waste in the sense
  that matters, because Rule 26 makes duplicate dispatch an expected outcome:
  each executor judges for itself whether the work is already done, so a row
  filed twice produces one dispatch that reads, decides and stops.
- **A suppressed observation** costs the defect's remaining lifetime, and it is
  **unrecoverable**: the evidence lived in one run's context, and the next
  reader has no path back to it. Nothing in the ledger records "someone saw
  this and did not file it."

So the gate that would be correct here — "is this really a bug, is it really
unreported?" — is exactly the gate a discoverer cannot pass from where they
stand, because answering it requires the fix-side and ledger-side view they do
not hold. Deciding that is the reviewer's, the PA's and the operator's work.
Rule 41 removes the question from the discoverer's path rather than answering
it for them: the "要不要提" pause **is** the failure mode.

### What the submit seam can observe, and what it cannot

- **Observable**: a `bug`-tagged row entering the closure. `--tags bug` selects
  the reserved bug-flow tag (`internal/tasktags.BugTag`), which drives the row's
  priority tier and the adoption tally `TaskBugAdoptionStats`
  (`internal/tools/tasks.go:8599`) prints as
  `[bug-flow] tasks with tag bug: N (verified V, reopened R, other O)`. A
  correction round is therefore visible in aggregate — the retraction above is
  itself a row a reader can find.
- **Not observable**: whether an anomaly the run saw **should** have produced a
  row. That is a fact about one run's context, and the repo keeps no trace of
  the anomalies that were noticed and dropped. What exists is the rule's prose
  and the tally's aggregate drift; a gate asserting "you should have filed"
  would be guessing, and its false positives would land on runs whose
  observation was checked and genuinely explained. Rule 41 states this in those
  words rather than shipping a check that cannot decide.
- **Not a substitute for observation**: the tally counts rows, never evidence.
  A row whose payload names no command and no output counts exactly like one
  that carries both — which is why the first requirement (evidence into the
  session directory) is prose and not a number.
