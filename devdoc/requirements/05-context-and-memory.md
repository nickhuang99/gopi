<!-- doc-class: historical -->
# Requirements 05 — Context & Memory

**Module B5 of the reverse-requirements series (dispatched as item `[6/15]` of the umbrella's
15-item worklist — see the id-scheme note below).** Reverse analysis: the built system is the
evidence; the inference of the user need follows from it; the value of this document is the
gap (§6), not the inference.

| | |
|---|---|
| **Branch / worktree** | `docs/req-05-context-memory-repair` @ `~/workspace/gopi-req-b5-fix` (Rule 6a/23 — a fresh branch from master, never a sibling's). The original pass's branch `feat/req-b5-context-memory` @ `~/workspace/gopi-req-b5` **no longer exists on this host**; this pass read the merged document from master and repaired it in place, per memory #3808 (never overwrite a sibling's merged doc wholesale). |
| **Baseline read (original pass)** | master `17cec86b`, 2026-09-16 |
| **Baseline read + re-measurement (this pass)** | master **`812f9a60`** ("Merge branch 'feat/cwd-lifetime-recovery'"), host **`peerbox`**, 2026-09-18. Every number in §6 was re-measured here; values that moved carry both readings. All `file:line` anchors were re-read at `812f9a60` and resolve (§Verification). |
| **Deliverable** | this file, and nothing else (`git diff --name-only 812f9a60..HEAD` → one path) |
| **Verification** | docs-only: no code, no test, no `make fmt` (Rule 5/7); the project's own gate for a docs branch is the single-file diff, recorded in §Verification at the end of this file |
| **Measured** | live reads of `~/.gopi/memory.db` + `~/.gopi/sessions/**/*.jsonl`, 2026-09-18 (§6.5 holds the queries and their output; transcript in `~/.gopi/task-sessions/_evid-req05/`) |
| **Overlap** | `01a0ab3e-02ef-7540-921e-baa5159087e8` — the `B5/12` series' row (the umbrella's §7.2 map lists it as `B5`, `completed`). Its document **is** this file (`ebad73a5` on master), so per memory #3808 this pass repaired the merged document rather than writing a second one. |
| **This document's own dispatch row** | `01a0ab12-38ae-7436-9a5c-de86b136d2c4` — series `[6/15]`, `source=llm`, status `in_progress`. Its payload is the module brief, and it is the **live origin of the citation defects repaired here** (§4 W3 quotes its payload; see the front-matter note). Read the distinction carefully when following a citation: **the brief is this series' row, the merged document is the other series' row** — the original pass conflated the two, which is exactly defect 1 below. The umbrella `00-method-and-standards.md` was read for the standard (§4.1 front matter, §4.2 template, §7.1 X1–X5, §7.3 the front-matter/repair rule) and **not** edited. |

**Repair pass, 2026-09-18 — what this pass changed, and what it deliberately did not.**
Changed: the front matter (branch/worktree, both revisions, the umbrella-required keyed boundary
block, the X1/X2 restatement), §4 W1/W3/W5 (three citation defects — below), a new W8 (the
operator row that decides §6.4.1), every number in §6.1–§6.5, §6.3.1–§6.3.3, §6.4.1 (which moves
from *never decided* to *decided*), §7 (four missing test families), the C1–C6 check (C6 was
absent), and a new §Verification. **Not changed:** the document's structure, its argument, its
§1–§3 reconstruction of the need, and every finding that survived re-measurement — the sibling's
reading of the need stands. Superseded numbers are **kept and marked `[superseded]`**, not
deleted (memory #3808: repair what fails, leave the weak resolutions visibly marked).

**Three citation defects found in the original pass, all repaired in place. All three are the same
shape — a row cited as operator evidence whose `source` column does not support it — which is why
this pass re-checked the `source` of every row §4 quotes:**

1. §4 W3 quoted "an agent run has a finite attention budget and must not re-derive …" as "the
   briefing for this module" and attributed it to row `01a0ab3e-02ef`. That row contains the string
   "attention" **nowhere** (`select instr(payload,'attention') …` → `0`) — it is the *other*
   series' (`B5/12`) row. The sentence is in `01a0ab12-38ae-7436-9a5c-de86b136d2c4`, i.e. **this
   module task's own dispatch row** (`source=llm`, `[6/15]`, status `in_progress`). **No
   `source=human` row carries it at all** (`payload like '%attention budget%'` → 1 row, the `llm`
   one). The need is real, but its authority was overstated: it is a *briefing's* paraphrase, i.e.
   `[inferred]`. §4 W3 now says so; §4 W8 supplies the operator's actual words on this module.
2. §4 W5 named row `01a0abd1-157a-…` the operator's hand-pasted context. It is `source=llm`
   (`select source …` → `llm`): it is *the agent's own* briefing channel, not the operator's —
   which weakens the "the operator works around the store by hand" reading of §6.4.3.
3. §4 W1 called its two rows "two independently reported defects". The second
   (`01a0a986-6687-…`) is `source=llm` — an agent-filed follow-up to the first, not a second
   operator report. The operator reported **one** in-run defect; the remaining half of it was
   found by the agent. §4 W1, §6.6 and §8 now say so. **This is the same defect shape as (1):
   an `llm`-authored row cited as if it carried operator authority** — which is why this pass
   re-checked the `source` column of **every** row quoted in §4 rather than only the two the
   original pass flagged.

**Id-scheme note (umbrella §7.2 — two schemes are live).** This document is **`B5`** in the
`B1…B12` scheme and item **`[6/15]`** in the dispatch series that produced it. Both are correct;
they are the umbrella's two numbering schemes, not a contradiction.

## Boundary

```
Boundary:      the agent remembers — two different budgets on the same memory: the
               thread of the run it is in (in-run context management), and what it
               learned across runs (the durable distilled store + the organ that
               reads it).
In scope:      internal/hippocampus/*.go (the organ: engine, brief, decision, host,
               config, types — 6 production files, 1,154 lines); internal/tools/memory*.go
               (16 production files — the tool FACE: search/store/update/feedback, the
               brew pipeline, procedural workflows, provenance; memory.go alone 3,519
               lines at 812f9a60); internal/agent/compact.go + internal/agent/context.go
               (in-run compaction, the instruction anchor, the trim); the in-run working
               area that survives compaction (internal/agent/loop.go:4412, :5508, :5546);
               the memory tool as the LLM sees it (internal/agent/loop.go:1051) and the
               operator's surface onto the store (cmd/agent/cli_memory.go, cli_brew.go);
               and ~/.gopi/memory.db's SCHEMA as a durable contract (tables, columns,
               indexes, tiers).
Out of scope:  node→node knowledge-base sync (memory_sync*.go, cmd/agent/cli_memory_sync.go)
               — that is B4/12 (devdoc/requirements/04-sync-and-nodes.md); tool routing
               rules and tool NAMES as a taxonomy — B7/12; the tool COUNT itself
               (GOPI.md Rule 3, 17 tools) — forbidden ground for this series; code,
               tests and formatting changes of any kind (documents-only module).
Boundary why:  this is a user-visible responsibility and not a directory, because the
               need ("do not lose the thread within a run" / "do not re-derive across
               runs") is stateable without naming any package, and the implementation
               is split across THREE homes that no single path contains — internal/tools
               (the face), internal/hippocampus (the organ), internal/agent (the in-run
               machinery). Matching the directory would produce three documents for one
               need. The mismatches that make this explicit are X1/X2 and X4 below.
```

**Boundary why — the two umbrella mismatches (X1/X2, umbrella §7.1) that made the directory
reading wrong.** The umbrella renamed its mismatch ids `M<n>` → `X<n>` after this document was
written; the ids below are the umbrella's current ones (the original pass, like three sibling
documents, used `M1`/`M2` for them — umbrella §7.4 item 3 records that collision).

**X1 — a file-level doc comment names a package that does not exist.**
`internal/tools/memory_brew.go:1` begins `// Package memory — brew pipeline: distill reusable
lessons …` while the same file's package clause is `package tools`
(`grep -n '^package' internal/tools/memory_brew.go` → `13:package tools`); the sibling memory
files use the honest form (`internal/tools/memory_sync.go:1` `// Package tools — memory_sync.go:
node→node knowledge-base sync (M4).`). `internal/tools` holds **109** production `.go` files at
`812f9a60` (`find internal/tools -maxdepth 1 -name '*.go' ! -name '*_test.go' | wc -l` → `109`;
**95** at `17cec86b`). *[superseded: the original pass wrote "291 .go files" from
`ls internal/tools/*.go | wc -l`, which counts test files too; 291 is the count of ALL `.go`
files at `17cec86b`, and the umbrella §7.1 X1 row itself corrects an earlier version of the same
mistake.]* The doc comment claims a package identity that every other file in the directory does
not share — residue of this code having once been its own package. Consequence for this document:
the "memory subsystem" has no single package boundary to cite, so the boundary is drawn by role
(organ / tool face / context machinery), which is what the block above does.

**X2 — one user-visible need is served by two code faces that never share a package.**
The tool face (`internal/tools/memory*.go` — 16 production files; `memory.go` alone **3,519**
lines at `812f9a60`, 3,516 at `17cec86b` — *[superseded: "3,516 lines"]*) and the organ
(`internal/hippocampus/`, 6 production files, **1,154** lines at both revisions) implement the
*same* user-visible capability — "the agent remembers". They are separate packages, they compose
the same store, and the split is historical rather than functional. Measured consequence: the
organ's host is **off by default** (`internal/agent/hippocampus_config.go:13` — "GOPI_HIPPOCAMPUS
unset/off ⇒ NewAgent builds NO host, every hook below is …"), so on a default host *all* live
memory behaviour comes from the tool face and none from the organ. A boundary that names both
must therefore say which one is load-bearing — this document does (§6.1).

