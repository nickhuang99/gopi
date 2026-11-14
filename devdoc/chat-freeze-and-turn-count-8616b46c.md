<!-- doc-class: historical -->
# `gopi chat` session 8616b46c — the freeze, and the two turn counts

**Measured:** 2026-10-20 on this host (`hosta`), against the session's own
artifacts under `/home/user/.gopi/sessions/8616b46c-db23-41cb-a657-c3a519abd998/`
(session dir, 75 MB raw log). Branch base `43f202c`.

This document exists because the operator reported BOTH symptoms as
repeatedly-reported defects and asked for a diagnosis from the RAW log, not an
explanation. It separates what was measured from what was inferred, because the
2026-10-20 run before this one withdrew two claims that rested on a code comment
and a single observation (Rule 41 requirement 3).

---

## 1. What the artifacts say (measured, read-only)

| artifact | reading | command |
|---|---|---|
| `session.jsonl` | 702 rows | `wc -l` |
| `session.jsonl` | 165 `request` rows | `grep -c '"type":"request"'` |
| raw log | 166 `→ REQUEST` markers | `grep -c '^→ REQUEST'` |
| raw log, last line | `data: {"type":"message_stop"}` | `tail -1` |
| `session.jsonl`, last event | a `request` row, 08:04:38.089 | last row decode |
| `session.jsonl` | **no `session_end`** | `grep` for the event type |
| `session.dump.json` | `category: orphan-no-session-end`, written 08:39:19 | `cat` |
| `signal-provenance.jsonl` | **absent** | `ls` |

Numbered units, each naming its producer (see `internal/sessionevent/counters.go`,
which is the one registry these names come from):

- **rounds = 165** — one JSONL `request` row = one HTTP request/response.
- **raw HTTP requests = 166** — the raw log's `→ REQUEST` markers. One more than
  the JSONL because the raw log also records sends the JSONL never wrote.
- **turns = 15** — one *operator* row (the message a person typed) to the next.
- **loop passes** — would be `session_end`'s `total_iterations`; this session
  never wrote one, so a surface must derive it or say it cannot.

## 2. Symptom A — the freeze

**What is measured, and it is enough to exclude the guarded paths.**

The process stopped after the assistant turn it had just finished. The raw log
ends with a cleanly completed stream (`message_stop`), the JSONL's last row is
that turn's `request` row, and the screen dump — written at **08:04:38.206**, i.e.
during whatever ended the process — carries this status row:

```
deepseek-flash ·  turn16  ·  no tool   ·  144k/200k≡ ·↑18M↓1MΣ98.3%Δ99% · ready
```

and, two rows above it, a **shell prompt**:

```
you>
nick@hosta:~/work/gopi$ ^C tool ...
```

**The inference (labelled as such).** A shell prompt on the same screen means
`gopi` had already released the terminal and `bash` was reading it; `^C` echoed at
the prompt is the *shell's* echo. Both facts are consistent with: the terminal is
back in cooked mode (the editor restored it on its way out) and what the operator
typed went to the shell, which is why no keystroke reached gopi and why the
frozen screen kept showing `ready` — that row was painted before the exit and
nothing repainted it.

**Why that means the guarded paths did not run.** Every documented exit route
writes `session_end`:

- `/quit`, Ctrl+D, EOF → `finishChat` closeout;
- SIGINT/SIGTERM → the watcher at `cmd/agent/cli_chat.go:2226-2300` calls
  `ag.Interrupt()` and breaks to the same closeout (the comment there states the
  bug this fixed: *"a kill mid-turn or Ctrl+C at the prompt killed the process
  outright — session_end never written"*);
- a panic → `recoverRunPanic` (`internal/agent/crashdump.go`), which writes a
  dump *and* re-panics;
- `signalrec` records provenance for the modes that do not own their own
  handler, re-raising for the ones that do.

None of them happened: no `session_end`, no `signal-provenance.jsonl`, **and no
error-path crash dump** (the only dump present is the 08:39 *backfill*, whose
category says exactly that: no `session_end`, no request context).

**NOT DONE — stated honestly.** I could not reproduce the freeze and I did not
find the mechanism that killed the process without any of its guards firing.
What this run delivers for symptom A is the *shape* of the evidence, which is
what a future attempt needs:

1. absence of `session_end` + absence of a panic dump + absence of a signal
   record is **not** "the guards failed" — it is "the process stopped where no
   guard was reachable", i.e. a signal the handler never saw (`SIGKILL`), a
   `_exit` with no deferred work, or a death inside a syscall;
2. the terminal *was* restored, so this is not a crash mid-raw-mode;
3. the screen dump is the one artifact that lands at the moment of death
   (08:04:38.206, after the JSONL's last write at 08:04:38.104) — a run that
   hangs on purpose writing a small "I am about to exit" marker there would make
   the next occurrence decidable in one read.

Filing a root-cause task on this evidence is the next step; inventing a fix for a
mechanism I did not observe would be the exact defect Rule 41 requirement 3 is
about.

## 3. Symptom B — `round 165/165` versus `turn201 … turns typed: 201/201`

**This was a genuine defect, and it is fixed.** It is not a unit disagreement.

The two surfaces were counting DIFFERENT THINGS and only one of them was wrong:

| surface | value | unit | verdict |
|---|---|---|---|
| `gopi session replay <id>` | `rounds=165 … turns=15` | rounds / operator turns | **correct** |
| `gopi session log <id> --rounds` | 165 rounds | rounds | correct |
| `gopi session log <id> --turns` | 15 turns | operator turns | correct |
| `gopi chat --replay <id>` | `turn201`, `turns typed: 201/201` | **its own definition** | **WRONG** |

`internal/replaydrive.LoadRecording` built its `Turns` from **every `user` row
with string content**. This session has 202 of those, of which **187 are harness
injections** (`[system] …` task events and `[t+…]` clock ticks) and **15 are the
operator's own rows**. Every other surface counts a turn by the operator-row rule
(`sessionevent.IsHumanRoundBoundary` → the reader's `TurnBoundary`). The replay
was the only surface with its own definition, so it typed 187 lines the person
never typed onto the filmed screen — 35 766 bytes instead of 19 268 — and called
each one a turn.

**The fix** (`internal/replaydrive/replaydrive.go`): a user row that is a harness
injection is no longer a turn. It still SEAMS an exchange (`flush` runs), so
bounding is preserved; the predicate is `sessionevent.IsHumanRoundBoundary`
itself, not a second spelling of `"[system]"`, so the replay and the session
views cannot drift again.

**A/B on the very session in the report** (same file, two binaries):

```
PRE-FIX   injection budget: 1937 key unit(s) over 201 recorded turn(s), 35766 byte(s)
PRE-FIX   turns typed: 201/201
POST-FIX  injection budget:  624 key unit(s) over  15 recorded turn(s), 19268 byte(s)
POST-FIX  turns typed: 15/15
```

Regression test: `internal/replaydrive/harness_row_not_a_turn_test.go`,
observed red (`turns = 4, want 2`) with the production fix reverted and green
with it restored.

## 4. A third defect found while reading the artifacts: the dump names a session that does not exist

Reading the dump for symptom A surfaced this:

```json
"session_id": "session",
"session_log": "/home/user/.gopi/sessions/8616b46c-.../session.jsonl",
```

`"session"` is the bare-session layout's **fixed file name** (`session.jsonl`),
not a session id. `BackfillOrphanCrashDumps` derived the id as
`TrimSuffix(filepath.Base(recordPath), ".jsonl")`. On this host **142 of the
orphan dumps** carry that same false identity.

**Why nothing caught it.** `reconcileOrphanDump` derived the id it *expected*
from the **dump's** basename — `session.dump.json` → `"session"` — so the wrong
claim agreed with the wrong expectation and the check passed. A wrong value that
agrees with itself is invisible.

**The fix** (one definition, Rule 14): `internal/sessionlayout.SessionID(path)` is
now the single place a session id is resolved from a record path — the recorded
`session_start.sessionId` first, then the layout. The dump WRITER uses it, and the
retract VERIFIER uses it against the record beside the dump. `gopilog.FileToID`
became a thin alias of it rather than a second copy of the same algorithm.

Regression test: `internal/agent/orphan_dump_identity_test.go`, red
(`dump claims session_id = session`) → green.

**NOT DONE, and it is a decision for the operator.** The 142 dumps already on
disk keep `"session_id": "session"`. They are evidence, and the codebase's own
stated position (`crashdump.go`, the `legacyOrphanDumpCategory` comment) is that
existing dumps are not rewritten. Consequence: with the corrected verifier those
142 dumps can **never** be retracted, because their claim cannot match any real
id — which quietly removes the "~117 stale false positives" backlog the
retraction existed to clear. The honest options are (a) a one-off rewrite of the
`session_id` field, keyed on the `session_log` path each dump already carries, or
(b) accept them as permanent. Not done here: rewriting evidence is the operator's
call, not a diagnosis's.

## 5. What the operator should take away

- The counter discrepancy is **fixed**: one definition of a turn, everywhere,
  proved by A/B on the session in the report.
- The freeze is **characterised, not fixed**: the evidence excludes every guarded
  exit path, so whatever ended the process bypassed all of them. The next
  occurrence is made decidable by reading the screen dump's timestamp against the
  session log's — the one artifact that lands at the moment of death.
