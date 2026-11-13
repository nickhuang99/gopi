package main

// serve_lock_self_hold.go — the SELF-HOLD admission rule at the ONE acquisition
// funnel (acquireServeLock), and the "this process is not another holder"
// rendering the refusal needs.
//
// THE DEFECT, measured on nick-sager 2026-10-17 (task 01a149e8's predecessor):
//
//	gopi task finish-cleanup --apply
//	→ gopi task finish-cleanup: serve lock held by a live daemon: another merge
//	  process is already running (lock /home/nick/.gopi/serve.lock held by pid
//	  2196749) — refusing double-serve …
//	  lock-holder: pid 2196749
//	  lock-kernel-records: 1 record(s) on the refused inode 25034787:
//	    6: FLOCK ADVISORY WRITE 2196749 103:05:25034787 0 EOF
//
// and pid 2196749 WAS the command: `pgrep -a -x gopi` listed exactly one process
// during that run, the one holding the lock. So the command refused ITSELF:
// `--dry-run` (which never takes the lock) worked while `--apply` was 100%
// unusable — the asymmetry is what located the defect.
//
// WHY A PROCESS CAN REFUSE ITSELF. `flock(2)` is attached to the OPEN FILE
// DESCRIPTION, not to the process: a second `open`+`flock(LOCK_EX|LOCK_NB)` in
// the SAME process, on a DIFFERENT descriptor, contends with the first and gets
// EWOULDBLOCK. A face that legitimately acquires the single-instance lock more
// than once inside one process (an outer face holding it while an inner step
// asks for it again) is therefore told "another merge process is already
// running" by its own guard.
//
// THE RULE. Self-hold is ADMISSION, announced, never refusal — the same shape
// as the existing ancestor admission (mergerLockHeldByAncestor): a process
// cannot deadlock itself, and refusing it protects nothing, because the writer
// the flock excludes is the SAME writer.
//
// THE PROOF IS THE KERNEL'S, NOT THE STAMP'S (Rule 15 — an inherited marker is
// evidence, never permission): the admission fires only when the kernel's own
// evidence (tools.LockEvidences: the /proc/locks row joined on the lock file's
// inode plus the per-process fdinfo FLOCK scan) names at least one holder and
// EVERY holder it names is THIS process. Three consequences, each deliberate:
//
//   - a THIRD-PARTY holder anywhere in the set → no admission, the ordinary
//     contention refusal stands (merge mutual exclusion is untouched: two REAL
//     merger passes still refuse each other);
//   - an UNPROVABLE scan (no /proc, an unreadable fd table) → no admission —
//     "I cannot say" is not "it is mine" (Rule 15);
//   - a STRAY holder (an unlinked copy of the lock path) → no admission: that
//     case has its own refusal, and it exists because such a holder's guard is
//     detached from the name a new acquirer would take.
//
// WHAT IS DELIBERATELY NOT HERE (Rule 35): no second lock, no pid file, no
// "self" flag file, no bypass switch. The flock stays the only mechanism; this
// file only decides WHO a contention is against.

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/nick/gopi/internal/tools"
)

const (
	// serveLockSelfHoldLine announces the admission. Printed (never silent) for
	// the same reason the ancestor admission is: a state that lets a face run
	// while a lock is held must be visible in a captured log (Rule 15).
	serveLockSelfHoldLine = "%s: the single-instance lock is already held by THIS process (pid %d) — admitting this face instead of refusing it its own flock (a flock lives on the open file description, so a second descriptor in the same process is refused by its own guard)\n"

	// serveLockSelfPidSuffix marks this process in a holder list, so a refusal
	// that names several pids can never be read as "and this one is another
	// process too".
	serveLockSelfPidSuffix = " (this process)"

	// serveLockThirdPartyHolderFmt is the refusal sentence's holder text when
	// the stamped pid is this process but the flock is kept alive by OTHERS:
	// the sentence must name the third parties, never the reader.
	serveLockThirdPartyHolderFmt = "the live holders pid %s (this process's own pid is only the stamp, not a holder)"
)

// serveLockSelfHeld answers "is this contention against THIS process?" from the
// kernel's evidence, and returns the THIRD-PARTY holders it found (empty when
// the answer is yes).
//
// ok is false for every unprovable case (no evidence, an unreadable scan, a
// stray holder) — the caller then refuses as before.
func serveLockSelfHeld() (ok bool, thirdParty []int) {
	path, err := serveLockPath()
	if err != nil {
		return false, nil
	}
	ev, probed := tools.LockEvidences(path)
	if !probed || len(ev) == 0 {
		return false, nil
	}
	self := os.Getpid()
	selfSeen := false
	for _, e := range ev {
		if e.Stray {
			// A stray holder is its own refusal (serveLockStrayHolderRefusalError);
			// admitting here would drop the guard-detached case on the floor.
			return false, nil
		}
		if e.PID == self {
			selfSeen = true
			continue
		}
		thirdParty = append(thirdParty, e.PID)
	}
	if !selfSeen || len(thirdParty) > 0 {
		return false, thirdParty
	}
	return true, nil
}

// serveLockAdmitSelfHeld builds the admission result for a contention that only
// this process is holding: the announcement, and the same read-only,
// NON-LOCKING handle the ancestor admission hands out
// (inheritedLockHandleForExemptFace — one shape for both admissions, Rule 2).
//
// The handle guards nothing and releases nothing: the flock lives on the
// descriptor its acquirer in this process already holds, so closing this one is
// a no-op on the guard. A failure to open it is REPORTED and admitted anyway —
// the guard is the outer descriptor's, not this handle's.
func serveLockAdmitSelfHeld(prog string) *os.File {
	fmt.Fprintf(os.Stderr, serveLockSelfHoldLine, prog, os.Getpid())
	handle, err := inheritedLockHandleForExemptFace()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: admitted on this process's own single-instance lock, but %v — proceeding (the guard is the outer descriptor's, not this one's)\n", prog, err)
		return nil
	}
	return handle
}

// serveLockThirdPartyHolderText renders the holder field of the contention
// sentence from the THIRD-PARTY pids — used only when the stamp names this
// process, so the refusal cannot call the reader "another merge process".
func serveLockThirdPartyHolderText(pids []int) string {
	parts := make([]string, 0, len(pids))
	for _, p := range pids {
		parts = append(parts, strconv.Itoa(p))
	}
	return fmt.Sprintf(serveLockThirdPartyHolderFmt, strings.Join(parts, ", "))
}
