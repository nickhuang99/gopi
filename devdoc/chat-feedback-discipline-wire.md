<!-- doc-class: normative -->
# The chat prompt already carries the memory vote discipline — a wire measurement, and the guard that keeps it there

> **Provenance.** `TestDocAnchorsResolveInNormativeDocs` was retired on 2026-10-22 in `5a35f61` (deleted with `internal/docclass`), so a `file.go:NNN` reference in this page is a landing-time record, not an anchor a live gate enforces — nothing resolves one today. The line numbers stay useful to a reader; they are not checked by the suite. Citations below are quoted as they were written — where one of them reads as present tense ("is RED", "resolves", "fails"), read it as the state of THAT run, never as a gate this tree still has.

Task `01a11fbdd016735fa5cb8eabcf6a4447` (sibling of `01a11fc08bb47373aeff98f317ab40af`,
the prefetch seeding task). No prompt text was added; the deliverable is a
measurement plus the regression guard that turns a **caller contract** into a
red build.

## The claim under test, and why it was wrong

The task was filed on a reading of the code: *"the chat system prompt carries no
`toolRoutingRules`, so a chat turn is never told to vote on the memory hits it
reads — the prefetch's useless rows therefore accumulate."*

The reading is half right and the half it gets wrong is the half that matters.
Measured on the shipped carriers:

| path | assembly | carries the vote rule? |
|---|---|---|
| `gopi chat --tools` (`ChatOps`) | `chatOpsSystemPrompt(a)` = `a.SystemPrompt` + the chat block | **yes, verbatim** |
| `gopi chat` (no tools, `Agent.Chat`) | `chatSystemPromptServed(a)` = persona + the chat block | no — and correctly so |

`ChatOps` composes `a.SystemPrompt` as its base, and every production carrier
builds that agent through `newAgent` → `assembleSystemPrompt()` (`loop.go`), whose
registry includes the `toolRouting` section. So the memory if-then block — clause
**c** included — is on the chat `--tools` wire, and the prefetch task's own report
had already measured the model voting on injected hits.

The plain path is the honest absence: `gopi chat` without `--tools` advertises no
tool definitions at all, so a vote instruction there would be an instruction the
model cannot carry out — the exact failure the chat block's own limbs
(`chatNoToolsLimb` / `chatToolsLimb`, `chatMemoryTimingNoToolsLimb`) are written to
avoid.

## What is genuinely load-bearing, and therefore what the guard must pin

The `--tools` limb rests on a **caller contract**, not on a property of the
assembly: `chatOpsSystemPrompt` reads the base from `a.SystemPrompt`, while the
tool wire (`a.chatOpsToolDefs()`) is built independently. So an edit that dropped
that argument — or a carrier that built its agent without it — would strip the
vote discipline off the chat wire **while the memory tool stayed advertised**: the
model would hold a tool it was never told how to use, and nothing would go red.

That is the gap that was real, and it is a test's job, not a prompt paragraph's:
adding a chat-owned copy of the rule would be a **second definition** of a rule
the kernel already states (Rule 1/13/14) and is exactly what the parent task's
chosen shape (A1) exists to avoid.

## The guard

`internal/agent/chat_feedback_discipline_test.go` — five assertions, all against
the COMPOSED strings the request builders send:

1. **`extractMemoryVoteRule`** reads clause c VERBATIM out of `toolRoutingRules`
   (the shipped string), so every chat assertion below says "the chat wire carries
   *this* sentence" and cannot drift from the worker's copy.
2. `TestMemoryVoteRuleIsOneDefinition` — the floor: the clause still exists and
   still names both halves (`rating=useful/useless/unused` … `id=#N` … the
   useless→feedback+update consequence).
3. `TestChatOpsPromptCarriesTheMemoryVoteDiscipline` — the `--tools` path carries
   it **verbatim and exactly once** (a second occurrence is a second definition,
   not a miss).
