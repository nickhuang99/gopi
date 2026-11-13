//go:build linux

package lineedit

import "golang.org/x/sys/unix"

// PTY master helpers. They live here (not in the tests) because they are the
// same ioctl family as the termios access above: a caller that opens /dev/ptmx
// must resolve the slave's name (TIOCGPTN) and unlock it (TIOCSPTLCK) before the
// slave can be opened. Keeping them beside the termios calls means the ioctl
// knowledge has one home.

// ptyNumberRequest is the ioctl that reports a PTY master's slave number.
const ptyNumberRequest = unix.TIOCGPTN

// ptyUnlockRequest is the ioctl that clears a PTY master's lock flag.
const ptyUnlockRequest = unix.TIOCSPTLCK

// PtyNumber returns the slave number of an opened PTY master (fd is the master).
func PtyNumber(fd int) (int, error) {
	return unix.IoctlGetInt(fd, ptyNumberRequest)
}

// PtyUnlock clears the lock bit that keeps a fresh PTY's slave unopenable.
func PtyUnlock(fd int) error {
	unlock := 0
	return unix.IoctlSetPointerInt(fd, ptyUnlockRequest, unlock)
}
