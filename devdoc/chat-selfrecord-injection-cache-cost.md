<!-- doc-class: normative -->
# The chat surface's self-record contract — carrier choice, and the cache it costs

> **Provenance.** `TestDocAnchorsResolveInNormativeDocs` was retired on 2026-10-22 in `5a35f61` (deleted with `internal/docclass`), so a `file.go:NNN` reference in this page is a landing-time record, not an anchor a live gate enforces — nothing resolves one today. The line numbers stay useful to a reader; they are not checked by the suite. Citations below are quoted as they were written — where one of them reads as present tense ("is RED", "resolves", "fails"), read it as the state of THAT run, never as a gate this tree still has.

**Task**: `01a11df4af797b8091d80f8ce7e19b41` (child of epic `01a11db0`), branch
`task/01a11df4af797b8091d80f8ce7e19b41`, commit `218a09460`.

**What was wrong.** The `[IMPORTANT]` self-record contract existed on the TASK
path only. `gopi chat` never carried it and never called `recordSelfNotes`, so a
chat round's block was recorded **nowhere** — not into `self-record.md`, not into
the context carrier, not into memory. The two halves of the feature disagreed:
one path was taught and listened to; the other was neither.

## 1. The miss rate, measured before touching anything

Generator: `~/.gopi/sessions/<this-run>/missrate.py` (read-only over
`~/.gopi/sessions`). Scope: every directory whose `console.log` says
`chat table:` — 182 real chat sessions, **7314 rounds**.

**The denominator is `type=request` events.** `recordSelfNotes` is called once
per round (`loop.go:4564` before this change), so one round = one LLM call = one
`request` event. Counting `user` events instead undercounts the opportunity by
~5x (the same corpus: 1366 user events vs 7314 requests), because one chat turn
may take several tool rounds.

| arm | numerator rule | result | miss |
|---|---|---|---|
| A | a single assistant **content block** carries both markers | 2/7314 | 99.97% |
| B | A, with fenced code blocks stripped first | 2/7314 | 99.97% |
| C | B, with inline code spans stripped AND the opener **starting a line** | **0/7314** | **100.00%** |
| D | numerator over assistant text blocks instead (1339) | 2/1339 | 99.85% |

Scope C is the contract's own shape — `selfrecord.go` states the delimiters are
on their own lines *so a block is findable in prose*. **Both A/B "compliance"
hits are false positives**: they are session `dec7b68b`, where the model is
*analysing hermes' design* and writes the marker inside an inline code span and a
table cell —

```
| **注入规则** | `system_prompt.py:503` ← `SELF_RECORDING_PROMPT` | 每轮回答末尾必须追加 `[IMPORTANT]\n<本轮总结>…
```

So the true pre-change compliance on the chat path is **zero**, and the
segmentation is per content BLOCK (joining an event's blocks lets an opener and a
closer pair across them and manufactures a hit).

Two instruments, two lessons: strip the fence **and** the inline span, and
require the opener to start a line; and never count an event as compliant from a
joined text.

## 2. Why the carrier is the system prompt's TAIL, not a harness user message

The task path injects the contract as a second user message (`selfRecordRule`).
That carrier **cannot serve `gopi chat`**: `gopi chat` puts the operator's turn
on the wire as the first user message of *every* request, and consecutive user
text messages are **coalesced** into one (`internal/deepseek/claude_client.go`).
An injected user message would therefore arrive as an extra text block of the
operator's *own* turn — the turn boundary the console promises would be gone.
`maybeInjectSelfRecordRule` already refuses a plain prompt for exactly this
reason.

The system prompt has no such coupling, and the chat prompt is rebuilt per turn
from the same parts, so the append is byte-stable turn to turn. Placement is the
**tail** because the reply-language directive is the HEAD and the persona body
must start at the very next block (`TestChatPromptCarriesDirectiveAtHead`) — and
because a tail cannot move the bytes of any block that came before it.

One injection point, `maybeAppendChatSelfRecordRule`, called by **both**
assemblies (`assembleChatSystemPrompt`, `chatOpsSystemPrompt`), so "the chat
surface is taught the contract" has a single definition.

## 3. What the append costs — measured, not assumed

**Method.** Five independent one-turn `gopi chat` runs per binary, each in its
own `GOPI_HOME`. The **first request of each run is a cold prefix** (fresh
session, same prompt, same tools, same model), so its `cache_miss` is the price
of the prefix the binary sends, with no history in the way. Per-request DELTAS
of `session.jsonl`'s cumulative `cache_hit`/`cache_miss` — the raw totals are
session averages and are not the instrument.

| arm | first-request miss, 5 runs | median | system bytes |
|---|---|---|---|
| base | 22142 / 22142 / 22142 / 22159 / 22166 | 22142 | 12988 |
| new  | 22302 / 22313 / 22314 / 22314 / 22320 | 22314 | 13756 |

Delta = **+172 miss tokens**, the block's own bytes (768 chars ÷ ~4.5 ≈ 171).

> Erratum: the message of commit `218a09460` says "+161 median". That number is
> the block's byte-derived token estimate (768 chars ÷ 4.77), not the measured
> median delta, which is `+172` as tabulated above (mean delta `+162.4`). The
> table and the task report carry the measured figures; nothing in the code or
> the measurement changed.
**The two distributions do not overlap** (max base 22166 < min new 22302), so the
separation is clean and independent of run-to-run jitter.

**Nothing already cached was invalidated** — the block is a pure append. In a
warm 24-round conversation the per-request delta pattern is identical between
arms (the prefix grows by the same block); cumulative hit rate 94.75% → 95.61%,
which is the same 31k-token prefix counted against a slightly larger total.

**Wall time is NOT a usable comparator in this design** and is not reported as
evidence: the two arms DIVERGE in behaviour (the contract makes the answer
longer, which lengthens the next turn), so the median wall time difference
(7.14s → 8.94s) measures the model's output, not the prompt. The cold-prefix
miss-token count above is the stable quantity.

## 4. The capture half

`recordSelfNotes` is now reached from both chat paths — the plain path
(`loop.go`, the `chatOnce` accept branch) and the tool-enabled path
(`chat_ops.go`, the no-tool accept branch) — so the block the model writes lands
in this run's `self-record.md` beside the session JSONL. A chat records its
rounds the same way a task run does; the note is not duplicated by the two paths
(a turn takes exactly one of them).

**Live proof** (real binary `/tmp/gopi-new3`, real chat runs):

```
GOPI_HOME=/tmp/ab/fin/plain  gopi chat --once --turns 1 -p 'alpha=3 beta=7 gamma=11'
  → .../self-record.md  1 line   "- 本轮收到 alpha=3 beta=7 gamma=11 三个赋值但无动词，无工具可用，未执行任何操作；…"
