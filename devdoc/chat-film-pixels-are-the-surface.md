<!-- doc-class: normative -->
# Task Report: the filmed chat surface is photographed on its own ground, with no caption band and no driver notices painted into the pixels

Task `01a10343-f5f5-7e4a-9b4a-2d15b0a86056` · branch `task/01a10343f5f57e4a9b4a2d15b0a86056`

## The operator's verdict, restated as the acceptance

`gopi chat --replay <session> --film --out /tmp/replay.gif` was rejected: the GIF
"does not look like the gopi chat surface — wrong size, wrong colours, no info
bar, extra descriptive text". The ticket proposed a mechanism: `--film` renders
TEXT instead of photographing the window. The acceptance is the operator's, and it
is about PIXELS: frames show a recognisable dark gopi chat UI with its info bar,
no log text, no caption pollution.

## Verdict

DOABLE, and delivered. The pixel SOURCE half of the ticket is **factually wrong**
and is corrected below (Rule 15: a wrong premise left standing is a defect).
Three real mechanisms produced the rejected artifact; all three are fixed and each
is held by a test that measures pixels, not prose.

## Completed

### 1. The diagnosis — where `gopi chat` draws, and why the grabbed window was blank

`--film` DOES photograph the real window. `shot.SettledGrab` grabs the filmed
terminal's own X11 window on a private Xvfb display and writes one PNG per beat;
no renderer draws the picture. Measured, both artifacts:

| artifact | canvas | modal colour | declared ground `#131926` | pure white |
|---|---|---|---|---|
| rejected `/tmp/replay.gif` frame 0 | 706x392 | `#FFFFFE` at **90.8%** | **0 of 276752 px = 0.0%** | 90.8% |
| this task's `/tmp/filmfix2/replay.gif` frame 0 | 706x342 | `#131926` at **90.3%** | **217994 of 241452 px = 90.3%** | **0 pixels** |
| this task's film, all 118 frames | 706x342 | `#131926` | **24958899 of 28491336 px = 87.6%** | **0 pixels** |

Commands (in `/tmp/inkprobe`, a scratch probe — Rule 39, never in the tree):

```
go run . gifground /tmp/replay.gif          →  706x392 frames=150 ground 0/276752 = 0.0%  white 0.0%  modal #FFFFFE at 90.8%
go run . gifground /tmp/filmfix2/replay.gif →  706x342 frames=118 ground 217994/241452 = 90.3%  white 0.0%  modal #131926 at 90.3%
go run . identity /tmp/replay.gif /tmp/filmfix-run/frame-0001.png
  → gif frame 0: 706x392   png: 706x342   GEOMETRY DIFFERS
go run . bands /tmp/filmfix2/frame-0118.png
  → top colours: #131926=86.8% #DAA520=9.9% #FFD700=1.7% #FFF8DC=1.6%
```

The per-row bands of the delivered film's last frame (26 rows, 13 px each) show
the surface's own structure: ink 1-24% on every row, **0.0% pure white on every
one of the 26 rows**, and the bottom three rows carrying the status/info bar's
gold.

**The info bar is present on EVERY frame, measured rather than asserted.** The
surface's status line is its last text row (`internal/lineedit/region.go`:
`statusRows = 1`, `StatusRow = rows - statusRows`), i.e. text row 25 (y 328-342 px)
of the 26 the film's geometry declares. Across the 118 photographed frames, the
number of frames whose last text row is BLANK (zero pixels differing from the
declared ground) is **0 of 118** — the info bar is inked in every single frame, and
`frame-0118.png`'s bottom three text rows carry 958/1723/2265 inked pixels with
2161/1695/952 of them gold chrome. The rejected artifact could not show this: its
canvas was white and its bottom 50 px were the appended caption band.

The three causes, each measured rather than argued:

- **(A) The filmed terminal had no ground of its own.** The chat surface's
  palettes are FOREGROUND-ONLY tables (`internal/lineedit` `HermesDarkPalette`
  `#FFF8DC`/`#FFD700`) composed against a dark screen; the filmed xterm kept its
  own white default, so `#FFF8DC` text was drawn at ~1.2:1 on white and the window
  read as a white picture. **This is why the frames were "largely blank white" —
  not because the text never arrived.** The text arrived and was invisible.
