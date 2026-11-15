<!-- doc-class: normative -->
# Voice input provenance — how a turn says "these characters were dictated"

`/voice` inserts a recogniser's transcript into the editing line
(`cmd/agent/chat_voice.go` → `chatInput.VoiceInsertLine`). From that moment the
characters are ordinary runes: the operator presses Enter, and by the time the
request is built, a dictated sentence and a typed one are byte-identical. The
model therefore cannot tell that 「预约」 arrived as speech rather than as keys —
which is exactly the situation in which a language model "tidies" a word the
user never mistyped.

This document is the mechanism. Three questions it answers: where the fact is
recorded, where it becomes a line of text, and what it deliberately does not say.

## 1. The fact lives on the buffer, per rune

`internal/lineedit`'s `Value` carries a parallel slice `voice []bool` beside
`runes []rune` (`internal/lineedit/valuevoice.go`). `true` means "this character
is still the one a dictation producer inserted, and the operator has not removed
it". There is no other store: not a flag on the editor, not a field in the voice
mode's state.

**Why per rune and not a flag.** The requirement's hard case is *delete the
transcript, type your own*: a flag "this line came from `/voice`" survives that
edit and produces a false claim about text that is not there. A per-rune bit
cannot: `Backspace`, `DeleteForward`, `KillToStart` and every other mutation
splice the two slices together, so a deleted rune takes its provenance with it
and the count falls to zero by ordinary editing rather than by a rule of its own.

**Every mutation path is listed, because a path that splices `runes` alone
silently produces stale provenance** — a false claim is worse than a missing one.
Maintained in `valuevoice.go`: `Reset` (empties), `SetText` and history recall
(stamps everything plain — a recalled line is the operator's own message, not a
transcript), `spliceRunes`/`insertRunesStamped` (stamped at the moment the runes
land), `insertVoiceRunes`, `deleteVoiceRange` (called by `Backspace` /
`DeleteForward` / `KillToStart` / `KillToEnd`), and the draft-settle path.
`valueSnapshot` publishes `runes` and `voice` in ONE assignment, so a foreign
reader can never pair a count with a buffer those runes are no longer in.

`Value.VoiceRunes() int` is the one reading.

## 2. The insert road names its producer

`InsertDictationAtEnd` is a distinct entry point from `InsertLineText`, and
`lineInsert` carries a `dictated bool` beside its placement. The reason is that
the two calls answer different questions: not "where does this text go" but
"whose characters are these". A caller that wanted only the placement would
otherwise have to pass `false` for a fact it never measured, and the one producer
that does measure it must not be able to forget. The zero value is *not
dictated*, which is the honest default for a completion menu or a caller's own
line.

## 3. The carrier measures at the one moment it is still true

`chatInput.VoiceRunes()` (`cmd/agent/lineedit_wire.go`) reads the buffer, and
`cli_chat.go` measures it **in the reader goroutine**, right where `replayFace`
and `replayMoment` are taken, into `lineRead.voiceRunes`.

**Why there and not later in the loop**: `Editor.Run` resets the buffer at the
start of the next read, so by the time any later stage looks, the buffer belongs
to the following line. That instant is the only one where the submitted line's
provenance is still the buffer's provenance.

A cooked run (no editor) and a nil `chatInput` both answer `0` — no line exists
to measure, and "no dictated characters" is the honest reading of no line.

## 4. The message gains one line, last

`chatVoiceMessage` (`cmd/agent/chat_voice_input.go`) prefixes `out.Message` with
`sessionevent.VoiceInputPrefixFor(n)` when, and only when, the submitted line
carries dictated runes. It is composed at the END of the message assembly — after
the boundary seal, the screen reference and the prefetch adoption — so it
describes the bytes that will actually be asked and recorded.

- **`Message`, never `Display`.** `Display` is the operator's own line on their
  screen. The harness's sentence about provenance is for the model; showing it to
  the operator would display words they did not type. Verified in a real pty run:
  the terminal shows the bare transcript, the request body carries the prefix.
- **An empty message takes nothing.** A turn with no text is not a dictated
  message, and a prefix above nothing claims text that does not exist.
- **The spill path still works.** A line too large for the boundary is billed to
  a file and the message becomes a reference; the prefix goes first and the
  reference follows. The two facts are independent and both are true.

## 5. The wording is a provenance statement, not a quality warning

`sessionevent.VoiceInputPrefix` says the text was **dictated**, is a
**transcript**, and is **not characters the user typed** — and stops there.

**This is the design decision the whole file protects.** "This text may contain
speech-recognition errors" instructs the model to hunt for faults, and a model
told to hunt will "correct" correct words into wrong ones — the failure the
operator named (「预约」 becoming 「预压」). Provenance lets the model weigh the
text; a quality warning tells it to distrust the text.

The vocabulary is machine-pinned
(`TestVoiceInputPrefixDescribesProvenanceNotQuality`): the prefix must contain
*dictat* / *transcript* and must not contain *error*, *mistake*, *wrong*,
*incorrect*, *typo*, *fix*, *correct the*, and must be one line.

## 6. What the prefix is NOT: harness chatter

`VoiceInputPrefix` is deliberately **not** in `sessionevent.HarnessMessageMarkers`.
That list answers "is this row the FRAMEWORK talking?" — and a row carrying the
prefix is not: it is the operator's own input with one line of provenance above
it.

Listing it there would make `IsHarnessInjected` answer yes, `IsTurnOpeningRow`
fall through to its framework leg and answer "no turn", `session log --turns`
lose the turn, and `term.go` drop the operator's own message from the terminal
transcript. The distinction is pinned by
`TestVoiceInputPrefix_IsNotHarnessChatter`, which asserts both
`!IsHarnessInjected(prefixed)` and `IsTurnOpeningRow(prefixed, "human")`, and was
confirmed on a real session: `gopi session log --turns <id>` prints
`turns = 1` with the prefixed row as the turn's prompt.

## 7. Where the pieces live

| piece | file |
|---|---|
| per-rune provenance + the rule | `internal/lineedit/valuevoice.go` |
| the parallel slice and its maintainers | `internal/lineedit/value.go` |
| the dictation insert road | `internal/lineedit/lineinsert.go` |
| the carrier's reading | `cmd/agent/lineedit_wire.go` (`VoiceRunes`) |
| the composition | `cmd/agent/chat_voice_input.go` |
| the call site | `cmd/agent/cli_chat.go` (`lineRead.voiceRunes`, then `chatVoiceMessage`) |
| the wording and its predicate | `internal/sessionevent/voiceinput.go` |

## 8. Verification

```
go test ./internal/lineedit/ -run 'Dictation|Voice' -count=1        # 2.782s ok
go test ./cmd/agent/ -run TestVoiceInputPrefix -count=1             # 3.575s ok
```

The end-to-end evidence is a real `gopi chat` on a real pty against a stub
Anthropic endpoint that saves every request body. The recorded bodies:

- `/voice` turn → `messages[0].content` opens with
  `[voice input] The text below was dictated: …` followed by the transcript
  `开饭时间早上九点至下午五点。…`
- typed turn → `messages[0].content` is `hello from the keyboard …`, no prefix
- the operator's terminal shows the transcript with no prefix line in either case