**X4 — the CLI is a *surface*, not a module — restated here because this document names two
CLI files as in-scope.** `cmd/agent/cli_memory.go` and `cmd/agent/cli_brew.go` appear in the
`In scope:` list above, and they are *not* a "CLI module": they are the operator's **surface onto
this module's store**, one `cli_*.go` verb-dispatcher file per verb family inside a flat
`cmd/agent` package (**45** non-test `cmd/agent/cli_*.go` files, **80** production files in that
directory at `812f9a60`; the umbrella's §7.1 X4 row measured **45 + 33 = 78** at `9ce8e159` —
the difference is four non-CLI files added between those revisions, not a contradiction). The
verbs belong to the module whose need they expose, so `gopi memory …` is this module's surface
and `gopi memory sync …` (`cli_memory_sync.go`) is B4's (`devdoc/requirements/04-sync-and-nodes.md`)
— the same store, two modules, two documents. Evidence: the shared verb registry is
`cmd/agent/cli_verbs.go:6` ("cliVerbs is the single source of truth for the TOP-LEVEL gopi CLI
verb words"), and the per-verb files only shape its arguments. Consequence for this document:
neither file is *in* the boundary; both are cited as the operator's contact point for it
(§2's operator row, §6.4.3).

## 1. Purpose (product-owner language)

A coding agent run is short-lived and its attention is finite. Two things therefore have to be
true, and they are different things:

1. **Within a run, the thread must not be lost.** A long conversation eventually stops fitting.
   When that happens the agent must keep working: the user's *current instruction* must survive
   the squeeze, the earlier work must be summarised rather than dropped into silence, and the
   conclusions the agent has already reached (and the small list of what it is doing right now)
   must still be visible afterwards. A run that forgets its own instruction mid-flight, or that
   endlessly re-summarises itself without freeing anything, is the failure this half exists to
   prevent.

2. **Across runs, the agent must not re-derive what it already learned.** Yesterday's hard-won
   procedure — the build gate, the trap in a test suite, the reason a branch was abandoned —
   should be available today in seconds, not rediscovered at the price of the same investigation
   twice. This is the "five minutes instead of re-inventing the wheel" promise, and it is the
   reason a durable store exists at all.

The two halves are deliberately kept distinct because their budgets differ: the first spends
tokens *inside* a run and is judged by whether the run finishes, the second spends tokens
*across* runs and is judged by whether future runs get shorter.

## 2. Actors served

| Actor | What they get | Evidence |
|---|---|---|
| **The agent itself (the LLM in a run)** | The only real consumer. It reads the start-of-task reminder, the injected knowledge digest, the injected compaction summary, and it is the only writer of lessons and of quality votes. | The tool description is written as instructions to the model (`internal/agent/loop.go:1049`…); the compaction summary is injected as a user message (`internal/agent/compact.go:298`). |
| **The operator** | Reads and reverses everything: the store is one file, every write has a CLI, feedback is recomputable from an append-only event table. | `gopi memory update --tier …` (`cmd/agent/cli_memory.go:42`); `memory_feedback --suggest-cold` (`:64`); `FeedbackRecompute` rebuilds the counters from events (`internal/tools/memory.go:3160`–`3176`). |
| **The next session** | Inherits the distilled rows and the committed conclusions of its predecessor — the actor the "do not re-derive" half is *for*. | `session_conclusions` (**5,366** rows live at 2026-09-18; 589 on 2026-09-16 — *[superseded]*); the harness feeds committed conclusions back into the next item (`internal/agent/loop.go:5508`). |
| **The run's own briefing channel** | A *competing, manual* channel: a run pastes "what is already known — do not re-derive" into its own next briefing instead of consulting the store. **Note whose channel this is** — the row's `source` is `llm`, so it is the *agent* doing the pasting, not the operator (§4 W5, corrected). | Ledger row `01a0abd1-157a-7e74-92e4-a562f42453b5` opens `CONTEXT (what is already known — do not re-derive):` (verbatim, quoted in §4); `select source …` → `llm`. |

The honest asymmetry: the store's *writers* are the agent, its *inspectors* are the operator,
and its *beneficiary* is a future run that usually exists on the same machine but in a
different process, day and task.

## 3. End-to-end workflow

**A. Entry points**

1. *Start of a run* — the harness injects a start-of-task reminder to consult the knowledge
   base, plus a digest of the currently resident lessons, as user messages that are also
   written to the session log (`internal/agent/loop.go:3618`–`3640`).
2. *Stuck / done-before / mid-task* — the agent calls the merged knowledge tool with one of four
   actions (`search` / `store` / `update` / `feedback`), read-only `search` being parallel-safe
   and the three writers serial (`internal/agent/loop.go:1051`).
3. *Mid-run context pressure* — when the estimated conversation size crosses the trigger
   fraction of the window the agent compacts: the oldest messages are replaced by a generated
   summary block, keeping a pinned instruction anchor
   (`internal/agent/compact.go:552`–`566`; trigger `0.75` of `200000` tokens,
   `internal/agent/context.go:19`, `:33`).
4. *Operator, any time* — the CLI reads, edits, exports, recomputes, and sweeps
   (`cmd/agent/cli_memory.go`, `cmd/agent/cli_brew.go`).
5. *Session exit, at most once per 24 h* — the brew sweep distils fresh sessions into lessons
   (`cmd/agent/cli_brew.go:150`, interval guarded by `~/.gopi/brew_state.json`).

**B. What happens inside**

- **Within-run**: size is estimated each round; over the trigger, the oldest messages are handed
  to a summariser whose output replaces them, prefixed by a reference-only marker
  (`internal/agent/compact.go:298`, `:314`). A mechanically extracted anchor section preserves
  the *exact* current instruction (`anchorHeading`, `:137`, budget `:141`). If the anchor cannot
  be freed, compaction is **pinned**: it refuses to loop, warns, and backs off
  (`compactPinnedAnchorWarning`, `:91`, `warnPinnedAnchor` `:94`, called `:599`, `:654`).
  Separately, the harness keeps a small working area of per-item conclusions and re-injects it
  after compaction, trimming what is already committed (`internal/agent/loop.go:5508`, `:5546`).
- **Across-run**: the tool face scores candidate rows (query-token coverage first, then tier,
  then staleness), returns hits with value signals the model is asked to judge, records the
  verdict as an event, and recomputes materialised counters from those events. Rows can be
  demoted so they stop being first-tier answers (`internal/tools/memory.go:1410`–`1481`; the
  contract at `:1476`–`1479` is *demotion*, and the only deletion path is the explicit
  single-record `gopi memory delete`, `:2889`). In parallel, brew slices session logs, asks a
  three-condition judge whether a chunk holds a reusable lesson, and writes surviving lessons
  tagged `brew` so a later refill never re-ingests its own output
  (`internal/tools/memory_brew.go:1`–`12`, `memory_sync.go:419`).
- **The organ** composes the same store as a bounded retrieve → judge → act cycle
  (`internal/hippocampus/engine.go:13`–`16`) with a flat one-verb decision contract
  (`internal/hippocampus/decision.go:6`–`28`).

**C. Exit conditions**

- A search returns ≤ `limit` (default 5) rows, cold rows only as a last-resort bucket when no
  warm/hot row matches (`internal/tools/memory.go:1476`–`1481`).
- A compaction ends when the estimate is ≤ the target (0.35 of the window,
  `internal/agent/compact.go:40`) — or refuses, with a warning and a cooldown, when pinned.
- Brew ends per session batch, with a pre-touched state file so a crash cannot loop-retry
  (`cmd/agent/cli_brew.go:141`).
- An operator edit ends with the row's fields rewritten and the change logged.

## 4. Why (with citations)

Every claim below is cited to a ledger row, a file:line at `17cec86b`, a commit SHA, or a
GOPI.md rule; anything else is marked `[opinion]`.

**W1 — "Do not lose the thread" was demanded by the operator as a bug, twice.**

> `01a0a93c-2add-704f-aea8-f27652432f92` (verbatim, entire payload):
> "clearly this is a bug when reaching memory limit which is running in a cycle. fix this."

That row became the livelock fix `903c0d5c` ("fix(agent): stop the compaction livelock at the
context limit", 2026-09-16) — and its follow-up row found the *trigger* still wrong:

> `01a0a986-6687-741f-8396-08697191b59a` (`source=llm` — an agent-filed follow-up finding to W1,
> not a second operator report; verbatim): "compaction: anchors jam against the head —
> follow-up user messages should not be the 'last user' stop floor"
> … "WHAT IS NOT FIXED (this task): the TRIGGER. Measured on the reporter's session c2eff914: 149× …"

Landed as `55a4fc2b` ("fix(compact): a harness follow-up must not be the instruction anchor",
2026-09-16). So: **one** operator-reported defect (W1's first row, the livelock) plus **one**
agent-filed follow-up that found the trigger still wrong, both in the same half — in-run context
management is not a nice-to-have, it is the half that has actually failed in production.
*[corrected in this pass: the original pass called these "two independently reported defects";
the second row's `source` is `llm`, so the operator reported one and the agent's follow-up found
the remainder. §6.6 and §8 quote W1/W2 as the operator rows accordingly.]*

**W2 — "Do not explode the backend context" is a stated daemon requirement.**

> `01a0974f-7bff-742f-b09b-7116c89f39fe` (verbatim): "when gopi is running in deamon mode such as
> merger and worker, it should prevent its context from exploding in backend LLM. i.e. when
> worker starts a new task, it should make sure its context window is fresh new without any
> context leaking from previous task … the ideal method is just start a new g[opi process]"

This is the *policy* half of the boundary: per-task isolation is the operator's chosen shape,
and compaction is the second line for a single long run.

**W3 — "Do not re-derive" is the module brief's framing — `[inferred]`, and the original pass
mis-attributed it.**
The sentence "an agent run has a finite attention budget and must not re-derive, each session,
what previous sessions learned; therefore context is compacted and distilled lessons persist
across sessions" exists in exactly ONE row of the ledger: `01a0ab12-38ae-7436-9a5c-de86b136d2c4`
(`select task_id,source … where payload like '%attention budget%'` → one row, `source=llm`,
status `in_progress`). *[superseded: the original pass attributed it to `01a0ab3e-02ef-…` — that
row contains the string "attention" nowhere (`select instr(payload,'attention') …` → `0`).]*
**No `source=human` row carries it.** So W3 is evidence of what the *briefing* asserts about the
need — and note *which* briefing: `01a0ab12-38ae` is this document's own dispatch row (the
`[6/15]` series), not the sibling `B5/12` series whose merged document this file is. **A dispatch
row describing the module it dispatches is the weakest possible citation for that module's need**
— it is upstream of the very analysis being cited. Under umbrella §8.6 (quote, don't characterise)
and §6.1 (`source=human` is the primary canon) it must be labelled `[inferred]`. The inference's
weakest link: the briefing is itself an LLM-authored task payload, so it is a *restatement* of the
need whose operator origin is not in the ledger. The need still stands — §4 W1, W2 and W8 are
operator rows — but its authority comes from those, not from this sentence.

**W4 — the value rule that governs the whole store: a tool's worth is that the LLM calls it.**

> GOPI.md Rule 11 (`GOPI.md:318`): "任何工具开发/修改 … 必须以'LLM 真实调用该工具'为验收标准" and its
> mechanism note (`GOPI.md:~329`): "工具的价值 = LLM 真的用它 … git_blame 0 次/21435、code_search
> 0.3%——工具存在但没被说服 = 白写".

Applied here (§6.5 measures it live): the family is adopted — of the **892 sessions begun after**
the tool-family merge `0e172c07`, **887** contain a knowledge-base call and in **879** of those
it is the first tool call recorded in the file. *[superseded: the original pass wrote 68 of 135
(50.4 %) — the denominator was the sessions *on disk at that moment* on its host, not the
sessions begun after the merge; see §6.5's re-measurement note.]* Rule 11 is therefore
*satisfied for this family*, unlike the rule's own motivating examples.

**W5 — the store is meant to be consulted before hard work, not after — but the row is the
AGENT's own channel, not the operator's.** *[partly superseded]*

> `01a0abd1-157a-7e74-92e4-a562f42453b5` (verbatim opening line): "CONTEXT (what is already
> known — do not re-derive):"

Note what this row is: *the run* preventing re-derivation by hand, pasting prior knowledge into
its briefing. `select source …` → **`llm`** — so it is the agent's own briefing channel.
*[superseded: the original pass read this as the operator pasting context by hand; the row's
`source` field says otherwise.]* The need it evidences is unchanged (a run that opens by
carrying prior knowledge forward), but it is evidence about *delivery shape*, not about the
operator, and §6.4.3's reading is weakened accordingly.

**W6 — the mechanism has a written purpose that the code contradicts (see §6.3).**
The tool description the model reads states: "≥2 useless votes auto-demote to cold (reversible)"
(`internal/agent/loop.go:1055` at `812f9a60`, verbatim). No automatic demotion exists in
production code: the only writer of the tier column is the explicit operator edit path
(`internal/tools/memory.go:2849` at `812f9a60` — `:2846` at `17cec86b` — `sets = append(sets,
"tier=?")`), and the cold-surfacing scan is a *suggestion* list by construction
(`internal/tools/memory.go:3301`; the function is `FeedbackSuggestCold` at `:3283` at `17cec86b`
and `:3286` at `812f9a60` — the cited `:3301` was this query's cold-filter line, which is `:3304`
at `812f9a60`)
whose SQL filters `useless_count >= fbColdSuggestThreshold` (`:263` → `3.0`) — and the operator
surface says so in words (`cmd/agent/cli_memory.go:64`, "as SUGGESTIONS only — demotion is
executed manually").

**W7 — one tool face over four verbs was a deliberate decision, not drift.**
`ab097ca1` ("feat(memory-tool): fold memory_search/store/update/feedback into one `memory` tool
with action param …", 2026-09-10) merged into master at `0e172c07` ("resolve merge feat/memory-tool
into master — tool-family merge complete (task/job/memory single action-param tools, 17 tools
total)", 2026-09-10 07:03 +0800), which is also the Rule 3 tool-count anchor.

**W8 — the operator DID decide "consultation is enforced policy, not model volition" — and
§6.4.1 of the original pass said nobody had.** *[new in this pass; the finding it refutes is kept
at §6.4.1]*

> `01a08d9a-649a-7193-8440-0d6a389bcda4` — `source=human`, status `completed`, 2026-09-10 23:15,
> summary *"Make 'consult memory first' a non-skippable harness step (nick: 知道要去查=主动性根本，
> 不许写'agent 应该记得查')"*. Payload, quoting the operator verbatim and in the original
> language:

> 「复用你的意思，'存了不查等于白存'，重要的是不仅存而且能够查，
> 更重要的是**知道要去查来用，这个是'主动性'的根本的体现**。」
> …
> 「所以**不许写"agent 应该记得查记忆"**——那和"agent 应该不硬编码"是同一类空话」
> … 三层失效: 「记得住（存储）→ 找得到（检索）→ 想得起来要用（自觉调用）… 想不起来要查 =
> **白有**」 … 硬约束: 「3. **不许"依赖 LLM 自觉"**——机制必须是 harness 强制的。」

This is the operator's own three-legged model of the need (store → retrieve → *remember to
retrieve*) plus a hard constraint that the third leg must be mechanism, not exhortation. Every
enforcement device the original pass found and could only *infer* a purpose for is this row's
implementation: the task-start checkpoint (`internal/agent/loop.go:3618`
`maybeInjectTaskStartSearch`, "forcing the loop to begin with a search **WITHOUT needing LLM
volition**"), the task-end completion store (`internal/agent/loop.go:160`
`completionCheckpointMessage`, injected by `:3658` `maybeEnforceCompletionStore`, "对称强制 with
the task-end"), the review-time consult question
(`internal/agent/loop.go:186` `consultCheckpointMarker`, candidate C of this very row's
design — the row lists candidates A/B/C and orders "先做 C + A"), and the A/B escape hatch
(`internal/agent/loop.go:1679` `NoEnforceMemory`). **§6.4.1's "never decided" verdict was
wrong** and is corrected there; the consequence for the C1–C6 check is that the enforcement
branch is *asked for* by the operator, i.e. C3-legitimate rather than an added hazard.

**[opinion]** The tension inside the store is older than either half: it was designed to be
*scored* (tiers, confidence, decay) before anyone measured whether scoring changes what the model
does. §6.5's numbers are the first honest look.

## 5. Acceptance in user terms

A run is correct with respect to this module when, from the outside:

1. **The instruction survives.** In a run long enough to compact several times, the agent's final
   actions still serve the user's latest instruction, and the log shows the instruction was
   preserved verbatim in the compaction anchor.
2. **Compaction always ends.** No run loops forever re-summarising. Either the size genuinely
   drops, or the system stops, says so, and continues with a cheaper trim. (Acceptance is
   observable in the session log: a warning + backoff, never an unbounded summariser loop.)
3. **Nothing is lost silently.** Earlier conversation consumed by compaction is represented by a
   reference-only summary block, and the agent's own conclusions are re-injected after it.
4. **A known answer is cheaper than a new investigation.** A task that repeats a previously
   solved problem retrieves the prior lesson instead of rediscovering it — measurable as: the
   retrieval counter increments for the matching row in the same session.
5. **A wrong answer can be retired.** A lesson that a later run contradicts can be corrected, and
   the correction is visible on subsequent reads; nothing has to be *deleted* to achieve this.
6. **Everything is inspectable and reversible by one person.** One file, one CLI, one recompute
   command; no background daemon, no service to keep alive.
7. **The operator is not asked to trust a score.** The operator can list the votes, list the
   demotion candidates, and see why a row is where it is.

The user does **not** ask for: a smarter embedding model, a bigger store, autonomous learning
without review, or deletion policies. Nothing in the evidence requests them.

*[Reconciliation added in this pass — the sentence above is about EVIDENCE and it stays true, but
it is not the whole answer, and leaving the tension unnamed is how a §5 and a §6 contradict each
other in the same document.]* C4 ("cleanup with a named bound") is an **operator constraint from
the umbrella standard**, not a request found in the ledger — so the correct statement is: *the
evidence requests no deletion policy, and the operator's own constraint list requires a bound on
anything that accumulates.* §6.2.1/§6.4.2 file exactly that gap, and it is a constraint-driven
finding rather than an evidence-driven one. Under umbrella §8.1 ("never invent a requirement to
fill a section") the acceptance list above is correct to omit it; under C4 the gap is real.

## 6. Gap analysis — code against need

### 6.1 Serves

- **The across-run need is served and *used*.** Live store (`~/.gopi/memory.db`, re-measured
  2026-09-18 at master `812f9a60`): **3,774 distilled rows** (1,848 on 2026-09-16 —
  *[superseded]*), **44,441 retrieval events**, **2,942 rows (77.9 %) have been returned to a
  model at least once**. Under Rule 11 that is the verdict that matters, and it is positive: this
  is not a knowledge base nobody retrieves from. (Both readings land at ~77–78 % "ever retrieved"
  — the ratio is stable even though the store doubled in two days.)
- **The in-run need is served defensively**, not just hopefully: the trigger/target split
  (0.75 trigger, 0.35 target, `internal/agent/context.go:33`, `internal/agent/compact.go:40`),
  the pinned-anchor refusal (`internal/agent/compact.go:91`–`109`), the cooldown
  (`:44`), and the turn-scoped harness-message exemption (`internal/agent/context.go:67`,
  `:466`) are all direct answers to real defects, each with a regression test (§7).
- **The working area genuinely survives compaction**: committed item conclusions are re-injected
  and trimmed (`internal/agent/loop.go:5508`, `:5546`), so "the thread" survives even where the
  conversation text does not.
- **The operator asked for the enforcement, and the enforcement is what ships** (§4 W8): the
  three-legged model (store → retrieve → *remember to retrieve*) is implemented as three
  harness checkpoints, each with a test that asserts it fires exactly once (§7).
- **Reversibility is real**: `memory_feedback` is an append-only event table (**3,281** events
  over **1,191** distinct rows at 2026-09-18; 1,376 / 646 on 2026-09-16 — *[superseded]*), and
  the materialised counters are a pure function of it — `FeedbackRecompute` zeroes and rebuilds
  (`internal/tools/memory.go:3172`, the `UPDATE … SET useful_count=0, useless_count=0` at
  `:3175`). Cross-checked live: **exactly 1,130 rows carry non-zero counters and exactly 1,130
  distinct rows carry a useful/useless vote — no drift** (312/312 on 2026-09-16 — *[superseded]*).
  The mechanism is honest about itself.
- **The need the operator named as "白有" (stored but not remembered) now has a mechanism**: the
  review-time consult checkpoint (`internal/agent/loop.go:186`) exists *because* row
  `01a08d9a` chose candidate C of its own three designs ("先做 C + A"), and the checkpoint's
  message is deliberately framed as a measurement, not a work order
  (`internal/agent/memconsult_test.go:117` `TestConsultCheckpointMessage_IsMeasurementNotWorkOrder`).

### 6.2 Falls short

1. **The store has no retention bound — C4 is unmet, not merely unpolished.** Nothing prunes. A
   *single-record* delete exists (`gopi memory delete <id>` → `MemoryDelete`,
   `internal/tools/memory.go:2889`, surface `cmd/agent/cli_memory.go:49`, `:856`) — and the schema
   even wires it correctly (AFTER DELETE triggers clean the FTS/trigram indexes, foreign DBs cleaned
   explicitly) — so this is not "deletion was never thought of". What is missing is any **bound**:
   no constant, CLI verb, comment or scheduled job names a size limit or a discard rule for rows
   nobody deletes; and the closest thing to a written policy is a *demotion* contract rather than a
   *deletion* bound (`internal/tools/memory.go:1476`–`1479`: `// Tier-1 results = warm/hot only.
   Cold is the last-resort bucket: it only surfaces when NO warm/hot row matches at all — the "no
   longer 1st tier to return" contract for maintenance-demoted rows.`). Live: **3,774 rows
   accumulated in 25 days** (distilled_at range 2026-08-24
   02:17 → 2026-09-18 11:12; 1,848 rows / 23 days on 2026-09-16 — *[superseded]*), of which
   **832 rows (22.0 %) have never been retrieved** (421 / 22.8 % — *[superseded]*). The growth
   rate is **~2.0 rows/day over the whole window, and the never-retrieved SHARE is essentially
   flat (22.8 % → 22.0 %)** — so this is not a store filling with garbage; it is a store with no
   *bound*. [inferred — weakest link: growth observed over one 25-day window on one host; the
   flat ratio is a two-point observation, not a trend.] With no bound and no refusal behaviour,
   C4 ("cleanup with a named bound") is a filed conflict, and §6.4.2 is the never-written
   decision behind it.
2. **The 5W1H/exam apparatus is half-fed, and it has stopped growing.** **1,072 of 3,774 rows
   (28.4 %)** carry `exam_questions` (1,072 of 1,848 = 58 % on 2026-09-16 — *[superseded]*); the
   per-column counts show the apparatus was never uniform —
   `kw_what` 1,072, `kw_why` 963, `kw_where` 711, `kw_who` 698, **`kw_when` 230**, and only
   **133** rows carry all five labels (the original pass's "all five labels or none" is refuted by
   the column counts — it is a gradient, not a step). The decisive measurement: **every one of the
   last 399 rows distilled (after 2026-09-16 23:59) carries an EMPTY `exam_questions`** — new
   ingestion writes none. Those columns exist to make "alternate phrasings" retrievable
   (`internal/tools/memory.go:196`–`208`, `:1746`), so the newer two-thirds is indexed the *less*
   phrasally-rich way. This is a *content* gap produced by a *schema* decision, and it is
   currently widening.
3. **The brew channel produces very little, and its output is a rounding error of the store.**
   Live: **6 rows of 3,774** carry a brew-shaped source tag (`sources like '%brew%'` — and only
   two of the six say `brew` outright; the other four are free-text provenance strings that merely
   contain the word), brew last ran **2026-09-18 13:00** (`~/.gopi/brew_state.json`). 5 of 1,848 /
   last run 2026-09-16 17:32 on the original pass — *[superseded]*. The pipeline is well-tested
   (§7) but at this yield it is not the mechanism that fills the store: between the two passes the
   store grew **1,848 → 3,774 rows (+1,926 in two days)** while the brew-tagged population moved
   5 → 6 — i.e. the interactive store action carried essentially all of it. A tool whose adoption is
   structural (the model calls it) rather than output-based is a distinction the roadmap never
   had to make.
4. **The organ is dormant on a default host while duplicating a live path.** The tool face already
   performs a bounded retrieve→judge with tokens/tier/confidence signals; the organ performs a
   bounded retrieve→judge→act on the same store, behind a switch that defaults off
   (`internal/agent/hippocampus_config.go:13`, `:24`). [inferred — weakest link: whether the
   operator intends the organ to *supersede* the tool's search path or to coexist as a background
   proactivity layer; both are defensible readings of the six-file organ and this document cannot
   settle it from code alone. **Partially narrowed in this pass**: §4 W8 shows the operator
   *chose* harness-enforced consultation over invited consultation, which makes "coexist as a
   background layer" the likelier reading of the two — but that is a reading, not a decision.]

### 6.3 Over-builds

1. **The resident ("hot tier") injection path is nearly dead, and the prompt's claim about it is
   conditional on a store that has almost never existed.** The start-of-task digest injects the
   **3** most recent hot rows (`internal/tools/memory.go:321` `hotResidentInjectLimit = 3`,
   `:322` `hotResidentMaxChars = 600`; digest built by `:3407` `MemoryHotResident`, called at
   `internal/agent/loop.go:3629`), and the accompanying harness message tells the model "these
   distilled lessons are currently tier=hot — consult them directly when relevant"
   (`internal/agent/loop.go:3655`). *[superseded line numbers: the original pass cited
   `memory.go:318`/`:3420`; the constants sit at `:321`/`:322` and the function at `:3407` at
   `812f9a60`.]*

   **Live re-measurement, and this REFUTES the original pass's headline:** there is **1 hot row**
   (id `1892`, distilled 2026-09-11 03:58, 6 retrievals) out of **3,774** — not zero out of 1,848
   as the original pass measured. *[superseded: "there are ZERO hot rows in 1,848 … the digest is
   always empty and the model is told a condition that is false".]* The digest is **not** dead
   code: the injection string `Hot-tier resident candidates` appears as a genuine `user` message
   (excluding occurrences quoted inside a tool's own output) in **736 of 3,134 session files**
   (737 injections — one session resumed once), i.e. the resident-injection branch has executed
   hundreds of times. What survives of the finding is narrower and still a defect:
   the hot tier is *polluted by accident* — a single row drifted to hot and has stayed there for
   7 days — so the digest, when it fires, offers the model a candidate that no one chose, while
   the harness message asserts "currently tier=hot" as if residency meant quality. The over-build
   is not "the path never fires"; it is "**residency is claimed as a quality signal by a tier
   nothing maintains**".
2. **Tier scoring moves almost nothing observable — and the demotion cohort is not the one the
   prompt describes.** Cold rows are demoted out of the default result set — the ranker partitions
   them into a last-resort bucket that surfaces only when *no* warm/hot row matches at all
   (`internal/tools/memory.go:1466`–`1481`: `if hitsByID[id].Tier == coldTier { cold = append(cold, id) }`
   … `ordered := warmHot; if len(ordered) == 0 { ordered = cold }`), and the tool description says
   the same in the model's words ("cold-tier rows excluded", `internal/agent/loop.go:1055`).
   *[superseded line numbers: the original pass cited `:1465`–`:1473`.]* Yet the 122 cold rows carry
   **1,988 retrieval events
   between them** (1,397 on 2026-09-16 — *[superseded: the same counter grew; `last_retrieved` is
   a single timestamp, not a history, so the delta is real activity, not a re-count]*), and the
   cohort maximum is **503** — a cold row (`#390`) that is the **most-retrieved row in the entire
   store**. *[superseded: the original pass reported the two most-retrieved rows as "319 and 317
   events"; re-measured, the top-3 most-retrieved rows are warm `#849` 589, warm `#848` 577, warm
   `#862` 564, and the top cold row is `#390` at 503. The old 319/317 pair does not exist at
   `812f9a60` — no row has exactly those counts.]* So the ranking inversion the original pass
   *suspected* (cold rows among the most-retrieved) is now **established and stronger**: the
   single most-consulted row in the store sits in the bucket the ranker demotes. [inferred —
   weakest link: `last_retrieved` is one timestamp, so the schema still cannot prove the 1,988
   events were counted *after* demotion.]
3. **Cold's documented cause does not match cold's actual composition — and the original pass's
   supporting detail was wrong.** All 122 cold rows have `useless_count = 0`, and **no production
   code path writes the tier column automatically** (the sole writer is
   `internal/tools/memory.go:2849` at `812f9a60` / `:2846` at `17cec86b`, the explicit
   `MemoryUpdate` edit path; `--suggest-cold` only prints advice, `:3286` `FeedbackSuggestCold`,
   filter `useless_count >= fbColdSuggestThreshold` = `3.0` at `:263`). *[superseded: the original
   pass reported "42 of the 122 do carry a single post-hoc `unused` vote". That is false at
   re-measurement: **0 of the 122 cold rows carry ANY feedback event at all**
   (`select count(*) … where tier='cold' and exists(select 1 from memory_feedback f where
   f.memory_id=d.id)` → `0`).]* The cohort is also confirmed as a single day's artefact: **all 122
   cold rows were distilled on 2026-08-24** (the store's first day), alongside 542 warm rows from
   the same day — so this is a seeding/maintenance decision applied to one day's batch, not a
   graded outcome. (Demotion without deletion is still a sound C2/C4 design; the over-build is the
   *automation that was documented but not built*.)
4. **Procedural memory is inert, and it grew 15× while staying inert.** **60** rows in the
   `procedural` table (4 on 2026-09-16 — *[superseded]*), **total `use_count` = 0** — not one
   workflow has ever been executed, all 60 never used. A workflow memory nothing executes is a
   stored value nobody reads — Rule 11's exact failure mode, in miniature, at 15× the scale the
   original pass saw. It is also B4's sync surface, so this document reports rather than judges it.

### 6.4 Decided (was "Never decided") and still open

**6.4.1 is WITHDRAWN — the decision exists, and the original pass said nobody had made it.**

1. ~~**Forced consultation vs. invited consultation — never decided.**~~ **REFUTED.** Ledger row
   `01a08d9a-649a-7193-8440-0d6a389bcda4` (`source=human`, status `completed`, 2026-09-10)
   *is* the decision: "不许'依赖 LLM 自觉'——机制必须是 harness 强制的" (§4 W8 quotes it in
   full), and its four hard constraints ("不新增工具" / "必须有开关，默认保守" / "不许依赖 LLM
   自觉" / "注入命中率差就报告不做") map one-to-one onto what shipped: the one-shot latches
   (`maybeInjectTaskStartSearch` `internal/agent/loop.go:3618`, `maybeEnforceCompletionStore`
   `:3658`) rather than new tools; the escape hatches (`NoEnforceMemory` `:1679`,
   `NoEnforceMemoryConsult` `:1689`) as the switch; enforcement rather than exhortation; and the
   consult checkpoint shipped as *measurement* whose message forbids spawning work
   (`internal/agent/memconsult_test.go:117`). *[superseded: the original pass read the escape
   hatches as evidence "the operator's answer is 'policy, but switchable' — this is the reading I
   would adopt, but **no row states it**". A row states it.]* The live question that remains is
   narrower and is **not** answered by that row: candidate A's hit-rate gate ("若候选 A 的注入
   命中率实测很差（比如 <50% 有用），明确报告"不做"") has no evidence attached — the row
   *demands* the number; this document does not find it reported anywhere.
2. **Retention.** The *mechanism* to discard exists — `gopi memory delete <id>`
   (`internal/tools/memory.go:2889` `MemoryDelete`, `cmd/agent/cli_memory.go:49`) — but no row,
   constant, or scheduled job states *when* a lesson should be discarded, i.e. the **policy** is
   the missing decision, not the verb. The store's own shape (append-only feedback, a cold tier
   that means "demote" rather than "discard", provenance columns) reads as a deliberate "keep
   everything", but a keep-everything policy with no size bound is a decision nobody wrote down;
   §6.2.1 is its cost, and re-measurement (22.8 % → 22.0 % never retrieved while the store
   doubled) says the cost is *latent*, not acute. *[corrected in this pass: the original pass
   wrote "no row, constant, or CLI verb states when a lesson should be discarded", which
   overstated the absence — the verb exists; the policy does not.]*
3. **Who owns the briefing channel.** §4 W5's row is the *agent* carrying prior knowledge into its
   own briefing by hand (`source=llm`) — not, as the original pass read it, the operator doing so.
   Whether the task briefing should *automatically* carry retrieved lessons — i.e. whether the
   store should feed the queue's briefings rather than only the agent's tool calls — is still not
   decided anywhere. *[opinion]* This remains the highest-leverage undecided item in the module,
   because it is the one channel with a guaranteed reader — but its supporting evidence is weaker
   than the original pass claimed, and it now also has a partial answer in the shipped
   task-start checkpoint (`loop.go:3618` injects a *search instruction*, not retrieved lessons;
   the retrieval itself is still the model's move).

### 6.5 Quantitative claims, re-measured live

All figures below were produced by reading `~/.gopi/memory.db` and `~/.gopi/sessions/**/*.jsonl`
in the session that wrote this file — the original pass on **2026-09-16** at master `17cec86b`,
this repair pass on **2026-09-18** at master `812f9a60` on host `peerbox`. The **Measured** column
carries the `812f9a60` value, with the `2026-09-16` value after it marked *[superseded]* where it
moved. Re-measurement note (original pass): its adoption counts drifted by one call between two
runs because the measuring session itself calls the merged tool — that is a real property of this
measurement, and this pass therefore reports a **median call count, not a fixed one** (see the
last rows).

| Claim | Measured | How |
|---|---|---|
| Distilled rows | **3,774** *(1,848)* | `select count(*) from distilled_qa` |
| Retrieval events | **44,441** *(19,716)* | `sum(retrieval_count)` |
| Rows ever retrieved | **2,942 (77.9 %)** *(1,427 / 77.2 %)* | `retrieval_count > 0` |
| Rows never retrieved | **832 (22.0 %)** *(421 / 22.8 %)* | the complement |
| Tiers | cold 122 / warm 3,651 / **hot 1** *(cold 122 / warm 1,727 / hot 0)* | `group by tier` |
| Cold rows with useless votes | **0 of 122** | `useless_count = 0` on every cold row |
| Cold rows with ANY feedback event | **0 of 122** | `exists(select 1 from memory_feedback f where f.memory_id=d.id)` |
| Cold cohort's distillation date | **all 122 on 2026-08-24** | `date(distilled_at,'unixepoch') group by 1` |
| Cold rows' retrieval events | **1,988** *(1,397)* | `sum(retrieval_count) where tier='cold'` |
| Most-retrieved row in the store | **warm `#849` 589**; top cold is `#390` at **503** *(no row has 319/317)* | `order by retrieval_count desc limit 5` |
| Feedback events | **3,281** — useful 2,504 / useless 549 / unused 228 *(1,376 — useful 623 / useless 315 / unused 438)* | `memory_feedback group by rating` |
| Distinct rows carrying a vote | **1,191** *(646)* | `count(distinct memory_id)` |
| Rows with counters vs voted rows | **1,130 vs 1,130 — no drift** *(312 vs 312)* | two counts, identical |
| Procedural rows | **60**, `sum(use_count) = 0` *(4, `sum = 0`)* | `procedural` |
| Brew-authored rows | **6 of 3,774** *(5 of 1,848)* | `sources like '%brew%'` |
| Rows carrying `exam_questions` | **1,072 of 3,774 (28.4 %)** *(1,072 of 1,848 = 58 %)* | non-empty `exam_questions` |
| Rows carrying all five `kw_*` labels | **133** (per-column: what 1,072 / why 963 / where 711 / who 698 / **when 230**) | `exam_questions` + `kw_*` counts |
| Newest 399 rows (after 2026-09-16) with `exam_questions` | **0 of 399** | `distilled_at > '2026-09-16 23:59:59'` |
| Ledger rows consulted by the harness | `session_conclusions` **5,366** *(589)* | `select count(*) from session_conclusions` |
| LLM adoption, all time | **1,539** session files contain a knowledge-base call | JSONL `tool_use` scan |
| LLM adoption, legacy wire names | **3,328 calls, 0 AFTER** the merge commit | split on the call's own `timestamp` vs `0e172c07` (2026-09-10T07:03:01+0800) |
| LLM adoption, merged tool | **6,543 calls, 0 BEFORE** that commit | same split |
| Sessions begun after the merge | **892 files; 887 (99.4 %) call it, and in 879 of those it is the file's FIRST tool call** *(135 files; 68 = 50.4 %; all 68 first)* | per-file `tool_use` order + the call's `timestamp` |
| Repeat use within a session | **511 of those 887 contain ≥6 calls**; median 6, max 38 *(48 of 68)* | per-file call count |
| In-run trimming | `context_trim` **2,866 calls** *(2,334)* | JSONL scan (vs `job` 18,721 / `task` 12,141 — the original pass's `job` 1,501 / `task` 1,116 counted only a fragment of the tree) |

**Two measurement corrections this pass had to make, both worth naming.**

0. **The store is a moving target, and this document's own reads move it.** Re-reading
   `select count(*) from distilled_qa` minutes after the table above was written returned **3,775**
   (one row more), because the session doing the measuring also stores lessons — exactly the
   instrument-effect the original pass hit in its adoption counts (which drifted by one call
   between two runs). Every store figure in this document is therefore a **reading with a
   timestamp**, not a constant; the table above is the `2026-09-18` reading, and a reviewer
   re-running it should expect ±1–2 rows. This is why the numbers that *decide* findings
   (23 % never retrieved, 1 hot row, 0 of 122 cold rows with votes, 0 of the newest rows with
   `exam_questions`) are quoted as **ratios and zeros**, not as absolutes.

1. **File mtime is not session time.** The original pass split sessions at the merge commit using
   the session files' mtime, which gives **1,261 pre / 1,873 post** (re-measured at
   `2026-09-18T19:2x`, total 3,134 — and it *moves as sessions are written*, which is the first
   reason not to use it) — and, worse, silently
   re-labels a session whenever anything touches the file. Splitting on **each call's own
   `timestamp` field** (present in every JSONL line of this logger; `unparsed_ts = 0` across
   13,417 tool calls) is the honest instrument, and it is what produced the post-merge row above.
   The two methods agree on the *substance* (the legacy wire names never appear after
   `0e172c07`) — but they disagree on the denominator (1,873 by mtime vs **892 sessions begun
   after the merge** by first-call timestamp), which is exactly the kind of number that decides
   whether a claim reads "half" or "essentially all".
2. **"Post-merge" must mean "the session BEGAN after the merge", not "the file was written
   after".** A resumed session's file keeps its original content and gains new lines; classifying
   it by mtime makes a pre-merge session count as post-merge. Classifying by its **first tool
   call's timestamp** is the definition used above.

**Honest reading of the adoption line (Rule 11).** The 879/887 "first tool call" is *not* pure
model appetite: the harness injects the task-start checkpoint
(`internal/agent/loop.go:3618` `maybeInjectTaskStartSearch`, "forcing the loop to begin with a
search **WITHOUT needing LLM volition**"), so the first move is largely engineered — and by §4 W8
that is exactly what the operator asked for. The evidence that the model *keeps* using it — 511
of 887 sessions with ≥6 calls (median 6, max 38), and 78 % of the store ever retrieved — is what
makes the Rule 11 verdict positive rather than an artefact. [inferred — weakest link: JSONL call
counts cannot separate "the model chose" from "the harness nudged", so the strength of the
appetite claim rests on call counts *after* the first.]

### 6.6 The requirement-vs-mechanism verdict on the distilled store

Two statements, deliberately separated:

- **(a) "The agent must not re-derive what it already learned."** This is a **REQUIREMENT**: it is
  the module's stated purpose and it is backed by operator rows — `01a0a93c` (W1, the in-run
  half, operator) and `01a0974f` (W2, the daemon-context policy, operator) — plus `01a0a986`
  (W1's agent-filed follow-up) and `01a08d9a` (W8, "存了不查等于白存 … 想得起来要用" — the
  operator's own statement of this exact need). *[superseded: the
  original pass cited "§4 W3, W5" for this — W3 turns out to be an LLM-authored brief and W5 an
  `llm`-sourced row (§4, citation defects), so neither is operator evidence; the need is
  nonetheless operator-stated, via W1/W2/W8.]* It is *satisfied in practice* — 2,942 rows have
  been handed to a model, and the tool is used at the start of 887 of the 892 sessions begun
  since the tool-family merge. Nothing here is a bet.
- **(b) "Rows are scored useful/useless, decayed, and demoted to cold at ≥2 useless votes."**
  This is **MECHANISM, and a badly-formed justification** — exactly the form the briefing warned
  against. It is unfalsifiable as a need ("the user wants decayed exponential weighting" is not a
  need), it is partially **not implemented** (§6.3.3), and its headline effect is invisible in the
  data (cold rows are among the most-retrieved rows — the store's single most-consulted row, `#390`
  at 503 retrievals, is cold; and the hot tier contains exactly one accidentally-promoted row).
  Its one defensible core is *reversibility*: an append-only event table plus a pure recompute
  makes every quality decision one an operator can reverse — that part is a genuine requirement
  (C2), and it is the part to keep.

**Verdict:** the distilled store is a **requirement met**; the scoring/demotion/residency apparatus
around it is an **implementation bet whose evidence is currently weak** — not worthless (the
event/recompute/reversal core earns its place), but over-sold in the tool's own instructions
(§6.3.1, §6.3.3). A requirement document must record the need (a) and demote (b) to "mechanism,
justified only by reversibility". **This pass re-measured the numbers behind this verdict and it
survives, strengthened**: the "ever-retrieved" share is flat at ~78 % while the store doubled, but
the *residency* half of the apparatus is now measurably worse than the original pass could show
(no hot cohort the design intended; one accidental hot row driving 736 injections of a "these are
hot" claim).

### 6.7 X1/X2 revisited as gaps

- **X1** (originally `M1`) is a naming defect with consequences: the doc comment at
  `internal/tools/memory_brew.go:1` tells a future reader that a `memory` package exists. Anyone
  following it into `internal/tools/` (**109** production files) finds the brew pipeline as a set
  of unexported functions in a 3,519-line-file neighbourhood. Rule 13's spirit (a name that exists
  to be referenced) is defeated by a package name that exists only in prose. *[superseded: the
  original pass wrote "291 files"; see the X1 note in the Boundary section — 291 counts test files
  and was a `17cec86b` number.]*
- **X2** (originally `M2`) is the structural gap behind §6.2.4: one need, two faces, one of them
  dormant. The cost is cognitive, not functional — the next agent asking "where does memory live?"
  gets 109 production files on one side and 6 files on the other, with no rule saying which is
  authoritative.

## 7. Implications for tests (naming existing tests; writing none)

This module is documents-only, so no test is written or changed. What the analysis implies for the
existing suite. **Every test named below was resolved by `grep -rn 'func <Name>(' --include='*_test.go'`
at `812f9a60`; the 32 tests the original pass named all still exist, and this pass adds the four
families it missed — three of which decide this document's own §6 findings.**

**The suite already covers the in-run half well — the defects it answers were each shipped with a
guard:**

- `internal/agent/compact_cycle_test.go` — `TestCompact_NoLivelockAcrossRounds`,
  `TestCompact_PinnedWindowBacksOffInsteadOfLooping`, `TestCompact_SavingsVerdictUsesSameBasis`,
  `TestCompact_LargeSliverWindowStillConverges`, `TestCompact_FieldNumbersStopLooping`
  (the materialisation of §4 W1).
- `internal/agent/compact_pinned_anchor_test.go`, `compact_followup_anchor_test.go`,
  `compact_p0_test.go` / `_p1_` / `_p2_`, `compact_sim_test.go`, `compact_test.go`
  (`TestTrimContext_CompactsWithSummary`, `TestTrimContext_CompactionFallsBackToTrim`,
  `TestTrimContext_NoCallUnderThreshold`).
- `internal/agent/context_test.go` — `TestNewAgent_DefaultContextWindow`,
  `TestTakeOldest_ReturnsOldestInOrder`, `TestTrimToFit_NeverLeavesOrphanToolHead`,
  `TestSummarizeOldToolResults_FoldsOnlyOldLargeResults`; `context_trim_test.go`.
- `internal/agent/writetaskarea_test.go`, `taskarea_review_test.go`, `taskarea_takeover_test.go`
  — the working area that survives compaction.

**The across-run half is covered at the mechanism level:**

- `internal/tools/memory_feedback_test.go` — `TestMemoryFeedbackVotePersists`,
  `TestMemoryFeedbackContradiction`, `TestMemoryFeedbackSessionDedup`,
  `TestMemoryFeedbackRecompute`, `TestMemoryFeedbackRecomputeRecentNoDecay`,
  `TestMemoryFeedbackSuggestCold`, `TestMemoryFeedbackRankingIntegration`,
  `TestMemoryFeedbackLegacySchemaNoColumns`.
- `internal/tools/memory_brew_test.go` — `TestJudgeChunkThreeConditions`,
  `TestDistillLessonValidation`, `TestStoreBrewLessonTagsSourceAndNeverOverwritesNonBrew`,
  `TestRunBrewEndToEndWithStub`, `TestRunBrewResilientToErrors`; plus the call-budget and
  session-accounting tests.
- `internal/tools/memory_procedural_test.go` (`TestProceduralDedup`,
  `TestProceduralSourceTagging`, …), `memory_polish_test.go` (`TestMemorySearchCJKHit` — the
  CJK/trigram recall path §6.2.2 depends on), `memory_m1_test.go` / `memory_m2_test.go`
  (the 5W1H/exam columns), `memory_db_init_regression_test.go`, `memory_schemaatomic_test.go`.
- `internal/hippocampus/*_test.go` — `TestHippocampusEnvHasOneReader`
  (`internal/hippocampus/config_test.go:74`; note the *comment* that names it in production code is
  `internal/agent/hippocampus_config.go:18`, which spells it `TestHippocampus_EnvLiteralSingleReader`
  — a citation trap, not the test's name), `TestLoadConfigEnvOverridesAndFallsBack`,
  `TestDefaultsAreTheDocumentedConstants`, `TestDecisionVerbsAreTheFiveConstants`,
  `TestBriefFromResult_*` (the organ's budget and weight arithmetic).

**Four test families the original pass missed, three of which decide its own gaps:**

1. **`internal/agent/hippocampus_test.go`** — the organ's *behavioural* level ladder, which is the
   only place the organ's semantics are pinned: `TestHippocampus_Lv0_ArchiveOnly`,
   `_Lv1_AppendMemoAsMessage`, `_Lv2_MetaEventIsAMessageNotPromptSection`,
   `_Lv3_QuotaDegradesWithoutSpawningActivations`, `_Lv3_QuotaWindow`,
   `_ConfidenceVetoDegradesToArchive`, `_HardStopEndsInFlightRecall`, `_GateAndKnobs`, and —
   decisive for §6.2.4 — **`TestHippocampus_DefaultOffLeavesBehaviourUnchanged` (`:762`)**. That
   test asserts the organ's default-off path is *byte-identical* to the tool-only behaviour, i.e.
   the codebase itself declares the organ **additive, not load-bearing**. §6.2.4's claim that the
   organ is dormant-by-default is therefore not an inference from the switch alone — it is asserted
   by a test, and §6.2.4's confidence rises accordingly.
2. **`internal/agent/hippocampus_test.go:419` `TestHippocampus_StartOfRunSupersedesTaskStartSearch`**
   — this is the test that decides what §6.4.1 should have been about: it pins the *interaction*
   between the organ's start-of-run recall and the tool-start checkpoint, i.e. the codebase
   already treats "who consults at task start" as a decided, tested ordering rather than an open
   question.
3. **`internal/agent/memconsult_test.go`** — the review-time consult checkpoint's own contract:
   `TestConsultCheckpoint_ArmedFiresOnceAtFirstCommit`, `_UnarmedInjectsNothing`,
   `_DisabledByOwnSwitch`, `_DisabledByNoEnforceMemory`, `_InjectionIsLogged`, and
   **`_MessageIsMeasurementNotWorkOrder` (`:117`)**, which pins the operator's hard constraint 4
   ("never ship a noise generator") as an assertion. The same file holds the adoption meter:
   `TestMemoryAdoption_MeasuresConsumedRows`, `_CarelessSearchIsVisible`,
   `_EmptySearchIsNotAConsumptionFailure`, `_NoSearchLeavesSentinels`, `_SameRowTwiceCountsOnce`,
   `_TextMentionIsNotConsumption`, and `TestMemConsultConfigParity_EnvRegistered` — i.e. a
   `Test...`-level instrument already exists for the §6.5 "does the model actually consume hits"
   question, which is a *stronger* measurement than the call-count proxy this document uses.
4. **`internal/agent/resume_test.go:288` `TestResume_NoTaskStartSearchEnforcement`** *and*
   `internal/tools/memory_test.go` — the store's own behavioural tests, which the original pass
   named only through the feedback file: **`TestMemorySearchColdDemoted` (`:816`, the direct
   regression guard for §6.3.2's ranking inversion), `TestMemoryStoreDedup` (`:288`) and
   `TestMemoryStoreDedupKeyTruncation` (`:321`, §6.2.1's "rows are never duplicated" premise),
   `TestMemorySearchBumpsRetrievalCount` (`:573`, the counter every §6.5 retrieval number is read
   from), `TestMemorySearchLimit` / `TestMemorySearchLegacySchemaReadOnly` /
   `TestMemorySearchForeignFileRejected` / `TestMemorySearchNonBlocking`.** A §6 claim that reads a
   counter must name the test that says the counter is written when and only when it should be.

**Direction the gaps imply** (each is a *candidate*, not a request made by this document):

1. §6.3.1/§6.3.3 — a test asserting that the model-visible instruction text matches implemented
   behaviour: the description's "≥2 useless votes auto-demote to cold" has no code path, and the
   harness message's "currently tier=hot" is asserted for a tier whose only member arrived by
   accident. This remains the highest-value missing guard, because the mismatch is *told to the
   model* — and note the shape this pass's data forces: the guard must assert **the text against
   the writer set** (no code path may claim a tier transition it does not perform), not "hot is
   empty" (which is false intermittently).
2. §6.2.4 — a test (or a devdoc sentence) asserting `GOPI_HIPPOCAMPUS` semantics: *partly answered*
   by `TestHippocampus_DefaultOffLeavesBehaviourUnchanged` (the organ is additive), which leaves the
   narrower question — what does the organ contribute when it IS on — still unnamed by any test.
3. §6.2.3 — a yield assertion for brew: distilled-per-session-batch, so 6 rows across 25 days is
   distinguishable from "working as designed".
4. §6.3.4 — a first-use assertion for procedural rows (60 rows, `use_count` 0 everywhere), or an
   explicit statement that procedural memory is B4's surface and unexercised here.
5. §6.2.2 — the *new* gap this pass measured and the original pass could not see: the 5W1H/exam
   columns receive **nothing** from current ingestion (0 of the newest 399 rows). A test could pin
   that the store path and the ingest path agree on which columns they populate — the schema's
   promise (`TestMemoryStoreKeywordsBreadcrumb`, `internal/tools/memory_test.go:382`) is currently
   exercised only for the interactive path.

**Note on Rule 27:** none of the directions above may be satisfied by generating device load; the
only quantities involved are counts read from the store, not throughput.

## 8. Confidence and unknowns

| Claim | Confidence | Weakest link |
|---|---|---|
| In-run management (compaction + anchor + ceiling) is a **requirement**, evidenced by one operator-reported defect (the livelock) plus an agent-filed follow-up that found the trigger still wrong, and two landed fixes. | high | none material — the ledger rows and commits are unambiguous. |
| Across-run retrieval is a **requirement met, not a bet** — 2,942/3,774 rows retrieved (77.9 %); 887 of the 892 sessions begun after the tool-family merge call it, first move. | medium-high | [inferred] the harness injects the task-start checkpoint (`loop.go:3618`), so "first call" is partly engineered — and by §4 W8 that is the operator's *intent*, not a confound; call counts *after* the first (511/887 with ≥6, median 6) carry the model-appetite claim. |
| The scoring/demotion/residency apparatus is **mechanism over-sold** — the documented ≥2-vote auto-demotion has no code path (`memory.go:2849` is the only tier writer); all 122 cold rows have zero useless votes and zero feedback events of any kind. | high | [inferred] the cold cohort's *origin* (which maintenance action demoted 122 rows, all distilled 2026-08-24) is not recorded in the DB; I verified the absence of any production auto-demoter and the cohort's counters, not the act itself. A maintenance script outside this repo, or a direct SQL statement, could have done it. |
| The **hot-resident** claim is broken in a *narrower* way than the original pass said: the digest does fire (736 sessions), but the tier it advertises has one accidental member. | medium-high (the counts) / high (the mismatch) | [inferred] the *message* the model receives is unconditional while the tier is unmaintained; whether that misleads in practice is not measurable from the store — `TestMemoryAdoption_*` is the instrument that could, and this document did not run it. |
| Retention is **unbounded** and C4 is unmet. | medium-high | [inferred] 25-day window on one host; the "never delete" policy is read from design (cold-not-deleted + provenance), not from a written statement. The mitigating measurement (never-retrieved share 22.8 % → 22.0 % while the store doubled) is a two-point observation, not a trend. |
| §6.4.1 (forced vs invited consultation) is **DECIDED** by operator row `01a08d9a`. | high | none material for the *decision's existence* (the row is `source=human`, `completed`, and states the constraint verbatim). [inferred] only for the *mapping* from the row's four constraints to the shipped devices — that mapping is mine; the row states the constraints, not the implementation. |
| The organ is **dormant by default**; whether it *should* supersede the tool path is open. | high (dormancy) / medium (value judgement) | [inferred] `TestHippocampus_DefaultOffLeavesBehaviourUnchanged` (`internal/agent/hippocampus_test.go:762`) pins the dormancy as a *tested contract*, so the dormancy is not in doubt; whether the operator intends coexistence or supersession is not decided anywhere, so the value judgement on the duplication is mine. |
| Brew is **low-yield** (6 rows of 3,774, two of them tagged `brew` outright) and its purpose is currently carried by the interactive store action. | high (counts) / medium (interpretation) | [inferred] brew may be mid-development; yield over a longer window could differ. Note the tagging itself is loose — four of the six "brew" rows are free-text provenance strings that merely contain the word. |
| The 5W1H/exam columns are **half-fed and now frozen**: 1,072 of 3,774, with **0** of the newest 399 rows carrying them. | high (counts) | [inferred] non-empty = filled; I did not check whether the filled values are *good* phrasings, and I cannot tell from the store whether ingest stopped writing them intentionally or regressed. |

**What is not inferred at all** (verified by reading the cited line, not by reasoning): every
`file:line` in §3–§6 was re-read at `812f9a60` before this pass was committed (§Verification
records the check); all ledger rows quoted in §4 are verbatim payload text except where marked —
**and this pass corrected two rows whose `source` field contradicted the original pass's
characterisation** (W3, W5); the commit SHAs and their subjects were read from `git show -s`.

## C1–C6 check

*(The section title is corrected: the umbrella §5 defines **six** constraints, C1–C6, and the
original pass checked five — C6, the premise of the whole series, was absent. C6's row is new.)*

| | Constraint | Verdict | Evidence |
|---|---|---|---|
| **C1** | Static binary, zero deps, no plugins | **PASS** | The store is SQLite through `modernc.org/sqlite` — pure Go, no CGO, no Python, no subprocess (`internal/tools/memory.go:3`; the db-busy comment at `:77`–`:84` names the concurrency reasoning). The CLI is the same binary. The only external call is the LLM HTTP request brew already needs. No plugin surface. |
| **C2** | Operator observes / reverses everything | **PASS** | One file (`~/.gopi/memory.db`); every mutation has a CLI (`cmd/agent/cli_memory.go:42` update/`--tier`, `:64` `--suggest-cold`); feedback is append-only with a pure recompute (`internal/tools/memory.go:3162` `FeedbackRecompute`, zeroing at `:3175`); injected harness messages are session-logged for audit (`internal/agent/loop.go:3633`, pinned by `internal/agent/memconsult_test.go:140`). Cross-checked live: counters and events agree at **1,130/1,130** *(312/312 — [superseded])*. |
| **C3** | Daemon simplicity — every branch multiplies failure surface | **PASS, with one watched branch, now better understood** | Nothing resident: the organ is off by default (`internal/agent/hippocampus_config.go:13`, `:24`) and brew runs at most once per 24 h, guarded, pre-touching its state file so a crash cannot retry-loop (`cmd/agent/cli_brew.go:141` write-then-run, `:150` `RunBrew`; last run 2026-09-18 13:00 per `~/.gopi/brew_state.json`). Watched branch (unchanged): the auto-brew performs a network LLM call from a background goroutine at session exit — bounded, best-effort, and the one place a memory feature spends money unasked. **New in this pass:** the two *harness* branches (task-start checkpoint, review-time consult) are not daemon branches at all — they are one-shot latches inside a run, they are switched (`NoEnforceMemory`, `NoEnforceMemoryConsult`), and §4 W8 shows the operator asked for exactly this shape ("不许依赖 LLM 自觉"), so under C3 they are *requested* branches rather than added hazards. |
| **C4** | Cleanup with a named bound + refusal behaviour | **CONFLICT — filed in §6.2.1 / §6.4.2** | A refusal and a per-record delete both exist — cold is never *auto*-deleted (outside a search's default result set; `internal/tools/memory.go:1476`–`1479`), and `gopi memory delete <id>` (`MemoryDelete`, `:2889`; surface `cmd/agent/cli_memory.go:856`) removes one row with its index entries — but **no named bound** and no pruning path, so the bound is a human's attention. **3,774 rows in 25 days**, 832 never retrieved. Re-measurement slightly softens the *urgency* without answering the constraint: the never-retrieved share is flat (22.8 % → 22.0 %) while the store doubled. Not silently ignored: recorded here as an unmet constraint rather than papered over. |
| **C5** | One-person solutions; cost + cognitive budget are requirements | **PASS** | Single file, single CLI, one flash call per brew session; the cognitive budget is respected in the in-run half (**2,866** `context_trim` calls — 2,334 [superseded]), explicit anchor budget of 4,000 chars (`internal/agent/compact.go:141`), digest limited to 3 rows / 600 chars (`internal/tools/memory.go:321`–`322`). Recorded cost note: **3,328 + 6,543** knowledge-base calls are a real budget line *(4,728+577 [superseded])*, and §6.2.1's unbounded store is the one place where the *operator's* cognitive budget (reading it) is not yet defended. |
| **C6** | `why` over `what` — the project must be able to say what a feature is FOR | **PASS for the need, CONFLICT for the apparatus (which is precisely this document's §6.6 verdict)** | The product half passes and now has a verbatim operator source: `01a08d9a` states the need as a three-legged model with a purpose ("知道要去查来用，这个是'主动性'的根本的体现"), which is what §1's two halves restate in product-owner language. The mechanism half is C6's failure mode in miniature: the scoring/demotion/residency apparatus ships with a *stated* purpose the code does not implement (the tool description's "≥2 useless votes auto-demote to cold", `internal/agent/loop.go:1055`) — a feature whose written "why" is not its "what". Under C6 that is the defect to file, and §6.3.1/§6.3.3 file it. There is also a narrower C6 finding: the memory tool's own package doc says `// Package memory — brew pipeline` (`internal/tools/memory_brew.go:1`) while the package is `tools` — a *purpose statement attached to an identity that does not exist* (X1). |

## What I could not determine

1. **Whether the organ is meant to supersede the tool's search path or coexist with it.** The
   six-file organ performs a bounded retrieve→judge→act over the same store, but its switch defaults
   off — *and that default-off path being byte-identical is asserted by a test*
   (`TestHippocampus_DefaultOffLeavesBehaviourUnchanged`), which makes "additive" the code's own
   reading. What the organ is FOR when switched on is still not stated anywhere; §6.2.4 records the
   gap and the judgement this document makes instead of a decision nobody wrote.
2. **Who (or what) actually demoted the 122 cold rows**, and on what evidence. All 122 have zero
   useless votes **and zero feedback events of any kind** (re-measured; the original pass's "42
   carry an `unused` vote" is refuted) and they are one cohort — every one of them distilled
   **2026-08-24**, the store's first day, alongside 542 warm rows from the same day. I verified
   that no production code path demotes automatically and that the explicit edit path is the only
   tier writer; a maintenance action performed outside this repository, or a direct SQL statement,
   is invisible in the schema. The `memory_feedback` table does not record it.
3. **Whether the 1,988 retrievals attributed to cold rows happened before or after their demotion.**
   `last_retrieved` is a single timestamp, not a history, so the counter cannot be partitioned.
   §6.3.2 states the observation without claiming the ordering. What *is* now established is the
   rank: cold `#390` at 503 is the most-retrieved row in the store.
4. **Brew's intended yield.** Six rows in the whole store could be "early" or "by design"; the
   module records the measurement, not a verdict on the pipeline's ambition — and four of the six
   are free-text provenance strings that happen to contain the word "brew", so even the *count* is
   the loose bound of the claim.
5. **What "the thread" costs in tokens.** I can name the trigger (0.75 of 200,000) and the target
   (0.35) but the effective token cost of a compaction is only observable per session; I did not
   aggregate it, and no stored counter records it. A quantitative claim here would be a
   fabrication, so it is absent rather than estimated.
6. **Why some event-bearing rows materialise no counters.** The gap **grew** (re-measured: 3,281
   events over 1,191 distinct rows, but only 1,130 rows carry non-zero counters — a 61-row gap;
   1,376/646/312 on 2026-09-16). I verified the *materialised* set is internally consistent
   (1,130/1,130), and I did not publish a speculation about decay weights I had not reproduced.
7. **NEW — whether the 5W1H/exam columns were deliberately retired or silently regressed.** 0 of
   the newest 399 rows carry `exam_questions`; no row, comment, or commit I read states a decision
   to stop writing them. This is the same *shape* of defect as §6.3.1 (a written purpose the code
   no longer fulfils), and it is the one finding in this pass whose cause I cannot even bound.
8. **NEW — whether candidate A's hit-rate gate was ever answered.** Row `01a08d9a` makes it a hard
   constraint ("若候选 A 的注入命中率实测很差（比如 <50% 有用），明确报告'不做'"), and this host's
   ledger contains no row reporting that number. I could not tell whether it was measured on the
   other host and never filed, or never measured. `internal/agent/memconsult_test.go`'s
   `TestMemoryAdoption_*` family is the instrument that would answer it; I did not run it (it
   needs a scratch DB and a session fixture, and this task is documents-only).

## Verification

- **Deliverable:** this file, on branch **`docs/req-05-context-memory-repair`** in worktree
  `~/workspace/gopi-req-b5-fix`, created from master **`812f9a60`**
  (`git worktree add ~/workspace/gopi-req-b5-fix -b docs/req-05-context-memory-repair`, Rule 6a;
  a **fresh** branch, Rule 23 — the original pass's worktree and branch no longer exist on this
  host, and this pass read the merged document from master rather than continuing anyone's branch).
- **No code changed:** `git diff --name-only 812f9a60..HEAD` lists exactly
  `devdoc/requirements/05-context-and-memory.md`, and `git status --porcelain` in the worktree is
  empty (Rule 30). The base of record is the commit this branch was cut from — **not** a moving
  `master`, which §6.3 of the umbrella warns is the way a citation rots.
- **Every `file:line` citation was re-read at `812f9a60`.** 57 distinct `path:line` citations
  (`grep -oE '[A-Za-z0-9_./-]+\.(go|md):[0-9]+' … | sort -u` → `57`; a narrower
  `(internal|cmd)/…\.go:[0-9]+` form yields `52`) were extracted from this document and **all 57
  resolve** to a non-empty line inside the file's length (`[ -f ]` + line ≤ `wc -l`, in the
  worktree at `812f9a60` → `bad=0`). Four are **bare-handle repeats** of a full path already
  given nearby (`loop.go:3618`, `memory.go:318`, `memory.go:2849`, `memory_sync.go:419`) — the
  same targets as `internal/agent/loop.go:3618`, `internal/tools/memory.go` `:321`/`:322`,
  `:2849` and `internal/tools/memory_sync.go:1`; they resolve by basename. The anchors that
  *moved* between `17cec86b` and `812f9a60` are corrected inline with both readings, and the
  *stale* half is spelled out at each site so a reader can follow either revision's number:
  `memory.go:318→:321`/`:322` (resident-inject constants; `:318` is a comment line at
  `812f9a60`), `:3407` `MemoryHotResident` (`:3420` at `812f9a60` is body text), `:2849` for the
  tier writer, `:3286` `FeedbackSuggestCold` (the original `:3301` was the cold-filter line of that
  query, `:3304` at `812f9a60`), and
  `internal/tools` `291→109` production files.
- **Every number in §6.5 was produced by a command run in this session** (2026-09-18, master
  `812f9a60`, host `peerbox`, `~/.gopi/memory.db` and `~/.gopi/sessions/**/*.jsonl`); each is
  re-runnable from the "How" column of the table, and the raw transcript is written to
  `~/.gopi/task-sessions/_evid-req05/` (outside the worktree — Rule 30's untracked-litter rule).
- **Build/tests deliberately not run:** no code touched, and no test yet exists for a requirement
  document (Rule 5's deliverable for a docs task is the document). Rule 27 is trivially satisfied —
  this task performed no device load; every quantity above is a count read from a store or a file.
- **What a reviewer should re-run first:** §6.5's store rows (`sqlite3 ~/.gopi/memory.db …`) and
  the post-merge adoption split (the JSONL scan keyed on each call's own `timestamp`), because
  those two carry the document's refutations of the original pass.
