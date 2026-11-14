<!-- doc-class: normative -->
# chat `/voice`'s continuous streaming arm (`doubao-stream`)

**What landed, and WHICH of the two attempts landed.** Two independent runs built a
"live streaming dictation" arm at the same time and the two designs were
incompatible — one `internal/doubaostream.Session` shape, two spellings of it. The
add/add conflict was resolved in favour of **master's landed design** (commit
`823305a17`, "streaming dictation — drafts live in the line, Ctrl+G settles the
sentence"); the other attempt's parallel session type, its `internal/lineedit`
draft seam and its CLI live arm were **not** re-introduced. This document describes
the design that SURVIVES, and then records the handful of things the losing attempt
got right that survive with it.

| engine | recorder | shape |
|---|---|---|
| `local`, `doubao` | `voiceChatRecorderStaged` (unchanged) | windows: capture 6s → transcribe → wait → append |
| `doubao-stream` | `voiceChatRecorderStaged.Live` | ONE continuous v2 session: partials while the operator speaks |

The live arm is reached only by NAMING the streaming engine (`config.VoiceEngineDoubaoStream`,
`cmd/agent/chat_voice.go`'s `voiceLiveEngineSelected`); it is never the default, so a
machine with nothing deployed keeps exactly the batch behaviour it had — that
property is the reason the two engines are separate values at all.

## The surviving design, seam by seam

**1. The session: `internal/doubaostream.Session`.**
`Open(ctx, Options)` → `Say(pcm)` → `Finish(tail) (*Result, error)` → `Close()`,
with `PartialCount()` and `FinishTimedOut()` as the observations a caller needs to
tell a real transcript from a timed-out one. `readLoop` owns the socket and folds
partials; `awaitReader()` is the join that makes `Close`'s contract (the socket is
gone and the reader has stopped using it) and `Finish`'s contract (the transcript
returned is the one the reader had finished folding) true.

**2. The provisional row: `internal/lineedit/draft.go`.**
A cumulative partial cannot go into the buffer — the next partial would have to be
edited back out, and the operator's caret would move under them while they speak. It
cannot be appended either: the service re-sends the WHOLE sentence each time, so
appending `你` + `你好` + `你好吗` builds `你你好你好吗`. So the partial is drawn on a
row of its own, DIRECTLY BELOW the editing line, replaced on every arrival, and
settled when the turn commits (`SetDraft(text, done)` / `Draft()` / `DraftVisible()`,
drained at the top of repaint by `drainDraft`). The buffer is untouched until the
final transcript goes through the ordinary insert road.

**3. The arm and the loop: `cmd/agent/chat_voice_live.go`.**
`voiceChatRecorderStaged.Live(opts, sink, finish)` is the live half of the recorder,
beside its window half and over the SAME capture (`voiceStagedStream` is the one
reader of the device; it is not restarted, re-spawned or duplicated). A feeder
(`voiceLiveFeeder`) drains its chunks into packets and `Say`s them; `draftSink` puts
each cumulative partial on the provisional row and `Settle`s the final text through
the line's own insert road. `Ctrl+G` (`lineedit.KeyLiveFinish`) ends the utterance,
claimed by `voiceLiveFinishKeyClaims`, which is the ONE place that key is decided
(Rule 13) and which checks the key's ORIGIN — an injected replay key cannot end a
human's utterance.

**THE CAPTURE IS OPENED PER UTTERANCE, not per mode**, and that is deliberate: a
streaming session is billed per audio second and terminated by an explicit
end-of-stream flag, so a stream left open between utterances would be a session the
service is waiting on and the operator is not using. One utterance = one session =
one capture.

## What the two attempts agreed on, by independent measurement

Two runs, two designs, and the same two defects found on the first real run. That
convergence is the reason this section exists: it says the defects are properties of
the SHAPE (a foreign producer writing a row the terminal draws) rather than of
either author's reading.

**The row was drawn and the screen never repainted.** The wake channel is a one-byte
pipe and the read loop re-derives a LEVEL on the way out of every read outcome
before parking. A level that is not in that set is spent on a read the loop has
already committed to blocking. The surviving design carries `draftPending()` in that
set, the same position `lineInsertPending` holds and for the same reason.

**The operator was told the wrong mechanism.** A live row printed BEFORE the
recorder is armed makes "can this recorder run a live turn?" answer false about a
recorder the mode has not installed yet: the live note never appears, and the window
note describes a session with no windows. The surviving design asks the RECORDER
(`if _, ok := m.recorder.(voiceChatLiveRunner); ok`) rather than printing both, and
the row advertises only a key the recorder can actually honour.

