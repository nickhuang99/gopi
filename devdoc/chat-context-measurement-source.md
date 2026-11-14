<!-- doc-class: historical -->
# Task Report: the chat context measurement — where "measured 0" actually comes from

Task: `01a10e3682ea7744876f06ef67dbf184`. Epic: *"chat 的 ctx used 值从不来自 API 实测…本任务修数据来源侧"*.

**Headline: the briefing's premise was measured and found FALSE in one half and
TRUE in another, and the two halves have different fixes.** The streamed API usage
**does already reach `Context`** on a normal round; what never reached it was the
`measured` NUMBER PRINTED IN THE LOG LINE, which is read one branch *after* the
compaction itself zeroes the field. Both halves are fixed here.

---

## 1. Source + measurement verdict (deliverable 1)

### The measured shape of the two usage frames

Read off this repo's OWN raw HTTP logs, not off documentation. Session
`8981132f-7fbd-40b6-b9fd-d5e777fc3de4`, its raw log, two lines of the same round:

```
line 6:   data: {"type":"message_start","message":{"id":"5203ddb0-…","usage":{"input_tokens":29456,"cache_creation_input_tokens":0,"cache_read_input_tokens":2048,"output_tokens":0,"service_tier":"standard"}}}
line 310: data: {"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":29456,"cache_creation_input_tokens":0,"cache_read_input_tokens":2048,"output_tokens":113,"service_tier":"standard"}}
```

Counted across that whole session (script over the 61 MB raw log):

```
starts 144 deltas 144
message_delta frames with input_tokens == 0 : 0
MAX prompt-side total (input+cache_creation+cache_read) : 151407
```

