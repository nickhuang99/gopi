package deepseek

// Shared dump-file naming (Rule 13/14 — single source of truth).
//
// Both death-dump writers (dump1214Request in error_1214.go and
// dumpFailedRequest in minimal_core.go) must name their file through
// writeDumpFile. Before this helper each site built the name itself as
// time.Now().Format("20060102-150405") + ".dump.json" — a 1-second
// resolution with no uniqueness suffix, so a second dump inside the same
// wall-clock second TRUNCATED the first via os.WriteFile. The first failed
// request survived nowhere, destroying the dump's only purpose
// (replayability / resume). Keeping the naming in one place is what makes
// the collision-free invariant hold for BOTH writers at once.

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/nickhuang99/gopi/internal/scratch"
)

// Named constants — Rule 1 (no magic numbers / bare keywords).
const (
	// dumpNameTimeLayout is the timestamp layout used in dump file names.
	dumpNameTimeLayout = "20060102-150405"

	// dumpFileSuffix is the file-name suffix shared by every request dump.
	dumpFileSuffix = ".dump.json"

	// dumpMaxNameAttempts bounds the collision-retry loop: attempt 0 uses
	// the bare timestamp, attempts 1..n-1 append the disambiguating suffix.
	// A retry only happens when the candidate already exists, so this bound
	// is a runaway guard, never a normal path.
	dumpMaxNameAttempts = 100

	// dumpFilePerm is the mode of a freshly created dump file.
	dumpFilePerm = 0o644

	// dumpDirPerm is the mode of the dump directory.
	dumpDirPerm = 0o755
)

// dumpNameTimestamp returns the timestamp every dump candidate name starts
// with (local time, second resolution).
func dumpNameTimestamp(t time.Time) string {
	return t.Format(dumpNameTimeLayout)
}

// dumpCandidateName builds the n-th candidate file name for a dump taken at
// timestamp ts: attempt 0 is the bare timestamp, later attempts append
// "-1", "-2", … so a same-second dump never reuses a name.
func dumpCandidateName(ts string, attempt int) string {
	if attempt <= 0 {
		return ts + dumpFileSuffix
	}
	return ts + "-" + strconv.Itoa(attempt) + dumpFileSuffix
}

// writeDumpFile writes body into dir as a timestamped dump file and returns
// the path it was created at. The create is O_CREATE|O_EXCL, so an already
// existing name (a dump taken in the same second) is NEVER overwritten —
// the next candidate gets a "-1"/"-2" suffix instead.
//
// A dump is best-effort diagnostics: on any failure the returned path is ""
// (the caller then reports "dump write failed"), the error is logged, and
// the caller's fail-fast error still carries the real cause (Rule 15b — no
// silent failure, never fatal).
func writeDumpFile(dir string, body []byte, logTag string) string {
	return writeDumpFileAt(dir, dumpNameTimestamp(time.Now()), body, logTag)
}

// writeDumpFileAt is writeDumpFile with an explicit timestamp, so the
// same-second collision rule is testable deterministically (Rule: no sleeps
// — a test passes ONE ts to prove two writes in the same second both land).
func writeDumpFileAt(dir, ts string, body []byte, logTag string) string {
	if err := scratch.MkdirAllWithSuiteRunOwnerPerm(dir, dumpDirPerm); err != nil {
		log.Printf("%s dump mkdir failed: %v", logTag, err)
		return ""
	}
	for attempt := 0; attempt < dumpMaxNameAttempts; attempt++ {
		path := filepath.Join(dir, dumpCandidateName(ts, attempt))
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, dumpFilePerm)
		if err != nil {
			if os.IsExist(err) {
				continue // same-second collision — take the next suffix
			}
			log.Printf("%s dump create failed: %v", logTag, err)
			return ""
		}
		if _, err := f.Write(body); err != nil {
			log.Printf("%s dump write failed: %v", logTag, err)
			// Close, but a close error must not mask the write error.
			if cerr := f.Close(); cerr != nil {
				log.Printf("%s dump close after failed write: %v", logTag, cerr)
			}
			return ""
		}
		if err := f.Close(); err != nil {
			log.Printf("%s dump close failed: %v", logTag, err)
			return ""
		}
		return path
	}
	log.Printf("%s dump name exhausted %d attempts (dir %s)", logTag, dumpMaxNameAttempts, dir)
	return ""
}
