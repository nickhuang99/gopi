<!-- doc-class: normative -->
# The chat memory-timing rule — the operator's instruction IS the trigger

> **Provenance.** `TestDocAnchorsResolveInNormativeDocs` was retired on 2026-10-22 in `5a35f61` (deleted with `internal/docclass`), so a `file.go:NNN` reference in this page is a landing-time record, not an anchor a live gate enforces — nothing resolves one today. The line numbers stay useful to a reader; they are not checked by the suite. Citations below are quoted as they were written — where one of them reads as present tense ("is RED", "resolves", "fails"), read it as the state of THAT run, never as a gate this tree still has.

**Task**: `01a0e46a-1442-7261-8c11-bd3dd4660907` · **Branch**:
`task/01a0e46a144272618c11bd3dd4660907` · **Date**: 2026-09-28

## The gap this closes

Three behaviour rules belong to `gopi chat` and NOT to the shared worker prompt:
the write boundary, the surface/self-observation block, and memory's STORE TIMING.
The first two had landed. The third had not — and the way it was missing is worth
recording, because the rule EXISTED, in two surfaces, and neither of them is a
chat prompt:

| surface | file | who reads it |
|---|---|---|
| task-start harness checkpoint | `internal/agent/loop.go:215` | a TASK run |
| the memory tool description | `internal/agent/loop.go:1373` | every run, task and chat alike |

So a chat turn read the tool description's worker half ("USE after a task with a
reusable lesson") and inherited **task-end** semantics for a surface that has no
task end. The event contract itself (`eventDrivenStoreTriggers`) was already the
right contract — it was simply absent from the one block that tells a chat what
it is.

## The landing point, and why it is that one

`chatContextRulesFor(toolsOn bool)` in `internal/agent/chat_context.go` — the
chat-only block assembled by `assembleChatSystemPrompt` (plain path) and
`chatOpsSystemPrompt` (`--tools` path). It is deliberately NOT `promptSections`:
that registry is the TASK prompt, and every background run reads it, so a chat
behaviour rule placed there would tell a worker it is a conversation.

The new paragraph lives in its own file, `internal/agent/chat_memory_timing.go`,
mirroring `chat_write_boundary.go`. Two reasons for a separate file rather than
more lines in `chat_context.go`:

1. `chat_context.go` is already 382 lines of block assembly; a second paragraph
   with its own limbs and its own tests reads better beside it than inside it.
2. `loop.go` — the other candidate — is a **doc-anchor target**: a dozen landed
   docs carry `loop.go:NNNN` anchors and `TestDocAnchorsResolveInNormativeDocs`
   reddens one entry per inserted line. Keeping the paragraph out of `loop.go`
   keeps that gate's line budget neutral.

## The two limbs (a false instruction is followed)

The precedent is `chatNoToolsLimb`/`chatToolsLimb`, and it applies here for the
same reason. The plain path carries **no tools on the wire**: it cannot call
`memory(action=store)` at all, so "store it NOW" would teach an invocation with
no execution. Forking on the same `toolsOn` bool gives each path only what is
true of it:

- **plain (no tools)** — the instruction to remember is one you HAND OVER: say
  what is worth keeping, and *never claim a memory was stored*.
- **`--tools`** — memory IS on the wire (`chatOpsToolDefs` = the ordinary
  surface): call it as part of THIS turn, and never claim a memory you did not
  write.

## The shared half is the ONE definition, not a re-typed copy

The trigger list is `eventDrivenStoreTriggers` verbatim (Rule 1/13/14). That
constant already renders into the memory tool description and the task-start
checkpoint, so rendering it a third time into the chat block is what keeps the
three surfaces from telling different LLMs different trigger sets. The action
spelling comes from `toolnames.ToolMemory` / `toolnames.ToolMemoryActionStore`.

The paragraph also carries the CONTRAST that keeps the two behaviour modes apart
(`chatMemoryWorkerContrast`): *"That is NOT a task run's timing: there is no task
end here to defer to."* Without it the paragraph still reads as a plausible store
trigger while silently reinstating the worker's timing — which was the defect.

## What the harness already does per turn (and why the prompt rule is not redundant)

The `--tools` path carries a chat-only per-turn store checkpoint
(`maybeChatOpsStoreCheckpoint`, `chat_ops.go`), keyed on the SAME event: it fires
when a turn *"has done real work with tools and has not stored anything yet"*.
That is a NUDGE, bounded by construction to at most one per turn, and it fires at
the END of a turn. The prompt rule fires EARLIER and FINER — the moment the
operator speaks — and a turn that stores on that moment disarms the checkpoint
through its own `memoryStored` latch. The two describe one contract; the prompt
sentence is the one that can act on the trigger.