- **(B) The animation arm appended a caption band.** `chat_film_gif.go` passed a
  per-frame caption to `internal/gifgen`, which draws a WHITE strip of BLACK
  `internal/text2img` text UNDER every frame — the "extra descriptive text", in a
  second rasteriser the product does not use. That is the whole of the 706x392 vs
  706x342 discrepancy.
- **(C) The driver's own notices were photographed into the picture.** The replay
  driver wrote `[gopi replay] turn 2/4: …` and `[gopi] chat table: …` as BARE
  `os.Stderr` writes with no cursor-positioning sequence; once the reserved chat
  layout paints rows at ABSOLUTE positions, a bare write lands on a row the layout
  owns and the film photographs it. Producer raw stream, byte offsets 114435 and
  115131, each with no preceding `\e[r;cH` — the sentences are readable inside the
  rejected GIF.

### 2. The frames now carry the real surface's pixels

- **(A)** `internal/lineedit` declares the surface's ground ONCE
  (`GroundBackgroundHex` `#131926`, `GroundForegroundHex` `#b7b7b7` — the
  operator's own terminal colours), and `internal/shot`'s `FilmTerminalSpec`
  carries a per-terminal ground switch (`xterm -bg/-fg`,
  `xfce4-terminal --color-bg=/--color-text=`) emitted by `TerminalArgs()` for
  EVERY usable row. A row with no ground switch is **refused by name**
  (`groundArgs`), so a future terminal cannot silently film a white window again.
- **(C)** Every driver notice now goes through ONE sink
  (`cmd/agent/chat_notice_sink.go`): when the beat protocol is installed — i.e.
  this process is a film's producer — the notices are routed to the session's own
  `console.log`, and the agent's logger seam is passed `nil` for a filmed run. The
  words are kept (Rule 15), only their destination moved. A run that is not
  filming is byte-for-byte unchanged.

### 3. The caption no longer pollutes the pixels

`chatFilmComposeGIF(out string, frames []string)` — the `captions` parameter is
**gone from the signature**, not passed empty, so no caller can re-introduce the
band. `gifgen`'s caption machinery is untouched and still serves `gopi gif`.
The ledger's words remain available through an EXISTING, non-polluting channel:
the run copies the producer's own `beats.tsv` into the film directory beside the
frames, and the film's closing notice says which row joins which frame.

### 4. Regression tests — structural pixel evidence, not comments

- `internal/film/ground.go` + `ground_test.go` — criterion 6 (`6. ground`): the
  film's canvas must be ≥50% the declared ground. Its floor is measured BETWEEN
  the two populations (the surface's own frames 85-90%, the rejected white window
  ~0%), and it is proved RED on a white-ground fixture and GREEN on a
  surface-ground one.
- `TestChatFilmGIFPixelsAreThePhotographs` (`cmd/agent/chat_film_gif_test.go`) —
  the animation's canvas is the frame's own geometry and its pixels are the
  photographed PNG's pixels (geometry + pixel identity; `CaptionRows == 0`).
- `TestFilmNoticesAreNotPaintedIntoTheDeliveredFrames`
  (`cmd/agent/chat_notice_sink_test.go`) — measures a REAL film's frames
  (`GOPI_FILM_PIXEL_PROBE`): one canvas, dark-ground share floor, zero pure-white
  pixels. Skips BY NAME with no probe set, so it can never report "the film is
  fine" about a run nobody made.
- `TestFilmGroundProbeOnARealArtifact` — the criterion against a delivered GIF
  (`GOPI_FILM_GROUND_PROBE`/`_EXPECT=pass|fail`); no expectation set = report only.
- `TestFilmGroundMirrorsNoSecondDefinition` — the criterion reads the ONE
  exported ground (Rules 13/14).
- `TestChatNoticeRoute*` / `TestChatNoticesToConsoleLog*` — the notice sink moves
  the notices, restores the previous destination, and REFUSES `nil` by name.

## Not Done (honest)

- **The operator's own eyes are the last acceptance slot and are not filled by
  this run.** The report cannot see the GIF. Everything a machine can measure is
  green; the sentence "yes, that is gopi chat" is the operator's to write.
- One sentence of `[gopi replay]`-flavoured text IS inside the frames and it is
  NOT pollution: `[the recording ends here — gopi replay has no further scripted
  answer for this turn]` is `replaydrive.tailAnswerNotice`, served to the agent as
  the MODEL'S ANSWER TEXT (`closingSteps`), so the surface draws it in the answer
  row exactly as it draws any answer. It is conversation content — the replay's
  script ran out — not a notice painted over the interface. Producer byte offset
  105421 shows it appended to the streaming answer, in the answer row, under the
  layout's own cursor sequence. A recording with an answer for that turn does not
  show it at all.
- Frame geometry is the terminal's own cell grid over `config.ChatIdealGeometry`
  (the film reads it, never a second literal pair — `cmd/agent/chat_film.go`):
  117x26 cells → 706x342 px measured. The cells the surface is designed for match
  the declared geometry exactly; the PIXELS follow the filmed terminal's own font
  metrics, which is why the acceptance's geometry claim is met in CELLS. The GIF's
  canvas is that pixel rectangle and nothing more (`CaptionRows == 0`, asserted).
- Sibling tasks `01a101be` (terminal choice) and `01a10337` (>64 beats deadlock)
  own `internal/shot/terminalregistry*.go` and the beat ceiling respectively; this
  branch touches neither of their behaviours (the registry change here is the
  ground pair, tests updated in place).

## Verification

- Build: `go build ./...` → **exit 0**;
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → **exit 0** (`bin/` is git-ignored).
- Tests: `make test-budget PKGS='./cmd/agent ./internal/film ./internal/lineedit ./internal/replaydrive ./internal/shot'`
  → **exit 0**, stamp `2026-10-04T04:26:47+08:00 … elapsed=649s exit=0 outcome=pass
  stream=run-1791059207-1969847.json`, summary line
  `packages: 5 finished, 0 with a failure` (./cmd/agent, ./internal/agent,
  ./internal/film, ./internal/lineedit, ./internal/replaydrive, ./internal/shot).
  A stream query over that run's own JSON reports `packages done: [agent film
  lineedit replaydrive shot]`, `fails: []`.
  HARDENED GATES: `TestSyncBarePeersTestCoversEveryChangedFile` — the one test that
  enforces "every changed cmd/agent file appears in a coverage list" — passes on
  this branch (`go test ./cmd/agent/ -run TestSyncBarePeersTestCoversEveryChangedFile`
  → `ok … 0.406s`); an earlier draft of this branch failed it, which is why the
  branch's `cmd/agent` files are all in `syncPeersBare*`'s lists.
- `go test ./internal/standards/ -run 'RepoHygiene' -count=1` →
  `ok github.com/the operator/gopi/internal/standards 8.613s` (exit 0) — no blob was
  added by this branch; Rule 39's own evidence here is also
  `git status --porcelain go.mod go.sum` printing nothing, and the worktree clean.
- `../bin/gopi doccheck` → `doccheck: the devdoc corpus is consistent — every file
  under devdoc/ has a well-formed row` (exit 0) — this report's own row is in
  `devdoc/DOC-INDEX.tsv` in the same commit (Rule 36).
- Ran (Rule 31 — the surface itself, verbatim):

```
/tmp/gopi-01a10343c chat --replay a3d34ed1-b998-4caf-9474-9d2b374e1b90 \
    --film /tmp/filmfix2 --out /tmp/filmfix2/replay.gif
