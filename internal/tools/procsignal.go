package tools

// procsignal.go — a signal is addressed to a PROCESS, never to a NUMBER
// (Rule 42). These are the primitives; WHO may be signalled is the caller's
// question, and this file deliberately answers nothing about it.
//
// WHY THE PRIMITIVE EXISTS. A pid is a recyclable number: a caller decides
// about pid N by reading /proc, then signals "pid N". Between those two reads
// the process may have exited and the kernel may have handed the number to
// something else entirely — and SIGKILL cannot be caught, so nothing softens
// the misdelivery. The decision and the signal are two observations of one
// process and they must be the SAME process: the incarnation (pid + kernel
// start ticks) is re-read immediately BEFORE the signal and immediately AFTER
// it, and a change on either side means the number no longer names what was
// decided about, so nothing is claimed and nothing further is sent.
//
// WHY BOTH SIDES. A pre-flight read alone leaves open the window it was meant
// to close (the pid can be recycled between the check and kill(2)); a
// post-flight read alone cannot refuse a signal already sent. Together they
// answer the question a caller actually needs — "did MY process receive it?" —
// and the three outcomes are reported separately because they are three
// different facts (Rule 15).
//
// This file was extracted from the close-out reap (removed 2026-11-05). The
// reap used it to signal its own leftovers; the job tool uses it to signal a
// killed job's detached descendants. Nothing here knows about either: the
// before-signal hook and the sender are PARAMETERS, so each caller supplies
// its own and this file carries no caller's vocabulary.

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/nick/gopi/internal/procname"
)

// SignalOutcome is what one incarnation-guarded signal did. The three values
// are three different facts, and a caller reports them by name rather than
// collapsing them into one bool (Rule 15: silent failure).
const (
	// SignalDelivered: the same incarnation was read before AND after.
	SignalDelivered = "delivered"
	// SignalSkippedIncarnationChanged: a reading changed — nothing is claimed.
	SignalSkippedIncarnationChanged = "skipped-incarnation-changed"
	// SignalAlreadyGone: the kernel says ESRCH — the process had already
	// exited, which is usually the outcome the caller wanted.
	SignalAlreadyGone = "already-gone"
)

// procSignalInitPid is the process the kernel reparents a dead launcher's
// children to. It is never a descendant of anything here.
const procSignalInitPid = 1

// ProcessIncarnationOf reads a pid's kernel incarnation (stat field 22) as a
// string. "" = unreadable, which is NEVER a licence to signal: an unmeasured
// incarnation cannot be compared, so the guard refuses.
func ProcessIncarnationOf(pid int) string {
	ticks, ok := ProcessStartTicks(pid)
	if !ok {
		return ""
	}
	return normalizeTicks(ticks)
}

// ProcessStartTicks reads a process's incarnation — the value that tells a
// live process from a recycled pid number. ok=false when it cannot be read.
//
// Deliberately a VAR so a test can pin the reading and exercise the real
// branch under it (the same seam discipline every other process primitive
// here uses).
var ProcessStartTicks = func(pid int) (string, bool) {
	return procname.StartTicks(procRootName, strconv.Itoa(pid))
}

// OwnPid is the pid that must always be EXCLUDED from a signal sweep: the
// asking process itself.
var OwnPid = os.Getpid

// normalizeTicks canonicalises a stat reading for comparison (the kernel's
// field is decimal with trailing whitespace).
func normalizeTicks(raw string) string {
	return strings.TrimSpace(raw)
}

// SignalIfSameIncarnation delivers sig to pid ONLY IF pid still names the
// process whose incarnation was recorded at decision time, and reports which
// of the three outcomes happened.
//
// A REFUSAL IS A RESULT, NOT AN ERROR (SignalSkippedIncarnationChanged): "the
// pid was reused" and "the delivery failed" are different sentences to an
// operator, so the return carries both the outcome and the reason.
//
// before, when non-nil, runs after the pre-flight incarnation check and before
// send — it is where a caller files its own provenance record, and its failure
// is REPORTED but never blocks the signal (the signal is the operation, the
// record is its evidence). send is the delivery itself; pass syscall.Kill
// unless a caller needs a seam.
func SignalIfSameIncarnation(
	pid int,
	recordedTicks string,
	sig syscall.Signal,
	before func(pid int, sig syscall.Signal) error,
	send func(pid int, sig syscall.Signal) error,
) (string, error) {
	if pid <= procSignalInitPid || pid == OwnPid() {
		return SignalSkippedIncarnationChanged,
			fmt.Errorf("pid %d is never a signal target", pid)
	}
	if recordedTicks == "" {
		return SignalSkippedIncarnationChanged,
			fmt.Errorf("pid %d has no recorded incarnation, so no signal may be addressed to it", pid)
	}
	// ① BEFORE — the pid must still be the process the decision was about.
	if now := ProcessIncarnationOf(pid); now != recordedTicks {
		return SignalSkippedIncarnationChanged,
			fmt.Errorf("pid %d incarnation changed before the signal (recorded %s, now %s)", pid, recordedTicks, now)
	}
	// ② the caller's own record, filed before the signal so a victim's own
	// account can name its killer instead of reading as UNSOURCED.
	if before != nil {
		if aerr := before(pid, sig); aerr != nil {
			fmt.Fprintf(os.Stderr, "could not file the provenance intent for pid %d: %v\n", pid, aerr)
		}
	}
	err := send(pid, sig)
	if err != nil && errors.Is(err, syscall.ESRCH) {
		return SignalAlreadyGone, err
	}
	// ③ AFTER — the same number must still name the same process. A reading
	// that differs (or that can no longer be made, with the kernel NOT
	// answering ESRCH for our signal) means this process never received what
	// we sent: report it as a refusal and claim nothing.
	after := ProcessIncarnationOf(pid)
	if after != recordedTicks {
		if err != nil {
			// The signal itself failed AND the number moved: the honest answer
			// is the failure, which is what the caller prints.
			return SignalSkippedIncarnationChanged, err
		}
		return SignalSkippedIncarnationChanged,
			fmt.Errorf("pid %d incarnation changed after the signal (recorded %s, now %s)", pid, recordedTicks, after)
	}
	if err != nil {
		return SignalDelivered, err
	}
	return SignalDelivered, nil
}

// QuoteOrNone renders an optional string fact for a log sentence.
func QuoteOrNone(s string) string {
	if s == "" {
		return "<none>"
	}
	return strconv.Quote(s)
}
