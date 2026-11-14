<!-- doc-class: historical -->
# The agent's eye: an all-blank chat grid is no longer readable as "the screen is empty"

**Task:** `01a0ddb3-f47a-7803-99b1-31df5c66ad51` (briefing: *the chat dump stops at
`frames=3 dumps=1` before the answer is painted; `ui_state` reports the blank grid
as if it were the screen*).
**Worktree:** `/home/user/workspace/gopi-01a0ddb3f47a780399b131df5c66ad51`, branch
`task/01a0ddb3f47a780399b131df5c66ad51`.
**Evidence:** `/home/user/.gopi/sessions/01a0ddb3-f47a-7803-99b1-31df5c66ad51/evidence/`.

---

## 1. §2.1 — why the sink "stopped" (measured, code path named)

### The measurement

A real 117x26 pty (`pty.openpty` + `TIOCSWINSZ`), `gopi chat --dump-screen`, **no
input at all**, the artifact polled 17 times over 10 s:

```
pty=117x26 exit=0 idle=10.0s
distinct headers over time (17 samples):
  t=…  mtime=1790429874 bytes=5434 #gopi-chat-dump pid=148287 target=self:chat tools=off
      terminal=117x26 model=117x26 grid=117x26 geometry=layout mode=ansi
      frames=3 dumps=1 recorded=2026-09-26T13:37:54Z started=2026-09-26T13:37:53Z
final record: <byte-identical to the line above>
```

One distinct header, one mtime, for ten seconds. The artifact is **frozen at
`frames=3 dumps=1`** — the filed artifact, reproduced. (`evidence/idle/probe-idle.txt`)

**And the frozen snapshot IS the first install.** Polling the sink's own file
every 40 ms from process start (`evidence/seq-1790430509/`: 61 samples, each a
copy of the artifact as it stood):

```
61 distinct samples, every one:  frames=3 dumps=1   (26/26 grid rows all-blank, cursor row=0 col=0)
                                ↑ the FIRST file the sink ever installs is this all-blank body
same pty, 10 s later:            frames=13 dumps=4   (status row painted at row 24)
```

So the header an agent reads at the moment of doubt — `frames=3 dumps=1`,
`cursor row=0 col=0`, every row blank — is **exactly the pre-paint install**,
not a screen that emptied out.

### The mechanism (three named pieces, none of them a broken sink)

1. **`ScreenDumper.Write` counts every render write** — `d.frames++`
   (`internal/uiclient/dump.go:426`, and the count is what the record's `frames=`
   prints via `provenanceLine` → `lastFrameCount()`).
2. **`ScreenDumper.flush` installs the file only when the dump BODY changed, or on
   a forced write** — `if !force && body == d.lastSig { return nil }`
   (`internal/uiclient/dump.go:620`, the `flush` body). `dumps=` is `d.written`,
   i.e. the number of file installs.
3. **The chat layout writes only when the screen CHANGES.** `ChatEditor`'s read
   loop repaints on a keystroke, a resize or a publish (`internal/lineedit/editor.go`,
   `e.repaint()` call sites). An idle surface produces **no further writes at all**,
   so (1) has nothing more to count and (2) has nothing new to install.

So `dumps=1` is **not** "the sink wrote once and stopped": it is *one changed
frame → one file install*, and then genuinely nothing happened on the terminal.
The number that matters for a reader is that `frames=` is a fact about **paint
traffic**, not about the screen's content: measured on the same pty and code,
`frames=3 dumps=1` (idle), `frames=117 dumps=10` (a turn that painted),
`frames=687 dumps=45` (a chat that answered).

### §2.2 — the correct lifetime (design answer)

**The sink's lifetime is right; the READER's conclusion was wrong.** The dump is a
write-on-change, 100 ms-throttled sample (`dumpMinInterval`), and that is the
honest artifact: an idle screen is not a stream. Arming "late enough to be useful"
is not implementable — the surface cannot know when the agent will look — and
making the dump unconditional was excluded by the briefing. What was missing is
the distinction between *"nothing changed"* and *"there is nothing on the screen"*,
and that belongs to the reader (§3).

