<!-- doc-class: historical -->
# Session Reporting: "Unknown" Is Not "Zero", and "Log Elsewhere" Is Not "No Such Session"

**Class**: normative — two standing claims about how the `gopi session` family
renders a fact it did not measure. Both are Rule 15 (no silent failure) repairs
at the DISPLAY layer; neither changes what is stored.

## The two defects this closes

Both were found by running the real binary against the real profile, not by
reading code.

**(B) an unmeasured zero printed as a measured zero.** `ChatRecordTurn` folds
the provider's usage into `chats.{input,output,cache}_tokens`. When no positive
count is ever reported, those columns keep the schema DEFAULT `0` — the same row
as a session that really spent nothing. `session show` and `session list` both
printed `tok in=0 out=0 cache=0`, so the operator could not tell "this cost
nothing" from "nobody measured this".

Measured on master (`session show 0f758ec3-…`, a real 1-turn chat):

```
  tokens      in=0 out=0 cache=0
```

**(C) a row whose log is on another machine was reported as a session that does
not exist.** `chats` is an INDEX over session JSONL files, so it also carries
rows pointing at another host's profile, a reclaimed scratch root, or a sandbox
that has since been collected. `session show <id>` answered every such id with
`no session with id … in the chat index` — a false statement about existence.

Worse, the same sentence was returned for a shape the operator can produce
locally at will: **3436 session logs sit in peerbox's own profile with no `chats`
row**, and `session show` called every one of them nonexistent:

```
$ gopi session show 00071f7f-08ec-4974-99c6-7d264e0df063   # master
gopi session show: chat: no session with id "00071f7f-…" in the chat index
$ ls -d ~/.gopi/sessions/00071f7f-…*                       # the log is right there
/home/user/.gopi/sessions/00071f7f-…/
```

## What changed

**Thin — the display layer only.** No schema change, no new column, no migration,
no dedup, and `ChatRecordTurn`'s `MAX()` monotone fold is untouched (it is what
keeps a replayed or out-of-order turn from shrinking a row, and it is correct).

### B — one criterion, two renderings

`internal/tools/chat.go`:

- `ChatTokensReported(turns, in, out, cache) bool` — the ONE derived criterion
  (`turns > 0 && (in > 0 || out > 0 || cache > 0)`), documented as split out so
  the `list` and `show` faces cannot grow two answers for one fact.
- `ChatSessionDetail.TokensReported` carries it; `chatScanDetails` (list) and
  `ChatShow` (one row) fill it from the same call.
- `ChatTokenCountsFormat` is the ONE measured-clause format (`tok in=%d out=%d
  cache=%d`) and `ChatTokenUsageNote` / `ChatTokensUnmeasuredNote` the two
  unknown-clause constants — Rule 13/14 single definition points.

`cmd/agent/cli_session.go` renders the `show` tokens line through
`sessionTokensValue`, which reads the same constant and the same format; the
`--json` row (`sessionJSONRow`) gains a STRUCTURED `tokens_reported`, so a
script summing `input_tokens` can skip the rows where `0` means "not measured"
without parsing prose.

### C — the not-found branch asks the existing criterion

