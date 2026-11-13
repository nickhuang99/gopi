---
title: GOPI-CHAT
section: 1
header: GoPi Manual
---

# NAME

gopi-chat - an interactive, multi-turn chat session on the gopi CLI

# SYNOPSIS

<!-- BEGIN SYNOPSIS (copied verbatim from `gopi <verb> -h` by internal/standards/mancensus) -->

```
gopi chat [--tools] [--no-editor] [--no-color] [-v|--verbose] [--dump-screen[=PATH]|--no-dump-screen] [--screen-stream[=PATH]|--no-screen-stream] [--screen-replay SESSION|FILE] [--voice-audio FILE] [--resume SESSION] [--replay SESSION [--from-turn N]] [--policy POSTURE] [--film DIR [--out FILE.gif]] [chat-ideal-geometry COLSxROWS] [ambiguous-width wide|narrow] [chat-surface-turns N] [raw-log-path|session-id]
       gopi chat --once [-p|--prompt PROMPT] [--tools] [--max-turns N]
                        [--max-seconds S] [--stop-on TEXT]
```

<!-- END SYNOPSIS -->

# DESCRIPTION

**gopi chat** keeps a conversation context across turns: each reply is sent as
a free-form user message that continues the previous conversation, and the
model's text streams to stdout in real time.

Unlike a one-shot `gopi "<prompt>"` run, a chat is a SESSION: it can be
resumed, replayed, filmed, and — with `--tools` — given the agent's tools so the
model can read and edit files while you talk to it.

The synopsis above is COPIED from this verb's own `gopi chat -h` page by
`internal/standards/mancensus`; `cmd/agent/man_source_gate_test.go` fails the
build when the two drift. This page is the home for the detail a help index
does not carry: the flags below are this verb's own, and the paragraphs that
explain them belong here rather than on the help page.

# OPTIONS

Every flag below is one `gopi chat -h` accepts. The synopsis above is GENERATED
from that page; the flag spellings in this list are checked against it by the
same gate (`cmd/agent/man_source_gate_test.go`) — a flag named here that the
help page does not print fails the build — while the prose is this document's
own.

`--once`, `-p [PROMPT]`
: run ONE turn and exit — no prompt loop, no terminal required, no editor. The
prompt comes from `-p`/`--prompt`, from the positional argument, or from stdin
when neither is given. This is the scripted, agent-driven face of `gopi chat`,
and it is the shape to reach for in a pipeline.

`--prompt TEXT`
: the explicit prompt spelling of `--once`, with the same precedence.

`--max-turns N`
: cap the number of turns a chat run may take, and ARM the pipe-back driver
(the engine's own answer becomes the next turn's prompt). This is the CAP, not
the listing word: everywhere else in gopi `--turns` prints the turns and plays
nothing (`gopi session log <id> --turns`, `gopi session replay <id> --turns`),
so one spelling means one thing across surfaces. `--turns` written on this face
is REFUSED BY NAME and points here, never read as a cap.

`--max-seconds S`
: cap the wall-clock time a chat run may take.

`--stop-on TEXT`
: stop the run when the model's output contains TEXT.

`--tools`
: tool-enabled chat ("ops mode"): the model may call the agent's tools
mid-conversation.

`--no-color`
: no colour or emphasis anywhere in the chat chrome.

`--no-editor`
: disable the raw-mode line editor and read plain cooked lines.

`--verbose`, `-v`
: print the startup plumbing — session paths, the loaded configuration.

`--dump-screen[=PATH]`
: write a screen dump of this chat session. It is the artifact
`gopi ui_state --target chat` reads, and an unarmed chat writes none.

`--no-dump-screen`
: turn the screen dump OFF for this run: no artifact is written.