```

- Result: 118 beats, 118 frames photographed, 118-frame GIF, 1309008 bytes,
  25cs/frame; closing notice:
  `[gopi film] animation /tmp/filmfix2/replay.gif: 118 frame(s) at 25cs (4.0 frame(s)/s), 1309008 byte(s) — 118 ledger row(s), 118 frame(s) photographed`.
  Measurements after the run (all in `## Evidence (self-filed)`).
- Commit: (see the branch tip; recorded on `task/01a10343f5f57e4a9b4a2d15b0a86056`)

## Evidence (self-filed)

Every figure below was produced by a command this run executed.

1. **Frame identity with the photograph** —
   `go run . identity /tmp/filmfix2/replay.gif /tmp/filmfix2/frame-0001.png`
   → `gif frame 0: 706x342   png: 706x342` / `compared 241452 px, 0 differ`.
2. **Geometry of the rejected artifact** —
   `go run . identity /tmp/replay.gif /tmp/filmfix-run/frame-0001.png`
   → `gif frame 0: 706x392   png: 706x342` / `GEOMETRY DIFFERS`.
3. **Ground criterion on the delivered film (GREEN path)** —
   `GOPI_FILM_GROUND_PROBE=/tmp/filmfix2/replay.gif GOPI_FILM_GROUND_EXPECT=pass go test ./internal/film/ -run TestFilmGroundProbe -v`
   → `6. ground … OK`; `72605 of them carry the surface's declared ground #131926 (90.0%)`;
   `the GIF's frame geometry is 706x342 — the photographed frame's own size, with nothing appended`; `--- PASS`.