### The field consequence (a second, sharper defect than the blank grid)

`CompareGridUnusable` refused `frames=0` and a fallback geometry — and admitted a
**`frames=3`, all-blank** grid (geometry `layout`, so neither refusal fired).
The compare then answered from an empty grid:

```
  mismatch grid_row=-1 in_grid=false in_transcript=true frames=3 prefix=0
  reason="the transcript has this line but the grid does not … the paint lost it …"
```

That is a **manufactured paint loss** — a false accusation against the renderer,
produced by the reader's own emptiness. It is the same class of confident-absence
bug as the one filed, one layer deeper.

---

## 2. §2.3 — the decision, and why

**Decision: the blank grid is REPORTED, clearly marked, and the COMPARE REFUSES.**

* `ui_state` (the reader) **prints the grid, with a qualification above it**. It
  does not refuse outright because the grid itself is real evidence (a reader can
  still see the geometry, the provenance and the fact that nothing is painted);
  the briefing forbids "a warning while still showing the blank grid *unmarked*",
  and this is the marked form: the sentence rides **above** the grid, is carried in
  the `--json` twin, and denies the reading by name.
* `--compare` / `--all` **refuse** (exit 1, `refused`), because an answer there is
  a claim about a layer ("the paint lost it") that an empty grid cannot support.
  This *extends* the existing refusal set (`frames=0`, fallback geometry) without
  weakening either — both keep their own sentences and their own tests.

Two predicates, deliberately not one:

| | question | answer |
|---|---|---|
| `Candidate.Unpainted()` | did the observer ever see a frame? | `frames == 0` (unchanged, `598aa37a`) |
| `Candidate.BlankUnpainted()` | does the artifact carry content in any cell? | `ScreenDumpAllBlank(Dump)` — the GRID is asked |

`frames>0` can never substitute for the second: they are different facts, and the
first was exactly the assumption that failed in the field.

---

## 3. What changed (files, one line each)

| file | change |
|---|---|
| `internal/uiclient/dumpread.go` | `BlankUnpaintedNotice` (the sentence) + `Candidate.BlankUnpainted()` + `ScreenDumpAllBlank()` — the grid is the authority |
| `internal/uiclient/compare.go` | `CompareGridUnusable` gains the blank-grid refusal (after `frames=0`, before the geometry check: a blank grid is unreadable at ANY geometry) |
| `cmd/agent/cli_uistate.go` | the reader prints the sentence above the grid (text path) and in `unpainted_notice` (JSON path); help text: the two blank cases and the read-`frames=`-as-paint rule |
| `cmd/agent/chat_dump.go` | `chatDumpBlankGridNotice` + `chatReportDump` prints it when the frozen grid is all-blank (the pty run diagnoses its own artifact) |
| `cmd/agent/cli_chat.go` | `--dump-screen` help: **THE RECIPE** (3 steps, verbatim), `--compare`/`--session` indivisibility, and what the dump is |
| `internal/uiclient/blankunpainted_test.go` | new: the RED proof + regression guards (§6) |
| `internal/lineedit/screendump.go` etc. | **untouched** — no rebuild of the sink, the grid, the SGR model or the reader |

**No shell wrapper, no second dump path, no new pty driver, no new tool** — the
harness reuses the tree's own pty recipe and the sink's own writer.

---

## 4. The exact commands (now IN the help text)

`gopi chat -h`, under `--dump-screen` (verified verbatim in §6):

```
THE RECIPE (in this order; each step is needed):
  (1) in ONE real terminal start:  gopi chat --dump-screen
  (2) read it back:               gopi ui_state --target chat
  (3) check a line you know:      gopi ui_state --compare "<line>" --session <session-id> --target chat
--compare and --session are ONE call: --compare without --session errors by
design …
A grid that is blank with frames>0 is reported as such by
"gopi ui_state --target chat" (it prints an ALL-BLANK qualification above the
grid, and a --compare against it is REFUSED) - never as an empty screen.
```