`--screen-stream[=PATH]`
: record the renderer's RAW bytes — every paint write, verbatim and unthrottled,
with no frame dropped — instead of the dump's interpretation of them. **ON BY
DEFAULT**: the bytes of a paint sequence cannot be re-created afterwards, so a run
that did not record has nothing to replay, while the cost of recording is one file
beside the dump. The artifact is `<session-id>.screen.stream`,
decimal-length-prefixed frames between a `#gopi-screen-stream` header and a
`#gopi-screen-stream-end` trailer. A file recorded before the 2026-11-08 rename
(`.sgr.stream`, `#gopi-sgr-stream` header, `#gopi-sgr-stream-end` trailer) is
still READ — by this verb's `--screen-replay` and by `gopi session replay` — and
nothing new is ever written under that name.

  WHY IT EXISTS BESIDE THE DUMP RATHER THAN INSTEAD OF IT: the dump is this
  program's INTERPRETATION (throttled to one write per frame boundary, reduced
  to the cells the grid model can express), so both halves of "was that wrong
  cell the renderer or the reader?" come from the same interpreter. These bytes
  are parsed by nothing on the way in, so they replay through ANY interpreter —
  this build's, an older one, a stranger's — and the comparison is then a
  comparison of two readings, not of a thing with itself.

  With no PATH it lands beside this run's own session artifacts, named after the
  session. Joined form (`--screen-stream=PATH`) only, for the reason
  `--dump-screen` gives. A replay is redirected into this chat's own scratch
  directory, never into the recording it is reading. On a `--film` command line
  it is REFUSED by name: the painting process is a second gopi inside the
  private terminal and does not inherit argv — set `GOPI_SCREEN_STREAM` there
  instead, which the producer does inherit.

`--no-screen-stream`
: turn the raw stream OFF for this run: no artifact is written. It takes no value
(`--no-screen-stream=x` is refused by name). A declined run SAYS so — once before the
terminal is taken, once in its closing report — because a stream that is armed by
default and silently absent would be indistinguishable from a switch that failed
to arm.

  `--screen-stream` and `--no-screen-stream` may both appear on one command line: the
  LAST one wins (`--no-screen-stream --screen-stream` records; the reverse does not).
  The pair is the two directions of one switch, so an exported
  `GOPI_SCREEN_STREAM=0` overridden by `--screen-stream` on a single command line
  stays the ordinary way to re-arm a run. The off state costs the render path
  nothing: one nil check, no syscall, no file.

THE PRE-RENAME SPELLINGS ARE DEPRECATED ALIASES, NOT DEAD WORDS (operator
ruling, 2026-11-08): `--sgr-stream[=PATH]`, `--no-sgr-stream` and the older
sibling of `--screen-replay`, `--sgr-replay SESSION|FILE`, still parse and
behave exactly as the three flags above, and each one prints

    gopi: --sgr-stream is deprecated: use --screen-stream (still honoured; the old spelling is an alias)

on stderr, so a shell history or a script written before the rename keeps
working and learns the current word from the run itself. What the rename changed
is the NAME (the recorded bytes are a screen paint sequence — SGR is one escape
class inside it, not the thing being recorded); what it did NOT change is any
byte on disk: nothing is ever written under `.sgr.stream`, and every reader still
accepts that suffix and its `#gopi-sgr-stream` header.

`--screen-replay SESSION|FILE`
: PLAY BACK a recorded raw byte stream — read the frames `--screen-stream` wrote and
write them to stdout verbatim, in order, adding nothing and parsing nothing. This
is the only evidence channel that does not depend on the RENDER ALGORITHM:
`--replay` re-RUNS a session, so its output is this build's reading of it (turn
numbering, cache rows, layout, throttle) and two builds are supposed to differ; a
raw stream is what the renderer actually wrote, so two builds replaying one file
must produce byte-identical output. The comparison is `sha256sum` of the two runs,
and it must be one value.

  THE OPERAND HAS TWO FORMS. An existing `.sgr.stream` path is used exactly as
  written — a typed path is never re-derived, because re-deriving it would silently
  play a different file. Anything else is read as a session id, resolved the way
  every other session operand is, and the stream beside that session's record is
  played.

  THE PICTURE CARRIES NO TEXT OF ITS OWN. Every state word `--replay` prints is
  dropped here, because a line written between two frames is a byte the recording
  never contained and the comparison would see it. The state stays observable
  without a word: a paused run's stdout stops growing. Two things are still said,
  on stderr and after the picture — a stream whose recording run never wrote its
  close-out record (its bytes are genuine and its last frame may be partial, and
  saying so keeps a truncated file from reading as a short one), and a stream that
  cannot be read at all, which is the one case with a non-zero exit status.

  CONTROLS ARE `--replay`'S OWN, not a second vocabulary: any key pauses, any key
  resumes, Esc leaves. With stdin redirected there is no watcher — no terminal, no
  throttle, no controls — and the run plays at full speed, which is what a capture
  or a hash wants. The pace is a watching pace; a stream records bytes and not
  time, so the original timing is not reproduced and nothing claims that it is.

  A command line naming two things to run (`--replay`, `--resume`, a positional,
  `--film`) is REFUSED by name rather than silently picking one. `--screen-stream`
  beside it is stated as inert, not refused: a playback writes no chat surface, so
  there is nothing to record.


