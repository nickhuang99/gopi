package lineedit

import (
	"encoding/binary"

	"golang.org/x/sys/unix"
)

// winsizeBytes is the wire size of a TIOCGWINSZ reply (4 × uint16).
const winsizeBytes = 8

// winSize is the terminal geometry reported by TIOCGWINSZ.
type winSize struct {
	Rows uint16
	Cols uint16
	X    uint16
	Y    uint16
}

// ParseWinSize decodes a raw TIOCGWINSZ reply (little-endian, 4 × uint16).
// Exported so tests can build a geometry fixture without a real terminal.
func ParseWinSize(b []byte) winSize {
	if len(b) < winsizeBytes {
		return winSize{}
	}
	return winSize{
		Rows: binary.LittleEndian.Uint16(b[0:2]),
		Cols: binary.LittleEndian.Uint16(b[2:4]),
		X:    binary.LittleEndian.Uint16(b[4:6]),
		Y:    binary.LittleEndian.Uint16(b[6:8]),
	}
}

// TerminalCols reports fd's width in columns, read through the same TIOCGWINSZ
// request Terminal.Size makes. It returns 0 when the kernel cannot answer (fd is
// not a terminal, or the host reports no geometry) — 0 is "unknown", never a
// width, so a caller must treat it as "do not wrap" rather than as a sliver.
//
// It is EXPORTED because the width a caller streams to a terminal against must
// be the number this package wraps to (Rule 14): the chat REPL's fallback
// renderer has no editor and no region to ask, and re-deriving the ioctl there
// would be a second copy of this request that drifts the moment either side
// changes.
func TerminalCols(fd int) int {
	ws, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
	if err != nil {
		return 0
	}
	return int(ws.Col)
}

// TerminalRows reports fd's height in rows, read through the SAME TIOCGWINSZ
// request TerminalCols makes. It returns 0 when the kernel cannot answer ("fd is
// not a terminal", or the host reports no geometry) — 0 is "unknown", never a
// height.
//
// It is EXPORTED for the reason TerminalCols is: the chat surface's geometry
// decision (the ideal geometry's degradation notice) has to compare the
// TERMINAL's own numbers against the declared ideal, and a second ioctl written
// at that call site would be a second copy of this request (Rule 14) that drifts
// the moment either side changes. It is deliberately a separate function rather
// than a struct return: a caller that asks for the height is asking a different
// question, and `TerminalCols` staying a one-number answer is what keeps the
// wrap path's call sites readable.
func TerminalRows(fd int) int {
	ws, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
	if err != nil {
		return 0
	}
	return int(ws.Row)
}
