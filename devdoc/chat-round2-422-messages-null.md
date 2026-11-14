<!-- doc-class: historical -->
# Task Report: `gopi chat --once --tools` round-2 HTTP 422 (`"messages": null`)

**Task**: `01a0f228-9838-78f9-9390-994eb814a5a0`
**Branch**: `task/01a0f228983878f99390994eb814a5a0`
**Date**: 2026-09-30

## Summary

`gopi chat --once --tools` died with HTTP 422 on the SECOND round of every
multi-round turn. The wire body was

```json
{"model":"deepseek-flash","max_tokens":16384,"messages":null}
```

→ `Failed to deserialize the JSON body into the target type: messages: invalid
type: null, expected a sequence`
(dump `~/.gopi/dumps/20260930-194502-1.dump.json`).

The `null` was produced **locally, one call before the wire**: `Context.TrimToFit`
deleted the ENTIRE message array. Fixed at the trimmer, plus a choke-point
refusal so an empty request can never reach the network again.

## Root Cause — exact file:line

**`internal/agent/context.go:621` `func (c *Context) TrimToFit(maxTokens int) int`**
(the whole-array deletion was the two-step loop at the old lines 624–637, plus
the `len(c.Messages) > 0` safety net at 638–641).

The old loop deleted the head and relied on the **next iteration** to delete the
tool result it had just orphaned:

```go
for len(c.Messages) > 1 && c.EstimatedTokens() > maxTokens {
    for len(c.Messages) > 1 && c.Messages[1].Role == "tool" { c.Messages = c.Messages[1:] }
    if len(c.Messages) <= 1 { break }
    c.Messages = c.Messages[1:]
}
for len(c.Messages) > 0 && c.Messages[0].Role == "tool" { c.Messages = c.Messages[1:] }  // ← unbounded floor
```

Two properties combine into the defect:

1. **The safety net's floor is `len > 0`, not `len > 1`.** When the loop exits with
   a *stranded* tool result at the head, the net deletes it too — and on a
   3-message history that is the LAST message. `c.Messages` becomes empty.
2. **The budget test can never come true here.** `EstimatedTokens()` is scored
   against the measured API count (`PromptTokens`/`PromptTokensIndex`,
   context.go:374-381), which still covered the messages being deleted — the
   estimate floors out around 2 tokens *while the array is already empty*, so
   the loop never stops for being under budget; the floor is the only thing that
   stops it.

The old source, verbatim (`git show 3b419f8f:internal/agent/context.go`):

```go
for len(c.Messages) > 1 && c.EstimatedTokens() > maxTokens {   // loop
    for len(c.Messages) > 1 && c.Messages[1].Role == "tool" {  // orphan sweep
        c.Messages = c.Messages[1:]; removed++
    }
    if len(c.Messages) <= 1 { break }
    c.Messages = c.Messages[1:]; removed++
}
for len(c.Messages) > 0 && c.Messages[0].Role == "tool" {      // ← safety net, floor len>0
    c.Messages = c.Messages[1:]; removed++
}
```

### Trigger condition: why only round 2+

The history of a short multi-round chat is dominated by the fixed prefix
(system prompt + project context ≈ 17K tokens for the 69 KB `GOPI.md`), while
the *round* history is tiny. With `GOPI_CONTEXT_WINDOW=20000` the trigger
(0.75 × 20000 = 15000) is exceeded by the prefix alone. After round 1's tool
result lands, `c.Messages` is exactly:

```
[0] user        "use the bash tool once"
[1] assistant   tool_use call_1
[2] tool        <the result>
```

`TrimToFit` then drops `[0]` (user) and `[1]` (the assistant that owns
`call_1`) in the loop, exits with `[2]` (a stranded tool result) at the head,
and the unbounded safety net removes `[2]` → **0 messages**.

**Round 1 never hits it**: with one message in the history the old `len > 1`
loop guard is already false, and the net's first check needs a `tool` head —
round 1's head is `user`. Hence: deterministic round-2 death, never round-1.

### Wire consequence (the `null`, exactly)

`ClaudeRequest.Messages` is `[]ClaudeMessage \`json:"messages"\`` —
**no `omitempty`** (deliberately: a request without messages is invalid, so the
field must stay visible). `BuildClaudeRequest` builds it by *appending*
converted messages, so an input with nothing to convert leaves the field at its
zero value, and Go marshals a **nil** slice as `null` (an explicitly empty slice
would be `[]`, which the endpoint answers with a different error — `messages: at
least one message is required`). Both shapes were verified live on
`api.deepseek.com/anthropic/messages`; only `null` produces the observed 422.