`gopi ui_state -h` carries the same contract as its own section, `AN ALL-BLANK GRID
IS QUALIFIED, NOT ANSWERED`, with both cases (`unpainted` = `frames=0`, `blank` =
`frames>0` and every cell blank) and the exit-code consequence.

---

## 5. Before / after, same input

The input is the briefing's own artifact — its record verbatim (`pid=4122306 …
frames=3 dumps=1 geometry=layout`), an all-blank 117x26 grid, in a private
`GOPI_HOME` so the ordinary scan finds it, with the REAL session transcript of a
real answered run (`evidence/filed/REDGREEN/A2-old-compare.txt` vs `A4-new-compare.txt`).

**BEFORE (the old binary):**

```
#gopi-ui-state surface=chat pid=4122306 … state=exited …            ← exit 0
an interpreted cell grid (not bytes, not pixels): …
the target has exited cleanly: …
#gopi-screen-dump v=1 cols=117 rows=26                              ← the blank grid AS THE SCREEN
cursor row=0 col=0
…
ALL-BLANK sentence present? 0        ← nothing anywhere said it was blank
```

```
#gopi-ui-state-compare … transcript_lines=1 match=false             ← exit 3
  mismatch grid_row=-1 in_grid=false in_transcript=true geometry=layout cols=117 rows=26 frames=3 prefix=0
  reason="the transcript has this line but the grid does not … the paint lost it, re-wrapped it, or it has scrolled out"
```

**AFTER (the new binary, same files):**

```
#gopi-ui-state surface=chat pid=4122306 … state=exited …            ← exit 0
an interpreted cell grid (not bytes, not pixels): …
the target has exited cleanly: …
the dump sink interpreted frames (frames>0) but the grid is ALL-BLANK: this artifact shows
the surface's state before its own painted content appeared, or a painted screen whose
every cell was blank — it is NOT evidence that the surface's screen content is empty.
#gopi-screen-dump v=1 cols=117 rows=26
ALL-BLANK sentence present? 1
```

```
#gopi-ui-state-compare … transcript_lines=1 match=false             ← exit 1 (COULD NOT RUN)
  refused grid_row=-1 in_grid=false in_transcript=false geometry=layout cols=117 rows=26 frames=3 prefix=0
  reason="the dump sink interpreted frames (frames>0) but the grid is ALL-BLANK: … NOT evidence
          that the surface's screen content is empty."
```

**A painted screen is untouched** — same dump, same session, same line, old and
new binary: `match grid_row=16 in_grid=true in_transcript=true`, exit 0 both, and
**no** blank qualification either side
(`evidence/filed/REDGREEN/B2-new-compare.txt`, `B3-old-compare.txt`).

### The ladder, on the LIVE pty of this session (not only the filed record)

The same staged files read by both binaries — OLD = pre-change, NEW = committed
`300defd0` (`evidence/ladder.txt`, `/tmp`-free, staged under `evidence/ladder/`):

```
A   the FILED artifact                    frames=3 dumps=1   26/26 rows blank
B1  LIVE pty this session, FIRST install  frames=3 dumps=1   26/26 rows blank
B2  the SAME live pty ~10 s later         frames=13 dumps=4  24/26 rows blank (status row painted)

              ui_state                                 --compare "hello world"
A  OLD  exit=0  ALL-BLANK=no   notice unset          exit=3  mismatch … "the paint lost it"
   NEW  exit=0  ALL-BLANK=YES  notice set            exit=1  refused …
B1 OLD  exit=0  ALL-BLANK=no   notice unset          exit=3  mismatch … "the paint lost it"
   NEW  exit=0  ALL-BLANK=YES  notice set            exit=1  refused …
B2 OLD  exit=0  ALL-BLANK=no   notice unset          exit=3  mismatch
   NEW  exit=0  ALL-BLANK=no   notice unset          exit=3  mismatch   ← identical: no false refusal
