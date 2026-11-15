<!-- doc-class: normative -->
# The chat-slot registry: how `notify.cmd` finds a chat it is not descended from

**What this document is.** The design record of the sample `notify.cmd` sink
(`scripts/notify/notify.sh`, deployed to `~/.gopi/notify/notify.sh`) and of the
one small thing gopi had to gain for it to work: a `gopi chat` run now REGISTERS
ITSELF into a fixed, bounded set of slot files under the profile root. It states
the problem, the channel choice and its rejected alternative, the stdin-contention
mechanism with the measurement behind it, and what was deliberately not done.

## The problem, stated precisely

gopi hardcodes no notification mechanism. The task lifecycle fires an event, gopi
reads `notify.cmd` out of the configuration, execs it, and hands the payload over
on **stdin**; every remaining question — where the event goes, in what shape,
through what transport — belongs to the command. That contract is deliberate and
this change does not touch it.

The operator's configuration had grown the obvious sink:
`cat >> ~/.gopi/notify/task-events.jsonl`. A file is a fine terminal sink and a
poor console sink: the events pile up in a JSONL nobody reads unless they go
looking. What was wanted is that a task transition be *told to the chat consoles
that are running* — the console being a different process, often on a different
shell, and, crucially, **not a descendant of the process that emits the event**.
The emitter is the ledger's own machinery (a worker tick, a merge loop, a
`gopi task update` run from a third terminal).

That last fact is the whole design constraint. There is already a channel that
carries exactly this identity — `GOPI_CHAT_SURFACE_ID`, `<pid|self>:<session-dir>`,
published by the chat run at arm time and read by `gopi ui_state --target chat`
(`internal/config/chat_surface_identity.go`) — and it cannot be used here, because
**an environment variable travels DOWN**. It reaches this run's children and dies
with the process. The emitter is nobody's ancestor, so it inherits nothing.

## The channel that was chosen, and why a file

The chat run WRITES its identity where a sideways reader can find it. The value is
the *same value*, rendered by the *same function*
(`config.FormatChatSurfaceIdentity`) — Rule 13's one-spelling rule applied to an
identity that now has two readers.

The registry is a **fixed and bounded set**, and the fixedness is the point:

```
<profile>/chat_session_id_<0..9>.txt      # 10 slots, ChatSessionSlotLimit
```

- The count is a NAMED constant (`cmd/agent/chat_session_slots.go`,
  `ChatSessionSlotLimit`), and it is named because three independent readers must
  agree on it: the registration loop, the shell script (whose `SLOT_LIMIT` is
  compared against the Go constant by a test, so the number cannot be written
  twice and drift) and `CONFIG.md`.
- There is no directory scan that could grow, no index file, no lock, no daemon
  watching the slots, and no cleanup step (Rule 35: nothing here ticks; the files
  are the only state, and a stale entry costs one refused delivery).
- The write is `open(2)` with `O_TRUNC` over one small value — deliberately not a
  temp file + rename. A crash mid-registration leaves either the old value or the
  new one, never a half-written file that reads as an identity; and nothing is
  written outside the profile root, so Rule 22 has nothing to guard here.

**Registration is idempotent on the session, and the ceiling holds by
replacement.** `chatSessionSlotIndex` decides in this order:

1. the slot already carrying THIS identity → reuse it (a re-registration, or a
   resumed run in the same session directory, must not consume a second slot —
   the registry's size is the count of DISTINCT live surfaces);
2. otherwise the first free slot;
3. otherwise, with every slot taken, the OLDEST (smallest mtime, ties broken by
   index so the choice is deterministic and a test can pin it) — which is the
   entry a crash left behind. This is the line that reclaims it.

**Rejected alternative: reap stale slots by checking whether the pid is alive.**
A slot holds a pid; the temptation is to validate it. Refused for the same reason
`gopi notify --session` itself deliberately does not check liveness: a pid is
recyclable, a liveness probe in the subscriber makes a lucky guess load-bearing,
and the mtime rule costs nothing and is right whatever the pid means. A stale slot
does not accumulate — it is the first thing the ceiling reclaims.

## The script

`scripts/notify/notify.sh` is the sample target. Two destinations, and the order
is the safety property:

1. **every registered console** — one
   `gopi notify --session <session-directory> --message <summary>` per slot, the
   directory half taken from the slot value. A refused delivery is REPORTED and
   the loop carries on: one dead chat must not silence the other nine;
2. **ALWAYS, last** — `mkdir -p` + append the RAW payload to
   `<profile>/notify/task-events.jsonl`. This is the fallback ANCHOR: the
   subscribers that read that stream (`gopi notify events`, the zombie-audit
   recipes, anything that greps the file) keep working unchanged — on a machine
   with no chat running at all, and even when every slot above refused.

### stdin contention — the one subtlety, and its measurement

The payload arrives on **stdin**, and a pipe is not seekable: it can be read
exactly once. The script spawns up to ten `gopi notify` children; each would
otherwise inherit that same pipe, and a child that reads it — or merely closes it
early — silently eats bytes the anchor still needs. The shape that solves it:
capture the payload into a shell variable FIRST, before any child is started, and
give every child `</dev/null` so none of them can touch the stream.

**Measured, not reasoned** (2026-10-10, sandbox profile, `GOPI_HOME` pointed at
it): with the capture, the anchor receives the complete payload and the loop
reports `delivered=N refused=0`; with a per-child `cat`, the second reader gets
nothing. The regression is pinned by
`cmd/agent/chat_session_slots_script_test.go`, which reads the script's text —
the constants it must agree with, the `</dev/null` redirection, the executable
bit and the presence of the anchor — so the contract is checked on both sides of
the language boundary.

## What was deliberately NOT done

- **No new config key, no new verb, no new flag.** The registry reads the profile
  root through the existing `config.GopiDir`, exactly like every other gopi path.
- **No new resident dependency** (Rule 35): no daemon, no cron line, no timer, no
  watcher, no second store, no reaper. The script is exec'd by the delivery path
  that already exists, does its work and exits.
- **`gopi notify --session`'s liveness semantics are untouched.** It still does
  not check whether the target process is alive — that is a deliberate design, and
  a slot naming a dead chat is expected to be refused rather than silently
  dropped.
- **`notify.events` and the `notifyfeed` read semantics are untouched.**
- **The sink-path recogniser is untouched**, and that has ONE operator-visible
  consequence, stated here rather than hidden: because the sample command is now a
  shell script, `gopi notify sink` reports `(none)` for it, and a reader must name
  the stream explicitly (`gopi notify events --sink
  ~/.gopi/notify/task-events.jsonl`). Teaching the recogniser to guess a path out
  of an arbitrary script would be exactly the lie its header refuses.

## Verification

- Build: `go build ./...` → clean; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → clean.
- Tests: `make test-budget PKGS='./cmd/agent ./internal/standards'` — results in
  the task report beside this document, including the pre-existing reds in this
  package that this branch neither caused nor touches.
- Ran: the exact invocations and their output are in the task report's `## Ran:`
  lines — registration through a real `gopi chat`, delivery through the deployed
  script's own stdin path against a live profile, and the ledger transition that
  proves the anchor grows.
- `CONFIG.md` carries the key row and the slot contract;
  `go test ./internal/standards/ -run TestConfigDoc` is green.
