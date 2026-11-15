<!-- doc-class: normative -->
# Kitty keyboard protocol: Shift+Enter arrives as `CSI 13;2u`

## The defect

`gopi chat` in raw mode could not tell Enter from Shift+Enter. In raw mode the
terminal sends `0x0d` for Enter, and a terminal that has not been asked to
disambiguate sends the SAME `0x0d` for Shift+Enter: the modifier information is
destroyed inside the terminal, before any byte reaches this process. No decoder
can recover what was never sent.

The decode half was already built: `internal/lineedit/keys.go` maps `CSI 13;2u`
(shift-enter) and `CSI 13;5u` (ctrl-enter) to `KeyNewline` — tested since before
this task (`internal/lineedit/value_test.go`, case `shift-enter-csi-u`). What was
missing was the *negotiation* that makes a terminal send it: the kitty keyboard
protocol's disambiguate-escape-codes enhancement.

## The negotiation

Three sequences (spec: kitty keyboard protocol, "Progressive enhancement"), all
built in `internal/sgr/kitty.go`:

| sequence | meaning |
|---|---|
| `CSI ? u` | query the current flags |
| `CSI > 1 u` | PUSH flags 0b1 (disambiguate escape codes) onto the terminal's stack |
| `CSI < 1 u` | POP one entry, restoring whatever the flags were |

Push/pop rather than set/reset is what makes the exit correct without knowing
what the operator's shell or a parent program had configured: the pop restores
it.

## Where the code lives, and why

- **`internal/sgr/kitty.go`** — the wire vocabulary and the reply parser. The
  protocol has two sides here (this project emits the query AND must recognise
  the reply), and a drift between the two spellings fails *silently*: the query
  goes out, the reply is not recognised, the feature never turns on. `internal/sgr`
  already owns the escape vocabulary (`TestEscapeLiteralsLiveOnlyInSGR`), is a
  leaf, and its `PrivatePrefix`/`PrivatePrefixAlt`/`PrivatePrefixGreater`
  constants were already named there for this family.
- **`internal/lineedit/keyprotocol.go`** — the probe, its bounds, the prefetch
  queue, the verdict, and the pop.
- **`internal/lineedit/raw.go`** — `Open` runs the probe after entering raw mode;
  `Close` pops before restoring termios; `ReadEvent` returns prefetched bytes
  first; `InputPending` counts them.
- **`cmd/agent/chat_keyboard_protocol.go`** — what the chat surface SAYS.

## The two constraints that shaped the design

### 1. The probe must be bounded

A terminal that does not implement the protocol says nothing. Waiting forever
would hang `gopi chat` at startup on every xterm and tmux. The window is
`keyboardProbeWindow` (200ms, polled in `keyboardProbeSliceMS`=20ms slices so the
deadline is evaluated on a clock this code owns). Measured over a pty: Open on a
silent terminal takes ~200ms and then proceeds.

### 2. The probe must never eat a keystroke

If the operator types while the probe is parked, those bytes are theirs. Every
byte read during the window goes into a `prefetch` slice, and the parser decides:

- complete reply (`KeyboardProtocolComplete`) → push, and whatever followed the
  reply's own span goes to the prefetch;
- anything else (`KeyboardProtocolNoReply`) → stop asking immediately, keep every
  byte in ORDER in the prefetch, replay them;
- still a strict prefix (`KeyboardProtocolPartial`) → read more, until the window
  closes.

The three-way verdict (not two) is deliberate: a bool cannot distinguish "not a
reply" from "a reply split across two reads", and guessing wrong either swallows
a keystroke or waits for bytes that will never come. `ReadEvent` returns the
prefetch before it polls the descriptor, and `InputPending` reports it as pending
input — so a byte the probe held back cannot be overtaken by a byte that arrives
later.

