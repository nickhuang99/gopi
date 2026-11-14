<!-- doc-class: historical -->
# Should the chat/ui screen dump be ARMED BY DEFAULT?

> **RESOLVED for the chat surface, 2026-10-01 (task `01a0f720`).** The operator
> took arm (3): `gopi chat` now writes its dump BY DEFAULT, session-scoped, with
> the documented decline `--no-dump-screen` / a falsey `GOPI_CHAT_DUMP`. The
> decision, the measured cost basis and the traps are in
> `devdoc/chat-dump-default-armed.md`. The `gopi ui` surface is still opt-in and
> this document's question therefore stands for it.

**Status: DECISION OPEN — the behaviour is UNCHANGED (opt-in), and this document
is the question, the evidence and a recommendation for the operator.**

Task: `01a0e070-d783-72b0-adc7-c5d1cda6bf53`
Branch: `task/01a0e070d78372b0adc7c5d1cda6bf53`
Date: 2026-09-27
Subject: `gopi chat --dump-screen` / `gopi ui --dump-screen`,
`GOPI_CHAT_DUMP` / `GOPI_UI_DUMP`, and what an UNARMED
`gopi ui_state --compare` says about it.

---

## 0. The operator decision slot (OPEN)

Nothing in this task flipped a default. The dumped screen is a record of what the
operator's own terminal showed — model output, prompts, and whatever else was on
that screen — and every arm of the choice below costs something the operator has
not been asked about. So the question is recorded, exactly as it was put:

> **"Should `gopi chat` / `gopi ui` write a screen dump by DEFAULT (no flag, no
> env var), so that `gopi ui_state --compare` can audit the surface a gopi
> process actually painted? Today it is opt-in, and on this host
> `~/.gopi/sessions` holds 191 chat dumps and 0 ui dumps — i.e. the flag is
> effectively never armed, so the audit path is blind unless someone armed it in
> advance."**

The three arms, as the operator would weigh them:

| arm | what it buys | what it costs |
|---|---|---|
| **(1) UNCHANGED — opt-in** | no dump exists unless asked for; a run's byte stream is unchanged; no screen content on disk that nobody asked for | the audit path stays blind in practice (measured below: 0 ui dumps on this host) |
| **(2) DEFAULT ON (write one per surface run)** | every surface a gopi process paints is auditable after the fact — the exact case `ui_state --compare` exists for | a screen record lands on disk for **every** run, including runs nobody will audit; disk churn; and the file contains the screen's contents |
| **(3) DEFAULT ON, but SESSION-SCOPED and GC'd** | as (2), with the record pinned inside the run's own session dir and collected by an existing sweep rather than growing forever | still a record per run; and the retention question (how long, who deletes) becomes live |

**RECOMMENDATION: arm it by default, session-scoped, in the shape of arm (3)** —
because the measured failure is not "a dump exists that nobody wanted", it is
"the audit has never once been possible on an opt-in surface" — but the decision
is the operator's, and the default is **UNCHANGED** until they make it.

---

## 1. The explicit statement: the default is UNCHANGED

```text
no default was flipped by this task.
```

- `gopi chat` still writes a dump only with `--dump-screen[=PATH]` or
  `GOPI_CHAT_DUMP`; `GOPI_CHAT_DUMP` is a pre-existing knob `CONFIG.md` already
  catalogues (row `GOPI_CHAT_DUMP`, `cmd/agent/chat_dump.go:103`).
- `gopi ui` still writes one only with `--dump-screen[=PATH]` or `GOPI_UI_DUMP`
  (`CONFIG.md` rows `GOPI_UI_DUMP`, `GOPI_UI_DUMP_MODE`).
- **No knob was added** (see §5) — there is nothing new for `CONFIG.md` to carry.
- A run with no flag and no env var is byte-identical to what it was: the sink is
  nil and the surface's writer is returned unchanged
  (`newChatInputWithDumper`, `chatDumpWriter` — nil sink returns `out` verbatim).

The only change this task ships is to the **refusal path** (§4): an unarmed
compare now names the arming flag instead of only reporting the absence.

---

## 2. The measured cost basis (the number the recommendation rests on)

Measured on this host, 2026-09-27:

| fact | measurement | how |
|---|---|---|
| chat dumps on hand | **191** | `find ~/.gopi/sessions -name 'chat-screen.dump' \| wc -l` |
| ui dumps on hand | **0** | `find ~/.gopi/sessions -name 'ui-screen.dump' \| wc -l` |
| session dirs in the profile | 77497 | `ls ~/.gopi/sessions \| wc -l` |
| dump size, median / max | **5695 B / 9369 B** | `find … -printf '%s\n' \| sort -n` (min 466) |
| refresh cadence | `dumpMinInterval = 100ms` | `internal/uiclient/dump.go:71` — the file is rewritten at most once per 100 ms, temp+rename |
| refresh shape | throttled SAMPLE, not a frame log | `frames=` counts render writes, `dumps=` counts file refreshes; measured 90 vs a handful |

Reading the table:

