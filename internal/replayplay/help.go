package replayplay

// help.go — THE KEY MAP, in the words the help page, the `?` overlay and the
// verb's usage text all quote.
//
// It lives in this package because the bindings do: a key list maintained beside
// the verb would be a second copy of the key set, and the copy is what drifts
// when a binding is added (Rule 13). HelpLines is rendered by the overlay;
// KeyMapRows is the same list as plain "key — action" pairs, so the verb's -h
// page and the overlay cannot disagree about which keys exist.

import "strings"

// KeyMapRows is the key map as table rows: entry[0] is the key(s), entry[1] what
// they do. The verb's usage text is built from THIS slice (via KeyMapText), so a
// binding added here reaches the help page by construction.
var KeyMapRows = [][2]string{
	{"space", "pause / resume"},
	{"n or →", "advance one TURN, from the top"},
	{"p or ←", "go back one TURN, from the top"},
	{"↑ / ↓", "scroll a row up / down inside the turn"},
	{"PgUp / PgDn", "scroll a page up / down inside the turn"},
	{"Home / End", "first turn / LAST turn's BOTTOM"},
	{"G or g", "the very end: last turn, bottom row"},
	{"+ / -", "faster / slower"},
	{"?", "this help overlay (any key returns)"},
	{"q or ESC", "quit and restore the terminal"},
}

// keyColumnWidth is the width the overlay pads the key column to, so the actions
// line up (and the column is stable whatever a binding is called).
const keyColumnWidth = 12

// HelpLines renders the overlay: a title, the key map, and the two facts a
// viewer asks about a replay (what the material is, and that colour is the
// recording's when the recording had one).
//
// A line prefixed with helpAccentPrefix is drawn in the accent colour by the
// renderer: the STYLING and the TEXT are one value here, so the overlay cannot
// lose its emphasis while keeping the words.
func HelpLines() []string {
	lines := []string{
		helpAccentPrefix + PlaybackCommand + " — playback",
		"",
	}
	for _, row := range KeyMapRows {
		pad := keyColumnWidth - lineeditWidth(row[0])
		if pad < 1 {
			pad = 1
		}
		lines = append(lines, helpAccentPrefix+row[0]+strings.Repeat(" ", pad)+row[1])
	}
	lines = append(lines,
		"",
		"one step = one TURN — one operator row to the next, with every request",
		"and tool call the harness ran in between gathered INSIDE it. A turn's",
		"request, its reasoning, its prose and its tool calls are all inside that",
		"one step; the ↑/↓ arrows read the rest of a long one a row at a time, and",
		"PgUp/PgDn a page at a time. A step starts the turn it lands on at its TOP,",
		"so ←/→ are the movement BETWEEN turns and ↑/↓ the movement WITHIN one.",
		"END is the exception, and it is deliberate: a viewer reaching for the end",
		"key wants the LAST ROW of the material, not the top of its last turn — so",
		"End lands on the last turn's BOTTOM, and G says the same from anywhere",
		"(one key for 'the very end', so a viewer who never finds End still has a",
		"spelling). Home stays the top of the first turn: the two ends of the",
		"material are the two ends of the material.",
		"a ROUND (one HTTP request/response) is the transport's own unit and is not",
		"what this player steps through. `--rounds`/`--round N` list the transport",
		"the framework's own [system] rows are dim: they are not your words.",
		"",
		"material: the session's own "+SourceScreenDump+" (the grid's per-cell",
		"truecolour is copied verbatim) and its "+SourceSessionLog+" timeline.",
		"",
		"the terminal is restored on quit, on ^C, and on SIGTERM.",
	)
	return lines
}

// KeyMapText renders the same rows as the plain-text block a usage page carries.
func KeyMapText() string {
	var b strings.Builder
	for _, row := range KeyMapRows {
		pad := keyColumnWidth - lineeditWidth(row[0])
		if pad < 1 {
			pad = 1
		}
		b.WriteString("  " + row[0] + strings.Repeat(" ", pad) + row[1] + "\n")
	}
	return b.String()
}

// lineeditWidth is the display width used to pad the key column: go-runewidth's
// answer through the material's own helper, so a CJK key name (none today, but
// the key map is data) would pad correctly.
func lineeditWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeCells(r)
	}
	return w
}