```

**B1 is the decisive row**: a REAL pty run from this session produced the filed
header verbatim (`frames=3 dumps=1`, all-blank, `cursor row=0 col=0`) as its
**first** sink install — so the filed artifact is not a one-off; and B2 is the
same run ten seconds later, where the new reader must stay silent (it does).

---

## 6. Verification

### Build / vet / tests

```
go build ./...                                  → OK
go vet ./internal/uiclient/ ./cmd/agent/        → clean
go test ./internal/uiclient/ ./cmd/agent/ ./internal/lineedit/ -count=1
                                                → ok  (uiclient 0.27s, cmd/agent 6.8s, lineedit …)
```

### The RED proof (the change reverted, the new tests fail)

`Candidate.BlankUnpainted()` reverted to the pre-fix rule (the frame count alone),
in one file, then:

```
--- FAIL: TestBlankGridWithFramesIsNotAScreen
    an all-blank 117x26 grid with frames=3 must read as blank-unpainted — this is the measured
    chat artifact, and reading it as a screen is the filed defect
--- FAIL: TestCompareRefusesABlankGridInsteadOfAnswering
    status = "mismatch", want "refused" — answering a blank grid reports the reader's own
    emptiness as a paint loss
--- FAIL: TestBlankGridIsNotReadableAsARealEmptyScreen
    frames=3 blank=false — the measured idle artifact must take the reader's blank-with-frames branch
```

With the refusal removed from `CompareGridUnusable` — the same two tests fail on
`status = "mismatch"`. Restored → `ok github.com/nick/gopi/internal/uiclient`.
The `frames=0` guard (`TestUnpaintedDumpIsNotAnEmptyScreen`) and the geometry
refusals pass in every state, so the new case cannot be bought by weakening an old
one.

### The real pty runs (§4 of the evidence dir)

```
probe.py      (input + stub model)  → frames=117 dumps=10, answer_in_dump=True, answer_in_pty_raw=True
probe_idle.py (no input)            → frames=3 dumps=1, byte-identical for 10s (17 samples)
redgreen3.sh  (filed artifact)      → before: exit 0 + blank grid as the screen, exit 3 mismatch
                                      after:  all-blank sentence above the grid, exit 1 refused
redgreen5.sh  (painted artifact)    → verdicts IDENTICAL old vs new, no blank qualification
```

The stub is a local SSE server (`ANTHROPIC_BASE_URL`), so no API spend.

---

## 7. Not done / honest limits

1. **The filed `frames=3 dumps=1` artifact is not regenerable on demand.** Its
   exact numbers are a timing artifact of that box (my own idle run this session
   measured `frames=3 dumps=1`, `frames=4 dumps=2` and `frames=6 dumps=4` — same
   freeze, small-count noise). The RED proof therefore uses the briefing's record
   **verbatim** as the input artifact (script `filed_artifact.py`), not a
   re-timing of it.
2. **The idle chat's grid is not always all-blank.** Measured: after a few seconds
   the editing line (`you>`) and the status row land on a couple of rows, and then
   the artifact freezes with 2 painted rows. The blank case is the EARLY freeze
   (the filed one); both are correct reads under the new rule, and only the fully
   blank one takes the new branch.
3. **A `gopi ui` surface whose model painted nothing and whose screen was then
   cleared** is not covered by the new reader rule (its artifact would be blank +
   `frames>0` + possibly a fallback geometry). The briefing scoped this task to the
   chat surface; the same `BlankUnpainted()` predicate applies to any candidate, so
   extending it is a one-line change if `gopi ui` needs it.
4. **`internal/standards/` was not re-run to completion** in this window (the
   package takes >2 min of build+run and the session budget held). The gates that
   matter here were run instead: `go vet`, `gofmt`, and the three touched packages'
   suites.
5. **The chat's own stderr sentence (`chatDumpBlankGridNotice`) was not observed
   firing on a live blank run.** In the idle close-out run I captured, the grid had
   the status row painted by the time `Close()` froze it (`frames=13`), so the
   sentence was not owed and did not print — evidence that the guard does not fire
   when it should not, but not a positive sighting. The positive path is covered by
   the `uiclient` tests (the same predicate and the same notice text).