4. **Red path (the same criterion on the rejected artifact)** — measured 0.0%
   (0 of 276752 px) on `/tmp/replay.gif`, whose modal colour is `#FFFFFE` at
   90.8%: green on the fixed film (90.3%), red on the rejected one — the two
   populations the criterion's floor was set between. Command line in the table
   above.
5. **Every frame of the delivered film** —
   `GOPI_FILM_PIXEL_PROBE=/tmp/filmfix2 go test ./cmd/agent/ -run TestFilmNoticesAreNotPaintedIntoTheDeliveredFrames -v`
   → `measured 118 frame(s) of 706x342: 24958899/28491336 px on the declared ground #131926 (87.6%), 0 pure-white px` / `--- PASS`.
6. **Notice sink is real** — the producer session's own
   `~/.gopi/sessions/198fdbad-45f9-4bfe-81a6-71616ed4e56b/console.log` holds
   `[gopi replay] turn 2/4: typing the recorded prompt` and
   `[gopi] chat table: chat row ensured: 198fdbad-…`; `grep -o` over the filmed
   terminal's own stream (`/tmp/filmfix2/producer.log`) finds NO `turn 2/4` and NO
   `chat table` — the mid-run leak is gone.
7. **Agent-notice seam** — `cli_chat.go` passes `noticeScreen = nil` when
   `sess.beats != nil`; the run's own logger keeps the record in `console.log`.
8. **Caption band is structurally impossible** — `chatFilmComposeGIF`'s signature
   no longer has a caption parameter, and `res.CaptionRows == 0` is asserted.
9. **`go.mod` / `go.sum` unchanged** — `git status --porcelain go.mod go.sum`
   prints nothing (no new dependency; Rule 35).
10. **Worktree clean (Rule 30)** — `git status --porcelain` printed nothing after
    the commit.
11. **The info bar is inked in every frame** — a scan of all 118 photographed
    frames of `/tmp/filmfix2`, asking only whether the surface's status row (text
    row 25, y 328-342 px of the 26 the geometry declares —
    `internal/lineedit/region.go` `statusRows = 1`) holds ANY pixel differing from
    the declared ground:
    `python3` over `frame-*.png` → `frames 118 status-row-blank 0`. Zero frames
    have a blank status row, i.e. the info bar is present in all of them; the
    rejected artifact's bottom 50 px were the appended caption band instead.
12. **Operator-facing comparison pair** (evidence, not committed — Rule 39):
    `/home/user/.gopi/sessions/01a10343-f5f5-7e4a-9b4a-2d15b0a86056/evidence/`
    holds `replay-rejected.gif` (the artifact the operator rejected, 706x392),
    `replay-fixed.gif` (this run's, 706x342), `rejected-vs-fixed.png` (their frame
    0s stacked, 706x740) and `beats.tsv`.

## Files

- `cmd/agent/chat_film_gif.go` — caption band removed from the animation arm.
- `cmd/agent/chat_film.go` — filmed geometry / closeout notice.
- `cmd/agent/chat_notice_sink.go` (new) + `_test.go` (new) — the ONE destination
  for this run's driver notices, and the frame-level pixel proof.
- `cmd/agent/chat_replay.go`, `cmd/agent/chat_replay_beats.go`,
  `cmd/agent/cli_chat.go` — notices through the sink; the agent logger's screen
  seam denied for a filmed run.
- `internal/film/ground.go` (new) + `ground_test.go` (new),
  `internal/film/checks.go` — criterion 6 and its voice.
- `internal/lineedit/palette.go` — the ONE declaration of the surface's ground.
- `internal/shot/terminalregistry.go` + `_test.go` — the ground pair per terminal,
  refusing a row that has no ground switch.
- `internal/replaydrive/replaydrive.go` — the `Notice` seam for the stub's own
  notices.
- Evidence (NOT committed — Rule 39): `/tmp/filmfix2/` (frames, `beats.tsv`,
  `producer.log`, `replay.gif`), `/tmp/inkprobe/`, and the run's session directory
  `/home/user/.gopi/sessions/01a10343-f5f5-7e4a-9b4a-2d15b0a86056/`.
