<!-- doc-class: normative -->
# The web page's microphone (`gopi web`) — web-voice-input

Route B: the recording runs **on the machine running `gopi web`**, through the
same device policy and the same engine seam the TUI's `/voice` uses, and the page
receives an **editable draft** — never a silently sent turn.

Task `01a16d1d-fc51-71df-a633-0442c630a150`. Endpoints: `cmd/agent/web_voice.go`.
Page: `cmd/agent/cli_web.go` (button, styles, script). Tests:
`cmd/agent/web_voice_test.go` + the `voice:` scenario in
`cmd/agent/cli_web_page_scenarios.js`.

## Why the server records and the browser does not

The browser's `MediaRecorder` (`getUserMedia`) is route A, and it was rejected for
one reason: **it puts a second microphone policy in JavaScript**. Everything that
makes voice usable here — the four-tier device ladder, identity by name, the
liveness probe before a real recording, the "reachable but silent" verdict — lives
in `cli_voice_device_policy.go` and is read by every voice shape gopi has. Route A
would receive audio with **no identity behind it**: nothing to print in a refusal,
nothing an operator can pin with a device flag, and a fix to the policy unable to
land on that door.

The cost is stated rather than hidden: **the microphone is the one attached to the
host running `gopi web`**. A browser on a laptop talking to a server on another
host records the *server's* room. This is why `gopi web` binds loopback by
default; a remote page should be reached over ssh port-forwarding, not by
publishing the server.

## The wire

| verb + path | body in | body out |
|---|---|---|
| `POST /api/voice/start` | — | `{ok, device, notes}` · 409 if one is running · 503 on a device/engine refusal (the server's own sentence) |
| `POST /api/voice/stop` | — | `{ok, draft, transcript, device, notes, truncated, tail_dropped}` · 409 if none is running |

Both are POST-only (a browser prefetch or an `<img>` probe must never open a
device), and neither sends a turn: the page inserts `draft` into the composer and
the operator presses Enter, exactly like the TUI.

## The four things that are easy to get wrong

**1. The slash hazard.** On this carrier a line whose first word is `/something`
does not reach the model — `classifyChatLine` routes it to the command table and
`serveSlashCommand` answers it locally. A transcript is *speech*: an operator who
says "slash new" is not asking to clear the conversation, and a cloud engine
returning `/new` for a misheard utterance would reset the session instead of
drafting a message. `webVoiceDraftText` neutralises it into the carrier's **own**
literal-send form (`chatSlashEscape`, `//`), which `chatLineEscape` un-escapes to
the operator's text. A second escape invented here would be a second answer to
"how do I send a literal slash" on the same wire.

**2. The capture is the TUI's recorder, not a loop over the builder.** Stopping
closes the stream, and a close **discards** the window the builder is holding.
`chat_voice_recorder_staged.go` already solves that (stop the reader, drain it,
*then* close) because the tail of a long recording was exactly what went missing
before (task `01a11b81`). The web carrier therefore drives
`*voiceChatRecorderStaged` — `Round` per window, `Drain` at stop — instead of
writing a second capture loop: measured on a real device, a naive close lost the
audio captured after the click entirely, which the first live run showed as an
empty transcript 3 s after `start`.

**3. The lock order.** `voiceMu` (the voice endpoints, one recording at a time)
first; `s.mu` (**the turn lock**) only around the *finish*, because `Round` calls
the engine on the shared agent that a streaming turn is mutating. `start`
deliberately does **not** take `s.mu`: opening a device is a ~1 s probe that
touches no agent state, and charging it to the turn lock would queue a live turn
behind a microphone.

**4. The page's state is the server's.** The button's recording state follows the
*answer*, never the click, so a device refusal (503 with its own sentence) leaves
it idle. A draft is inserted **at the caret** of a composer the operator may
already be editing, and an empty transcript inserts nothing and says so.

## The page harness had to be tightened

`cli_web_page_harness.js` used to keep its **own list** of element ids, so a
button deleted from the shipped HTML was still served by the shim — measured: with
`id="mic"` removed from `webChatPage`, the voice scenario still reported
`PASS: 17/17`. The shim is now built from the **page's own `id="…"` attributes**
(`shippedElementIDs`), so a missing element reads as `null` for the same reason a
browser does. With that repaired, the same mutation turns the scenario red
(`FAIL the page ships a mic button`), which is what makes the test worth having.

## What is verified, and how

- Red-proof 1 — disable the neutralisation: `TestVoiceDraftNeutralisesTheSlashHazard`
  and `TestVoiceDraftIsCarriedAsTextNotAsACommand` go red (`draft "/new" … would be
  executed as a command`); restore verified byte-identical.
- Red-proof 2 — remove the two `mux.HandleFunc` lines: `TestVoiceStartStopRoundTrip`
  fails (`start answered 404`).
- Red-proof 3 — delete the button from the page markup: the harness goes red.
- Live, against a real device (Rule 31): `POST /api/voice/start` → 200 naming
  `pulse:@DEFAULT_SOURCE@` after the policy's probe; a second start → 409; stop
  with no recording → 409; `GET` → 405.

## Known limit (honest)

The **engine** is not exercised by this change: on this host the configured engine
(`sauc`) answered `read: the stream ended without a transcript` for a silent
microphone, which the endpoint reports verbatim as a 503. Speech-in →
draft-out is proven at the seam (`Round`/`Drain` are the TUI's own, and their
transcripts are concatenated in stream order), not end-to-end against a speaking
operator.
