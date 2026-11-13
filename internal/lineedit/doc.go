// Package lineedit is a zero-dependency raw-mode line editor for the gopi
// CLI, plus the terminal plumbing it stands on.
//
// Why a hand-rolled editor (roadmap §2.3/§2.4): the interactive paths used to
// read cooked lines with bufio.Reader.ReadString('\n'), which delegates every
// editing key to the terminal — Up wrote a literal escape sequence into the
// buffer, there was no cursor model, and (the defect that motivated the work)
// each newline of a pasted multi-line block ended a read, so a 10-line paste
// became 10 model turns. golang.org/x/term is NOT available on this host
// (offline, not in the module cache), so raw mode is entered with the x/sys/unix
// termios ioctls — already a direct dependency — and every escape sequence is
// parsed here.
//
// The pieces:
//
//	Terminal   full termios snapshot save/restore, raw mode, SIGWINCH as a
//	           readable event, poll-based (not EINTR-based) wakeups
//	Decoder    bytes to Key events (arrows, word motions, kill/yank, Alt+Enter,
//	           bracketed paste, CSI-u)
//	Value      the editing buffer (rune cursor) plus the paste-burst state
//	History    bounded recall ring, persisted atomically to one file
//	Renderer   in-place redraw of prompt+buffer at the terminal width
//	Screen     a vt100 cell grid used by tests to assert what was DRAWN
//
// None of it has terminal-state side effects on its own: raw mode is owned by
// Terminal (and restored on every exit path), the buffer by Value, and the
// drawing is asserted through Screen — engineering rule 10.2, rendered screen
// content rather than "the callback fired".
package lineedit
