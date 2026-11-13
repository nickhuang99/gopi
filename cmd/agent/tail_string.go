package main

// tail_string.go — "the last N characters of a string", re-homed here.
//
// WHY IT MOVED (retire-merge step 1, 2026-10-28). The helper was declared in
// merger_face_shared.go, a file that existed only to carry what OUTLIVED the
// retired resident merger (epic merger-delete-dead-impl, 2026-09-19). Of its two
// contents, `mergerTakeoverLogBaseName` was a dead constant — the take-over
// launcher that wrote that file is gone and no live face names it any more — so
// the file can go. `tailString` is NOT merger machinery: its callers are the
// daemon-start report (cli_daemon_spawn.go) and its arm in the daemon scope test,
// both of which are about `gopi worker --daemon`, a face that has nothing to do
// with merging.
//
// So the general helper is re-homed in its OWN file FIRST, and the merge file is
// then deleted (Rule 43: a symbol referenced outside the mechanism is moved out
// before the mechanism goes). The body is unchanged, character for character, so
// no rendered daemon-start verdict moves.

// tailString returns the LAST n characters of s ("" for n<=0 or an empty s).
// It exists for the daemon-start report: a failed start's CAUSE is at the END
// of the child's log, and printing the whole log would bury it.
func tailString(s string, n int) string {
	if n <= 0 || s == "" {
		return ""
	}
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