`--voice-audio FILE`
: name the audio the `/voice` mode READS instead of capturing it — a 16kHz mono
s16le PCM file, or a WAV of exactly that geometry (anything else is refused by
name, with the rate, channels and depth it carries). NO capture device is
enumerated or opened. The file is delivered at the rate it was spoken, so the
mode's windows, boundary rules and transcripts are the ones a live capture
produces. From the environment the channel is `GOPI_CHAT_VOICE_AUDIO`; the
flag wins, and a bare flag is a usage error rather than a silent fall back to
the microphone.

:warning: `/voice` runs the LIVE streaming arm when the engine is `streaming`
(v2) or `sauc` (v3) — the transcript is rewritten in the editing line WHILE you
speak. For `sauc` that means the BIDIRECTIONAL endpoint (`sauc/bigmodel_async`)
rather than the batch-semantics one a file run opens; measured 2026-10-13, the
first text arrives about 2.5 s into a 4.8 s clip and is rewritten every ~300 ms
until the sentence closes. `audio_engine_param.sauc.endpoint` overrides the
choice, on both arms. The `recording` and `local` engines keep the six-second
window loop.

`--resume SESSION`
: continue a session named by the seed argument.

`--replay SESSION`
: re-run a RECORDED session on this very surface.

`--policy POSTURE`
: the posture this run gives a command line the rule table has NO opinion
about. The spellings are `allow`|`yolo`|`auto` (all say allow),
`on-request`|`ask`|`prompt` (all say ask — the DEFAULT) and
`forbidden`|`deny`|`never`. Four channels decide, highest first: this flag,
then `GOPI_CHAT_POLICY`, then `settings.json` `"chat_policy"`, then the
built-in default. A value that is not one of the spellings is REPORTED and
the next channel decides — it is never silently read as the default. The
posture is resolved ONCE at startup and frozen: no tool, model or later flag
can widen it. The legacy `chat_op_auto` boolean is mapped, not
reimplemented (`true` reads as allow and says so once). Rules come from the
fenced `chat-policy` block of the project `GOPI.md`; `--verbose` prints how
many were loaded and where they came from.

ENFORCED, not merely reported. The chat write boundary
(`internal/tools/chat_write_guard.go`) judges a command line against this
frozen posture when a tool call arrives: it REFUSES a command the posture
forbids and runs one it allows, and both directions are measured. That is
still not a promise that `yolo` runs your commands — a rule in the
`chat-policy` fence can narrow what the posture allows, and a command that
runs can still fail on its own.

`--film DIR`
: film a replayed session: run the recorded session inside a terminal and
assemble the frames.

`--out FILE.gif`
: the GIF `--film` writes.

`--film-terminal NAME`
: the terminal emulator `--film` drives.

`--wechat`
: the film/replay presentation variant.

`--scratch DIR`
: the BEAT PROTOCOL's scratch directory.

`--max-beats N`
: how many beats the beat protocol may run.

`--ack-timeout-ms N`
: the beat protocol's acknowledgement timeout.

`--cols N`, `--rows N`
: the surface geometry a filmed or beat-driven run is drawn at.

`--chat-ideal-geometry COLSxROWS`
: the chat surface's ideal geometry, and therefore the width its canvas is
drawn for. At least this wide the canvas is drawn at EXACTLY this width — the
remainder to the right stays blank, the box is never stretched, and the info
bar's columns are pinned anyway. A narrower terminal keeps its own width and is
TOLD which piece of chrome it cannot host, never a silent downgrade. Rows are
never imposed: the status row is anchored to the screen's LAST row, so the
ideal row count appears only in that notice and in the film's own default. The
default is the geometry every live measurement of this surface was taken at.
The joined form (`--chat-ideal-geometry=COLSxROWS`) and the pre-verb form
(`gopi --chat-ideal-geometry … chat`) are accepted, and a value that is not
`COLSxROWS` is reported so the NEXT channel decides — a typo never makes chat
unusable. See `CONFIG.md` for the full row.