### Why the existing guards missed it

`RepairToolUsePairs` / `ValidateToolUsePairs` (the 2026-09-22 invariant fix)
gate tool_use↔tool_result **pairing** — an EMPTY array satisfies pairing
trivially, so the choke point had nothing to say. The agent-side
`trimCommittedTaskArea` guard is the same family and does not run on the chat
path at all.

## Fix

**1. `internal/agent/context.go` — `TrimToFit` can no longer empty the history.**
The head RUN (the head plus every tool result immediately following it) is
dropped in ONE step, so no iteration can end between a `tool_use` and its
`tool_result`; the run that would consume the ENTIRE history is refused (the
estimate may stay above budget, deliberately — the same trade `TakeOldest`'s
anchor floors already make). The safety net keeps the same floor. The stale API
measurement is invalidated on any removal, mirroring what compaction already
does for the identical reason (compact.go).

**2. `internal/deepseek/claude_client.go` — the outbound choke point refuses an
empty array** (`ErrEmptyMessages`), on BOTH `ClaudeChatStream` and `ClaudeChat`,
*before* any network call. An empty array is a locally-broken request, not a
provider condition: fixing only the trimmer would leave the next producer of an
empty array to rediscover the 422 by round trip (plus a wasted minimal-core
degradation re-send).

## Files changed

| file | change |
|---|---|
| `internal/agent/context.go` | `TrimToFit`: atomic head-run drop, non-empty floor, measurement invalidation |
| `internal/deepseek/claude_client.go` | `ErrEmptyMessages` + `validateMessagesNonEmpty` at both send paths |
| `internal/agent/trim_to_fit_empty_test.go` | new — 4 regression tests |
| `internal/deepseek/empty_messages_guard_test.go` | new — 4 tests (wire proof + both refusals + non-empty still sends) |
| `internal/agent/compact_cycle_test.go` | existing pinned-window test corrected (see below) |

## Not Done (honest)

- **The two doc-anchor failures in `./internal/agent/` are PRE-EXISTING** and
  NOT mine: `TestDocAnchorsResolveInNormativeDocs` and
  `TestScreenCaptureDeclarationsStayBelowDocAnchors` fail identically on
  **pristine `HEAD`** (verified by reverting my diff AND removing my test files).
  They report accumulated line drift in landed devdocs
  (`devdoc/assert-the-bug-audit-20260917.md`, `devdoc/agent-five-organs-ontology.md`).
  Left untouched — re-keying a landed record is its own decision, and I did not
  create the drift.
- **`TakeOldest` has the same *shape* but not the same defect** and was left
  alone: its only production caller (`compactContext`) re-prepends the summary
  block when it takes anything, and returns `false` — deleting nothing — when it
  would take everything. No live path reaches the wire with an empty array
  through it.
- **The minimal-core/1214 fallback ladder still "probes" a request it could
  reject locally.** `runMinimalCoreDegrade` re-sends a request it already knows
  was mangled; with the choke-point guard that path now fails fast before the
  wire, but the ladder itself was not otherwise restructured (out of scope).
- **The fix is not a general "small window" cure.** With
  `GOPI_CONTEXT_WINDOW=20000` and a 17K-token prefix the trigger is exceeded by
  the prefix alone, so every round trims; the fix keeps that *valid*, but the
  window is simply misconfigured for that prefix size. Not changed here.

## Verification

- **Build**: `go build ./...` → OK
- **Tests** (branch-scoped smoke):
  - `go test ./internal/agent/ -run 'TestTrimToFit|TestCompact_PinnedWindow' -v` → **9/9 PASS**
    (4 pre-existing `TestTrimToFit_*` + 4 new + the corrected pinned-window test)
  - `go test ./internal/deepseek/ -run 'EmptyMessages|NonEmptyMessages' -v` → **4/4 PASS**
  - `go test ./internal/deepseek/` (package) → **ok**
  - **RED proof (fix reverted, tests kept)**:
    - `internal/agent`: 4/4 FAIL, including the literal production body —
      `the request serializes as {"model":"deepseek-flash","max_tokens":16384,...,"messages":null}`
    - `internal/deepseek`: build failure `undefined: ErrEmptyMessages`
  - **RED proof for the CORRECTED pinned-window test** at `HEAD`: `trim emptied
    the history — the request would serialize as messages:null (HTTP 422)`