## Adoption evidence (Rule 11) — what was measured, and what was NOT claimed

Rule 11 requires LLM adoption proof for prompt-only work. Two measurements, both
from real runs against a **private sandbox profile** (`gopi sandbox create`;
`GOPI_HOME` pointed into it, built binary):

1. **The paragraph reaches the wire.** A local SSE stub recorded the emitted
   request body. The system prompt the `--tools` chat path actually sent carried
   `[CHAT MEMORY TIMING — the operator's instruction is your trigger]`, the verbatim
   event list, the worker contrast, and the TOOLS limb (18 tools on the wire).
   The plain path sent the same paragraph with the NO-TOOLS limb (0 tools). A
   worker run sent neither the paragraph nor any chat block (17 tools).
2. **The trigger produces the call.** A real `chat --once --tools` turn whose
   prompt said *记住这个* issued `memory(action="store")` — the session JSONL shows
   the tool_use, and the answer opened with the stored id.

**The honest limit of claim 2**: hippocampus is default-OFF
(`GOPI_HIPPOCAMPUS` unset ⇒ `a.Hippocampus == nil` ⇒ `maybeChatOpsStoreCheckpoint`
is a no-op), so in that run the per-turn checkpoint COULD NOT have fired — no host
was built. The store can therefore only have come from the prompt text.
**But "prompt text alone caused it" is not assertable from this run**, because the
run also carried the memory tool description — which is the shared surface whose
worker wording this task did not change. The A/B that would isolate this
paragraph (same turn, paragraph present vs absent, checkpoint off either way)
would need a build of the PREVIOUS prompt, and was not run inside the session
budget. So the claim this task makes is: **the paragraph is emitted, and a real
turn with the trigger in front of it stored** — not "the paragraph alone did it".
That is a prompt-leg non-claim by the write-boundary task's own standard, stated
here rather than glossed.

**How much of the gap the paragraph actually closes DEPENDS ON THE LIMB, and the
captures measure it.** On the `--tools` path the memory tool IS on the wire (18
tools) and its description already carries the event list — but framed for a task
run ("USE after a task with a reusable lesson", "task end is the floor"). What the
paragraph adds there is the CONTRAST, plus the instruction to act on the trigger
in THIS turn. On the PLAIN path the request carries **zero tools** and therefore
**no tool description at all**: before this change that path was told NOTHING
about memory, and after it the paragraph is the only memory statement the model
receives. The gap was widest exactly where the fix is strongest.

**Mechanism leg**: none, and none is claimed. This rule is prompt-only by
construction — there is nothing to enforce server-side about WHEN a model decides
to call a tool, and inventing a gate for it would fire on turns that legitimately
have nothing to store.

## The correction: the chat block refuses the worker's deadline (a21af405)

The first landing (77ab266d) rendered `eventDrivenStoreTriggers` verbatim — and
that constant ends with the worker's `(task end is the floor, not the ceiling)`.
So the chat block said, in ONE paragraph, both *"there is no task end here to
defer to"* **and** *"task end is the floor"*. The second is FALSE on this surface
and it is exactly the failure the limb precedent warns about: a false instruction
is the worst kind because it is followed. The paragraph's own contract honoured
here is Rule 15 — a missing instruction is survivable, a wrong one is not.

The repair splits the constant by READER, not by wording:

| definition | true of | rendered by |
|---|---|---|
| `eventDrivenStoreEventList` | every surface that can store | chat block + both task surfaces |
| `eventDrivenStoreTaskEndClause` | a task run only | the two task surfaces only |
| `eventDrivenStoreTriggers` | = list + " " + clause (**byte-identical**) | loop.go checkpoint, memory tool description |

Nothing was deleted from the worker's contract and no rendering drifted:
`TestEventDrivenStoreTriggers_SurviveInBothSurfaces` (untouched) still passes,
which is the proof that the split is a split and not an edit. The chat block
gained exactly one capability — to cite the EVENTS while refusing the DEADLINE.

## Rule 11 adoption — the pair that actually isolates the change

The first adoption run (`--tools`) proved the paragraph reaches the model but NOT
that it changed anything: the unchanged memory **tool description** was on the
same wire and carries the full contract, so a store that turn is not attributable
to the paragraph.