4. `TestPlainChatPromptCarriesNoMemoryVoteDiscipline` — the no-tools path carries
   neither the rule nor the routing block, asserted *together with* the presence
   of the chat block, so the absence cannot be satisfied by an assembly that
   silently stopped rendering anything.
5. `TestTaskPromptStillCarriesTheVoteDisciplineOnce` /
   `TestChatOpsToolWireAlwaysAdvertisesMemory` — the controls: the rule was not
   moved out of the kernel, and the tool wire really does advertise `memory`
   (so the chat-limb claims are about a tool that is there), while a bare
   `Agent{}` — no `SystemPrompt` — produces no vote rule from anywhere (the
   discipline has no second source).

### Red proof

Replacing the composition's base argument with `_ = base` in
`chatOpsSystemPrompt` (the coupling dropped) fails assertion 3 with the exact
sentence it wants; restoring it passes. Same run, same command:

```
$ go test ./internal/agent/ -run TestChatOpsPromptCarriesTheMemoryVoteDiscipline
--- FAIL: TestChatOpsPromptCarriesTheMemoryVoteDiscipline (0.01s)
    the --tools chat prompt does not carry the kernel's vote rule verbatim …
```

## Verification on the wire (Rule 31)

A sandbox profile (`gopi tool sandbox create --label chatfb …`), the built
binary, `gopi chat --tools --no-editor` driven from a pipe:

```
$ GOPI_HOME=$SB ./bin/gopi chat --tools --no-editor <<< '…vote on each hit…'

$ grep -c 'IMMEDIATELY after each memory(action=search) vote memory(action=feedback, rating=useful/useless/unused) id=#N' \
        $SB/.gopi/sessions/c128d66b-….raw.log
1                      # the SYSTEM section of a real request body, not the assembly
```

and the votes themselves — every rating in the enum, so the guard against
over-correction is measured rather than asserted:

| run | votes (`session.jsonl`, `memory` tool_use) | what it proves |
|---|---|---|
| `c128d66b…` | `#1 useful`, `#3 unused` (reason: "内容正确且与我的实测不矛盾…中性一票") | `useful` and the deliberately-mild `unused` both occur — the model does not reflexively reach for `useless` |
| `f0bdc51b…` | `#1 useful`, `#3 useful`, `#4 useless` + `memory(action=update)` on `#4` | `useless` fires when a record is *contradicted*, and the contradiction triggers the correction the rule names |

`#4` was a deliberately planted false record ("chat never votes, it carries no
toolRoutingRules"); the run voted `useless` **and** rewrote it in place, and
`gopi memory get 4` afterwards shows the rewritten topic and answer — the vote
and the consequence, both observed.

## Not done (honest)

- **The no-tools limb is asserted, not driven.** `chatPrefetchTurnText`'s doc
  already states the limit; this change does not add an end-to-end run of the
  plain path (it has no vote to observe — the absence is the claim).
- **`TestDocAnchorsResolveInNormativeDocs` is RED on this branch, and the red is
  NOT mine — measured three ways.** With the deliverable in the tree:
  `3089 anchors across 697 normative docs … 242 permitted, 34 new, 12 stale
  permits`. With the two new files moved out and `devdoc/DOC-INDEX.tsv` reverted:
  `3089 anchors across 696 normative docs … 242 permitted, 34 new, 12 stale
  permits` — the SAME line, byte for byte, only the doc count moving (my doc is
  the 697th, and it contributes **zero** anchors: it carries no `*.go:NNN`
  token). The 34-new drift is inherited from the delivery-point merge
  (`dc9821e6e`) and is untouched here — this report is the ranked-drift channel
  the gate's own failure text points at.
- The chat-limb coupling is pinned as an invariant, not enforced by a fallback:
  the degraded shape (tool advertised, discipline absent) remains reachable for a
  hand-built `Agent` — see assertion 5's doc comment for why a prompt-side
  fallback was rejected.