So **both frames carry the prompt-side total, and they agree on it**; `message_delta`
differs from `message_start` only by `output_tokens`. The briefing's claim
("只有 message_delta 且只带 output_tokens") is **false on this wire** — and the
memory hit #6315 was right that a `message_start` fallback exists, but its
description of the delta arm ("records SetPromptTokens only in the message_delta
arm … message_start's usage is only the no-delta fallback") describes a fallback
that **did not exist in the code**: at the point this task started, the no-delta
fallback fed the **COST tracker only** (`loop.go`, `if !sawDelta && a.Cost != nil
&& startUsage.TotalTokens() > 0 { a.Cost.AddClaudeUsage(startUsage) }`) and never
`SetPromptTokens`.

### Source, with file:line

| what | where (before this change) |
|---|---|
| SSE frame → event: `message_start` puts `wire.Message.Usage` into `ev.Usage` | `internal/deepseek/claude_client.go:1276-1279` |
| SSE frame → event: `message_delta` puts `*wire.Usage` into `ev.Usage` | `internal/deepseek/claude_client.go:1280-1283` |
| `ClaudeUsage` fields | `internal/deepseek/claude_client.go:288-293` |
| the ONLY stream arm that wrote the measurement | `internal/agent/loop.go:5600-5602` (message_delta) |
| the `message_start` arm — stored the usage, wrote nothing | `internal/agent/loop.go:5517-5518` |
| the no-delta fallback — cost only | `internal/agent/loop.go:5608-5610` |
| non-streaming arm (already correct) | `internal/agent/loop.go:5390-5392` |
| `SetPromptTokens` | `internal/agent/context.go:203-206` |
| `EstimatedTokens` consumes it | `internal/agent/context.go:408-422` |

### The decisive experiment

A probe test called the REAL `chatOnce` against a scripted SSE server and then read
the context:

```
AFTER STREAM: PromptTokens=10 PromptTokensIndex=1 msgs=1 est=10
LOG: 🧠 context window: est 9000 tokens (measured 0) > 1500 (450.0% of 2000) — compacted: …
trigger est(before trim)=9000 PromptTokens AT TRIGGER=9000
AFTER COMPACT: PromptTokens=0
```

That is the whole diagnosis in four lines: the measurement **is** recorded (10, from
the frame's usage), the trigger **does** judge it (9000), and the log still prints
`measured 0` — because `compactContext` sets `a.Ctx.PromptTokens = 0` (compact.go,
"the API measurement … must be invalidated or the next round re-triggers on the
stale count") **before** `loop.go`'s compaction line formats `a.Ctx.PromptTokens`.

Field corroboration, from the live profile:

```
$ grep -rh "est [0-9]* tokens (measured [0-9]*)" ~/.gopi/sessions/*/console.log | grep -oh "(measured [0-9]*)" | sort | uniq -c | sort -rn
     95 (measured 0)

$ grep -rl "context window: est .* (measured [1-9]" ~/.gopi/sessions/*/console.log
(no output)
$ grep -rl "context window: est" ~/.gopi/sessions/*/console.log | wc -l   → 9 sessions, 150 lines
```

95 of 150 lines carry a `(measured N)` field and **every one of them is 0** — while
the same sessions' raw logs are full of non-zero prompt-side counts (session
`8981132f`: 144 such frames, its last measuring **151407**, against a 200 000 window
and a 150 000 trigger — i.e. that session's compaction fired on a *measured
149 456*, and reported `measured 0`).

## 2. What was changed (deliverable 2)

**a. `internal/deepseek/claude_client.go`** — the three-term prompt-side sum gets
ONE definition and an absence predicate, instead of being re-spelled:

```go
func (u ClaudeUsage) PromptSideTokens() int      // input + cache_creation + cache_read
func (u ClaudeUsage) HasPromptMeasurement() bool // PromptSideTokens() > 0
```

`AddClaudeUsage` now reads `PromptSideTokens()` (Rules 1/13/14: the value the
context and the cost tracker both need has one spelling).

**b. `internal/agent/loop.go` — the `message_start` arm now calibrates the context**
(`SetPromptTokens(ev.Usage.PromptSideTokens())`), so a round that never reaches a
`message_delta` — interrupted, over-budget and re-issued, a stream that dies
mid-flight — is measured instead of falling back to chars/4. The double-count the
old comment warned about is impossible by construction: `SetPromptTokens`
**assigns**, and the delta is a strictly more complete frame of the same round. The
COST tracker still takes exactly ONE frame per round (the delta, with the start as
its no-delta fallback) because that one **accumulates**.

**c. `internal/agent/loop.go` — the compaction line reads the measurement where it
still exists**: `measuredAtTrigger := a.Ctx.PromptTokens` is captured before the
compaction branch and used in the log, so the line reports the count the trigger
actually judged. The DECISION path is untouched: the early return, `limit`,
`EstimatedTokens()` and every consumer of `EstimatedTokens` behave exactly as
before.

## 3. Verification (Rule 31 — the surfaces actually run)

```
Build:  CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/     → exit 0, bin/gopi 30 594 300 bytes
Tests:  go test -count=1 -run 'TestStreamMessageStartUsage|TestStreamUsageIsMeasured|TestStreamZeroUsage|TestCompactionLogNamesTheMeasured' ./internal/agent/   → ok
        go test -count=1 -run 'TestSetPromptTokens_Calibrates|TestCompact_' ./internal/agent/                                                              → ok
        go test ./internal/deepseek/ (claude usage tests)                                                                                                  → ok
Red:    the two NEW tests fail on the pre-change tree and pass on it —
        TestStreamUsageIsMeasuredWithoutAMessageDelta → "PromptTokens = 0 after a delta-less stream, want 9200"
        TestCompactionLogNamesTheMeasuredCountAtTrigger → "the compaction line does not carry the measured count the trigger judged"
Ran:    GOPI_CONTEXT_WINDOW=42000 GOPI_COMPACT_TRIGGER=0.75 GOPI_COMPACT_TARGET=0.35 \
        GOPI_COMPACT_KEEP_RECENT=1 ./bin/gopi chat --once --tools
        (real provider, real key, real repo, real CONFIG.md reads — §4c quotes the
         invocation in full)
Result: session bdf97970-a71d-47eb-ab1f-3b3f6b2088e5 printed
        "est 71839 tokens (measured 36267) > 31500 … — compacted: heuristic …",
        and 36267 is EXACTLY the first message_start frame of that same session's
        raw log (33963 + 2304) — the API's own number, on a real compaction line
```

## 4. The real chat session (deliverable 3)

Session `ce556db1-82a3-48e0-a792-c0acb704367a`, run with
`GOPI_CONTEXT_WINDOW=26000`, its own console log verbatim:

```
you> [gopi] ⚠️ context window: est 20186 tokens > 19500 (77.6% of 26000) — trimmed 0 oldest message(s)
[gopi] ⚠️ context window: est 38639 tokens > 19500 (148.6% of 26000) — trimmed 1 oldest message(s)
```

The second line is the measurement doing its job: on the previous round the provider
reported `"input_tokens":33894,"cache_read_input_tokens":2304` (**36 198**
prompt-side), and the estimate the trigger judged is **38 639** = 36 198 measured +
the messages added since — where the pure chars/4 heuristic for the same context was
20 186 (line 1). Before this change the second round would have been judged on
20 186 (still under the 19 500… no — that is exactly the point: **the measured basis
is 91 % larger than the heuristic**, which is why the trigger must fire on it).

A second, independent real session confirms it (session
`329f9d99-3fb6-4717-97a1-7d40a8a14e2d`, `GOPI_CONTEXT_WINDOW=20000`):

```
you> [gopi] ⚠️ context window: est 20203 tokens > 15000 (101.0% of 20000) — trimmed 0 oldest message(s)
[gopi] ⚠️ context window: est 37671 tokens > 15000 (188.4% of 20000) — trimmed 1 oldest message(s)
```

Its raw log's last prompt-side frame reads
`"input_tokens":2023,"cache_creation_input_tokens":0,"cache_read_input_tokens":35968`
(37 991 measured), against a 20 203 heuristic for the same corpus: **the measured
basis is ~1.9× the guess**, which is the entire reason the trigger must fire on the
API's number.

## 4b. A drift this change had to avoid

The first version of the `PromptSideTokens`/`HasPromptMeasurement` pair was placed
beside `ClaudeUsage`. The doc-anchor gate immediately reported **one new drift**:

```
devdoc/plain-prompt-first-user-message-scope.md: anchor internal/deepseek/claude_client.go:560
    points at a BLANK line … — the anchor drifted off its symbol
```

`claude_client.go:560` is a **dated landed record's** anchor and a landed record is
never re-keyed, so the honest-looking repair (a baseline permit) would have been a
permit for a line this change had no business moving. Measured, same tree, same test,
three placements:

```
master                                  : … (240 pre-existing drift entries permitted, 9 new, 9 stale permits)
the two functions beside ClaudeUsage    : … (240 … permitted, 10 new, 9 stale permits)   ← 1 new
the two functions beside TotalTokens    : … (240 … permitted, 10 new, 9 stale permits)   ← 1 new (:481 instead)
appended at the END of the file         : … (240 … permitted, 9 new, 9 stale permits)   ← identical to master
```

So the two accessors live at the **end** of `claude_client.go`, with the reason
written where the next author will read it. Appending is the only placement that
shifts nothing.

## 4c. THE REAL SESSION, WITH THE COMPACTION LINE (deliverable 3 — the acceptance this epic named)

Session `bdf97970-a71d-47eb-ab1f-3b3f6b2088e5`, its own stdout, verbatim:

```
[gopi] memory compaction: summarized 7 oldest message(s) into a memory block
[gopi] ⚠️ 🧠 context window: est 71839 tokens (measured 36267) > 31500 (171.0% of 42000) — compacted: heuristic 55788 → 21855 tokens (132.8% → 52.0%, target 35.0%) — assertion: after ≤ target+summary = false (measured-before=71839 heuristic-after=21855 limit=31500)
```

`(measured 36267)` — **non-zero**, in a real chat run against the real provider, on a
line that prints under the compaction branch (the branch that prints `measured` at
all). Reproduce:

```
printf '用 read 工具读 CONFIG.md 第 1 页，然后只回复 ok1\n…offset=200…\n…offset=400…\n…offset=600…\n…offset=800…\n/quit\n' \
  | GOPI_CONTEXT_WINDOW=42000 GOPI_COMPACT_TRIGGER=0.75 GOPI_COMPACT_TARGET=0.35 GOPI_COMPACT_KEEP_RECENT=1 \
    ./bin/gopi chat --once --tools
```

**And the number is provably the API's, not a coincidence of scale.** The same
session's raw log holds 10 `message_start` frames; its FIRST one is:

```
36267 {'input_tokens': 33963, 'cache_creation_input_tokens': 0, 'cache_read_input_tokens': 2304, 'output_tokens': 0}
```

36267 = 33963 + 2304, and the log line printed **36267**. The trigger fired after
that session's first LLM round — so the count it judged came from that round's
`message_start`, i.e. from the arm this change added; the old code would have left
`PromptTokens` at 0 for that round and printed `(measured 0)`.

Artifact kept with the session (Rule 37 — a chat's files live in its own session
directory): `/home/user/.gopi/sessions/bdf97970-a71d-47eb-ab1f-3b3f6b2088e5/artifacts/measured-36267-evidence.txt`.

**Why earlier real runs only reached the `— trimmed …` line** (worth recording: it
is a property of the compaction gate, not of this fix). `trimContext` returns early
to `trimToFitStickyHead` — whose line carries NO `measured` field — whenever
`compactionPaused()` is true, and a first trigger with an empty compressible window
arms `compactStructuralNoOpUntil` (a ~5-minute backoff). A short `--once` script
with a small window trips that first, and then every later turn prints the trim
line. The window has to be large enough that the FIRST trigger finds ≥
`CompactKeepRecent+2` messages to compress — 42000 with `KEEP_RECENT=1` did it.

## 5. Not Done (honest)

- **`--forward`-style display work and the `ctx used` status row are untouched** —
  out of scope by the epic, and owned by task `01a10dd01b8a7f0b873bf7f4b899a810`.
  This change touches neither the formatter nor the row geometry.
- **The streaming assertion still runs through a scripted SSE server** (Rule 27
  forbids manufacturing load; a stub is the sanctioned instrument) — the
  end-to-end path is covered instead by §4c's real session, whose measured value is
  matched frame-for-frame against the provider's own raw log.
- **`TestDocAnchorsResolveInNormativeDocs` is red on this tree, and was red on
  master before this change** (9 new / 9 stale drift entries in both). The number
  is identical to master's, which is the property this change had to preserve — see
  §4b for the placement decision that keeps it there. It is not this task's defect
  and not fixed here.

## 6. Evidence (self-filed)

- `grep -o '"usage":{[^}]*}' <session>/8981132f…raw.log | sort | uniq -c` → frames
  both from `message_start` (output_tokens 0) and `message_delta` (output_tokens N)
  with identical prompt-side counters. Scripted count: `starts 144 deltas 144`,
  `input_tokens==0: 0`.
- Probe run output quoted in §1 (`AFTER STREAM: PromptTokens=10`, `LOG: … (measured 0)`,
  `AFTER COMPACT: PromptTokens=0`).
- Red proof: `go test -run 'TestStreamUsageIsMeasuredWithoutAMessageDelta|TestCompactionLogNamesTheMeasuredCountAtTrigger'` against the reverted tree
  → `--- FAIL: TestStreamUsageIsMeasuredWithoutAMessageDelta` / `--- FAIL: TestCompactionLogNamesTheMeasuredCountAtTrigger`; same two PASS with the change restored.
- Real session `ce556db1-82a3-48e0-a792-c0acb704367a` console log, quoted verbatim in §4.
- Real session `bdf97970-a71d-47eb-ab1f-3b3f6b2088e5` stdout (`/tmp/measure-probe7.out:93`):
  `est 71839 tokens (measured 36267)`; that session's raw log's first `message_start`
  frame is `36267 = 33963 + 2304`. Artifact:
  `/home/user/.gopi/sessions/bdf97970-…/artifacts/measured-36267-evidence.txt`.
- Pre-existing-red baseline, measured on both trees in the same session:
  `TestDocAnchorsResolveInNormativeDocs` → `9 new, 9 stale permits` on master AND on
  this branch (the earlier placement: `10 new, 9 stale` — the delta §4b removes).

## 6b. The delivery-point sync — master merged, and the gates re-run ON THE MERGED TREE

While this branch was being written, master moved 12 commits ahead, and two of the
touched paths were ours: `internal/agent/loop.go` and `devdoc/DOC-INDEX.tsv`. Per
Rule 30a the alignment runs LAST, and per the measured lesson (memory #4910) **the
gates are run on the MERGED tree, never on the precondition tree** — a clean merge
can still be red.

```
git merge master   →  Merge made by the 'ort' strategy; 32 files changed, 3514 insertions(+), 54 deletions(-)
                      NO conflict; NO conflict markers anywhere under internal/ or devdoc/
HEAD f568ad47   master 6c30db40    git merge-base --is-ancestor master HEAD → exit 0
```

Master's own 12 commits added the `gopi notify --session … --message` direction
(`loadSessionInbox` in the SAME `loop.go` function region this change lives in), and
both features survive side by side in the merged file (grep: `measuredAtTrigger`/
`PromptSideTokens` present; `loadSessionInbox` present).

**The gates, re-run on the merged tree** (all in
`/home/user/workspace/gopi-01a10e3682ea7744876f06ef67dbf184`):

| gate | result |
|---|---|
| `go build ./cmd/agent/` (after the merge) | **BUILD OK**, `bin/gopi` 30 645 241 bytes |
| `gopi doccheck` (the merged DOC-INDEX, 2051 rows = master's 2050 + ours 1, `comm -23` lost-rows check → empty) | `the devdoc corpus is consistent` |
| `go test -run 'TestStreamUsage\|TestCompactionLog\|TestSetPromptTokens\|TestCompact_\|TestAddClaudeUsage\|TestPromptSideTokens\|TestClaudeUsage\|TestFoldLog\|TestStreamMessageStart' ./internal/agent ./internal/deepseek` | `ok …/internal/agent 4.478s` / `ok …/internal/deepseek 2.072s` |
| **real chat, the acceptance surface, re-run on the merged binary** | **session `cf1d9dd6-0b93-4a3c-b645-e01c0fe79b7e`** printed `est 48317 tokens (measured 36260) > 31500 (115.0% of 42000) — compacted…`, and that session's raw log's FIRST `message_start` frame is `33956 + 0 + 2304 = 36260` — digit-for-digit. Artifact: the session's own `artifacts/measured-36260-after-master-merge.txt` |

**Note on the merged `TestDocAnchorsResolveInNormativeDocs`** — this run was
measured on BOTH trees (`master` read from the object store in a `--detach`
worktree, never a foreign worktree used as scratch — Rule 29), and the numbers are
**identical**: `2952 anchors across 644 normative docs … 240 permitted, 16 new, 10
stale permits` on each, with the 14 `new`-drift entries the SAME 14 entries in both
(`diff` of the two normalized sets → empty). §4b's `9 new / 9 stale` figure was
master's number **before** master itself moved these 12 commits (`internal/tools/
job.go` and `cmd/agent/cli_tasks.go` are among the files master's newer commits
shifted); the property §4b protects is unchanged and re-measured here: **this
branch adds zero drift**, because the count on the merged tree equals the count on
the master it merged. The two accessors stay at the end of `claude_client.go`.

## 7. Files

- `internal/deepseek/claude_client.go` — `PromptSideTokens`, `HasPromptMeasurement`, `AddClaudeUsage`
- `internal/agent/loop.go` — `message_start` calibrates `Context`; `measuredAtTrigger`; the two other `SetPromptTokens` sites read the shared accessors
- `internal/agent/stream_usage_measure_test.go` — 4 tests (new)
