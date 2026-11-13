package daemonlifetime

// The guard for Watch's borrowed-fd treatment.
//
// WHY THERE IS NO BEHAVIOURAL GUARD HERE, measured rather than assumed: the
// hazard is a wrapper becoming UNREACHABLE while the descriptor is still needed,
// and Watch's reading goroutine has no such state — its three completion routes
// are EOF (n==0, err!=nil → selfTerminate), a written byte (n>0 → selfTerminate)
// and a read error (the disarm branch, which prints and returns). The first two
// kill this process with SIGKILL, so no observation can be taken in a living
// process; the third needs an O_NONBLOCK read end that production never creates
// (the holder's pipe comes from os.Pipe, blocking by construction) — and a fixture
// that forces it would park or kill the suite rather than measure the fix.
//
// What CAN be verified, and is: the production source carries the classification
// and the detach at the construction site. This is a source pin, not a red-then-
// green behavioural guard, and it is reported as such.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestWatchDetachesItsBorrowedWrapper pins Watch's classification and treatment
// against the production source, so a later edit that drops the detach (or the
// reason for it) fails here rather than passing for lack of a fixture.
func TestWatchDetachesItsBorrowedWrapper(t *testing.T) {
	src, err := os.ReadFile(watchSourcePath(t))
	if err != nil {
		t.Fatalf("read the daemonlifetime source: %v", err)
	}
	text := string(src)
	// The detach must follow the wrapper: os.NewFile first, Detach immediately.
	wrap := strings.Index(text, "f := os.NewFile(uintptr(fd), EnvName)")
	if wrap < 0 {
		t.Fatalf("daemonlifetime.go no longer wraps the inherited death-pipe fd with os.NewFile — the site moved and this pin must follow it")
	}
	tail := text[wrap:]
	detach := strings.Index(tail, "borrowedfd.Detach(f)")
	nilGuard := strings.Index(tail, "if f == nil {")
	if detach < 0 || nilGuard < 0 || detach < nilGuard {
		t.Fatalf("Watch no longer detaches its borrowed wrapper after os.NewFile (nilGuard=%d, detach=%d) — "+
			"the descriptor is BORROWED (its number arrives through %q, so this process never opened it), and a wrapper "+
			"dropped on the disarm route would have its finalizer close the HOLDER's descriptor", nilGuard, detach, EnvName)
	}
	if !strings.Contains(text, "OWNERSHIP: this descriptor is BORROWED") {
		t.Fatalf("daemonlifetime.go no longer states the ownership classification at the Watch site — the classification is the load-bearing step (Detach on an OWNED wrapper would leak the descriptor forever)")
	}
}

// watchSourcePath resolves daemonlifetime.go next to this test file.
func watchSourcePath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller could not resolve this test file's path — the source pin cannot find daemonlifetime.go")
	}
	return filepath.Join(filepath.Dir(thisFile), "daemonlifetime.go")
}