- **The 191-vs-0 asymmetry is the actual defect the epic names.** 191 chat dumps
  exist because *someone armed those runs by hand* (they are evidence of the
  prior self-observation tasks, under `evidence/` subdirectories); `0` ui dumps
  exist because nobody ever armed a `gopi ui` run. The flag is not "rarely
  used": on the ui surface it has never been used at all.
- **The cost of arm (2)/(3) is not the file size.** A median 5.7 KB per run, held
  for the life of the session dir, is small next to the session's own `.jsonl`.
  The cost is (a) a screen record per run — a new class of artifact the operator
  has not agreed to keep, and (b) the fact that, being a throttled SAMPLE, it is
  **lossy**: it is not a replay log and must never be read as one.
- **Cost of the no-flag path is exactly zero** — the writer is returned
  unchanged when the sink is nil, so arming the default is a real behavioural
  choice, not a free one.

**Retention (Rule 35's four things).** Arm (3) would need the record collected by
something that already ticks: `internal/scratch`'s GC (`gopi scratch-gc`, the
existing `scratchGCTick`), which already sweeps aged, unowned scratch entries —
**no new daemon, no cron, no new state file**. This is *why* (3) and not (2) is
the recommendation. Were arm (1) kept, the existing session tree's own retention
applies unchanged.

---

## 3. Why the audit is currently blind (the loop, measured)

The self-observation loop has three steps (`devdoc/chat-self-observation-loop.md`):

```
1. ARM   gopi chat --dump-screen          (or GOPI_CHAT_DUMP=1)
2. FIND  gopi ui_state --target chat
3. CHECK gopi ui_state --compare "<line>" --session <session.jsonl> --target chat
```

Step 3 needs a dump written in step 1 — and step 1 is the step nobody takes
before the run whose display is in question. The check therefore reports on
nothing, which is a *correct* refusal (exit 1) but a useless one unless it tells
the reader how to have a dump next time. That is what §4 fixes.

---

## 4. The change that shipped: the refusal NAMES THE ROUTE OUT

### 4a. What it said before (measured, verbatim, pre-fix)

```console
$ GOPI_HOME=<empty profile> ./bin/gopi ui_state --compare "no dump was ever armed here" --target chat
gopi ui_state: no ui-screen.dump dump found in any scanned location
exit=1
```

One line. It names the artifact it looked for, and nothing else: no flag, no verb,
no sentence saying a dump is something you ARM. An agent reading it learns there
is no screen — not that a screen could be had for the price of a flag. That gap
is the reason the loop stayed unrun.

### 4b. What it says now (measured, verbatim, post-fix)

```console
$ GOPI_HOME=<empty profile> ./bin/gopi ui_state --compare "no dump was ever armed here" --target chat
exit=1
stderr: gopi ui_state: no ui-screen.dump dump found in any scanned location
stderr: a surface writes a dump only when it is armed: `gopi chat --dump-screen` (the chat surface, artifact chat-screen.dump) or `gopi ui --dump-screen` (the ui surface, artifact ui-screen.dump); without it the surface's byte stream is unchanged
```

and the machine rendering carries it as a field rather than a sentence to scrape:

```console
$ GOPI_HOME=<empty profile> ./bin/gopi ui_state --json --compare "x" --target chat
exit=1
{
  "tool": "ui_state",
  "refusal": "no ui-screen.dump dump found in any scanned location",
  "route_out": "a surface writes a dump only when it is armed: `gopi chat --dump-screen` (the chat surface, artifact chat-screen.dump) or `gopi ui --dump-screen` (the ui surface, artifact ui-screen.dump); without it the surface's byte stream is unchanged",
  "scanned": 0,
  …
}
```

- The absence is still reported first, and the exit code is still **1** (a check
  that could not run) — the fact and the route are two sentences, not one
  negotiation.
- The route is **added beside** `ErrNoCandidates`, never spliced into it: that
  error is embedded in other sentences (the `--list` view, the JSON `refusal`
  field), and an arming instruction inside it would leak into refusals whose
  remedy is something else entirely.
- It fires **only** on the absence, and the absence has TWO shapes — a scan that
  found nothing (`ErrNoCandidates`) and a `--session` naming a directory no dump
  belongs to (`NoDumpForSessionError`). Both are gated through ONE predicate,
  `uiclient.DumpAbsent(err)`; an ambiguity keeps its own remedy (`--pid`), and a
  `--list` that found a dump does not carry the sentence at all.
- Machine callers get it as a **field**, not a sentence to scrape:
  `"route_out": "a surface writes a dump only when it is armed: …"` in
  `gopi ui_state --json`.

The sentence is built from `uiclient.DumpNotArmedNotice`, which composes the flag
and both verbs from constants — a notice can never name a flag no surface writes
(Rule 13).

### 4c. Red → green