GOPI_HOME=/tmp/ab/fin/tools  gopi chat --once --tools --turns 1 -p 'alpha=3 beta=7 gamma=11'
  → .../self-record.md  1 line   "- 收到 alpha=3/beta=7/gamma=11 裸参数三元组，未在本仓或记忆中检索到任何对应上下文（keyword_search 0 命中），故按数…"
```

## 5. The residue this change had to repair

`TestChatPromptDropsBlockWhenSectionIsEmptied` compared the served prompt
against `systemPromptWithOutputLanguage(chatSystemPrompt)` — a pre-contract
baseline. Once the contract is appended by the assembly, that comparison would
have read the contract itself as "separator residue left by an emptied section".
The test now compares against the pre-contract baseline **with the contract
appended**, and asserts the residue property directly (no doubled separator). The
distinction matters: the emptied-section contract is about a stray *separator*,
not about the assembly's own tail.

## Not done

- The `--tools` path's capture is wired and unit-tested, but the *live* proof
  above used a single-turn run whose answer happened to be a plain text round;
  a live multi-round tool-enabled conversation was not driven.
- Scope C (line-anchored) is the strictest reading and is the one the contract
  text supports, but the contract does not literally REQUIRE the opener to start
  a line — the extractor accepts a marker anywhere inside the text. The census
  reports all three scopes so the reader is not forced to accept one reading.
- One host, one model (`deepseek-flash`), one terminal geometry. Latency numbers
  from the raw-log watcher for the repeat arms were not usable (each run was
  `--turns 1`, so a single request's byte stream does not carry a second request
  to delimit it) — the warm-session arm's 19 vs 24 requests do carry them, and
  those are the TTFT figures; they are reported as a range, not as a verdict.

## Evidence (self-filed)

- `missrate.py` over `~/.gopi/sessions` → `sessions=182 rounds=7314 A=2 B=2 C=0`,
  miss 99.97% / 99.97% / 100.00%.
- `rep_report.py base new` → the two miss-token rows in §3.
- `go build ./...` → exit 0; `CGO_ENABLED=0 go build -o /tmp/gopi-new3 ./cmd/agent/` → exit 0.
- `go test ./internal/agent/ -run 'TestChat'` → ok 31.2s.
- `make test-budget PKGS='./internal/agent ./internal/standards'` → 3 failures,
  all reproduced on a clean `git archive master` tree in `/tmp/head-check`:
  `TestDocAnchorsResolveInNormativeDocs` (254 baseline-drift lines in both trees,
  identical), `TestCommandsDocGeneratedBlockMatchesSource`,
  `TestCommandsDocGateRedProofStaleGeneratedBlock`. Pre-existing, not this diff.
