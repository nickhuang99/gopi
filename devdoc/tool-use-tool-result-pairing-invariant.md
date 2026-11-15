<!-- doc-class: normative -->
# The tool_use / tool_result pairing invariant — proven, not asserted

Task: `01a0c5d2-1219-780f-b523-7fc1ddca7534` (branch `fix/tooluse-pairing-invariant`).

## The defect, in the provider's own words

`trimCommittedTaskArea()` (`internal/agent/loop.go`) dropped the committed
working region with

```go
a.Ctx.Messages = append(a.Ctx.Messages[:1], a.Ctx.Messages[ci:]...)
```

and its comment justified the unconditional `[:1]` head with *"the walk is
orphan-safe by design"*. That assertion was false. The head is safe only while
`messages[0]` carries no tool_use whose tool_result sits inside the dropped
window `[1, ci)`. When it does, the kept head is an unbalanced tool_use, and the
Claude Messages API rejects the request deterministically. The recorded death,
verbatim from `~/.gopi/dumps/20260922-040822.dump.json`:

```
HTTP 400
{"error":{"message":"messages.1: `tool_use` ids were found without `tool_result`
 blocks immediately after: call_00_ET_TBCTnLz8k12TGe2ZEVxl2105. Each `tool_use`
 block must have a corresponding `tool_result` block in the next message.",
 "type":"invalid_request_error", ...}}
```

The dumped request carried 3 messages, head `assistant[thinking, tool_use
call_00_ET_TBCTnLz8k12TGe2ZEVxl2105]` with no result for it, and 17 tool defs —
346 messages had become 3.

Two consequences, and the second is why it was not a one-off:

1. the live run died at the hand-off (measured: 1 iteration, 0.4 s);
2. **the damaged array is what the raw log recorded** — and the raw log is the
   resume checkpoint. Every resume rebuilt the same malformed request and died
   on its first request, forever.

The minimal-core degradation made the shape worse rather than better: it strips
the `tools` array while replaying the orphan tool_use, i.e. it declares no tools
and then shows a tool call.

## The fix: prove the invariant, at every layer that can carry it

An invariant that the harness's OWN context management can break is not
assertable in prose. So it is checked at both ends of the pipeline:

| Layer | File | What it does |
|---|---|---|
| trim site | `internal/agent/loop.go` | the kept head is **derived** (`trimCommittedTaskAreaHead`): index 0 is kept only when every tool_use it carries is answered inside the kept region; otherwise the head goes too |
| loop outbound | `internal/agent/loop.go` | `deepseek.ValidateToolUsePairs(req)` on the built request — fail closed, naming the iteration index |
| checkpoint in | `internal/agent/resume.go` | `LoadCheckpoint` — the ONE door a persisted checkpoint enters through — repairs the recovered history and refuses what it cannot repair |
| resume outbound | `internal/agent/loop.go` (`runEngine`) | the recovered history is repaired and proved before the run starts |
| wire choke point | `internal/deepseek/claude_client.go` | `ClaudeChat` **and** `ClaudeChatStream` repair, then gate, every request whichever layer built it |
| the repair/proof itself | `internal/deepseek/toolpair.go`, `internal/agent/context.go` | `RepairToolUsePairs` / `ValidateToolUsePairs` (wire) and `repairToolUsePairs` / `verifyToolUsePairs` (source messages) |

### Why the head is DROPPED rather than the tool_use

The repair at the trim site deliberately does **not** rewrite the head's blocks.
Two reasons:

- `BuildClaudeRequest` replays a tool_use that the model and the user already
  saw; removing it silently rewrites history the provider expects to be
  replayable (the same class of mistake as dropping a thinking block, which
  this codebase already documents as a 400).
- The briefing's content does not depend on the epic message surviving: every
  committed item's conclusion is carried forward by the
  committed-conclusions block. So a head that cannot be answered is dropped,
  and the alternative — keeping it — trades a readable-but-dropped briefing for
  a malformed request.

The **wire-side** repair does the opposite (it supplies the missing result)
precisely because that layer cannot drop anything: it is the net under every
builder, including third-party callers, and deleting a message there would be a
silent rewrite of someone else's array.

### Fail-closed, never fail-silent

- A repair is *logged* and recorded via `recordClientFailure` — it keeps the run
  alive, it does not excuse the damage; the dump names the orphaned ids.
