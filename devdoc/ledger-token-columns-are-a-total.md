<!-- doc-class: normative -->
# The Ledger's Token Columns Are a TOTAL — the "PEAK" Label Was the Defect

**Class**: normative — one standing claim about what the `gopi session` family's
token numbers MEAN, plus the correction of a claim this same page made for one
commit. The columns and their fold are unchanged; what changed is the word, the
note, and the truth behind both.

> **THIS PAGE USED TO SAY "PEAK", AND THAT WAS WRONG** (9f00fd60c → corrected by
> task `01a1954e`, 2026-11-01). The withdrawn claim read: *"`input_tokens` is
> folded with `MAX`, therefore it is a high-water mark"*. The inference is from
> the SQL's SHAPE, and the shape misleads. The value being folded is not a
> per-request size — it is the `CostTracker`'s **cumulative** count, and `MAX`
> over a monotonically increasing series returns its **last** element, i.e. the
> session's running total. **The number was always a total; only the label was
> lying, and the label it was lying under was "peak".**

## The two facts that settle it

### 1. What the recorder is handed

`ChatRecordTurn`'s three operands are the tracker's cumulative fields, passed
whole — the same three at all three call sites:

```go
// cmd/agent/cli_chat.go:4584, cmd/agent/term.go:417, cmd/agent/cli_web.go:1605
ag.RecordChatTurn(ag.Cost.TotalPromptTokens, ag.Cost.TotalCompletionTokens,
    ag.Cost.TotalCacheHitTokens+ag.Cost.TotalCacheMissTokens)
```

and those fields only ever advance, one increment per API REQUEST —
`internal/deepseek/claude_client.go:303`:

```go
ct.TotalPromptTokens += u.PromptSideTokens()
```

So the series the SQL folds is `t₁ ≤ t₁+t₂ ≤ t₁+t₂+t₃ ≤ …`, monotonically
non-decreasing, and `MAX` of it is its final value. The **only** thing the `MAX`
buys is idempotence: a replayed or out-of-order write cannot shrink the row.
**Do NOT "fix" the fold** — a sum of these same operands would count every
earlier turn once per later turn, which is the arithmetic error the withdrawn
page worried about and then walked straight into from the other side.

### 2. The measurement (real ledger, real wire log, 2026-11-01)

Session `d6120d04-98fa-454d-a2f0-51ea7c661956` — three requests, six wire `usage`
blocks (each request emits a `message_start` block and a `message_delta` block;
the tracker absorbs the delta when one was seen). The prompt side is
`input_tokens + cache_creation_input_tokens + cache_read_input_tokens`:

```bash
python3 - <<'PY'
import re
sid="d6120d04-98fa-454d-a2f0-51ea7c661956"
p="/home/user/.gopi/sessions/%s/%s.raw.log"%(sid,sid)
rx=re.compile(r'"usage":\{([^}]*)\}')
def num(b,k):
    m=re.search(r'"%s":(-?\d+)'%k,b); return int(m.group(1)) if m else 0
blocks=[]; cur=[]
for line in open(p,errors='replace'):
    if line.startswith('→ REQUEST'):
        if cur: blocks.append(cur)
        cur=[]; continue
    if '"usage"' not in line: continue
    for m in rx.finditer(line):
        b=m.group(1)
        cur.append((num(b,'input_tokens'),num(b,'cache_creation_input_tokens'),
                    num(b,'cache_read_input_tokens'),num(b,'output_tokens')))
if cur: blocks.append(cur)
sel=[b[-1] if len(b)>1 else b[0] for b in blocks if b]
print("per-request prompt side:", [a+b+c for a,b,c,_ in sel])
print("SUM =", sum(a+b+c for a,b,c,_ in sel), " MAX single =", max(a+b+c for a,b,c,_ in sel))
PY
```

```
per-request prompt side: [36455, 36576, 36674]
SUM = 109705  MAX single = 36674
```

The ledger row for that session:

```bash
python3 -c "
import sqlite3
con=sqlite3.connect('file:/home/user/.gopi/memory.db?mode=ro',uri=True)
print(list(con.execute(\"select input_tokens,cache_tokens from chats where session_id='d6120d04-98fa-454d-a2f0-51ea7c661956'\")))"
```

```
[(109705, 109705)]
```

**`109705` is the SUM, not the `36674` peak.** Confirmed a second time on a
longer session, `34bb9adc-0618-4e05-b849-6bd15757dec6`: 117 requests, symmetric
sum `8086768`, per-request maximum `102312` — the stored column again equals the
sum and is 79× the peak. The stored number is the session's running total at the
moment of its last recorded turn.