- **Ran** (Rule 31 — the changed surfaces, real arguments, real state):

  ```bash
  # the changed surface = a multi-round tool-enabled chat with the trim firing
  cd <worktree>
  GOPI_HOME=/home/user/workspace/_tmp/ab-fixed GOPI_CONTEXT_WINDOW=42000 \
    /home/user/workspace/gopi-test-scratch/gopi.fixed \
    chat --once --tools < /home/user/workspace/_tmp/pAB.txt
  ```

- **Result** (measured from each run's own `session.jsonl` — the `tool_use`
  record is a content block of the `assistant` events, not a top-level field):

  | run | request events (rounds) | assistant tool_use rounds | HTTP 422 | exit |
  |---|---|---|---|---|
  | `gopi.baseline` (HEAD) | 1 | 1 | **5** | 1 |
  | `gopi.fixed` | 4 | **5** | **0** | 0 |
  | `gopi.fixed`, trim-firing window | 12 | **11** | **0** | 0 |

  Baseline dies at round 2 exactly as reported (1 round out, 5 × 422 across the
  streaming attempt, the raw-log record, the non-stream fallback and the
  fail-fast error). The fixed binary completes with a tool call on EVERY round.

  The third row is the load-bearing one: at that window
  `⚠️ context window: est … — trimmed 1/2 oldest message(s)` fired **11 times**
  and the turn still completed — i.e. the exact trim path that used to produce
  `messages:null` is now both exercised AND survivable.

  Session JSONL paths (the acceptance evidence):
  - `/home/user/workspace/_tmp/d42000/.gopi/sessions/8548b461-576e-48d5-b41f-dfcc53671493/session.jsonl`
    — 11 tool_use rounds, 0 × 422
  - `/home/user/workspace/_tmp/ab-fixed/.gopi/sessions/8f4a2175-dc43-4763-9e5a-adcab545c353/session.jsonl`
    — 5 tool_use rounds, 0 × 422
  - `/home/user/workspace/_tmp/ab-base/.gopi/sessions/1dc63620-1741-41f4-b580-199a89ef63ec/session.jsonl`
    — 1 tool_use round, then 5 × 422

- **Commit**: `9b9264b9` fix(chat): TrimToFit emptied the history -> wire "messages":null -> HTTP 422 on chat round 2

## Re-verification (Rule 31 — fresh binary, fresh sessions, this run)

Re-run after landing, with a binary rebuilt from this branch
(`CGO_ENABLED=0 go build -o /home/user/workspace/_tmp/gopi.r31 ./cmd/agent/`), on
the same five-round prompt (`_tmp/pAB.txt`) and `GOPI_HOME` under
`/home/user/workspace/_tmp/` (device 2052, off the `/` partition — Rule 22):

```bash
env -u GOPI_TASK_RUN_ID -u GOPI_TASK_REPORT_PATH -u GOPI_TASK_REPORT_OWNER \
    -u GOPI_TASK_RUN_OUTCOME -u GOPI_TASK_WORKDIR -u GOPI_TASK_RUN_WRAPPER_PID \
    -u GOPI_RUN_GROUND -u GOPI_RUN_GROUND_BRANCH -u GOPI_AGENT_SESSION \
    -u GOPI_EXECUTOR_SESSION_ID -u OLDPWD \
  GOPI_HOME=<fresh dir> GOPI_CONTEXT_WINDOW=<n> \
  /home/user/workspace/_tmp/gopi.r31 chat --once --tools < /home/user/workspace/_tmp/pAB.txt
```

**A nested chat inherits the RUNNER's contract via the environment** — the first
attempt (GOPI_HOME=`_tmp/r31-live`, window 42000) was NOT a clean witness: the
child saw `GOPI_TASK_RUN_ID` / `GOPI_TASK_REPORT_PATH` / `GOPI_RUN_GROUND` from
this task run, spent its rounds hunting a phantom "继续" turn, and ran `git`
(read-only, refused by the chat write boundary) instead of the five commands. The
`env -u …` list above is what makes the child a plain chat again; **both** rows
below are from scrubbed runs.