- The gate then re-reads the array and **refuses to send** anything the repair
  could not fix (`refusing to send a request that violates the
  tool_use/tool_result pairing invariant: ...`). Nothing reaches the wire
  unpaired, and nothing unpaired is what the raw log records.

## Tests

Master-RED (uses only pre-existing symbols, so it compiles and RUNS on master —
no build failure masking the assertion):

- `internal/agent/toolpair_invariant_test.go`
  - `TestTrimCommittedTaskArea_PoisonedHeadMustNotKeepOrphan` — the dumped window
  - `TestResume_PoisonedCheckpointRepairsInsteadOf400`
  - `TestLoadCheckpoint_PoisonedRawLogRecoversARepairedHistory`
  - `TestTrimCommittedTaskArea_HealthyShapeUnchanged` (no behaviour change)
  - `TestResume_HealthyHistoryUnchanged` (no behaviour change)
- `internal/deepseek/toolpair_wire_gate_test.go`
  - `TestClaudeChatStream_ServerNeverSeesUnbalancedToolUse` — asserts on what
    the **server** received, not on a local variable

GREEN-only (uses the new repair/proof helpers):

- `internal/agent/toolpair_repair_test.go`,
  `internal/deepseek/toolpair_test.go` (repair shapes: joins an existing tool
  round / gets its own message / healthy request byte-identical / minimal-core
  degradation never emits an orphan with tools stripped / the gate names the
  offending id and index).

### ⚠ One existing assertion was FLIPPED, and here is why

`internal/agent/resume_test.go::TestResume_DanglingToolUse` documented the
**bug**: it asserted that a dangling tool_use is "simply carried in history and
the API-side conversion keeps it", and that the last message stays the bare
continuation text. That is the exact shape the API rejects with the 400 above —
it was a test pinning the defect in place. It now asserts the property that
matters: the resumed wire carries a tool_result for every tool_use (the dangling
`call_9` gets a synthesized one), and the continuation still arrives. The flip is
recorded in the test's own comment.

`TestTrimCommittedTaskArea_*` assertions were **not** relaxed: the existing
`DropsAnalysisKeepsEpicAndCommit` / `KeepsLaterLiveWork` tests still pass
unchanged, and the healthy-shape cases above pin the no-behaviour-change
property.

## Live evidence (Rule 31)

The real poisoned checkpoint was reconstructed from the dump and resumed with
two binaries — master's and this branch's — same command, same arguments, same
provider:

```
# RED (master)
go build -o /home/user/workspace/tmp/gopi-master ./cmd/agent/
GOPI_HOME=.../liveopt-master ./gopi-master resume .../poisoned-raw.log .../cont.txt
→ 0.4s | 1 iterations | FAIL
→ Error: LLM call failed (stream: deepseek: API error (status 400):
  "messages.1: `tool_use` ids were found without `tool_result` blocks
   immediately after: call_00_ET_TBCTnLz8k12TGe2ZEVxl2105 ...")

# GREEN (this branch)
GOPI_HOME=.../liveopt-fixed ./gopi-fixed resume .../poisoned-raw.log .../cont.txt
→ 4 successful requests, stream=true, tools=17, UNPAIRED=NONE on every one
```

The repaired result for the orphan is visible **on the wire** of the resumed
session's raw log — the checkpoint the *fixed* process persists is already
sound, which is what stops the death from repeating across resumes:

```
0 assistant [thinking, tool_use call_00_ET_TBCTnLz8k12TGe2ZEVxl2105]
1 user      [tool_result call_00_ET_TBCTnLz8k12TGe2ZEVxl2105]   ← synthesized
2 assistant [thinking, text, tool_use call_00_ET_UDn4u8i7lQ5apc8zkKjv4422]
3 user      [tool_result call_00_ET_UDn4u8i7lQ5apc8zkKjv4422, text]
```

## Not done

- The **corpus migration**: the invariant is now checked at the two choke
  points (the loop's build and the client's send) and at the trim site, but the
  ~30 other call sites of `BuildClaudeRequest` inside the tree are protected
  only by the client-side net, not by a per-site proof. That is the intended
  design (one net at the choke point), not a gap to close by hand — see the
  devdoc section "fail-closed, never fail-silent".
- **`internal/agent/chat_ops.go` / `compact.go` reuse**: the loop's own proof
  covers the ReAct path; the compact/chat paths rely on the client net. No
  observed failure there, so no code was added speculatively.
- `make fmt` was deliberately NOT run (task constraint).
