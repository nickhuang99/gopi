package main

// chat_replay_help.go — the DISCOVERY half of `gopi chat --replay`: the worked
// command lines a reader can paste, and what COLSxROWS means in pixels.
//
// WHY IT IS A NAMED PART. `--replay`, `--film`, `--out` and
// --chat-ideal-geometry are four words on one page, and the operator's complaint
// that produced this file was exactly a DISCOVERY failure: the words existed and
// nothing on the page led to them (and `--replay -h` answered a usage error
// instead of the page). A help section is the only path a user has from "I want
// to see that session again" to the command that does it, so the examples are
// commands this build actually runs — each one is exercised in
// chat_replay_geometry_test.go against the same constants the parser reads
// (Rule 1b/13: a page that spells a flag differently from its parser is a page
// advertising a command line the verb refuses).

import (
	"fmt"

	"github.com/nickhuang99/gopi/internal/config"
)

// chatReplayExamples is that section: five real command lines in the order a
// reader walks them, then what the size means in pixels, then the font question
// answered rather than left hanging.
//
// chatBeatUsageSection is joined into the SAME scoped page (chatReplayHelp) and
// states the beat
// protocol's own three words: `--scratch`, `--cols` and `--rows` are accepted by
// this verb's parser (chat_replay_beats.go) and used by the film driver's own
// documented command lines, yet until this section the page never named them — a
// flag the parser accepts and the page never mentions is not discoverable, which
// is the C14 finding scripts/replay-check.sh reports. The words come from their
// constants (Rule 1/13), never retyped.
const chatBeatUsageSection = `
` + chatFlagScratch + ` DIR   the BEAT PROTOCOL's scratch directory: this run reads the beat
             ledger from DIR, acknowledges each beat and writes into DIR — the
             SAME directory name the replay launcher takes, so the driver films
             this surface with the launcher it already has. Pairs with ` + chatFlagReplay + `.
` + chatFlagCols + ` N      / ` + chatFlagRows + ` N
             the geometry the layout is installed at, as two positive integers
             (also ` + config.ChatIdealGeometryEnv + ` or the settings key
             ` + config.ChatIdealGeometrySettingsKey + `). A run REFUSES a terminal that disagrees with them:
             a film drawn at another width is a different picture.
` + chatFlagMaxBeats + ` N
             the most beats this run renders, so a long recording cannot render
             for ever: the film then holds at most this many frames. Omitted, the
             protocol's own default bounds it.
` + chatFlagAckTimeoutMS + ` N
             how long one beat waits for its acknowledgement, in milliseconds;
             omitted, the protocol's own default bounds the wait.
`

var chatReplayExamples = `
Recording, replaying and filming — one worked command line each:

  0. drive it    gopi chat ` + chatFlagReplay + ` <session-id> ` + chatFlagScratch + ` <dir> ` + chatFlagCols + ` 117 ` + chatFlagRows + ` 26
  1. record      gopi chat
  2. find it     gopi session log --sessions
  3. replay      gopi chat --replay <session-id>
  4. film        gopi chat --replay <session-id> ` + chatFlagFilm + ` ` + chatFlagOut + ` /tmp/replay.gif
  5. size it     gopi chat ` + chatGeometryFlag + ` 100` + config.ChatIdealGeometryDimSep + `30
                 (also via ` + config.ChatIdealGeometryEnv + ` or the settings key
                 ` + config.ChatIdealGeometrySettingsKey + `). In a replay an EXPLICIT size wins over
                 the recording's own; each step's contract is in man gopi-chat.

WHAT "COLS×ROWS" MEANS IN PIXELS: a CELL count, not a physical size. One cell is
` + chatCellSizeReference + ` in the film's default terminal, so ` + chatGeometryValue(100, 30) + ` comes
out at ` + chatPixelsFor(100, 30) + ` and the ideal ` + config.ChatIdealGeometryDefault + ` at ` + chatPixelsFor(117, 26) + ` (xterm adds its
own ` + fmt.Sprint(chatRefBorderPx) + ` px border). A terminal with a bigger font gives MORE pixels for the
same cell count, which is why the surface is configured in cells: "the glyphs
are too small" is a question about the TERMINAL the film runs in, not about this
geometry — ` + chatTerminalFlag + ` NAME answers it, and nothing here changes a font.
The cell-by-cell table is in man gopi-chat.

`

// The reference cell size of the film's default terminal, and the derived pixel
// figure for a geometry. They are named constants because the help page, the
// film's own measurements and this file's test all quote the same pair
// (Rule 1b/13) — and because a reader comparing two runs' frame sizes needs the
// px-per-cell figure to be reproducible rather than retyped.
const (
	// chatRefCellW / chatRefCellH are the nominal pixel width and height of ONE
	// character cell in the film's default terminal (xterm with its built-in 6×13
	// fixed face). They are a reference for READING a cell count, not a promise:
	// a frame's real size is measured from the frame itself (film's own checks
	// read the PNG), and a terminal with a different face gives different pixels
	// for the same cells — which is the point the section makes.
	chatRefCellW = 6
	chatRefCellH = 13

	// chatCellSizeReference is how the help page states that measurement.
	chatCellSizeReference = "6×13 px (xterm's built-in fixed face)"

	// chatRefBorderPx is the border xterm draws around its character grid, in
	// pixels: a frame's real width is cols×chatRefCellW + chatRefBorderPx (and
	// the same for height). It is a MEASURED constant — the film's own frames
	// are 604x394 px for a 100x30 terminal — and it exists so the pixel figures
	// the help page quotes can be checked against a frame rather than being a
	// cell-count claim dressed as a size.
	chatRefBorderPx = 4
)

// chatPixelsFor renders COLSxROWS as the pixel size a film frame comes out at,
// composed from the constants above so the three example lines, the default and
// a real frame cannot disagree about the cell size or the border (Rule 13).
func chatPixelsFor(cols, rows int) string {
	return fmt.Sprintf("%dx%dpx", cols*chatRefCellW+chatRefBorderPx, rows*chatRefCellH+chatRefBorderPx)
}
