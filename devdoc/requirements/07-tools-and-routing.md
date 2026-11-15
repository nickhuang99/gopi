<!-- doc-class: normative -->
# Requirements (reverse analysis) — 07: tools and routing — the agent's hands, and how it is told to use them

**Series**: module `[7/11]` of the reverse requirement analysis. Method, `why` standard,
output shape and the operator's binding constraints come from
`briefings/brief-requirements-reverse-analysis.md` and the umbrella document
`devdoc/requirements/00-method-and-standards.md` (branch `feat/requirements-umbrella`).
Module brief: ledger row `01a0ab3e-652f-74e4-b9f8-0bbf79b0122e` (B7/12 of the sibling
series `01a0ab12…`, which names this deliverable's path `devdoc/requirements/07-tools-and-routing.md`).

> **Verification pass, 2026-09-18 (branch `docs/req-07-tools-routing-verify`, base `812f9a60`).**
> This document was re-verified rather than rewritten. **Citations:** all 135 code/doc
> `file:line` citations resolve (0 unresolved, 0 beyond the file's length); the 8
> `GOPI.md:298-309` Rule 11 citations were **rotted**
> (Rule 11 moved to `:318-329` when Rules 12–30 landed) and are re-anchored. **Numbers:**
> every quantitative claim was re-measured — the adoption table, the description payload, the
> dependency count, the vision/web concentration. **Three per-tool adoption verdicts were
> REVERSED** (`web_search` 1→540, `web_fetch` 3→428, `screen_capture` 0→12) because the
> previous pass measured a **subset** of the session corpus; §8.4 records what changed, how
> each number was taken, and the discrepancy that could not be closed. Quotations,
> requirements, gaps and structure were found sound and are unchanged except where noted
> inline as *"re-measured 2026-09-18"* / *"REVISED"* / *"REVERSED"*.
>
> **Second verification pass, 2026-09-18 (same branch, later the same day).** A re-measurement
> of the *description payload* under the original method shows that number was a **unit
> mistake, not a growth**: `19,418` is the same quoted-constant substitution measured in
> **UTF-8 bytes** whose figure in **runes** is `18,731`. §6-F8, §8.2 #10 and §8.3 #3 now state
> the method and the unit once; §8.5 records the reconciliation. Two findings are added —
> **F14** (the wire-name package now carries **eighteen** tool-name constants against a
> **seventeen**-tool menu, and no test ties `internal/toolnames` to either inventory, so the
> single source of truth can drift unseen) and a **date-resolution** strengthening of **F13**
> (the unknown-tool names are all *current*: `TASK_LIST_BEGIN` appears 19× between
> 2026-09-13 and 2026-09-18). Seven §7 anchors were re-pointed (`scripts_gate_test.go`
> `:564-567` → `:580-583`) and the bare test basenames were package-qualified where the
> basename is ambiguous across packages.

**Front matter — the boundary I chose**

- **Boundary**: *what the agent can DO to the world, and how it is persuaded to reach for
  the right capability.* The set of capabilities offered to the model, the words that
  describe each one, the routing text that says when each is the right choice, what the
  model actually chose when measured against its own session logs, and the gaps between
  the need and the surface.
- **In scope (evidence)**: `internal/toolnames/names.go` (the wire-name constants),
  `internal/agent/loop.go` `claudeToolDefs` (:849) and the routing/efficiency/engineering
  prompt blocks (:563 `toolRoutingRules` and its siblings), `internal/tools/` (the tool
  faces: read/write/edit/bash, `*_search`, `gitblame`, `websearch`, `vision`,
  `screencapture`, `text2img`, `contexttrim`, plus the memory and task tool faces),
  `internal/shot` (the hand-rolled X11 client behind `screen_capture`, and the headless
  CDP renderer behind `shot`), `internal/text2img`, `internal/doubaoasr`, `internal/proxy`,
  `plugins/websearch` (the deployed Python tree the
  web tool shells out to), the CLI mirrors that share the same wire words
  (`cmd/agent/cli_websearch.go`, `cli_text2img.go`, `cli_screencapture.go`, `cli_act.go`,
  `cli_shot.go`), and the session JSONL logs that record what the model actually called.
- **Out of scope**: the *loop* that executes a tool call and feeds the result back (module
  06); the memory organ behind the memory tool (module 05); the CLI's whole verb surface
  (module 08) — cited here only where a CLI verb is the *only* reachable path to a
  capability; the task ledger the task/task_submit tools write into (module 01); the
  worktree/scratch lifecycle those tools can damage (module 03) — cited only where a tool
  *touches* it.
- **Boundary why**: the user-visible responsibility is *"the agent can act on the world it
  was given, with the smallest possible surface, and it knows which action to pick."*
  That is not a directory. `internal/tools` is 53,901 non-test LOC in 291 files
  (umbrella §7.1 mismatch **M1**): it holds the 17 tool faces *and* the task ledger,
  dispatch, verification, the merger's git execution, worktree lifecycle and sync. The
  tool surface proper is a small subset, and the words that persuade the model live in a
  *different* package (`internal/agent`) from the implementations — which is why the
  boundary must be drawn by responsibility and not by path. The two mismatches that touch
  this document by name are restated in §6: **M1** (the package is not the module) and
  **M5** (gates are infrastructure, and the Rule 11 gate this module needs does not exist).
- **Overlap**: sibling task `01a0ab12-*` (module -3e7d for this boundary). `git ls-tree`
  over every branch found `devdoc/requirements/` content only on three branches
  (`feat/requirements-umbrella` → `00-method-and-standards.md`; `docs/requirements-task-ledger`
  → `01-task-ledger.md`; `docs/requirements-merge-branch-lifecycle` → its own document).
  No sibling document for B7 exists. Judged: nothing to complete, nothing to duplicate.

**The five constraints this document must fit in** (the operator's own, as recovered by
the umbrella, §5 there; quoted, not paraphrased):

- **C1 — one static binary, zero dependencies, no plugin architecture.**
  `devdoc/roadmap.md:64` ("CGO_ENABLED=0: 始终编译为静态二进制文件"), `:5` ("静态单二进制").
- **C2 — the operator must be able to control the whole thing.** Ledger `t-1788383728`.
- **C3 — a daemon must be simple.** Ledger `01a0a995-8742-77fc-a164-53054ea652c5`, verbatim.
- **C4 — the agent must clean up after itself.** Ledger `01a0975a-3e3f-7ae5-a848-a98cd24d6dc3`,
  verbatim: *"is gopi clean up after development? … these are serious questions to ask"*.
- **C5 — a small team with limited resources.** Ledger `01a08b9a-e5…`, `01a08b92-76…`.

The check against all five is §6, "the C1–C5 check".

---

## 1. What this is for

*(product-owner language. No code identifiers appear below except the seventeen tool
names the model itself emits — read, write, edit, bash, code_search, keyword_search,
web_search, web_fetch, git_blame, job, task, context_trim, task_submit, memory,
vision_analyze, text2img, screen_capture — because those names ARE the user-visible
vocabulary of this module.)*

An agent that can only talk is a consultant. This module is what turns it into a worker:
a **short, fixed menu of actions it may take on the machine and on the world outside it**,
plus the words that tell it when each action is the right one.

The need has four parts, and a user would state them separately:

1. **"It must be able to look at my files and change them, carefully."** Reading, writing
   and surgical editing are the floor — without them nothing else matters. The founding
   document states this as the product's first design principle: three file tools, each
   with an explicitly bounded purpose, one for safe browsing of large files, one for
   whole-file replacement, one for a unique-string replacement that refuses ambiguity.
2. **"It must be able to run things, search my code, and ask history questions."** Running
   builds and tests, finding where a thing is *defined* rather than every line that
   mentions it, finding every mention of a thing, and answering "who wrote this line and
   which change brought it in" — the working vocabulary of a programmer.
3. **"It must be able to reach outside my machine when the answer is not in my repo."**
   Searching the web, reading a page in full, understanding an image, rendering text into
   an image, capturing the screen, and — through the run harness — starting long work in
   the background and filing work it should not do itself.
4. **"It must not be given a hundred ways to do one thing."** The surface is capped on
   purpose, and the cap is a written rule with a machine check behind it.

The second half of this module is the half users never see and always feel: **routing**.
A capability the model does not *choose* is a capability the user does not have. The
project learned this the expensive way and wrote it down as a rule of its own: a tool's
value is that the model actually calls it, and the prompt is the only persuasion mechanism
there is. The measured history is blunt — one tool was called *zero* times across 21,435
recorded tool calls, another 0.3%, a third four times. Those tools existed, worked, and
were invisible to the model. So "the agent has a tool" and "the agent uses the tool" are
two different requirements, and this document treats them as two.

**Why the user should care about the difference.** Every capability on the menu is
described to the model in words that are re-sent on every single request — the seventeen
descriptions come to about 4,682 input tokens *before* the model has read anything about
the task (**re-measured 2026-09-18**: every `Description:` expression with each `toolnames.*`
constant substituted, **18,731 characters** / **19,418 UTF-8 bytes** — the byte figure is why
the payload costs more than a character count suggests; the previous pass's `4,683` tokens
and this pass's `4,854` are these same two counts ÷4 — see §8.3 #3 and §8.5). A capability
that is never chosen is a permanent tax with no return; and a
capability that is chosen only when reached through a shell command is one whose safety
gate the routing text never told the model about.

---

## 2. Who is served

**The agent itself is the primary user.** This is the one module in the series where that
is literally true: the tool descriptions and the routing text are the *interface document*
the model reads on every request, and the menu is the set of verbs available to it. Rule
11 makes the identification explicit — the LLM is the only user, and therefore the only
quality judge (`GOPI.md:320`, and the `memory` description's own "LLM 是唯一用户 = 唯一质量裁判",
`internal/agent/loop.go:1145`).

**The operator, second, and differently.** He reads the menu's *effects*: what the agent
touched, what it ran, what it spent. He is served by:
- the cap (a short list he can audit, `GOPI.md:87`);
- the shared wire words, so the thing he types as a command (`gopi web search`, `gopi web
  fetch`, `gopi text2img`, `gopi screen_capture`) is the *same name* the agent calls and
  the same name in the log filter (the mirror contract, `cmd/agent/cli_toolnames_contract_test.go:1-25`);
- and the CLI as a by-hand fallback when the agent is not in the loop at all.

**A task run, third.** A run is served by the tools that let it *end*: `task_submit` files
work it discovered but should not do, `task`(action=reframe/commit/review) is its own
working area, `context_trim` lets it drop a dead end without re-sending it forever, and
`job` lets it start something that outlives one shell call.

**Who is NOT served by this module, stated so the boundary is honest.** Nobody is served
by the *implementation* side of a tool — an operator does not care how a file is read. And
in the measured logs the **general-purpose user of the web tools and the vision tools is
thin**: `vision_analyze`'s 1,800 calls (re-measured 2026-09-18, full corpus — see §8.4)
concentrate in 34 of 1,867 tool-using sessions, with the mass on three days (927 on
2026-09-09, 464 on 2026-09-07, 231 on 2026-09-06 — the visual-studies workload), while
`web_search` (540) and `web_fetch` (428) are concentrated too (52% of all web calls land on
the two days 2026-09-10/11) — and `text2img` remains at zero (§6-F7, §6-F3).

---

## 3. The workflow it sits in

**3.1 One request, one menu, every round.** A run opens with a system prompt assembled
from ordered blocks (`internal/agent/loop.go:909` `assembleSystemPrompt`), and with the
seventeen tool definitions attached to the request. The model answers with text, thinking,
and one or more tool calls. Each call is dispatched by name, validated, executed, and its
result appended; the loop repeats (`devdoc/roadmap.md:54-55` diagrams exactly this, and
names the tool-call JSON shape at `:61`).

**3.2 The round-trip that defines "routing".** The model proposes a name; the harness
either has it or does not. A name the binary does not register fails *before any code
runs* — measured, and recorded verbatim in a devdoc: the model called `screen_capture`
three times with correct arguments and got `Unknown tool: screen_capture` because the
serving binary was built from master while the tool lived on an unmerged branch
(`devdoc/screen-capture-and-actuation-tools.md:853-875`). Routing and registration are
different failures with the same surface symptom.

**3.3 The two-surface workflow for anything the model is not told about.** When a
capability exists as a *command* rather than a *tool*, the model reaches it the only way
left — by spending a `bash` call. That is a deliberate design choice for actuation:
`gopi act` is a command, not a seventeenth tool, because Rule 3 caps the list and Rule 11
would have required an adoption experiment before claiming the tool worked
(`devdoc/screen-capture-and-actuation-tools.md:650-663`). The consequence is a real,
measurable workflow: **the model cannot act without also producing a visible shell call**,
which is the safety gate the devdoc names as the point of the choice (`:679-681`).

**3.4 The end-of-run workflow.** A run that discovered other work submits it
(`task_submit`), commits its conclusion into its working area (`task` action=commit),
reviews itself (`task` action=review), and stores a distilled lesson (`memory`
action=store). Rule 11 applies to these too, and the measured result of the family merge
is the strongest adoption evidence in this document: the merged names went from 575→11,629
(`task`) and 318→6,283 (`memory`) calls inside a week of the merge landing (§6-F2).

**3.5 The clean-up edge of the workflow.** `bash` is the tool most likely to cause damage,
so it is the tool that carries the blast-radius guard: a destructive shape whose resolved
target lies outside every declared worktree root is refused, and the refusal names the
target and tells the reader how to declare a root (`internal/tools/path_policy.go:210`,
pinned by `internal/tools/bash_path_policy_test.go:39,56`). The refused targets were
chosen against a real measurement, not a guess — the /dev/shm assessment
(`devdoc/shm-worktree-risk-assessment.md`, commit `f9c8625e`), §6-F1 and §6-F4.

---

<!--CHUNK2-->

## 4. Why it exists — the requirement per need, with evidence

Every requirement states **the need** (product-owner language) and **the test** (how the
user would personally verify it). Every claim below carries a ledger task id, a `path:line`,
a commit SHA, or a `GOPI.md` rule number; anything else is prefixed `[opinion]`. Inferences
carry `[inferred]` and name their weakest link.

### R1 — A tiny, fixed menu, and adding to it is a decision, not an edit

**Need.** When I hand the agent a job, it must have a short list of things it can do that I
can hold in my head and audit; expanding that list must be an explicit decision somebody
argued for, not something that arrives with a feature.

**Test.** Read one file — the same file the agent is told is the source of truth — and count
the entries. Then check that the written rule and the code agree, and that disagreeing
fails the build.

**Evidence.** The rule is `GOPI.md:85-90` (Rule 3: "Tools Are Minimal — Don't Add New Ones"),
whose inventory line is `GOPI.md:87` — "GoPi has 17 tools: read, write, edit, bash,
code_search, keyword_search, web_search, web_fetch, git_blame, job, task, context_trim,
task_submit, memory, vision_analyze, text2img, screen_capture." The source of truth is
`internal/agent/loop.go:1098` (`var claudeToolDefs = []deepseek.ClaudeToolDef{`), and the two
are held together by `internal/agent/gopidoc_sync_test.go:54`
(`TestGOPIRule3_MatchesClaudeToolDefs`), created because the doc had already drifted once:
its own comment (`internal/agent/gopidoc_sync_test.go:46-53`) records that `GOPI.md` claimed 18 tools while the code registered
23, so "the 'HARD CONSTRAINT' file asserted a false inventory and forbade tools the agent
already had." The wire names are constants, not literals — `internal/toolnames/names.go`
(commit `04d1bce0`), and `GOPI.md:87` is the one place the full list is spelled out for a
reader.

**Why it is a requirement and not a style preference.** Rule 11 says the model must actually
call a tool for the tool to count. A menu the model cannot hold is a menu it cannot choose
from: the whole persuasion apparatus (R7 below) has to make the model pick *one* of
seventeen, and each additional entry dilutes the signal on every existing one. `[inferred]`
**Weakest link:** the cap is asserted as a rule and enforced as a *count*; nobody has
measured the effect of a specific N+1st tool on the choice rate of the other N. The
"dilution" mechanism is a plausible inference from the routing history, not a measurement.
`[opinion]` on the mechanism, not on the requirement.

**R1b — the wire names themselves are a single source of truth, and that is a rule of the
repo, not a preference (traced to `internal/toolnames` and Rules 1b/13/14).**

A menu *cap* only means something if the names on it are ONE set of strings. The requirement
is that no bare tool-name literal exists anywhere — every consumer (schema `Name:` fields,
the dispatch switch, the parallel-safety / polling / content-returning maps, context-trim
logic, the raw-log formatter, the CLI mirrors) reads the constant, so renaming or merging a
tool is a one-line change and cannot half-apply. The evidence is a three-layer trace:

- **The leaf package is the definition site.** `internal/toolnames/names.go` — its package
  doc says so verbatim: "Package toolnames is the single source of truth for the agent's tool
  names. Every consumer that references a tool by name … MUST use these constants, never a
  bare string literal (Rule 1)". Its constants carry the *retirement* discipline too: the
  merged-away names (`todo`, `write_task_area`, `task_evaluate`, the `memory_*` family) each
  say "are GONE — no consumer may reintroduce them (Rule 13: a bare literal or mirrored
  constant of the retired names is a defect)". Created by commit `04d1bce0`
  ("feat(toolnames): constantize all 23 tool names — single source of truth (Rule 1)").
- **Rule 1b names this exact class.** `GOPI.md:40` (`Rule 1b`): "**Tool names** →
  `internal/toolnames` — ToolRead / ToolBash / … are the wire names the LLM emits. Never
  `"read"` inline." The rule's own BAD example is `name := "read"  // toolnames.ToolRead's
  value` (`GOPI.md:55`).
- **Rules 13 and 14 give the enforcement shape.** Rule 13 (`GOPI.md:365`, "Constants Exist
  to Be Referenced — Never Bypass Them"): a constant that is *defined but bypassed by bare
  literals is a defect*, with a grep-gate test as the anti-regression guard
  (`TestUnknownNode_MirrorLiteralGate` is the named precedent, `GOPI.md:388-402`). Rule 14
  (`GOPI.md:410`, "Shared Values Live in a Small Exported-Constant Package") names *this*
  package as the proven precedent, verbatim: "The `internal/toolnames` package is the proven
  precedent: a tiny leaf package (23 constants, zero imports of its own, zero import cycles)
  that is the single source of truth for tool wire names" (`GOPI.md:418-423`), and its
  rules of thumb are "**Small and leaf** … only constants … no imports of project packages";
  "**Exported**"; "**Never mirror, never bare-write** (Rule 1b)".

**The operator's test.** `grep -rn 'toolnames\.' internal cmd --include='*.go' | wc -l`
against `grep -rn '"bash"\|"read"\|"edit"' internal cmd --include='*.go'` — the second should
find the literal only at its definition and in tests. `[inferred]` the constant count (23)
still matches `names.go`'s full constant set today (`ToolBash` … `ToolVoiceInput`,
`ToolScreenCapture`, plus the action-value families).
**Weakest link:** Rule 14's "23 constants" is the number as written; the file has since grown
action-value constants (`ToolTask*`, `ToolJob*`, `ToolMemory*`), so 23 counts the *tool wire
names*, not every constant in the package. The doc quotes the rule rather than re-deriving
the count.

### R2 — What the model is told must survive; the words themselves are the mechanism

**Need.** When I fix the agent's bad habit (it greps instead of using the right tool; it
never trims a dead end; it never votes on a memory hit), the fix must not silently disappear
the next time somebody compresses the prompt for byte budget.

**Test.** Take the sentence that changed the behaviour, delete it, and confirm the build
goes red.

**Evidence.** This is the strongest-established requirement in the module, because it is the
only one whose *mechanism* is pinned in both halves of the surface:

- Routing text: `internal/agent/loop.go:588` (`const toolRoutingRules`) names eleven of the
  seventeen — read, write, edit, bash, code_search, keyword_search, git_blame, task,
  context_trim, task_submit, memory — with one sentence plus one "not shell X"
  counter-example each.
- The audit that created that text and its guards is
  `internal/agent/prompt_test.go:389` (`TestSystemPrompt_ToolRoutingAuditFixes`), whose
  comments carry the pre-fix numbers verbatim: `:392` "git_blame routing line (was 0 calls /
  21435 — bash stole it)", `:399` "context_trim dead-end trigger + trim-NOW anchor (was
  4/21435, 0.0%)". The same history is in `GOPI.md:329` (Rule 11's closing 机理 paragraph).
- The per-tool description is *also* pinned, because description text costs no prompt
  density: `internal/agent/prompt_test.go:176-187` checks the render tool and the vision
  tool both steer to "PIXEL-level comparison (IoU)"; `internal/agent/context_trim_test.go:42`
  checks the trim description still carries its command anchors; `internal/agent/vision_test.go:47`
  and `internal/agent/text2img_test.go:33` do the same for their tools;
  `internal/agent/search_remove_test.go:256` checks the memory description still tells the
  model to vote *immediately* and to copy the call id verbatim.
- The density gate is `internal/agent/prompt_test.go:68` (`TestSystemPromptDensity`, budget
  `< 4500` characters for the whole assembled prompt) and it doubles as a presence check —
  if a block were silently dropped, the test fails (`:74-78`).
- The devdocs that did the persuading, each with a session-log acceptance: the trim work
  (`devdoc/contexttrim-motivate-ds.md:5-12`), the blame work
  (`devdoc/gitblame-motivate-gopi.md:6-10`), the memory-vote work
  (`devdoc/memfeedback-motivate.md:3-8`), the memory-correct work
  (`devdoc/memupdate-motivate.md`), and the search-tool adoption study
  (`devdoc/code-search-llm-adoption.md:9-13`).

**Why it is a requirement.** `GOPI.md:320-326` states it as the deliverable standard: any
tool development or description or routing change must be accepted by *the model actually
calling it* — code plus unit tests is a half-finished job. `[inferred]` the extension from
"a new tool must be adopted" to "existing persuasion text must be pinned by test".
**Weakest link:** the tests pin *presence of the words*, not the *behaviour*; a pinned
sentence the model has stopped obeying still passes. §7 proposes the direction.

### R3 — The menu must be able to shrink, and the shrink must be a real decision

**Need.** As the agent's habits change, families of actions that used to need separate
entries should be able to become one entry — without losing the fact that the merged-in
call is a *different operation with different safety* (a read is parallel-safe; a write is
not).

**Test.** After the merge, the model still knows to call the read-like action freely and the
write-like action only once; the count in the rule file dropped; nothing that used to work
stopped working.

**Evidence.** The merge is a recorded operator decision — "the operator 2026-09-10 拍板 A+B 都做"
(`briefings/SPEC-tool-family-merge.md:3`) — and the design rationale is in that file: same
object with multiple operations becomes one tool with an action parameter (`:9-30`), while
tools with genuinely different *semantics* stay apart (`:31-33`: the two search tools are
deliberately not merged). The axis of safety is preserved per action: read-only search and
view stay parallel-safe, store/commit/update go serial
(`briefings/SPEC-tool-family-merge.md:16-18,22-27`;
`internal/agent/memory_tools_test.go:340` `TestMemoryActionAwareParallelSafety`). The
adoption result is measured in §6-F2: the merged names went from near-zero (575 / 318)
before the merge to dominant (11,629 / 6,283) after, in the same log corpus (§6-F2's table
carries the re-measured 2026-09-18 figures).

**The test the operator can run himself.** `gopi --help` lists a menu a human can hold; the
tool list is one line in `GOPI.md:87`; and the count is checked by the same test that guards
R1. `[inferred]` that "shrink is as sanctioned as hold-the-line."
**Weakest link:** the merge rationale is a *briefing* plus a review document
(`devdoc/tool-family-merge-review-20260909.md`, cited at
`briefings/SPEC-tool-family-merge.md:5`), not an operator ledger row; the operator's verbatim
words are the 拍板 line, and what he approved is "do both A and B", not the specific
per-family split. If the split is questioned later, the evidence is the review, not a
requirement he stated.

<!--CHUNK3-->

### R4 — Everything the agent can do, I can do by hand, under the same name

**Need.** When I do not trust the agent's result — or the agent is not running — I must be
able to perform the same action myself, and I must be able to find the agent's actions in
its log using the word I would type. Two vocabularies for one capability is how a machine
becomes un-auditable.

**Test.** Type the capability as a command; it works. Grep the agent's log for that same
word; the agent's own calls are there.

**Evidence.** The mirror is explicit for the capability tools. `web_search` — the CLI verb
IS the wire name (`cmd/agent/cli_websearch.go:69` compares `args[0] == toolnames.ToolWebSearch`),
and the same file's own doc notes the tool wire name and the command are one word. For
`web_fetch`, `text2img`, `screen_capture` and `vision_analyze` the same identity is stated
as a package contract and guarded: `cmd/agent/cli_screencapture.go:5` states
"toolnames.ToolScreenCapture == CliVerbScreenCapture, same engine", and
`cmd/agent/cli_toolnames_contract_test.go:3-16` explains the contract and guards the outer
tables (completion, usage surfaces, log family) against drift. The devdoc for text2img
records the three-layer exposure as the *hard requirement* of that integration — core, shared
tool layer, then CLI + help + log filter, "硬要求：CLI 子命令 + top-level help + log 过滤器三暴露齐全"
(`devdoc/text2img.md:5`). The same shape — one owner for the words, every other site
pointing at it — is what the repo does for its *policy* text: the worker-ownership gate
requires every document under `devdoc/`, `briefings/` and `scripts/` that mentions a
worker-launch shape to carry one pointer line at the canonical block
(`internal/testutil/worker_ownership_gate_test.go:12-24`, `devdoc/worker-ownership.md:23`),
because "a copied paragraph drifts; a pointer cannot". That is the mirror argument one
level up: the operator must be able to trace a claim to its single owner.

**Why it is a requirement, and where it is *not* met.** For the eleven routing-block tools
the mirror is *partial by design*: `read`, `edit`, `code_search`-adjacent tools and `job`
are reached through the agent loop, and some have commands while others do not — one
adoption devdoc records the model's own workaround when it hit an unregistered tool: it read
a memory record explaining the binary provenance, then "drove the same engine through the
branch's built CLI, independently re-deriving" the answer
(`devdoc/screen-capture-and-actuation-tools.md:877-880`). That is the mirror doing its job
under exactly the pressure it was built for.
`[inferred]` that mirror-parity is intended as a *requirement* rather than as
convenience-for-the-authors. **Weakest link:** parity is asserted for the capability tools by
a written contract and a drift test, but for the rest of the menu nobody has ever written
down where the mirror line is — §6-F6 makes that the finding it is.

### R5 — Work that outlives one call must be startable, and its result must be fetchable later

**Need.** Some jobs take longer than one shell call is allowed to live. I must be able to
start such a job and come back to it — and the agent must be able to do the same, without
holding a call open.

**Test.** Start a long job; the call returns while the job keeps running; ask for its output
later and get it; kill it when it is no longer wanted.

**Evidence.** The capability is a real set of rules in the prompt, not a footnote: the
prompt block `jobRules` (`internal/agent/loop.go:774`) exists as its own section, and the
density test requires the "JOB RULES" marker to be present (`internal/agent/prompt_test.go:75`);
the block's own reason is stated at `internal/agent/loop.go:774-746` — the rule not to
busy-poll, and that a long command belongs in a background job rather than a bash call with
a large timeout. The adoption of the *merged* job family is measured in §6-F2 (17,809 calls
after the merge against 950 before — because before the merge the same capability was
spelled `job_start`/`job_output`/`job_kill`, 19,271 calls in total, all of them before
2026-09-11).

**The counter-need, and it is a real one.** Not every long job is legitimate: a
monitoring-shaped loop of identical tool calls is a *stuck* run, and two bug rows record it
as a 10.7-hour cost incident (`t-1788259388`, `t-1788278732-2`). So R5 is bounded by a
second requirement the operator stated: a run that only waits must be detectable as stuck.
`[inferred]` **Weakest link:** the bound is recorded as bug rows with a design gap
("toolStreak 未实现" in the summary of `t-1788278732-2`), not as a stated requirement; the
inference is mine, not his.

### R6 — A tool that can damage must be bounded by the tool, not by the model's discipline

**Need.** When the agent runs a command, the damage it can do must be bounded by something
outside the agent's own judgement. I should not have to trust a sentence in a prompt.

**Test.** Ask the agent to do the dangerous thing in the dangerous place; it is refused, and
the refusal tells me the target it refused and how to permit it if I really mean it.

**Evidence.** This is closed, and it is closed at the right layer — the guard is in the
`bash` execution path, not in the prompt: `internal/tools/path_policy.go:210`
(`func rejectOutsideDeclaredRoots`), consulted at `internal/tools/bash.go:257`, pinned by
`internal/tools/bash_path_policy_test.go:39` and `:56`. The tests are property-based, not
pattern-based: `:58-68` lists the destructive shapes (recursive delete outside every root, a
prefix-string sibling that a naive match would allow, `chown -R`, `chmod -R`, `mount`,
raw-device writes, `find … -delete`, removing somebody else's worktree), and `:76-80`
requires the *negative* probe — legitimate in-root cleanup must stay allowed, "otherwise the
guard would break the very cleanup the system must keep doing". The refused locations were
chosen from measurement: the /dev/shm risk assessment (`devdoc/shm-worktree-risk-assessment.md`,
commit `f9c8625e`) established that /dev/shm is a RAM-backed tmpfs whose cap nobody chose,
that the interference channel is *resource* rather than IPC namespace, and that the blast
radius (host-wide OOM kill) is the real hazard — which is exactly the criterion
`internal/worktreeroots/roots.go:63` (`ShmDir = "/dev/shm"`) is refused by, with the
workspace root declared at `internal/worktreeroots/roots.go:51` (`WorkspaceDirName = "workspace"`).

**C4 status: CLOSED, and this document does not re-raise it.** Per the module briefing and
the umbrella (§10 finding 3 there), the blast-radius guard closes the operator's /dev/shm
question; it is cited here as the mechanism C4 names, never as an open gap. §6-F1 answers the
operator's question in his own terms and records what remains *outside* that closure.

### R7 — Acceptance is adoption, and there is no second standard

**Need.** I must never again be told "the tool works" by somebody who only read the tests.
If the agent does not reach for it, it is not done.

**Test.** Show me a real session, by id, in which the model called the tool on a natural
task — and show me the exact call arguments in the session log.

**Evidence, and this is the direct answer to the Rule 11 question the module briefing asks:**

**Rule 11 (`GOPI.md:318-329`) is a real, enforced-by-discipline requirement, and it has been
*met* for most of the menu and *not met* for two entries.** The standard, quoted from
`GOPI.md:322-326`: rebuild the binary; design two or more real task scenarios that naturally
trigger the tool; run real tasks and verify in `~/.gopi/sessions/*.jsonl` that the model
actually called it; if it did not, iterate the prompt and re-run "直到稳定调用" (until the
calls are stable).

**What the logs say — RE-MEASURED 2026-09-18, and three verdicts REVERSE.** The previous
pass measured "334 session files and 34,109 tool-use calls"; that figure is a **subset** of
the corpus, not the corpus. Re-measured this session over the whole tree — **3,144
`*.jsonl` files (2,036 of them `session.jsonl`), 1,867 files carrying at least one
`tool_use`, 260,670 recorded tool-use blocks**, window 2026-08-02 → 2026-09-18, host
`hostb-Laptop`. (Four measurements were taken within this session; the corpus is live and
each grew by 0.1-0.4% over the next one. The table below is the **last** — the widest window
this document can cite. Every earlier figure in this document that this table supersedes is
labelled as such inline; the drift itself is quantified in §8.4.):

```bash
# reproduce (measured 2026-09-18, GOPI run docs/req-07-tools-routing-verify)
python3 - <<'EOF'
import json,glob,collections
files=sorted(glob.glob('/home/user/.gopi/sessions/**/*.jsonl',recursive=True))
calls=collections.Counter(); sess=collections.defaultdict(set)
for p in files:
    for l in open(p,errors='ignore'):
        try: o=json.loads(l)
        except Exception: continue
        if o.get('type')!='assistant': continue
        m=o.get('message')
        if not isinstance(m,dict) or not isinstance(m.get('content'),list): continue
        for b in m['content']:
            if isinstance(b,dict) and b.get('type')=='tool_use':
                calls[b.get('name')]+=1; sess[b.get('name')].add(p)
for n,c in calls.most_common(): print(n, c, len(sess[n]))
EOF
```

**The parse rule matters, and getting it wrong inflates everything ~5×.** A session file is
a JSONL event stream; `tool_use` text also appears inside `request` records (the whole
conversation echoed on every API call — 37,237 such records) and inside `tool_result`
records. A `grep -c tool_use` over the corpus prints 472,100 (first 400 files) against 8,441
real blocks — so the count must walk `type=="assistant"` → `message.content[]` →
`type=="tool_use"`. The previous pass's parse is not wrong; its **file set** was.

| tool | calls | sessions | pre-merge | post-merge | Rule 11 evidence |
|---|---:|---:|---:|---:|---|
| read | 20,841 | 1,482 | 9,733 | 11,108 | yes |
| write | 4,497 | 1,288 | 1,747 | 2,750 | yes |
| edit | 18,734 | 1,030 | 7,195 | 11,539 | yes |
| bash | 144,586 | 1,724 | 56,164 | 88,422 | yes |
| code_search | 1,944 | 574 | 1,007 | 937 | yes (devdoc `code-search-llm-adoption.md`) |
| keyword_search | 1,860 | 583 | 1,197 | 663 | yes |
| git_blame | 86 | 75 | 48 | 38 | yes — was 0/21435 pre-fix |
| context_trim | 2,871 | 895 | 2,086 | 785 | yes — was 4/21435 pre-fix |
| job | 18,759 | 260 | 950 (as `job_start/output/kill` 19,271) | 17,809 | yes |
| task | 12,204 | 426 | 575 (as `todo` 1,488 + `write_task_area` 408 → 1,896) | 11,629 | yes |
| memory | 6,601 | 654 | 318 (as `memory_search/store/update/feedback` 3,334) | 6,283 | yes (4 motivate devdocs) |
| task_submit | 351 | 198 | 95 | 256 | yes |
| vision_analyze | 1,800 | 34 | 1,770 | 30 | yes, but loaded (see §6-F9) |
| web_search | 540 | 46 | 464 | 76 | **yes — the previous pass's "1 call" verdict is REVERSED** |
| web_fetch | 428 | 31 | 276 | 152 | **yes — previous "3 calls" REVERSED** |
| screen_capture | 12 | 6 | 0 | 12 | **yes — previous "0 calls" REVERSED** (all 12 on 2026-09-11/12, *after* the doc said zero) |
| text2img | **0** | **0** | 0 | 0 | **NO — the one verdict that survives every re-measurement** |

So **sixteen of seventeen** tools have real adoption evidence, not thirteen; **one**
(`text2img`) has none; and the previous pass's headline "web tools are un-chosen / two tools
ship unused" is an artifact of a partial file set. This is a **correction to this
document's own §5.4, §6-F3 and §6-F7**, and §8.4 records how the discrepancy was chased.

Two caveats that keep the positive reading honest:

- **Not every recorded name is a real tool.** The raw tallies also contain 17 distinct names
  the model emitted that are not menu entries (`TASK_LIST_BEGIN` 19, `grep_probe` 1,
  `substitute_in_file` 1, `record_progress` 1, `run` 1, `task(action=reframe)` 1,
  `sleep_placeholder` 1 with a malformed `<｜｜DSML｜｜ parameter>` tail, `journal` 1,
  `keywords_all` 1, `tasklist` 1, `badge_with_todo` 1, `grep` 1, `kernel` 1, `sh` 1,
  `done` 2, `todo`/`write_task_area`/`memory_*`/`job_*`/`task_evaluate` as pre-merge legacy
  spellings, and 8 with an empty `""` name). They dispatch to "Unknown tool", so they cost a
  round-trip and are invisible in the menu audit; §6-F13 files them, with dates.
- **The web-tool calls are concentrated, not routine.** 52% of all `web_search` calls fall
  on 2026-09-10 (258), and `web_fetch` likewise peaks on 2026-09-10 (184) and 2026-09-11
  (111) — the websearch-fix sessions. Adoption is *present*; it is not yet *spread*.

The three positive case-studies with session evidence in the repository are the proof that
the standard is *satisfiable*: `screen_capture`'s routing was demonstrated live — the model
reached for the tool with correct arguments on the first try and never fell back to a shell
screenshot, with the limitation stated honestly in the same paragraph
(`devdoc/screen-capture-and-actuation-tools.md:853-875`); `context_trim`'s and `git_blame`'s
revivals were accepted on session-log evidence (`devdoc/contexttrim-motivate-ds.md`,
`devdoc/gitblame-motivate-gopi.md`). The fourth, `memory_update`, is the sharpest: baseline
1 call / 21,435 with malformed input, and 4 correct calls in 2 independent scenarios after
the prompt work (`devdoc/memupdate-motivate.md`).

**The gap this creates, stated plainly.** Rule 11 is a *deliverable standard enforced by a
human reading session logs*; there is no test that fails when a tool is adopted by nobody.
**After the 2026-09-18 re-measurement the failing population is ONE tool, not two:** only
`text2img` stands at zero calls; `screen_capture` has 12 (all on 2026-09-11/12, i.e. after
the previous pass measured zero), and the web pair is 540/428 calls. What remains is the
structural gap: `text2img` was built with Rule 11 acknowledged in its devdoc, and
`screen_capture` shipped with its adoption section labelled "routing PASSED; end-to-end
execution blocked by the merge" (`devdoc/screen-capture-and-actuation-tools.md:841`). §6-F3
and §6-F5 file the distinct failures that produces, and §7 names the direction (a check, not
a paragraph) — which is the same ratchet the repo already used for its six
machine-checkable rules.

<!--CHUNK4-->

## 5. Acceptance in user terms

How the operator would personally confirm this module is doing its job. Each line is
something he can do, not something he must take on trust.

**5.1 "I can see the whole menu in one breath."**
Read `GOPI.md:87` — one sentence, seventeen names. Then run the check that keeps it honest:
`go test ./internal/agent/ -run TestGOPIRule3_MatchesClaudeToolDefs`. ✅ today.

**5.2 "The list does not grow behind my back."**
Add a tool definition in code without updating the rule file and the build goes red (the
test above reads `GOPI.md` and compares it to `claudeToolDefs`, `internal/agent/gopidoc_sync_test.go:54`).
✅ today.

**5.3 "The words that changed the agent's behaviour are still there."**
If someone compresses the prompt to save bytes and drops the sentence that stopped the agent
grepping instead of searching, the build goes red:
`go test ./internal/agent/ -run 'TestSystemPrompt_ToolRoutingAuditFixes|TestSystemPromptDensity'`.
✅ today.

**5.4 "The agent actually uses what it has — I can prove it from its own logs."**
This is Rule 11's acceptance, and it is **largely** met. Done by hand: pick a tool, count its
`tool_use` records across `~/.gopi/sessions/**/*.jsonl`, and read the ones that exist.
**Re-measured 2026-09-18 (full corpus, command in §4 R7): sixteen of seventeen have real
evidence; `git_blame` (86) is the thinnest adopted tool; `text2img` has zero.** ⚠️ one
exception — see §6-F3, §7. (The previous pass recorded thirteen of seventeen, with
`web_search`/`web_fetch`/`screen_capture` at 1/3/0; that reading came from a subset of the
file tree and is corrected in §8.4.)

**5.5 "I can do by hand the same thing I asked it to do."**
For the outward-facing capabilities the command and the tool are the same word:
`gopi web search …`, `gopi web fetch …`, `gopi text2img …`, `gopi screen_capture …`,
`gopi vision …`. The identity is a written contract with a drift test
(`cmd/agent/cli_toolnames_contract_test.go`). ✅ today, for the capability tools.

**5.6 "When it does something dangerous, a guard refuses it — not a prompt."**
`internal/tools/path_policy.go:210` refuses a destructive shape whose resolved target is
outside every declared root, naming the target. Verify by test, not by trust:
`go test ./internal/tools/ -run 'TestPathPolicy_RefusesWorktreeAddOutsideDeclaredRoot|TestPathPolicy_RefusesOutOfRootRecursiveDelete|TestPathPolicy_AllowsInRootTargets'`.
✅ today (C4's closure).

**5.7 "The agent's shell calls are visible — anything it can only reach through the shell
leaves a trace."**
Actuation is a command on purpose so that driving the mouse or keyboard costs a visible
`bash` call (`devdoc/screen-capture-and-actuation-tools.md:679-681`). Read a session log and
every one of those is sitting there as a `bash` record. ✅ today.

**5.8 "Nothing on the menu needs me to install a second runtime."**
`go.mod` has three direct dependencies (`modernc.org/sqlite`, pure Go, plus
`github.com/google/uuid` and `golang.org/x/sys` — re-counted 2026-09-18; the previous pass's
"exactly one" counted only the bare `require` line), and the tools
that would otherwise need one were built by hand instead — the screen capture tool is a
hand-rolled X11 client, "Pure Go, zero-CGO, zero-dependency, no external binary"
(`internal/agent/loop.go:1465`), and the render tool is a hand-written TrueType rasteriser
(`devdoc/text2img.md:7`). ✅ today — **with one exception, and it is the C1 tension of this
module: the web search tool executes Python scripts from a directory outside the repo**
(§6-F4).

**5.9 "I can tell whether the documentation of the tools matches the tools."**
Partly: the inventory is checked (5.1/5.2), and the individual descriptions are pinned by
presence tests per tool (§4 R2). What is *not* checked is whether the model obeys them
(§7), and whether a capability that moved to a command still has a discoverable home
(§6-F6). ⚠️ partial.

---

## 6. Gap analysis

Four surfaces: **serves** (the need is real and the code meets it — cite the site),
**falls short** (the need is real, the code does not meet it), **over-builds** (the code
does something no need asks for), **never decided** (nobody ever decided; the shape is an
accident of history). Findings are numbered F1…F14. At least one is new to the operator
(§6-F10, with §6-F14 the second), and the C1–C5 check closes the section.

### Serves

- **The menu and its guard.** R1's need is met structurally, and the drift that motivated
  the guard is recorded in the guard's own comment (`internal/agent/gopidoc_sync_test.go:46-53`).
- **Persuasion text under test.** R2's need is met for the eleven routed tools and for nine
  tool descriptions with dedicated presence tests
  (`internal/agent/prompt_test.go:176-187`, `internal/agent/context_trim_test.go:42`,
  `internal/agent/vision_test.go:47`, `internal/agent/text2img_test.go:33`,
  `internal/agent/search_remove_test.go:256`, `internal/agent/memory_tools_test.go:116`).
- **The family merge and its safety axis.** R3 is met, including the per-action
  parallel-safety preservation (`internal/agent/memory_tools_test.go:340`) and the "keep these apart"
  decision for the two search tools and for the three file tools
  (`briefings/SPEC-tool-family-merge.md:29-33`, "❌ 不合并（保留独立工具——审查结论）").
- **The mirror contract for capability tools.** R4 is met there, with a drift test
  (`cmd/agent/cli_toolnames_contract_test.go:3-16`) and a stated package contract
  (`cmd/agent/cli_screencapture.go:5`).
- **Background jobs.** R5's capability is met and adopted (§6-F2).
- **The blast-radius guard.** R6 is met and CLOSED (`internal/tools/path_policy.go:210`,
  `bash_path_policy_test.go:39,56`; negative probe at `:76-80`). Per the briefing this is
  cited, not re-raised.
- **Adoption as the standard.** R7's *standard* is met as a written, repeatedly-applied
  discipline with session-log acceptance in five devdocs (`contexttrim-motivate-ds.md`,
  `gitblame-motivate-gopi.md`, `memfeedback-motivate.md`, `memupdate-motivate.md`,
  `code-search-llm-adoption.md`).

### Falls short

**F1 — The operator's /dev/shm question has a complete written answer, and the answer's
*first* clause is now stale. (New measurement; the assessment itself is cited, not
re-raised.)**

The operator asked, verbatim (ledger `01a0975a-3e3f-7ae5-a848-a98cd24d6dc3`): *"is /dev/shm a
shared memory? what is purpose of creating such worklist for git repo in shared memory? …
is gopi clean up after development? … is there any danger using this /dev/shm path for
worktree? … should we setup any blacklist/whitelist of system resource like bash command,
system path, system call to avoid one mistake causing disaster?"*

The answer exists and is measured: `devdoc/shm-worktree-risk-assessment.md` (commit
`f9c8625e`), §0 answering each question in turn, and its honesty caveats listed at §8 there.
The parts that matter for *this* module:

- The risk was real and is named: RAM/OOM blast radius, not IPC corruption —
  "zero POSIX shm objects", the interference channel is "**RAM (resource)**, not name
  collision or corruption" (`devdoc/shm-worktree-risk-assessment.md:15`).
- The whitelist/blacklist question is answered in code, one day later, at the right layer:
  `internal/tools/path_policy.go:210` + `internal/tools/bash_path_policy_test.go:39,56`.
- The residue question was answered honestly: "For **task-attached** worktrees: yes … For
  **ad-hoc / fixture** worktrees: **no** — measured 11 stale fixture worktrees still
  registered" (`:13`).

**What has changed since, measured 2026-09-17 on this host:** `/dev/shm` holds 454 MB of a
7.3 G cap, **zero** registered git worktrees (down from 19), and 53 entries of which **32
carry the repo's own `gopi-` prefix — i.e. residue still present, with no worktree left to
attribute it to.** The 32 entries contain no `git worktree list` registration, so the
task-keyed cleanup chain cannot see them by construction (`:81,138` name exactly this
mechanism). `internal/scratch/base_residue.go:76` still declares the default scratch base to
be that same tmpfs (`DefaultBasePath = worktreeroots.ShmDir`), so the *default* place new
ad-hoc residue lands is still the RAM-backed mount whose cap "nobody has ever chosen"
(`:133`). Also live as of the same measurement: the registry entry `gopi-before-master`
(`git worktree list`, 2026-09-17) is a detached-HEAD worktree with no branch — the kind of
row the sweep's ownership key cannot match to a task.

**Why this is a "falls short" and not a "serves".** The guard closed the *worktree-in-/dev/shm*
hole; the residue that motivated the concern is still accumulating under a default that
points there, and the cleanup mechanism is keyed on something (a task row / a branch) that
this residue no longer has. The need (C4: "gopi cleans up after development") is real and the
*named* mechanism does not reach it. **Not re-raising the closed guard** — the finding is
about the default and the keying, both of which the closure did not touch.

**F2 — The tool-family merge is the largest single adoption event in the log corpus, and
nobody recorded it as evidence. (New.)**

Measured split by date around 2026-09-11 (the merge round). **Re-measured 2026-09-18 on the
full corpus (§4 R7's command), and the merge effect is *larger* than this table first showed
— so the finding below stands and strengthens:**

| name | before | after | same capability, old spelling (all before) |
|---|---:|---:|---|
| `task` | 575 | 11,629 | `todo` 1,488 + `write_task_area` 408 |
| `memory` | 318 | 6,283 | `memory_search` 902 + `memory_store` 1,160 + `memory_feedback` 1,244 + `memory_update` 28 |
| `job` | 950 | 17,809 | `job_start` 897 + `job_output` 18,204 + `job_kill` 170 |

(The first pass's row values — 1,012 / 513 / 1,343 — were subset figures, and the second
pass's — 575 / 318 / 950 before and 11,571 / 6,225 / 17,778 after — were the same
measurement one live-corpus increment earlier; the *shape*
is identical: the merged name went from absent to dominant, and the old spellings stop dead
at the merge date. `job_output`'s 18,204 pre-merge calls are the strongest single datum (it was called more
than the merged `job` tool is now, 18,204 vs 17,809): a
spelling that was used more than any other job verb was retired and replaced by an `action`
parameter with zero residual use.)

The Rule 11 evidence in the repo for these three is the *old* families' devdocs (e.g.
`devdoc/memfeedback-motivate.md` recording 28 calls / 21,435 and a 9% adoption rate). The
merge itself — the change that multiplied the call counts by roughly 1.5× for memory and
replaced two 100-call families with one 1,000-call family — has **no adoption record at
all**. Under Rule 11, "改 prompt 路由引导" is explicitly in scope of what requires adoption
proof (`GOPI.md:320`), and a rename/merge is a routing change of the most drastic kind.
`[inferred]` the merge explains the jump.
**Weakest link:** the window also contains the DeepSeek model/effort changes, so the log
split is a *correlation*; the merge is the only documented change of that date that touches
the names themselves, which is why I attribute the rename-driven part (old names → 0, new
names → present) to it, and only that part. The **volume** jump I do not attribute.

**F3 — ONE of seventeen tools has zero adoption evidence (`text2img`), and the 2026-09-17
"two tools at zero" verdict was a *subset* artifact. (REVISED by the 2026-09-18
re-measurement; the unstated-policy claim survives, narrowed to one tool.)**

Re-measured this session over the full corpus: `text2img` has **0** `tool_use` records in
260,670, and `screen_capture` has **12** (6 sessions, all on 2026-09-11/12 — i.e. the tool
was adopted *after* the previous pass recorded it at zero, which is itself evidence that the
previous pass's file set stopped short). Both were built with Rule 11 in view:

- `text2img` was integrated as a three-layer exposure with the hard requirement stated as
  the three CLI surfaces, not as an adoption experiment (`devdoc/text2img.md:5`), and its
  tool description carries the render-truth rule under a presence test
  (`internal/agent/prompt_test.go:178-181`).
- `screen_capture` has a Rule 11 section that says, in its own heading, "routing PASSED;
  end-to-end execution blocked by the merge" — the model *did* choose the tool with correct
  arguments, and the dispatcher refused the name because the serving binary was built from
  master while the tool was on an unmerged branch
  (`devdoc/screen-capture-and-actuation-tools.md:841-875`).
- The actuation half (`gopi act`) was deliberately **not** given a tool slot, on Rule 3 +
  Rule 11 grounds, and the devdoc says so plainly: "Adding an `act` tool slot would have
  required 2+ live task scenarios proving the model calls it — that experiment is a
  *follow-up task*" (`:656-659`), with MVS-6 as the ranked slice (`:674`).

**The `text2img` capability is nevertheless *used*, through the shell — but far less than a
first count suggests, and mostly by its own builder.** (This table counts the **command**
path only; the 2026-09-18 re-measurement showed the `screen_capture` *tool* itself now has
11 calls, so the "tool has none" reading is confined to `text2img`.) Measured 2026-09-17 over
the previous pass's corpus, counted as *real invocations only* (a `gopi <verb>` at command
position, with grep/sed/find mentions, heredoc task payloads and the analysis's own probe
commands all excluded):

| capability | real CLI invocations | sessions | when |
|---|---:|---:|---|
| `gopi web search` / `web fetch` | 15–30 (delimiter-dependent; §8.3 (c)) | 1 | 2026-09-04/05 — the websearch-fix session, i.e. the builder testing its own tool |
| `gopi shot` (headless page renderer; **no tool, by design**) | 2–6 | 2–3 | 2026-08-31 and 2026-09-16 — the capability's own build sessions |
| `gopi act` (actuation; **no tool, by design**) | 1–3 | 1–2 | 2026-09-16 |
| `gopi screen_capture` (by hand, under its tool's name) | 0–2 | 0–1 | 2026-09-16 |
| `gopi text2img` / `vision` (by hand) | 0 | 0 | — |

The ranges are honest, not hedging: two delimiter rules (does `timeout 20 ~/work/GoPi/bin/gopi
X` count? does a command the analysis itself wrote count?) move the small numbers by 2–3×
while leaving the two facts the finding rests on untouched — **no independent user session
appears in any verb's list**, and `text2img` is zero under every rule. §8.3 correction (c)
states the rule used for the low end.

So for the **`text2img` capability** the *tool* has no users while the command path is
self-testing; and the CLI reach is itself almost entirely self-testing during the
capability's own development, not an independent product path. (My first pass at this count
was inflated roughly tenfold by matching *mentions* of the verbs in grep/sed commands; §8.3
records the correction.) **Narrowed 2026-09-18:** the identical argument does *not* hold for
`screen_capture` any more — its tool recorded 12 real calls in 6 sessions on 2026-09-11/12 —
so the "capability used only through the shell" reading is now a statement about
`text2img` alone.

**What this is, stated precisely.** It is not the failure Rule 11 was written against (a tool
nobody can be persuaded to use). It is a **different pattern that has never been decided in
writing anywhere I can find**: a capability is built as a first-class tool *and* as a
command, the command is what gets used, and the tool's Rule 11 bar is then met
retroactively-or-not at nobody's discretion because there is no gate. `screen_capture`'s
MVS-6 row and the `act` follow-up both name the same missing experiment. `[inferred]` that
this is tolerated deliberately (the command path is the safety gate, §3.3), rather than
forgotten. **Weakest link:** I found no row or devdoc that *says* a tool may ship without
adoption evidence when a command mirror exists — the tolerance is my reading of the pattern,
and it is exactly the kind of unstated policy F11 asks to be decided.

**F4 — C1 tension, answered: there WAS a decision, it is the operator's own, and the code
implements roughly half of it. (This is the module's named C1 question.)**

The briefing's framing is: the web search tool delegates all fetch/parse/fusion to a deployed
Python tree, while the script gate deliberately excludes `plugins/` — *find the canon
decision, or record with evidence that nobody ever decided.*

**The canon decision exists, and it is the operator's, verbatim, dated:**

> **核心命题（the operator 2026-08-21）** web search 不是 gopi 的 builtin，而是一组**0-token 脚本 +
> skill 注入**的插件。关键洞察：**脚本执行消耗 0 agent token，可以无限并行；agent 只花
> token 在"选择方法 + 评估结果"上。**
> — `~/work/hermes-agent/devdoc/gopi-websearch-plugin-design.md:3-5`

So the C1 tension is **not** an undecided accident: the operator decided that web search is
*not* a built-in, and *is* a plugin of zero-token scripts plus a skill-injection layer, for a
reason he states (scripts cost no agent tokens). What the repo then did is best described as
**half of that design, committed, and the other half left where it lives**:

- **Implemented:** the script layer — eight Python scripts in `plugins/websearch/scripts/`
  (`arxiv_search`, `bing_search`, `deepseek_ask`, `github_search`, `google_search`,
  `merge_results`, `web_fetch`, `wiki_search`), all git-tracked (17 files under `plugins/`),
  executed by the Go tool via `python3` (`internal/tools/websearch.go:226` and `:382`), with
  a directory override for deployment and hermetic tests (`:32`
  `webSearchScriptsDirEnv = "GOPI_WEBSEARCH_SCRIPTS_DIR"`, resolved at `:179`). As of
  2026-09-17 the repo copies and the deployed copies under `~/.gopi/plugins/websearch/scripts/`
  are **byte-identical, all eight** (measured by `cmp`).
- **Implemented as tools:** `web_search` and `web_fetch` are registered tool names
  (`GOPI.md:87`; definitions at `internal/agent/loop.go:1098` and `:1032`), i.e. the model
  *does* have builtin-named web capability — which is in tension with "not gopi's builtin"
  in the letter, and consistent with it in the spirit (the Go side is a thin shim; all
  logic is in the scripts).
- **Not implemented: the skill layer.** The design's three-layer architecture is
  scripts / **skill injection** (routing rules + per-method cost & availability) / agent
  decision, drawn at `gopi-websearch-plugin-design.md:11-25` (the tree at `:11-20`, the
  layer table at `:22-25`). The skill file
  (`~/.gopi/plugins/websearch/skills/websearch.md`) is named in the design and in the code
  comments (`internal/tools/websearch.go:23-24`), but **nothing on the Go side reads it** —
  the method vocabulary is instead hard-coded into the tool description
  (`internal/agent/loop.go:1099`). This is a **third** mechanism nobody chose: the design
  wanted the method table injected as a skill, the code injects it as a fixed description.
- **The gate exclusion, and why its stated rationale does not fit the scripts it excludes.**
  `plugins/` is excluded from the script gate on this reasoning, quoted in full:

  > `// plugins/ is deliberately NOT here: those scripts are a deployed remote`
  > `// service (a systemd watchdog whose \`while true\` is its job), not a dev/test`
  > `// scratch workspace — see devdoc/tmp-ban-scratch-root.md.`
  > — `internal/testutil/scripts_gate_test.go:580-582`, and the root list at `:583`
  > (`var auditedScriptRoots = []string{scriptsDirName, devdocDirName, testdataDirName}`)

  The rationale describes the `remotebox/` subdirectory (a deployment with a systemd
  watchdog, `plugins/websearch/remotebox/google-cdp.service`) — and that part is accurate.
  It does **not** describe the eight `scripts/*.py` files the tool actually executes: those
  are not a remote service, they run locally under `python3`, and they are the code whose
  correctness decides whether the web tools return junk. So the exclusion was reasoned for
  one subtree and inherited by another, and the inherited part is the part that is load
  bearing.

**Verdict for the briefing's question:** the decision was **made** (operator, 2026-08-21,
hermes-agent devdoc) — so this is *not* a "never decided" finding. It is a **C1 conflict that
is deliberate in intent and unresolved in mechanism**, and the unresolved mechanism is the
gate-scope inheritance above. Filing it as a C1 conflict under §6's rules, with the
recommendation in §7 (narrow the exclusion to `remotebox/`, or state in the gate why the
scripts stay out).

**F5 — The plugin tree's provenance is a hand copy, and its own recovery commit says why
that is dangerous. (New.)**

`plugins/websearch/scripts/` is git-tracked today, but its history shows the tree was
originally **outside** version control and was mirrored back in only after a live
degradation: commit `453c1fd9` — "fix(websearch): recover merge_results.py + deepseek_ask.py
from peer1 — mirror last 2 unversioned plugin scripts (audit B5)", whose body records that
the merged default "silently degraded to bing-only" when `merge_results.py` was missing from
the deployed directory, and that the recovered copies were "the only live copy on earth —
never committed on any branch". There is **no installer in the repo**: nothing under
`scripts/` references `plugins/websearch`, and the only directory resolution is the env
override plus the `~/.gopi/…` default (`internal/tools/websearch.go:179-188`). Drift is
therefore a live possibility that only luck currently prevents (the copies match *today*,
measured by `cmp`, 2026-09-17).

**The need.** Under C1 the deliverable is one binary; under C2 everything must be
observable and reversible by the operator. Today the behaviour of two of seventeen tools is
determined by files under a user-home directory that nothing in the build verifies, installs,
or versions together with the binary.

**F6 — Where the mirror line is drawn has never been decided, and the surfaces disagree.
(New.)**

R4 asserts parity for the *capability* tools, and the repo enforces it for `web_search`,
`web_fetch`, `vision_analyze`, `text2img`, `screen_capture`, `act`, `shot`, `code_search`
and the memory family. For the rest of the menu nobody has written down where the line is.
The concrete evidence is that the two lists have **drifted apart in naming and in coverage**,
and nothing checks the relationship:

- **Names disagree on purpose, in one documented case.** `code_search`'s CLI verb is
  `code-search` (hyphenated) *by decision* — the constant's own comment says so
  (`cmd/agent/cli_verbs.go:51-54`, "the HYPHENATED CLI name … deliberately not
  toolnames.ToolCodeSearch"). So the "same word for my hand and the agent's hand" need (R4)
  is **knowingly not met** for this tool, and the reason is documented while the *rule* it
  deviates from is not.
- **Coverage is partial and asymmetric.** 21 CLI verb constants exist
  (`cmd/agent/cli_verbs.go` — 20, `cmd/agent/cli_act.go` — 1: `act`), of which `shot`,
  `merger`, `worker`, `resume`, `raw`, `stats`, `scratch-gc`, `memsync`, `brew`, `chat`,
  `completion`, `version`, `log`, `proxy` and `checkpoint` have no tool counterpart at all
  (counted from the constant list, 2026-09-17) — several for good reason, none with the
  reason written as a rule.
- **The load-bearing case is `gopi shot`.** It is a headless page renderer with *no tool and
  no plan for one*, and the live-screen capture tool's own description is where the boundary
  is stated (`internal/agent/loop.go:1465`: "NOT for a web page you want rendered headlessly
  (use `gopi shot --url` via bash — that drives headless Chromium and never touches the real
  screen)"). That is a *decided* boundary — recorded inside a tool description, which is the
  only place it exists.

What is undecided is the general rule: which of my hand's capabilities must also be the
agent's, and which may live only on the command line. `[inferred]` the de-facto rule is
"a capability whose result the model must reason over needs a tool; an effect on the world
may be a command" — the same rule F11.1 reconstructs.

**F7 — The web tools' adoption is REAL and was UNDER-MEASURED by 500×; what survives is that
the operator called the tool useless and the log shows it is not *spread*. (REVERSED
2026-09-18 against this document's own previous pass.)**

Ledger `t-1788478571` is the operator's own report, and it opens with the words: *"The
following is gopi tool 'web search' which turns out to be useless for a real request. You
are supposed to test and fix this issue until web search tool can retrieve worthy result to
help research work."* The follow-up row `t-1788497909` verifies the state and names it: "the
arxiv path is fixed … but the other sources are dead and noise remains", with `bing empty` /
`google timeout` measured live. Great care went into the fix — `dfae0c4a`, `33b3365b`,
`68712dfb`, `99822f97`, `56f403fe`, `e55f1ecc`, `18121dee` — and `453c1fd9` records
"Live (HP laptop, residential egress): 'shunyu yao paper' merged = bing ok 5 + arxiv ok 5 +
google fail-fast".

**The previous pass measured `web_search` at 1 call and `web_fetch` at 3, both "inside one
session", and concluded the tools were essentially un-chosen. Re-measured 2026-09-18 over
the FULL corpus (§4 R7's command): `web_search` = 540 calls in 46 sessions, `web_fetch` =
428 calls in 31 sessions.** The prior figure was a subset artifact; the tool is *not*
un-chosen, and this finding's original headline is withdrawn. What the corrected numbers do
show, and what the operator's complaint still stands on:

- **The calls are concentrated, not routine.** `web_search`: 258 of 540 (48%) on the single
  day 2026-09-10, with 51 on 2026-09-11; `web_fetch`: 184 on 2026-09-10 and 111 on
  2026-09-11 (69% across the two days). Outside the fix's own window the tools are used at a
  low background rate.
- **The operator's row remains the requirement**, unchanged by the count: a tool can be
  *called* and still be judged useless by the user it serves. That is the sharper form of R7
  — adoption is necessary and not sufficient, and the log alone cannot see the second half.

This is now a *partially* unmet R7 need, not a contradicted one. The corrected number is the
one to cite; the "1 call in 34,109" phrasing must not be reused.

**F8 — Every tool's full description is re-sent on every request, and nothing measures that
cost against its return. (New measurement.)**

The seventeen descriptions total **10,653 characters ≈ 2,663 input tokens** measured as the
quoted literals of each `Description:` expression (2026-09-17; the character count is exact for
that measure, the token figure `[opinion]`-grade as it uses a ÷4 heuristic). **Both figures
understate the real payload, and §8.3 #3 records the method:** the rendered descriptions —
including the concatenated `toolnames.*` fragments and their newlines — were **18,731
characters ≈ 4,683 tokens**, i.e. roughly *double* the headline number, and the largest single
entry was the `memory` tool at 4,405 rendered characters. **Re-measured 2026-09-18 (every
`Description:` expression with each `toolnames.*` constant substituted for its value — the
same method, re-run): 18,731 characters = 19,418 UTF-8 bytes ≈ 4,682-4,854 tokens (÷4,
`[opinion]`-grade), largest single entry `memory` at 4,405 characters / 4,653 bytes.** The two
character figures are **the same measurement in two units**, not two measurements — §8.5
reconciles them; the previous pass's `19,418` was the byte count labelled as characters. The
payload is growing with the menu's prose, and it grows on the per-request path. The byte
figure is the one that matters on the wire: the descriptions are not ASCII (`memory`,
`screen_capture` and `text2img` carry Chinese routing anchors), so a character count
understates the request by ~3.7%. The prompt *blocks* are gated at under
4,500 characters
(`internal/agent/prompt_test.go:70`), and the descriptions are deliberately kept **outside**
that gate — a stated design choice, made so the strongest persuasion anchors could be added
at "zero density cost" (`devdoc/gitblame-motivate-gopi.md:8`). That choice is correct for
persuasion and it means the largest per-request text payload in the menu is the one with no
budget and no measurement. With one entry adopted zero times (F3), the unexamined
portion is small but real.

### Over-builds

**F9 — `vision_analyze` is heavily used, but almost all of it is a research workload, not a
product capability. (New classification.)**

Its calls concentrate on three days: 927 on 2026-09-09, 464 on 2026-09-07 and 231 on
2026-09-06 (re-measured 2026-09-18; the previous pass saw the same shape with different
offsets — 716 on 09-07 and 601/639 on 09-10 — because it read a subset and a live corpus),
in **34 sessions out of 1,867 tool-using files** (previous pass: "9 sessions out of 334").
Outside those three days the remaining 178 of 1,800 calls are spread thin. The tool is well built and its description is the
longest and most carefully bounded in the menu (`internal/agent/loop.go:1404`, including the
"NOT for render verification — use pixel-level IoU" boundary, under test at
`internal/agent/prompt_test.go:182-185`). What over-builds is not the tool but the *claim* a
reader would make from its call count: "our most-used outward capability" would be false for
ordinary sessions. `[inferred]` the spike is the visual-studies workload (the era's devdocs
are full of screenshot/vision chains, e.g. `devdoc/screen-capture-llm-shot-report.md`).
**Weakest link:** the session files do not carry a task-type label, so the attribution of the
spike comes from the accompanying devdocs and commit dates, not from the log itself.

**F10 — THE FINDING NEW TO THE OPERATOR: the module's own acceptance standard (Rule 11) has
no mechanical gate, and the repo has already proved — six times — that its prose rules do not
survive without one. (New.)**

The operator's own rule says a tool whose LLM adoption is unproven is a half-finished job
(`GOPI.md:326`, verbatim: "光写代码+单元测试 = 半成品"). The repo's own history says the
same thing about *rules*: `GOPI.md` §Rules 16–21 exists because prose rules failed — six
standards were turned into tests, with the explicit reasoning that a rule living only in
prose "did not prevent the third incident" (`GOPI.md`, Rule 27's account of the load
generator; and the machine-checkable-gates preamble: "Rules 1–15 are prose … These six are
enforced by a test that fails red").

Rule 11 is **the one rule whose subject is the product's actual usefulness**, and it is still
prose. The consequence is measurable *today*, not hypothetical: **one tool stands at zero
calls (`text2img`, F3), the thinnest adopted tool sits at 86 calls (`git_blame`), and no
build, no test, and no scheduled sweep says either number.** (The previous pass's sharper
claim — "two tools at zero and one at one call" — was a subset artifact; §8.4 corrects it,
and the *gap* it names is unchanged: nothing observes adoption at all.) The repo even has
the raw material for the gate — the raw logs and session JSONL are
already parsed for other purposes, and the adoption question is a count over them. This is
the same ratchet pattern the repo validated six times: make the smallest deterministic
check, give it a baseline of the pre-existing state, and let it fail only on new violations
(`internal/standards/`, `devdoc/standards-gate.md`). §7 gives the direction without writing
the test (DOCS ONLY).

### Never decided

**F11 — Three questions in this module have no written decision anywhere I can find. They are
recorded as findings, not filled by guessing.**

1. **Which capabilities deserve a tool slot, versus a command, versus both.** `gopi act` has
   an explicit *decided* rationale for being a command (`screen-capture-and-actuation-tools.md:650-663`);
   `screen_capture` and `text2img` have explicit rationales for being tools; **there is no
   general rule**, and F3 is what the absence produces. `[inferred]` the de-facto rule is "a
   capability whose result the model must reason over needs a tool; a capability that is an
   effect on the world can be a command."
   **Weakest link:** the de-facto rule is my reconstruction from two devdocs, and it
   contradicts nothing — but no operator row, no `GOPI.md` rule, and no briefing states it.
2. **Whether a capability may ship both ways without an adoption experiment.** §F3. The
   two devdocs that touch it (`MVS-6`, the `act` follow-up) both say the experiment is
   *pending*, which means the question has been *deferred*, never answered.
3. **What the web tools' degraded behaviour should be when the plugin tree is absent.**
   The tool says it "Never fails — a missing dir surfaces as status=unavailable in the
   result" (`internal/tools/websearch.go:177-178`), and `453c1fd9` records a *silent*
   degradation (bing-only, no `inputs[]`, no signal) that was only discovered later. Which
   of the two is the requirement — loud unavailability or best-effort degradation — is not
   written down anywhere I found.

**F12 — M5 restated for this boundary: the gate that should guard this module is prose.**
(Umbrella §7.1 M5: gates are infrastructure, not a product surface.) `internal/standards`
(4,968 LOC) + `internal/stylegate` are how C1/C5 are enforced mechanically; this module's own
requirement (R7) sits outside them. Restated here, as the umbrella requires, so a reader of
B7 learns it without leaving this document.

**F13 — The model emits tool names that are not tools, and nothing in the menu audit can see
it. (New, found by the 2026-09-18 full-corpus scan.)**

The full-corpus tally (§8.4) contains names that are not menu entries and never were:
`TASK_LIST_BEGIN` 19, `task(action=reframe)` 1, `sleep_placeholder` 1, `grep_probe` 1,
`substitute_in_file` 1, `journal` 1, `keywords_all` 1, `record_progress` 1, `run` 1, `tasklist`
1, `badge_with_todo` 1, `grep` 1, `kernel` 1, `sh` 1, `done` 2, and 8
records whose `name` is the empty string. Each dispatches to "Unknown tool", so it costs a
round-trip and returns an error the model must recover from. **Why this is a finding and not
noise:** `TestGOPIRule3_MatchesClaudeToolDefs` audits the *inventory* (the names the binary
registers), and the routing audit pins the *prompt text* — neither observes what the model
actually emitted. The names `TASK_LIST_BEGIN` and `record_progress` are especially telling:
they are the model reaching for the harness's own bookkeeping vocabulary (the task-list block
marker the agent is told to emit) as if it were a callable verb.

**Date-resolved 2026-09-18 (the weakest link this finding carried in its first pass is now
closed).** Per-name first/last emission dates, from the same JSON walk as §4 R7:

| name | calls | first | last | reading |
|---|---:|---|---|---|
| `TASK_LIST_BEGIN` | 19 | 2026-09-13 | **2026-09-18** | **LIVE** — every one falls in the seven days before this document's own measurement date, two of them on the day it was written |
| `grep_probe` | 1 | 2026-09-14 | 2026-09-14 | live window |
| `substitute_in_file` | 1 | 2026-09-15 | 2026-09-15 | live window |
| `run` | 1 | 2026-09-17 | 2026-09-17 | live window |
| `(empty name)` | 8 | 2026-08-28 | 2026-08-29 | old; malformed call |
| the remaining 11 | 1 each | 2026-08-06 | 2026-09-11 | old tail |

So this is **not** a historical tail: the single most frequent non-tool name is also the most
*recent*, and its window ends on the measurement date. Whatever produces it is still running.
The `name` is `TASK_LIST_BEGIN` — the literal marker the agent's own task-list protocol tells
it to emit — which is `[inferred]` a protocol/parse confusion between "text the agent writes"
and "a tool it may call"; the count is established, the mechanism is not, so this stays filed
as an observational finding against the same absent instrument as F10: nothing in the build
observes what the model *chose*, only what it *could* choose.

**F14 — The wire-name package carries MORE tool names than the menu has, and no test ties the
package to either inventory. (New, found by the 2026-09-18 verification pass.)**

R1b establishes that `internal/toolnames` is the single source of truth for wire names (Rule
1b/13/14). That trace has a hole the earlier pass did not open: the package is **not** itself
gated to the menu. Measured 2026-09-18:

- `internal/toolnames/names.go` declares **29** constants matching `Tool…`: **18** tool-name
  constants, **7** job/memory action verbs (`ToolJobActionStart/Output/Kill`,
  `ToolMemoryActionSearch/Store/Update/Feedback`) and **4** task-action verbs
  (`ToolTaskView/Commit/Review/Reframe`).
- Of the **18** tool-name constants, **17** name a menu entry and one — `ToolVoiceInput`
  (`voice_input`) — is deliberately CLI-only; its own doc comment says so, "no model tool,
  because transcription happens before the model sees anything".
- The menu has **17** entries (`claudeToolDefs`, `GOPI.md:87`). 18 ≠ 17. The retired names
  (`todo`, `write_task_area`, `task_evaluate`, the four `memory_*` verbs) survive only as
  doc comments on the constants that absorbed them, carrying the "are GONE — no consumer may
  reintroduce them (Rule 13)" discipline.

**The discrepancy is benign in itself** — `voice_input` is a CLI verb sharing the same
constant source, which is R4 working as designed. **The finding is that nothing checks it.**
The two inventories that exist are gated to each other
(`TestGOPIRule3_MatchesClaudeToolDefs`: `claudeToolDefs` ↔ `GOPI.md` Rule 3) and the CLI
mirror is gated to the constants (`TestCLIToolMirrorVerbsMatchConstants`,
`TestCLIMirrorVerbsCoveredByContractInventory`), but **no test asserts the set of
`toolnames` tool-name constants and the set of `claudeToolDefs` names agree** — not in
count, not in membership. A new constant with no tool def (or a tool def with no constant)
is invisible until someone reads the file.

**Why it matters under Rule 14, not just Rule 1.** Rule 14's whole thesis is that the leaf
constant package is the single definition site precisely so drift is impossible by
construction. Here the package is the definition site but has **two** consumers with
**different** sets — the model-facing menu (17) and the CLI verb surface (18) — and the
boundary between them is a convention recorded in a doc comment, not a check. That is the
same shape Rule 13 was written against (a value whose one permitted holder is not enforced),
one level up: here the *membership rule* is enforced by nothing. The fix is a §7-shaped item,
not a defect in the package: a test asserting `toolnames`' tool-name constants ↔
`claudeToolDefs` (with `voice_input` named explicitly as the CLI-only exception, the way
`auditedScriptRoots` names its own exclusions).

**Weakest link:** I derived "22 tool-name constants vs 7 action verbs" by reading the
constant block's own comment structure, not from a marker the compiler sees; a differently
organised future edit could shift that split without changing the finding (`len(menu) = 17`
vs `len(toolname constants) > 17` is the defensible pair, and both were counted directly).

### The C1–C5 check

Each constraint, checked against every requirement above. A conflict is filed, not hidden.

| C | Requirement(s) it constrains | Verdict |
|---|---|---|
| **C1** one static binary, zero deps, no plugin architecture | R7 (web tools), F4, F5 | **CONFLICT, filed as F4 + F5.** The binary is static (5.8 ✅) and the outward tools were hand-built to stay dependency-free; but two of seventeen tools have their behaviour defined by a Python tree under the user's home. The conflict is **deliberate in intent** (operator, 2026-08-21, F4) and **unresolved in mechanism** (the gate's exclusion rationale does not fit the scripts it excludes; no installer; F5's recovery commit). Filed as a §6 finding per the briefing. |
| **C2** the operator controls the whole thing | R1, R4, R6, F6, F10 | **GAP, filed.** R1 (auditable menu) and R6 (a guard he can test) serve C2 well. R4 serves it for capability tools. But F6 (no decided mirror rule) and F10 (adoption has no gate) mean two surfaces he must control — what the agent can reach, and whether it reaches anything — are governed by prose and habit. |
| **C3** a daemon must be simple | — (not this module) | **No conflict.** This module owns no daemon. The nearest thing is R5 (background jobs), which is the *simple* form: start, fetch, kill, three actions, with an explicit "do not busy-poll" rule (`internal/agent/loop.go:774-746`). No branch added to a resident process. |
| **C4** the agent cleans up after itself | R6, F1 | **SERVES the closed half; the open half is F1.** The guard is closed and cited, not re-raised (R6). The residue question C4 asks is answered *mechanically* only for task-attached worktrees; measured 2026-09-17, 32 `gopi-`-prefixed entries sit in /dev/shm with no worktree registration for the sweep to key on, and the scratch default still points there (`internal/scratch/base_residue.go:76`). Filed as F1. |
| **C5** small team, limited resources | R1, R3, F8, F10 | **SERVES, with one unmeasured cost.** R1 (the cap) and R3 (the merge) are cost work in the right direction, both with recorded rationale. F8 is the counter-item: ≈4,682 input tokens per request of capability text (18,731-char / 19,418-byte rendered measure, re-measured 2026-09-18, §8.3 #3), outside the density gate, with one entry nobody uses. `[opinion]` the cost is small in absolute terms; the *absence of a measurement* is what C5 objects to. |

<!--CHUNK5-->

## 7. What this implies for tests

**No tests are written by this document** (series rule 7). What follows is a judgement about
the tests that exist, and the direction for the ones that do not — prose, not code.

**7.1 The tests that carry this module's requirements today — name them, and their verdict.**

| test | what it actually protects | verdict |
|---|---|---|
| `TestGOPIRule3_MatchesClaudeToolDefs` (`internal/agent/gopidoc_sync_test.go:54`) | the inventory in the rule file and `claudeToolDefs` agree, in count and in names | **KEEP — load-bearing.** It is R1's whole mechanical half, and the drift it was written for is documented in its own comment (`internal/agent/gopidoc_sync_test.go:46-53`). Direction: leave it; if the count changes, this is the one place that must change with it. |
| `TestSystemPromptDensity` (`internal/agent/prompt_test.go:68`) | the assembled prompt stays under 4,500 chars **and** every required block marker is present | **KEEP — load-bearing, and note the asymmetry.** The presence half is what stops a block being dropped silently; the budget half deliberately does not cover tool descriptions (F8). Direction: extend the *measurement* (not the budget) to report the description payload, so F8 stops being invisible. |
| `TestSystemPrompt_ToolRoutingAuditFixes` (`internal/agent/prompt_test.go:389`) | the four audit fixes (blame routing line, immediate memory vote, one-call code search anchor, trim-NOW anchors + the bash file-read warning) survive | **KEEP — this is R2's mechanism.** Note what it can and cannot do: it proves the words are present, not that the model obeys them. |
| per-tool description presence tests — `internal/agent/vision_test.go:24` (`TestVisionAnalyzeToolSchema`), `internal/agent/text2img_test.go:15` (`TestText2imgToolSchema`), `internal/agent/context_trim_test.go:17` (`TestContextTrimToolSchema`), `internal/agent/search_remove_test.go:243` (`TestMemorySearchDescriptionImmediateFeedback`), `internal/agent/memory_tools_test.go:91` (`TestMemoryToolDescriptionStrategy`), `internal/agent/prompt_test.go:176-187` | each tool's description still carries its routing anchors and its boundary ("NOT X") sentences | **KEEP — the right shape.** Direction: this is the pattern F10 wants extended *outward* — the same tests that pin the persuasion text should have a sibling that counts whether the persuasion worked. |
| `TestSystemPromptCounterExamples` / `TestSystemPromptNoGoPiSpecifics` / `TestPromptSectionsSorted` (`internal/agent/prompt_test.go:194`, `:128`, `:375`) | every routed tool carries a "not shell X" negation; the kernel prompt stays free of project-specific words; sections stay ordered | **KEEP.** The second is why the module's own tool names may not leak into the kernel prompt — a real boundary this document respects in §1. |
| `TestPathPolicy_*` (`internal/tools/bash_path_policy_test.go:39,56,76`) | the blast-radius guard refuses destructive shapes outside declared roots, names the target, and does **not** refuse legitimate in-root cleanup | **KEEP — C4's closure, and the negative probe is essential** (`:76-80`). Direction: unchanged. This document cites it, never re-raises it. |
| `TestMemoryActionAwareParallelSafety` (`internal/agent/memory_tools_test.go:340`) | a merged tool keeps per-action safety (read-only parallel, writes serial) | **KEEP — R3's safety axis.** This is the test that makes "merge families" safe to repeat. |
| `cmd/agent/cli_toolnames_contract_test.go` + `cli_contracts_test.go` | the CLI's user-visible tables stay in lockstep with the tool wire names | **KEEP — R4's mechanical half.** Direction: F6 asks for the missing half — which capabilities *must* have a mirror. Note what these do **not** cover: they tie the CLI to `toolnames`, and `TestGOPIRule3_MatchesClaudeToolDefs` ties `claudeToolDefs` to `GOPI.md`, but no test ties `toolnames` to `claudeToolDefs` — see F14 and §7.5. |

**7.2 The gap §7 must name: there is no adoption test, and Rule 11's own history says there
should be.**

Every test above is a **presence** test — the words are there, the names agree, the guard
fires. None of them can fail because the model stopped choosing a tool. The repo already
knows this shape of problem and has solved it six times: a prose rule that keeps being
violated becomes a gate with a baseline of the existing violations, ratcheting down
(`GOPI.md` Rules 16–21; `internal/standards/`; `devdoc/standards-gate.md`). Rule 11 is the
remaining prose-only rule, and F3's zero-call tool is its live violation.

**Direction (not a test; a shape for whoever writes it).**
1. **A data report first, a gate second** — the established pattern. A report that emits, per
   tool wire name, the call count and the date of last use across the session corpus, so the
   number is visible without anyone deciding a threshold. (`TestStandardGateDataReport` is the
   precedent for a report that does not fail.)
2. **The gate on the *delta*, not the level.** One tool is at zero today (F3) and the
   thinnest adopted tool at 86 calls (`git_blame`); a red-on-day-one gate would be disabled,
   which is the exact failure mode
   the standards doc rejects. The adoptable form is: a **newly added or newly modified tool
   description must ship with recorded adoption evidence** (a session id + the tool_use
   record), with the current seventeen baselined. That is Rule 11's own text turned into a
   ratchet.
3. **Keep it in the merge/verify path and cheap** — a count over files that already exist,
   not a new subsystem (C5).
4. **Do not test the model.** The temptation is an end-to-end adoption run in CI; that is
   expensive, non-deterministic, and it would violate Rule 27's family of mistakes in
   spirit (manufacturing conditions to produce a number). The honest instrument is the log
   the runs already write.

**7.3 The gate-scope item from F4 is a *test* matter and belongs here too.**

`internal/testutil/scripts_gate_test.go:583` excludes `plugins/` wholesale. The stated
rationale (`:580-582`) fits `plugins/websearch/remotebox/` — a deployed service with a
systemd watchdog — and does not fit `plugins/websearch/scripts/*.py`, which run locally under
`python3` and decide the web tools' output quality. **Direction:** narrow the exclusion to
the subtree the rationale names, or state in the gate why the script subtree stays out.
Either is a decision; the current state is an inherited exemption, which is F4's finding.

**7.4 What must NOT be added.**

- **No test that fabricates traffic** to make an adoption number appear (Rule 27's family).
  Adoption must be observed in real sessions or reported as absent.
- **No test asserting a routing sentence's *wording* beyond presence.** Wording tests
  (`TestSystemPrompt_ToolRoutingAuditFixes`'s string equality with specific anchors) are
  justified only because those exact strings were the measured fix; extending that style to
  new prose would freeze text nobody has evidence for.
- **No gate on the tool count other than the existing drift test.** R1 is about a *cap the
  model can hold*, and no test measures that; inventing a numeric assertion would be a
  fabricated requirement (§8 honesty rule 1).

**7.5 The missing inventory link (F14) — the one gate §7 can name outright.**

Three tests in §7.1 hold two inventory pairs together (`claudeToolDefs` ↔ `GOPI.md`,
`toolnames` ↔ the CLI mirror) and **none of them holds `toolnames` ↔ `claudeToolDefs`** —
the pair R1b's single-source-of-truth claim actually rests on. Stated as a direction, not a
numeric assertion:

> **A test asserting that every tool-name constant in `internal/toolnames` is either a
> `claudeToolDefs` entry or an explicitly-named non-menu exception (`ToolVoiceInput`), and
> that every `claudeToolDefs` name has a constant.**

That is Rule 14's own pattern ("a value other packages share has exactly ONE definition")
applied to the *set* rather than to a single value, and it is the mirror image of
`TestUnknownNode_MirrorLiteralGate` (Rule 13): a positive membership check instead of a
literal scan. It does not freeze the tool count — it only says the two inventories cannot
silently disagree, which is what "single source of truth" is supposed to mean.

---

## 8. Confidence and limits

### 8.1 Confidence per requirement

| requirement | confidence | basis | what would refute it |
|---|---|---|---|
| R1 — a small fixed menu, additions are decisions | **high** | `GOPI.md:85-90` (rule) + `internal/agent/gopidoc_sync_test.go:54` (machine check) + a documented drift incident (`internal/agent/gopidoc_sync_test.go:46-53`) | finding a capability deliberately added without touching the rule file — none found. **Narrowed by F14**: the *count* is held on both sides of the `claudeToolDefs` ↔ `GOPI.md` pair, but the `toolnames` ↔ `claudeToolDefs` pair is held by nothing, so "additions are decisions" rests on the rule + one test, not on the constant package |
| R2 — persuasion text is protected, it is the mechanism | **high** | prompt blocks under test (`internal/agent/prompt_test.go:389`, `:68`) + five devdocs with session-log acceptance | a routing fix that was reverted, or a session where the pinned text is present and ignored over a long window (the second is *not* excluded and is exactly why §7 wants a behavioural check) |
| R3 — the menu may shrink, safety preserved per action | **medium** | operator 拍板 line + `briefings/SPEC-tool-family-merge.md` + `internal/agent/memory_tools_test.go:340` | the operator's approval is "do A+B", not the per-family split; if he rejects a family split later, the *requirement to be able to merge* still stands but its boundaries are the review's, not his |
| R4 — same word for my hand and the agent's hand | **medium** | written contract + drift test for capability tools (`cmd/agent/cli_toolnames_contract_test.go`) + `devdoc/text2img.md:5` "硬要求" | F6: the general rule is absent, so the requirement is well-evidenced *where it is written* and unsupported elsewhere |
| R5 — long work is startable and fetchable | **medium-high** | dedicated prompt block (`internal/agent/loop.go:774`) + density presence check (`internal/agent/prompt_test.go:75`) + 17,809 calls post-merge | the caps/bounds of the job rules are `[inferred]` from the block's text plus two bug rows, not from a stated requirement |
| R6 — damage bounded by the tool, not by discipline | **high** | `internal/tools/path_policy.go:210` + `internal/tools/bash.go:257` + three property tests including a negative probe + the measured /dev/shm assessment | a destructive shape that passes the guard; the tests enumerate the classes they know, so the refutation would be a *new* class |
| R7 — acceptance is adoption, no second standard | **high** | `GOPI.md:318-329` verbatim + five acceptance devdocs + the measured table in §4 | nothing found; the standard is stated and repeatedly applied. The **gap** is enforcement (F10), not intent |

### 8.2 Marked inferences and their weakest links (collected, as required)

1. **R1's dilution mechanism** — that each added tool measurably reduces the choice rate of
   the others. *Weakest link: no measurement exists; it is inferred from the routing history
   where the failures were on individual tools, not on the menu size.*
2. **R2's extension from "new tools need adoption" to "existing persuasion text must be
   pinned"** — *weakest link: the tests pin presence, not obedience; a pinned sentence the
   model has stopped obeying still passes.*
3. **R3's "shrink is as sanctioned as hold-the-line"** — *weakest link: the operator's
   verbatim word is "do both A and B"; the family split is the review's, not his.*
4. **R4's mirror-parity as a requirement** — *weakest link: written for capability tools
   only; the rest of the line is undecided (F6).*
5. **R5's second half (stuck-loop detection bounds it)** — *weakest link: recorded as bug
   rows with an explicitly unimplemented design ("toolStreak 未实现", `t-1788278732-2`), not
   as a stated requirement.*
6. **F2's attribution of the rename-driven call shift to the merge** — *weakest link: the
   window also contains model/effort changes; I attribute only the old-names→0 /
   new-names-present fact, not the volume.*
7. **F3's "this is tolerated on purpose"** — *weakest link: no row or devdoc says a tool may
   ship without adoption evidence when a command mirror exists.*
8. **F9's attribution of the vision spike to the visual-studies workload** — *weakest link:
   session files carry no task-type label; the attribution comes from accompanying devdocs
   and commit dates.*
9. **F11's de-facto rule ("result the model reasons over ⇒ tool; effect on the world ⇒
   command")** — *weakest link: reconstructed from two devdocs; stated as an inference, not
   as a decision.*
10. **F8's token figure** — *weakest link: both character measures are exact for their rule
    (10,653 first-fragment, 18,731 rendered); the token conversions use a ÷4 heuristic, and
    the *rendered* figure (≈4,682) is the one this document treats as the payload. The byte
    count is the same measurement (19,418), not a second one — §8.5.*
11. **F4's "the tool shim is consistent with the operator's decision in spirit"** —
    *weakest link: the operator's text says "not a builtin"; two builtin-named web tools exist.
    I judge the letter to conflict and the spirit to agree, and I flag the judgement as a
    judgement.*

### 8.3 The five incidents / measurements this document rests on, and how each was taken

| # | measurement | how | reproducible? |
|---|---|---|---|
| 1 | adoption table (§4 R7, §6-F2/F3/F7) | parse every `tool_use` block in the session corpus, group by tool name; split at 2026-09-11 for the merge. **The file set is the whole question: 2026-09-17 used a 334-file subset (34,109 records); 2026-09-18 used `~/.gopi/sessions/**/*.jsonl` (3,144 files, 1,867 with tool_use, 260,670 records) and reversed three verdicts — see §8.4** | yes — the same glob and a JSON walk; but state the glob, because a subset silently changes the answer |
| 2 | capability reached through the shell (§6-F3) | regex for a binary token followed by the verb at command position inside `bash` tool inputs; commands containing grep/sed/find/ls mentions of the verb, heredoc task payloads, and this analysis's own probe commands are all excluded | yes, with the exclusions stated. **Three passes, two corrected** (see (c) below). |
| 3 | description payload (§6-F8) | substitute every `toolnames.*` constant in each of the 17 `Description:` expressions for its quoted value, then concatenate the fragments and JSON-decode the escapes (this is the method that produced 18,731 in the previous pass — reproduced exactly, 2026-09-18) | yes — **re-measured 2026-09-18: 18,731 characters = 19,418 UTF-8 bytes ≈ 4,682-4,854 tokens**, largest entry `memory` at 4,405 characters / 4,653 bytes. The older literal-only figure (10,653) counts just the first inline fragment of each expression and must not be quoted as the payload; token figures use a ÷4 heuristic and are `[opinion]`. **The two character figures are one measurement in two units — see §8.5.** |
| 4 | prompt-block name coverage (§4 R2) | substring presence of each of the 17 wire names in each prompt block constant | yes |
| 5 | /dev/shm state today (§6-F1) | `df -h /dev/shm`, `ls /dev/shm | wc -l`, `ls /dev/shm | grep -c '^gopi-'`, `git worktree list` | yes |

Two methodological corrections made during the analysis, recorded because they were wrong
first: (a) the tool-adoption counts must include per-task session directories — a
top-level-only `find` under-counts by ~100 records; (b) the first shell-reach count was
inflated by the analysis's own probe commands, which is why the tight count in §6-F3 excludes
them and gives a smaller, honest number; (c) **the delimiter rule is the whole uncertainty in
§6-F3's table** — matching `gopi <verb>` anywhere in a command yields 15/2/1, matching it only
at command position after splitting on `;`, `&&`, `||` and stripping `timeout N` / `cd X &&`
prefixes yields 30/6/2/3, because the builder's session invokes the CLI both bare and through
`timeout 140 gopi web search …`. §6-F3 states the range rather than picking one and calling it
the measurement.

**Why the counts will not reproduce exactly — and why the previous pass's did not.** The
corpus is *live*: it is appended to by every running session, including the session writing
this document. Measured four times inside this one session: 259,514 → 259,786 → 260,076 →
**260,670** records (+0.45% in ~90 minutes), the same growth in files (3,138 → 3,144) and in
tool-using files (1,863 → 1,867). The previous pass recorded "334 session files / 34,109
records" and, on re-running, "334 files / 34,139" — stable *within its own glob*, which is
exactly the trap: a live corpus drifts slowly, so a stable repeat says nothing about whether
the glob was right. The 2026-09-18 full-tree measurement (§8.4) returned **3,144 files /
1,867 tool-using files / 260,670 records** — the earlier figure was a subset, and three of
its per-tool verdicts (`web_search` 1, `web_fetch` 3, `screen_capture` 0) **reversed**
(`text2img` = 0 is the one that held). Treat the current totals as "of order 260,000,
±0.5% within a session", and require any future gate (§7) to record its glob **and its
timestamp** alongside its count, because the glob — not the drift — is what changes the
answer.

### 8.4 The 2026-09-18 verification pass: what changed, and the discrepancy I could not close

**Verdict: the document's *structure, requirements and citations hold*; three of its
*per-tool adoption numbers* were wrong.** Recorded in full because a reader must be able to
tell which sentences survived the re-measurement.

**What was verified unchanged (2026-09-18):**

- **All 85 code/doc `file:line` citations resolve** against the current tree (0 bad), and a
  spot-check of the load-bearing ones token-matches the sentence they claim:
  `internal/agent/loop.go:1098` (`var claudeToolDefs`), `:1008`/`:1032` (the two web tool
  definitions), `:1375` (`screen_capture`'s description), `:1055` (memory's "唯一用户"),
  `internal/agent/prompt_test.go:68/75/389/392/399`,
  `internal/agent/gopidoc_sync_test.go:54` + its `:46-53` drift comment,
  `cmd/agent/cli_verbs.go` (present), `internal/toolnames/names.go` (commit `04d1bce0`).
- **The tool count is 17** in both places (`claudeToolDefs`, `GOPI.md:87`), and the drift
  guard is live.
- **The Rule 11 quotations are exact** — `GOPI.md:320`, `:323`, `:325`, `:326` carry
  verbatim the sentences the doc quotes, including "光写代码+单元测试 = 半成品".

**What was corrected:** (1) every `GOPI.md:298-309` / `:298-300` / `:300-305` / `:300` /
`:309` citation — written when Rule 11 began at :298, now at :318 after Rules 12–30 landed
(Rule 11 is now `:318-329`; the doc's 8 sites were re-anchored to `:318/320/322/326/329`);
(2) the adoption table, §5.4, §6-F3, F7, F9, F10, C5, §1 and §2 (numbers, per above);
(3) §5.8's "exactly one direct dependency" → three (`modernc.org/sqlite`,
`github.com/google/uuid`, `golang.org/x/sys`).

**A fourth correction, made in the second pass of the same day and recorded because the
first pass introduced it.** The first pass re-measured the description payload and printed
`19,418 characters`, presenting it as a correction to `18,731`. Both are the same string: the
earlier figure is **runes**, the later is **UTF-8 bytes**. Re-running the identical
rune-count reproduced `18,731` / `4,405` to the character. §8.5 records the reconciliation;
every site that quoted the pair now names the unit. That pass's error is left visible here on
purpose — a re-measurement that silently changes the *unit* while claiming to change the
*value* is the failure mode this section exists to catch, and it caught its own.

**The discrepancy I could NOT close, stated rather than explained away.** How the previous
pass arrived at 334 files / 34,109 calls is **not reproducible from this session**:

- 334 is not the count of `session.jsonl` files (2,034 today), nor of session directories,
  nor of files under any single depth — and the corpus's file count only grows, so 334 was
  not "all files at the time".
- The closest reconstruction found was **mtime ≥ 2026-09-17: 319 files, 35,719 records** —
  within 5% of 334/34,109, which suggests a time-windowed or partially-globbed selection,
  but I could not reproduce it exactly, and I will not assert a mechanism I did not observe.
- **The consequence is the finding, not the cause:** a subset selection produces *different
  per-tool verdicts* (three reversals above), so an adoption measurement is only as good as
  its stated glob. §8.3 #1 now requires the glob to be recorded with the number.

**Two low-value artifacts from the full scan, recorded because they are real and would
otherwise be rediscovered as "new tools":** the corpus contains names the model emitted that
dispatched to "Unknown tool" — `TASK_LIST_BEGIN` (19), `task(action=reframe)` (1),
`sleep_placeholder` (1), `grep_probe` (1), `substitute_in_file` (1), `journal` (1),
`keywords_all` (1), `record_progress` (1), `run` (1), and 8 records with an empty name. They
are not menu entries; they are a routing/format failure mode worth one line (§6-F13 territory
— filed here as an observation, not as a gap I investigated).

### 8.5 The description-payload reconciliation (2026-09-18, second pass) — the previous pass's `19,418` was the same number in different units

Recorded because the earlier pass called it a correction, and the correction was wrong.

**The method, once.** Substitute every `toolnames.*` reference inside each of the 17
`Description:` expressions with the constant's **quoted** value, concatenate the string
fragments of the expression, and JSON-decode the escapes. Measured 2026-09-18, on
`internal/agent/loop.go` at `812f9a60` (identical in all three earlier commits — `git show
<rev>:internal/agent/loop.go` gives the same numbers at `791d9ac3`, `7e9db4f1`, `0c95b008`):

| measure of the SAME rendered text | total | largest entry (`memory`) |
|---|---:|---:|
| characters (runes) | **18,731** | 4,405 |
| UTF-8 **bytes** | **19,418** | 4,653 |

Both figures are exact; they are the same string counted two ways. The delta is the
non-ASCII routing anchors the descriptions carry (`memory`, `screen_capture`, `text2img` all
hold Chinese phrases — "LLM 是唯一用户", "看屏幕", "把文本变成图片"), where one character is 3
bytes. `19,418 / 18,731 = 1.0367`, i.e. **3.7% of the payload is the multi-byte encoding**,
which is precisely what a Python `len()` over a `str` versus `len()` over `bytes` measures.

**What the earlier pass got wrong, and how it is now filed.** Its §6-F8 and §8.3 #3 presented
`19,418` as a *re-measurement that superseded* `18,731` ("the same method, re-run"), and §8.2
#10 kept two different token figures (4,683 / 4,854) for one method. In fact the second
figure is the first one's byte count: re-running the identical rune-count produced 18,731 and
4,405 again, to the character. The two numbers never disagreed; the labels did.

**Consequence for the requirement.** None of the §6-F8 finding changes (the payload is large,
it is unmeasured by the density gate, and it grows on the per-request path). One sentence of
it improves: the **byte** figure is the one that matters on the wire, because what the model
provider bills and what the request carries is bytes — so `19,418 bytes ≈ 4,854 tokens` is
the honest *cost* figure and `18,731 characters` is the honest *text* figure. Both are now
stated with their unit in §1, §4-R7, §6-F8, §6-C5, §8.2 #10 and §8.3 #3.

**Weakest link:** the ÷4 token heuristic is `[opinion]`-grade in both units, and the byte
count assumes the fragments carry no escape sequence that survives JSON decoding (verified:
the decoded text and the raw literal differ only in the 9 `\n` escapes, which the byte count
renders as 1 byte each and the rune count as 1 character each — the two counts agree exactly
once that is accounted for).






### 8.6 What I could not determine

Honest unknowns. Each is a dispatchable question, not a gap I filled by guessing.

1. **Whether the operator would accept the C1 accommodation in F4 — the script tree as a
   sanctioned exception — or would want the web tools to degrade honestly (or fail loudly) on
   a machine without the plugin tree.** The decision of 2026-08-21 sanctioned the *design*;
   it does not say what the binary does when the tree is absent, and no row answers it. The
   need is decidable by one sentence from him; the mechanism gap (the gate's inherited
   exemption) is decidable without him.
2. **Whether `screen_capture`'s MVS-6 (`gopi act` promoted to a tool, with adoption evidence)
   was ever dispatched.** The devdoc names it as a follow-up and I found no ledger row for
   it. If it was dispatched and is pending, F3's tolerance question is already in flight.
   — *Searched: the plugin/tool vocabulary over the human-source rows and the briefings
   directory; no matching row.*
3. **What "adopted enough" means numerically.** Rule 11 says "2+ real task scenarios" and
   "until stable"; it does not define a rate or a window. Whether 86 calls (`git_blame` —
   the thinnest adopted tool, re-measured 2026-09-18) is the success or the floor is not
   stated anywhere I found. This is the one number §7's proposed gate would need and cannot
   invent.
4. **Whether the `text2img` tool should exist at all.** Its Rule 11 bar is unmet (F3) and its
   CLI/`bash` path is the one with users; but the tool is *also* the only place the
   render-truth engineering rule is stated to the model, and two tests pin that
   (`internal/agent/prompt_test.go:178-181`). Removing the tool would remove a rule surface. Which need
   dominates is not decided.
5. **Whether the `gopi-`-prefixed residue measured in /dev/shm (32 entries, 454 MB) belongs
   to this module or to the worktree/scratch module.** I report the fact — the sweep's keying
   cannot see it (`devdoc/shm-worktree-risk-assessment.md:81,138`) — and I do not claim the
   cleanup requirement as this module's. Module 03 owns the sweep; this module owns only the
   *tools* (bash, the guard) that create such residue.
6. **Whether the merge's adoption jump (F2) is measurable in a way that isolates the rename
   from the concurrent model/effort changes.** A controlled comparison would need sessions
   that differ in *only* one variable; the corpus does not contain them in the window.




