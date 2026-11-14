<!-- doc-class: historical -->
# The downgrade notice is one sentence per RUN, not one per resolution

Task `01a10e2b7a467b5ba014924dff61774a`, 2026-10-04. The epic's symptom was an
end-to-end test red on the master baseline:
`testsuite/chat_palette_e2e_test.go`'s
`TestChatDowngradeNoticeReachesStderrOnAPTY` counts the palette downgrade notice
in a child's stream and demands **exactly one** (`byMark == 1 && byKnob == 1`).
Measured on master: **3 of each**.

## What the run actually prints (the reproduction)

```
go test ./testsuite/ -run TestChatDowngradeNoticeReachesStderrOnAPTY -count=1 -v
```

```
chat_palette_e2e_test.go:329: the downgrade notice appears 3 times
  (mark "not drawable here") / 3 times (the COLORTERM knob),
  want exactly 1 of each (one capability decision per run)
--- FAIL: TestChatDowngradeNoticeReachesStderrOnAPTY (67.63s)
FAIL	github.com/nick/gopi/testsuite	69.887s
```

The three occurrences, read off the QUOTED stream (offsets into it, `\x1b` shown
as `<ESC>`), each a full copy of the same sentence:

```
[gopi] palette "hermes" needs 24-bit colour and is not drawable here
       (TERM="256color" COLORTERM="") — drawing "palette256";
       set COLORTERM=truecolor for 24-bit colour
```

| # | preceded by | what that pins |
|---|---|---|
| 1 | `…this session writes in <dir> — no git checkout here…` | the BANNER stage (`runChatWithInput`), before the input layer exists |
| 2 | `gopi chat: screen dump → …/chat-screen.dump` | the editor path's DECLARATION stage (`newChatInputWithPalette`, `PaletteForCapability(declared)`) |
| 3 | `<ESC>[?2004h<ESC>[?u<ESC>[6n<ESC>[c<ESC>]11;?<ESC>\<ESC>P+q636F6C6F7273<ESC>\…` | the terminal's ANSWERS arriving, then `PaletteForCapability(measured)` |

Occurrence 2's wording is the tell: it names `TERM/"256color" COLORTERM=""` —
the environment — while the run's own screen (the same capture, further down)
draws `38;5;230`, i.e. the **256 table**. The sentence about "hermes needs
24-bit" was TRUE of the declaration and is a **lie about the run** once the
terminal has answered.

**Independent confirmation of the count, off the shipped binary.** The same
three sites were reproduced on the FIXED tree with the source reverted (a
`captureStderr`-counted ptrace of the child is not needed: the testsuite case
prints the whole stream). And the cheap seam-level repro —
`cmd/agent/palette_notice_once_pty_test.go`, which drives the one production
call that FINISHES the decision — reports `2` there, because it starts at
stage 2 and never runs the banner: the arithmetic of "banner + declaration +
measurement = 3" and "declaration + measurement = 2" is the same defect seen
from two entry points.

## Root cause — measured, not a race

**One decision, resolved three times, each resolution announcing by itself.**

`PaletteForCapability` (`internal/lineedit/colour.go`) both mapped a capability
to a table **and wrote the notice** — `announcePaletteDowngrade` fired from
inside it. The console, meanwhile, does not decide its colours in one step:

1. `runChatWithInput` (`cmd/agent/cli_chat.go`) resolves the environment's
   declaration for the banner → `PaletteForCapability(chatCap)` — **print 1**.
2. `newChatInputWithPalette` (`cmd/agent/lineedit_wire.go`) resolves the same
   declaration again, for the table the surfaces draw with until a terminal
   answers → `PaletteForCapability(declared)` — **print 2**.
3. The same function opens the terminal, asks it
   (`lineedit.CapabilityForOpenTerminal`), and re-derives the table from the
   answer → `PaletteForCapability(measured)` — **print 3**, and this is the only
   one whose table the operator ever sees.

That is the whole of it: **three resolutions, one decision**. Two independent
mechanisms make it happen and both are deliberate —
(a) the staged resolver (measurement first, declaration as the fallback, so the
door opens with a table already in hand), and (b) the duplicated declaration
(the banner and the input layer each resolve the environment before the terminal
is consulted).

**It is not a startup race.** All three prints are on a single goroutine, in
program order, in every run of the failing case; the `-count=1` failure and the
three occurrences are deterministic (reproduced on the baseline, on the fixed
tree under a reverted source, and on master as the epic reported). There is
also no concurrency between them: nothing here is decided on a second goroutine.

**The notice was RIGHT twice and about the wrong thing.** Two of the three
described `hermes`, while the run drew `palette256`. So the defect is not
"three copies of a true sentence" — it is **two false sentences plus one true
one**, which is why the fix is not a dedupe.

