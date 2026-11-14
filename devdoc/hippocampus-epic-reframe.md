<!-- doc-class: historical -->
# Epic Reframe Report: Hippocampus 独立子 Agent

## Verdict: TOO_BIG → 4 children enqueued

Epic row: `01a08913` (result: `verdict=TOO_BIG items=4`). This session's job was
diligence + decomposition, not implementation.

## Completed

- Read the 523-line design `/home/user/work/claude/nick/create_hippocampus-agent.txt`
  (nick's peerbox) + the epic briefing.
- Investigated the existing architecture: agent loop, tool layer, memory system,
  scheduler/heartbeat interaction, prompt assembly.
- Recorded the interface findings to the knowledge base (memory id=1581) so the
  child tasks inherit them without re-deriving.
- Decomposed into 4 self-contained child tasks.

## Key investigation findings (handoff for the children)

1. **The memory surface is already Go functions** — `internal/tools/memory.go`
   (~3212 lines) exports `MemorySearch(query, limit, intent)` / `MemoryStore` /
   `MemoryGet` / `MemoryUpdate` / `MemoryFeedback` / `MemoryDelete` /
   `MemoryHotResident` / `MemoryListRecent` (+ `MemoryHit`, `scanHit`).
   → A hippocampus loop calls the DB **in-process**; no tool round-trip needed,
   no new tool is required (Rule 3 respected).
2. **A tiered LLM sub-loop has a precedent** — `internal/tools/memory_brew*.go`:
   `brewClient interface{ Chat(ctx, system, user) }`, `brewFlashClient` wrapping
   `*deepseek.Client`. Flash **must** be thinking-disabled (GLM always-thinking
   → HTTP 400; `reasoning_effort=low` fallback). `callBrewJSON` extracts JSON,
   per-call timeout constants, stub client in tests. Copy this shape for
   Flash (`divergent retrieval`) vs Pro (`decision`).
3. **The passive path this epic replaces** — `internal/agent/loop.go`
   `maybeInjectTaskStartSearch` (~line 3432): harness-side forced one-shot
   `memory_search` user message + hot-resident digest, gated by
   `NoEnforceMemory`. Today's behavior must stay **byte-identical when the new
   path is off** (default-off gate).
4. **Interrupt plumbing today** — `Agent.Interrupt()` (ctx cancel + latched
   bool) = user-level hard stop; `internal/agent/heartbeat.go` = 30s tick /
   3 stale rounds / `onRemind` callback (a natural Lv-0/Lv-1 host); notify via
   `GOPI_TASK_NOTIFY_EVENTS` (`loop-stuck`, `loop-stuck-toolful`).
   Lv-2/Lv-3 (in-context meta-event, soft interrupt with quota) have **no**
   existing channel — they are the expensive part, and the design doc itself
   defers them to phases 2–3.
5. **Prompt assembly** — the system prompt is rendered from ordered
   `promptSections` (`assembleSystemPrompt`, ~4.5K density gate). A hippocampus
   briefing can only enter as a **user/system message**, never as a new section.
6. **Two doc gaps** — `devdoc/agent-five-organs-ontology.md` does **not exist**
   in the repo or in git history (the peerbox has `AI智能体的本体论与计算模型.md`);
   and there is no `internal/hippocampus` package, no `HIPPOCAMPUS*` env knob,
   no branch. Both must be authored, not found.

## Not Done (honest)

- **No implementation, no build, no test run, no commit of code.** The epic is
  gated TOO_BIG: the design names 4 deliverables (independent thinking loop,
  Flash/Pro tiering, 4-level interrupt channels, DB interface) plus a spec that
  does not exist yet, and its own roadmap spans 3 phases (Lv0-1 → event bus →
  Lv-3). Cramming a "minimal working version" into this session would have
  produced an unverified stub against a moving spec.
- The 4-level interrupt channels were **not** prototyped — that is child 3.

## Decomposition (enqueued children)

| task id | goal | acceptance |
|---|---|---|
| `01a08919-8791` | spec devdocs: five-organs ontology + hippocampus mapping | doc exists in devdoc/, mapping table organ↔package, referenced by later children |
| `01a08919-8b28` | core loop: retrieve→judge→act over in-process memory, Flash/Pro tiering | unit tests green (`go test ./internal/...`), flash thinking-disabled, JSON decision parse, stub client in tests |
| `01a08919-8ef1` | invocation path + Lv-0..Lv-3 channels + hard stop | default-off gate: existing passive path byte-identical; second user-level interrupt honored; no silent failure (Rule 15) |
| `01a08919-9399` | real-session adoption proof + report | real session (`echo "<task>" \| ./bin/gopi`) shows the hippocampus loop retrieving/judging/storing in `~/.gopi/sessions/*.jsonl`; report with evidence |

## Verification

- Build: not applicable (no code change this session).
- Tests: not applicable.
- Ledger: `gopi task show 01a08913` → `verdict=TOO_BIG items=4`;
  `gopi task list` lists the 4 children as `pending`.
- Memory: distilled interface findings stored (id=1581).

## Constraints for the children

Rule 6 worktree/branch (`feat/hippocampus-*`), Rule 1/13/14 constants (no bare
literals; `internal/toolnames` style leaf package for new shared values),
Rule 11 (a new tool/prompt change needs real-LLM adoption proof — but this epic
should need **no new tool**), Rule 5 (build → test → commit → report).
