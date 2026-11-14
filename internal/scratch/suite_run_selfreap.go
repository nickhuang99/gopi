// The SELF-REAP half of the suite-run family: the judgement a suite asks about
// the ONE run root it created itself, at the moment that suite is ending.
//
// WHY THIS IS A DIFFERENT ARM FROM base_suite_runs.go (measured on this host,
// 2026-10-22): /dev/shm held 19 `suite-*-run-*` roots / 1.7G, every one created
// the same day, and NOTHING collected them —
//
//	$ ls -d /dev/shm/suite-* | wc -l      → 19
//	$ for d in /dev/shm/suite-*; do grep '^pid=' $d/.holder; done
//	   → every registration named a pid with no /proc entry (dead)
//
// The family's only DELETING collector is SweepBaseSuiteRuns, which the
// operator's one-shot `gopi tool scratch-gc base` and the placement's
// reclaim-before-decline call; the resident tick deliberately only REPORTS it
// (see BaseSuiteRunReport's header — deleting the shared base from a resident
// pass was the 2026-10-01 mistake). And that sweep's age window is
// DefaultStaleAge = 6h, so a root minted this morning is NOT collectable this
// afternoon: the day's residue waits a full window before any collector may
// touch it. The in-process cleanup (internal/testutil's FinishSuiteForTestMain)
// covers only the CLEAN exit; a suite ended by a signal skips it, and nothing
// else in the process ever runs.
//
// WHAT THE SELF-REAP MAY CITE AND WHAT IT MAY NOT. The other collectors reason
// about strangers: an age window, a /proc scan, a holder registration — because
// they cannot know who made a root. The run that mined the root does not have
// that problem, and must not use the weaker evidence: it knows the PATH it
// created, and the registration inside it names THIS PROCESS. Both are checked
// here, and BOTH must hold:
//
//  1. the path is in this system's suite-run namespace (nameIsDefaultBaseSuiteRun)
//     — the same namespace predicate every sibling arm uses, so a caller that
//     points this at anything else (a task worktree, another program's
//     directory) reaps nothing;
//  2. the owner record inside it proves its owner DEAD (gone, or a pid whose
//     incarnation was recycled). A record naming a LIVE process is refused with
//     that pid in the reason — whoever it names, **this process included**.
//
// WHY "THIS PROCESS" IS ALSO REFUSED (measured 2026-10-23, `make test-budget
// PKGS='./internal/cgroupscope ./cmd/agent'`): the earlier spelling of rule 2 read
// "written by THIS process OR already dead", i.e. a LIVE registration naming the
// caller was an authorisation. That is true of residue and false of a run: the
// suite's signal arm (internal/testutil) asks this verdict from a handler that
// fires while the process is very much ALIVE — `cmd/agent`'s
// TestWorkerStopReleasesOwnWorktree raises SIGTERM at its own pid, and the run
// then continues for 2,900 more tests. The arm moved the live root aside under
// that run (/dev/shm/suite-cmd-agent-run-755918610 → …_shelved), a concurrent
// writer recreated the original name, the restore failed with `file exists`, and
// every test that had been using the root met
// `TempDir: stat …: no such file or directory` — four red tests in one run, all
// green alone. **A live registration is evidence of a RUN, never of residue**:
// it authorises a REPORT, and the collector that may act on it is the start-time
// sweep of the same suite, which reads the same record once the owner is gone
// (ReapDeadSuiteRunRootsUnder).
//
// The age window is deliberately NOT part of this judgement: a root this process
// created moments ago is exactly the one it is entitled to remove, and requiring
// it to be six hours old would be a collector that never fires.
package scratch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nickhuang99/gopi/internal/procname"
)

// The sentences a self-reap reports. Constants for the usual reason (Rule 1b):
// the caller's log line and the regression test quote the same words.
const (
	// SelfReapReasonOwn: the registration inside the root names this process —
	// a LIVE owner, i.e. a RUN whose root this is, never residue. It is a KEEP
	// sentence (see the package header for the measured reason): the root of a
	// live run is removed by that run's own clean exit, or by the next run of the
	// same suite once the recorded pid is gone.
	SelfReapReasonOwn = "the owner registration names this process, which is LIVE — a run keeps its own root"
	// SelfReapReasonOwnerDead: the registration names a process the kernel
	// confirms gone (or a pid whose incarnation was recycled).
	SelfReapReasonOwnerDead = "the owner registration names a process that is gone"
	// SelfReapReasonNotOurs: the path is outside the suite-run namespace.
	SelfReapReasonNotOurs = "not a per-run suite root of this system"
	// SelfReapReasonOtherLive: the registration names ANOTHER live process —
	// another run's root, never this one's to remove.
	SelfReapReasonOtherLive = "the owner registration names another LIVE process"
	// SelfReapReasonUnmeasured: the record could not be read or the kernel would
	// not answer. Unknown is never a delete (Rule 15).
	SelfReapReasonUnmeasured = "the owner could not be measured"
	// SelfReapReasonAbsent: the candidate is already gone.
	SelfReapReasonAbsent = "the path does not exist"
)