The parser is strict by construction and the strictness is tested as a SWEEP:
`TestReplyParserNeverClaimsAKeySequence` asserts `NoReply` for every sequence the
key decoder knows (`\r`, `\x1b\r`, `\x1b[13;2u`, arrows, bracketed paste, …) —
because a parser that claimed one of those would eat it.

## The exit path

`Close` pops. Not a second teardown, not a signal-only path: `Close` is already
the ONE restore path (a normal return, a deferred Close, a panic unwinding, and
`CaptureSignals`' fatal handler, which calls `Close` before re-raising). The pop
is guarded by `keyboardPushed`, so it is symmetric with the push and never fires
twice. `TestKeyProtocolPopOnTheSignalPath` drives the signal handler and observes
the pop.

## The invariants this code MUST keep

These are the standing claims of the design (a regression to any of them is a
defect, and each one has a test that fails red):

1. The probe MUST be bounded: `Open` on a terminal that answers nothing returns
   after `keyboardProbeWindow` and must never block the start of `gopi chat`.
2. The probe MUST NOT eat a keystroke: no byte typed inside the window may be
   dropped, reordered, or handed on split across a sequence boundary.
3. Push and pop MUST be symmetric — one stack entry each — so a run cannot leave
   the operator's terminal with the disambiguation enabled after exit.
4. `internal/sgr` is the single source of truth for the sequences; no other
   package may spell `CSI > 1 u` (Rule 13/14 — `TestEscapeLiteralsLiveOnlyInSGR`).
5. The decoder MUST be untouched: plain Enter submits, Shift+Enter (`CSI 13;2u`)
   and Alt+Enter insert a newline, and bracketed paste stays content.

## The observability (Rule 15)

`KeyboardProtocol` is a three-state verdict, not a bool:

| state | meaning | what the chat layer says |
|---|---|---|
| `KeyboardProtocolUnavailable` | no probe ran (cooked path, no terminal) | nothing — never blame a terminal for a question never asked |
| `KeyboardProtocolUnsupported` | asked, silent | the ONE Alt+Enter advice (through `WriteNotice`, so it takes the answer band instead of being erased by the next repaint) |
| `KeyboardProtocolSupported` | answered, enhancement pushed | nothing, unless `--verbose`, which prints the verdict and the flags |

## Measured on the real surface (not in this file's prose)

Driving the actual `gopi chat` binary inside a pty where the master plays the
terminal (a small harness that forks the binary on a pty, answers the query and
records the raw bytes): see the task report's `## Verification`, which carries the
invocation and the byte counts.
On the answering run the wire carries `…\x1b[?2004h\x1b[?u\x1b[>1u…` and later
`…\n\x1b[<1u\x1b[?2004l\x1b[?25h`; on the silent run the query goes out and NO
push does.

## Landed by cherry-pick, onto a master that had moved

This change was authored on 2026-09-28/29 and its branch was retired with its
commits still only in the object store; it reached master by `git cherry-pick` of
the three original commits (feature → test → docs), authors preserved. Master had
moved underneath it, and two collisions were resolved rather than overridden —
both of them the SAME class, "an older version of this feature arriving into a
tree that has since grown its own answer":

- **the prefetch queue.** `internal/lineedit` grew a capability probe of its own
  (`probe.go`, the colour measurement) with ONE replay buffer — `prefetch` +
  `probeMu`, read by `takePrefetch`, written by `addPrefetch`. The keyboard probe
  brings its own queue with the same job. They are now ONE queue: this file's
  appends go through `addPrefetch`, `ReadEvent` keeps draining through
  `takePrefetch`, and the two probes therefore cannot disagree about which byte
  arrived first. `Prefetched()` (the byte count, master's) and `PrefetchedBytes()`
  (this file's accessor for the ordering test) are the two observables over it.
- **the REPL's setup.** The startup screen (`chatComposeStartupScreen`) landed in
  `cli_chat.go` while this feature was in the object store. Both are setup steps
  and they are now sequential, not alternatives: the screen is composed and
  committed to the band, then the keyboard negotiation's outcome is announced
  through the SAME notice seam (`chatInput.WriteNotice`) the chat_surface notices
  already use.

### On real hardware (no pty fixture), 2026-09-28

The same run driven through a REAL `xterm` on the display, with `script`
recording the wire (the drive script and its two transcripts were left in the
authoring run's own session directory, `/home/user/.gopi/sessions/<that task id>/evidence/`,
which the session reclaimer has since collected — the measurements they recorded
are quoted here, the files are gone, and this note says so rather than citing a
path a reader can no longer open):

- the query `CSI ? u` goes out and **nothing answers it** — xterm implements no
  kitty keyboard protocol — so **no `CSI > 1 u` is written** and one `Alt+Enter`
  advice line is printed. That is the `KeyboardProtocolUnsupported` path measured
  on hardware rather than played by a fixture.
- a raw-mode probe of what xterm emits for the key itself (a script behind a
  runner, both in that same now-collected session directory) recorded **`Shift+Return` → `0d`** — the same byte
  plain Enter sends. This is the DEFECT's premise confirmed on real hardware: the
  modifier is gone before any byte reaches this process, which is exactly why the
  negotiation, and not a decoder change, is the fix.
- the pop: after the run ends the wire carries `\x1b[?2004l\x1b[?25h` and NO
  `CSI < u` — correct, and not a missing pop: the pop is symmetric with the push
  and no push happened. An observed absence, read as one.

**Superseded 2026-10-02: Alt+Enter DOES work on this host's terminal — measured,
not inferred.** The paragraph above read "unmeasured on this host" for two weeks;
the operator then measured it directly in the real terminal xterm, and the
measurement is the opposite of a silence:

- **`Alt+Enter` inserts a newline inside a message.** Done by hand, in the real
  `xterm` this host runs, against a live `gopi chat`: the cursor moves to a second
  line of the same message instead of submitting the turn. So the fallback advice
  the degradation path prints is CORRECT on the very terminal that needs it —
  which is the only claim the advice makes.
- **`Shift+Enter` still behaves as plain `Enter`** — it SUBMITS. Same run, same
  terminal: Shift is not reported, which is the silent-terminal path's whole
  premise confirmed by hand rather than by a fixture.
- **Alt+Enter works whether or not this binary's negotiation ran**, because it
  never depended on it: the terminal itself encodes Alt as the `ESC` prefix
  (`ESC CR` → `KeyNewline`, keys.go), a mapping that predates the negotiation.
  That is why the fallback needs no push: on a terminal that cannot report the
  modifier, the modifier is still in the byte.

**What the earlier run got wrong, and why it is worth recording.** It probed for
the byte with a raw-mode reader and recorded NOTHING for `alt+Return` (with and
without the `metaSendsEscape` resource) — an ABSENCE OF OBSERVATION, which it then
labelled as an absence of the capability. The lesson is the one Rule 15 keeps
stating: "the probe saw no byte" and "the byte is not sent" are different facts,
and a probe that is itself the suspect cannot distinguish them. The 2026-10-02
measurement is the human being the instrument — press the key in a real terminal,
watch the screen — and it decided the question the probe could not. Which xterm
resource made the difference remains unestablished; it no longer matters to the
advice, which is now measured to be right where it is printed.

## Landed by cherry-pick, onto a master that had moved

This change was authored on 2026-09-28/29 and its branch was retired with its
commits still only in the object store; it reached master by `git cherry-pick` of
the three original commits (feature → test → docs), authors preserved. Master had
moved underneath it, and two collisions were resolved rather than overridden —
both of them the SAME class, "an older version of this feature arriving into a
tree that has since grown its own answer":

- **the prefetch queue.** `internal/lineedit` grew a capability probe of its own
  (`probe.go`, the colour measurement) with ONE replay buffer — `prefetch` +
  `probeMu`, read by `takePrefetch`, written by `addPrefetch`. The keyboard probe
  brings its own queue with the same job. They are now ONE queue: this file's
  appends go through `addPrefetch`, `ReadEvent` keeps draining through
  `takePrefetch`, and the two probes therefore cannot disagree about which byte
  arrived first. `Prefetched()` (the byte count, master's) and `PrefetchedBytes()`
  (this file's accessor for the ordering test) are the two observables over it.
- **the REPL's setup.** The startup screen (`chatComposeStartupScreen`) landed in
  `cli_chat.go` while this feature was in the object store. Both are setup steps
  and they are now sequential, not alternatives: the screen is composed and
  committed to the band, then the keyboard negotiation's outcome is announced
  through the SAME notice seam (`chatInput.WriteNotice`) the chat_surface notices
  already use.

**This record is the LAST unlanded piece of the change, and it landed by
re-derivation, not by cherry-pick.** Measured 2026-10-02: the branch that
authored it (`task/01a0fb1052d27938a3c3c42b1021c79b`) was retired and deleted,
and every one of its other commits is on master by content — the three
cherry-picks above, patch-id identical, authors and dates preserved. The two
collisions recorded here were resolved once, on the way in; what arrives now is
this section alone, ported onto the master that has since grown the two
2026-10-02 sections below.

## What was deliberately NOT changed

- the decoder (`keys.go`) — its CSI-u mapping already existed and its semantics
  are untouched;
- plain Enter (still submits) and bracketed paste (still content) — asserted by
  `TestKeyProtocolBareEnterStillSubmits` and
  `TestKeyProtocolCRLFDedupSurvivesTheProbe`;
- non-tty paths — the probe lives inside `Open`, which a pipe never reaches
  (`IsTerminal` gate), so a piped `gopi chat` emits zero extra bytes.

## The startup screen consumes the verdict (2026-10-02)

The negotiation had one operator-facing surface (`chatKeyboardUnsupportedNotice`,
a notice printed at startup). It now has a second, and it is the one a new user
actually reads: the chat startup screen's **"Tips for getting started"** panel
carries the newline key, and its WORDING is the three-state verdict.

**Why the panel and the notice are not redundant.** The notice is a one-shot line
that scrolls away with the first answer; the panel is the screen. An operator who
did not read the notice had, until now, nothing left on the screen that said how
to write a second line — and the two keys differ by terminal, so it is not
guessable. The panel is therefore the first place this feature reaches a reader
who was not looking for it.

**The three states, as the panel renders them** (`chatBannerTipNewline`,
`cmd/agent/chat_banner.go`; the wording lives in the named constants
`chatBannerTipNewlineKitty` / `chatBannerTipNewlineFallback`):

| verdict | the panel's line |
|---|---|
| `KeyboardProtocolSupported` | `Shift+Enter inserts a newline` |
| `KeyboardProtocolUnsupported` | `Alt+Enter inserts a newline (this terminal cannot report Shift+Enter)` |
| `KeyboardProtocolUnavailable` (cooked / piped) | **nothing** — no tip is drawn |

The fallback line states the key AND the reason on one row, and that is a
measured decision rather than a preference: the obvious key is the one that does
not work here, so naming only the substitute ("Alt+Enter inserts a newline")
leaves the operator who reaches for Shift+Enter with a submitted message and no
explanation. The explanation is exactly what the negotiation measured, so it is
the honest half of the sentence.

**Measured on a real pty, three states, by playing the terminal** (this branch's
binary, 100x30, `python3` stdlib pty; the transcript and the raw masters are in
that run's session directory):

- **silent** — the query `CSI ? u` goes out, nothing answers, no `CSI > 1 u` is
  pushed, and the panel's row reads `Alt+Enter inserts a newline (this terminal
  cannot report Shift+Enter)` — the whole sentence, uncropped.
- **answering** — the harness sends `CSI ? 1 u`, the surface pushes
  `CSI > 1 u` (and pops `CSI < 1 u` on exit), and the panel's row reads
  `Shift+Enter inserts a newline`. The other state's line is absent: the verdict
  is exclusive.
- **cooked (pipes, no terminal)** — `frames=0`, the historic one-liner is
  byte-identical to master's binary, and NEITHER newline line appears.

**The panel's width had to change with it, and that is part of the measurement.**
`chatBannerPanelMaxCols` was 44; the fallback sentence is 71 cells, so at the old
cap the operator's own terminal would have shown
`Alt+Enter inserts a newline (this terminal …` — the reason cropped off the line
whose whole point is the reason. The cap is now 71 and
`TestChatBannerPanelHoldsEveryTipWhole` pins the relation: at the widest terminal
the panel ever gets, every tip the panel draws is drawn WHOLE, so a future longer
tip fails red instead of being silently cropped on the operator's screen.

## Resolved against the tips panel's own restructure (2026-10-02, merge commit)

The section above was written against the panel as it stood when this task
started. WHILE THIS TASK WAS IN FLIGHT, `master` landed its own redesign of the
same panel (`6544dad5` — "startup screen takes the page over, and its tips
rotate"): the tips split into a MUST list drawn on every run plus a WEIGHTED
ROTATING pool, and `chatBannerTipsMax` went 5 → 6 to leave the rotation a slot.
That commit deliberately did NOT settle the newline wording — its own comment
says "the surface cannot yet say which of the three cases THIS terminal is in",
and names the tri-state wording as the sibling task's deliverable (this one).

So the two are one design now, not two competing ones, and the resolution is
worth recording because it is the part a later reader would otherwise re-derive:

- **The newline line is a MUST tip, and its wording is the verdict.** It is
  produced by `chatBannerTipNewline(spec)` — the three-state function — and it
  enters the must list through `chatBannerMustTips`. Being a must tip is the
  point: the key that inserts a newline is exactly the knowledge that must not
  rotate away.
- **The three states are unchanged** (`chatBannerTipNewlineKitty` /
  `chatBannerTipNewlineFallback` / nothing), and the cooked path still prints no
  tip at all.
- **`KeyboardProtocolUnavailable` is "does not apply", not "ranked last".** An
  inapplicable must tip costs no row: its slot is handed to the rotation, so a
  cooked run still fills the panel. `chatBannerTip.applies` carries this, and
  `chatBannerToolsOnly` is its second user (`/tools` on a tool-less run).
- **`chatBannerTipPriority` names the MUST list's order.** Every must tip is
  drawn, so the ranks are a statement about what the screen LEADS WITH rather
  than a capacity trade — and one of them is a contract, not a preference: the
  interrupt sentence must precede the exit sentence, because
  `testsuite/chat_pty_interrupt_test.go` reads the pty stream and stops at the
  exit sentence's marker.
- **The queue tip survived the restructure as a rotating line** (low weight): the
  redesign's pool did not carry it, and a restructure that silently deletes a
  fact about the REPL is a regression wearing a new layout.
- **`chatBannerPanelMaxCols` stays 71** — the fallback sentence is still the
  widest tip any state draws, and the width reasoning above is unchanged.

**The three states were RE-MEASURED on the merged tree** (branch tip after the
delivery-point sync, real pty 100x30, python3 stdlib harness in that run's
session directory), because a merge that changes the panel changes what the
panel prints:

| mode | the wire | the panel's line |
|---|---|---|
| silent | query `CSI ? u` out, NO push, NO pop (9199 B) | `Alt+Enter inserts a newline (this terminal cannot report Shift+Enter)` — at byte 3296, the whole sentence |
| answered | query out, push `CSI > 1 u`, pop `CSI < 1 u` (8949 B) | `Shift+Enter inserts a newline` at byte 3327; the other state's line ABSENT |
| cooked (pipes) | no query at all; `frames=0` (2395 B) | neither line; the historic one-liner BYTE-IDENTICAL to a binary built from master |

The witnesses are unchanged from the earlier run, so the merge did not move the
behaviour — which is the thing a re-measurement is for.
