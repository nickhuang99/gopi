<!-- doc-class: historical -->
# Task Report: the chat screen dump lost the startup screen — a DCS string sequence was parsed as text

Task `01a0ff97-0caa-7c9e-982e-b5a7015718a3`, branch `task/01a0ff97…`.

## Completed

- **Root cause, measured at every step** (5-why below): `internal/lineedit/screen.go`'s
  escape parser had a STRING-sequence arm for OSC (`ESC ]`) and **none for DCS
  (`ESC P`)**. The chat surface's own capability probe sends `ESC P + q <hex> ESC \`
  (XTGETTCAP) *before* the layout paints; with no DCS arm the `P` was consumed as a
  two-byte escape, the payload painted into the grid as text, and the payload's ST
  (`ESC \`) was read as the start of the next sequence — swallowing the introducer of
  the layout's own `ESC[1;24r`. The scrolling region was therefore never installed,
  and the startup screen was overwritten row-by-row instead of scrolling into history.
- **Fix in the interpreter layer** (`internal/lineedit/screen.go`): one shared
  `stringSeqLen` reader for both string-sequence introducers (OSC and DCS, identical
  framing: payload to ST or BEL, an inner non-ST ESC aborts), plus the DCS arm that
  calls it. `oscLen` became that shared reader rather than a second copy.
- **Minimal regression test** (`internal/lineedit/screen_dcs_string_test.go`, 4 tests)
  written against the minimal byte shapes, using the shipped `sgr.GetCapabilityQuery`
  so it cannot drift from what the probe sends. Red before the fix, green after.
- **Deliverable 5 — the dump carries the startup screen on BOTH paths**, real `gopi
  chat` and `--replay`, on the real profile (see Verification).

### The 5-why, each step with its measurement

1. **Why is the dump's grid blank for rows 0..22?** Because nothing ever painted there
   *in the interpreter's view*: the startup screen was written at row 24, the region's
   bottom, and each band row overwrote the previous one.
   *Measured:* PTY raw carries the screen (`grep -c "gopi v0.1.0"` = 1), the artifact
   does not (`grep -c` on the dump = 0); grid rows 000–022 blank, `scrollback lines=0`.
2. **Why did the band's rows overwrite each other instead of scrolling?** Because no
   scrolling region was installed — `ESC[1;24r` is in the paint stream but
   `scroll_region` in the dumped grid stayed `top=0 bottom=25`.
   *Measured:* same run, `grep -c` of `ESC[1;24r` in the feed = 1; dumped
   `scroll_region top=0 bottom=25`.
3. **Why is no region installed when the sequence is right there?** Because its `ESC`
   was consumed by the preceding sequence. Feeding the 8623 captured bytes and then
   removing exactly the 30 DCS bytes flipped the run: `region=0..25 scrollback=0
   bannerRow=-1` → `region=0..23 scrollback=30` with `╭── gopi v0.1.0 ──…` at
   `scrollback[23]`.
4. **Why does the DCS eat the next `ESC`?** Because `consumeEscape` had no DCS arm: a
   non-`[` byte is consumed as a two-byte escape, so `ESC P` ended there and the
   payload's own `ESC \` (ST) was read as the introducer of the next sequence.
   *Measured minimal:* `"\x1bP+q636F6C6F7273\x1b\\\x1b[1;24r"` → `region 0..25`, grid row
   000 = `q636F6C6F7273[1;24r` (pre-fix).
5. **Why did the parser have an OSC arm and no DCS arm?** Because the OSC arm was
   added from a *different* observed defect (a bubbletea background-colour query, whose
   payload painted `1;?` over the header) — the fix was written for the introducer in
   front of it rather than for the class "string sequence". DCS is the same class;
   nothing in the file generalized it.

**This is why `--no-color` "fixed" it**: `CapabilityForEnv(noColor=true)` skips the
probe, so the DCS bytes never enter the stream. Measured, same binary, same terminal:
`--no-color` → banner in the dump; without it → not.

## Not Done

- **When the startup screen reaches the `scrollback` is still governed by the band's
  own row budget, and that is a LAYOUT question this fix deliberately did not answer.**
  The fix makes the `top == 0` scrollback arm REACHABLE — before it, no region was ever
  installed, so `scrollRows` never saved a row and the screen was destroyed instead of
  filed — and the measured consequence is that the screen's rows now DO reach history
  once later band rows push them past the region's top: in the real-chat artifact the
  banner and both tips rows are in `scrollback` after ~34 band rows. What the fix does
  NOT do is make the startup screen reach history *at the moment it is replaced*: the
  band's live row sits at the region's BOTTOM (`ESC[1;24r`, live row 24), so a
  `writeBandLine` promotion replaces the screen's rows in place and they enter history
  only later. Whether the live row should be the region's TOP (so committed rows scroll
  DOWN into permanent view) is a reserved-layout design decision, and it is the exact
  place the epic forbids me to touch: A5 depends on the current `top == 0` semantics, so
  I left the layout alone and report the residual behaviour instead of guessing.
- The `gopi ui` surface was not re-verified separately; its dump uses the same
  interpreter and the same probe, so it is fixed by the same change, but I did not run
  its own acceptance surface.
- `recorded=true` (the byte-exact "served from the recording" notice a filmed run
  prints) did not appear on this replay run's stderr — that notice is the beat-driven
  path's, and this run was not driven by the beat protocol. The replay was still a real
  replay, on its own stderr evidence: 8 turns / 23 scripted responses, "model base URL
  bound to the in-process stub at http://127.0.0.1:40389 (no provider is contacted)",
  "executor seam installed: 33 recorded tool result(s) will be served verbatim; the real
  dispatch is unreachable for this run", "turn 1/8: typing the recorded prompt". I
  verified those, not the `recorded=true` spelling, so I claim the former and not the
  latter.
- The run's own quit was refused: `injecting "/quit" was refused: this period's key
  ceiling is reached (64 of 64 keys injected)`. The replay therefore ended on my
  `timeout`, so the artifact is a mid-run frame rather than a clean shutdown — which is
  what the acceptance wanted (a live screen), but it is not a completed 8-turn replay.

## Verification

```
Build: CGO_ENABLED=0 go build ./... → OK
       CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ → OK
Tests: red (pre-fix screen.go, new test present):
       go test -count=1 ./internal/lineedit/ -run 'DCS|OSCAndDCS|SplitStringSequence'
       --- FAIL: TestDCSQueryPaintsNoCellAndDoesNotEatTheNextSequence
           screen_dcs_string_test.go:49: a DCS payload painted into the grid — row 000
           reads "q636F6C6F7273P+q524742[1;24r"
       --- FAIL: TestDCSQueryKeepsTheCommittedStartupScreenInScrollback
       --- FAIL: TestOSCAndDCSShareOneStringReader (x2 subtests)
       --- FAIL: TestSplitStringSequenceIsResumedNotPainted
       FAIL github.com/nick/gopi/internal/lineedit 0.793s
       green (fixed):
       --- PASS: TestDCSQueryPaintsNoCellAndDoesNotEatTheNextSequence
       --- PASS: TestDCSQueryKeepsTheCommittedStartupScreenInScrollback
       --- PASS: TestOSCAndDCSShareOneStringReader (4/4 subtests)
       --- PASS: TestSplitStringSequenceIsResumedNotPainted
       ok github.com/nick/gopi/internal/lineedit 0.945s
       branch-scoped suites (the packages this diff touches):
       go test -count=1 ./internal/lineedit/ -skip TestProbeMeasuresDepthFromRealAnswers
         ok github.com/nick/gopi/internal/lineedit 36.757s
       go test -count=1 ./cmd/agent/ -run 'ChatDump|ChatSurface|Dump'
         ok github.com/nick/gopi/cmd/agent 9.300s
       go test -count=1 ./internal/sgr/ ./internal/uiclient/
         ok github.com/nick/gopi/internal/sgr 0.006s
         ok github.com/nick/gopi/internal/uiclient 0.698s
       the epic's own guard (lineedit A5 + scrollback assertions):
       go test -count=1 ./internal/lineedit/ -run 'Scrollback|A5|Scroll'
         ok github.com/nick/gopi/internal/lineedit 0.942s
```

### Ran: the real `gopi chat` path

```
$ script -qec "stty rows 26 cols 117; ./bin/gopi chat --tools --dump-screen=<out>/live-chat.dump" /dev/null </dev/null
# (timeout 25s; exit 124 = the interactive REPL waiting, as expected)
```

Result: the artifact now carries the startup screen. Verbatim from `live-chat.dump`:

```
scroll_region top=0 bottom=23
lines_begin count=26
000|│              ▐███   ▄▄  ███▌              │ Up / Down recall earlier messages
002|│                ▜████████▛                 │ ────────────────────────…
004|│                                           │ This session
005|│    interactive chat, in this terminal     │ session  c5a19d5a-34b2-46db-bab7-b1958d552063
009|│ …ce/gopi-01a0ff970caa7c9e982eb5a7015718a3 │ version  v0.1.0
011|╰───────────────────────────────────────…
```

and, in the same artifact's scrollback (the rows the band has already pushed past the
top of the region):

Exactly which section carries what (measured by section, not by grep over the file):

```
live-chat.dump   grid  rows 000–001  : the logo box (▐███ / ▜████████▛, "Up / Down recall…")
                 scrollback[23..]    : "╭── gopi v0.1.0 ──…", "Tips for getting started",
                                       "Ctrl+C interrupts the running turn"
replay.dump      scrollback[23..31]  : the whole screen — logo, "Tips for getting started",
                                       "Ctrl+C interrupts the running turn", "This session"
```


```
023|╭── gopi v0.1.0 ───────────────────────────
024|│                                           │ Tips for getting started
026|│                                           │ Ctrl+C interrupts the running turn
029|│              ▐███▛▘  ▝▜███▌               │ /tools flips tools on and off
```

### Ran: the `--replay` path, on the real profile

```
$ script -qec "stty rows 26 cols 117; ./bin/gopi chat --tools --replay 6a00c4c8-ee48-4c19-8782-63c087500d04 --dump-screen=<out>/replay.dump" /dev/null </dev/null
# (timeout 40s; exit 124)
# stderr (verbatim, tail):
#   gopi replay] re-running session 6a00c4c8-ee48-4c19-8782-63c087500d04 from
#     /home/user/.gopi/sessions/.../session.jsonl (8 turn(s), 23 scripted response(s), 33 recorded...
#   gopi replay] model base URL bound to the in-process stub at http://127.0.0.1:40389
#     (no provider is contacted; the override is restored when this run ends)
#   gopi replay] the recording contains tool calls - the tool-enabled turn runner is on for
#     this re-run; every call is answered from the recording and never dispatched
#   gopi replay] executor seam installed: 33 recorded tool result(s) will be served verbatim;
#     the real dispatch is unreachable for this run
#   gopi replay] turn 1/8: typing the recorded prompt
#   gopi replay]  injecting "/quit" was refused: this period's key ceiling is reached (64 of 64)
```

Result: `scroll_region top=0 bottom=15`, 272 frames; the dumped artifact carries the
logo and the tips in the grid and in the scrollback:

```
001|│ …ce/gopi-01a0ff970caa7c9e982eb5a7015718a3 │ version  v0.1.0
023|╭── gopi v0.1.0 ─────────────────────────────
024|│                                           │ Tips for getting started
026|│                                           │ Ctrl+C interrupts the running turn
029|│              ▐███▛▘  ▝▜███▌               │ /tools flips tools on and off
030|│              ▐███   ▄▄  ███▌              │ a pasted block stays ONE message
034|│                                           │ This session
```

## Evidence (self-filed)

- `--no-color` / colour contrast (same binary, same terminal, `script -qec` at 117x26):
  `banner_in_dump` 1 with `--no-color` vs 0 without; `scroll_region top=0 bottom=23` vs
  `top=0 bottom=25`.
- The 30-byte causal isolation: 8623 captured sink bytes; remove the two XTGETTCAP DCS
  sequences (`re.sub(rb'\x1bP.*?\x1b\\', b'', b)`) → 8593 bytes; same interpreter:
  `region=0..25 scrollback=0 bannerRow=-1` before, `region=0..23 scrollback=30` with
  `SCROLL[23]|╭── gopi v0.1.0 ──` after.
- Minimal parser probe (pre-fix): `"\x1bP+q636F6C6F7273\x1b\\\x1b[1;24r"` →
  `region 0..25`, grid row 000 `q636F6C6F7273[1;24r`; with `\x1b[?2004h\x1b[?u` prefixed
  → row 000 `q636F6C6F7273P+q5247…`. Post-fix: `region 0..23`, payload paints nothing.
- The feed capture and the red/green outputs are kept under the task's own session
  directory (`…/evidence/`): `m5/feed.bin` (8623 bytes the sink was fed),
  `m5/feed-nodcs.bin`, `red/red-output.txt`, `red/green-output.txt`,
  `fixed/live-chat.dump`, `fixed/replay.dump`.
- **`TestProbeMeasuresDepthFromRealAnswers` is a PRE-EXISTING flake, not this
  change's.** It asserts `RGB = true` from `"\x1bP1+r5247423D31\x1b\\"` and reads
  `rgb=unknown bytes=67`; its harness answers the probe from a goroutine reading the
  pty master, so a reply can miss the probe's read window. Measured in all four
  combinations: post-fix, whole package → PASS then FAIL on a repeat; pre-fix
  (HEAD~2's `screen.go`), whole package → FAIL, and single-test with `-count=1` →
  PASS then FAIL. It also never reaches the `Screen` parser this change touches (it
  goes through `Terminal.ProbeCapability`), and the chat surface's dump cannot depend
  on it — the same suite is green on a fresh process and the DCS defect reproduces
  with the probe answering nothing. So this change neither causes it nor is gated by
  it, and I did not paper over it with a skip in the committed test set.
- `git status --porcelain` in the worktree: empty (committed).