`--ambiguous-width wide|narrow`
: how many cells an East-Asian **ambiguous** rune (box drawing, arrows, some
punctuation) occupies on the terminal this chat draws into. `wide` = two cells
(the CJK-terminal reading), `narrow` = one (the Western reading, gopi's
historical behaviour). It changes CELL ARITHMETIC only, never the text. Declaring
neither word leaves the LOCALE in charge — `LC_ALL`, then `LC_CTYPE`, then
`LANG` — and a locale that says nothing means `narrow`. Same knob via
`GOPI_AMBIGUOUS_WIDTH` or the settings key `ambiguous_width`; the flag is
published into that environment channel before the subcommand chain, so all
three channels meet in ONE reader. An unusable value is reported and the next
channel decides — a typo never makes chat unusable. See `CONFIG.md` for the full
row.

`--chat-surface-turns N`
: how many **self-driven turns** the `chat_surface` tool may add to ONE
**period** — the stretch of conversation between two lines a HUMAN wrote. The
default is 20, accepted `2..1000`. Every line you submit opens a fresh budget;
the model can never renew it itself, and the tool's own `max_turns` argument is
clamped INTO this range, so it can shorten a period but never lengthen one. The
number is announced on the console (`self-driven turn 1/20 of this period`), and
a period that spends it CLOSES with a notice rather than running on. Same knob
via `GOPI_CHAT_SURFACE_TURNS` or the settings key `chat_surface_turns`; the
joined form (`--chat-surface-turns=30`) and the pre-verb form
(`gopi --chat-surface-turns 30 chat`) are accepted, and an unusable value is
reported so the NEXT channel decides. See `CONFIG.md` for the full row.

`--film-terminal NAME`
: the terminal emulator `--film` drives. Usable names: `xterm`,
`xfce4-terminal`. Same knob via `GOPI_FILM_TERMINAL` or the settings key
`film_terminal`. This is the answer to "the glyphs are too small": the font
belongs to the TERMINAL the film runs in, not to the geometry, and nothing here
changes a font.

`--out FILE.gif`
: encodes the photographed frames into ONE looping GIF, each frame captioned
with its own beat number. The parity of BOTH dimensions and the Netscape loop
block are held for EVERY product, not only `--wechat`: an encoder that
subsamples chroma refuses an odd frame dimension outright, whichever reader is
named.

`--wechat`
: the film/replay presentation variant — the same GIF held to a phone
conversation view's own byte ceiling. Over the ceiling the run REFUSES with the
numbers rather than handing over a file the phone would reject.

`--scratch DIR`
: the BEAT PROTOCOL's scratch directory: this run reads the beat ledger from
DIR, acknowledges each beat and writes into DIR — the SAME directory name the
replay launcher takes, so the driver films this surface with the launcher it
already has.

`--cols N`, `--rows N`
: the surface geometry a filmed or beat-driven run is drawn at, as two positive
integers. A run REFUSES a terminal that disagrees with them: a film drawn at
another width is a different picture.

`--max-beats N`
: the most beats a run renders, so a long recording cannot render for ever; the
film then holds at most this many frames. Omitted, the protocol's own default
bounds it.

`--ack-timeout-ms N`
: how long one beat waits for its acknowledgement, in milliseconds. Omitted,
the protocol's own default bounds the wait.

`--memory-prefetch`
: typing-time READ-ONLY memory prefetch for the chat — the hits ride into the
next turn. Nothing is written to the memory DB; each run appends one record
line per event to the session directory. Written bare it means ON, which is how
the example above writes it; `--memory-prefetch=off` turns it off again, which
is the way to override an exported `GOPI_MEMORY_PREFETCH`. Precedence:
`--memory-prefetch=off` > `GOPI_MEMORY_PREFETCH` > settings `memory_prefetch`.
OFF means OFF: nothing is started, no query is issued, and the prompt is
byte-identical to the pre-knob tree. This is a TOP-LEVEL flag: write it before
the verb (`gopi --memory-prefetch chat`), never after.