**Why `input_tokens == cache_tokens` on EVERY non-empty row written since
2026-08-31** is an algebraic identity, not a provider's quirk and not a second
bug — and getting this right matters because the wrong explanation is the
tempting one:

```go
// internal/deepseek/claude_client.go:303 — the ONLY accumulator on the wire
ct.TotalPromptTokens += u.PromptSideTokens()                       // Input + CacheCreation + CacheRead
ct.TotalCacheHitTokens += u.CacheReadInputTokens
ct.TotalCacheMissTokens += u.CacheCreationInputTokens + u.InputTokens
```

The third operand the CLI records is `TotalCacheHitTokens + TotalCacheMissTokens`
= `CacheRead + (CacheCreation + Input)` — **the same sum, term for term**, as
`TotalPromptTokens`. So the two columns are equal by construction on this path,
and the measured ledger agrees — **on the non-empty rows** (the predicate is part
of the number, not a detail: `input_tokens > 0`): `deepseek-flash` 123 such rows,
0 differing (`started_at` 2026-09-11 → 2026-10-31; its newest row ENDS 2026-11-01,
which is a different column — the range above is measured on `started_at` alone);
`deepseek-v4-pro` 2 such rows, 0 differing.

That predicate is load-bearing, because the table also holds **83 rows with
`input_tokens = 0`** — empty shells whose two columns are trivially equal
because nothing has been written yet. `chats` is **274** rows in total, and it
partitions exactly:

| rows | what they are |
|---|---|
| 83 | `input_tokens = 0` — an empty shell, equal for want of a value |
| 125 | `input_tokens > 0` and equal — the identity above, all of them written 2026-08-31 or later |
| 66 | `input_tokens <> cache_tokens` — the older write shape below, all of them written before 2026-08-31 |

A reader who counts 190 `deepseek-flash` rows and finds "123 equal" written here
would be right to call this page wrong; the missing word is `non-empty`.

The 66 rows that DO differ are all older than 2026-08-31 (newest differing row:
2026-08-30 08:14), while the OLDEST non-empty equal row is `deepseek-v4-pro`
2026-09-10 12:40 — the split is total, not approximate: 125 of 125 non-empty rows
at or after 2026-08-31 are equal, and 66 of 66 before it are not. They sit under
THREE other models
(`glm-5.3-flash` 50, `deepseek-v4-flash` 14, `hy4-preview` 2 — and no row of
either deepseek model differs at all) — i.e. they were
written by an EARLIER formula, before the `+ u.InputTokens` term reached
`TotalCacheMissTokens`. Those gaps are a historical write-shape, **not** a live
disagreement, and reading them as "the provider reported an uncached remainder"
would be inventing a cause: the identity above holds regardless of what the
provider reports, because `PromptSideTokens` and the cache operand are built from
the same three wire fields.

**What that means for a reader of the ledger**: `cache_tokens` does not tell you
the CACHE-HIT count, because it is the whole prompt side. And the session log's
own rows do not rescue it — they carry cumulative readings on the chat paths too.
Measured on `d6120d04`, all three `request` rows:

| row | prompt_tokens | cache_hit | cache_miss | hit+miss |
|---|---|---|---|---|
| 0 | 36455 | 0 | 36455 | 36455 |
| 1 | 73031 | 36352 | 36679 | 73031 |
| 2 | 109705 | 72832 | 36873 | 109705 |

Each row's `hit+miss` equals that row's cumulative `prompt_tokens`, so the split
is cumulative as well; the last row (72832 / 36873) is the session's final split
and sums to the stored `cache_tokens`. A consumer that wants a PER-REQUEST split
must read `gopi session raw` and take each frame's own numbers — summing this
file's columns gives 219191 for prompt, which is the same over-count that makes
`session log`'s COST block wrong.

## What the withdrawn page got right, and what it missed

It was right that **the two surfaces print different quantities and both said
only "tokens"**, and right that fixing the FOLD would be wrong. It went wrong in
one step: it named the quantity from the fold's shape instead of from the
recorder's input, and it then had to explain a large gap (`8086768` vs
`393,314,088`) as "peak vs sum" when the truth was **"sum vs sum-of-sums"**. The
second half of that gap was never this page's business at all — it belongs to
`session stats`, which is where the next section goes.

## The second defect this page must NOT paper over

The withdrawn note told the reader to reconcile the two numbers by running
`gopi session stats`, calling its `prompt tokens` "the SUM over requests". **That
is false on the chat paths**, and it is now measured:

- `stats` adds up the SESSION LOG's own `request` rows
  (`internal/gopilog/stats.go`); it does not read the ledger.
