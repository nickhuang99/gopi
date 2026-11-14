<!-- doc-class: normative -->
# Prompt Layering: Role-Inherent / Abstract Principle / Concrete Material

> **Provenance.** `TestDocAnchorsResolveInNormativeDocs` was retired on 2026-10-22 in `5a35f61` (deleted with `internal/docclass`), so a `file.go:NNN` reference in this page is a landing-time record, not an anchor a live gate enforces — nothing resolves one today. The line numbers stay useful to a reader; they are not checked by the suite. Citations below are quoted as they were written — where one of them reads as present tense ("is RED", "resolves", "fails"), read it as the state of THAT run, never as a gate this tree still has.

**Status:** authoritative (the three-layer division the operator ruled on 2026-09-29)
**Code home of the authority:** `internal/agent/industry_practice.go` (file header)
**Guards:** `internal/agent/industry_practice_test.go`, `internal/agent/prompt_density_budget_test.go`, `internal/agent/prompt_test.go`

## What this document is

The operator's ruling, restated as a rule a future agent can apply:

> A rule can be true of ONE role, of EVERY engineering discipline, or of ONE
> machine — and each of the three belongs somewhere else.

This is the *why* behind a habit that had until now been practice without a
name: the kernel system prompt must stay generic, project-specific material
belongs in memory or in the project's own `GOPI.md`, and the two must never be
confused because the confusion is invisible until it has already happened.

## The three layers

| | Layer | What decides membership | Where it lives | What it costs |
|---|---|---|---|---|
| 1 | **Role-inherent** (角色固有) | it is true because of *what the agent is*; needs no argument, no evidence, no exception | the kernel prompt sections — `internal/agent/loop.go`'s `promptSections` | kernel budget on **every turn** |
| 2 | **Abstract principle** (抽象原则) | it is a *method*, true of every engineering discipline, that anyone who hit the originating failure could re-derive | the kernel prompt, in the block that owns its subject | kernel budget, **earned** by timeliness |
| 3 | **Concrete material** (具体素材) | it names an *artifact*: a path, a URL, a version, a repo, a person, a number from one incident | **memory** (`memory(action=store)`); a repo doc or `GOPI.md` for the project-bound part | nothing — retrieved when true |

### Layer 1 — role-inherent

Examples already in the kernel: `Use tool_use blocks` (`headerRules`), `lead
with the result` (`efficiencyRules`), `No recaps; lead with the tool call`
(`conciseThinkingRules`), `never claim to have seen ... something you have not`
(`chatSystemPrompt`).

The distinguishing property is that **justifying one is a category error**: "why
should the agent lead with the result?" has no answer beyond "because it is an
agent answering a human". If a sentence *needs* a justification to be obeyed, it
is not this layer.

Layer 1 must also be **short**, and for the same reason it needs no
justification: this is the layer that costs kernel budget on every single turn,
with no retrieval gate to prevent the spending.

### Layer 2 — abstract principle

The seven pre-existing `[ENGINEERING RULES]` (from the text2img case-study) are
all this layer: `CREATE within the constraint FIRST`, `5-why root cause first`,
`verify the SMALLEST sample`, `every bug fix ships a minimal repro test`,
`ZERO-DEP default`. Each was *derived* — learned from a specific failure — and
each names a **method**, never an artifact. Rule 8 (added by this task) joins
them: `别重复造轮子: search prior art before you build (industry, then repo) and
reuse it — 复用不重做。`

### Layer 3 — concrete material

The path of a sibling agent project to read for prior art. A mirror URL for a
blocked API. "Our library is #907". A pinned tool version. An operator's own
working directory.

All of these are **true and useful** — that is exactly why they are dangerous.
They are meaningless to a future project, they go stale without anyone editing
them, and they spend budget that every unrelated task pays.

## The move, when something feels like it belongs in the prompt

Apply in this order — the cheap layers absorb the expensive ones:

1. **Does it name an artifact** (a path, a repo, a URL, a name, a number from one
   incident)? → **Layer 3, memory.** Stop here. This is the answer most of the
   time, and stopping here is the intended outcome.
2. **Is it a method, true of every engineering discipline, that you could
   re-derive from the failure it came from?** → **Layer 2, kernel prompt.**
