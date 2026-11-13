//go:build linux

package lineedit

import "golang.org/x/sys/unix"

// termiosReadRequest/termiosWriteRequest are the Linux termios ioctl requests
// (the POSIX TCGETS/TCSETS spellings; the BSD TIOCGETA/TIOCSETA pair does not
// exist here). Referencing the named request constants — never a bare number —
// keeps one source of truth for the ioctl pair.
const (
	termiosReadRequest  = unix.TCGETS
	termiosWriteRequest = unix.TCSETS
)

// readTermios snapshots the FULL terminal attribute set for fd.
func readTermios(fd int) (termiosState, error) {
	t, err := unix.IoctlGetTermios(fd, termiosReadRequest)
	if err != nil {
		return termiosState{}, err
	}
	return termiosState{raw: *t}, nil
}

// writeTermios restores a full terminal attribute snapshot onto fd.
func writeTermios(fd int, st termiosState) error {
	t := st.raw
	return unix.IoctlSetTermios(fd, termiosWriteRequest, &t)
}

// termiosState carries the FULL termios attribute set — every field of the
// struct, not a bit mask of the flags we happened to think about. Raw mode
// clears IXON/ICRNL/BRKINT/OPOST/IEXTEN and zeroes VMIN/VTIME; hand-OR-ing
// ECHO|ICANON|ISIG back cannot reconstruct any of those fields (hermes
// cli.py:938-951 makes the same argument), so the snapshot is the only
// restorable form.
type termiosState struct {
	raw unix.Termios
}
