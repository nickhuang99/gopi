<!-- doc-class: normative -->
# Chat film's animation arm — `--out FILE.gif`, and the close of B3

**Task:** 01a10156 · **Branch:** `task/01a10156…` · **Status:** delivered

`gopi chat --replay <session> --film --out /tmp/replay.gif` now produces a real
playable animated GIF. This document is the design record: what was there, what
was decided, and the two traps the work turned up.

---

## What already existed, and what was missing

`--film DIR` was already a working feature: it starts a private X display
(`internal/shot.FilmDisplay`) and a private `xterm` on it, runs a **second copy
of the same binary** inside that terminal as the producer
(`gopi chat --replay <id> --scratch <dir> --cols C --rows R`), and photographs
the terminal once per **drawn beat** — a state change the producer announces
through `internal/replay`'s ledger (`beats.tsv`) and waits to have
acknowledged. The frames
(`frame-%04d.png`), the ledger and an evidence `frames.tsv` land in the film
directory.

What it did **not** do was produce the artifact an operator actually hands to
somebody: an **animation**. The frames were the film's own record, and the
animation was a second command away (`gopi gif --frames <every png in order>
--out …`) — even though the pixels were already captured and the encoder already
existed. That gap is what `--out` closes: one command line, one GIF.

## Decision: the encoder is `internal/gifgen`, and nothing else

`internal/gifgen` is the backend behind `gopi gif`. It is pure Go over the
standard library's `image/gif`, and it draws captions through
`internal/text2img` — the repository's one rasteriser. Rule 35 forbids a second
encoder and a new dependency, so the film's frames go **in through
`gifgen.Encode`** and `go.mod`/`go.sum` are untouched (verified: both are
unmodified in the diff).

The frames are read back **from disk** — the PNGs the driver itself just wrote —
rather than from an in-memory buffer. The reason is `--dump-screen`-adjacent: if
the GIF were composed from a second, in-memory copy of the pixels, then the
directory's contents and the animation's could drift apart and no test could
tell. The animation is a function of the deliverable.

## Decision: the pace, and why it is uniform

A beat is a **state change of the surface**, not a video frame. The capture
cadence — one settled grab per beat — is the speed of *this machine* and of
xterm's repaint; it has nothing to do with the pace the recorded conversation
actually had. Replaying the capture's real time would therefore make the film's
speed an artifact of how fast the filmer ran, and **two films of the same
recording would play at different speeds on different hosts**.

So the film uses **one uniform delay per frame**, taken from gifgen's own default
— the pace `gopi gif` already uses when its caller names none:

```go
chatFilmGIFDelayCS = gifgen.DefaultDelayCentiseconds   // 25 cs = 250 ms per beat
```

The constant **references** gifgen's value rather than restating it (Rules
13/14): the film and `gopi gif` must not hold two ideas of what a frame's
duration is. The help text states that pace, and its number comes from
`strconv.Itoa(chatFilmGIFDelayCS)` for the same reason — a hand-written "25cs" in
a usage page is the second definition Rules 1/13 exist to prevent. That is also
why `chatUsage` is a **`var`** and not a `const`: Go refuses a function call in a
constant expression, and the composed value is worth more than the constant
keyword.

The GIF's loop count is left to the encoder's own resolution (an unset
`Loop` resolves to loop-forever) — one animation, one decision, in one place.

## Decision: the caption is the ledger row

A GIF of a UI with no words is unreadable to anyone who did not record it. Each
frame therefore carries its own beat number and the ledger's own kind + preview
for that beat:

```
#7 tool_call: bash: go build ./...
```

The numbers the acceptance compares — `frames.tsv` rows, `beats.tsv` rows, the
GIF's frame count — are printed **on the frames they count**. The words come
from the *same read* of the ledger the driver's frame loop uses: `readFilmBeats`
became a projection of a new `readFilmLedger`, so the frame's caption and the
frame's identity come from one parse. A second parse is how a caption ends up
describing a different row than the picture beside it.

Preview is cut to 40 runes for the caption; the ledger's own preview column
stays longer. When the caption still overflows the canvas, gifgen *warns* — and
this driver **prints that warning** rather than swallowing it (Rule 15).

## Decision: the composition is held to the ledger's arithmetic

`composeGIF` refuses to report success unless the encoded frame count equals the
ledger's row count:

```
animation /tmp/replay.gif: 6 frame(s) at 25cs (4.0 frame(s)/s), 54367 byte(s) — 6 ledger row(s), 6 frame(s) photographed
```

The acceptance criterion is made a **runtime invariant**. A film that animates
fewer beats than it recorded is refused, not reported as complete — and both
numbers are printed side by side so a reader never has to trust one account.

---

## B3: `--dump-screen` on a film run

**The defect.** `--dump-screen` was accepted on a film command line and had no
effect: the run exited 0 and nothing was written where the caller pointed.