3. **Is it merely what the agent IS, needing no justification?** → **Layer 1.**

## Why the division is enforced, not merely preferred

**Layer 3 in the kernel is already a build failure.**
`TestSystemPromptNoGoPiSpecifics` fails on project-specific wording in the
kernel — `devdoc/`, `tasks/`, `go build ./`, an internal case-study name such as
`text2img` — because a future project without this one's document layout must
still receive a self-sufficient prompt. **That gate is this division made
mechanical.**

It is also *partial*, and knowing its blind spot is part of applying the rule: a
path belonging to a **different** project's layout is invisible to it (nothing
inside one repo can see another repo's conventions). That is why the move above
must be a habit, and why `industry_practice_test.go` carries its own layer-3
guard (`TestIndustryPracticeRulesCarryNoProjectMaterial`) for the class of
material — paths, home references, URL schemes — rather than for one instance.

**And layer 2 cannot simply be demoted to memory either.** Memory is
*retrieved*: the routing block says "start each task with `memory(action=search)`",
which fires when the task looks familiar. A principle parked there is therefore
absent on exactly the tasks that had not yet noticed they needed it. "Look for
prior art before building" is worth nothing to a model that has *already decided
to build* — it has to be present **before** that decision, on the turn the
decision is made. That timeliness, not the principle's importance, is what buys
it kernel space.

## The rule this task added, as the worked example

Rule 8 is the division applied to a real request. The operator asked (2026-09-29)
for the principle "don't rebuild what already exists / look at how the industry
does it" to reach the prompt **generally**, and supplied a concrete example: a
sibling agent harness already present on this machine, in a directory of the
operator's own.

Applying the move:

- the **example** (a path on this machine) → layer 3 → **memory**, and it is
  deliberately *not* named in the kernel — `TestIndustryPracticeRulesCarryNoProjectMaterial`
  fails the build if anyone folds it back in;
- the **method** (search prior art — industry, then repo — before you build) →
  layer 2 → `[ENGINEERING RULES]` rule 8;
- the **wording** ("before building", "reuse it") needs no justification → it is
  not layer 1, it is a derived habit, which is why it took a *number in the
  existing list* rather than a section of its own.

## Where each limb renders (the two prompt paths)

The principle reaches four surfaces; the **shared clause has exactly one
definition** (`industryPracticeClause`), and the numbered form is composed from
it (`industryPracticeRule8 = industryPracticeRule8Number + industryPracticeClause`).

| Path | Carrier | Limb |
|---|---|---|
| task kernel | `[ENGINEERING RULES]` rule 8 | affirmative — the tool routing block is already on this path |
| chat, no tools (`assembleChatSystemPrompt`) | `industryPracticeRules(false)` | **honest** — says it cannot search, and forbids the false prior-art claim |
| chat + ops (`chatOpsSystemPrompt`) | `industryPracticeRules(true)` | affirmative — names `keyword_search`/`code_search`/`web_search`/`web_fetch` from `internal/toolnames` |
| worker task prompt | inherits the kernel entry | affirmative |

The two limbs are `Rule 15`'s honesty requirement, and they follow the existing
precedent in `chat_context.go`: `chatNoToolsLimb`/`chatToolsLimb`,
`chatMemoryTimingRules(toolsOn)`, `chatWriteBoundaryRules(toolsOn)`. The reason
is stated by the chat block's own header — **a FALSE instruction is the worst
kind, because it is followed** — so the plain path (no tools on the wire) is told
what is true of it rather than handed the other path's instruction.

## Where rule 8's source is recorded

A rule without its origin becomes folklore. Rule 8's provenance — the operator's
2026-09-29 ruling, the worked example, and the reason the example stayed out —
lives in the header comment of `internal/agent/industry_practice.go`, beside the
constant it explains, so the next reader does not have to reconstruct it from the
git log.

## The defect this task had to repair first (measured)

The rule needed ~112 characters of kernel space it did not have, and the space
was accounted wrongly. Measured on base `81ce8fe4`, with
`GOPI_OUTPUT_LANGUAGE=zh-CN` in force:

| quantity | chars |
|---|---|
| previous gate's measurement (`len(assembleSystemPrompt())`) | **4789** |
| the operator's reply-language directive + separator | 292 |
| the kernel it claimed to be measuring | **4495** |
| headroom the kernel actually had | **5** |

The gate said "the assembled prompt (identity + all discipline blocks)" and
measured the composed string — which INCLUDES the directive. So on master the
gate was **RED for every operator who configured a reply language**, green for
every operator who had not: the same tree, a different verdict, decided by an
environment variable the kernel does not control.

**The repair changes WHAT THE GATE MEASURES — it does not relax it.** 4500 still
bounds the kernel, and a new assertion holds the directive PRESENT in the
composed prompt (the fix would otherwise be satisfiable by dropping it). The
kernel is now a first-class function (`systemPromptKernel`,
`industry_practice.go`), so every kernel-only assertion measures a name instead
of a string it must remember to strip: deriving the kernel by subtraction had
made three pre-existing registry tests (`TestAssembleSystemPrompt*Order`,
`…EmptySectionDropped`, `…StableOrder`) compare the COMPOSED prompt against a
kernel-only expectation — green only on machines with no configured language.

**Where the 112 characters came from: duplication, not dilution.** The session
budget was stated twice — once in the tick line (`clockRuleBase`) and once in
task rule 5 (`sessionBudgetRule`) — at a cost of 126 characters of pure
repetition. The two halves now split ONE rule: the tick line carries the clock
and what the ceiling means, rule 5 carries the remedy and the escalation mark.
Two further cuts were pure duplication as well (`Call code_search FIRST` inside a
bullet already named `code_search:`; `…the transcript of what you said` → the
bullet's own subject). **No guarded sentence was weakened**: three strings this
task initially trimmed turned out to be asserted anchors
(`not for reading files or searching code`, `sed/head/tail = byte-precise
extraction INSIDE a compound command`, `not bash git blame`,
`Call code_search FIRST with the bare identifier`,
`start each task with memory(action=search)`) and every one was restored.

**Measured after:** kernel **4488** / 4500 (headroom 12), rule 8 = 112 chars.

## A second discipline this task ran into: loop.go is an anchor target

`TestDocAnchorsResolveInNormativeDocs` resolves `devdoc/*.md` line references
into the code. `internal/agent/loop.go` carries many of them, so **inserting
lines in loop.go re-keys every anchor below the insertion**: a first attempt that
grew the file by 13 lines produced 16 newly-failing anchors, whose message
prescribes re-keying ("the doc names a line that master's refactor moved; the
record is a landed record whose anchors this change deliberately does NOT
rewrite — fix = re-key by symbol").

The repo's own precedent is the cheaper discipline: commit `7a887af5` states
"keep loop.go line-count neutral so no doc anchor drifts". This task ended at
**`internal/agent/loop.go` delta 0** and **zero new anchor entries** (pristine master's 179
entries, unchanged), by (a) keeping new prose in *new files* — the authority
comment, the strings, the kernel accessor and all new tests live in
`industry_practice.go` / `industry_practice_test.go` /
`prompt_density_budget_test.go` — and (b) taking the prompt characters it needed
from INSIDE existing lines rather than by adding lines.

**Honest residual:** that gate is **already RED on master** (179 pre-existing
entries: `assert-the-bug-audit-20260917.md`, `assertion-totality-gate-master-red.md`
and others citing files master has since refactored, plus a wiped `GOPI_TASK_NOTIFY`
fixture whose "expected [data] field missing (path: data)" entries also pre-exist).
This task neither created nor repaired them: it verified against a pristine
`81ce8fe4` worktree that the entry SET is unchanged, and does not rewrite landed
records (the gate's own instruction).

## Related

- `devdoc/system-prompt-review-2026-09-07.md` — the review that produced the
  current block structure and the "one sentence + one counter-example" discipline.
- `devdoc/text2img.md` — the source of rules 1–7.
- `GOPI.md` — Rule 5 (report), Rule 37 (a chat does not commit), and the project
  material that layer 3 would otherwise leak into the kernel.
- `CONFIG.md` — the `output_language` knob whose directive interacts with the
  density budget (see `systemPromptDensityBudget` in
  `prompt_density_budget_test.go`).