The isolating pair is the **plain** path, where the paragraph is the ONLY memory
text on the wire (0 tools, tool descriptions not sent). Same prompt — an operator
saying 记住 — to a control binary built from `master` (no paragraph) and to this
branch's binary:

| | control (master) | this branch |
|---|---|---|
| first thinking | "What are my behavior rules here? I'm in chat mode…" then reasons from the **plain** tool description's task framing | "Per my behavior rules: the operator's instruction is the trigger, and it fires at once. I cannot call memory(action=store) myself — this path carries no tools. So I should … hand the operator the lesson to save, and never claim a memory was stored." |
| answer | the lesson must go through `task submit`; "它同样只活到会话结束，我不装作它被记下来了" | quotes the limb, hands the lesson over, and closes "**我还没有记住它。**" |

Both binaries behaved honestly in the sense that neither CLAIMED a store on the
plain path — but only the branch's turn was told what its trigger IS (the
operator's instruction, firing at once) and what to do with the lesson (hand it
over). The control had no rule about the operator's instruction at all; it
reached `task submit` from the worker-facing tool text. **Claimed**: the paragraph
is the only memory text on that wire and the branch's turn cites it verbatim.
**Not claimed**: that the two answers' outcomes differ in a way one run can
establish; both paths correctly refused to fabricate a store, and the observable
difference is the REASONING, from one run per arm.

## Rule 35 — no new dependency

The paragraph is composed at assembly time from constants that already exist. No
tool, script, cron line, daemon, state file or config key was added.

## What the delivery-point sync dragged in: a PRE-EXISTING master red
Merging master at close-out (`90417833`) turned the epic's own acceptance
selection red — `TestDocAnchorsResolveInNormativeDocs`, which the criteria name,
failed with `0 new, 2 stale permits`. The red was **master's, not the branch's**:
proved by checking master's tip out detached in a throwaway worktree
(`9aba625b`) and reading the identical failure there, and by
`git diff HEAD master` over the three files that decide the verdict
(`docanchor_test.go`, `testdata/docanchor-baseline.tsv`, the normative doc)
printing nothing.

Both stale rows are ONE site — `devdoc/cli-ui-feasibility-study.md` →
`internal/lineedit/editor.go:745`, plus its bare `editor.go:745` continuation
spelling — and they went stale the same way. Earlier repair attempts (ledger rows
`01a0e582`, `01a0e63e`, `01a0e5f8`, all `completed`) recorded the drift as a
permit; a permit that matches nothing is itself an error, so the allow-list could
never converge. The gate's message already carries the remedy — *"the site was
fixed or is no longer gated; delete the entry"* — and the allow-list's whole point
is that it **ratchets down**: the fix is two lines deleted (`72351e9a`, 285 → 283
rows), red observed before and green after.

The lesson worth keeping: **a "pre-existing red on master" whose remedy is an
ADDITION (a permit) and not a REMOVAL (the stale row) is a red that will recur**.
The permit list is a ratchet; the retry loop that only ever appends is what kept
this one alive across four attempts.

## Verification

See the task report at
`~/.gopi/sessions/01a0e46a-1442-7261-8c11-bd3dd4660907/task-report.md`:
`go build ./...` clean; `go test ./internal/agent/` green; the branch-scoped
gates (`ChatMemory`, `ChatPromptsCarryThe`, `ChatWriteBoundary`, `DocAnchors`,
`EventDrivenStoreTriggers`) green; both emitted-prompt captures and the
store-producing turn recorded above.

## Scope note: how the two runs touched the operator's own profile

The Rule 11 runs used `GOPI_HOME=<sandbox>` and reached the API through the
sandbox settings symlink — but `--tools` runs DO execute tools with real side
effects, and two of them called `memory(action=store)` in this session's
operator-visible profile (`~/.gopi/.gopi/memory.db`, which is append-only: fixture
rows are permanent). Both rows are genuinely useful lessons and were left in
place deliberately rather than silently removed; they are recorded here so a later
reader of that ledger knows where they came from. The no-tools control turns
wrote nothing of the kind — which is itself the limb working as specified.

**Recipe for the next agent**: for adoption evidence on a chat path, prefer the
**no-tools** arm. It executes no tool, so it cannot write to a real ledger, and it
is the only arm where the paragraph is the sole memory instruction on the wire —
the isolating arm and the safe one are the same arm.
