<!-- doc-class: normative -->
# The chat surface's operating duty — which work the console does itself

**Class**: normative (implementation + rule)
**Landed**: 2026-10-07 · task `01a11343-0c04-7ce0-be86-2e2e831005c6` (peerbox)
**Widened**: 2026-10-11 · task `01a12afc-0030-7749-9402-4fec805fa8f0` (hosta) — the
scope clause, see "The scope, widened" below.
**Files**: `internal/agent/chat_pa_duty.go`, `internal/agent/chat_context.go`,
`internal/agent/chat_pa_duty_test.go`

## What this closes

The operator's console — the PA — was routing its OWN housekeeping through the
task queue: a check, a diagnosis, a `git tag`, releasing a dead worktree, all
promoted to a worker row. The operator's ruling (2026-10-07):

> 这一类工作是简单的维护工作就是举手之劳，应该是 **PA 的工作**，不用启用一个
> **昂贵的 worker**……pa 只是**不允许实际编码 debug**，其他**运维的工作都是可以
> 代劳人类用户的**。

The ruling is a ROUTING decision, and nothing in the prompt stated it. So the
console knew how to file a row and not that it was allowed — and expected — to
simply DO the cheap thing.

## The paragraph: `chatPADutyRules(toolsOn)` in `internal/agent/chat_pa_duty.go`

Marker `chatPADutyMarker` (`[CHAT PA DUTY — the operator's work is YOURS, not a
task; coding is the exception]` — it named "the cheap maintenance work" until the
2026-10-11 widening, which was the narrow reading), then five clauses:

1. **YOU DO IT YOURSELF** — and since 2026-10-11 the clause opens with the SCOPE
   rather than with a chore list: *everything the operator would otherwise do by
   hand EXCEPT coding and debugging*. The chores below it are now explicitly
   *"examples of the scope and not its boundary"* — RESEARCH (the industry's
   practice and this repository's, two options compared and one chosen), shell
   and git HOUSEWORK (the READ-ONLY git forms are named as the write-boundary
   paragraph's, which OWNS that list — this paragraph does not re-list them),
   a SHORT JOB the operator would otherwise run by hand, checks and diagnosis
   (read a failure, reproduce on the smallest sample, bisect, measure the real
   output, count what actually is), ledger and configuration edits, artifacts and
   evidence files, and cross-host work through the peer flag. The paragraph also
   states the routing behaviour outright: *when the request is one the operator
   would have carried out themselves, CARRY IT OUT*.
   The COST is stated in the paragraph, not only in a comment: promoting a
   hand's-breadth job buys **one worktree + one model run + one merge cycle**, and
   it HIDES the action from the operator — when the row fails, the maintenance
   did not happen while the queue reads clean (measured: a run wrote an empty
   skeleton and the row read `completed`).
2. **A change to the REPOSITORY is YOURS TOO** — editing source, a rule, a spec,
   a gate, running the project's gate over your own change, and staging/committing
   it: `tools.ChatOpRepoWriteRefusalSentence()` admits the class at every posture
   (the operator's **2026-10-24 ruling**, which reversed the pre-ruling hard no-go —
   a console that cannot commit cannot finish anything, and on the night of the
   ruling the dispatched workers were the thing breaking the machine). What
   survives is the commit path's **DISCIPLINE**, which is a practice and not a
   permission: `git add` ONE file at a time (never `-A` or `.`), `git
   status --porcelain` empty before reporting done, no ref move over work this run
   does not own. The **TASK route** (`gopi task submit "<the work>"`) survives for
   **OWNERSHIP**: a change that wants a branch of its own, a deterministic
   worktree, a review and a merge is filed there, or the operator asked for a row.
   The test is still the **CATEGORY OF THE ACTION**, never its difficulty and never
   its size.
2b. **The ONE act the scope in (1) still retires** — and the only one — is the
   **REF MOVE**: a branch/reset/push over a ref this run does not own. Not the
   `commit` (it is admitted, (2)), and not the edit. The authority runs the whole
   width of what the operator would do by hand and **stops at a ref**, because a
   ref decides what OTHER work is reachable and this console owns none of that. The
   clause says in its own words that this refusal is the write boundary's, *"not a
   second judgement to make here"*, and names the correct move: prepare the exact
   command, state its consequence and hand it to the operator (§3).
3. **OPERATOR-ONLY** — rewriting an already-published ref, deleting a branch or a
   tag whose work is not proven covered, and any force spelling that steps over a
   refusal. State the exact command and its consequence, then ASK.
4. **"the PA may not do the actual coding debug" ≠ "fix nothing"** — the console
   may READ and may MEASURE, and it files the finding with its reproduction as
   one line. What the ruling still says is that the **fix keeps a task's OWNER**
   (a branch, a rollback point, a review): writing the change, running the gate
   over it and committing it are all admitted (2), and the repair is filed there
   rather than improvised here. That is a question of **WHO owns the change**,
   never of permission.
5. **VERIFY your own housekeeping by its RESULT, never by the command** that was
   supposed to produce it; a refusal is a VERDICT to read, not an obstacle to
   route around; read a usage page to its END before concluding "there is no such
   thing"; the peer flag is a cross-host tool, not a reason to improvise a
   transport.

## The scope, widened (2026-10-11)

The first landing named MAINTENANCE work as the console's, and the model read
exactly that width. The operator's complaint (2026-10-11):

> current gopi chat is restricted with too many rules. it should function like PA
> which can work on behalf of user. i.e. user doesn't have to do all
> shell/scripting/devops. therefore all research/operation except of
> coding/debugging should be duty of gopi chat who is acting as a role of office
> work which means communicating/acting with authorization of human user. and it
> should have full authority to run short job which includes everything except of
> tasks which involves programming/debugging.

Three edits, all in the same paragraph, none of them a new mechanism:

1. **Clause (1) is a SCOPE, not a chore list.** The ruling is quoted as its own
   constant, `chatPADutyScope` — *"everything the operator would otherwise do by
   hand EXCEPT coding and debugging"* — and the paragraph says WHY it is stated
   that way: *a list is what a reader completes with "…and not the rest"*. The
   chores that used to BE the clause (research, shell/git housework, a short job,
   checks, diagnosis, ledger/config edits, artifacts, cross-host work) are kept,
   explicitly re-labelled **"examples of the scope and not its boundary"**. The
   routing behaviour the operator asked for is stated as one sentence: *"when the
   request is one the operator would have carried out themselves, CARRY IT OUT"* —
   so "should I?" is what (1b)'s bands answer, never the default reply.
2. **The exclusion gets its own clause, (2b).** A wide scope with an unreadable
   bound is the defect in the other direction, so the one act the scope retires is
   named: **the ref move** (originally **the commit**; see the reversal note at the
   end of this section), and the reason is the boundary's own — a ref decides what
   other work is reachable, and the console owns none of that. The
   asymmetry is deliberate and is what makes the wide statement safe: **reading the
   operator's machine is recoverable; so is writing a file (git holds the history);
   rewriting a ref is not.**
3. **The exclusion is bounded by CODING, not by SUBJECT MATTER** (clause (4)):
   research, operation and a short job *around* a defect are still the console's,
   so "read this function / run this command / what changed here" is ordinary work
   and only *producing the change* is the task.

The marker changed with the scope — `[CHAT PA DUTY — the operator's work is
YOURS, not a task; coding is the exception]` — because the old one
(`…the cheap maintenance work is YOURS…`) named the narrow reading.

### What did NOT move (the constraint the task carried)

The write boundary is untouched, top to bottom: no new permission, no new band,
no new tool, no new knob. Rule (2b) is the boundary's own rule named as the bound
of the scope, and the boundary's dispatch-point enforcement
(`internal/tools/chat_write_guard.go`) is unmodified — which is the point: the
widening is **which work is the console's**, never **what it may write where**.
The new regression half of `TestChatPADutyQuickOpsKeepsTheOtherTwoClasses`
asserts both directions: the scope sentence is present, AND a call past the
ask-only threshold with nothing declared is still refused by the guard the
paragraph shares its numbers with.

> **2026-10-24 — the repository half of the asymmetry was REVERSED, and (2)/(2b)
> had to follow.** `tools.ChatOpAdmit` began admitting `ChatOpClassRepoWrite` at
> every posture (measured: `git commit -m x` → `class=REPOSITORY WRITE
> allowed=true`), while this paragraph still taught *"a repository change is a
> task (2) and never a permission"*, *"ONLY a change to the REPOSITORY is a
> task"* and *"(2b) the ONE act … is the commit"* — the prompt contradicting
> itself and the guard, one paragraph apart. The three clauses were rewritten:
> (1b)'s tail renders `tools.ChatOpRepoWriteRefusalSentence()` (the guard's own
> sentence, same origin) and keeps the ask for the LONG class alone; (2) states
> the repository write as the console's, keeping the commit path's DISCIPLINE and
> routing to a task for **OWNERSHIP** rather than permission; (2b) retires the
> **REF MOVE**. Clause (1b)'s ref-move sentence and `chat_write_boundary.go` were
> NOT touched — `01a16bb6` owns that class.
>
> **A boundary that is still open, and is NOT this paragraph's:** the *fence*
> layer (`internal/tools`' built-in ```chat-policy``` table, where `*` is
> `prompt`) still refuses the same command that `ChatOpAdmit` admits, and
> `git branch -f …` classifies as OPERATOR WORK. Filed as `01a16bfd`.



| path | wire | limb |
|---|---|---|
| plain (`gopi chat`) | **0 tools** | `chatPADutyNoToolsLimb` — you cannot run the check or file the row: name it, hand the operator the command, and NEVER claim one was run |
| `--tools` (`gopi chat --tools`) | 18 tools | `chatPADutyToolsLimb` — bash/read/task_submit are on this wire: take the OP action in THIS turn, read the result back, do not file a row for it |

Both limbs are vars, not consts, because they compose rendered values (tool
names, the route) rather than typing them — a Go const cannot call a function.

## Where it lands, and why THERE

`chatContextRulesFor(toolsOn)` (`internal/agent/chat_context.go`) appends it
immediately AFTER `chatWriteBoundaryRules`, so the chat block ends with its two
ORDER paragraphs: **what this surface may NOT do** (8), then **what it MUST do
itself** (9). The numbered index comment above that function was updated in the
same edit (it is the slot registry a sibling block-editor reads).

It is deliberately **NOT** in `promptSections`: that registry is read by every
background task run, and "the cheap work is yours" is the opposite of a worker's
contract, whose deliverable IS the committed change. Asserted both directions.

### The rejected alternative — putting the duty in the write-boundary paragraph

Rejected: that paragraph's job is a **boundary** (what may not be written, and
where the artifact goes). The duty is its mirror image — an **obligation** (what
must be done here instead of filed). Folding them would make one paragraph carry
a prohibition and an obligation whose enforcement halves differ (the boundary HAS
a mechanism at the dispatch point; the duty has none), and the boundary's limb
pair would have to grow a second axis. Two files, one order, read together.

