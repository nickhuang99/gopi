// Package borrowedfd owns the ONE retention idiom for file descriptors this
// process did NOT open.
//
// The defect it exists for: os.NewFile(fd, name) wraps an already-open
// descriptor AND installs runtime.SetFinalizer(f.file, (*file).close)
// (GOROOT/src/os/file_unix.go). The wrapper therefore claims OWNERSHIP of a
// descriptor it never opened. When the wrapper becomes unreachable — the
// function returns, the result is dropped into `_`, or only a local held it —
// the runtime's finalizer goroutine (runtime.runfinq) closes that descriptor.
// Two damages follow, and they are why a wrapper may never simply be dropped:
//
//  1. SURVIVAL — the number is closed under whoever really owns it (an os/exec
//     ExtraFiles dup, or a number handed in through an environment variable),
//     so the owner's next readdirent/close returns EBADF;
//  2. RECYCLE — the kernel hands the freed number to whatever is opened next,
//     so the owner's own handle silently denotes a STRANGER's file.
//
// The measured production shape (task 01a0b5cd, devdoc/fd-finalizer-leak.md):
// `TempDir RemoveAll cleanup: readdirent 001: bad file descriptor`, fd 7, whose
// stack arrived from runtime.runfinq — a directory recursion's number, recycled
// under a dropped lock wrapper.
//
// READING A WRAPPER'S OWN STATE CANNOT DECIDE THIS — the classification is:
// a wrapper is BORROWED when its fd number was NOT opened by this process (it
// arrived through os/exec's ExtraFiles renumbering, or was named by another
// process in the environment), and OWNED when this process called os.Open /
// os.OpenFile / os.Pipe itself. BORROWED wrappers are the only ones that need
// Detach; calling Detach on an OWNED wrapper would suppress the close of a
// descriptor this process really owns and leak it for the life of the process.
//
// A BORROWED wrapper that the site itself CLOSES is NOT a defect site: os.File
// Close sets the poll.FD's closed flag and clears the finalizer
// (os.(*file).close → runtime.SetFinalizer(file, nil), GOROOT/src/os/file_unix.go),
// so the already-run finalizer is a no-op. `defer f.Close()` on a borrowed
// number borrowed only to write a byte is correct as it stands.
//
// This package is a LEAF on purpose: it imports os, runtime and time only, and
// no project package, so every consumer (including leaf packages of their own,
// such as internal/daemonlifetime) can import it without a cycle — and so that
// the retention idiom AND the instrument that measures it have exactly ONE
// definition (Rule 14) rather than a copy per package.
package borrowedfd

import (
	"os"
	"runtime"
)

// wrappers retains every wrapper this process detached, so the Go runtime can
// never finalize one and close the descriptor it names.
//
// WHY A PACKAGE-LEVEL SLICE AND NOT A LOCAL: the hazard is a GC finalizer, and a
// finalizer runs only once its object is UNREACHABLE. A local variable going out
// of scope is exactly what makes it unreachable, so the retention must outlive
// the call. The slice is bounded by the number of borrowed descriptors this
// process ever wrapped — a handful per process, never a per-iteration allocation
// — so retaining them costs nothing and leaks nothing: the kernel reclaims each
// descriptor when the process exits, and the wrapper is the LAST thing standing
// between a borrowed number and the collector.
var wrappers []*os.File

// Detach clears f's finalizer and keeps the wrapper reachable for the life of
// this process, without closing the descriptor.
//
// Call it at EVERY exit of a function that wrapped a BORROWED descriptor —
// refusals included. The refusal exits matter most: they are the ones an
// ordinary run takes (a probe that decides not to adopt), and they are exactly
// the paths where nothing else references the wrapper.
//
// A nil f is not an error: os.NewFile's answer for an unusable number is
// sometimes nil (the documented `uintptr(0)` sentinel shape), and there is then
// no finalizer to forget.
func Detach(f *os.File) {
	if f == nil {
		return
	}
	runtime.SetFinalizer(f, nil)
	wrappers = append(wrappers, f)
}

// Retained reports how many wrappers this process currently retains. It exists
// so a guard can prove the retention actually happened rather than inferring it
// from a green measurement that might be measuring nothing.
func Retained() int { return len(wrappers) }
