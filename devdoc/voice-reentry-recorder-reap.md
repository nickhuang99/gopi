<!-- doc-class: normative -->
# `/voice` re-entry leaked one recorder process per entry — the guards were per-OBJECT, the resource is per-ENTRY

**Scope**: `cmd/agent/chat_voice_recorder_staged.go`, `cmd/agent/voice_recorder_reentry_reap_test.go`.
**Measured at**: `67f23d74d` (this tree), against a live `gopi chat --resume` on host
`host-b`.
**Task**: `01a170a6-6f50-7955-8331-f31057657f4e`.

## The symptom, as the operator saw it

A long-lived chat had accumulated fourteen `parecord` children in state `Z`, every
one parented to the chat process:

```
$ pgrep -x parecord | wc -l
14
$ ps -o pid,ppid,stat,lstart,cmd -p $(pgrep -x parecord | paste -sd,)
    PID    PPID STAT                  STARTED CMD
 431808  331978 Z    Sun Oct 25 06:15:12 2026 [parecord] <defunct>
 …
2212490  331978 Z    Sun Oct 25 20:42:20 2026 [parecord] <defunct>
$ ps -o pid,stat,etime,cmd -p 331978
 331978 Sl+     18:09:13 gopi chat --resume f729d555-…
```

One zombie per `/voice` recording, spread across eighteen hours of one session.

## The measurement that redirected the investigation

The obvious reading — "the 2026-10-22 fix never landed" — is **wrong**, and the
process table says so:

```
$ ls -l /proc/331978/exe
/proc/331978/exe -> /home/user/work/gopi/bin/gopi (deleted)
$ /proc/331978/exe --version
revision: b9be76fdba18f7fccef2820a1dce7bee5cd07517
$ ps -o lstart= -p 331978
Sun Oct 25 06:03:58 2026
```

Four of the zombies were created at 20:35–20:42 on Oct 25 — i.e. by *that* binary,
which started at 06:03:58. And that binary's four recorder files are byte-identical
to this tree:

```
$ git diff b9be76f 67f23d74d --stat -- cmd/agent/chat_voice_recorder_staged.go \
    cmd/agent/cli_voice_audio_source.go cmd/agent/chat_voice.go cmd/agent/cli_voice_client.go
(no output)
```

(Note the trap that produced the *other* wrong reading: `git merge-base --is-ancestor
7300279 b9be76f` exits **1**. On a clone/merge channel a rebased or cherry-picked
commit changes hash, so the commit is not an ancestor even though its content is
present — `GOPI.md` §4.0 already records this. Content is the evidence; the hash is
not.)

So the leak is **live in current master**, and the 2026-10-22 work (task
`01a163df`) was necessary but not sufficient.

## Root cause

That fix made **one teardown correct**: the forced `Kill`+`Wait` inside
`stopVoiceRecorder`, and `Setpgid` on the spawn. It did not make the teardown
**reachable a second time**.

`voiceChatRecorderStaged`'s teardown guards — `readerOnce`, `stopOnce`,
`drainOnce`, `closeOnce` — were fields of the **object**, and the object is built
**once per chat session**:

```go
// cli_chat.go
voiceMode := newChatVoiceMode(input, chatVoiceRecorder(chatVoiceAudioPath(voiceAudio)))
```

while `/voice` is a **toggle**. So:

| | what happens |
|---|---|
| entry 1 `Select` | spawns recorder #1; `closeOnce` armed |
| entry 1 `Close` | `stopReader` + `stream.Close` → recorder #1 signalled and `Wait`ed ✓ |
| entry 2 `Select` | spawns recorder #2, assigns it to `r.stream` |
| entry 2 `Close` | `closeOnce.Do` is **already spent** → no-op → recorder #2 is never signalled, never `Wait`ed; it exits on its own and stays in the table ✗ `<defunct>` |

`readerOnce` is spent for the same reason, so entry 2's capture has no reader
either — the second half of the same defect.

