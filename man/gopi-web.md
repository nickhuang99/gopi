---
title: GOPI-WEB
section: 1
header: GoPi Manual
---

# NAME

gopi-web - serve a local browser chat UI, and reach the web tools from the shell

# SYNOPSIS

<!-- BEGIN SYNOPSIS (copied verbatim from `gopi <verb> -h` by internal/standards/mancensus) -->

```
  gopi web [SESSION-ID] [--addr :PORT] [--tools] [--thinking-live]
```

<!-- END SYNOPSIS -->

# DESCRIPTION

**gopi web** with no subcommand serves an embedded chat page over HTTP: open the
printed URL in a browser and chat with the model in a ChatGPT-style page.
Answers stream in live over Server-Sent Events, and the SERVER holds the
authoritative conversation history — each turn only sends the new message.

The same top-level word is also the door to the web TOOLS
(`gopi web search`, `gopi web fetch`), which is a declared exception to the
one-door rule and not an accident: both spellings reach the same predicate, so
neither can shadow the other and no spelling of these tools is ever spent as an
agent prompt. The grouping puts the two tool faces on the page; the top-level
word starts the server.

**The default posture is loopback**, and that is a safety decision rather than a
convenience one: with `--tools`, tool turns execute on this host **with no
approval gate**, so they are treated as local-shell-level trust. Binding a
non-loopback address is how you opt out of that posture, and it mints a per-run
access token which the banner prints in every URL. Requests from this host need
no token (a browser here is the same operator who holds the shell). The token is
a WEB access token — never the model API key.

**ONE SERVER PER PROFILE, ANY NUMBER OF BROWSERS.** The server is the thing that
executes turns against a session, so a second one on the same profile would run
the same conversation twice; it is refused with the lock path and the holder the
kernel recorded. The criterion is the PROCESS, not the address: two servers on
two different ports of one profile are still two servers, and the second is
refused as well. **Browsers are NOT limited** — the page, the SSE stream and the
access token are unchanged, and any number of tabs reach the one server (they are
clients; the server cannot and need not count them). The refusal's remedy is
therefore a tab on the running server, never a second `gopi web`.

The lock is the same kernel flock the rest of gopi uses for a run directory
(`internal/runlock`, `run.lock`), placed under this profile — so it is released
by the process exiting, including a crash, and never goes stale the way a pid
file would. It is a DIFFERENT question from the per-session run lock: that one
arbitrates who executes one session directory, this one arbitrates who is the
server at all.

# OPTIONS

`SESSION-ID`
: continue an EXISTING session in the browser. The address is the one
`gopi chat <session-id>` takes — a session id, a task id, or a raw-log path — and
the run APPENDS to that session (`gopi chat <session-id>` and `gopi web
<session-id>` therefore read one resolver and one conversation). Continuing seeds
the history the session already had, restores the switches it recorded at
start-up for the ones this command line does not state (each restore is
reported), and holds the run directory's kernel lock for the duration, so a
browser session and `gopi session resume` cannot execute one directory at once.
An address that names no session is REFUSED with the paths it looked at; it never
falls back to a fresh conversation. Omit it to start a new one.

`--addr :PORT`
: listen address (default `127.0.0.1:8090`). The joined spelling
`--addr=HOST:PORT` works as well.

`--tools`
: tool-enabled turns — the full gopi toolset, the same as `gopi chat --tools`.
Executes tools on this host with no approval gate; treat as local-shell-level
trust.

`--thinking-live`
: stream the model's live reasoning as `event: thinking` frames, one raw fragment
per frame, so a client can draw the model's own thinking while it works. OFF by
default, and with it off nothing is installed on the engine — the wire carries
not one extra frame.

# SEE ALSO

`gopi web -h`, `gopi -h`, `man gopi`, `man gopi-tool` (the same word's other
meaning), `man gopi-chat` (the terminal surface with the same tool semantics).
