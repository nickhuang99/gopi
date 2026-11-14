package agent

import (
	"bytes"
	"encoding/json"
	"os"

	"github.com/nickhuang99/gopi/internal/procstat"
	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// ProcessAliveStampField is the session_start key that records the writing
// process's PID. Its value is the ONLY evidence of liveness available to a
// later run of gopi: the session JSONL is append-only, so a session whose
// process is still running simply has no session_end line yet — the missing
// marker is not evidence of death.
//
// Named (not inlined) because the writer (NewSessionLoggerForRun) and the
// reader (ClassifyOrphanSession) must agree on the exact key (Rule 1b/13).
const ProcessAliveStampField = "pid"

// OrphanStatus is the classification of a session JSONL that a previous
// crash-dump backfill sweep would have called an "orphan".
//
// Three values, not two: "crashed" is a CLAIM about a process that died
// badly, and a claim needs evidence. Whenever the evidence is missing the
// answer is OrphanUnknown, which the caller reports as nothing at all
// (Rule 15 — the startup line stays silent rather than over-claiming).
type OrphanStatus string

const (
	// OrphanCrashed — the process is demonstrably GONE and no clean end
	// marker exists: the one case a crash dump may legitimately report.
	OrphanCrashed OrphanStatus = "crashed"
	// OrphanLive — the writing process is still alive, or the session ended
	// cleanly. Both mean "not a crash" and are reported as nothing.
	OrphanLive OrphanStatus = "live"
	// OrphanUnknown — the session cannot be classified (unreadable file, no
	// liveness stamp, a process table the OS refused to answer for). Not
	// reported as a crash.
	OrphanUnknown OrphanStatus = "unknown"
)

// OrphanVerdict couples a classification with the reason that produced it, so
// the startup line and the devdoc can explain WHY a session was reported (or
// deliberately not reported) instead of printing a bare count.
type OrphanVerdict struct {
	// Status is the classification.
	Status OrphanStatus
	// Reason is a short human-readable clause trace, e.g.
	// "pid 1234 gone + no session_end". It names the clause that decided.
	Reason string
	// PID is the liveness stamp read from the session_start event (0 when
	// the session predates the stamp).
	PID int
}

// sessionScanHeadMax is how many leading bytes are searched for the
// session_start event. The event is written and fsynced FIRST, before any
// other line, so it is always the first line — the bound only protects
// against a pathologically long first line, it is not a search window.
const sessionScanHeadMax = 4096

// ClassifyOrphanSession decides whether the session JSONL at path is a crash.
//
// The predicate has exactly two clauses, and BOTH must hold:
//
//	clause 1 (process gone): the PID stamped into the session_start event is
//	    no longer alive. A session whose writer still exists is alive, not
//	    crashed — the original bug was classifying exactly these files as
//	    crashes, which is why every plain `gopi` start printed a phantom
//	    count. If no PID was stamped, or the OS will not answer for that PID,
//	    the clause is INDETERMINATE: the session is OrphanUnknown, never a
//	    crash (Rule 15: unclassifiable ⇒ not reported).
//
//	clause 2 (no clean end): the file carries no session_end marker in its
//	    tail. A session that ended cleanly has already told us how it ended;
//	    a lingering dump for it is stale bookkeeping, not evidence.
//
// Only clause1==gone AND clause2==absent yields OrphanCrashed. Note the
// asymmetry that fixes the original bug: an unreadable session (no PID
// visible) is Unknown, while an unreadable session that also has no
// session_end is NOT promoted to Crashed.
func ClassifyOrphanSession(path string) OrphanVerdict {
	pid, ok := sessionStampPID(path)
	if !ok {
		// No usable liveness stamp: we cannot prove the process is gone.
		// Falling back to the old "no session_end ⇒ crash" rule here is
		// exactly the over-claim this epic removes.
		return OrphanVerdict{Status: OrphanUnknown, Reason: "no liveness stamp in session_start"}
	}
	switch procstat.Alive(pid) {
	case procstat.VerdictAlive:
		return OrphanVerdict{
			Status: OrphanLive,
			Reason: "process still alive — session not finished, not a crash",
			PID:    pid,
		}
	case procstat.VerdictUnknown:
		return OrphanVerdict{
			Status: OrphanUnknown,
			Reason: "process liveness undeterminable (OS did not answer for this pid)",
			PID:    pid,
		}
	}
	if HasSessionEnd(path) {
		return OrphanVerdict{
			Status: OrphanLive,
			Reason: "clean session_end present — ended normally",
			PID:    pid,
		}
	}
	return OrphanVerdict{
		Status: OrphanCrashed,
		Reason: "process gone and no session_end — crashed",
		PID:    pid,
	}
}

// sessionStampPID reads the PID recorded in the session's session_start
// event. ok is false when the file is unreadable, empty, carries no
// session_start line, or the stamp is absent/non-numeric — every one of
// which is "no evidence of death", never "dead".
func sessionStampPID(path string) (int, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()

	head := make([]byte, sessionScanHeadMax)
	n, err := f.Read(head)
	if n <= 0 && err != nil {
		return 0, false
	}
	head = head[:n]

	// The stamp may sit on any of the head's lines: survive a leading
	// blank/garbage line rather than declaring the session unstamped.
	for _, line := range bytes.Split(head, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var ev struct {
			Type string `json:"type"`
			PID  int    `json:"pid"`
		}
		if json.Unmarshal(line, &ev) != nil {
			continue
		}
		if ev.Type != sessionevent.EventTypeSessionStart {
			continue
		}
		if ev.PID <= 0 {
			return 0, false
		}
		return ev.PID, true
	}
	return 0, false
}
