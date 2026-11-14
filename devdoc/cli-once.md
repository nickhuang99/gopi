<!-- doc-class: historical -->
# `gopi cli "<prompt>" --once` — one non-interactive turn

**Branch:** `feat/cli-once` · **Files:** `cmd/agent/cli_term_once.go` (new),
`cmd/agent/cli_term_once_test.go` (new), `cmd/agent/cli_term.go` (dispatch + help),
`cmd/agent/testdata/term_help_prefix.txt` (red-proof fixture).

## Why

`gopi cli` is a second VIEW over the `gopi web` engine (same `webServer`, same
single persistent agent, same `s.mu` turn discipline, same server-held history,
same tools). That view needs a terminal and a text browser, so **nothing could
drive one turn from a script** — which is what an agent-authored self-test (the
epic's CLI-as-eyes-and-hands goal) is built on.

## What it is, and what it deliberately is not

`--once` is the SAME turn, reached without a browser: an in-process
`httptest` server over the REAL `webChatHandler()`, one POST to `webChatPath`,
and the SSE stream read as it arrives. The model sees the same request, the same
system prompt and the same tool wiring — because `handleChat` IS the code path,
not a copy of it. No second engine, no toolcall self-observation layer, no new
dependency (`httptest` is stdlib).

Alias: `-p` (the familiar prompt-in/answer-out spelling). `--prompt "…"` is the
third, named-flag spelling.

### The three streams (the caller-observable contract)

| stream | carries |
|---|---|
| stdout | **the answer** — the engine's own terminal `result` string, written ONCE, and nothing else |
| stderr | the plumbing: session log path, a wait notice, one line per tool call/result, slash-command output, the failure reason |
| fd 3 | the machine-readable channel, when the caller opens it: each SSE frame byte-for-byte as the engine wrote it (`event: delta` / `tool` / `command` / `queued` / `done`) |

Exit status: `0` only for a COMPLETED turn that produced text; `1` a failed or
text-less turn (with a reason on stderr); `2` a usage error.

## The measured surprise (root cause, not a parameter)

The first live run printed the answer **twice** (`OKOK`). Root cause, not tuning:
`ChatOps`' degenerate-round guard re-issues a round when a final answer is
shorter than `minFinalAnswerChars` (40), so a short answer is generated a second
time and only the second one is the turn's result. The same shape hits a tool
turn for a different reason — its deltas carry the model's narration between
tool calls ("Let me read that file…"), which is not the answer.

So the answer stream is the engine's terminal `result`, written once, and the
raw delta stream is what fd 3 is FOR. Measured on the fix:
`gopi cli "say OK" --once` → stdout `OK` (exit 0), fd 3 carrying
`event: delta {"text":"OK"}` then `event: done {"result":"OK"}`.

## What the help now says

`gopi cli -h` names both spellings and the purpose (the epic's discoverability
target), keeping its capability-first shape; `gopi cli --once -h` prints the
face's own page (invocations, options, the three streams, the exit statuses).

## Verification

```
go build ./...                                     : OK
gofmt -l cmd/agent/cli_term_once.go cli_term.go    : (clean)
go vet ./cmd/agent/                                : (clean)
go test -count=1 -run 'TestTermOnce' ./cmd/agent/  : ok
go test -run 'TestTerm|TestCLIContract|TestUsage|TestHelp|TestCliCompletion' ./cmd/agent/ : ok
```

Live invocations (real network, `api.deepseek.com`):

```
$ gopi cli "say OK" --once            → stdout "OK", exit 0
$ gopi cli --tools "Read /etc/hostname and tell me its contents." --once
   stdout: the answer text only
   stderr: gopi cli: ⚙ call read /etc/hostname
           gopi cli: ⚙ result read peerbox
   fd 3  : event: tool {"kind":"call","name":"read","summary":"/etc/hostname"}
           event: tool {"kind":"result","name":"read","summary":"peerbox"}
           … event: delta … event: done
$ gopi cli -p "say OK" --once </dev/null   → exit 0 (stdin closed)
$ printf 'q\n' | gopi cli --once "say OK"  → exit 0 (under a pipe)
$ gopi cli --once </dev/null               → exit 2, usage message
```

Residue: three consecutive live runs, then `ps` for a process born in the run
window — **no `gopi cli … --once` process, no `links`**; nothing listening on the
default web port. The HTTP server is in-process and closed before return.

Red proof: `TestTermOnceHelpPredicateIsNotBlind` applies the help predicate to
`testdata/term_help_prefix.txt` — `gopi cli -h` as it actually shipped before
this change — and requires it to be FLAGGED (plus a positive probe so the gate
cannot forbid the structure it asks for).

## Not Done (honest)

- The delta stream is not on stdout; a caller wanting the turn's unrolling reads
  fd 3. Documented as such in the face's own help and header.
- Reasoning/thinking tokens reach no stream at all.
- No per-turn timing, token/cost count, image attachment, or continuity across
  runs (one invocation = one turn). These are the follow-up study's list.