## One spelling — Rule 1/13/14

Nothing in `chat_pa_duty.go` is typed:

- the route `gopi task submit` comes from **`chatTaskSubmitRoute()`** — the SAME
  helper `chatWriteBoundaryRules` renders, so the two paragraphs cannot teach two
  different commands for one decision (`TestChatPADutyAgreesWithTheWriteBoundaryOnWhatIsATask`);
- tool names from `internal/toolnames`; CLI words from `internal/cliverbs`; the
  sync verb from `internal/taskverbs`;
- the peer flag is `tools.NotifyPeerFlag` — the ONE **importable** leaf spelling.
  (`cmd/agent` declares `syncPeerFlag`/`memorySyncPeerFlag` for the same word; a
  package main constant is unreachable from `internal/agent`, and typing the
  literal is what Rule 1b bans — so the imported leaf is the correct reference,
  and the note here records why the prompt's flag and the sync faces' flag are
  spelled in three places that must agree.)
- the repository-write **admission sentence** is `tools.ChatOpRepoWriteRefusalSentence()`
  — the SAME string the guard renders when it reports the class, so the prompt and the
  guard cannot teach two different permissions for one class (asserted by
  `TestChatOpCategoryThreePathsAgree`'s PATH 2, which reads both ends);
- the forbidden verb is `tools.ChatPartlyAllowedGitVerbs[1]` (`commit`) — read
  from the list the write boundary's **enforcement** reads;

  > **2026-10-17 (epic `01a1510e` 第 1–5 项) — 这条已经变了，原文保留作为来路。**
  > 写边界的执行臂不再持有任何动词表：`ChatPartlyAllowedGitVerbs` 随动词判定一起删除，
  > `chatCommitWord()` 现在返回 `chatGitCommitVerbWord` —— 一个**散文词**，在
  > `chatWriteBoundaryRules` 的规则 (2) 里与这段同拼写。权威陈述见 `devdoc/chat-policy.md`：
  > 能跑什么由操作员的 ```chat-policy 围栏 + 启动 posture 决定，`chat_pa_duty.go` 的
  > 义务段只**教形状**，不参与判定（`TestChatPADutyAgreesWithTheWriteBoundaryOnWhatIsATask`
  > 仍然成立，因为路由串还是同一个 `chatTaskSubmitRoute()`）。
- the force vocabulary is **composed at runtime** from `internal/branchform`
  (`chatForceSpellings()`). This is deliberate: a source literal carrying a force
  flag beside a branch verb is exactly the shape the
  `branch-force` standards gate refuses in a shipped instruction, and a prompt
  paragraph IS a shipped instruction. The reader sees `-B/-C/-f/--force`; the
  source contains no spelling of them at all.

**What the file deliberately does NOT do**: re-render the banned git verb list.
That sentence belongs to `chatWriteBoundaryRules` — two renderings of one
boundary is how they drift. `dutyNamesNoBannedVerbList` asserts the duty
paragraph carries none of them.

## Rule 3 / 35 compliance

No new tool, no new script, no resident process, no state file, no configuration
key and **no escape-hatch flag**. The paragraph is composed at assembly time from
constants that already exist. There is no enforcement half because there is
nothing to enforce at a dispatch point: the console can already run every OP
action through the tools it has — the defect was that it did not know it was
required to.

## Verification pointer

The recipe used here (and re-runnable):

```bash
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
SB=$(bin/gopi tool sandbox create --label paduty --task-id <id> | tail -1)
# local SSE stub writes the request body, then answers — zero API spend
GOPI_HOME="$SB" ANTHROPIC_BASE_URL=http://127.0.0.1:<port> ANTHROPIC_AUTH_TOKEN=stub \
    ./bin/gopi chat --once [--tools] -p 'ping'
# the body's `system` is a LIST of blocks:
#   "\n".join(b['text'] for b in req['system'])
```

Measured on the emitted bytes: the plain path carries the marker, the
`NoTools` limb and **zero** tools; `--tools` carries the marker, the `Tools`
limb and 18 tools; the write-boundary marker is present on both and precedes the
duty paragraph on both.