`--memory-prefetch`
: typing-time READ-ONLY memory prefetch for the chat — the hits ride into the
next turn. This is a TOP-LEVEL flag: write it before the verb
(`gopi --memory-prefetch chat`), never after.

# ENVIRONMENT

GOPI_PALETTE
: which emphasis table to draw. Left UNSET the run follows the terminal's own
depth: `"hermes"` on a terminal advertising 24-bit colour (`COLORTERM=truecolor`),
`"palette256"` where the terminal declares the 256-colour palette (`TERM`
contains `"256color"` — e.g. `xterm-256color`, `tmux-256color`), and `"basic"`
on the 8/16-colour floor (8 named colours, works everywhere,
theme-independent). `"hermes"` is a warm GOLD palette for a dark terminal — the
answer body is `#FFF8DC`; `"hermes-light"` is the same roles for a light
background. A table the terminal cannot draw is never emitted: the run falls
back to the deepest table it CAN draw and says so on stderr, naming the table
asked for and the variable that would enable it (`COLORTERM=truecolor`). An
unrecognised name is reported the same way and the run continues on the
default. Set it in your shell profile. The capability decision itself lives in
`internal/lineedit`.

GOPI_CHAT_VOICE_AUDIO
: the environment spelling of `--voice-audio FILE` (the flag wins).

GOPI_CHAT_DUMP
: the environment spelling of `--dump-screen`: `1` selects the default path and
any other value IS the path; a falsey value (`0`, `false`, `off`, `no`,
`disabled`) turns the dump off. The flag overrides it, and `--dump-screen`
re-arms the dump in the face of a falsey value.

GOPI_SCREEN_STREAM
: the environment spelling of `--screen-stream[=PATH]` / `--no-screen-stream`: unset
means the default (recorded); a truthy value (`1`) selects the resolved path; a
falsey value (`0`, `false`, `off`, `no`, `disabled`) declines the stream exactly
as `--no-screen-stream` does; and any other value IS the path. The flag overrides
it. This is the channel that WORKS on a `--film` command line, where both flag
spellings are refused by name — the painting process inside the private terminal
inherits the environment and not argv.

  The name is terminal-agnostic on purpose: the recording is not a chat-only
  capability, and a worker run's output is worth having verbatim too. The
  PRE-RENAME spelling `GOPI_CHAT_SGR_STREAM` is still read (the current name wins
  when both are set), and a run that finds only the old one says so on stderr, so
  a shell profile written before the rename is visible rather than merely
  tolerated. The artifact is `<session-id>.screen.stream` between a
  `#gopi-screen-stream` header and a `#gopi-screen-stream-end` trailer; files
  written under the old `.sgr.stream` / `#gopi-sgr-stream` spelling are still
  READ, by this verb's `--screen-replay` and by `gopi session replay` alike.

# THE STATUS ROW — THE CONTRACT `-h` ROUTES TO

`gopi chat -h` defines every symbol the row can print; this section carries the
two facts that belong to the CONTRACT rather than to the glossary, which is why
they live here and not on the routing page.

**A row that shows the cumulative rate alone is a NARROW row, not a session with
no turn.** The metrics group prints the context pair and the prompt cache's
`Σ`/`Δ` pair together; when the terminal cannot hold the whole group, the `Δ`
reading is dropped WHOLE — never truncated to a digit or a stray marker. So an
absent `Δ` states the terminal's width, and an absent `Σ` would be the only
thing that could mean "no turn to measure yet".

**The predicted-versus-measured reconciliation is in the session log.** The row
shows either the backend's own prompt count (`≡`, a measurement) or gopi's local
estimate (`≈`, an approximation); the two are reconciled PER REQUEST and the
signed delta is written down rather than inferred from the screen:

```
gopi session log <id> --json      # one token_reconciliation row per request
```

The ladder both sides use, the order the cells yield in, and the measurements
behind them are in `devdoc/chat-help-layering.md`.

# THE SCREEN DUMP, AND THE AUDIT BUILT ON IT

