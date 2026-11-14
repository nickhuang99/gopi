<!-- doc-class: normative -->
# A replay records its memory refusals — it does not draw them

**The defect.** `gopi chat --replay <session>` re-runs a finished conversation so
the operator can watch it again. The memory family was already correctly
**refused** on that path (`internal/replaymode`, one decision point), but the
refusal SENTENCE was printed as a chat message:

```
[gopi] 🧠 [gopi replay] refused to store an auto-memory note in the operator's ledger (价格对比算清…)
[gopi] 🧠 [gopi replay] refused to store a captured response in the operator's ledger (| a | b |…)
```

Measured on a real pty run, 2026-10-25, replaying session
`5d7d30d1-5887-4bad-9189-b7c64ef89275`: 7 `🧠` rows inside the answer band,
interleaved with the recording's own replies
(`~/.gopi/sessions/01a174dd-…/replay-evidence/screen-before.txt`).

Two separate faults, and both had to be fixed:

1. **WRONG PLACE.** The sentence went through `a.Logger`, and a chat run points
   that logger at the layout's flow seam (`cmd/agent/chat_flow_seam.go`) — so a
   display whose whole job is to draw a session that already happened read as
   though *this* run had tried to write and been turned away. Once per captured
   note and once per captured response.
2. **AN UNGATED WRITE.** `recordSelfNotes` is the ONE write point for a round's
   `[IMPORTANT]` notes, and BOTH of its halves are writes: the append to the
   session's `self-record.md` and the INSERT into the `chat_notes` retrieval
   index (`selfrecord_noteindex.go`). A replay reached both. The per-note refusal
   above was its *symptom*, one layer downstream.

## What changed

| site | before | after |
|---|---|---|
| `internal/agent/memory_replay_guard.go` | `announceReplayRefusal` → `a.Logger` (the band) | `recordReplayMemoryRefusal` → `logs/replay-memory-refusals.log` in the profile; **nothing** on the surface |
| `internal/agent/selfrecord.go` `recordSelfNotes` | **ungated**: appended to `self-record.md` + indexed into `chat_notes` on a replay | gated on `replaymode.Active()` *before* any allocation; records ONE line and returns nil |
| `internal/agent/loop.go` — memory tool `store` | refused (already) | refused **and recorded** |
| `internal/agent/loop.go` — memory tool `feedback` / `update` | **ungated** (a recorded `memory feedback`/`update` call reached the operator's ledger) | refused **and recorded**, returning a sentence that names the refusal |
| `internal/agent/selfnotecapture.go` / `selfnotecaptureresponse.go` | refused, printed | refused, recorded |
| `cmd/agent/chat_prefetch.go`, `chat_prefetch_turn.go` | refused, printed on `os.Stderr` (which the flow seam draws) | refused, recorded |
| `cmd/agent/commands.go` — `/memory store` typed into a replay | **ungated**: a slash store wrote to the operator's ledger | refused, recorded; the answer the operator sees names it |
| `cmd/agent/chat_replay_memory_evidence.go` | — | **new**: the ONE cmd-side spelling (`announceReplayMemoryRefusal`) |

Naming: `memoryStoreReplayRefusal`/`…Refused` became `memoryWriteReplayRefusal`/
`…Refused`, because the gate now covers `feedback` and `update` too — a name that
says *store* while gating three actions is a lie a later reader would trust.

## Why a FILE, and which file

Rule 15 says a refused write must stay READABLE. Rule 37/35 say a chat owns no
branch and no session of its own — and a **replay mints no session at all**
(`internal/agent/session.go` refuses, measured in the same run), so there is no
per-run directory to hang evidence off. Inventing one for a run whose contract is
"writes nothing outside the recording" would be the write this epic exists to
stop.

So the record lands where this process already owns durable state: the profile's
own logs directory.

```
$GOPI_HOME/.gopi/logs/replay-memory-refusals.log     (mode 0600, append-only)
```

One line per refused write, stamped with the instant:

```
2026-10-25T21:12:03Z [gopi replay] refused to record a round's [IMPORTANT] notes …: this process is re-running …
```

The directory is resolved through `config.GopiDir()` — the ONE resolution every
consumer uses, so `GOPI_HOME` is honoured and the near-miss is refused there. A
profile that cannot be resolved or written is REPORTED on stderr by the writer
(never a silent no-op), and stderr is not the answer band.

## The shape of the gate (unchanged)

ONE question, asked in one place per call site: `replaymode.Active()` — never a
mode string, never a flag, never an env escape hatch (Rule 35). The sentence
comes from `replaymode.Refuse`, so the vocabulary cannot drift between the agent
sites and the verb sites.

`recordSelfNotes` is gated at its TOP, before extraction: a replay allocates no
record state, creates no session directory, and returns nil — which is also what
tells `captureSelfNotes` there is nothing to consider.

## Not changed, on purpose

- **The non-replay path is byte-identical in behaviour**: the same note is
  captured, the same row is stored, the same `📝 self-record: N note(s) appended`
  announcement appears. Every regression case carries a CONTROL arm that runs the
  same call disarmed and requires the write to happen, because "nothing appeared"
  proves nothing about a path that never wrote (Rule 31).
- **`search` and `get`/`list` stay allowed** on a replay — they are reads.

## Verification

See the task report (`tasks/` or `$GOPI_TASK_REPORT_PATH`) for the verbatim
commands: `go build ./...`, the branch-scoped suite, the real
`gopi chat --replay` screen diff (before/after), the `~/.gopi/memory.db` sha256
and `distilled_qa` row count on both sides, and the evidence file the run wrote.