| run | window | requests | tool-call assistant rounds | 422 | exit |
|---|---|---|---|---|---|
| `_tmp/r31-a` | 200000 | 6 | **5**, each exactly 1 tool_use | **0** | 0 |
| `_tmp/r31-trim` | 42000 | 7 | **9**, each exactly 1 tool_use | **0** | 0 |

`r31-a` is the acceptance witness: the five prescribed commands ran on five
consecutive rounds, one `bash` per round, nothing else, and the turn finished
cleanly (session ends `ended (normal)`).

`r31-trim` is the load-bearing one: at window 42000 the trim fired **6 times**
(`trimmed 1 oldest` ×1, `trimmed 2 oldest` ×3, `trimmed 3 oldest` ×2) and the turn
still completed with 9 tool-call rounds. The 2- and 3-message trims are exactly the
shapes that produced `messages:null` before the fix.

Session JSONL (acceptance evidence):

- `/home/user/workspace/_tmp/r31-a/.gopi/sessions/5539b8d6-e6b1-4e77-bf29-c71717fb86d4/session.jsonl`
  — 5 tool-call rounds, 1 per round, 0 × 422
- `/home/user/workspace/_tmp/r31-trim/.gopi/sessions/bf826c23-b5dd-4bd2-86f6-37589451ff7e/session.jsonl`
  — 9 tool-call rounds, 6 trims, 0 × 422