## The hardening ported onto the surviving design

**A behavioural case for the join, and an honest note about what it cannot do.**
`internal/doubaostream/session_test.go` drives the surviving API against a REAL
loopback WebSocket fake server (`net/http/httptest`, stdlib only — Rule 35): the
frames the service actually read, the tail carried in front of the end-of-stream
flag, the abort path that sends no flag, and the fact that `Close` leaves the socket
gone.

**`internal/doubaostream/session_teardown_guard_test.go` — the join as a SOURCE
guard, and the measured reason it is not a behavioural one.** The wait is not
decoration and no ordinary assertion can see its absence: the caller still gets its
`*Result`, the transcript is usually already complete, and a fake server inside the
test binary stops reading at the same instant `Close` stops waiting — the process
does not exit while another goroutine is blocked in a read, so both orders converge.
Measured (2026-10-09): the behavioural case (`session_test.go`'s
`TestSessionCloseWaitsForTheServicesOwnReadOfTheClose`) is GREEN with the
`awaitReader()` call DELETED from `Close`. What the behavioural cases do prove is
what the design actually delivered — the frames read, the tail in front of the flag,
the abort path. The absence of a join is a fact about the SOURCE, so the guard
parses `session.go` with `go/ast` and asserts that each teardown (`Close`, `Finish`)
calls `awaitReader` — anchored on names, so a reformat or a comment cannot retire
it, and a rename fails loudly instead of passing by absence (Rule 15).
**RED is proved by deleting the await**: the guard then names the method and the
shape it wants. (Its own name is
`TestSessionTeardownAwaitsTheReader`.)

## What the losing attempt had and the surviving design does not

Recorded so a later reader does not hunt for machinery that was never here. Each of
these was built, measured and then **dropped with its design** — none is a TODO.

- **The spoken stop word** (`voiceChatLiveStopWords`, `voiceChatLiveSaidStop`,
  `voiceChatLiveStripStopWord`, matched on the CUMULATIVE partial's end). The
  surviving design has no stop-word machinery at all: an utterance is ended by the
  finish key (`Ctrl+G`) and by nothing else. The word's own limits, for the record if
  it is ever wanted back: it must be matched on the cumulative partial's END, so a
  recogniser that splits `停止` across two partials still matches on the second, but
  one that reverses it into `止停` would not; and on a synthetic speaker (`espeak -v
  zh`, PulseAudio null-sink) it was never recognised in any run — the service
  answered `07， 07，`.
- **The independent recorder type** (`voiceChatRecorderLive`, `Listen(ctx, opts,
  obs)`, `sessionHolder`, an optional-interface type assertion in `enter`). The
  surviving design puts the live half on the recorder that already exists
  (`voiceChatRecorderStaged.Live`) over the same capture, so there is no second
  recorder and no second seam to keep in agreement.
- **The independent draft accessor** (`LineDraft`-shaped `SetLiveDraft` /
  `ClearLiveDraft` / `LiveDraft`). The surviving design exposes `SetDraft(text,
  done)` / `Draft()` / `DraftVisible()` and its live-arm assertion is made on the pty
  GRID (`cmd/agent/chat_voice_live_pty_test.go`). That is also why the losing
  attempt's Esc-mid-turn case is an honest **non-port**: its assertion
  (`Editor.LiveDraft() == ""`) has no analogue to port TO. The portable half of its
  intent is the src guard above; the assert-on-the-accessor half is not claimed.
- **The branch's CLI live arm** (`cli_voice_live.go`). The surviving design's CLI
  side is `cli_voice_stream.go` (`transcribeFileStream`, `voiceStreamCredentials`,
  `voiceReportStream*`), which landed with master.

## Not done, and the honest limits

- **No live-arm case exercises `Esc` mid-utterance against a PARTIAL on the row.**
  The losing attempt found a real defect here (Esc cancelled the session and
  discarded a transcript the operator had watched appear) and fixed it by committing
  what had been seen. Whether the surviving design has that defect is **not
  measured** — it is on the pty path, and the pty cases in
  `cmd/agent/chat_voice_live_pty_test.go` drive the finish key, not Esc-mid-partial.
  Stated as an open question rather than a claim: this is the first thing to test if
  a live run shows a vanished sentence.