- On the **task** path those rows carry the per-request **DELTA**
  (`internal/agent/loop.go:5105`: `p = a.Cost.TotalPromptTokens - prevPrompt`),
  so a task run's sum is a genuine per-request sum.
- On the **chat** paths they carry the **CUMULATIVE** reading
  (`internal/agent/chat_ops.go:838` assigns `p = a.Cost.TotalPromptTokens`, and
  `:843` hands that `p` to `LogRequest` unmodified), so a chat's "sum" is a sum of
  running totals — counting turn 1 once per later turn.

**Measured on a COMPLETED session, so the number cannot move under the reader**
(`d6120d04-98fa-454d-a2f0-51ea7c661956`, 3 requests, ended 2026-10-24):

```bash
$ ./bin/gopi session stats d6120d04-98fa-454d-a2f0-51ea7c661956
│ requests       3
│ prompt tokens  219,191   (avg 73,063/request)
│   per-request  min 36,455 │ max 109,705
```

The truth is **`109,705`** — the ledger's own column, and the session's running
total. `219,191` is `36455 + 73031 + 109705`: each cumulative row counted again,
so the sum is `sum(cumulative)` where the truth is `max(cumulative)`.

**The inflation factor is `sum(cumulative) / final` and it is NOT a function of
the turn count alone** — an earlier draft of this page said "roughly k/2" and the
second measurement refuted it. Measured on two completed chats:

| session | turns k | reported | true | ratio | k/2 would be |
|---|---|---|---|---|---|
| `d6120d04` | 3 | 219,191 | 109,705 | **2.0×** | 1.5 |
| `34bb9adc` | 117 | 393,314,088 | 8,086,768 | **48.6×** | 58.5 |

Both rows are monotonic (checked), and both ratios differ from `k/2`, in opposite
directions. **The factor tracks the SHAPE of the growth, and the direction is
counter-intuitive enough to write down**: `d6120d04`'s increments are near-uniform
(36455, 36576, 36674 — a growing context that has not yet hit a plateau), so its
`(k+1)/2` lands almost exactly. `34bb9adc`'s increments ACCELERATE — its 117 rows
in four quarters average 41,678 → 58,316 → 79,746 → 95,810 — so its cumulative
curve is concave-up and the sum sits BELOW the linear-growth estimate: 48.6×
instead of 58.5×. (An accelerating series makes the average of the cumulative
values closer to the final one; a decelerating series would push it the other way.) **The only safe statement is the mechanism**
(the sum re-counts every earlier turn), never a multiplier — the multiplier is a
property of one session's growth curve, and quoting it is how this page got a
wrong figure in the first place.

**A LIVE session was measured first and its figures are deliberately NOT cited**
(2026-11-01, Rule 38). `e1e22e9f` printed `prompt tokens 250,670,748,726` at one
moment, then `270,822,754,328`, then `271,656,602,686` within a few minutes —
its request count went 1905 → 1982 → more while the page was being written. Any
figure quoted from it is a reading of an instant, not a property of the code, so
it cannot serve as evidence a later reader can check. The completed session above
can, and it shows the same defect a thousandfold more cheaply.

**Consequence for this page**: the new note names the session LOG as where
per-request figures live and deliberately does **not** send the reader to
`stats` for a total. `stats`'s own defect — a sum over rows whose quantity varies
by path — is filed as its own row rather than fixed here: the fix belongs to that
reader (it must take the delta, or the last value per counter segment, and say
which), and a label change in `session show` cannot repair arithmetic in
another verb (Rule 28: one change, one owner).

## What changed (one fact, four surfaces)

1. **`internal/tools/chat.go` — `ChatTokenCountsFormat`** now renders
   `total tok in=%d out=%d cache=%d`. One constant, referenced by the index row
   (`chatTokenClause`) and the summary page (`sessionTokensValue`), so the two
   faces cannot drift (Rule 1/13).
2. **`ChatTokenBasisNote`** (`var []string`) replaces the withdrawn note: it says
   the columns are RUNNING TOTALS, names the `MAX` fold and what it buys
   (idempotence), and points at the RAW WIRE LOG for per-request figures. The
   command is spelled from `cliverbs.Program` + `cliverbs.SessionRawAddress`, so
   a verb rename moves the sentence with it. It is a slice of lines because the
   show page renders it in the value column's indented slot.
   **This pointer was corrected by measurement, twice.** It first named
   `gopi session stats`; then `gopi session log`; running the second on
   `d6120d04` showed it prints a SUMMARY COST block, and its `Prompt: 219,191`
   is the sum of rows that carry CUMULATIVE readings on the chat paths — the
   same defect as `stats`, reported under a different verb. `gopi session raw`
   is the surface that prints the wire frames (`"input_tokens":36455` verified),
   so that is what the note names, and the test now FAILS if the note sends a
   reader to a derived summary.
   **`ChatTokenBasisNoteText()`** joins it for messages and assertions.