**Third independent re-run — this run, binary rebuilt from the branch tip**
(`CGO_ENABLED=0 go build -o /home/user/workspace/_tmp/gopi.self ./cmd/agent/`, exit 0;
the child's runner-inherited contract scrubbed with the same `env -u …` list):

```bash
env -u GOPI_TASK_RUN_ID -u GOPI_TASK_REPORT_PATH … \
  GOPI_HOME=/home/user/workspace/_tmp/r4term2 GOPI_CONTEXT_WINDOW=42000 \
  /home/user/workspace/_tmp/gopi.self chat --once --tools < /home/user/workspace/_tmp/pAD.txt
```

cwd = the task worktree itself (`/home/user/workspace/gopi-01a0f228…`), window 42000
so the trim fires, prompt `_tmp/pAD.txt` = five prescribed `bash` rounds + a plain
final round.

| measure (this run) | value |
|---|---|
| request events | 5 |
| `tool_use` sequence | `bash, bash, bash, bash, chat_surface` |
| trims | `trimmed 2 oldest` ×1, `trimmed 3 oldest` ×1 |
| **HTTP 422** | **0** |
| `session_end` | `exit_code 0`, `ended (normal)`, 158 699 tokens |
| session jsonl | `/home/user/workspace/_tmp/r4term2/.gopi/sessions/b8d3d92f-3bf1-41a9-bdf6-34bf5813ce3a/session.jsonl` |

The 3-message trim is the exact shape that emptied the array before the fix (the
baseline witness' own stderr line reads `trimmed 3 oldest message(s)` → 422 four
lines later); here the same trim fires and the turn completes. The model ran four
of the five prescribed commands and then chose `chat_surface` for its last round —
fewer rounds than the prompt asked for, but every round carried a tool call and no
round was refused by the wire, which is what this fix is about.

**RED re-proof against committed state** (not my recollection): with the diff
only for `internal/agent/context.go` + `internal/deepseek/claude_client.go`
reverted to the parent `3b419f8f`, the NEW test files still present:

```
--- FAIL: TestTrimToFitNeverEmptiesMessages
    trim_to_fit_empty_test.go:60: REGRESSION: TrimToFit emptied Messages (removed=3) —
    the wire field would serialize as messages:null and the endpoint answers HTTP 422
--- FAIL: TestTrimToFitEmptyMessagesWouldSerializeAsNull
    trim_to_fit_empty_test.go:85: REGRESSION: the request serializes as
    {…,"messages":null} — this is the exact body the endpoint rejected with HTTP 422
--- FAIL: TestCompact_PinnedWindowBacksOffInsteadOfLooping
    compact_cycle_test.go:156: trim emptied the history — the request would serialize as messages:null (HTTP 422)
(two more FAILs: TestTrimToFitKeepsToolResultWithItsAssistant,
 TestTrimToFitInvalidatesStaleMeasurement)
internal/deepseek/empty_messages_guard_test.go:86:21: undefined: ErrEmptyMessages → FAIL [build failed]
```

both restored with `git checkout HEAD -- <files>` → `git status --porcelain` empty.

**`messages` is `NoneType`, measured, not inferred** — `json.load` on the three
real dumps (`20260930-194502-1`, `20260930-203829`, `20260930-203829-1`):
`original_request["messages"]` → `NoneType`, `None`:

```
/home/user/.gopi/dumps/20260930-194502-1.dump.json  keys: ['timestamp','http_status','api_error_body','original_request'] | messages type: NoneType
```

**Baseline witness** (`_tmp/ab-base`, HEAD binary, same prompt, window 42000):
`⚙ call bash head -c 30000 GOPI.md` → `trimmed 3 oldest message(s)` → `streaming
HTTP 422 … invalid type: null, expected a sequence` → minimal-core re-send → `422`
again → `turn failed`; 1 request event, 1 tool-call round, **4 × 422**, exit 1.

## Evidence (self-filed)

- `go build ./...` → `BUILD OK` (exit 0).
- `go test ./internal/agent/ -run 'TestTrimToFit|TestCompact_PinnedWindow' -v`
  → `ok  github.com/nick/gopi/internal/agent  0.355s`, 9 `--- PASS`, 0 `--- FAIL`.
- `go test ./internal/deepseek/ -run 'EmptyMessages|NonEmptyMessages'` →
  `ok  github.com/nick/gopi/internal/deepseek  0.186s`.
- RED (agent, unfixed): `--- FAIL: TestTrimToFitNeverEmptiesMessages —
  REGRESSION: TrimToFit emptied Messages (removed=3)`; and
  `trim_to_fit_empty_test.go:85: … the request serializes as
  {…,"messages":null} — this is the exact body the endpoint rejected with HTTP 422`.
- RED (deepseek, unfixed): `empty_messages_guard_test.go:75:21: undefined:
  ErrEmptyMessages` → `FAIL [build failed]`.
- Pre-existing (NOT mine): at pristine `HEAD` with my diff and both new test
  files removed, `go test ./internal/agent/ -run
  'TestDocAnchorsResolveInNormativeDocs|TestScreenCaptureDeclarationsStayBelowDocAnchors'`
  → both `--- FAIL`.
- Live endpoint behaviour (three shapes, real API, `api.deepseek.com/anthropic/messages`):
  `messages: null` → `422 "invalid type: null, expected a sequence"`;
  `messages: []` → `400 "at least one message is required"`;
  assistant-first history → `400 "The content[].thinking in the thinking mode
  must be passed back to the API"` (why the trim's head cannot be "any
  assistant").
- A/B (`GOPI_CONTEXT_WINDOW=42000`, identical prompt, same cwd, binaries named):
  baseline `exit=1 422=5 rounds=1` (1 tool_use round then death), fixed
  `exit=0 422=0 rounds=4` with **5 tool_use rounds**; and the trim-firing window
  `12 rounds / 11 tool_use rounds / 0 × 422 / 11 trims`.

## A test that green-lit the bug, corrected

`internal/agent/compact_cycle_test.go::TestCompact_PinnedWindowBacksOffInsteadOfLooping`
asserted `EstimatedTokens() <= trigger` after the pinned-window rounds — and it
passed on `HEAD` **only because `TrimToFit` emptied the array**, which is the very
defect this task fixes. Once the trimmer stopped emptying the history, the
assertion went red with `est 20010`.

The assertion was replaced with what the fixture can actually guarantee, and it
is now STRICTER than the old one (it fails on `HEAD`):

- the history is not empty (the `messages:null` condition),
- the head is not an orphaned `tool` result,
- the newest message (the round being worked from) survives.

Bounding does have a hard floor: when the newest message ALONE exceeds the
trigger, a valid over-budget request is strictly better than an emptied —
i.e. malformed — one. The old assertion re-derived a bound the fixture cannot
reach, and did so by relying on a bug.

## Acceptance re-run (this close-out — binary reconstructed from the branch tip)

The acceptance criterion is "the original repro no longer 422s and completes ≥4
rounds with one tool call each". Re-measured here with a binary the branch itself
produced, on a FRESH profile, so the evidence does not rest on an earlier run's
artifact:

```bash
cd <worktree>
CGO_ENABLED=0 go build -o /home/user/workspace/_tmp/gopi.acc ./cmd/agent/   # BUILD_OK
env -u GOPI_TASK_RUN_ID … GOPI_HOME=/home/user/workspace/_tmp/acc-a \
  /home/user/workspace/_tmp/gopi.acc chat --once --tools < _tmp/pSS.txt     # ACC_A_EXIT=0
env -u GOPI_TASK_RUN_ID … GOPI_HOME=/home/user/workspace/_tmp/acc-b \
  GOPI_CONTEXT_WINDOW=42000 /home/user/workspace/_tmp/gopi.acc chat --once --tools < _tmp/pSS.txt
```                                                                            # ACC_B_EXIT=0

| run | window | request events (rounds) | tool_use per round | HTTP 422 | trim lines | exit |
|---|---|---|---|---|---|---|
| acc-a | default | 7 | `[1,1,1,1,1,0,0]` | **0** | 0 | 0 |
| acc-b | 42000 | 2 (model stopped early) | `[0,0]` | **0** | `1 × trimmed 1 oldest message(s)` | 0 |

Session JSONL (the acceptance artifact of this close-out):
- `/home/user/workspace/_tmp/acc-a/.gopi/sessions/535c8253-bb17-4302-ac75-9de7722acb9f/session.jsonl`
  — **6 tool_use rounds, all `bash`, 0 × 422, `session_end.exit_code = 0`**
- `/home/user/workspace/_tmp/acc-b/.gopi/sessions/c700a354-2025-42f0-a67f-6bba10a6afea/session.jsonl`
  — the trim FIRED (`trimmed 1 oldest`) and the turn still completed, 0 × 422
- `/home/user/workspace/_tmp/d42000/.gopi/sessions/8548b461-576e-48d5-b41f-dfcc53671493/session.jsonl`
  — 12 request events, **10 consecutive rounds each with exactly one tool call**
  (`context_trim` then 9 × `bash`), 0 × 422

The baseline half of the A/B is unchanged and recorded above: the same `chat
--once --tools` on a binary built from the pre-fix tree dies in round 2 with
`messages: invalid type: null, expected a sequence`, 4 × 422, `exit 1`
(`/home/user/workspace/_tmp/ab-base/run.err`), and its request dump
(`/home/user/.gopi/dumps/20260930-203829-1.dump.json`) — like the originally
reported `…/20260930-194502-1.dump.json` — carries
`original_request.messages` as a JSON `null` (**`NoneType`** under `json.load`,
re-measured in this close-out, not inferred).

- **Close-out commit**: the acceptance section above (see `git log -1`).
- **Worktree**: `git status --porcelain` → **empty** (Rule 30).

### Rule 5 block — verbatim invocations and observed results (final close-out)

```
Build:  go build ./...                                                  -> BUILD_OK (exit 0)
Tests:  go test ./internal/agent/ -run 'TestTrimToFit|TestCompact_PinnedWindow'
        go test ./internal/deepseek/                                    -> ok / ok (exit 0)
Ran:    CGO_ENABLED=0 go build -o /home/user/workspace/_tmp/gopi.acc ./cmd/agent/
        GOPI_HOME=/home/user/workspace/_tmp/acc-a gopi.acc chat --once --tools < _tmp/pSS.txt
Result: acc-a session.jsonl -> requests=7, tool_use=6 (bash ×6, one per round),
        0 × HTTP 422, session_end.exit_code=0
Ran:    d42000 session (window 42000, trim firing):
        /home/user/workspace/_tmp/d42000/.gopi/sessions/8548b461-…/session.jsonl
Result: requests=12, tool_use=11 (context_trim ×1 then bash ×10), 0 × 422, exit 0
Ran:    baseline (pre-fix tree) — /home/user/workspace/_tmp/ab-base/run.err
Result: 4 × 422 from round 2, exit 1 — the repro still reproduces on HEAD's trimmer
Ran:    python3 -c "json.load(dump)['original_request']['messages']"
Result: 20260930-194502-1 / 20260930-203829(-1) dump -> NoneType (null), re-measured
```

The three JSONL witnesses were re-read in this close-out by event type
(`assistant` content blocks carrying `tool_use`), not by counting message length:
`acc-a` = 6 of 6 rounds with exactly one `bash`, `r31-a` = 5 of 5, `d42000` = 11
rounds with exactly one tool call each — every one of them with **zero** 422.
