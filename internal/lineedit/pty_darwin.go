//go:build darwin

package lineedit

import "fmt"

// PtyNumber / PtyUnlock have NO working implementation on darwin.
//
// Linux resolves a pty master's slave through two ioctls — TIOCGPTN (which
// REPORTS the number) and TIOCSPTLCK (which CLEARS the lock) — and
// internal/lineedit/pty_linux.go is built on exactly those. Darwin's pty
// interface is a different mechanism altogether: the slave name is obtained with
// TIOCPTYGNAME, and the lock is cleared by grantpt(3)+unlockpt(3), which are libc
// calls. gopi is built with CGO_ENABLED=0 on every target (Rule 12's declared
// build), so those libc entry points are not reachable from this binary at all.
//
// So these are NAMED REFUSALS rather than silent no-ops — the discipline
// internal/sigsource/install_other.go already established. A caller that asked
// for a pty on darwin learns WHY it cannot have one; it does not get a
// zero-valued descriptor that fails later in a confusing place.
//
// WHAT DEPENDS ON THIS: internal/shot's text capture opens a pty to film a
// terminal on the text surface. On darwin that surface therefore reports
// "pty unavailable on this host" and the rest of `gopi tool shot` (window,
// screen, rect capture — pure X11 protocol) is unaffected.

// PtyNumber reports that a pty master's slave number cannot be resolved here.
func PtyNumber(fd int) (int, error) {
	return 0, fmt.Errorf("lineedit: resolving a pty slave number needs TIOCGPTN (linux) — darwin uses TIOCPTYGNAME plus grantpt/unlockpt, and this binary is built with CGO_ENABLED=0 so those libc calls are unavailable (fd %d)", fd)
}

// PtyUnlock reports that the pty lock cannot be cleared here.
func PtyUnlock(fd int) error {
	return fmt.Errorf("lineedit: clearing a pty lock needs TIOCSPTLCK (linux) — darwin uses unlockpt(3), and this binary is built with CGO_ENABLED=0 so that libc call is unavailable (fd %d)", fd)
}