3. **`sessionTotalTokensKey = "token total"`** — the show page's key column says
   `token total`, not `peak tokens`, because a reader scans the key column first.
   **Eleven cells, and that ceiling is enforced by the column, not by taste**: the
   page renders `%-*s%s` at `sessionShowKeyWidth = 12`, so a 12-cell key leaves
   ZERO separating cells and the value is welded to the label. The first spelling
   tried here was `total tokens` — exactly 12 — and the shipped page read
   `total tokenstotal tok in=109705`. Every substring assertion passed, because a
   glued line contains both fragments; the test now asserts the SEPARATED form.
4. **The JSON face publishes `tokens_basis: "session-total"`**
   (`ChatTokenBasisWord`). The wire keys keep their spelling — renaming
   `input_tokens` breaks every existing reader, and silently re-pointing a key's
   MEANING is the breakage Rule 15 forbids — so the fact rides a separate field.
   **The value was `per-request-peak` for exactly one commit**; it shipped and
   was corrected the same day with no reader built on it, and leaving a
   permanently false machine-readable value would be worse than the rename.

## Verification (Rule 31: the surfaces were RUN)

```
$ ./bin/gopi session show d6120d04-98fa-454d-a2f0-51ea7c661956
  token total total tok in=109705 out=262 cache=109705
               these are this session's RUNNING TOTALS, not per-request peaks: the recorder
               is handed the tracker's cumulative count each turn, and the MAX fold makes
               the last write win so a replay can never shrink it. Per-request figures live
               in the RAW WIRE LOG — `gopi session raw` prints each request.

$ ./bin/gopi session list --all --limit 2600 | grep d6120d04
turns 3, total tok in=109705 out=262 cache=109705

$ ./bin/gopi session show d6120d04-98fa-454d-a2f0-51ea7c661956 --json
{'input_tokens': 109705, 'output_tokens': 262, 'cache_tokens': 109705,
 'tokens_basis': 'session-total', 'tokens_reported': True}
```

Both `cache_tokens` values above read **109705** — the whole prompt side, per the
identity in §2 — and an earlier draft of this block printed `72832` in both
places. That was not a typo to shrug at: `72832` is a real number, but it is the
last request's `cache_hit`, i.e. the number this page exists to stop readers from
expecting in that column. A verification block that shows the wrong reading is
worse than no block, so these lines are copied from the commands above.

Red proof — the fixture (`internal/tools/chat_token_total_vs_peak_test.go`) is
built so the two quantities COULD NOT be confused: the per-request prompt sides
are `100` then `200`, so the cumulative column reads `100` then `300`; the SUM is
`300` and the largest SINGLE reading is `200` (`chatTokenTotalFixturePeak` is
derived from the second request, never restated). The discriminating arm asserts
both that the column equals the sum **and** that it is not the peak, and the
fixture's own premise is checked first — a fixture that stopped separating them
fails loudly there (`fixture cannot separate a peak from a total`) instead of
letting every later line pass vacuously. Restoring the withdrawn `peak tok in=`
format turns `TestLedgerTokenColumnsAreARunningTotal` red and names the peak the
reader would have mistaken for the value.

**Note the trap that fixture avoids**: with cumulative totals `300` and `300` the
sum and the peak coincide and the arm proves nothing. Two requests whose prompt
sides are equal would have produced exactly that, which is why the fixture uses
`100`/`200` and asserts the separation before using it.

## What this page does NOT say

- It does not say the ledger is wrong. The `MAX` fold is correct and stays.
- It does not say `session stats` — or `session log`'s COST block — is the
  authority for spend. On a chat path BOTH are inflated by the same mechanism
  (they sum rows that carry cumulative readings), and both are separate defects
  with a separate owner: on the COMPLETED session `d6120d04` — whose true total is
  109,705 — `session log` printed `Prompt: 219,191` and `session stats` printed
  `prompt tokens 219,191`, the same wrong number from two verbs. They are filed
  on the ledger as `01a195b0-0912-7bb4-9247-da2352e3f125` (the summing reader in
  `internal/gopilog/reader.go:472` and the two verbs built on it), which also
  carries the third shape a fix must survive: a session whose counter RESETS
  mid-run (`66243525-16ab-4ce7-9133-84d74c539840` resets twice — line 1581,
  44,155,810 → 142,860; line 2915, 40,741,990 → 137,568), so "take the last
  value" is as wrong there as summing is here.
- It adds no knob, no column, no migration. A label, a note, and one wire field.