## The fix

The invariant, stated once and enforced at the one place entries open:

> **Every resource this object opens belongs to an ENTRY, so every guard
> protecting one is armed by that entry.**

`beginEntry()` (called at the top of `Select`, before any spawn) releases anything
the previous entry left behind and zeroes the entry-scoped half: the stream, the
reader's channels, the four `sync.Once` guards, the device/notes/schedule state,
the drain flags, and the per-entry round options. Object-level configuration
(`audioSource`, `engine`) survives by design — it is how the run was started.

Two ordering facts that are load-bearing, both recorded at the call sites:

1. **The reset happens AFTER the old reader has joined, never before.** That
   goroutine reads this object's schedule/window state while it runs; zeroing
   first and joining after would be a data race against the goroutine being
   released.
2. **The reset happens BEFORE the spawn, never after.** A `Select` that fails to
   open a capture must leave the object in a state whose *next* entry works; a
   reset placed after the spawn would leave the failed entry's spent guards in
   place.

`stopReader`/`Close`/`beginEntry` all pass through one locked teardown
(`retireCapture` / `retireCaptureLocked`, serialized by `entryOnce`) because an
enter and a leave are different goroutines whenever a toggle lands mid-wind-down —
the same class of defect one level down.

## Why not simply build a fresh recorder per entry

Rejected with a reason, not by preference: the recorder is not the only thing that
would have to be rebuilt. The mode, the status row's voice seam
(`status.setVoiceFn(voiceMode.livePhase)`) and the key gate all bind to that one
object at session start, so a per-entry object becomes a per-entry **wiring**
problem across three surfaces. Resetting the entry-scoped half keeps one owner.

## The regression test

`TestVoiceChatRecorderReapsEveryEntrysRecorder` (`cmd/agent/voice_recorder_reentry_reap_test.go`).

It drives the **production** path — `chatVoiceRecorder("")` → `Select("")` →
`Round(...)` → `Close()`, twice over **one** object — with three stand-ins
(`pactl`, `arecord`, `parecord`) installed in front of `PATH`. **No production seam
was added for the test**: the code under test `exec`s those names by name, so a
directory earlier on `PATH` is reached with the code unmodified.

Two arms:
- `a capture that produced audio` — the child writes one window of PCM and exits.
- `a device that produced no audio` — the child opens, produces nothing and exits;
  the round must **report** the silence (Rule 15) *and* leave no child behind.

**The criterion is the process table, not a signal**: a zombie answers `kill(pid,0)`
with success, so "reaped" is only decidable as `ESRCH` **plus** the absence of
`/proc/<pid>`. The failure message prints the row's state character, so a leftover
live process and a leftover zombie read differently.

### Red proof, measured on both arms

Run through `go test -overlay`, which substitutes the pre-fix blob **without
touching the worktree** — an earlier in-tree revert had failed to take effect and
passed a RED case (a false green), so the red proof must be shown, not assumed:

```bash
go test -overlay "$EV/overlay.json" ./cmd/agent/ \
  -run 'TestVoiceChatRecorderReapsEveryEntrysRecorder' -count=1 -v
```

```
--- FAIL: TestVoiceChatRecorderReapsEveryEntrysRecorder (6.08s)
    entry 2: recorder pid 3436883 is still in the process table after the entry was left (state=Z) — …
    entry 2: recorder pid 3436894 is still in the process table after the entry was left (state=Z) — …
```

Both arms red (one line each), the same command green after the fix.

## What this does NOT claim

- The **fourteen already-unreaped processes under the live chat are not reaped by
  this change** — they belong to a process running the old binary and stay until
  that session exits. This is a forward fix.
- `voice_recorder_reap_test.go` already covers "a recorder still running at
  teardown is forced and reaped"; that arm was **not** duplicated here (a linger
  arm was written and removed — it cost 34 s/run for a property already proven
  elsewhere). What was missing, and what this test proves, is the **re-entry**
  shape.
