// Worktree-cwd release — THE ONE IMPLEMENTATION of "this process hands back
// the directory it was standing in".
//
// THE OBLIGATION (PROJECT.md Rule 24's sibling, measured 2026-09-14): a process
// that can stand inside a task's linked worktree must release its CWD when it
// is done with that worktree, because Linux has no syscall to move ANOTHER
// process's cwd — the release is a PRODUCER OBLIGATION, never something a
// reclaim arm can do on the holder's behalf. The release implementation cannot
// live in the reclaim arm either: package tools is the CONSUMER of the reclaim
// arms (internal/agent and cmd/agent both import it), so a reusable release
// function defined there would have to be imported BY them, which is the cycle
// that kept this duty duplicated per call site in the first place.
//
// So the chdir lives in this leaf package (zero project imports, the
// internal/toolnames precedent of PROJECT.md Rule 14): every producer — the
// `gopi task run` wrapper's worker-round close-out, the merger pass, the
// executor child's exit, the resumed session — reaches it by IMPORT, and there
// is exactly one spelling of the destination (ProcessRootPath) in the tree.
//
// WHY THE ROOT: `git worktree remove` deletes the directory the process is
// standing in, and a process holding a DELETED directory keeps its inode —
// and every file under it — alive until the process exits (measured: a holder
// at "/…/wt (deleted)" while `git worktree list` still named the tree). The
// filesystem root always exists, so the chdir cannot fail on a directory
// somebody else removed first.
//
// BEST-EFFORT BY CONTRACT: the release is a SIDE EFFECT, never control flow.
// A failed chdir leaves the caller exactly as it was and is REPORTED (GOPI.md
// Rule 15) — an operator who sees repeated failures learns that directories
// will keep their inodes until the process exits; nobody ever gets a task
// outcome changed by a chdir.
//
// NOT THE SAME DUTY AS internal/daemoncwd (say so, so a future reader does not
// mistake the two for competing implementations): daemoncwd decides the cwd a
// long-lived CHILD must START in, before it exists — the spawn-side half.
// This package performs the RELEASE a live process makes when it is done with
// a worktree — the exit-side half. A child that escaped its inheritance still
// owes this release once it has stood anywhere else; a process that never left
// the root owes nothing. Both halves are producer obligations and neither
// weakens finish-cleanup guard ③.
package worktreerelease

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// ProcessRootPath is the directory a producer's close-out re-parents its CWD
// to. It is owned HERE (Rule 13/14) and referenced from both the releaser and
// the callers' diagnostics: a second spelling of this destination elsewhere in
// the tree is the very drift the release rules exist to stop.
const ProcessRootPath = "/"

// Release re-parents THIS process's CWD to ProcessRootPath and returns an
// error when it could not. Callers report the error (Rule 15) and continue:
// the release never changes a task outcome.
//
// A process that is already standing at ProcessRootPath is a no-op — the
// chdir is idempotent, so a close-out that runs on every exit path (deferred
// calls, nested wrappers) costs nothing after the first one.
func Release() error {
	// The already-there check keeps the common nested case silent: a worker
	// round's release runs after the wrapper's own release only in the
	// `task run` face (which exits), but a resumed/test process may call it
	// repeatedly, and a repeated chdir to the same directory is noise.
	if wd, err := os.Getwd(); err == nil && wd == ProcessRootPath {
		return nil
	}
	if err := os.Chdir(ProcessRootPath); err != nil {
		return fmt.Errorf("release cwd to %s: %w", ProcessRootPath, err)
	}
	return nil
}

// ReleaseReporting performs the release FOR A PRODUCER SEAM and reports a
// failure to w as the ONE operator-facing line (prog names the face: "gopi task
// run", "gopi worker", "gopi merger", "gopi executor"). Returns true when the
// process is (now) standing at ProcessRootPath — including the two cases where
// no chdir was needed.
//
// TWO SKIPS, both deliberate and both measured:
//
//  1. A process already at ProcessRootPath — nested close-outs cost nothing.
//  2. A GO TEST BINARY (TestProcessExecutableSuffix). MEASURED, not theoretical:
//     wiring this seam into the producer close-outs without this guard moved the
//     cmd/agent SUITE's cwd to "/" — every later cwd-relative test broke (30
//     failures, e.g. "read fixture ../../tests/fixtures/…: no such file"), because
//     production close-out seams are exercised in-process by the suite. The
//     release's contract is "this process is DONE with the directory and may
//     die"; a test binary is neither — its cwd is the HARNESS's working
//     context, shared by every sibling test in the run, and the directory the
//     kernel keeps alive is the one the go-test directory owns. Relocating it
//     is not a release, it is a hijack. A test that needs to observe the
//     release calls Release() directly (cmd/agent/cwd_release_rig_test.go does
//     exactly that, in a re-exec'd holder).
//
// This is the convenience the close-out seams use so the report shape — and the
// "a failure is never fatal" contract — live in one place (Rule 1: one message
// shape).
func ReleaseReporting(w io.Writer, prog string) bool {
	if inTestProcess() {
		return true
	}
	if err := Release(); err != nil {
		fmt.Fprintf(w, "%s: %v (the removed directory's inode stays busy until this process exits)\n", prog, err)
		return false
	}
	return true
}

// TestProcessExecutableSuffix is the file-name suffix a compiled Go test binary
// carries (`go test` builds `<pkg>.test`). It is the mechanical mark of "this
// process is a test harness, not a producer" — the same suffix the scratch gate
// uses for the same class of artifact (Rules 1b/13: one spelling, here).
const TestProcessExecutableSuffix = ".test"

// inTestProcess reports whether THIS process is a compiled Go test binary (see
// TestProcessExecutableSuffix for why the seam consults it). An unreadable
// executable path is NOT treated as a test process: the release is a producer
// obligation and the default is to perform it, never to skip it silently.
func inTestProcess() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	return strings.HasSuffix(exe, TestProcessExecutableSuffix)
}
