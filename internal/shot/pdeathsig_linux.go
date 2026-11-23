//go:build linux

package shot

import "syscall"

// filmChildSysProcAttr is the SysProcAttr every filmed child is started with,
// expressed as a FUNCTION rather than a struct literal at each call site.
//
// WHY THE SPLIT EXISTS: `Pdeathsig` is a Linux-only field on
// syscall.SysProcAttr (checked with `go doc syscall.SysProcAttr` per GOOS, not
// from memory). A literal naming it cannot compile on darwin at all — "unknown
// field Pdeathsig" — so the platform difference has to live in a helper that
// returns the whole struct, not in a value passed to a literal.
//
// Setpgid is on BOTH platforms and is the half that keeps the child out of this
// run's process group, so a signal aimed at the group does not race the kernel's.
func filmChildSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
}