- **RED** (mechanism present, wiring absent): with `DumpNotArmedNotice` defined
  but printed nowhere, `go test ./cmd/agent/ -run TestUIStateUnarmed` fails
  exactly as the pre-fix binary behaves:

  ```text
  --- FAIL: TestUIStateUnarmedCompareNamesTheRouteOut (0.00s)
      cli_uistate_unarmed_route_test.go:60: an unarmed compare does not name the route out:
          gopi ui_state: no ui-screen.dump dump found in any scanned location
  --- FAIL: TestUIStateUnarmedCompareCarriesTheRouteAsAField (0.00s)
      cli_uistate_unarmed_route_test.go:114: JSON route_out = "", want the arming route "…"
  ```

- **GREEN**: with the wiring back, `go test ./cmd/agent/ -run 'TestUIStateUnarmed'`
  passes, together with the pre-existing arming contract
  (`TestChatDumpArming_*`, `TestUIStateRefusesAmbiguityThenAnswers`).

`cmd/agent/cli_uistate_unarmed_route_test.go` holds three legs: the route is named
on an absence (GREEN), it is NOT named when a dump was found (negative control —
a notice that fires on every run measures nothing), and `--json` carries it as
`route_out`.

---

## 5. No knob was added

```text
no knob added — CONFIG.md needs no new row for this task.
```

Adding one would have meant a new settings key or env var to control the
default, which is exactly the decision the operator has not made. The refusal
sentence is not configurable and needs no channel: it is the reader's own text
about its own artifact. `CONFIG.md`'s existing rows (`GOPI_CHAT_DUMP`,
`GOPI_UI_DUMP`, `GOPI_UI_DUMP_MODE`) already document the arm as it stands.

---

## 6. Rule 31 evidence: the surface EXECUTED for real

A real 117×26 pty (`pty.openpty` + `TIOCSWINSZ`), the real binary, a local SSE
stub model — harness and per-step logs under
`/home/user/.gopi/sessions/01a0e070-d783-72b0-adc7-c5d1cda6bf53/evidence/`
(`unarmed_vs_armed_loop.py`, `step-*.txt`, `loop-transcript.txt`).

**Unarmed (no dump on hand) — steps C and D, run with the FIXED binary** (the
harness's own C/D legs were captured before the rebuild and show the pre-fix
line; the post-fix wording is quoted in §4b from `bin/gopi`, exit 1):

```console
$ GOPI_HOME=<empty profile> ./bin/gopi ui_state --target chat
exit=1
stderr: gopi ui_state: no ui-screen.dump dump found in any scanned location
        a surface writes a dump only when it is armed: `gopi chat --dump-screen` …

$ GOPI_HOME=<empty profile> ./bin/gopi ui_state --compare "no dump was ever armed here" --target chat
exit=1   (same two lines — the absence, then the route out)
```

**Armed — the loop:**

```console
$ GOPI_HOME=<profile> ANTHROPIC_BASE_URL=http://127.0.0.1:<port>/anthropic \
    ./bin/gopi chat --dump-screen
# paints; the stub answers with the marker row; the REPL freezes the artifact at close-out
# record: #gopi-chat-dump … grid=117x26 frames=90

$ GOPI_HOME=<profile> ./bin/gopi ui_state --target chat
exit=0   (state=exited — the dump is the writer's last frame, said out loud)

$ GOPI_HOME=<profile> ./bin/gopi ui_state --compare "AUDIT-MARKER-ROW-PAINTED-OK" \
    --session <profile>/…/<session>/session.jsonl --target chat
exit=0   match  in_grid=true in_transcript=true   (agree — the painted row IS the transcript's text)

$ GOPI_HOME=<profile> ./bin/gopi ui_state --compare "AUDIT-MARKER-ROW-PAINTED-OK-ALTERED" \
    --session <profile>/…/<session>/session.jsonl --target chat
exit=3   mismatch  in_grid=false in_transcript=false
         reason="neither the transcript nor the grid has this line verbatim: …"
```

Exit codes as contracted: **0 agree / 3 disagree / 1 could not run**. The exact
transcripts, with stdout and stderr, are in `step-B-armed-compare-agree.txt` and
`step-E-armed-compare-disagree.txt`.

**A measured pitfall worth carrying** (it cost a run here): the marker MUST be
short enough to fit the grid row, because the dump's grid presents the band's
layout, where a long answer line WRAPS into several rows — a compare against the
unwrapped transcript line then reports `in_grid=false` on a paint that was in
fact faithful. That is a geometric fact about the band, not a lost line: check
`prefix=` before blaming the surface.

---

## 7. Not done (honest)

- **The default was not changed** — by design (constraint 1). This document is
  the decision slot, not the decision.
- **No retention path was built for a default-armed dump**, because no
  default-armed dump exists. If the operator chooses arm (2)/(3), the retention
  work is the existing `internal/scratch` GC sweep plus a stale-age policy, and
  it should be its own task.
- **The ui surface (`gopi ui --dump-screen`) was not exercised in a live pty
  here**: the chat surface carries the same sink and the same reader, and the
  audit loop was run end-to-end on chat. The ui verb's own discovery path
  (`ui-screen.dump`) was exercised only through the refusal legs (`0` ui dumps
  on this host — that absence is the epic's own motivating measurement, and it is
  reproduced in step C/D).