`ChatSessionLogOnMachine(id)` reuses `chatSessionLogPresent` — **the same
function the list filter and the `show` presence fact already go through**. No
second probe was written (the constraint's Rule 13/14 requirement); the evidence
is that all three surfaces call the one function:

```
internal/tools/chat.go: chatSessionLogPresent  ← the single implementation
  ├─ chatPresenceProbe.present      (list page filter)
  ├─ ChatShow                       (show's FilePresent)
  └─ ChatSessionLogOnMachine        (the not-found branch)
```

The not-found branch now splits into three answers instead of one:

| what is true | answer |
|---|---|
| row present, log HERE | the summary (`log file  present`), no note |
| row present, log NOT here | the summary + `index` line naming the next step |
| no row, log HERE | `ChatSessionNotIndexedError` — names the gap and `gopi chat backfill` |
| no row, no log | unchanged `no session with id …` |

The `[hidden] N session(s) hidden — their session log is not on this machine`
line and `--all` were already correct and are untouched.

## The wording correction (found by measuring the bytes, not the claim)

The first draft of both constants said *"the API reported no usage"*. The task's
own acceptance spelled the expected evidence as "raw log usage 全零字节".
Reading the actual raw log falsified that premise for the repro session:

```
$ grep -roh '"usage":{[^}]*}' ~/.gopi/sessions/0f758ec3-…/ | sort | uniq -c
"usage":{"input_tokens":0,"output_tokens":0}      # × 6 — present, and all zero
```

A usage block WAS sent; every counter in it was `0`, so nothing reached
`CostTracker`. Compared with a session that does fold real numbers:

```
"usage":{"input_tokens":13256,"cache_read_input_tokens":1152,"output_tokens":0,…}
```

So "the API reported no usage" would itself have been a false claim about bytes
that are in the log. The criterion is unchanged (both shapes are "not
measured"), and the constants now say what is actually known:

```
tok unknown — no positive usage was ever reported
tokens  unknown — no positive token count was ever reported for this session:
        every recorded turn carried either no usage block or all-zero counters,
        so the stored token columns hold their default 0 rather than a measured
        spend
```

This is the Rule 15 discipline applied to the fix for a Rule 15 defect: the
message has to describe the real reality, including when the reality is "we
cannot tell these two shapes apart, and both mean we have no number".

## Evidence

Red proofs were taken with a binary built from **unmodified master**
(`git archive master`, built off-worktree), green proofs from this branch.

**B — red (master) → green (branch)**

```
master:  tokens      in=0 out=0 cache=0
master:  0f758ec3-…  (…, turns 1, tok in=0 out=0 cache=0, started 2026-10-04 17:44, …)

branch:  tokens      unknown — no positive token count was ever reported …
branch:  0f758ec3-…  (…, turns 1, tok unknown — no positive usage was ever reported, …)
```

**B — counter-case (real numbers must survive)**

```
branch:  session 8fa8e910-…   tokens      tok in=1258325 out=40268 cache=1258325
```

**C — red (master) → green (branch)**

```
master:  gopi session show: chat: no session with id "a43ed4a8-…" in the chat index
branch:  log file    missing
branch:  index       the session EXISTS in the chat index here, but its session log
                     is NOT on this machine (--all on `gopi session list` lists the
                     row); the bytes are only on the machine that recorded the
                     session — copy that machine's sessions tree over, or re-index
                     the logs THIS machine holds with `gopi chat backfill`
```

**C — the local-log gap (3436 rows on this host)**

```
master:  gopi session show: chat: no session with id "00071f7f-…" in the chat index
branch:  gopi session show: chat: no row for session 00071f7f-… in the chat index,
         but this machine HAS a session log for it — re-index the logs this machine
         holds with `gopi chat backfill`, then try again
```

**C — counter-case (a truly unknown id keeps its sentence)**

```
branch:  gopi session show: chat: no session with id "deadbeef-0000-…" in the chat index
```

**One criterion, three surfaces** — the consistency test drives both real faces:

```
internal/tools/chat_token_usage_test.go
  TestChatShowAndListAgreeAboutTheLogPresence      list row vs show, same row
  TestChatSessionLogOnMachineIsTheSharedCriterion  id-only vs ChatShow verdict
```

## Not done

- **No stored flag for the token fact.** `TokensReported` is derived from the
  row, not persisted, so it can never disagree with the columns it describes. A
  session that genuinely spent exactly zero cannot be distinguished from one
  whose counters were all zero — gopi's data does not express the difference,
  and the wording now says so rather than pretending otherwise.
- **No attempt to separate "no usage block" from "all-zero usage block"** for
  an existing row: the raw log would have to be re-read per row, and both mean
  the same thing to a reader (no number). The criterion treats them alike.
- **Master's `session show` does not yet tell the operator the `index` line** —
  that is this branch's change, unreleased.