// SuiteRunSelfReapVerdict judges whether a CALLER may remove the per-run root at
// path. It deletes nothing: the verdict is the authorisation, and the caller
// reports it whether it acts or not.
func SuiteRunSelfReapVerdict(path string) (collectable bool, reason string) {
	if path == "" {
		return false, SelfReapReasonAbsent
	}
	if _, err := os.Lstat(path); err != nil {
		// Vanished (or unreadable): nothing for this arm to authorise, and an
		// unreadable path is an unknown, never a delete (Rule 15).
		return false, SelfReapReasonAbsent
	}
	if !nameIsDefaultBaseSuiteRun(filepath.Base(path)) {
		return false, SelfReapReasonNotOurs
	}
	v := OwnerVerdictFor(path)
	if !v.Registered {
		// The pin registers the root it creates, so an unregistered suite root is
		// not the shape this arm serves. It stays for the age-window arm, which is
		// the collector that reasons about strangers.
		return false, fmt.Sprintf("%s (%s)", SelfReapReasonUnmeasured, v.Reason)
	}
	if v.Collectable {
		return true, SelfReapReasonOwnerDead
	}
	if v.Outcome == procname.OutcomeLive && v.Record.Pid == os.Getpid() {
		// A LIVE registration is evidence of a RUN, never evidence of residue —
		// whoever the registration names, this process included. See
		// SelfReapReasonOwn for the measurement that turned this into a refusal.
		return false, SelfReapReasonOwn
	}
	if v.Outcome == procname.OutcomeLive {
		return false, fmt.Sprintf("%s (pid %d)", SelfReapReasonOtherLive, v.Record.Pid)
	}
	return false, fmt.Sprintf("%s (%s)", SelfReapReasonUnmeasured, v.Reason)
}

// ReapOwnSuiteRunRoot removes the per-run root at path IF SuiteRunSelfReapVerdict
// authorises it, and reports the decision either way so a caller can print it.
//
// The removal goes through removeCollectable — the ONE deleting primitive of this
// package — so a root carrying a dead FUSE mount, a permission oddity or a nested
// tree is handled exactly as every other arm handles it, rather than by a second
// deletion implementation.
func ReapOwnSuiteRunRoot(path string) (removed bool, reason string, err error) {
	collectable, reason := SuiteRunSelfReapVerdict(path)
	if !collectable {
		return false, reason, nil
	}
	if err := removeCollectable(path); err != nil {
		return false, reason, fmt.Errorf("remove suite run root %s: %w", path, err)
	}
	return true, reason, nil
}

// ReapDeadSuiteRunRootsUnder collects the run roots of ONE suite (name prefix
// pattern, e.g. "suite-tools-run-") directly inside parent whose recorded owner
// is PROVEN dead, and reports every root it declines with the reason.
//
// WHY THIS ARM EXISTS IN ADDITION TO THE SIGNAL HANDLER: a signal handler is the
// in-process half, and it cannot reach every ending. The testing package's own
// `-timeout` clock stops the binary with os.Exit(2) — no signal, no deferred
// statement, no handler — and a SIGKILL is unreachable by construction. Those
// endings leave exactly the residue measured on this host (19 roots / 1.7G, every
// .holder naming a pid with no /proc entry), and the family's only other deleting
// collector is gated on DefaultStaleAge = 6h, so today's residue waits a full
// window. This arm is the MISSING half: it runs at the ONE moment a suite is
// entitled to judge its own family — its own start, before it creates anything —
// and it needs no age window at all, because the evidence is the owner record
// (gone/recycled) and not a guess about a stranger.
//
// THE SCOPE IS THE SUITE'S OWN PREFIX under a parent the pin itself uses, so one
// suite can never collect another's roots, and a root whose record names a LIVE
// pid — another run happening right now on the shared base — is kept and named.
func ReapDeadSuiteRunRootsUnder(parent, pattern string) (removed int, kept []string, err error) {
	entries, rerr := os.ReadDir(parent)
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return 0, nil, nil
		}
		return 0, nil, fmt.Errorf("read %s: %w", parent, rerr)
	}
	self := os.Getpid()
	for i, entry := range entries {
		if i >= ScanLimit {
			return removed, kept, fmt.Errorf("%s has more than %d entries — sweep stopped early", parent, ScanLimit)
		}
		name := entry.Name()
		if !strings.HasPrefix(name, pattern) {
			continue
		}
		path := filepath.Join(parent, name)
		// The self-reap judgement already refuses a foreign namespace, an
		// unreadable record and a record naming ANOTHER live pid; the only extra
		// exclusion here is this very process's own root, which is the one the
		// signal handler and the clean path own.
		if v := OwnerVerdictFor(path); v.Registered && v.Outcome == procname.OutcomeLive && v.Record.Pid == self {
			// This run's OWN root: not residue, not a decision — the clean exit and
			// the signal handler own it. Skipped silently so the kept list stays a
			// list of things an operator must look at.
			continue
		}
		collectable, reason := SuiteRunSelfReapVerdict(path)
		if !collectable {
			kept = append(kept, path+": "+reason)
			continue
		}
		if rerr := removeCollectable(path); rerr != nil {
			kept = append(kept, path+": "+rerr.Error())
			continue
		}
		removed++
	}
	return removed, kept, nil
}
