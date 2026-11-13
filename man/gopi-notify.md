---
title: GOPI-NOTIFY
section: 1
header: GoPi Manual
---

# NAME

gopi-notify - subscribe to the task-event stream, and deliver a message to a session

# SYNOPSIS

<!-- BEGIN SYNOPSIS (copied verbatim from `gopi <verb> -h` by internal/standards/mancensus) -->

```
  gopi notify events [--sink PATH] [--event NAME]... [--task ID]... [--since T]
                     [--limit N] [--json] [--cursor PATH] [--follow]
  gopi notify failed [--sink PATH] [--since T] [--cursor PATH] [--json]
  gopi notify sink
  gopi notify [--peer <ALIAS>] --session <SESSION-ID> --message <TEXT>
  gopi notify --notify-remote            (the PEER side of --peer: stdin in, receipt out)
  gopi notify -h
```

<!-- END SYNOPSIS -->

# DESCRIPTION

**gopi notify** is one verb with two directions, and the object is what changes,
not the subject: gopi tells somebody else that something happened.

**Reading the lifecycle stream.** The sub-verbs `events`, `failed` and `sink`
consume the task-lifecycle events gopi produces. Three ways to subscribe exist,
and this verb is only the second:

1. **fan-out in the sink command** — a consumer that needs its own copy the
   moment an event lands adds it there;
2. **this verb, with a cursor** — a loop (a PA, a monitor, a fleet roster)
   resumes without duplicating or losing a record;
3. **read the file directly** — with a JSON stream decoder, never line by line.

An unreadable sink is an error, never an empty success.

**Delivering to a session.** `--session` plus `--message` appends one line into
that session's inbox. Both flags are required: `--session` alone says nothing to
send, `--message` alone has no recipient. Delivery is **durable** — the message
is appended to the session's `messages/inbox.jsonl`, so it survives the session
not running at all, and the session takes it at its next round boundary through
the same single take point a background-job completion notice uses. A message is
delivered once; a `delivered` marker is appended beside it.

`--peer` ships the same request to another host over the ssh leg the sync family
already trusts, where `--notify-remote` resolves the session in that host's own
profile. The hop is **idempotent on the message itself** — the id is derived
from (session, text, source) — so re-running a command whose ssh leg died after
the peer had already written appends nothing and says so. A message the peer's
session has already taken is refused by name instead, because re-appending would
hand the model the same instruction twice.

An idle console checks its own inbox while it waits at the prompt, so a
delivered message becomes the next turn by itself. A turn already in flight is
never interrupted.

# EXIT STATUS

`0`
: the read succeeded — including "nothing new".

`1`
: the read failed, or no sink could be resolved.

# SEE ALSO

`gopi notify <sub> -h`, `gopi -h`, `man gopi`, and `man gopi-task` (the events
being read are task-lifecycle events).
