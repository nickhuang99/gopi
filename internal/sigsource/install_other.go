//go:build !linux

package sigsource

import (
	"fmt"
	"os"
	"runtime"
)

// This file is the NON-LINUX degradation, and it is a deliberate door rather
// than a leftover: siginfo_t is a Linux/Posix shape whose sender fields are read
// here through rt_sigaction(2), which does not exist on darwin or windows. The
// recorder that consumes this package (internal/signalrec) keeps its existing
// signal.Notify path unchanged on those hosts, and the record it writes simply
// carries no si_code fields.
//
// Rule 15 in the small: the absence is NAMED (UnsupportedReason), never silent.
// A reader on darwin asking "was this kill from a process or the kernel?" gets
// "not captured on this host", not an empty string that could be mistaken for
// "no sender".

// Delivered always reports "not captured" on this target.
func Delivered(sig os.Signal) Source { return Source{} }

// Arm is a no-op here.
func Arm(sigs []os.Signal) error { return nil }

// Release is a no-op here.
func Release(sigs []os.Signal) {}

// UnsupportedReason names the degradation for a caller that reports it.
func UnsupportedReason() string {
	return fmt.Sprintf("sigsource: siginfo capture is Linux-only (this is %s/%s) — si_code fields are absent from the record", runtime.GOOS, runtime.GOARCH)
}
