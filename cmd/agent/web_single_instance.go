package main

// web_single_instance.go — ONE `gopi web` server at a time, per profile.
//
// WHY IT EXISTS. The operator's ruling (2026-11-07, this row's briefing) draws
// the line exactly: a browser TAB is a client and there may be any number of
// them, but the SERVER is the thing that executes turns against a session, and
// two servers on one profile would execute the same conversation twice. The
// address/port is deliberately NOT part of the criterion — "even address/port
// combination is considered single instance, still we only accept one `gopi
// web` instance" — so the lock is keyed on the PROFILE, not on a socket.
//
// WHAT IT IS NOT. It does not limit browsers: the HTTP page, the SSE stream and
// the access token are unchanged, and N tabs still reach the one server. It does
// not replace the per-session run lock (`run_lock_seam.go`), which arbitrates
// who EXECUTES a session directory; this one arbitrates who is the server at
// all, and the two are different questions with different scopes.
//
// THE MECHANISM IS THE ONE THAT ALREADY EXISTS: `internal/runlock` — the kernel
// flock on a directory's `run.lock`, the same file, the same holder stamp, the
// same refusal text. No new lock file format, no pid file that can go stale, no
// second implementation of "who holds this" (Rule 14 / Rule 35).

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/runlock"
)

const (
	// NOTE on the program name: the refusal and the notice below render
	// webResumeProgName (`"gopi web"`), the face's ONE program-name constant,
	// defined in web_resume.go. It is REUSED rather than re-declared — a second
	// constant holding the same string would be exactly the bare-literal
	// duplication Rule 1/13 forbid.

	// webServerLockDirName is the directory (under the profile) whose kernel
	// lock means "a `gopi web` server is running on this profile". Named
	// (Rule 1): the refusal text, the missing-profile notice and the tests all
	// read THIS, so a rename cannot leave one of them naming a path nothing
	// takes.
	webServerLockDirName = "web-server"

	// webServerLockBusyFormat is the refusal a SECOND server gets. It names the
	// lock, the holder the kernel recorded, and the live door — because the
	// operator's next move after this refusal is a browser tab on the server
	// that already runs, not a second attempt.
	webServerLockBusyFormat = "%s: another %s server is already running on this profile (%s) — %s\n" +
		"%s: one server at a time; open another BROWSER TAB on the running one (its URL is on its own banner), or stop it first\n"

	// webServerLockNoProfileFormat is the notice printed when the profile
	// directory cannot be resolved. It is NOT a refusal: a server whose lock
	// cannot be placed still serves, and silence would make "single instance"
	// indistinguishable from "this build forgot to check" (Rule 15).
	webServerLockNoProfileFormat = "%s: cannot resolve the profile directory (%v) — the single-instance lock is NOT taken, so a second server on this profile would not be refused\n"
)

// webServerLockDir is the ONE place the lock's directory is derived from a
// profile directory — the production seam and its test both call it, so a test
// cannot assert about a path the server does not take (Rule 14).
func webServerLockDir(gopiDir string) string {
	return filepath.Join(gopiDir, webServerLockDirName)
}

// webTakeServerLock takes the profile-wide server lock and returns its release.
// The caller MUST call the release on every exit path (it is a defer in
// runWeb), including the listen-failure return below.
//
// An UNRESOLVABLE profile returns a no-op release and the notice, never an
// error: the lock is a safety property of a working profile, and turning a
// missing profile into a refusal here would make this face refuse to start for
// a reason that has nothing to do with what the operator asked for.
func webTakeServerLock() (func(), error) {
	gopiDir, err := config.GopiDir()
	if err != nil || gopiDir == "" {
		fmt.Fprintf(os.Stderr, webServerLockNoProfileFormat, webResumeProgName, err)
		return func() {}, nil
	}
	lockDir := webServerLockDir(gopiDir)
	h, lerr := runlock.Lock(lockDir)
	if lerr != nil {
		fmt.Fprintf(os.Stderr, webServerLockBusyFormat,
			webResumeProgName, webResumeProgName, lerr.Error(), runlock.PathIn(lockDir),
			webResumeProgName)
		return nil, lerr
	}
	return func() {
		// A release failure is reported, never swallowed (Rule 15): the
		// operator needs to know the profile is still marked as served by a
		// process that is going away.
		if rerr := h.Release(); rerr != nil {
			fmt.Fprintf(os.Stderr, "%s: releasing the server lock %s: %v\n", webResumeProgName, h.Path(), rerr)
		}
	}, nil
}
