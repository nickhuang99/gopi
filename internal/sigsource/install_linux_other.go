//go:build linux && !amd64

package sigsource

import (
	"fmt"
	"os"
	"runtime"
)

// This file is the LINUX-WITHOUT-AMD64 degradation: the capture trampoline is
// x86_64 assembly (trampoline_amd64.s) because siginfo_t's field offsets and the
// signal-entry register convention are architecture facts, not portable ones.
// On every other Linux architecture the package still COMPILES and every caller
// still works — Delivered simply reports Valid=false, which internal/signalrec
// renders by omitting the si_code fields, i.e. the exact record shape that
// existed before this epic.
//
// It is NOT an error path: a recorder that refused to run on arm64 would trade a
// missing observation for a missing record, and the record is the older, more
// important artifact.

// Delivered always reports "not captured" on this target. The reason is carried
// by UnsupportedReason so a diagnostic can say WHICH degradation is in force
// instead of implying a kernel refusal.
func Delivered(sig os.Signal) Source { return Source{} }

// Arm is a no-op here: nothing is installed, so nothing can be captured and no
// disposition is touched.
func Arm(sigs []os.Signal) error { return nil }

// Release is a no-op here (nothing was armed).
func Release(sigs []os.Signal) {}

// UnsupportedReason names the degradation for a caller that reports it. It is
// never empty on this target — an unlabelled absence is the silence Rule 15
// forbids.
func UnsupportedReason() string {
	return fmt.Sprintf("sigsource: siginfo capture is implemented for linux/amd64 only (this is linux/%s) — si_code fields are absent from the record", runtime.GOARCH)
}