`--dump-screen` writes a THROTTLED SAMPLE (one write per frame boundary, at
most one per 100ms) of the paint stream, and the layout paints only when the
screen CHANGES — so an idle chat freezes at `dumps=1` and then writes nothing.
That makes `frames>0` a statement about how much paint happened, never a
promise that anything is on the grid: the GRID is the answer. A blank grid
with `frames>0` is qualified as such by `gopi ui_state --target chat` (an
ALL-BLANK notice), and the automatic audit REFUSES it rather than reading it as
agreement.

WHO READS IT — gopi itself, from inside, with no human in the loop:

- the answering turn, through its own `chat_surface` tool: `action=see` reads
  the painted grid as a cell grid, and `action=compare` checks a line it wrote
  against BOTH layers (the painted cells and the authoritative transcript);
- the CLI verb, which audits the WHOLE answer as ONE call:

```
gopi ui_state --target chat --all
```

Nothing above asks a reader to assemble the observation by hand: the audit
derives its authority from the dump (the session record one level above it),
so it needs no line to check and no session named. Its ledger reports how many
lines of the answer arrived on the painted grid and how many disagreed, with
the direction (which layer lost it) per disagreement. `gopi ui_state -h`
carries the audit's damage vocabulary (`paint_loss`,
`wrap_artifact_or_loss`, `cjk_wrap`, `emphasis_unpainted`) and the
single-line narrowing of the same check — whose authority is derived the same
way, and which names a different transcript on purpose.

