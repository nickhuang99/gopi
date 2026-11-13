//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package lineedit

import "golang.org/x/sys/unix"

// termiosReadRequest/termiosWriteRequest are the BSD termios ioctl requests.
const (
	termiosReadRequest  = unix.TIOCGETA
	termiosWriteRequest = unix.TIOCSETA
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
// struct, not a bit mask of the flags we happened to think about.
type termiosState struct {
	raw unix.Termios
}