**The root cause, which decides the fix.** A film run is a **driver**. It paints
nothing. The screen is painted by a *second copy of this binary* inside the
private terminal, and the argv that copy receives is built by the driver
(`runChatFilmOut`) — so a flag *this* process read is simply absent there.
`--dump-screen` is therefore the one class of word a film cannot honour:
accepting it arms a sink on a process with no surface.

**Why the environment is not the same case — and this is the working route.**
`GOPI_CHAT_DUMP` is *inherited* by the producer (the terminal child gets the
caller's environment with `DISPLAY` set), so an env-armed dump **does** work in a
film run. And the default (no env, no flag) is fine too: the producer arms its
own dump beside its own session artifacts. So the fix is narrow:

| the word | in a film run | why |
|---|---|---|
| (nothing) | allowed | the producer dumps on its own terms |
| `GOPI_CHAT_DUMP=PATH` (env) | **works** | inherited by the producer |
| `--dump-screen[=PATH]` (flag) | **refused** | cannot reach the painter |
| `--no-dump-screen` (flag) | **refused** | the decline cannot reach it either, so the filmed run would silently keep dumping |

The off switch is refused for the mirror reason, and its refusal is a *different
sentence* from the arm flag's — a decline and a request are opposite intents and
need opposite explanations. Both refusals name `GOPI_CHAT_DUMP` as the route that
works, because a refusal a caller cannot act on is only half an answer.

**The same principle checked for `--scratch`.** `--scratch` is *not* in that
class, and the film's own code says why: the driver **builds** the producer's
argv and passes `--scratch <dir>` itself, so the word is delivered by the very
mechanism that made `--dump-screen` undeliverable. The general rule is therefore
"a word the driver forwards is honoured; a word that must reach the painter and
is not forwarded is refused" — and it is stated that way in
`chatFilmDumpRefusal`'s comment so the next flag is classified by the rule rather
than by precedent.

---

## Two traps the work turned up

**1. A bare `--film` swallowed the next flag.** The operator's command line is
`--film --out FILE.gif` — a bare `--film` whose directory is derivable from the
`--out`. Implementing that, the argv walk advanced `i` past the next slot
*unconditionally*, so `--out` was consumed as the film's directory value and its
path then fell through to the positional handler and was read as **a session to
continue**:

```
gopi chat: --replay names the session to re-run (...) and the argument
".../x.gif" names a session to continue — a replay re-runs its own recording
```

The fix: advance the slot **only when a value was actually taken**
(`dir != ""`). A bare `--film` consumes one token, not two. This is exactly the
class of defect Rule 31 exists for — the unit test of `chatParseFilmArg` passed
throughout, because the bug was in its *caller's* consumption arithmetic, and only
running the real command line showed it.

**Where does a bare `--film` put its frames?** `resolveFilmDir` settles it once
the whole command line is known: a named directory wins; a bare `--film` takes
the **animation's own directory** (the caller named that path as the destination
for this run's product, so the frames land beside the picture they produced);
and bare `--film` with no `--out` is **refused by name** — the film would have
nowhere to write, and choosing somewhere silently is how a directory full of PNGs
turns up where nobody looked.

**2. The help-surface inventory is gated, and it fired.** Adding a flag to
`gopi chat` turned two gates red — the checked-in inventory
(`devdoc/gopi-chat-web-help-surface-inventory.md`) and its `g5ExpectedFlags` pin.
Neither was hand-edited: the count was **re-derived** with the inventory's own
generator, `scripts/g5-chat-web-help-surface.sh`, which reports 15 flags for
`gopi chat` and 41 for the group (was 14/40). Rule 38: a census figure names its
generator.

---

## Files

| file | change |
|---|---|
| `cmd/agent/chat_film_gif.go` | new — the animation arm: `--out` parsing, the caption, `chatFilmComposeGIF`, B3's refusal |
| `cmd/agent/chat_film.go` | `runChatFilmOut`, `resolveFilmDir`, `readFilmLedger` (with `readFilmBeats` as its projection), per-beat caption capture, `composeGIF`, the usage block |
| `cmd/agent/cli_chat.go` | `--out` in the argv walk; `gifOut`/`dumpFlagStated` tracking; the B3 and `--out`-needs-`--film` refusals; dispatch; the flag vocabulary; the usage lines |
| `cmd/agent/chat_film_gif_test.go` | new — flag parsing, the 2-frame minimal sample, the caption, B3, the vocabulary |
| `cmd/agent/chat_film_test.go` | the bare-`--film` cases and `resolveFilmDir` |
| `cmd/agent/cli_chat_web_help_surface_basic_test.go` | the re-derived flag pin (40 → 41) |
| `devdoc/gopi-chat-web-help-surface-inventory.md` | the re-derived table row and Totals |

No new dependency, no new verb, no new file format: `go.mod` and `go.sum` are
unmodified.