THE AUDIT'S EXIT CONTRACT, from its ledger word `outcome=agreed / disagreed /
could-not-run`: `0` = the audit RAN and the two layers agreed; `3` = at least
one line disagreed; `1` = the check could NOT RUN (no dump armed, nothing
painted, a fallback geometry) — deliberately NOT "they agree", because an
unchecked screen is not a verified one.

WHY IT IS ON BY DEFAULT, AND WHAT IT COSTS. The sink re-interprets the paint
stream through `uiclient.ScreenDumper` — CPU plus one throttled write per
frame boundary, measured 5793 bytes for a 117x26 frame. That is the CEILING,
not the rate: the layout paints only when the screen changes and an idle chat
stops writing after its first dump. A self-observation capability that is off
until someone remembers a flag is one nobody has, so the default is armed per
run — and the path is printed before the REPL takes the terminal (the
"screen dump ->" line at start-up), so the record is never written in silence.
The frame/write ratios and the session-level measurements behind these numbers
are in `devdoc/chat-help-layering.md`.

# RECOVERING A CRASHED CHAT

Every chat RECORDS the options it was started with, so a crashed one comes back
with them. Find the session, then resume it:

```
gopi session log --sessions     list sessions (a crashed one has no END row)
gopi chat --resume <session-id>
gopi chat <session-id>
```

`--resume <id>` continues the session WITH the options that session was started
with (`--tools`, `--no-color`, a `--dump-screen` destination, `--no-dump-screen`,
`--screen-stream`, `--no-screen-stream`, …), restored from the session's own record — one word, no command line to
reconstruct. `<session-id>` as the seed does the same, but only when this
command line states no options of its own.

WHAT WINS: the recorded options are COMBINED with this command line, so
anything written here adds to them — and overrides them for the one option that
has two candidate values (which dump destination, or no dump at all). A run
that states NOTHING about the dump does not count as stating one: the default
is armed, and the record's own choice still wins. Which options were restored
is printed at start-up, and an option recorded by ANOTHER gopi version is named
as not restored rather than dropped in silence.

WHAT CANNOT BE RESTORED: a session that predates the record (or a run with no
options) recorded nothing — that is said out loud, and its flags must be
replayed by hand. The record is per RUN, so each hop of a repeated crash carries
the options forward.

`gopi session list` shows chat ROWS, which only exist once a turn has
completed: a chat killed before its first reply is in
`gopi session log --sessions` and has no row at all.

# RECORDING, REPLAYING AND FILMING

The five worked command lines on `gopi chat -h` are the discovery path; what
each step's flags MEAN is here.

`--replay SESSION --scratch DIR --cols 117 --rows 26`
: the beat protocol's own command line, and the one the film driver runs as its
producer: the scratch holds the beat ledger the run acknowledges, and the two
integers are the geometry its layout is installed at (a terminal that disagrees
is refused).

`--film DIR`
: photographs the re-run once per drawn beat, writing `frame-%04d.png` and
`beats.tsv` into DIR.

`--out FILE.gif`
: encodes those frames into ONE looping GIF, each frame captioned with its beat
number; `--film DIR` alone writes the frames and the ledger only. This is how to
WATCH a session rather than read its dump.

A RECORDING also records the terminal it was on (columns × rows), so a later
replay draws at the size the session really happened at rather than at a
built-in default; `gopi session log --sessions` lists recorded sessions oldest
first, and the id it prints is what `--replay` takes. An EXPLICIT geometry wins
over the recording's own.

WHAT COLS×ROWS MEANS IN PIXELS, cell by cell. It is a CELL count, not a
physical size, so the film's reference terminal — xterm with its built-in fixed
face — gives one cell = 6×13 px and a frame of:

```
COLS × 6  by  ROWS × 13  pixels, plus xterm's own 4 px border
```

which reads as:

```
117x26   the ideal, and the film's default   -> 706x342px
100x30   a smaller film                      -> 604x394px
80x24    a classic 80-column terminal        -> 484x316px
```

# FILES

`~/.gopi/sessions/<session-id>/`
: the chat session's own directory — its session log, raw log and, when
`--dump-screen` is armed, `chat-screen.dump`; when `--screen-stream` is armed,
`<session-id>.sgr.stream` beside it (the renderer's own bytes, which the dump's
grid is one interpretation of).

`screen-turn<N>.sgr.stream`
: written by `/screen`, holding THE SCREEN OF TURN `<N>` — one turn's painting,
WHOLE. `N` is the turn the file's own bytes paint, taken from the renderer's
turn header (`──── turn N · <prompt>`), never from a counter, so the name and the
content cannot disagree. `/screen` with no argument captures the CURRENT turn
(the newest one the recording holds); `/screen 58` captures turn 58. It is a
valid recording in its own right: the same length-prefixed format, its own header
and trailer, and the moment the SOURCE began rather than the moment the copy was
made. The next turn's message carries the file's absolute path, its byte count,
its frame count and how to read it in pages — never its bytes, which is the whole
reason the command writes a file.

Nothing is truncated and there is no way to ask for less: a screen the model only
half saw is a screen it will describe wrongly. A turn the recording does not hold
is REFUSED by name, with the range of turns that would have worked, and a
recording that carries no turn header at all (an old stream) is refused too —
a segment chosen by guesswork is worse than no segment.

`/screen` costs no model turn and never blocks: it is answered by the REPL
locally, and the reference it parks is consumed by exactly ONE following turn.
A run that has painted nothing is told so ("nothing has been drawn since the
stream opened") rather than handed a reference to an empty file, and a run that
declined the recording (`--no-screen-stream`) is told which flag would have given
it one.

`context-turn<N>.md`
: written by `/context`, holding the EXACT REQUEST the model received at turn
`<N>`: the system prompt as it was sent, every message in order (text, thinking,
tool calls with their input, tool results, images summarized as type + size +
sha256) and a token count per block and in total. `N` is a TURN — the same
axis `gopi session show <id>` counts — and the page header also states the
interval of ROUNDS that turn covered, because one turn routinely takes dozens
of round-trips and a request-level export must never be read as a turn.
`/context --round N` exports one ROUND instead and names the file
`context-round<N>.md`.

NOTHING IS PRINTED EVERY TURN. The console does NOT summarise the request it
sent: a line on every turn is a cost paid by every reader, and the facts it
carried are the file's own header. The body is never printed either — it is tens
of thousands of tokens, and anything printed into the conversation is re-sent with
every later turn — so `/context` is the discovery path and the file is the
payload. `/context` with no argument exports the newest turn; `/context N`
exports turn `N`; `/context -h` (or `/help context`) documents the flags. It
costs no model turn and injects nothing into the next one.

# SEE ALSO

`gopi chat -h` (the full help page for this verb), `man gopi` (the CLI index),
`gopi-session(1)` (inspecting a session after the fact).