## The fix — the announcement moves to the run's FINAL decision

The mapping is split into a pure half and a speaking half
(`internal/lineedit/colour.go`):

- `PaletteForCapabilityNoticed(c) (Palette, string)` — the table, and the ONE
  sentence the operator is owed; **writes nothing**.
- `PaletteForCapability(c) Palette` — the same mapping, then writes whatever
  survived (kept for the many callers that resolve ONCE per producer, where the
  decision and the sentence are the same event).
- `PaletteDowngradeNotice(c) string` — the pure query, for a caller that must
  HOLD a stage's sentence without drawing anything.

`announcePaletteDowngrade` (which both decided and spoke through `os.Stderr`) is
replaced by `paletteDowngradeNotice`, which **returns** the sentence. One
wording, one place (Rule 13).

With that division the console holds each stage's sentence instead of printing
it, and says **exactly one, at the point the decision finishes** — after the
terminal has answered, before the first paint:

- `cli_chat.go` holds the banner resolution's sentence and does not say it while
  an input layer will still finish the decision.
- `lineedit_wire.go` holds the declaration's sentence; when the measurement
  overrules the table **the held sentence is DISCARDED** (the table it describes
  is about to stop being drawn) and the measurement's own sentence takes its
  place; the survivor is written once, on the diagnostic stream.
- The **cooked** path (no editor, no later stage — the resolution IS the
  decision) and the **one-shot** face (`--once`, which supplies its own input
  layer) say the sentence where the decision is made.

**Rule 15 is not weakened, and that is the point of the split.** The notices that
must be heard are still heard, once each:

- a downgrade the run actually made → one sentence, naming the table asked for,
  the depth it needs, TERM/COLORTERM as observed, the table drawn, and the exact
  knob+value that would enable the request;
- a measured/declared disagreement (`lineedit.CapabilityNotice`) → unchanged,
  its own sentence, its own cause;
- an unknown `GOPI_PALETTE` → unchanged, its own sentence (`PaletteSelection`);
- a run that asked for nothing, or got what it asked for → still silent.

What is removed is a sentence about a table that was never drawn — a *fact*
that had no business being on the terminal, not a diagnostic.

## Regression guards

Two, at two levels, and the second is the cheap one that runs on every change:

| guard | what it pins | red proof |
|---|---|---|
| `cmd/agent/palette_notice_once_pty_test.go` — `TestChatStagedCapabilityDecisionSaysItsNoticeOnce` | drives the ONE production call that finishes the decision (`newChatInputWithPalette`) on a real pty with the failing environment; asserts `byMark == byKnob == 1`, and that the survivor names hermes AND palette256 | on the pre-fix source: `the downgrade notice appears 2 times … / 2 times …, want exactly 1 of each` — measured |
| `internal/lineedit/palette_notice_once_test.go` — `TestPaletteForCapabilityNoticedHoldsTheSentenceAndWritesNothing`, `TestPaletteDowngradeNoticeNeverWrites` | the MECHANISM: the noticed form writes zero bytes while returning a non-empty sentence; the announcing form says the SAME sentence (one wording, one line); the pure query writes nothing and owes nothing to a satisfied or unasked-for request | fails at once if a resolution starts speaking from inside again |

The second test in `cmd/agent` (`TestChatStagedDecisionHonouringTheRequestSaysNothing`)
guards the tempting wrong fix — "print from the FIRST stage": with an UNSET knob
the run must be silent, so a fix that always speaks from the declaration also
fails.

The end-to-end case itself is unchanged: it is the acceptance, and it was
correct all along.

## Files

- `internal/lineedit/colour.go` — the mapping split; `paletteDowngradeNotice`
  returns instead of writing
- `cmd/agent/lineedit_wire.go` — hold the declaration's sentence, replace it on
  a measurement, say one; `writePaletteNotice`
- `cmd/agent/cli_chat.go` — do not speak for a stage a later stage may overrule;
  speak on the paths where that stage IS the decision
- `cmd/agent/palette_notice_once_pty_test.go`, `internal/lineedit/palette_notice_once_test.go` — the guards

## What was NOT done

- The **three resolutions are still three resolutions**. They are deliberate
  (the door opens with a table; the terminal gets asked; the banner has a table
  before the editor exists), and making them one would be a redesign of the
  startup order rather than a fix for this defect. What changed is that only the
  LAST one speaks — which is the property the operator and the test care about.
- `gopi ui` (`cmd/agent/cli_ui.go`) keeps `PaletteForEnvCapability`, the
  announcing form: it resolves ONCE, so its notice was never duplicated.
- The banner's `chatPal` is still reassigned from `input.Palette()` after the
  input layer is built (one table for the whole run); the held sentence travels
  with the capability, not with the table, so the two cannot disagree.
